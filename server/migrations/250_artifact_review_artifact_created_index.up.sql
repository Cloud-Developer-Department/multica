-- Review history per artifact (FR4.6).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_artifact_review_artifact_created
    ON artifact_review (artifact_id, created_at);
