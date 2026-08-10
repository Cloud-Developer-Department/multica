-- Per-issue lookups (version history and issue-scoped filters). Single-statement
-- file so the concurrent build does not run inside a transaction (repo convention).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_document_issue
    ON issue_document (issue_id);
