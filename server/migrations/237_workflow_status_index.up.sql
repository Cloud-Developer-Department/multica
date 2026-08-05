-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workflow_status
    ON workflow (status);
