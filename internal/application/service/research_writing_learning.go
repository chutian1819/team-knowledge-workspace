package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const TypeResearchWritingLearning = "research:writing-learning"

type ResearchWritingLearningRequest struct {
	Enabled bool   `json:"enabled"`
	ModelID string `json:"model_id"`
}
type ResearchWritingLearningStatus struct {
	Enabled   bool   `json:"enabled"`
	ModelID   string `json:"model_id"`
	Status    string `json:"status"`
	LastError string `json:"last_error"`
}
type researchWritingWorkerKey struct{}
type researchWritingLearningPayload struct {
	types.TracingContext
	EventID string `json:"event_id"`
}

func (s *ResearchDraftService) WritingLearning(ctx context.Context, kbID, pageID string) (*ResearchWritingLearningStatus, error) {
	if _, err := s.Get(ctx, kbID, pageID); err != nil {
		return nil, err
	}
	result := &ResearchWritingLearningStatus{Status: "disabled"}
	var setting types.ResearchWritingLearningSetting
	caller := types.CallerFromContext(ctx)
	err := s.db.WithContext(ctx).Where("caller_tenant_id = ? AND created_by = ? AND page_id = ? AND knowledge_base_id = ?", caller.TenantID, researchWritingAuthor(ctx), pageID, kbID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result.Enabled, result.ModelID = setting.Enabled, setting.ModelID
	if !setting.Enabled {
		return result, nil
	}
	result.Status = "idle"
	var job types.ResearchWritingExtraction
	err = s.db.WithContext(ctx).Where("caller_tenant_id = ? AND created_by = ? AND page_id = ?", caller.TenantID, researchWritingAuthor(ctx), pageID).Order("updated_at DESC").First(&job).Error
	if err == nil {
		result.Status, result.LastError = job.Status, job.Error
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return result, nil
}

func (s *ResearchDraftService) SetWritingLearning(ctx context.Context, kbID, pageID string, req ResearchWritingLearningRequest) (result *ResearchWritingLearningStatus, resultErr error) {
	ctx, observation := startResearchObservation(ctx, "learning.configure", kbID, pageID, map[string]any{"learning_enabled": req.Enabled, "model_id": req.ModelID})
	defer func() { observation.Finish(resultErr) }()
	ctx, _, err := s.authorize(ctx, kbID, true)
	if err != nil {
		return nil, err
	}
	if _, err = s.Get(ctx, kbID, pageID); err != nil {
		return nil, err
	}
	caller := types.CallerFromContext(ctx)
	// Workers recheck human account and membership. API-key service identities
	// cannot become an unattended grant without a persisted human membership.
	if req.Enabled && (caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) || types.TaskInitiatorFromContext(ctx).APIKeyID != 0) {
		return nil, researchInput("automatic writing learning requires a signed-in user")
	}
	if len(req.ModelID) > 128 {
		return nil, researchInput("model_id exceeds 128 bytes")
	}
	setting := &types.ResearchWritingLearningSetting{CallerTenantID: caller.TenantID, CreatedBy: researchWritingAuthor(ctx), PageID: pageID, KnowledgeBaseID: kbID, Enabled: req.Enabled, ModelID: strings.TrimSpace(req.ModelID), UpdatedAt: time.Now()}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "caller_tenant_id"}, {Name: "created_by"}, {Name: "page_id"}}, DoUpdates: clause.AssignmentColumns([]string{"enabled", "model_id", "updated_at"})}).Create(setting).Error
	if err != nil {
		return nil, err
	}
	return s.WritingLearning(ctx, kbID, pageID)
}

func newWritingExtraction(ctx context.Context, event *types.ResearchWritingEvent, modelID string, automatic bool) *types.ResearchWritingExtraction {
	caller, _ := json.Marshal(types.CallerFromContext(ctx))
	initiator, _ := json.Marshal(types.TaskInitiatorFromContext(ctx))
	return &types.ResearchWritingExtraction{EventID: event.ID, KnowledgeBaseID: event.KnowledgeBaseID, PageID: event.PageID, CallerTenantID: types.CallerFromContext(ctx).TenantID, CreatedBy: researchWritingAuthor(ctx), ModelID: modelID, Automatic: automatic, Caller: caller, Initiator: initiator, Status: "queued", CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

// Queue errors are visible in the durable extraction row; a successful human
// save is never rolled back because inference or its queue is unavailable.
func (s *ResearchDraftService) enqueueWritingLearning(ctx context.Context, event *types.ResearchWritingEvent) {
	if writingEventLearnable(event) != nil || (event.Kind != "save" && event.Kind != "accept") {
		return
	}
	var setting types.ResearchWritingLearningSetting
	if err := s.db.WithContext(ctx).Where("caller_tenant_id = ? AND created_by = ? AND page_id = ? AND enabled = ?", types.CallerFromContext(ctx).TenantID, researchWritingAuthor(ctx), event.PageID, true).First(&setting).Error; err != nil {
		return
	}
	job := newWritingExtraction(ctx, event, setting.ModelID, true)
	result := s.db.WithContext(context.WithoutCancel(ctx)).Clauses(clause.OnConflict{DoNothing: true}).Create(job)
	if result.Error != nil || result.RowsAffected == 0 {
		return
	}
	var err error
	ctx, observation := startResearchObservation(ctx, "learning.enqueue", event.KnowledgeBaseID, event.PageID, map[string]any{"model_id": job.ModelID, "automatic": true})
	ctx = observation.Event(ctx, event)
	defer func() { observation.Finish(err) }()
	payload := researchWritingLearningPayload{EventID: event.ID}
	langfuse.InjectTracing(ctx, &payload)
	raw, _ := json.Marshal(payload)
	if s.task == nil {
		err = errors.New("writing learning task queue is unavailable; retry learning manually")
	} else {
		_, err = s.task.Enqueue(asynq.NewTask(TypeResearchWritingLearning, raw), asynq.Queue(types.QueueMaintenance), asynq.TaskID("research-writing-learning:"+event.ID), asynq.MaxRetry(3), asynq.Timeout(4*time.Minute), asynq.Retention(24*time.Hour))
	}
	if err != nil {
		_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(job).Updates(map[string]any{"status": "failed", "error": safeWritingLearningError(err), "updated_at": time.Now()}).Error
	}
}

func (s *ResearchDraftService) currentWritingWorkerCaller(ctx context.Context, job *types.ResearchWritingExtraction) (context.Context, error) {
	var caller types.Caller
	var initiator types.TaskInitiator
	if json.Unmarshal(job.Caller, &caller) != nil || caller.TenantID == 0 || caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) {
		return ctx, access.ErrForbidden
	}
	_ = json.Unmarshal(job.Initiator, &initiator)
	// API credentials are deliberately not restored as an authorization grant.
	if initiator.APIKeyID != 0 {
		return ctx, access.ErrForbidden
	}
	var user types.User
	if err := s.db.WithContext(ctx).Where("id = ? AND is_active = ?", caller.UserID, true).First(&user).Error; err != nil {
		return ctx, access.ErrForbidden
	}
	var member types.TenantMember
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND status = ?", caller.TenantID, caller.UserID, types.TenantMemberStatusActive).First(&member).Error; err != nil {
		return ctx, access.ErrForbidden
	}
	caller.Role = member.Role
	ctx = initiator.Apply(ctx)
	ctx = context.WithValue(ctx, types.UserIDContextKey, caller.UserID)
	ctx = types.WithCaller(ctx, caller)
	ctx = types.WithExecutionTenant(ctx, caller.TenantID)
	ctx = context.WithValue(ctx, researchWritingWorkerKey{}, job)
	return ctx, nil
}

func (s *ResearchDraftService) recheckWritingWorker(ctx context.Context) (context.Context, error) {
	job, worker := ctx.Value(researchWritingWorkerKey{}).(*types.ResearchWritingExtraction)
	if !worker {
		return ctx, nil
	}
	ctx, err := s.currentWritingWorkerCaller(ctx, job)
	if err != nil {
		return ctx, err
	}
	var setting types.ResearchWritingLearningSetting
	if err = s.db.WithContext(ctx).Where("caller_tenant_id = ? AND created_by = ? AND page_id = ? AND enabled = ?", job.CallerTenantID, job.CreatedBy, job.PageID, true).First(&setting).Error; err != nil {
		return ctx, researchInput("automatic writing learning was disabled before completion")
	}
	// A queued job belongs to the exact consent/configuration in force when
	// the edit was saved. Disabling and re-enabling learning, or choosing a
	// different model, must not revive old work with the previous model.
	if setting.UpdatedAt.After(job.CreatedAt) {
		return ctx, researchInput("automatic writing learning settings changed; retry this edit manually")
	}
	return ctx, nil
}

func (s *ResearchDraftService) HandleResearchWritingLearning(ctx context.Context, task *asynq.Task) error {
	var payload researchWritingLearningPayload
	if json.Unmarshal(task.Payload(), &payload) != nil || payload.EventID == "" {
		return fmt.Errorf("invalid writing learning task: %w", asynq.SkipRetry)
	}
	// Also supports the in-process Lite dispatcher; Asynq middleware already
	// owns its active parent and AttachTraceparent leaves that intact.
	ctx = langfuse.AttachTraceparent(ctx, payload.LangfuseTraceparent)
	ctx = langfuse.WithObservationIdentity(ctx, payload.LangfuseUserID, payload.LangfuseSessionID)
	var job types.ResearchWritingExtraction
	if err := s.db.WithContext(ctx).First(&job, "event_id = ?", payload.EventID).Error; err != nil {
		return err
	}
	if job.Status == "completed" {
		return nil
	}
	// A later explicit extraction supersedes a queued automatic task.
	if !job.Automatic {
		return nil
	}
	ctx, err := s.currentWritingWorkerCaller(ctx, &job)
	if err == nil {
		ctx, err = s.recheckWritingWorker(ctx)
	}
	if err != nil {
		_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(&job).Where("automatic = ? AND status IN ?", true, []string{"queued", "failed"}).Updates(map[string]any{"status": "failed", "error": safeWritingLearningError(err), "lease_until": nil, "updated_at": time.Now()}).Error
		return fmt.Errorf("writing learning access or setting changed: %w", asynq.SkipRetry)
	}
	ctx, observation := startResearchObservation(ctx, "learning.worker", job.KnowledgeBaseID, job.PageID, map[string]any{"event_id": job.EventID, "model_id": job.ModelID, "automatic": true})
	defer func() { observation.Finish(err) }()
	_, err = s.ExtractWritingEventRules(ctx, job.KnowledgeBaseID, job.PageID, job.EventID, ResearchWritingExtractRequest{ModelID: job.ModelID})
	if err != nil {
		// Authorization may fail before an extraction lease is acquired. Keep
		// queued-task failures observable without touching a newer live lease.
		_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(&types.ResearchWritingExtraction{}).Where("event_id = ? AND automatic = ? AND status IN ?", job.EventID, true, []string{"queued", "failed"}).Updates(map[string]any{"status": "failed", "error": safeWritingLearningError(err), "updated_at": time.Now()}).Error
	}
	return err
}

func (s *ResearchDraftService) ExtractWritingEventRules(ctx context.Context, kbID, pageID, eventID string, req ResearchWritingExtractRequest) (rules []*types.ResearchWritingRule, err error) {
	ctx, observation := startResearchObservation(ctx, "rules.extract_event", kbID, pageID, map[string]any{"event_id": eventID, "model_id": req.ModelID})
	defer func() {
		if err == nil {
			observation.Rules(ctx, rules)
		}
		observation.Finish(err)
	}()
	ctx, kb, err := s.authorize(ctx, kbID, true)
	if err != nil {
		return nil, err
	}
	event, page, err := s.writingSourceEvent(ctx, kbID, pageID, eventID)
	if err != nil {
		return nil, err
	}
	if err = writingEventLearnable(event); err != nil {
		return nil, err
	}
	ctx = observation.Event(ctx, event)
	job := newWritingExtraction(ctx, event, req.ModelID, false)
	if worker, ok := ctx.Value(researchWritingWorkerKey{}).(*types.ResearchWritingExtraction); ok {
		job = worker
	}
	ctx = observation.With(ctx, map[string]any{"automatic": job.Automatic, "model_id": req.ModelID})
	if err = s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(job).Error; err != nil {
		return nil, err
	}
	var stored types.ResearchWritingExtraction
	if err = s.db.WithContext(ctx).First(&stored, "event_id = ?", eventID).Error; err != nil {
		return nil, err
	}
	if stored.Status == "completed" {
		ctx = observation.With(ctx, map[string]any{"idempotent_replay": true, "model_id": stored.ModelID})
		rules = []*types.ResearchWritingRule{}
		err = s.db.WithContext(ctx).Where("source_event_id = ? AND knowledge_base_id = ?", eventID, kbID).Order("created_at ASC, id ASC").Find(&rules).Error
		if err != nil {
			return nil, err
		}
		// Idempotent retries still return source-derived text. Recheck the
		// extraction's full prompt context, not only the saved event lineage.
		for _, rule := range rules {
			if err = s.validateWritingRuleSource(ctx, kbID, rule); err != nil {
				return nil, err
			}
		}
		return rules, nil
	}
	now := time.Now()
	lease := now.Add(4 * time.Minute)
	result := s.db.WithContext(ctx).Model(&types.ResearchWritingExtraction{}).Where("event_id = ? AND status <> ? AND (status <> ? OR lease_until IS NULL OR lease_until < ?)", eventID, "completed", "processing", now).
		Updates(map[string]any{"status": "processing", "error": "", "lease_until": lease, "updated_at": now, "caller": job.Caller, "initiator": job.Initiator, "caller_tenant_id": job.CallerTenantID, "created_by": job.CreatedBy, "model_id": job.ModelID, "automatic": job.Automatic})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: writing learning is already processing", ErrResearchConflict)
	}
	defer func() {
		if err != nil {
			_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(&types.ResearchWritingExtraction{}).Where("event_id = ? AND status = ? AND lease_until = ?", eventID, "processing", lease).Updates(map[string]any{"status": "failed", "error": safeWritingLearningError(err), "lease_until": nil, "updated_at": time.Now()}).Error
		}
	}()
	contextEvents, err := s.writingEventContext(ctx, event)
	if err != nil {
		return nil, err
	}
	feedback, err := s.writingEventFeedback(ctx, event)
	if err != nil {
		return nil, err
	}

	input, err := json.Marshal(map[string]any{"event_id": event.ID, "kind": event.Kind, "instruction": event.Instruction, "base_content": event.BaseContent, "content": event.Content, "writing_rules": json.RawMessage(event.WritingRules), "writing_feedback": feedback, "writing_activity": contextEvents})
	if err != nil {
		return nil, err
	}
	if len(input) > 120000 {
		return nil, researchInput("writing sample and prompt context exceed 120,000 bytes; no partial learning was performed")
	}
	output, modelID, err := s.inferWritingCandidates(ctx, input, req.ModelID, researchTemplateID(page))
	ctx = observation.With(ctx, map[string]any{"model_id": modelID})
	if err != nil {
		return nil, err
	}
	ctx, err = s.recheckWritingWorker(ctx)
	if err != nil {
		return nil, err
	}
	if _, _, err = s.authorize(ctx, kbID, true); err != nil {
		return nil, err
	}
	if _, _, err = s.writingSourceEvent(ctx, kbID, pageID, eventID); err != nil {
		return nil, err
	}
	// Context provenance can also be revoked while the model is running.
	for _, item := range contextEvents {
		if err = s.validateWritingEventSourcesVisited(ctx, kbID, item, map[string]int{}, 0); err != nil {
			return nil, err
		}
	}
	hash := sha256.Sum256(input)
	contextIDs := make([]string, 0, len(contextEvents))
	for _, item := range contextEvents {
		contextIDs = append(contextIDs, item.ID)
	}
	metadata, _ := json.Marshal(map[string]any{"extraction_version": researchWritingExtractionVersion, "extraction_prompt": researchWritingExtractionPrompt, "model_id": modelID, "sample_sha256": hex.EncodeToString(hash[:]), "source_event_id": event.ID, "writing_feedback": feedback, "context_event_count": len(contextEvents), "context_event_ids": contextIDs})
	rules = []*types.ResearchWritingRule{}
	for _, candidate := range output.Rules {
		rules = append(rules, &types.ResearchWritingRule{ID: uuid.NewString(), TenantID: kb.TenantID, KnowledgeBaseID: kbID, Name: strings.TrimSpace(candidate.Name), Instruction: strings.TrimSpace(candidate.Instruction), TemplateID: researchTemplateID(page), Status: "candidate", Version: 1, SourcePageID: page.ID, SourceEventID: event.ID, CreatedBy: researchWritingAuthor(ctx), Metadata: metadata})
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lease ownership prevents an expired worker from committing after a retry.
		claim := tx.Model(&types.ResearchWritingExtraction{}).Where("event_id = ? AND status = ? AND lease_until = ?", eventID, "processing", lease).Updates(map[string]any{"status": "completed", "error": "", "model_id": modelID, "lease_until": nil, "updated_at": time.Now()})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrResearchConflict
		}
		if len(rules) > 0 {
			return tx.Create(&rules).Error
		}
		return nil
	})
	return rules, err
}
