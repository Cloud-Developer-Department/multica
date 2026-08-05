-- Per-node transition audit trail.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_wtl_node_created
    ON workflow_transition_log (node_id, created_at);
