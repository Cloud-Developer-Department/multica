-- Workflow-level transition audit trail.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_wtl_workflow_created
    ON workflow_transition_log (workflow_id, created_at);
