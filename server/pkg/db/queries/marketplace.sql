-- F-523 (CLO-617): marketplace listing + stats + download-dedup queries.
-- No foreign keys (repo rule): listing/stats/dedup rows are created and
-- removed together by the handler in one transaction. MVP is single-workspace:
-- every read/write is scoped to the caller's workspace via
-- source_workspace_id (PRD §2.1 / Q1).

-- name: CreateMarketplaceListing :one
INSERT INTO marketplace_listings (
    kind, title, summary, category, tags, version,
    author_id, author_display_name, source_workspace_id, template_id, template
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: CreateMarketplaceStats :exec
INSERT INTO marketplace_stats (listing_id) VALUES ($1);

-- name: GetMarketplaceListing :one
SELECT * FROM marketplace_listings WHERE id = $1;

-- name: GetMarketplaceListingByWorkspaceAndTitle :one
SELECT * FROM marketplace_listings
WHERE source_workspace_id = $1 AND title = $2
LIMIT 1;

-- name: ListMarketplaceListings :many
-- Published listings in one workspace. kind/category are equality filters;
-- q is matched through the GIN FTS index (PRD §4.2: to_tsvector('simple')
-- over title/summary/tags/author) so the marketplace_listings_fts_idx index
-- is actually used. Sorting is applied per the requested sort key
-- (name / downloads / latest).
SELECT
    l.id,
    l.kind,
    l.title,
    l.summary,
    l.category,
    l.tags,
    l.version,
    l.author_id,
    l.author_display_name,
    l.source_workspace_id,
    l.template_id,
    l.status,
    l.created_at,
    l.updated_at,
    COALESCE(s.downloads, 0) AS downloads,
    COALESCE(s.installs, 0)  AS installs
FROM marketplace_listings l
LEFT JOIN marketplace_stats s ON s.listing_id = l.id
WHERE l.source_workspace_id = sqlc.arg('workspace_id')
  AND l.status = 'published'
  AND (sqlc.arg('kind')::text = '' OR l.kind = sqlc.arg('kind'))
  AND (sqlc.arg('category')::text = '' OR l.category = sqlc.arg('category'))
  AND (sqlc.arg('search')::text = '' OR
       marketplace_listings_tsvector(l.title, l.summary, l.tags, l.author_display_name) @@
       plainto_tsquery('simple', sqlc.arg('search')))
ORDER BY
    CASE WHEN sqlc.arg('sort') = 'name' THEN l.title END ASC,
    CASE WHEN sqlc.arg('sort') = 'downloads' THEN COALESCE(s.downloads, 0) END DESC,
    l.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountMarketplaceListings :one
SELECT count(*)::bigint
FROM marketplace_listings l
WHERE l.source_workspace_id = sqlc.arg('workspace_id')
  AND l.status = 'published'
  AND (sqlc.arg('kind')::text = '' OR l.kind = sqlc.arg('kind'))
  AND (sqlc.arg('category')::text = '' OR l.category = sqlc.arg('category'))
  AND (sqlc.arg('search')::text = '' OR
       marketplace_listings_tsvector(l.title, l.summary, l.tags, l.author_display_name) @@
       plainto_tsquery('simple', sqlc.arg('search')));

-- name: ListMarketplaceListingsIncludingArchived :many
-- Archive-management view: archived listings are only visible to the
-- publisher / workspace owner, so the handler filters by author after
-- fetching (listing volumes are small in MVP).
SELECT
    l.id,
    l.kind,
    l.title,
    l.summary,
    l.category,
    l.tags,
    l.version,
    l.author_id,
    l.author_display_name,
    l.source_workspace_id,
    l.template_id,
    l.status,
    l.created_at,
    l.updated_at,
    COALESCE(s.downloads, 0) AS downloads,
    COALESCE(s.installs, 0)  AS installs
FROM marketplace_listings l
LEFT JOIN marketplace_stats s ON s.listing_id = l.id
WHERE l.source_workspace_id = $1
ORDER BY l.created_at DESC;

-- name: UpdateMarketplaceListingStatus :one
UPDATE marketplace_listings SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: IncrementMarketplaceDownloads :one
UPDATE marketplace_stats
SET downloads = downloads + 1, updated_at = now()
WHERE listing_id = $1
RETURNING downloads;

-- name: IncrementMarketplaceInstalls :one
UPDATE marketplace_stats
SET installs = installs + 1, updated_at = now()
WHERE listing_id = $1
RETURNING installs;

-- name: GetMarketplaceStats :one
SELECT * FROM marketplace_stats WHERE listing_id = $1;

-- name: UpsertMarketplaceStats :one
INSERT INTO marketplace_stats (listing_id)
VALUES ($1)
ON CONFLICT (listing_id) DO NOTHING
RETURNING *;

-- name: InsertMarketplaceDownloadDedup :one
-- Returns the listing_id only when the row was actually inserted. A
-- (listing, workspace, member) row that already exists yields no row (not an
-- error) — this is what lets the download-report handler dedupe within a
-- transaction without poisoning it with a 23505 abort (the handler must never
-- run further queries after a unique-violation inside the same tx).
INSERT INTO marketplace_download_dedup (listing_id, workspace_id, member_id)
VALUES ($1, $2, $3)
ON CONFLICT (listing_id, workspace_id, member_id) DO NOTHING
RETURNING listing_id;

-- name: DeleteMarketplaceListing :exec
DELETE FROM marketplace_listings WHERE id = $1;

-- name: DeleteMarketplaceStats :exec
DELETE FROM marketplace_stats WHERE listing_id = $1;

-- name: DeleteMarketplaceDownloadDedupForListing :exec
DELETE FROM marketplace_download_dedup WHERE listing_id = $1;
