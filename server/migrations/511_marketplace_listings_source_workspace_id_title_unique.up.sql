-- F-523 (CLO-617): listing title uniqueness within a workspace
-- (PRD §2.4 / Q5). Extracted from the CREATE TABLE in migration 287 so the
-- unique index is built with CONCURRENTLY in its own single-statement file,
-- per the repository's index rule (an inline UNIQUE in CREATE TABLE would
-- create the backing index without CONCURRENTLY).
-- Single statement + CONCURRENTLY per the repository's index rule.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS marketplace_listings_source_workspace_id_title_key
    ON marketplace_listings (source_workspace_id, title);
