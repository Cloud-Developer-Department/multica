-- F-523 (CLO-617): enforce one download per (listing, workspace, member).
-- Single statement + CONCURRENTLY per the repository's index rule.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS marketplace_download_dedup_listing_workspace_member_unique
    ON marketplace_download_dedup (listing_id, workspace_id, member_id);
