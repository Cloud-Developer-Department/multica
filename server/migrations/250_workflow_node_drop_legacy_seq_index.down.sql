-- Rollback: recreate the legacy single-column index so a down-migration of the
-- CLO-146 fix returns to the previous (incorrect) schema shape.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workflow_node_workflow_seq
    ON workflow_node (workflow_id, seq);
