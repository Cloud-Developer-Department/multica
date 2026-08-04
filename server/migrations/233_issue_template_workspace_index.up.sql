-- Single-statement CONCURRENTLY migration for the issue_template workspace
-- filter index (see 232_issue_template.up.sql for the table). CONCURRENTLY
-- cannot run inside a transaction, so this stays in its own file.
CREATE INDEX CONCURRENTLY idx_issue_template_workspace ON issue_template(workspace_id);
