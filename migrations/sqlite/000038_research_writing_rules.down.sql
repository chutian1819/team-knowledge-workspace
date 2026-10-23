-- SQLite mirror of Postgres migration 000124.
ALTER TABLE research_proposals DROP COLUMN writing_rules;
DROP TABLE research_writing_feedbacks;
DROP TABLE research_writing_rules;
