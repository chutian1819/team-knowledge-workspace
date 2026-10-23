package types

import "time"

// ResearchWritingRule belongs to the same audience as its research KB. Inferred
// rules remain candidates until a person explicitly approves them.
type ResearchWritingRule struct {
	ID               string `json:"id" gorm:"primaryKey;type:varchar(36)"`
	TenantID         uint64 `json:"-" gorm:"index"`
	KnowledgeBaseID  string `json:"knowledge_base_id" gorm:"index"`
	Name             string `json:"name"`
	Instruction      string `json:"instruction" gorm:"type:text"`
	TemplateID       string `json:"template_id"`
	Status           string `json:"status"`
	SourcePageID     string `json:"source_page_id"`
	SourceProposalID string `json:"source_proposal_id"`
	SourceEventID    string `json:"source_event_id" gorm:"index"`
	Version          int    `json:"version"`
	CreatedBy        string `json:"created_by"`
	// Metadata records extraction provenance, not another copy of sample text.
	Metadata  JSON      `json:"metadata,omitempty" gorm:"type:json"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ResearchWritingRuleSnapshot struct {
	ID          string `json:"id"`
	Version     int    `json:"version"`
	Name        string `json:"name"`
	Instruction string `json:"instruction"`
}

// Feedback is an explicit assessment of one rule used by a real proposal.
// It never changes a rule's status or trains a model automatically.
type ResearchWritingFeedback struct {
	TenantID       uint64    `json:"-"`
	CallerTenantID uint64    `json:"-" gorm:"primaryKey"`
	RuleID         string    `json:"rule_id" gorm:"primaryKey"`
	ProposalID     string    `json:"proposal_id" gorm:"primaryKey"`
	Rating         string    `json:"rating"`
	Reason         string    `json:"reason" gorm:"type:text"`
	CreatedBy      string    `json:"created_by" gorm:"primaryKey"`
	IsMine         bool      `json:"is_mine" gorm:"-"`
	UpdatedAt      time.Time `json:"updated_at"`
}
