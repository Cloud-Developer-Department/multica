-- Stage progress / barrier computation and stage+status filters.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workflow_node_workflow_stage_status
    ON workflow_node (workflow_id, stage, status);
