package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	pgindex "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type rebuildKBLookup struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s rebuildKBLookup) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type rebuildTenants struct {
	interfaces.TenantRepository
	tenant *types.Tenant
}

func (s rebuildTenants) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return s.tenant, nil
}

type rebuildQueue struct {
	task  *asynq.Task
	calls int
	err   error
}

func (q *rebuildQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.task = task
	q.calls++
	return &asynq.TaskInfo{}, q.err
}

// Deliberately deterministic test vectors, not a semantic-quality assertion.
// These fault/identity tests use the real source, chunk and index repositories.
type rebuildFixtureEmbedder struct {
	calls     int
	hook      func()
	traceHook func(context.Context)
}

func (e *rebuildFixtureEmbedder) GetModelName() string { return "fixture" }
func (e *rebuildFixtureEmbedder) GetModelID() string   { return "embedding" }
func (e *rebuildFixtureEmbedder) GetDimensions() int   { return 2 }
func (e *rebuildFixtureEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0}, nil
}
func (e *rebuildFixtureEmbedder) BatchEmbed(ctx context.Context, inputs []string) ([][]float32, error) {
	e.calls++
	if e.traceHook != nil {
		e.traceHook(ctx)
	}
	if e.hook != nil {
		e.hook()
	}
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = []float32{1, float32(i + 1)}
	}
	return out, nil
}
func (e *rebuildFixtureEmbedder) BatchEmbedWithPool(ctx context.Context, _ embedding.Embedder, inputs []string) ([][]float32, error) {
	return e.BatchEmbed(ctx, inputs)
}

type rebuildModels struct {
	interfaces.ModelService
	model    *types.Model
	embedder *rebuildFixtureEmbedder
}

func (m rebuildModels) GetModelByID(context.Context, string) (*types.Model, error) {
	return m.model, nil
}
func (m rebuildModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.embedder, nil
}

type rebuildShare struct {
	interfaces.KBShareService
	permission types.OrgMemberRole
}

func (s *rebuildShare) CheckTenantKBPermission(context.Context, string, uint64, types.TenantRole) (types.OrgMemberRole, bool, error) {
	return s.permission, s.permission.IsValid(), nil
}

type rebuildHarness struct {
	svc      *knowledgeService
	db       *gorm.DB
	ctx      context.Context
	kb       *types.KnowledgeBase
	queue    *rebuildQueue
	embedder *rebuildFixtureEmbedder
}

func newRebuildHarness(t *testing.T, vector, keyword bool) *rebuildHarness {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Chunk{}, &types.ResearchSourceVersion{}, &types.KnowledgeTag{}))
	require.NoError(t, db.Exec(`CREATE TABLE embeddings (id INTEGER PRIMARY KEY AUTOINCREMENT, created_at datetime, updated_at datetime, source_id TEXT NOT NULL, source_type INTEGER NOT NULL, chunk_id TEXT, knowledge_id TEXT, knowledge_base_id TEXT, tag_id TEXT, content TEXT, dimension INTEGER, embedding TEXT, is_enabled BOOLEAN, UNIQUE(source_id,source_type))`).Error)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7, CreatorID: "owner", Type: "document", EmbeddingModelID: "embedding", IndexingStrategy: types.IndexingStrategy{VectorEnabled: vector, KeywordEnabled: keyword, GraphEnabled: !vector && !keyword}}
	tenant := &types.Tenant{ID: 7, RetrieverEngines: types.RetrieverEngines{Engines: []types.RetrieverEngineParams{
		{RetrieverEngineType: types.PostgresRetrieverEngineType, RetrieverType: types.VectorRetrieverType},
		{RetrieverEngineType: types.PostgresRetrieverEngineType, RetrieverType: types.KeywordsRetrieverType},
	}}}
	engine := retriever.NewKVHybridRetrieveEngine(pgindex.NewPostgresRetrieveEngineRepository(db), types.PostgresRetrieverEngineType)
	registry := retriever.NewRetrieveEngineRegistry(nil, nil)
	require.NoError(t, registry.Register(engine))
	queue := &rebuildQueue{}
	embedder := &rebuildFixtureEmbedder{}
	svc := &knowledgeService{repo: repository.NewKnowledgeRepository(db), chunkRepo: repository.NewChunkRepository(db), kbService: rebuildKBLookup{kb: kb},
		tenantRepo: rebuildTenants{tenant: tenant}, task: queue, retrieveEngine: registry, modelService: rebuildModels{model: &types.Model{ID: "embedding", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, UpdatedAt: time.Unix(1, 0)}, embedder: embedder}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "owner", Role: types.TenantRoleAdmin})
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.OrgRoleEditor, nil, nil)
	require.NoError(t, err)
	ctx = grant.Context(ctx)
	source := &types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "kb", Title: "Original evidence", Type: "file", FileType: "png", FileHash: "immutable-file-hash", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, svc.repo.CreateKnowledge(ctx, source))
	for i, typ := range []string{types.ChunkTypeText, types.ChunkTypeImageOCR, types.ChunkTypeImageCaption, types.ChunkTypeParentText, types.ChunkTypeImageVector} {
		require.NoError(t, svc.chunkRepo.CreateChunks(ctx, []*types.Chunk{{ID: "chunk-" + typ, TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: "doc", ChunkType: typ, Content: "evidence " + typ, ChunkIndex: i, IsEnabled: true, IndexStatus: "failed", Status: int(types.ChunkStatusStored)}}))
	}
	require.NoError(t, db.Create(&types.ResearchSourceVersion{ID: "version", TenantID: 7, KnowledgeID: "doc", ContentHash: "immutable-file-hash", OriginalPath: "original.png", MarkdownPath: "original.md"}).Error)
	return &rebuildHarness{svc: svc, db: db, ctx: ctx, kb: kb, queue: queue, embedder: embedder}
}
func (h *rebuildHarness) sourceSnapshot(t *testing.T) string {
	t.Helper()
	var sources []types.Knowledge
	var chunks []types.Chunk
	var versions []types.ResearchSourceVersion
	require.NoError(t, h.db.Order("id").Find(&sources).Error)
	require.NoError(t, h.db.Order("id").Find(&chunks).Error)
	require.NoError(t, h.db.Order("id").Find(&versions).Error)
	// Derived readiness is allowed to change; original data and timestamps are not.
	for i := range sources {
		sources[i].EmbeddingModelID = ""
	}
	for i := range chunks {
		chunks[i].IndexStatus = ""
		chunks[i].Status = 0
	}
	raw, err := json.Marshal([]any{sources, chunks, versions})
	require.NoError(t, err)
	return string(raw)
}

func TestIndexRebuildRealRepositoriesPreserveSourcesAndAreIdempotent(t *testing.T) {
	h := newRebuildHarness(t, true, false)
	before := h.sourceSnapshot(t)
	first, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	duplicate, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, first.TaskID, duplicate.TaskID)
	require.Equal(t, 1, h.queue.calls)
	require.NoError(t, h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task))
	progress, err := h.svc.GetKnowledgeBaseIndexRebuild(h.ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, "completed", progress.Status)
	require.Equal(t, 3, progress.ChunksIndexed)
	var source types.Knowledge
	require.NoError(t, h.db.First(&source, "id = ?", "doc").Error)
	require.Equal(t, "embedding", source.EmbeddingModelID)
	var ready int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("index_status = ? AND status = ?", "ready", types.ChunkStatusIndexed).Count(&ready).Error)
	require.EqualValues(t, 3, ready)
	var rows []struct {
		ChunkID   string
		Dimension int
	}
	require.NoError(t, h.db.Table("embeddings").Order("chunk_id").Find(&rows).Error)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, 2, row.Dimension)
		require.NotContains(t, row.ChunkID, "parent_text")
		require.NotContains(t, row.ChunkID, "image_vector")
	}
	_, err = h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	require.NoError(t, h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task))
	var count int64
	require.NoError(t, h.db.Table("embeddings").Count(&count).Error)
	require.EqualValues(t, 3, count)
	require.Equal(t, before, h.sourceSnapshot(t))
}

func TestIndexRebuildKeywordOnlyDoesNotCallEmbedding(t *testing.T) {
	h := newRebuildHarness(t, false, true)
	h.kb.EmbeddingModelID = ""
	_, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	require.NoError(t, h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task))
	require.Zero(t, h.embedder.calls)
	var count int64
	require.NoError(t, h.db.Table("embeddings").Where("dimension = 0").Count(&count).Error)
	require.EqualValues(t, 3, count)
}

func TestIndexRebuildRepositoryFailureRetriesWithoutTouchingSources(t *testing.T) {
	h := newRebuildHarness(t, true, true)
	before := h.sourceSnapshot(t)
	require.NoError(t, h.db.Exec(`CREATE TRIGGER reject_index BEFORE INSERT ON embeddings BEGIN SELECT RAISE(FAIL, 'index store unavailable'); END`).Error)
	_, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	ctx := types.WithTaskRetryMetadata(context.Background(), 0, 2)
	require.Error(t, h.svc.ProcessKnowledgeIndexRebuild(ctx, h.queue.task))
	progress, err := h.svc.GetKnowledgeBaseIndexRebuild(h.ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, "retrying", progress.Status)
	var failed int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("index_status = ? AND chunk_type IN ?", "failed", []string{types.ChunkTypeText, types.ChunkTypeImageOCR, types.ChunkTypeImageCaption}).Count(&failed).Error)
	require.EqualValues(t, 3, failed)
	require.Equal(t, before, h.sourceSnapshot(t))
	require.NoError(t, h.db.Exec(`DROP TRIGGER reject_index`).Error)
	require.NoError(t, h.svc.ProcessKnowledgeIndexRebuild(types.WithTaskRetryMetadata(context.Background(), 1, 2), h.queue.task))
	progress, err = h.svc.GetKnowledgeBaseIndexRebuild(h.ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, "completed", progress.Status)
	require.Equal(t, before, h.sourceSnapshot(t))
}

func TestIndexRebuildEnqueueFailureReleasesLease(t *testing.T) {
	h := newRebuildHarness(t, true, false)
	h.queue.err = errors.New("queue unavailable")
	_, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.Error(t, err)
	progress, err := h.svc.GetKnowledgeBaseIndexRebuild(h.ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, "failed", progress.Status)
	h.queue.err = nil
	next, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	require.NotEqual(t, progress.TaskID, next.TaskID)
}

func TestIndexRebuildRejectsConfigurationChangeAndBusySources(t *testing.T) {
	for _, change := range []string{"config", "busy"} {
		t.Run(change, func(t *testing.T) {
			h := newRebuildHarness(t, true, false)
			_, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
			require.NoError(t, err)
			if change == "config" {
				h.kb.EmbeddingModelID = "other"
			} else {
				require.NoError(t, h.db.Model(&types.Knowledge{}).Where("id = ?", "doc").Update("parse_status", types.ParseStatusProcessing).Error)
			}
			err = h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task)
			require.ErrorIs(t, err, asynq.SkipRetry)
			require.Zero(t, h.embedder.calls)
		})
	}
}

func TestIndexRebuildDetectsConcurrentChunkEditWithoutOverwritingIt(t *testing.T) {
	h := newRebuildHarness(t, true, false)
	h.embedder.hook = func() {
		require.NoError(t, h.db.Model(&types.Chunk{}).Where("id = ?", "chunk-text").Update("content", "user edit during rebuild").Error)
	}
	_, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	require.ErrorIs(t, h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task), asynq.SkipRetry)
	var chunk types.Chunk
	require.NoError(t, h.db.First(&chunk, "id = ?", "chunk-text").Error)
	require.Equal(t, "user edit during rebuild", chunk.Content)
	var count int64
	require.NoError(t, h.db.Table("embeddings").Count(&count).Error)
	require.Zero(t, count)
}

func TestIndexRebuildScopeAndSharedPermission(t *testing.T) {
	h := newRebuildHarness(t, true, false)
	for _, role := range []types.OrgMemberRole{types.OrgRoleViewer, types.OrgRoleEditor, types.OrgRoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			shares := &rebuildShare{permission: role}
			h.svc.kbShareService = shares
			caller := types.Caller{TenantID: 8, UserID: "receiver", Role: types.TenantRoleAdmin}
			ctx := types.WithCaller(context.Background(), caller)
			grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, h.kb, role, shares, nil)
			require.NoError(t, err)
			ctx = grant.Context(ctx)
			_, err = h.svc.RebuildKnowledgeBaseIndex(ctx, "kb")
			if role != types.OrgRoleAdmin {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			// Revoke the share after admission; the worker must recheck it.
			shares.permission = types.OrgRoleViewer
			require.ErrorIs(t, h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task), asynq.SkipRetry)
		})
	}
	stranger := types.WithExecutionTenant(types.WithCaller(context.Background(), types.Caller{TenantID: 99, Role: types.TenantRoleAdmin}), 7)
	_, err := h.svc.GetKnowledgeBaseIndexRebuild(stranger, "kb")
	require.Error(t, err)
}

func TestIndexRebuildRechecksShareBetweenBatches(t *testing.T) {
	h := newRebuildHarness(t, true, false)
	shares := &rebuildShare{permission: types.OrgRoleAdmin}
	h.svc.kbShareService = shares
	caller := types.Caller{TenantID: 8, UserID: "receiver", Role: types.TenantRoleAdmin}
	ctx := types.WithCaller(context.Background(), caller)
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, h.kb, types.OrgRoleEditor, shares, nil)
	require.NoError(t, err)
	for i := 0; i < 41; i++ {
		require.NoError(t, h.svc.chunkRepo.CreateChunks(h.ctx, []*types.Chunk{{ID: fmt.Sprintf("extra-%d", i), TenantID: 7, KnowledgeID: "doc", KnowledgeBaseID: "kb", ChunkType: types.ChunkTypeText, Content: "extra evidence", IsEnabled: true, ChunkIndex: i + 10}}))
	}
	h.embedder.hook = func() { shares.permission = types.OrgRoleViewer }
	_, err = h.svc.RebuildKnowledgeBaseIndex(grant.Context(ctx), "kb")
	require.NoError(t, err)
	require.ErrorIs(t, h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task), asynq.SkipRetry)
	require.Equal(t, 1, h.embedder.calls)
	var ready int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("index_status = ? AND chunk_type = ?", "ready", types.ChunkTypeText).Count(&ready).Error)
	require.Zero(t, ready)
}

func TestIndexRebuildReadinessCheckpointRollsBackOnRepositoryFailure(t *testing.T) {
	h := newRebuildHarness(t, true, false)
	before := h.sourceSnapshot(t)
	require.NoError(t, h.db.Exec(`CREATE TRIGGER reject_ready BEFORE UPDATE OF embedding_model_id ON knowledges BEGIN SELECT RAISE(FAIL, 'checkpoint unavailable'); END`).Error)
	_, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	require.Error(t, h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task))
	var ready int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("index_status = ?", "ready").Count(&ready).Error)
	require.Zero(t, ready)
	require.Equal(t, before, h.sourceSnapshot(t))
}

func TestIndexRebuildRedisLeaseCoordinatesReplicasAndFailsClosed(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	t.Cleanup(func() { _ = client.Close() })
	first, second := newRebuildHarness(t, true, false), newRebuildHarness(t, true, false)
	first.svc.redisClient, second.svc.redisClient = client, client
	p, err := first.svc.RebuildKnowledgeBaseIndex(first.ctx, "kb")
	require.NoError(t, err)
	duplicate, err := second.svc.RebuildKnowledgeBaseIndex(second.ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, p.TaskID, duplicate.TaskID)
	require.Zero(t, second.queue.calls)
	key := indexRebuildKey(7, "kb")
	first.svc.releaseIndexRebuild(first.ctx, key, "stale-task")
	owned, err := first.svc.ownsIndexRebuild(first.ctx, key, p.TaskID)
	require.NoError(t, err)
	require.True(t, owned)
	require.NoError(t, first.svc.ProcessKnowledgeIndexRebuild(context.Background(), first.queue.task))
	next, err := second.svc.RebuildKnowledgeBaseIndex(second.ctx, "kb")
	require.NoError(t, err)
	require.NotEqual(t, p.TaskID, next.TaskID)
	server.Close()
	_, err = first.svc.RebuildKnowledgeBaseIndex(first.ctx, "kb")
	require.Error(t, err)
	require.Equal(t, 1, first.queue.calls)
}

func TestIndexRebuildExcludesArchivedAndDisabledEvidenceRemainsDisabled(t *testing.T) {
	h := newRebuildHarness(t, true, false)
	require.NoError(t, h.svc.repo.CreateKnowledge(h.ctx, &types.Knowledge{ID: "archived", TenantID: 7, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusArchived, EnableStatus: "disabled"}))
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("id = ?", "chunk-text").UpdateColumn("is_enabled", false).Error)
	_, err := h.svc.RebuildKnowledgeBaseIndex(h.ctx, "kb")
	require.NoError(t, err)
	require.NoError(t, h.svc.ProcessKnowledgeIndexRebuild(context.Background(), h.queue.task))
	p, err := h.svc.GetKnowledgeBaseIndexRebuild(h.ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, 1, p.DocumentsSkipped)
	var enabled bool
	require.NoError(t, h.db.Table("embeddings").Select("is_enabled").Where("chunk_id = ?", "chunk-text").Scan(&enabled).Error)
	require.False(t, enabled)
}

// This tests the real enqueue/worker OTLP bridge with an isolated receiver and
// deterministic vectors; it is not a live-provider semantic acceptance claim.
func TestIndexRebuildTracingConnectsEnqueueWorkerAndEmbedding(t *testing.T) {
	mgr, exported, _ := researchTraceReceiver(t, false)
	h := newRebuildHarness(t, true, false)
	ctx, request := mgr.StartTrace(h.ctx, langfuse.TraceOptions{Name: "POST /api/v1/knowledge-bases/:id/rebuild-index"})
	h.embedder.traceHook = func(ctx context.Context) {
		_, generation := mgr.StartGeneration(ctx, langfuse.GenerationOptions{Name: "embedding.fixture", Model: "fixture"})
		generation.Finish("3 vectors", nil, nil)
	}
	_, err := h.svc.RebuildKnowledgeBaseIndex(ctx, "kb")
	require.NoError(t, err)
	request.Finish(nil, nil)
	var payload types.KnowledgeIndexRebuildPayload
	require.NoError(t, json.Unmarshal(h.queue.task.Payload(), &payload))
	require.NotEmpty(t, payload.LangfuseTraceparent)
	require.Equal(t, request.ID, payload.LangfuseTraceID)
	worker := langfuse.AsynqMiddleware()(asynq.HandlerFunc(h.svc.ProcessKnowledgeIndexRebuild))
	require.NoError(t, worker.ProcessTask(context.Background(), h.queue.task))
	require.NoError(t, mgr.Shutdown(context.Background()))
	var workerSpanID, embeddingParent string
	for _, span := range exported() {
		if span.Name != "asynq."+types.TypeKnowledgeIndexRebuild && span.Name != "embedding.fixture" {
			continue
		}
		require.Equal(t, request.ID, hex.EncodeToString(span.TraceId))
		if span.Name == "asynq."+types.TypeKnowledgeIndexRebuild {
			workerSpanID = hex.EncodeToString(span.SpanId)
		}
		if span.Name == "embedding.fixture" {
			embeddingParent = hex.EncodeToString(span.ParentSpanId)
		}
	}
	require.NotEmpty(t, workerSpanID)
	require.Equal(t, workerSpanID, embeddingParent)
}
