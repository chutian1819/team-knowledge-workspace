package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const TypeResearchCapture = "research:capture"

type ResearchCaptureService struct {
	Repo   *repository.ResearchCaptureRepository
	kb     interfaces.KnowledgeBaseService
	kg     interfaces.KnowledgeService
	org    interfaces.OrganizationService
	share  interfaces.KBShareService
	tags   interfaces.KnowledgeTagService
	tenant interfaces.TenantService
	queue  interfaces.TaskEnqueuer
	models interfaces.ModelService
}

func NewResearchCaptureService(repo *repository.ResearchCaptureRepository, kb interfaces.KnowledgeBaseService, kg interfaces.KnowledgeService, org interfaces.OrganizationService, share interfaces.KBShareService, tags interfaces.KnowledgeTagService, tenant interfaces.TenantService, queue interfaces.TaskEnqueuer, models interfaces.ModelService) *ResearchCaptureService {
	return &ResearchCaptureService{repo, kb, kg, org, share, tags, tenant, queue, models}
}

func (s *ResearchCaptureService) owned(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	caller := types.CallerFromContext(ctx)
	kb, err := s.kb.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.TenantID != caller.TenantID || kb.CreatorID != caller.UserID || caller.UserID == "" {
		return nil, access.ErrForbidden
	}
	return kb, nil
}

// CurrentInbox resolves the durable binding without creating a collection or
// depending on its editable display name. A new user has no inbox yet.
func (s *ResearchCaptureService) CurrentInbox(ctx context.Context) (*types.KnowledgeBase, error) {
	c := types.CallerFromContext(ctx)
	if c.UserID == "" || c.TenantID == 0 {
		return nil, access.ErrForbidden
	}
	var binding types.ResearchInbox
	err := s.Repo.DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", c.TenantID, c.UserID).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kb, err := s.owned(ctx, binding.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if err = s.Repo.PersonalWorkspace(ctx, c.TenantID, c.UserID); err != nil {
		return nil, err
	}
	shares, err := s.share.ListSharesByKnowledgeBase(ctx, kb.ID, c.TenantID)
	if err != nil {
		return nil, err
	}
	if len(shares) > 0 {
		return nil, werrors.NewConflictError("inbox is shared; revoke its shares before using it as a private inbox")
	}
	if err = s.rejectSharedAgentAudience(ctx, c.TenantID, kb.ID); err != nil {
		return nil, err
	}
	return kb, nil
}

func (s *ResearchCaptureService) Inbox(ctx context.Context, template string) (*types.KnowledgeBase, error) {
	c := types.CallerFromContext(ctx)
	if err := s.Repo.PersonalWorkspace(ctx, c.TenantID, c.UserID); err != nil {
		return nil, err
	}
	var result *types.KnowledgeBase
	err := s.Repo.Serialize(ctx, fmt.Sprintf("research-inbox:%d:%s", c.TenantID, c.UserID), func() error {
		var binding types.ResearchInbox
		err := s.Repo.DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", c.TenantID, c.UserID).First(&binding).Error
		if err == nil {
			var e error
			result, e = s.owned(ctx, binding.KnowledgeBaseID)
			if e != nil {
				return e
			}
			shares, e := s.share.ListSharesByKnowledgeBase(ctx, result.ID, c.TenantID)
			if e != nil {
				return e
			}
			if len(shares) > 0 {
				return werrors.NewConflictError("inbox is shared; revoke its shares before using it as a private inbox")
			}
			return s.rejectSharedAgentAudience(ctx, c.TenantID, result.ID)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		kb := &types.KnowledgeBase{Name: "未分类", Description: "尚未分类的个人资料，仅自己可见。", Type: types.KnowledgeBaseTypeDocument}
		if template != "" {
			src, e := s.owned(ctx, template)
			if e != nil {
				return e
			}
			if src.Type != types.KnowledgeBaseTypeDocument {
				return werrors.NewBadRequestError("template must be a document knowledge base")
			}
			kb.EmbeddingModelID = src.EmbeddingModelID
			kb.SummaryModelID = src.SummaryModelID
			kb.ChunkingConfig = src.ChunkingConfig
			kb.VLMConfig = src.VLMConfig
			kb.StorageBackendID = src.StorageBackendID
			kb.StorageProviderConfig = src.StorageProviderConfig
			kb.VectorStoreID = src.VectorStoreID
			kb.IndexingStrategy = src.IndexingStrategy
			kb.ImageProcessingConfig = src.ImageProcessingConfig
		} else if s.models != nil {
			available, e := s.models.ListModels(ctx)
			if e != nil {
				return e
			}
			applyResearchInboxModels(kb, available)
		}
		// Stable ID allows recovery if KB creation succeeded before the binding write failed.
		kb.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("weknora-research-inbox:%d:%s", c.TenantID, c.UserID))).String()
		if existing, e := s.kb.GetKnowledgeBaseByID(ctx, kb.ID); e == nil && existing != nil {
			if existing.CreatorID != c.UserID {
				return access.ErrForbidden
			}
			result = existing
		} else {
			result, err = s.kb.CreateKnowledgeBase(ctx, kb)
			if err != nil {
				return err
			}
		}
		if err := s.rejectSharedAgentAudience(ctx, c.TenantID, result.ID); err != nil {
			return err
		}
		return s.Repo.DB.WithContext(ctx).Create(&types.ResearchInbox{TenantID: c.TenantID, UserID: c.UserID, KnowledgeBaseID: result.ID}).Error
	})
	return result, err
}

// Only the caller's accessible active models are candidates. Explicit defaults
// win; never borrow credentials from another user's private model or replace
// the settings of an existing inbox/template.
func applyResearchInboxModels(kb *types.KnowledgeBase, available []*types.Model) {
	choose := func(kind types.ModelType) string {
		var chosen *types.Model
		for _, model := range available {
			if model == nil || model.Type != kind || model.Status != types.ModelStatusActive {
				continue
			}
			if chosen == nil || (model.IsDefault && !chosen.IsDefault) ||
				(model.IsDefault == chosen.IsDefault && model.IsBuiltin && !chosen.IsBuiltin) {
				chosen = model
			}
		}
		if chosen != nil {
			return chosen.ID
		}
		return ""
	}
	kb.EmbeddingModelID = choose(types.ModelTypeEmbedding)
	kb.SummaryModelID = choose(types.ModelTypeKnowledgeQA)
	if id := choose(types.ModelTypeVLLM); id != "" {
		kb.ImageProcessingConfig.ModelID = id
		kb.VLMConfig = types.VLMConfig{Enabled: true, ModelID: id}
	}
}

func exactResearchAudience(actual []*types.OrganizationTenantMember, selected []uint64) bool {
	if len(actual) != len(selected) || len(actual) == 0 {
		return false
	}
	set := map[uint64]bool{}
	for _, id := range selected {
		if id == 0 || set[id] {
			return false
		}
		set[id] = true
	}
	for _, member := range actual {
		if member == nil || !set[member.TenantID] {
			return false
		}
	}
	return true
}
func (s *ResearchCaptureService) validateAudience(ctx context.Context, j *types.ResearchCaptureJob) error {
	members, err := s.org.ListTenantMembers(ctx, j.OrganizationID)
	if err != nil {
		return err
	}
	if !exactResearchAudience(members, j.SelectedTenantIDs) {
		return werrors.NewConflictError("selected workspaces must exactly match every current project member; refresh membership and confirm the complete audience")
	}
	role, err := s.org.GetTenantRoleInOrg(ctx, j.OrganizationID, j.TenantID)
	if err != nil {
		return err
	}
	if !role.HasPermission(types.OrgRoleEditor) {
		return access.ErrForbidden
	}
	shares, err := s.share.ListSharesByKnowledgeBase(ctx, j.TargetKBID, j.TenantID)
	if err != nil {
		return err
	}
	for _, share := range shares {
		if share.OrganizationID != j.OrganizationID || share.Permission != types.OrgRoleViewer {
			return werrors.NewConflictError("target must be dedicated to this project with read-only sharing")
		}
	}
	// Sharing a previously private target grants its WHOLE contents. It must
	// not silently publish older unselected documents already in that target.
	if len(shares) == 0 {
		targetItems, err := s.kg.ListKnowledgeByKnowledgeBaseID(ctx, j.TargetKBID)
		if err != nil {
			return err
		}
		if !researchTargetContainsOnlySelection(targetItems, j.Items) {
			return werrors.NewConflictError("unshared target contains other documents; choose an empty project collection or a collection already shared to this project")
		}
		var pages []types.WikiPage
		if err := s.Repo.DB.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", j.TenantID, j.TargetKBID).Find(&pages).Error; err != nil {
			return err
		}
		if !researchTargetPagesFromSelection(pages, j.Items) {
			return werrors.NewConflictError("unshared target contains existing research pages; choose an empty collection or a collection already shared to this project")
		}

	}
	return s.rejectSharedAgentAudience(ctx, j.TenantID, j.TargetKBID)
}
func researchTargetContainsOnlySelection(target []*types.Knowledge, selected []types.ResearchCaptureItem) bool {
	allowed := map[string]bool{}
	for _, item := range selected {
		if item.KnowledgeID != "" {
			allowed[item.KnowledgeID] = true
		}
	}
	for _, item := range target {
		if item == nil || !allowed[item.ID] {
			return false
		}
	}
	return true
}
func researchTargetPagesFromSelection(pages []types.WikiPage, items []types.ResearchCaptureItem) bool {
	allowed := map[string]bool{}
	for _, item := range items {
		allowed[item.KnowledgeID] = true
	}
	for _, page := range pages {
		ids := page.SourceKnowledgeIDs()
		if len(ids) == 0 {
			return false
		} // An authored/index page is not evidence selected by this operation.
		for _, id := range ids {
			if !allowed[id] {
				return false
			}
		}
	}
	return true
}

func (s *ResearchCaptureService) rejectSharedAgentAudience(ctx context.Context, tenant uint64, kb string) error {
	var agents []types.CustomAgent
	if err := s.Repo.DB.WithContext(ctx).Where("tenant_id = ?", tenant).Find(&agents).Error; err != nil {
		return err
	}
	for _, agent := range agents {
		selected := agent.Config.KBSelectionMode == "all"
		if agent.Config.KBSelectionMode != "none" {
			for _, id := range agent.Config.KnowledgeBases {
				selected = selected || id == kb
			}
		}
		if !selected {
			continue
		}
		var count int64
		if err := s.Repo.DB.WithContext(ctx).Model(&types.AgentShare{}).Where("agent_id = ? AND source_tenant_id = ?", agent.ID, tenant).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return werrors.NewConflictError("collection is accessible through a shared agent; remove that access or choose a dedicated collection")
		}
	}
	return nil
}

func captureRequestHash(kind, kb string, req types.ResearchCaptureRequest) string {
	req.IdempotencyKey = ""
	raw, _ := json.Marshal(struct {
		Kind    string
		KB      string
		Request types.ResearchCaptureRequest
	}{kind, kb, req})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *ResearchCaptureService) Start(ctx context.Context, kbID, kind string, req types.ResearchCaptureRequest) (*types.ResearchCaptureJob, error) {
	caller := types.CallerFromContext(ctx)
	source, err := s.owned(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 {
		return nil, werrors.NewBadRequestError("idempotency_key must be 1–128 characters")
	}
	if kind != "bookmarks" && kind != "share" {
		return nil, werrors.NewBadRequestError("invalid job kind")
	}
	requestHash := captureRequestHash(kind, kbID, req)
	var existing types.ResearchCaptureJob
	lookup := s.Repo.DB.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", caller.TenantID, req.IdempotencyKey).First(&existing).Error
	if lookup == nil {
		if existing.RequestHash != requestHash || existing.UserID != caller.UserID {
			return nil, werrors.NewConflictError("idempotency key already used by another request")
		}
		if existing.Status == "queued" {
			if err := s.enqueue(&existing); err != nil {
				return nil, err
			}
		}
		return &existing, nil
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return nil, lookup
	}
	j := &types.ResearchCaptureJob{ID: uuid.NewString(), TenantID: caller.TenantID, UserID: caller.UserID, KnowledgeBaseID: kbID, Kind: kind, IdempotencyKey: req.IdempotencyKey, Status: "queued", TargetKBID: req.TargetKBID, OrganizationID: req.OrganizationID, SelectedTenantIDs: req.SelectedTenantIDs, Initiator: types.TaskInitiatorFromContext(ctx)}
	if kind == "bookmarks" {
		if len(req.Items) == 0 || len(req.Items) > ResearchCaptureMaxItems {
			return nil, werrors.NewBadRequestError("provide 1–5000 bookmarks")
		}
		seen := map[string]bool{}
		for _, item := range req.Items {
			url, e := canonicalBookmarkURL(item.URL)
			if e != nil {
				return nil, werrors.NewBadRequestError(e.Error())
			}
			if seen[url] {
				continue
			}
			seen[url] = true
			if len(item.Title) > 2048 || len(item.Folder) > 2048 {
				return nil, werrors.NewBadRequestError("bookmark title or folder too long")
			}
			j.Items = append(j.Items, types.ResearchCaptureItem{URL: url, Title: item.Title, Folder: item.Folder, AddedAt: item.AddedAt, Status: "queued"})
		}
	} else {
		if len(req.KnowledgeIDs) == 0 || len(req.KnowledgeIDs) > 500 {
			return nil, werrors.NewBadRequestError("select 1–500 documents")
		}
		target, e := s.owned(ctx, req.TargetKBID)
		if e != nil {
			return nil, e
		}
		tenant, _ := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
		if err := access.ValidateKBTransferCompatibility(source, target, access.KBTransferMove, "reuse_vectors", tenant); err != nil {
			return nil, werrors.NewBadRequestError(err.Error())
		}
		if err := s.validateAudience(ctx, j); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, id := range req.KnowledgeIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			k, e := s.kg.GetKnowledgeByID(ctx, id)
			if e != nil {
				return nil, e
			}
			if k.TenantID != caller.TenantID || k.KnowledgeBaseID != kbID || k.ParseStatus != types.ParseStatusCompleted {
				return nil, werrors.NewBadRequestError("all selected documents must belong to the source and finish processing")
			}
			j.Items = append(j.Items, types.ResearchCaptureItem{KnowledgeID: id, Title: k.Title, Status: "queued"})
		}
		tagMap, e := s.kg.GetKnowledgeTags(ctx, req.KnowledgeIDs)
		if e != nil {
			return nil, e
		}
		for i := range j.Items {
			for _, tag := range tagMap[j.Items[i].KnowledgeID] {
				j.Items[i].Tags = append(j.Items[i].Tags, types.ResearchCaptureTag{Name: tag.Name, Color: tag.Color})
			}
		}
	}
	j.RequestHash = requestHash
	j.Total = len(j.Items)
	saved, err := s.Repo.Create(ctx, j)
	if err != nil {
		return nil, err
	}
	if saved.Status == "queued" {
		if err := s.enqueue(saved); err != nil {
			return saved, err
		}
	}
	return saved, nil
}
func (s *ResearchCaptureService) enqueue(j *types.ResearchCaptureJob) error {
	payload, _ := json.Marshal(map[string]any{"id": j.ID, "tenant_id": j.TenantID})
	_, err := s.queue.Enqueue(asynq.NewTask(TypeResearchCapture, payload), asynq.Queue(types.QueueMaintenance), asynq.TaskID("research-capture:"+j.ID), asynq.MaxRetry(3), asynq.Timeout(20*time.Minute))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return werrors.NewServiceUnavailableError("could not enqueue capture task; retry with the same idempotency key")
	}
	return nil
}
func (s *ResearchCaptureService) Retry(ctx context.Context, kb, id string) (*types.ResearchCaptureJob, error) {
	j, err := s.Job(ctx, kb, id)
	if err != nil {
		return nil, err
	}
	if j.Status == "completed" {
		return j, nil
	}
	if err := s.Repo.PrepareRetry(ctx, j); err != nil {
		return nil, err
	}
	if j.Status == "completed" {
		return j, nil
	}
	// A fresh delivery ID allows an explicitly retried archived task to run.
	payload, _ := json.Marshal(map[string]any{"id": j.ID, "tenant_id": j.TenantID})
	_, err = s.queue.Enqueue(asynq.NewTask(TypeResearchCapture, payload), asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(3), asynq.Timeout(20*time.Minute))
	return j, err
}
func (s *ResearchCaptureService) Job(ctx context.Context, kb, id string) (*types.ResearchCaptureJob, error) {
	c := types.CallerFromContext(ctx)
	j, err := s.Repo.Get(ctx, c.TenantID, id)
	if err != nil {
		return nil, err
	}
	if j.UserID != c.UserID || j.KnowledgeBaseID != kb {
		return nil, access.ErrForbidden
	}
	if _, err = s.owned(ctx, kb); err != nil {
		return nil, err
	}
	return j, nil
}
func (s *ResearchCaptureService) Jobs(ctx context.Context, kb string) ([]types.ResearchCaptureJob, error) {
	if _, err := s.owned(ctx, kb); err != nil {
		return nil, err
	}
	return s.Repo.List(ctx, types.CallerFromContext(ctx).TenantID, kb)
}

func (s *ResearchCaptureService) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p struct {
		ID       string `json:"id"`
		TenantID uint64 `json:"tenant_id"`
	}
	if err := json.Unmarshal(t.Payload(), &p); err != nil || p.ID == "" || p.TenantID == 0 {
		return fmt.Errorf("invalid capture task: %w", asynq.SkipRetry)
	}
	j, err := s.Repo.Get(ctx, p.TenantID, p.ID)
	if err != nil {
		return err
	}
	if j.Status == "completed" {
		return nil
	}
	claimed, err := s.Repo.Claim(ctx, j)
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("capture task currently leased")
	}
	defer func() { j.LeaseUntil = nil; j.UpdatedAt = time.Now(); _ = s.Repo.Save(context.WithoutCancel(ctx), j) }()
	fail := func(err error) error { j.Status = "failed"; j.Error = err.Error(); return err }
	// Re-evaluate membership and ownership at execution, so revoked callers cannot finish queued work.
	member, err := s.Repo.CurrentMember(ctx, j.TenantID, j.UserID)
	if err != nil {
		return fail(access.ErrForbidden)
	}
	if member.Role == types.TenantRoleViewer {
		return fail(access.ErrForbidden)
	}
	ctx = j.Initiator.Apply(ctx)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: j.TenantID, UserID: j.UserID, Role: member.Role})
	ctx = types.WithExecutionTenant(ctx, j.TenantID)
	tenant, err := s.tenant.GetTenantByID(ctx, j.TenantID)
	if err != nil {
		return fail(err)
	}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	source, err := s.owned(ctx, j.KnowledgeBaseID)
	if err != nil {
		return fail(err)
	}
	ctx, err = access.WithKBTaskWrite(ctx, source, j.TenantID)
	if err != nil {
		return fail(err)
	}
	if j.Kind == "share" {
		if _, err := s.owned(ctx, j.TargetKBID); err != nil {
			return fail(err)
		}
		if err := s.validateAudience(ctx, j); err != nil {
			return fail(err)
		}
	}
	j.Completed = 0
	j.Failed = 0
	var combined error
	for i := range j.Items {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		item := &j.Items[i]
		if item.Status == "saved" || item.Status == "shared" || item.Status == "moved" {
			j.Completed++
			continue
		}
		item.Error = ""
		if j.Kind == "bookmarks" {
			err = s.importItem(ctx, j, item)
		} else {
			err = s.shareItem(ctx, j, item)
		}
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			j.Failed++
			combined = errors.Join(combined, err)
		} else {
			j.Completed++
		}
		j.UpdatedAt = time.Now()
		if err := s.Repo.Save(ctx, j); err != nil {
			return fail(err)
		}
	}
	if j.Failed > 0 {
		j.Status = "partial"
		j.Error = "Some items failed; successful items will be skipped on retry"
		return combined
	}
	if j.Kind == "share" {
		if err := s.Repo.WithAudienceLock(ctx, j.OrganizationID, func() error {
			if err := s.validateAudience(ctx, j); err != nil {
				return err
			}
			_, err := s.share.ShareKnowledgeBase(ctx, j.TargetKBID, j.OrganizationID, j.UserID, j.TenantID, types.OrgRoleViewer)
			return err
		}); err != nil {
			return fail(err)
		}
		for i := range j.Items {
			j.Items[i].Status = "shared"
		}
	}
	j.Status = "completed"
	j.Error = ""
	return nil
}
func (s *ResearchCaptureService) importItem(ctx context.Context, j *types.ResearchCaptureJob, item *types.ResearchCaptureItem) error {
	return s.Repo.Serialize(ctx, fmt.Sprintf("research-url:%d:%s:%s", j.TenantID, j.KnowledgeBaseID, item.URL), func() error {
		var k *types.Knowledge
		var err error
		// Native duplicate detection returns the existing object together with a duplicate error.
		if item.KnowledgeID != "" {
			k, err = s.kg.GetKnowledgeByID(ctx, item.KnowledgeID)
		} else {
			k, err = s.kg.CreateKnowledgeFromURL(ctx, j.KnowledgeBaseID, item.URL, "", "", nil, item.Title, nil, types.ChannelBrowserExtension, nil)
		}
		if k == nil {
			if err == nil {
				return fmt.Errorf("native ingestion returned no document")
			}
			return err
		}
		if err != nil {
			var dup *types.DuplicateKnowledgeError
			if !errors.As(err, &dup) {
				return err
			}
		}
		if k.TenantID != j.TenantID {
			return access.ErrForbidden
		}
		if k.KnowledgeBaseID != j.KnowledgeBaseID {
			// Re-capture of evidence already moved out of the inbox is a
			// reference to the same record, only while it is still ours.
			var binding types.ResearchInbox
			if err := s.Repo.DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND knowledge_base_id = ?",
				j.TenantID, j.UserID, j.KnowledgeBaseID).First(&binding).Error; err != nil {
				return access.ErrForbidden
			}
			if _, err := s.owned(ctx, k.KnowledgeBaseID); err != nil {
				return access.ErrForbidden
			}
		}
		item.KnowledgeID = k.ID
		// Update only metadata while holding the row; never overwrite native parse state.
		if err := s.Repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var live types.Knowledge
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", k.ID, j.TenantID).First(&live).Error; err != nil {
				return err
			}
			values := map[string]any{}
			if len(live.CustomMetadata) > 0 {
				if err := json.Unmarshal(live.CustomMetadata, &values); err != nil {
					return err
				}
			}
			if _, exists := values["bookmark_source"]; exists {
				return nil
			}
			values["bookmark_source"] = map[string]any{"url": item.URL, "folder": item.Folder, "added_at": item.AddedAt, "captured_by": j.UserID, "captured_at": j.CreatedAt}
			raw, err := json.Marshal(values)
			if err != nil {
				return err
			}
			return tx.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ?", k.ID, j.TenantID).Update("custom_metadata", types.JSON(raw)).Error
		}); err != nil {
			return err
		}
		item.Status = "saved"
		return nil
	})
}
func (s *ResearchCaptureService) shareItem(ctx context.Context, j *types.ResearchCaptureJob, item *types.ResearchCaptureItem) error {
	if _, err := s.owned(ctx, j.KnowledgeBaseID); err != nil {
		return err
	}
	if _, err := s.owned(ctx, j.TargetKBID); err != nil {
		return err
	}
	member, err := s.Repo.CurrentMember(ctx, j.TenantID, j.UserID)
	if err != nil || member.Role == types.TenantRoleViewer {
		return access.ErrForbidden
	}
	if err := s.validateAudience(ctx, j); err != nil {
		return err
	}
	// Capture the latest source tags immediately before moving, then persist
	// the checkpoint before the native move clears source-local relations.
	if !item.TagsCaptured {
		current, err := s.kg.GetKnowledgeByID(ctx, item.KnowledgeID)
		if err != nil {
			return err
		}
		if current.KnowledgeBaseID == j.KnowledgeBaseID {
			tagMap, err := s.kg.GetKnowledgeTags(ctx, []string{item.KnowledgeID})
			if err != nil {
				return err
			}
			item.Tags = nil
			for _, tag := range tagMap[item.KnowledgeID] {
				item.Tags = append(item.Tags, types.ResearchCaptureTag{Name: tag.Name, Color: tag.Color})
			}
			item.TagsCaptured = true
			// Native audit targets are VARCHAR(64); two concatenated UUIDs
			// exceed that limit. Persist a stable UUID before the first move.
			item.MoveTaskID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("research-move:"+j.ID+":"+item.KnowledgeID)).String()
			item.Status = "moving"
			if err := s.Repo.Save(ctx, j); err != nil {
				return err
			}
		}
	}
	// Call the native move worker with a stable per-item task identity. Native checkpoints
	// keep unchanged document IDs and allow recovery after a move succeeded but this job crashed.
	taskID := item.MoveTaskID
	if taskID == "" {
		// Older in-flight jobs already have this identity in native recovery
		// metadata. Keep it when resuming instead of stranding a partial move.
		taskID = j.ID + ":" + item.KnowledgeID
	}
	payload, _ := json.Marshal(types.KnowledgeMovePayload{TenantID: j.TenantID, TaskID: taskID, SourceKBID: j.KnowledgeBaseID, TargetKBID: j.TargetKBID, KnowledgeIDs: []string{item.KnowledgeID}, Mode: "reuse_vectors", Initiator: j.Initiator})
	if err := s.kg.ProcessKnowledgeMove(ctx, asynq.NewTask(types.TypeKnowledgeMove, payload)); err != nil {
		return err
	}
	target, err := s.owned(ctx, j.TargetKBID)
	if err != nil {
		return err
	}
	writeCtx, err := access.WithKBTaskWrite(ctx, target, j.TenantID)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, tag := range item.Tags {
		t, err := s.tags.FindOrCreateTagByName(writeCtx, target.ID, tag.Name)
		if err != nil {
			return err
		}
		ids = append(ids, t.ID)
	}
	if err := s.kg.SetKnowledgeTags(writeCtx, item.KnowledgeID, ids); err != nil {
		return err
	}
	item.Status = "moved"
	return nil
}
