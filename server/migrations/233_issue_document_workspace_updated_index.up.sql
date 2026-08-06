-- Default ordering for the Issue Documents tab: most recently updated first,
-- scoped to the workspace. Single statement: CREATE INDEX CONCURRENTLY cannot
-- run inside a transaction or a multi-command migration file (repo convention).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_document_workspace_updated
    ON issue_document (workspace_id, updated_at DESC);
