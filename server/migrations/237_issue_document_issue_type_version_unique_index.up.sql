-- Version idempotency: at most one row per (issue_id, type, version), so a
-- duplicate submit for the same version fails loudly instead of silently
-- creating a second row. Single-statement file so the concurrent build does not
-- run inside a transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_document_issue_type_version
    ON issue_document (issue_id, type, version);
