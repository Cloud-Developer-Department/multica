-- Review-status filter on the Issue Documents tab. Single-statement file so the
-- concurrent build does not run inside a transaction (repo convention).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_document_workspace_status
    ON issue_document (workspace_id, status);
