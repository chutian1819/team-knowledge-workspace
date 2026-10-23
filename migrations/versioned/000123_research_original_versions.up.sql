ALTER TABLE research_source_versions
 ADD COLUMN original_path TEXT NOT NULL DEFAULT '',
 ADD COLUMN file_name TEXT NOT NULL DEFAULT '',
 ADD COLUMN file_type TEXT NOT NULL DEFAULT '',
 ADD COLUMN file_hash TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN research_source_versions.content_hash IS
 'SHA256 of the original bytes for file evidence; SHA256 of extracted Markdown for URL evidence.';
COMMENT ON COLUMN research_source_versions.file_hash IS
 'Native upload hash retained for provenance (currently MD5); content_hash uses SHA256.';
