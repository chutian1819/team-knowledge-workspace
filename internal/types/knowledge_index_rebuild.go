package types

import "time"

const TypeKnowledgeIndexRebuild = "knowledge:index_rebuild"

// KnowledgeIndexRebuildPayload refers to existing immutable chunk IDs. It never
// requests document parsing or carries model credentials.
type KnowledgeIndexRebuildPayload struct {
	TracingContext
	TaskID          string        `json:"task_id"`
	TenantID        uint64        `json:"tenant_id"`
	KnowledgeBaseID string        `json:"knowledge_base_id"`
	ConfigHash      string        `json:"config_hash"`
	Caller          Caller        `json:"caller"`
	Initiator       TaskInitiator `json:"initiator,omitempty"`
}

type KnowledgeIndexRebuildProgress struct {
	TaskID           string    `json:"task_id"`
	KnowledgeBaseID  string    `json:"knowledge_base_id"`
	Status           string    `json:"status"`         // queued, running, retrying, completed, failed
	DocumentCount    int       `json:"document_count"` // existing KB editor response contract
	DocumentsTotal   int       `json:"documents_total"`
	DocumentsIndexed int       `json:"documents_indexed"`
	DocumentsSkipped int       `json:"documents_skipped"`
	ChunksTotal      int       `json:"chunks_total"`
	ChunksIndexed    int       `json:"chunks_indexed"`
	VectorEnabled    bool      `json:"vector_enabled"`
	KeywordEnabled   bool      `json:"keyword_enabled"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
