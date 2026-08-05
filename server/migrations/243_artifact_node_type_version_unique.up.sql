-- Version idempotency: one row per (node_id, type, version) (AC-AR2).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_artifact_node_type_version
    ON artifact (node_id, type, version);
