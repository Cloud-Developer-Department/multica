-- F-523 (CLO-617): index for marketplace listing category filter.
-- Single statement + CONCURRENTLY per the repository's index rule.
CREATE INDEX CONCURRENTLY IF NOT EXISTS marketplace_listings_category_idx
    ON marketplace_listings (category);
