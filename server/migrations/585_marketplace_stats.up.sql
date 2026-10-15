-- F-523 (CLO-617): marketplace statistics. MVP tracks downloads and installs
-- counts only (rating/favourites are deferred). Split from the listing row so
-- high-frequency count writes never contend on the listing hot row.
--
-- No FK on purpose (repo rule: relationships live in the application layer);
-- listing rows and their stats rows are created/destroyed together in one
-- handler transaction.
CREATE TABLE marketplace_stats (
    listing_id uuid PRIMARY KEY,
    downloads  bigint NOT NULL DEFAULT 0,
    installs   bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);
