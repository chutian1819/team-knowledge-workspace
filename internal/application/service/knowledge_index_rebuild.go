package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const indexRebuildTTL = 24 * time.Hour
const indexRebuildBatchSize = 40

func indexRebuildKey(tenant uint64, kb string) string {
	return fmt.Sprintf("kb:index-rebuild:%d:%s", tenant, kb)
}

func (s *knowledgeService) saveIndexRebuild(ctx context.Context, key string, p *types.KnowledgeIndexRebuildProgress) error {
	p.UpdatedAt = time.Now()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if s.redisClient == nil {
		s.memIndexRebuildProgress.Store(key, raw)
		return nil
	}
	return s.redisClient.Set(ctx, key, raw, indexRebuildTTL).Err()
}

func (s *knowledgeService) loadIndexRebuild(ctx context.Context, key string) (*types.KnowledgeIndexRebuildProgress, error) {
	var raw []byte
	if s.redisClient == nil {
		value, ok := s.memIndexRebuildProgress.Load(key)
		if !ok {
			return nil, apperrors.NewNotFoundError("尚无索引重建任务")
		}
		raw = value.([]byte)
	} else {
		var err error
		raw, err = s.redisClient.Get(ctx, key).Bytes()
		if errors.Is(err, redis.Nil) {
			return nil, apperrors.NewNotFoundError("尚无索引重建任务")
		}
		if err != nil {
			return nil, err
		}
	}
	var p types.KnowledgeIndexRebuildProgress
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *knowledgeService) acquireIndexRebuild(ctx context.Context, key, task string) (bool, error) {
	if s.redisClient == nil {
		_, loaded := s.memIndexRebuildActive.LoadOrStore(key, task)
		return !loaded, nil
	}
	// Fail closed on Redis failure: a process-local fallback would allow
	// concurrent rebuilds in different replicas to erase each other's indexes.
	return s.redisClient.SetNX(ctx, key+":active", task, indexRebuildTTL).Result()
}

func (s *knowledgeService) ownsIndexRebuild(ctx context.Context, key, task string) (bool, error) {
	if s.redisClient == nil {
		value, ok := s.memIndexRebuildActive.Load(key)
		return ok && value == task, nil
	}
	value, err := s.redisClient.Get(ctx, key+":active").Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return value == task, err
}

func (s *knowledgeService) releaseIndexRebuild(ctx context.Context, key, task string) {
	if s.redisClient == nil {
		s.memIndexRebuildActive.CompareAndDelete(key, task)
		return
	}
	// Never release a successor's lease after a delayed worker wakes up.
	_, _ = s.redisClient.Eval(ctx, `if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) end return 0`, []string{key + ":active"}, task).Result()
}

func (s *knowledgeService) indexRebuildKB(ctx context.Context, kbID string, write bool) (*types.KnowledgeBase, error) {
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil {
		return nil, apperrors.NewNotFoundError("知识库不存在")
	}
	if write {
		if _, err := requireKBWrite(ctx, kb); err != nil {
			return nil, err
		}
		caller := types.CallerFromContext(ctx)
		if caller.TenantID != kb.TenantID {
			if _, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb, types.OrgRoleAdmin, s.kbShareService, nil); err != nil {
				return nil, apperrors.NewForbiddenError("重建索引需要知识库管理员共享权限")
			}
		}
	}
	return kb, nil
}

func (s *knowledgeService) GetKnowledgeBaseIndexRebuild(ctx context.Context, kbID string) (*types.KnowledgeIndexRebuildProgress, error) {
	kb, err := s.indexRebuildKB(ctx, kbID, false)
	if err != nil {
		return nil, err
	}
	if types.CallerFromContext(ctx).TenantID != kb.TenantID && !access.HasKBGrant(ctx, kb.ID, kb.TenantID, types.OrgRoleViewer) {
		return nil, apperrors.NewForbiddenError("无权查看该知识库")
	}
	return s.loadIndexRebuild(ctx, indexRebuildKey(kb.TenantID, kb.ID))
}

func (s *knowledgeService) indexRebuildConfig(ctx context.Context, kb *types.KnowledgeBase) (string, embedding.Embedder, []types.RetrieverType, error) {
	if kb == nil {
		return "", nil, nil, apperrors.NewNotFoundError("知识库不存在")
	}
	selected := []types.RetrieverType{}
	if kb.IsVectorEnabled() {
		selected = append(selected, types.VectorRetrieverType)
	}
	if kb.IsKeywordEnabled() {
		selected = append(selected, types.KeywordsRetrieverType)
	}
	if len(selected) == 0 {
		return "", nil, nil, apperrors.NewBadRequestError("请先启用向量或关键词索引")
	}
	if kb.Type == types.KnowledgeBaseTypeFAQ {
		return "", nil, nil, apperrors.NewBadRequestError("此重建接口仅支持文档资料集")
	}
	var model embedding.Embedder
	modelVersion := ""
	if kb.IsVectorEnabled() {
		if kb.EmbeddingModelID == "" {
			return "", nil, nil, apperrors.NewBadRequestError("请先配置 Embedding 模型")
		}
		row, err := s.modelService.GetModelByID(ctx, kb.EmbeddingModelID)
		if err != nil || row == nil || row.Type != types.ModelTypeEmbedding || row.Status != types.ModelStatusActive {
			return "", nil, nil, apperrors.NewBadRequestError("Embedding 模型不可用")
		}
		modelVersion = row.UpdatedAt.UTC().Format(time.RFC3339Nano)
		model, err = s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
		if err != nil || model == nil || model.GetDimensions() <= 0 {
			return "", nil, nil, apperrors.NewBadRequestError("Embedding 模型维度不可用")
		}
	}
	raw, _ := json.Marshal([]any{kb.EmbeddingModelID, modelVersion, kb.VectorStoreID, selected})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), model, selected, nil
}

func (s *knowledgeService) RebuildKnowledgeBaseIndex(ctx context.Context, kbID string) (*types.KnowledgeIndexRebuildProgress, error) {
	kb, err := s.indexRebuildKB(ctx, kbID, true)
	if err != nil {
		return nil, err
	}
	ctx = types.WithExecutionTenant(ctx, kb.TenantID)
	configHash, _, _, err := s.indexRebuildConfig(ctx, kb)
	if err != nil {
		return nil, err
	}
	sources, err := s.repo.ListKnowledgeByKnowledgeBaseID(ctx, kb.TenantID, kb.ID)
	if err != nil {
		return nil, err
	}
	if s.task == nil {
		return nil, apperrors.NewServiceUnavailableError("索引任务队列不可用")
	}
	key, taskID := indexRebuildKey(kb.TenantID, kb.ID), uuid.NewString()
	acquired, err := s.acquireIndexRebuild(ctx, key, taskID)
	if err != nil {
		return nil, apperrors.NewServiceUnavailableError("索引任务队列不可用")
	}
	if !acquired {
		p, err := s.loadIndexRebuild(ctx, key)
		if err != nil {
			return nil, apperrors.NewConflictError("索引重建正在提交，请稍后重试")
		}
		return p, nil
	}
	p := &types.KnowledgeIndexRebuildProgress{TaskID: taskID, KnowledgeBaseID: kb.ID, Status: "queued", CreatedAt: time.Now(), DocumentCount: len(sources), DocumentsTotal: len(sources),
		VectorEnabled: kb.IsVectorEnabled(), KeywordEnabled: kb.IsKeywordEnabled()}
	if err := s.saveIndexRebuild(ctx, key, p); err != nil {
		s.releaseIndexRebuild(ctx, key, taskID)
		return nil, apperrors.NewServiceUnavailableError("无法保存索引任务")
	}
	payload := types.KnowledgeIndexRebuildPayload{TaskID: taskID, TenantID: kb.TenantID, KnowledgeBaseID: kb.ID,
		ConfigHash: configHash, Caller: types.CallerFromContext(ctx), Initiator: types.TaskInitiatorFromContext(ctx)}
	langfuse.InjectTracing(ctx, &payload)
	raw, _ := json.Marshal(payload)
	_, err = s.task.Enqueue(asynq.NewTask(types.TypeKnowledgeIndexRebuild, raw), asynq.Queue(types.QueueMaintenance),
		asynq.TaskID(taskID), asynq.MaxRetry(2), asynq.Timeout(20*time.Minute))
	if err != nil {
		p.Status, p.Error = "failed", "任务入队失败，原件与分块保持不变；请重试"
		_ = s.saveIndexRebuild(context.WithoutCancel(ctx), key, p)
		s.releaseIndexRebuild(context.WithoutCancel(ctx), key, taskID)
		return nil, apperrors.NewServiceUnavailableError("索引任务入队失败")
	}
	return p, nil
}

func rebuildSourceFingerprint(source *types.Knowledge, chunks []*types.Chunk) string {
	ordered := append([]*types.Chunk(nil), chunks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	raw, _ := json.Marshal([]any{source, ordered})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func rebuildInventoryFingerprint(sources []*types.Knowledge) string {
	ordered := append([]*types.Knowledge(nil), sources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	raw, _ := json.Marshal(ordered)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (s *knowledgeService) ProcessKnowledgeIndexRebuild(ctx context.Context, task *asynq.Task) (resultErr error) {
	var payload types.KnowledgeIndexRebuildPayload
	if json.Unmarshal(task.Payload(), &payload) != nil || payload.TaskID == "" || payload.TenantID == 0 || payload.KnowledgeBaseID == "" || payload.Caller.TenantID == 0 {
		return fmt.Errorf("invalid index rebuild task: %w", asynq.SkipRetry)
	}
	key := indexRebuildKey(payload.TenantID, payload.KnowledgeBaseID)
	owned, err := s.ownsIndexRebuild(ctx, key, payload.TaskID)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("index rebuild task superseded: %w", asynq.SkipRetry)
	}
	p, err := s.loadIndexRebuild(ctx, key)
	if err != nil {
		return err
	}
	if p.TaskID != payload.TaskID {
		return fmt.Errorf("index rebuild task superseded: %w", asynq.SkipRetry)
	}
	var activeSource *types.Knowledge
	var activeChunks []*types.Chunk
	defer func() {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		owned, leaseErr := s.ownsIndexRebuild(finishCtx, key, payload.TaskID)
		if leaseErr != nil || !owned {
			return
		}
		if resultErr != nil {
			if activeSource != nil {
				_, _ = s.repo.UpdateSourceIndexRebuildState(finishCtx, activeSource, activeChunks, activeSource.EmbeddingModelID, "failed")
			}
			p.Status, p.Error = "failed", "索引重建未完成，部分索引可能已更新；原件、分块与历史保持不变，请修复模型或存储后重试"
			if !errors.Is(resultErr, asynq.SkipRetry) && summaryTaskWillRetry(ctx) {
				p.Status = "retrying"
			}
			logger.Warnf(finishCtx, "Index rebuild %s stopped (%T)", payload.TaskID, resultErr)
		} else {
			p.Status, p.Error = "completed", ""
		}
		if saveErr := s.saveIndexRebuild(finishCtx, key, p); saveErr != nil {
			if resultErr == nil {
				resultErr = fmt.Errorf("cannot persist index rebuild completion")
			}
			return // retain the lease so a queue retry cannot race a successor
		}
		if p.Status != "retrying" {
			s.releaseIndexRebuild(finishCtx, key, payload.TaskID)
		}
	}()
	ctx = types.WithCaller(payload.Initiator.Apply(ctx), payload.Caller)
	ctx = types.WithExecutionTenant(ctx, payload.TenantID)
	tenant, err := s.tenantRepo.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, payload.KnowledgeBaseID)
	if err != nil || kb == nil || kb.TenantID != payload.TenantID {
		return fmt.Errorf("index rebuild KB unavailable: %w", asynq.SkipRetry)
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: payload.Caller}, kb, types.OrgRoleAdmin, s.kbShareService, nil)
	if err != nil {
		return fmt.Errorf("index rebuild permission revoked: %w", asynq.SkipRetry)
	}
	ctx = grant.Context(ctx)
	configHash, model, selected, err := s.indexRebuildConfig(ctx, kb)
	if err != nil || configHash != payload.ConfigHash {
		return fmt.Errorf("index rebuild configuration changed: %w", asynq.SkipRetry)
	}
	engine, err := retriever.CreateRetrieveEngineForKB(ctx, s.retrieveEngine, s.ownership, kb.TenantID, kb.VectorStoreID)
	if err != nil {
		return err
	}
	engine, err = engine.SelectIndexTypes(selected)
	if err != nil {
		return fmt.Errorf("index rebuild store configuration unsupported: %w", asynq.SkipRetry)
	}
	sources, err := s.repo.ListKnowledgeByKnowledgeBaseID(ctx, kb.TenantID, kb.ID)
	if err != nil {
		return err
	}
	p.Status, p.Error = "running", ""
	p.DocumentCount = len(sources)
	p.DocumentsTotal, p.DocumentsIndexed, p.DocumentsSkipped, p.ChunksTotal, p.ChunksIndexed = len(sources), 0, 0, 0, 0
	if err = s.saveIndexRebuild(ctx, key, p); err != nil {
		return err
	}
	dimension := 0
	if model != nil {
		dimension = model.GetDimensions()
	}
	for _, source := range sources {
		if source.TenantID != kb.TenantID || source.KnowledgeBaseID != kb.ID {
			return fmt.Errorf("index rebuild source binding changed: %w", asynq.SkipRetry)
		}
		if isInFlightParseStatus(source.ParseStatus) || access.RejectMovingKnowledge(source) != nil {
			return fmt.Errorf("index rebuild source is busy: %w", asynq.SkipRetry)
		}
		if source.ParseStatus != types.ParseStatusCompleted && source.ParseStatus != "ready" {
			p.DocumentsSkipped++
			continue
		}
		chunks, err := s.chunkRepo.ListChunksByKnowledgeIDAndTypes(ctx, kb.TenantID, source.ID,
			[]types.ChunkType{types.ChunkTypeText, types.ChunkTypeImageOCR, types.ChunkTypeImageCaption})
		if err != nil {
			return err
		}
		prepared, err := s.repo.UpdateSourceIndexRebuildState(ctx, source, chunks, source.EmbeddingModelID, "processing")
		if err != nil {
			return err
		}
		if !prepared {
			return fmt.Errorf("source changed before indexing: %w", asynq.SkipRetry)
		}
		for _, chunk := range chunks {
			if strings.TrimSpace(chunk.Content) != "" {
				chunk.IndexStatus = "processing"
			}
		}
		activeSource, activeChunks = source, chunks
		fingerprint := rebuildSourceFingerprint(source, chunks)
		p.ChunksTotal += len(chunks)
		for start := 0; start < len(chunks); start += indexRebuildBatchSize {
			if err = ctx.Err(); err != nil {
				return err
			}
			owned, err := s.ownsIndexRebuild(ctx, key, payload.TaskID)
			if err != nil {
				return err
			}
			if !owned {
				return fmt.Errorf("index rebuild lease lost: %w", asynq.SkipRetry)
			}
			latestKB, err := s.kbService.GetKnowledgeBaseByID(ctx, kb.ID)
			if err != nil {
				return err
			}
			if latestKB == nil || latestKB.TenantID != payload.TenantID {
				return fmt.Errorf("index rebuild KB binding changed: %w", asynq.SkipRetry)
			}
			if _, err := access.ResolveKB(ctx, access.KBRequest{Caller: payload.Caller}, latestKB, types.OrgRoleAdmin, s.kbShareService, nil); err != nil {
				return fmt.Errorf("index rebuild permission revoked: %w", asynq.SkipRetry)
			}
			latestHash, _, _, err := s.indexRebuildConfig(ctx, latestKB)
			if err != nil || latestHash != payload.ConfigHash {
				return fmt.Errorf("configuration changed during rebuild: %w", asynq.SkipRetry)
			}
			end := min(start+indexRebuildBatchSize, len(chunks))
			batch := chunks[start:end]
			ids := make([]string, 0, len(batch))
			entries := make([]*types.IndexInfo, 0, len(batch))
			for _, chunk := range batch {
				if chunk.TenantID != kb.TenantID || chunk.KnowledgeID != source.ID || chunk.KnowledgeBaseID != kb.ID {
					return fmt.Errorf("index rebuild chunk binding changed: %w", asynq.SkipRetry)
				}
				ids = append(ids, chunk.ID)
				if strings.TrimSpace(chunk.Content) == "" {
					continue
				}
				entry := &types.IndexInfo{SourceID: chunk.ID, SourceType: types.ChunkSourceType,
					ChunkID: chunk.ID, KnowledgeID: source.ID, KnowledgeBaseID: kb.ID, KnowledgeType: kb.Type,
					Content: buildKnowledgeIndexContent(source, chunk.EmbeddingContent()), TagID: chunk.TagID,
					IsEnabled: chunk.IsEnabled && source.EnableStatus == "enabled"}
				entries = append(entries, entry)
				meta, err := chunk.DocumentMetadata()
				if err != nil {
					return err
				}
				if meta != nil {
					for _, question := range meta.GeneratedQuestions {
						if strings.TrimSpace(question.Question) == "" {
							continue
						}
						questionEntry := *entry
						questionEntry.SourceID = types.GeneratedQuestionSourceID(chunk.ID, question.ID)
						questionEntry.Content = buildKnowledgeIndexContent(source, question.Question)
						entries = append(entries, &questionEntry)
					}
				}
			}
			// Only derived index rows are replaced. This makes retries idempotent
			// even for engines whose BatchSave allocates a fresh row ID. Failure
			// is reported as partial; content and historical records stay intact.
			if err = engine.DeleteByChunkIDList(ctx, ids, dimension, kb.Type); err != nil {
				return err
			}
			if len(entries) > 0 {
				if err = engine.BatchIndex(ctx, model, entries); err != nil {
					return err
				}
			}
			p.ChunksIndexed += len(batch)
			if err = s.saveIndexRebuild(ctx, key, p); err != nil {
				return err
			}
		}
		current, err := s.repo.GetKnowledgeByID(ctx, kb.TenantID, source.ID)
		if err != nil {
			return err
		}
		currentChunks, err := s.chunkRepo.ListChunksByKnowledgeIDAndTypes(ctx, kb.TenantID, source.ID,
			[]types.ChunkType{types.ChunkTypeText, types.ChunkTypeImageOCR, types.ChunkTypeImageCaption})
		if err != nil {
			return err
		}
		if rebuildSourceFingerprint(current, currentChunks) != fingerprint {
			ids := make([]string, 0, len(chunks))
			for _, chunk := range chunks {
				ids = append(ids, chunk.ID)
			}
			_ = engine.DeleteByChunkIDList(ctx, ids, dimension, kb.Type)
			return fmt.Errorf("source changed during rebuild; retry when editing completes: %w", asynq.SkipRetry)
		}
		indexedModelID := ""
		if model != nil {
			indexedModelID = kb.EmbeddingModelID
		}
		committed, err := s.repo.UpdateSourceIndexRebuildState(ctx, source, chunks, indexedModelID, "ready")
		if err != nil {
			return err
		}
		if !committed {
			ids := make([]string, 0, len(chunks))
			for _, chunk := range chunks {
				ids = append(ids, chunk.ID)
			}
			_ = engine.DeleteByChunkIDList(ctx, ids, dimension, kb.Type)
			return fmt.Errorf("source changed before publishing index readiness: %w", asynq.SkipRetry)
		}
		// Keep the inventory comparison aware of this derived-only change.
		source.EmbeddingModelID = indexedModelID
		activeSource, activeChunks = nil, nil
		p.DocumentsIndexed++
	}
	currentKB, err := s.kbService.GetKnowledgeBaseByID(ctx, kb.ID)
	if err != nil {
		return err
	}
	currentHash, _, _, err := s.indexRebuildConfig(ctx, currentKB)
	if err != nil || currentHash != payload.ConfigHash {
		return fmt.Errorf("configuration changed during rebuild: %w", asynq.SkipRetry)
	}
	latestSources, err := s.repo.ListKnowledgeByKnowledgeBaseID(ctx, kb.TenantID, kb.ID)
	if err != nil {
		return err
	}
	if rebuildInventoryFingerprint(latestSources) != rebuildInventoryFingerprint(sources) {
		return fmt.Errorf("knowledge inventory changed during rebuild; retry when editing completes: %w", asynq.SkipRetry)
	}
	return nil
}
