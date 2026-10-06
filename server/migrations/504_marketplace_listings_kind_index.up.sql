-- F-523 (CLO-617): index for marketplace listing kind filter.
-- Single statement + CONCURRENTLY per the repository's index rule.
CREATE INDEX CONCURRENTLY IF NOT EXISTS marketplace_listings_kind_idx
    ON marketplace_listings (kind);
