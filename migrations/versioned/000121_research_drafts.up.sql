CREATE TABLE IF NOT EXISTS research_proposals (
    id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL, page_id VARCHAR(36) NOT NULL,
    base_version INTEGER NOT NULL, base_content TEXT NOT NULL,
    content TEXT NOT NULL, instruction TEXT NOT NULL DEFAULT '',
    model_id VARCHAR(64) NOT NULL DEFAULT '', source_refs JSONB NOT NULL DEFAULT '[]',
    source_versions JSONB NOT NULL DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'pending', created_by VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_research_proposals_page ON research_proposals(knowledge_base_id, page_id, created_at);
CREATE TABLE IF NOT EXISTS research_exports (
    id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL, page_id VARCHAR(36) NOT NULL,
    version INTEGER NOT NULL, format VARCHAR(8) NOT NULL, status VARCHAR(16) NOT NULL,
    error TEXT NOT NULL DEFAULT '', file_path TEXT NOT NULL DEFAULT '',
    file_name TEXT NOT NULL, created_by VARCHAR(64) NOT NULL,
    source_ids JSONB NOT NULL DEFAULT '[]', page_ids JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_research_exports_kb ON research_exports(knowledge_base_id, created_at);
