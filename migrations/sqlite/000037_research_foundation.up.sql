-- SQLite foundation for research capture/drafts (Postgres migrations 000120–000123).
CREATE TABLE IF NOT EXISTS research_inboxes (
 tenant_id BIGINT NOT NULL, user_id VARCHAR(36) NOT NULL,
 knowledge_base_id VARCHAR(36) NOT NULL, PRIMARY KEY(tenant_id, user_id)
);
CREATE TABLE IF NOT EXISTS research_capture_jobs (
 id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, user_id VARCHAR(36) NOT NULL,
 knowledge_base_id VARCHAR(36) NOT NULL, kind TEXT NOT NULL,
 idempotency_key VARCHAR(128) NOT NULL, request_hash VARCHAR(64) NOT NULL,
 status TEXT NOT NULL, total INTEGER NOT NULL DEFAULT 0,
 completed INTEGER NOT NULL DEFAULT 0, failed INTEGER NOT NULL DEFAULT 0,
 items TEXT NOT NULL DEFAULT '[]', target_kb_id VARCHAR(36) NOT NULL DEFAULT '',
 organization_id VARCHAR(36) NOT NULL DEFAULT '', selected_tenant_ids TEXT NOT NULL DEFAULT '[]',
 initiator TEXT NOT NULL DEFAULT '{}', error TEXT NOT NULL DEFAULT '', lease_until DATETIME,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(tenant_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS research_capture_kb ON research_capture_jobs(tenant_id, knowledge_base_id, created_at DESC);
CREATE TABLE IF NOT EXISTS research_source_versions (
 id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, knowledge_id VARCHAR(36) NOT NULL,
 content_hash VARCHAR(64) NOT NULL, source_url TEXT NOT NULL DEFAULT '',
 snapshot_path TEXT NOT NULL DEFAULT '', markdown_path TEXT NOT NULL,
 original_path TEXT NOT NULL DEFAULT '', file_name TEXT NOT NULL DEFAULT '',
 file_type TEXT NOT NULL DEFAULT '', file_hash TEXT NOT NULL DEFAULT '',
 image_paths TEXT NOT NULL DEFAULT '[]', captured_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(tenant_id, knowledge_id, content_hash)
);
CREATE INDEX IF NOT EXISTS research_source_knowledge ON research_source_versions(tenant_id, knowledge_id, captured_at DESC);

CREATE TABLE IF NOT EXISTS research_proposals (
    id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL, page_id VARCHAR(36) NOT NULL,
    base_version INTEGER NOT NULL, base_content TEXT NOT NULL,
    content TEXT NOT NULL, instruction TEXT NOT NULL DEFAULT '',
    model_id VARCHAR(64) NOT NULL DEFAULT '', source_refs TEXT NOT NULL DEFAULT '[]',
    source_versions TEXT NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'pending', created_by VARCHAR(64) NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_research_proposals_page ON research_proposals(knowledge_base_id, page_id, created_at);
CREATE TABLE IF NOT EXISTS research_exports (
    id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL, page_id VARCHAR(36) NOT NULL,
    version INTEGER NOT NULL, format VARCHAR(8) NOT NULL, status VARCHAR(16) NOT NULL,
    error TEXT NOT NULL DEFAULT '', file_path TEXT NOT NULL DEFAULT '',
    file_name TEXT NOT NULL, created_by VARCHAR(64) NOT NULL,
    source_ids TEXT NOT NULL DEFAULT '[]', page_ids TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_research_exports_kb ON research_exports(knowledge_base_id, created_at);

CREATE TRIGGER research_private_member_insert BEFORE INSERT ON tenant_members
WHEN NEW.status='active' AND NEW.deleted_at IS NULL AND EXISTS
 (SELECT 1 FROM research_inboxes WHERE tenant_id=NEW.tenant_id AND user_id<>NEW.user_id)
BEGIN SELECT RAISE(ABORT, 'research_private_workspace'); END;
CREATE TRIGGER research_private_member_update BEFORE UPDATE ON tenant_members
WHEN NEW.status='active' AND NEW.deleted_at IS NULL AND EXISTS
 (SELECT 1 FROM research_inboxes WHERE tenant_id=NEW.tenant_id AND user_id<>NEW.user_id)
BEGIN SELECT RAISE(ABORT, 'research_private_workspace'); END;
CREATE TRIGGER research_private_inbox_insert BEFORE INSERT ON research_inboxes
WHEN EXISTS (SELECT 1 FROM tenant_members WHERE tenant_id=NEW.tenant_id AND user_id<>NEW.user_id AND status='active' AND deleted_at IS NULL)
BEGIN SELECT RAISE(ABORT, 'research_private_workspace'); END;
CREATE TRIGGER research_private_inbox_update BEFORE UPDATE ON research_inboxes
WHEN EXISTS (SELECT 1 FROM tenant_members WHERE tenant_id=NEW.tenant_id AND user_id<>NEW.user_id AND status='active' AND deleted_at IS NULL)
BEGIN SELECT RAISE(ABORT, 'research_private_workspace'); END;
