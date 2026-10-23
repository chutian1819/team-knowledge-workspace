CREATE TABLE research_writing_events (
 id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, knowledge_base_id VARCHAR(36) NOT NULL,
 page_id VARCHAR(36) NOT NULL, kind VARCHAR(32) NOT NULL,
 instruction TEXT NOT NULL DEFAULT '', base_version INTEGER NOT NULL DEFAULT 0,
 result_version INTEGER NOT NULL DEFAULT 0, base_content TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '',
 model_id VARCHAR(128) NOT NULL DEFAULT '', proposal_id VARCHAR(36) NOT NULL DEFAULT '',
 parent_event_id VARCHAR(36) NOT NULL DEFAULT '', prompt_event_id VARCHAR(36) NOT NULL DEFAULT '',
 source_refs JSONB NOT NULL DEFAULT '[]', source_versions JSONB NOT NULL DEFAULT '{}',
 writing_rules JSONB NOT NULL DEFAULT '[]', ancestor_proposal_ids JSONB NOT NULL DEFAULT '[]',
 status VARCHAR(16) NOT NULL, error TEXT NOT NULL DEFAULT '', created_by VARCHAR(512) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX research_writing_events_page ON research_writing_events(page_id, created_at DESC);
CREATE TABLE research_writing_learning_settings (
 caller_tenant_id BIGINT NOT NULL, created_by VARCHAR(512) NOT NULL, page_id VARCHAR(36) NOT NULL,
 knowledge_base_id VARCHAR(36) NOT NULL, enabled BOOLEAN NOT NULL DEFAULT FALSE,
 model_id VARCHAR(128) NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(caller_tenant_id, created_by, page_id)
);
CREATE TABLE research_writing_extractions (
 event_id VARCHAR(36) PRIMARY KEY, knowledge_base_id VARCHAR(36) NOT NULL, page_id VARCHAR(36) NOT NULL,
 caller_tenant_id BIGINT NOT NULL, created_by VARCHAR(512) NOT NULL, model_id VARCHAR(128) NOT NULL DEFAULT '',
 automatic BOOLEAN NOT NULL DEFAULT FALSE, caller JSONB NOT NULL, initiator JSONB NOT NULL,
 status VARCHAR(16) NOT NULL, error TEXT NOT NULL DEFAULT '', lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX research_writing_extractions_page ON research_writing_extractions(page_id, updated_at DESC);
ALTER TABLE research_writing_rules ADD COLUMN source_event_id VARCHAR(36) NOT NULL DEFAULT '';
CREATE INDEX research_writing_rules_event ON research_writing_rules(source_event_id);
