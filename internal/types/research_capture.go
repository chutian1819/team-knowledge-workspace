package types

import (
	"encoding/json"
	"time"
)

// ResearchCaptureJob contains orchestration state only. Documents remain native knowledge entries.
type ResearchCaptureJob struct {
	ID                string                `json:"id" gorm:"primaryKey;type:varchar(36)"`
	TenantID          uint64                `json:"-" gorm:"uniqueIndex:research_capture_request"`
	UserID            string                `json:"-"`
	KnowledgeBaseID   string                `json:"knowledge_base_id"`
	Kind              string                `json:"kind"`
	IdempotencyKey    string                `json:"-" gorm:"uniqueIndex:research_capture_request"`
	RequestHash       string                `json:"-"`
	Status            string                `json:"status"`
	Total             int                   `json:"total"`
	Completed         int                   `json:"completed"`
	Failed            int                   `json:"failed"`
	Items             []ResearchCaptureItem `json:"items" gorm:"serializer:json;type:jsonb"`
	TargetKBID        string                `json:"target_kb_id,omitempty"`
	OrganizationID    string                `json:"organization_id,omitempty"`
	SelectedTenantIDs []uint64              `json:"selected_tenant_ids,omitempty" gorm:"serializer:json;type:jsonb"`
	Initiator         TaskInitiator         `json:"-" gorm:"serializer:json;type:jsonb"`
	Error             string                `json:"error,omitempty"`
	LeaseUntil        *time.Time            `json:"-"`
	ClaimedLease      *time.Time            `json:"-" gorm:"-"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

func (ResearchCaptureJob) TableName() string { return "research_capture_jobs" }

type ResearchCaptureItem struct {
	URL          string               `json:"url,omitempty"`
	Title        string               `json:"title,omitempty"`
	Folder       string               `json:"folder,omitempty"`
	AddedAt      int64                `json:"added_at,omitempty"`
	KnowledgeID  string               `json:"knowledge_id,omitempty"`
	Status       string               `json:"status"`
	Error        string               `json:"error,omitempty"`
	Tags         []ResearchCaptureTag `json:"tags,omitempty"`
	TagsCaptured bool                 `json:"tags_captured,omitempty"`
	MoveTaskID   string               `json:"move_task_id,omitempty"`
}
type ResearchCaptureTag struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}
type ResearchCaptureRequest struct {
	Items             []ResearchCaptureItem `json:"items"`
	IdempotencyKey    string                `json:"idempotency_key" binding:"required"`
	TargetKBID        string                `json:"target_kb_id"`
	OrganizationID    string                `json:"organization_id"`
	SelectedTenantIDs []uint64              `json:"selected_tenant_ids"`
	KnowledgeIDs      []string              `json:"knowledge_ids"`
}
type ResearchInbox struct {
	TenantID        uint64 `gorm:"primaryKey"`
	UserID          string `gorm:"primaryKey"`
	KnowledgeBaseID string
}

func (ResearchInbox) TableName() string { return "research_inboxes" }

// ResearchSourceVersion is immutable evidence, not another editable document.
// KnowledgeID is unchanged across refreshes and moves; permissions follow its current KB.
type ResearchSourceVersion struct {
	ID           string    `json:"id" gorm:"primaryKey;type:varchar(36)"`
	TenantID     uint64    `json:"-" gorm:"uniqueIndex:research_source_content"`
	KnowledgeID  string    `json:"knowledge_id" gorm:"uniqueIndex:research_source_content"`
	ContentHash  string    `json:"content_hash" gorm:"uniqueIndex:research_source_content"`
	SourceURL    string    `json:"source_url"`
	SnapshotPath string    `json:"snapshot_path,omitempty"`
	MarkdownPath string    `json:"markdown_path"`
	OriginalPath string    `json:"-"`
	FileName     string    `json:"file_name,omitempty"`
	FileType     string    `json:"file_type,omitempty"`
	FileHash     string    `json:"file_hash,omitempty"`
	ImagePaths   []string  `json:"image_paths" gorm:"serializer:json;type:jsonb"`
	CapturedAt   time.Time `json:"captured_at"`
}

func (ResearchSourceVersion) TableName() string { return "research_source_versions" }

// Original locators must not be exposed by the read-only versions API: native
// KB file previews accept known bound locators with read access. Originals use
// the dedicated version asset endpoint, which rechecks the stricter write
// grant before opening the stored path.
func (v ResearchSourceVersion) MarshalJSON() ([]byte, error) {
	type publicVersion ResearchSourceVersion
	return json.Marshal(struct {
		publicVersion
		HasOriginal bool `json:"has_original"`
	}{publicVersion: publicVersion(v), HasOriginal: v.OriginalPath != ""})
}
