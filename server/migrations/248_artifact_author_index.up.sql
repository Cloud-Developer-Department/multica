-- Search artifacts by author (agent / member) (FR3.7).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_artifact_author
    ON artifact (author_id, created_at);
