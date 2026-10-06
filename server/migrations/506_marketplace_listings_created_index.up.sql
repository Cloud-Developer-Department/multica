-- F-523 (CLO-617): marketplace listing recency sort index (newest first).
-- Single statement + CONCURRENTLY per the repository's index rule.
CREATE INDEX CONCURRENTLY IF NOT EXISTS marketplace_listings_created_idx
    ON marketplace_listings (created_at DESC);
