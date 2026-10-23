-- SQLite mirror of Postgres migration 000124.
CREATE TABLE research_writing_rules (
 id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
 knowledge_base_id VARCHAR(36) NOT NULL, name VARCHAR(320) NOT NULL,
 instruction TEXT NOT NULL, template_id VARCHAR(64) NOT NULL DEFAULT '',
 status VARCHAR(16) NOT NULL DEFAULT 'candidate' CHECK (status IN ('candidate', 'active', 'revoked')),
 source_page_id VARCHAR(36) NOT NULL DEFAULT '', source_proposal_id VARCHAR(36) NOT NULL DEFAULT '',
 version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0), created_by VARCHAR(512) NOT NULL,
 metadata TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX research_writing_rules_kb ON research_writing_rules(knowledge_base_id, created_at DESC);
CREATE TABLE research_writing_feedbacks (
 tenant_id BIGINT NOT NULL, caller_tenant_id BIGINT NOT NULL,
 rule_id VARCHAR(36) NOT NULL, proposal_id VARCHAR(36) NOT NULL,
 rating VARCHAR(16) NOT NULL CHECK (rating IN ('useful', 'useless', 'misapplied')),
 reason TEXT NOT NULL DEFAULT '', created_by VARCHAR(512) NOT NULL,
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(caller_tenant_id, rule_id, proposal_id, created_by)
);
CREATE INDEX research_writing_feedback_proposal ON research_writing_feedbacks(proposal_id, updated_at DESC);
ALTER TABLE research_proposals ADD COLUMN writing_rules TEXT NOT NULL DEFAULT '[]';
