-- One source_issue maps to at most one workflow instance (1:1).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_workflow_source_issue
    ON workflow (source_issue_id);
