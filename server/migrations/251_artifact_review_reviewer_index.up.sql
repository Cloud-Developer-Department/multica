-- Reviewer-dimension statistics (Q6).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_artifact_review_reviewer
    ON artifact_review (reviewer_id, created_at);
