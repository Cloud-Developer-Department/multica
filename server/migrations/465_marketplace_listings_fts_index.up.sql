-- F-523 (CLO-617): full-text search over title/summary/tags/author for the
-- marketplace keyword search. The vector is built by the IMMUTABLE helper
-- marketplace_listings_tsvector (migration 292) so this stays a valid
-- single-statement CONCURRENTLY index build.
-- Single statement + CONCURRENTLY per the repository's index rule.
CREATE INDEX CONCURRENTLY IF NOT EXISTS marketplace_listings_fts_idx
    ON marketplace_listings USING gin (
        marketplace_listings_tsvector(title, summary, tags, author_display_name)
    );
