package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

const researchHTMLMetadata = "_research_source_html"

type researchSourceRepository interface {
	FindResearchSourceVersion(context.Context, uint64, string, string) (*types.ResearchSourceVersion, error)
	CreateResearchSourceVersion(context.Context, *types.ResearchSourceVersion) error
}

func sourceContentHash(markdown string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(markdown, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

// archiveResearchSource runs inside native ingestion, before indexing. Any storage
// failure makes ingestion retry instead of falsely reporting durable capture.
func (s *knowledgeService) archiveResearchSource(ctx context.Context, kb *types.KnowledgeBase, k *types.Knowledge, result *types.ReadResult, rawMarkdown, html string, images []docparser.StoredImage) error {
	if k == nil || (k.Type != "url" && k.Type != "file" && k.Type != "file_url") || (k.Type == "url" && result == nil) {
		return nil
	}
	repo, ok := s.repo.(researchSourceRepository)
	if !ok {
		return nil
	} // older repository adapters/test doubles
	hash := sourceContentHash(rawMarkdown)
	var originalService interfaces.FileService
	if k.Type == "file" || k.Type == "file_url" {
		if k.FilePath == "" {
			return fmt.Errorf("cannot archive file evidence without its original path")
		}
		originalService = s.resolveFileServiceForPath(ctx, kb, k.FilePath)
		original, err := originalService.GetFile(ctx, k.FilePath)
		if err != nil {
			return fmt.Errorf("read original evidence: %w", err)
		}
		digest := sha256.New()
		_, hashErr := io.Copy(digest, original)
		_ = original.Close()
		if hashErr != nil {
			return fmt.Errorf("hash original evidence: %w", hashErr)
		}
		hash = hex.EncodeToString(digest.Sum(nil))
	}
	existing, err := repo.FindResearchSourceVersion(ctx, k.TenantID, k.ID, hash)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	v := &types.ResearchSourceVersion{ID: uuid.NewString(), TenantID: k.TenantID, KnowledgeID: k.ID, ContentHash: hash, SourceURL: k.Source, CapturedAt: time.Now(), ImagePaths: []string{}, FileName: k.FileName, FileType: k.FileType, FileHash: k.FileHash}
	fileSvc := s.resolveFileService(ctx, kb)
	type savedObject struct {
		svc  interfaces.FileService
		path string
	}
	saved := []savedObject{}
	complete := false
	defer func() {
		if !complete {
			for _, object := range saved {
				cleanupCopiedObjects(context.WithoutCancel(ctx), object.svc, []string{object.path})
			}
		}
	}()
	save := func(data []byte, name string) (string, error) {
		path, err := fileSvc.SaveBytes(ctx, data, k.TenantID, name, false)
		if err != nil {
			return "", err
		}
		saved = append(saved, savedObject{fileSvc, path})
		if s.resourceCatalog != nil {
			if _, ok := types.ParseResourcePath(path); ok {
				if err := s.resourceCatalog.Bind(ctx, path, types.ResourceOwnerKnowledge, k.ID, types.ResourceRelationSourceFile); err != nil {
					return "", err
				}
			}
		}
		return path, nil
	}
	if originalService != nil {
		// Copy within the original backend. CopyFile streams locally or uses
		// provider-side copying; a large original never needs a RAM buffer.
		v.OriginalPath, err = originalService.CopyFile(ctx, k.FilePath, k.TenantID, k.ID)
		if err != nil {
			return fmt.Errorf("archive original file: %w", err)
		}
		if v.OriginalPath == "" || v.OriginalPath == k.FilePath {
			return fmt.Errorf("original archival did not produce an independent object")
		}
		saved = append(saved, savedObject{originalService, v.OriginalPath})
		if s.resourceCatalog != nil {
			if _, ok := types.ParseResourcePath(v.OriginalPath); ok {
				if err := s.resourceCatalog.Bind(ctx, v.OriginalPath, types.ResourceOwnerKnowledge, k.ID, types.ResourceRelationSourceFile); err != nil {
					return fmt.Errorf("bind original evidence: %w", err)
				}
			}
		}
	}
	if html != "" {
		v.SnapshotPath, err = save([]byte(html), v.ID+".html.txt")
		if err != nil {
			return fmt.Errorf("archive original webpage: %w", err)
		}
	}
	// Reparse deletes extracted images. Keep independent immutable image objects for
	// old evidence versions, bound to the same knowledge for native ACL/deletion.
	replacements := map[string]string{}
	for _, image := range images {
		if image.ServingURL == "" {
			continue
		}
		if _, seen := replacements[image.ServingURL]; seen {
			continue
		}
		rc, err := fileSvc.GetFile(ctx, image.ServingURL)
		if err != nil {
			return fmt.Errorf("read source image: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, (64<<20)+1))
		_ = rc.Close()
		if readErr != nil {
			return fmt.Errorf("read source image: %w", readErr)
		}
		if len(data) > 64<<20 {
			return fmt.Errorf("source image exceeds 64 MiB archival limit")
		}
		copied, err := save(data, uuid.NewString()+imageExtForCopy(image.ServingURL, data))
		if err != nil {
			return fmt.Errorf("archive source image: %w", err)
		}
		replacements[image.ServingURL] = copied
		v.ImagePaths = append(v.ImagePaths, copied)
	}
	if result != nil {
		archivedMarkdown := rewriteContentImageURLs(result.MarkdownContent, replacements)
		v.MarkdownPath, err = save([]byte(archivedMarkdown), v.ID+".md")
		if err != nil {
			return fmt.Errorf("archive parsed source: %w", err)
		}
	}
	if err := repo.CreateResearchSourceVersion(ctx, v); err != nil {
		if errors.Is(err, repository.ErrResearchSourceVersionExists) {
			return nil
		} // deferred cleanup releases this losing attempt's files
		return err
	}
	complete = true
	return nil
}

// archiveResearchOriginalBeforeReplace protects legacy originals that predate
// the ingestion archive hook (including failed/unparsed uploads). Run before
// source replacement or chunk cleanup; failure leaves the live source intact.
// No parsing, embedding or summary generation is performed here.
func (s *knowledgeService) archiveResearchOriginalBeforeReplace(ctx context.Context, kb *types.KnowledgeBase, knowledge *types.Knowledge) error {
	return s.archiveResearchSource(ctx, kb, knowledge, nil, "", "", nil)
}

func (s *ResearchCaptureService) SourceVersions(ctx context.Context, kbID, knowledgeID string) ([]types.ResearchSourceVersion, error) {
	k, err := s.kg.GetKnowledgeByIDOnly(ctx, knowledgeID)
	if err != nil {
		return nil, err
	}
	if k.KnowledgeBaseID != kbID || !access.HasKBGrant(ctx, kbID, k.TenantID, types.OrgRoleViewer) {
		return nil, access.ErrForbidden
	}
	var versions []types.ResearchSourceVersion
	err = s.Repo.DB.WithContext(ctx).Where("tenant_id = ? AND knowledge_id = ?", k.TenantID, k.ID).Order("captured_at DESC").Find(&versions).Error
	return versions, err
}

// ReplaceSourceFile exposes the existing native replacement workflow without
// adding a second document identity, parser, or mutable content store.
func (s *ResearchCaptureService) ReplaceSourceFile(ctx context.Context, kbID, knowledgeID string, file *multipart.FileHeader, fileName string) (*types.Knowledge, error) {
	if _, err := s.owned(ctx, kbID); err != nil {
		return nil, err
	}
	current, err := s.kg.GetKnowledgeByID(ctx, knowledgeID)
	if err != nil {
		return nil, err
	}
	if current.KnowledgeBaseID != kbID || current.TenantID != types.CallerFromContext(ctx).TenantID {
		return nil, access.ErrForbidden
	}
	updated, err := s.kg.ReplaceKnowledgeFile(ctx, knowledgeID, file, fileName, nil)
	var duplicate *types.DuplicateKnowledgeError
	if updated != nil && errors.As(err, &duplicate) {
		return updated, nil
	}
	return updated, err
}
