DROP TRIGGER IF EXISTS research_private_member_guard ON tenant_members;
DROP TRIGGER IF EXISTS research_private_inbox_guard ON research_inboxes;
DROP FUNCTION IF EXISTS research_guard_private_workspace();
DROP TABLE IF EXISTS research_capture_jobs;
DROP TABLE IF EXISTS research_inboxes;
DROP TABLE IF EXISTS research_source_versions;
