package types

import "time"

// ResearchWritingEvent is an append-only journal of actual writing activity.
// Learning progress belongs to a separate row and never rewrites this evidence.
type ResearchWritingEvent struct {
	ID              string      `json:"id" gorm:"primaryKey;type:varchar(36)"`
	TenantID        uint64      `json:"-"`
	KnowledgeBaseID string      `json:"knowledge_base_id" gorm:"index"`
	PageID          string      `json:"page_id" gorm:"index"`
	Kind            string      `json:"kind"`
	Instruction     string      `json:"instruction" gorm:"type:text"`
	BaseVersion     int         `json:"base_version"`
	ResultVersion   int         `json:"result_version"`
	BaseContent     string      `json:"base_content" gorm:"type:text"`
	Content         string      `json:"content" gorm:"type:text"`
	ModelID         string      `json:"model_id"`
	ProposalID      string      `json:"proposal_id"`
	ParentEventID   string      `json:"parent_event_id"`
	PromptEventID   string      `json:"prompt_event_id"`
	SourceRefs      StringArray `json:"-" gorm:"type:json"`
	SourceVersions  JSON        `json:"-" gorm:"type:json"`
	WritingRules    JSON        `json:"-" gorm:"type:json"`
	// Legacy accepted proposals remain immutable provenance for drafts created before journaling.
	AncestorProposalIDs StringArray `json:"-" gorm:"type:json"`
	Status              string      `json:"status"`
	Error               string      `json:"error,omitempty" gorm:"type:text"`
	CreatedBy           string      `json:"created_by"`
	CreatedAt           time.Time   `json:"created_at"`
	LearningStatus      string      `json:"learning_status,omitempty" gorm:"-"`
	LearningError       string      `json:"learning_error,omitempty" gorm:"-"`
}

type ResearchWritingLearningSetting struct {
	CallerTenantID  uint64    `json:"-" gorm:"primaryKey"`
	CreatedBy       string    `json:"-" gorm:"primaryKey"`
	PageID          string    `json:"-" gorm:"primaryKey"`
	KnowledgeBaseID string    `json:"-"`
	Enabled         bool      `json:"enabled"`
	ModelID         string    `json:"model_id"`
	UpdatedAt       time.Time `json:"-"`
}

// One durable extraction per event makes task retries and manual clicks idempotent.
type ResearchWritingExtraction struct {
	EventID         string     `json:"event_id" gorm:"primaryKey;type:varchar(36)"`
	KnowledgeBaseID string     `json:"-"`
	PageID          string     `json:"-" gorm:"index"`
	CallerTenantID  uint64     `json:"-"`
	CreatedBy       string     `json:"-"`
	ModelID         string     `json:"model_id"`
	Automatic       bool       `json:"-"`
	Caller          JSON       `json:"-" gorm:"type:json"`
	Initiator       JSON       `json:"-" gorm:"type:json"`
	Status          string     `json:"status"`
	Error           string     `json:"error,omitempty" gorm:"type:text"`
	LeaseUntil      *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
