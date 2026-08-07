-- Filter by type / status.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_artifact_type
    ON artifact (type, status);
