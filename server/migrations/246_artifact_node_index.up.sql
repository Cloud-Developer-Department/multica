-- Node artifact list / version history.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_artifact_node
    ON artifact (node_id, created_at);
