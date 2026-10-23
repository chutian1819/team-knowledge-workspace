package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

type researchDocumentExporter interface {
	ExportResearch(context.Context, string, string, string, map[string][]byte) ([]byte, error)
}

type researchExportPayload struct {
	ID     string       `json:"id"`
	Caller types.Caller `json:"caller"`
}

var researchExportGate = make(chan struct{}, 1)
var researchImagePattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

func (s *ResearchDraftService) ListExports(ctx context.Context, kbID, pageID string) ([]*types.ResearchExport, error) {
	kb, err := s.kb.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return nil, access.ErrNotFound
	}
	if kb.WikiConfig != nil && kb.WikiConfig.ResearchOnly {
		if _, _, err := s.authorize(ctx, kbID, false); err != nil {
			return nil, err
		}
	} else if _, _, err := s.authorizeAssets(ctx, kbID); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Where("knowledge_base_id = ?", kbID)
	if pageID != "" {
		query = query.Where("page_id = ?", pageID)
	}
	rows := []*types.ResearchExport{}
	if err := query.Order("created_at DESC").Limit(30).Find(&rows).Error; err != nil {
		return nil, err
	}
	visible := make([]*types.ResearchExport, 0, len(rows))
	for _, row := range rows {
		// A restricted original-asset ZIP is never surfaced to a viewer.
		if _, err := s.GetExport(ctx, kbID, row.ID); err == nil {
			visible = append(visible, row)
		}
	}
	return visible, nil
}

func (s *ResearchDraftService) RequestExport(ctx context.Context, kbID, pageID, format string) (*types.ResearchExport, error) {
	if format != "md" && format != "docx" && format != "pdf" {
		return nil, researchInput("format must be md, docx or pdf")
	}
	page, err := s.Get(ctx, kbID, pageID)
	if err != nil {
		return nil, err
	}
	var pending int64
	caller := types.CallerFromContext(ctx)
	if err = s.db.WithContext(ctx).Model(&types.ResearchExport{}).
		Where("knowledge_base_id = ? AND status IN ? AND created_at > ?", kbID, []string{"queued", "processing"}, time.Now().Add(-15*time.Minute)).
		Count(&pending).Error; err != nil {
		return nil, err
	}
	if pending >= 3 {
		return nil, researchInput("at most three pending exports per research space; wait for completion")
	}
	export := &types.ResearchExport{ID: uuid.NewString(), TenantID: page.TenantID,
		KnowledgeBaseID: kbID, PageID: pageID, Version: page.Version, Format: format,
		Status: "queued", FileName: "research-" + page.ID + "." + format, CreatedBy: caller.UserID}
	if err = s.db.WithContext(ctx).Create(export).Error; err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(researchExportPayload{ID: export.ID, Caller: caller})
	if s.task == nil {
		err = errors.New("task queue unavailable")
	} else {
		_, err = s.task.Enqueue(asynq.NewTask(types.TypeResearchExport, payload),
			asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(2), asynq.Timeout(3*time.Minute))
	}
	if err != nil {
		s.db.WithContext(ctx).Model(export).Updates(map[string]any{"status": "failed", "error": "export queue unavailable"})
		return nil, err
	}
	return export, nil
}

func (s *ResearchDraftService) GetExport(ctx context.Context, kbID, id string) (*types.ResearchExport, error) {
	var export types.ResearchExport
	if err := s.db.WithContext(ctx).Where("id = ? AND knowledge_base_id = ?", id, kbID).First(&export).Error; err != nil {
		return nil, access.ErrNotFound
	}
	if export.Format == "zip" {
		if _, _, err := s.authorizeAssets(ctx, kbID); err != nil {
			return nil, err
		}
		if err := s.validateArchiveMembership(ctx, &export); err != nil {
			return nil, err
		}
	} else if _, err := s.Get(ctx, kbID, export.PageID); err != nil {
		return nil, err
	}
	return &export, nil
}

func (s *ResearchDraftService) DownloadExport(ctx context.Context, kbID, id string) (io.ReadCloser, *types.ResearchExport, error) {
	export, err := s.GetExport(ctx, kbID, id)
	if err != nil {
		return nil, nil, err
	}
	if export.Status != "ready" || export.FilePath == "" {
		return nil, nil, researchInput("export is not ready")
	}
	reader, err := s.files.GetFile(ctx, export.FilePath)
	return reader, export, err
}

// HandleResearchExport runs in the existing maintenance worker pool; the
// upload pool is never occupied by a conversion. The additional process-local
// gate bounds conversion pressure, and docreader has its own global gate.
func (s *ResearchDraftService) HandleResearchExport(ctx context.Context, task *asynq.Task) (err error) {
	var payload researchExportPayload
	if err = json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}
	select {
	case researchExportGate <- struct{}{}:
		defer func() { <-researchExportGate }()
	default:
		retry, hasRetry := asynq.GetRetryCount(ctx)
		maxRetry, hasMax := asynq.GetMaxRetry(ctx)
		if hasRetry && hasMax && retry >= maxRetry {
			s.db.WithContext(context.WithoutCancel(ctx)).Model(&types.ResearchExport{}).
				Where("id = ?", payload.ID).Updates(map[string]any{
				"status": "failed", "error": "export converter busy; request a new export", "updated_at": time.Now()})
		}
		return errors.New("export converter busy")
	}
	var export types.ResearchExport
	if err = s.db.WithContext(ctx).First(&export, "id = ?", payload.ID).Error; err != nil {
		return fmt.Errorf("%w: export missing", asynq.SkipRetry)
	}
	if export.Status == "ready" {
		return nil
	}
	ctx = types.WithCaller(ctx, payload.Caller)
	ctx = types.WithExecutionTenant(ctx, payload.Caller.TenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, payload.Caller.UserID)
	// Re-read current account/membership rather than trusting queued role data.
	if payload.Caller.UserID != "" && !types.IsSyntheticUserID(payload.Caller.UserID) {
		var user types.User
		if e := s.db.WithContext(ctx).Where("id = ? AND is_active = ?", payload.Caller.UserID, true).First(&user).Error; e != nil {
			err = access.ErrForbidden
		} else {
			var member types.TenantMember
			if e := s.db.WithContext(ctx).Where("user_id = ? AND tenant_id = ? AND status = ?",
				payload.Caller.UserID, payload.Caller.TenantID, types.TenantMemberStatusActive).First(&member).Error; e != nil {
				err = access.ErrForbidden
			} else {
				payload.Caller.Role = member.Role
				ctx = types.WithCaller(ctx, payload.Caller)
			}
		}
	}
	defer func() {
		if err != nil {
			status := "failed"
			retry, hasRetry := asynq.GetRetryCount(ctx)
			maxRetry, hasMax := asynq.GetMaxRetry(ctx)
			if hasRetry && hasMax && retry < maxRetry && !errors.Is(err, asynq.SkipRetry) {
				status = "queued"
			}
			s.db.WithContext(context.WithoutCancel(ctx)).Model(&export).Updates(map[string]any{
				"status": status, "error": err.Error(), "updated_at": time.Now()})
		}
	}()
	if err != nil {
		return fmt.Errorf("%w: access revoked", asynq.SkipRetry)
	}
	if err = s.db.WithContext(ctx).Model(&export).Updates(map[string]any{"status": "processing", "error": "", "updated_at": time.Now()}).Error; err != nil {
		return err
	}
	if export.Format == "zip" {
		data, buildErr := s.buildAssetArchive(ctx, &export)
		if buildErr != nil {
			return buildErr
		}
		stored, saveErr := s.files.SaveBytes(ctx, data, export.TenantID, export.FileName, false)
		if saveErr != nil {
			return saveErr
		}
		if err = s.db.WithContext(ctx).Model(&export).Updates(map[string]any{"status": "ready", "error": "", "file_path": stored, "updated_at": time.Now()}).Error; err != nil {
			_ = s.files.DeleteFile(ctx, stored)
		}
		return err
	}
	page, err := s.Get(ctx, export.KnowledgeBaseID, export.PageID)
	if err != nil {
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}
	if page.Version != export.Version {
		return fmt.Errorf("%w: draft changed; request a new export", asynq.SkipRetry)
	}
	sources, err := s.sources(ctx, export.KnowledgeBaseID, researchSources(page))
	if err != nil {
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}
	markdown := researchExportMarkdown(page, sources, export.Format)
	var data []byte
	if export.Format == "md" {
		// Markdown is the unchanged portable source; no converter required.
		data = []byte(markdown)
	} else {
		converter, ok := s.reader.(researchDocumentExporter)
		if !ok {
			return errors.New("configured document reader does not support research conversion; use the bundled gRPC document service")
		}
		images, loadErr := s.researchImages(ctx, markdown, sources)
		if loadErr != nil {
			return loadErr
		}
		data, err = converter.ExportResearch(ctx, page.Title, markdown, export.Format, images)
		if err != nil {
			return err
		}
	}
	// Permission can change during conversion. Never make stale-access output
	// downloadable; the download endpoint repeats this check as well.
	if _, err = s.Get(ctx, export.KnowledgeBaseID, export.PageID); err != nil {
		return err
	}
	stored, err := s.files.SaveBytes(ctx, data, export.TenantID, export.FileName, false)
	if err != nil {
		return err
	}
	if err = s.db.WithContext(ctx).Model(&export).Updates(map[string]any{
		"status": "ready", "error": "", "file_path": stored, "updated_at": time.Now()}).Error; err != nil {
		_ = s.files.DeleteFile(ctx, stored)
		return err
	}
	return nil
}

// Word/PDF use a readable provenance appendix. Markdown keeps the complete
// machine-readable evidence map so the short identifiers never become the
// sole record of a source version. The reviewed body is left unchanged.
func researchExportMarkdown(page *types.WikiPage, sources []*types.Knowledge, format string) string {
	var out strings.Builder
	out.WriteString(page.Content)
	out.WriteString("\n\n---\n\n## 来源与证据版本\n")
	var metadata map[string]json.RawMessage
	_ = json.Unmarshal(page.PageMetadata, &metadata)
	var versions map[string]struct {
		CaptureVersionID string `json:"capture_version_id"`
		ContentHash      string `json:"content_hash"`
		FileHash         string `json:"file_hash"`
		ObservedUpdated  string `json:"observed_updated_at"`
	}
	_ = json.Unmarshal(metadata["source_versions"], &versions)
	for index, source := range sources {
		title := researchProvenanceText(source.Title)
		if title == "" {
			title = "未命名资料"
		}
		if format == "md" {
			fmt.Fprintf(&out, "\n- %s · ID `%s` · 来源 %s\n", title, source.ID, researchProvenanceText(source.Source))
			continue
		}
		version := versions[source.ID]
		fmt.Fprintf(&out, "\n### %d. %s\n", index+1, title)
		if source.Source != "" {
			fmt.Fprintf(&out, "\n来源：%s\n", researchProvenanceText(source.Source))
		}
		fmt.Fprintf(&out, "\n资料编号：%s", researchShortID(source.ID))
		if version.CaptureVersionID != "" {
			fmt.Fprintf(&out, " · 证据版本：%s", researchShortID(version.CaptureVersionID))
		} else {
			out.WriteString(" · 尚无不可变归档版本")
		}
		hash := version.ContentHash
		if hash == "" {
			hash = version.FileHash
		}
		if hash != "" {
			fmt.Fprintf(&out, " · 内容指纹：%s", researchShortID(hash))
		}
		out.WriteString("\n")
		if observed, err := time.Parse(time.RFC3339Nano, version.ObservedUpdated); err == nil {
			fmt.Fprintf(&out, "\n写作时资料记录时间：%s\n", observed.UTC().Format("2006-01-02 15:04:05 UTC"))
		}
	}
	if len(sources) == 0 {
		out.WriteString("\n本研究稿未关联来源资料。\n")
	}
	if format == "md" && len(metadata["source_versions"]) > 0 {
		if versionText, err := json.MarshalIndent(metadata["source_versions"], "", "  "); err == nil {
			out.WriteString("\n### 写作时使用的完整证据版本\n\n```json\n" + string(versionText) + "\n```\n")
		}
	} else if len(sources) > 0 {
		out.WriteString("\n以上为写作时使用的证据标识。完整标识与版本记录随 Markdown 和项目资产包保留。\n")
	}
	fmt.Fprintf(&out, "\n研究稿版本：%d\n", page.Version)
	return out.String()
}

func researchShortID(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func researchProvenanceText(value string) string {
	// Source labels are metadata, not author-controlled document markup.
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "![", "[").Replace(value))
}

func (s *ResearchDraftService) researchImages(ctx context.Context, markdown string, sources []*types.Knowledge) (map[string][]byte, error) {
	wanted := map[string]bool{}
	for _, match := range researchImagePattern.FindAllStringSubmatch(markdown, -1) {
		wanted[match[1]] = true
	}
	result := map[string][]byte{}
	if len(wanted) == 0 {
		return result, nil
	}
	// Only persisted image records belonging to authorized source documents are
	// eligible. User-supplied URLs are never fetched by Go or LibreOffice.
	total := 0
	for _, source := range sources {
		chunks, err := s.chunks.ListAllChunksByKnowledgeID(ctx, source.TenantID, source.ID)
		if err != nil {
			return nil, err
		}
		for _, chunk := range chunks {
			if chunk.ImageInfo == "" {
				continue
			}
			var images []types.ImageInfo
			if json.Unmarshal([]byte(chunk.ImageInfo), &images) != nil {
				var image types.ImageInfo
				if json.Unmarshal([]byte(chunk.ImageInfo), &image) == nil {
					images = []types.ImageInfo{image}
				}
			}
			for _, image := range images {
				if !wanted[image.URL] && !wanted[image.OriginalURL] {
					continue
				}
				if err := s.validateResearchImageOwnership(ctx, source, image.URL); err != nil {
					return nil, err
				}
				if !strings.HasPrefix(image.URL, "resource://") && !strings.HasPrefix(image.URL, "local://") &&
					!strings.HasPrefix(image.URL, "minio://") && !strings.HasPrefix(image.URL, "s3://") {
					continue // Fail closed for unregistered external images.
				}
				file, err := s.files.GetFile(ctx, image.URL)
				if err != nil {
					return nil, err
				}
				bytes, readErr := io.ReadAll(io.LimitReader(file, 20*1024*1024+1))
				file.Close()
				if readErr != nil {
					return nil, readErr
				}
				total += len(bytes)
				if total > 20*1024*1024 {
					return nil, researchInput("export images exceed 20 MiB")
				}
				if wanted[image.URL] {
					result[image.URL] = bytes
				}
				if wanted[image.OriginalURL] {
					result[image.OriginalURL] = bytes
				}
			}
		}
	}
	for ref := range wanted {
		if result[ref] == nil {
			return nil, researchInput("image is not an accessible stored source attachment")
		}
	}
	return result, nil
}

func (s *ResearchDraftService) validateResearchImageOwnership(ctx context.Context, source *types.Knowledge, reference string) error {
	lookup, ok := s.catalog.(interfaces.KBResourceLookup)
	if !ok {
		return researchInput("stored-image ownership verification is unavailable")
	}
	owned, err := lookup.IsReferencedByKnowledgeBase(ctx, source.TenantID, source.KnowledgeBaseID, reference)
	if err != nil {
		return err
	}
	if !owned {
		return access.ErrForbidden
	}
	return nil
}
