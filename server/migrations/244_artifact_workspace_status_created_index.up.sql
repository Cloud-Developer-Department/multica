-- Review queue (status=submitted) ordering (AC-REV1) and stats range scans.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_artifact_workspace_status_created
    ON artifact (workspace_id, status, created_at DESC);
