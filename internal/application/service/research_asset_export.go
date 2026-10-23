package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

const researchArchiveLimit = 200 * 1024 * 1024

// SourceVersionAsset never accepts a storage path from the client. Permissions
// follow the source's current KB, including after moving or revoking a share.
// Downloading an original uses the stricter native original-file boundary.
func (s *ResearchDraftService) SourceVersionAsset(ctx context.Context, kbID, knowledgeID, versionID, kind string) (io.ReadCloser, string, error) {
	if kind != "original" && kind != "snapshot" && kind != "markdown" {
		return nil, "", researchInput("asset kind must be original, snapshot or markdown")
	}
	caller := types.CallerFromContext(ctx).Normalize()
	if kind == "original" && !caller.Role.HasPermission(types.TenantRoleContributor) {
		return nil, "", access.ErrForbidden
	}
	kb, err := s.kb.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return nil, "", access.ErrNotFound
	}
	required := types.OrgRoleViewer
	if kind == "original" {
		required = types.OrgRoleEditor
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb, required, s.shares, s.agents)
	if err != nil {
		return nil, "", err
	}
	ctx = grant.Context(ctx)
	item, err := s.knowledge.GetKnowledgeByIDOnly(ctx, knowledgeID)
	if err != nil || item == nil || item.KnowledgeBaseID != kbID || item.TenantID != kb.TenantID {
		return nil, "", access.ErrNotFound
	}
	var version types.ResearchSourceVersion
	if err := s.db.WithContext(ctx).Where("id = ? AND knowledge_id = ? AND tenant_id = ?", versionID, knowledgeID, kb.TenantID).
		First(&version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", access.ErrNotFound
		}
		return nil, "", err
	}
	stored, name := version.MarkdownPath, "source-"+version.ID+".md"
	switch kind {
	case "snapshot":
		stored, name = version.SnapshotPath, "snapshot-"+version.ID+".html.txt"
	case "original":
		stored, name = version.OriginalPath, archiveFileName(version.FileName, "source-"+version.ID)
	}
	if stored == "" {
		return nil, "", access.ErrNotFound
	}
	reader, err := s.files.GetFile(ctx, stored)
	return reader, name, err
}

// Asset export includes original files, so it follows the native original-file
// download boundary (Contributor + KB editor), not research document viewing.
func (s *ResearchDraftService) authorizeAssets(ctx context.Context, kbID string) (context.Context, *types.KnowledgeBase, error) {
	caller := types.CallerFromContext(ctx).Normalize()
	if !caller.Role.HasPermission(types.TenantRoleContributor) {
		return ctx, nil, access.ErrForbidden
	}
	kb, err := s.kb.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return ctx, nil, access.ErrNotFound
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, kb, types.OrgRoleEditor, s.shares, s.agents)
	if err != nil {
		return ctx, nil, err
	}
	return grant.Context(ctx), kb, nil
}

func (s *ResearchDraftService) RequestAssetExport(ctx context.Context, kbID string) (*types.ResearchExport, error) {
	ctx, kb, err := s.authorizeAssets(ctx, kbID)
	if err != nil {
		return nil, err
	}
	var pending int64
	if err := s.db.WithContext(ctx).Model(&types.ResearchExport{}).
		Where("knowledge_base_id = ? AND status IN ? AND created_at > ?", kbID, []string{"queued", "processing"}, time.Now().Add(-15*time.Minute)).
		Count(&pending).Error; err != nil {
		return nil, err
	}
	if pending >= 3 {
		return nil, researchInput("three exports are already pending")
	}
	caller := types.CallerFromContext(ctx)
	export := &types.ResearchExport{ID: uuid.NewString(), TenantID: kb.TenantID, KnowledgeBaseID: kbID,
		Format: "zip", Status: "queued", FileName: "project-" + kbID + ".zip", CreatedBy: caller.UserID}
	if err := s.db.WithContext(ctx).Model(&types.Knowledge{}).Where("knowledge_base_id = ?", kbID).
		Order("id ASC").Limit(1001).Pluck("id", &export.SourceIDs).Error; err != nil {
		return nil, err
	}
	if len(export.SourceIDs) > 1000 {
		return nil, researchInput("asset export supports at most 1000 documents per package; split the project")
	}
	if err := s.db.WithContext(ctx).Model(&types.WikiPage{}).Where("knowledge_base_id = ? AND slug LIKE ?", kbID, "research/%").
		Pluck("id", &export.PageIDs).Error; err != nil {
		return nil, err
	}
	if export.SourceIDs == nil {
		export.SourceIDs = types.StringArray{}
	}
	if export.PageIDs == nil {
		export.PageIDs = types.StringArray{}
	}
	if err := s.db.WithContext(ctx).Create(export).Error; err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(researchExportPayload{ID: export.ID, Caller: caller})
	if s.task == nil {
		err = errors.New("task queue unavailable")
	} else {
		_, err = s.task.Enqueue(asynq.NewTask(types.TypeResearchExport, payload),
			asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(2), asynq.Timeout(10*time.Minute))
	}
	if err != nil {
		s.db.WithContext(ctx).Model(export).Updates(map[string]any{"status": "failed", "error": "export queue unavailable"})
		return nil, err
	}
	return export, nil
}

type researchArchive struct {
	buffer bytes.Buffer
	writer *zip.Writer
	used   int64
	names  map[string]bool
}

func newResearchArchive() *researchArchive {
	a := &researchArchive{names: map[string]bool{}}
	a.writer = zip.NewWriter(&a.buffer)
	return a
}

func (a *researchArchive) add(name string, content []byte) error {
	if a.names[name] {
		return nil
	}
	if strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.Contains(name, "../") {
		return errors.New("invalid archive path")
	}
	a.used += int64(len(content))
	if a.used > researchArchiveLimit {
		return researchInput("asset export exceeds 200 MiB; split the project before exporting")
	}
	w, err := a.writer.Create(name)
	if err != nil {
		return err
	}
	if _, err := w.Write(content); err != nil {
		return err
	}
	a.names[name] = true
	return nil
}

func (s *ResearchDraftService) readArchiveFile(ctx context.Context, stored string, remaining int64) ([]byte, error) {
	if stored == "" {
		return nil, nil
	}
	reader, err := s.files.GetFile(ctx, stored)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > remaining {
		return nil, researchInput("asset export exceeds 200 MiB; select a smaller project")
	}
	return data, nil
}

func archiveFileName(name, fallback string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return fallback
	}
	return name
}

func (s *ResearchDraftService) validateArchiveMembership(ctx context.Context, export *types.ResearchExport) error {
	if len(export.SourceIDs) > 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&types.Knowledge{}).Where("knowledge_base_id = ? AND id IN ?", export.KnowledgeBaseID, []string(export.SourceIDs)).
			Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(export.SourceIDs)) {
			return access.ErrForbidden
		}
	}
	for _, id := range export.PageIDs {
		if _, err := s.Get(ctx, export.KnowledgeBaseID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *ResearchDraftService) buildAssetArchive(ctx context.Context, export *types.ResearchExport) ([]byte, error) {
	kbID := export.KnowledgeBaseID
	ctx, kb, err := s.authorizeAssets(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err := s.validateArchiveMembership(ctx, export); err != nil {
		return nil, err
	}
	var items []*types.Knowledge
	if err = s.db.WithContext(ctx).Where("knowledge_base_id = ? AND id IN ?", kbID, []string(export.SourceIDs)).Order("created_at ASC, id ASC").
		Limit(1001).Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) > 1000 {
		return nil, researchInput("asset export supports at most 1000 documents per package; split the project")
	}
	archive := newResearchArchive()
	defer archive.writer.Close()
	manifestItems := []map[string]any{}
	for _, item := range items {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		row := map[string]any{"id": item.ID, "title": item.Title, "source_url": item.Source,
			"channel": item.Channel, "parse_status": item.ParseStatus, "file_hash": item.FileHash,
			"created_at": item.CreatedAt, "updated_at": item.UpdatedAt}
		if item.FilePath != "" {
			name := "originals/" + item.ID + "/" + archiveFileName(item.FileName, "source")
			data, err := s.readArchiveFile(ctx, item.FilePath, researchArchiveLimit-archive.used)
			if err != nil {
				return nil, fmt.Errorf("read original %s: %w", item.ID, err)
			}
			if err = archive.add(name, data); err != nil {
				return nil, err
			}
			row["original"] = name
		}
		chunks, err := s.chunks.ListAllChunksByKnowledgeID(ctx, item.TenantID, item.ID)
		if err != nil {
			return nil, err
		}
		sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkIndex < chunks[j].ChunkIndex })
		var markdown strings.Builder
		chunkManifest := []map[string]any{}
		images := map[string]string{}
		for _, chunk := range chunks {
			chunkManifest = append(chunkManifest, map[string]any{"id": chunk.ID, "chunk_index": chunk.ChunkIndex,
				"content_revision": chunk.ContentRevision, "source_locators": chunk.SourceLocators, "type": chunk.ChunkType})
			if chunk.ChunkType == types.ChunkTypeText {
				markdown.WriteString(chunk.Content + "\n\n")
			}
			if chunk.ImageInfo == "" {
				continue
			}
			var refs []types.ImageInfo
			if json.Unmarshal([]byte(chunk.ImageInfo), &refs) != nil {
				var single types.ImageInfo
				if json.Unmarshal([]byte(chunk.ImageInfo), &single) == nil {
					refs = []types.ImageInfo{single}
				}
			}
			for _, image := range refs {
				if image.URL == "" || images[image.URL] != "" {
					continue
				}
				if err := s.validateResearchImageOwnership(ctx, item, image.URL); err != nil {
					return nil, err
				}
				// The path comes from an authorized persisted source record,
				// never from an export request or arbitrary Markdown link.
				data, err := s.readArchiveFile(ctx, image.URL, researchArchiveLimit-archive.used)
				if err != nil {
					return nil, fmt.Errorf("read image for %s: %w", item.ID, err)
				}
				sum := sha256.Sum256(data)
				name := "images/" + hex.EncodeToString(sum[:]) + ".bin"
				// Preserve common raster extensions for ordinary file viewers.
				switch {
				case bytes.HasPrefix(data, []byte{137, 80, 78, 71}):
					name = strings.TrimSuffix(name, ".bin") + ".png"
				case bytes.HasPrefix(data, []byte{255, 216, 255}):
					name = strings.TrimSuffix(name, ".bin") + ".jpg"
				case bytes.HasPrefix(data, []byte("GIF8")):
					name = strings.TrimSuffix(name, ".bin") + ".gif"
				case len(data) > 12 && string(data[8:12]) == "WEBP":
					name = strings.TrimSuffix(name, ".bin") + ".webp"
				}
				if err := archive.add(name, data); err != nil {
					return nil, err
				}
				images[image.URL] = name
				if image.OriginalURL != "" {
					images[image.OriginalURL] = name
				}
			}
		}
		body := markdown.String()
		// Unpublished quick notes deliberately have no retrieval chunks yet.
		// Their native manual metadata is the canonical Markdown and must be
		// included even when no model or parser has run.
		if item.IsManual() {
			manual, parseErr := item.ManualMetadata()
			if parseErr != nil {
				return nil, parseErr
			}
			if manual != nil {
				body = manual.Content
				row["manual_version"] = manual.Version
			}
		}
		for ref, target := range images {
			body = strings.ReplaceAll(body, ref, "../"+target)
		}
		mdPath := "markdown/" + item.ID + ".md"
		if err := archive.add(mdPath, []byte(body)); err != nil {
			return nil, err
		}
		row["markdown"], row["images"], row["chunks"] = mdPath, images, chunkManifest
		if s.db.Migrator().HasTable("research_source_versions") {
			var versions []types.ResearchSourceVersion
			if err := s.db.WithContext(ctx).Table("research_source_versions").Where("knowledge_id = ?", item.ID).
				Order("captured_at ASC").Find(&versions).Error; err != nil {
				return nil, err
			}
			records := []map[string]any{}
			for _, version := range versions {
				entry := map[string]any{"id": version.ID, "content_hash": version.ContentHash, "source_url": version.SourceURL,
					"captured_at": version.CapturedAt, "file_name": version.FileName, "file_type": version.FileType, "file_hash": version.FileHash}
				// A legacy replacement backup can have only an immutable
				// original; its missing parsed Markdown must not drop evidence.
				if version.OriginalPath != "" {
					name := "versions/" + version.ID + "/original/" + archiveFileName(version.FileName, "source")
					data, err := s.readArchiveFile(ctx, version.OriginalPath, researchArchiveLimit-archive.used)
					if err != nil {
						return nil, fmt.Errorf("read source version %s: %w", version.ID, err)
					}
					if err := archive.add(name, data); err != nil {
						return nil, err
					}
					entry["original"] = name
				}
				versionImages := map[string]string{}
				for index, stored := range version.ImagePaths {
					if err := s.validateResearchImageOwnership(ctx, item, stored); err != nil {
						return nil, err
					}
					name := fmt.Sprintf("versions/%s/images/%d.bin", version.ID, index)
					data, err := s.readArchiveFile(ctx, stored, researchArchiveLimit-archive.used)
					if err != nil {
						return nil, err
					}
					if err := archive.add(name, data); err != nil {
						return nil, err
					}
					versionImages[stored] = name
				}
				entry["images"] = versionImages
				for label, stored := range map[string]string{"snapshot.html.txt": version.SnapshotPath, "source.md": version.MarkdownPath} {
					if stored == "" {
						continue
					}
					name := "versions/" + version.ID + "/" + label
					data, err := s.readArchiveFile(ctx, stored, researchArchiveLimit-archive.used)
					if err != nil {
						return nil, err
					}
					if label == "source.md" {
						body := string(data)
						for ref, name := range versionImages {
							body = strings.ReplaceAll(body, ref, strings.TrimPrefix(name, "versions/"+version.ID+"/"))
						}
						data = []byte(body)
					}
					if err = archive.add(name, data); err != nil {
						return nil, err
					}
					entry[label] = name
				}
				records = append(records, entry)
			}
			row["versions"] = records
		}
		manifestItems = append(manifestItems, row)
	}
	// Formal drafts are native pages in dedicated research KBs. Export them
	// only after audience validation, retaining their version provenance.
	var pages []*types.WikiPage
	if err := s.db.WithContext(ctx).Where("knowledge_base_id = ? AND id IN ?", kbID, []string(export.PageIDs)).Find(&pages).Error; err != nil {
		return nil, err
	}
	drafts := []map[string]any{}
	for _, page := range pages {
		if _, err := s.sources(ctx, kbID, researchSources(page)); err != nil {
			return nil, err
		}
		name := "research/" + page.ID + ".md"
		if err := archive.add(name, []byte(page.Content)); err != nil {
			return nil, err
		}
		drafts = append(drafts, map[string]any{"id": page.ID, "title": page.Title, "version": page.Version,
			"source_refs": page.SourceRefs, "metadata": json.RawMessage(page.PageMetadata), "markdown": name})
	}
	manifest, err := json.MarshalIndent(map[string]any{"format_version": 1, "knowledge_base_id": kbID, "name": kb.Name,
		"exported_at": time.Now().UTC(), "documents": manifestItems, "research_drafts": drafts}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := archive.add("manifest.json", manifest); err != nil {
		return nil, err
	}
	if err := archive.add("README.txt", []byte("WeKnora project asset export v1\n\nmanifest.json contains source IDs, provenance, versions, file names and chunk locators.\noriginals/: original files. markdown/: current parsed text. images/: stored images.\nversions/: immutable source captures. research/: reviewed native Wiki research drafts.\nMarkdown is readable without WeKnora. The archive never includes model credentials, embeddings or account secrets.\n")); err != nil {
		return nil, err
	}
	if err := archive.writer.Close(); err != nil {
		return nil, err
	}
	if _, _, err := s.authorizeAssets(ctx, kbID); err != nil {
		return nil, err
	}
	if err := s.validateArchiveMembership(ctx, export); err != nil {
		return nil, err
	}
	return archive.buffer.Bytes(), nil
}
