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
 items JSONB NOT NULL DEFAULT '[]', target_kb_id VARCHAR(36) NOT NULL DEFAULT '',
 organization_id VARCHAR(36) NOT NULL DEFAULT '', selected_tenant_ids JSONB NOT NULL DEFAULT '[]',
 initiator JSONB NOT NULL DEFAULT '{}', error TEXT NOT NULL DEFAULT '', lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(tenant_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS research_capture_kb ON research_capture_jobs(tenant_id, knowledge_base_id, created_at DESC);
CREATE TABLE IF NOT EXISTS research_source_versions (
 id VARCHAR(36) PRIMARY KEY, tenant_id BIGINT NOT NULL, knowledge_id VARCHAR(36) NOT NULL,
 content_hash VARCHAR(64) NOT NULL, source_url TEXT NOT NULL DEFAULT '',
 snapshot_path TEXT NOT NULL DEFAULT '', markdown_path TEXT NOT NULL,
 image_paths JSONB NOT NULL DEFAULT '[]', captured_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(tenant_id, knowledge_id, content_hash)
);
CREATE INDEX IF NOT EXISTS research_source_knowledge ON research_source_versions(tenant_id, knowledge_id, captured_at DESC);
-- Personal inboxes inherit native tenant ACLs. Keep these tenants single-user
-- at the central write boundary, including invitation acceptance/reactivation.
CREATE OR REPLACE FUNCTION research_guard_private_workspace() RETURNS trigger AS $$
BEGIN
 PERFORM id FROM tenants WHERE id = NEW.tenant_id FOR UPDATE;
 IF TG_TABLE_NAME = 'research_inboxes' THEN
  IF EXISTS (SELECT 1 FROM tenant_members WHERE tenant_id=NEW.tenant_id AND user_id<>NEW.user_id AND status='active' AND deleted_at IS NULL) THEN
   RAISE EXCEPTION 'research_private_workspace' USING ERRCODE='23514';
  END IF;
 ELSE
  IF NEW.status='active' AND NEW.deleted_at IS NULL AND EXISTS (SELECT 1 FROM research_inboxes WHERE tenant_id=NEW.tenant_id AND user_id<>NEW.user_id) THEN
   RAISE EXCEPTION 'research_private_workspace' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER research_private_member_guard BEFORE INSERT OR UPDATE ON tenant_members FOR EACH ROW EXECUTE FUNCTION research_guard_private_workspace();
CREATE TRIGGER research_private_inbox_guard BEFORE INSERT OR UPDATE ON research_inboxes FOR EACH ROW EXECUTE FUNCTION research_guard_private_workspace();
