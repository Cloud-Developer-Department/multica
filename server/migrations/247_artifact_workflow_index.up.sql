-- Workflow artifact list.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_artifact_workflow
    ON artifact (workflow_id, created_at);
