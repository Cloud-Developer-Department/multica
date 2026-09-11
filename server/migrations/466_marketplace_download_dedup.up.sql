-- F-523 (CLO-617): download-count dedup ledger. PRD §4.6 / R2: the same
-- workspace member may only count one download per listing (anti-inflation).
-- A row is inserted on the first reported download/import for a
-- (listing, workspace, member); the unique index below (migration 295)
-- makes the retry a no-op instead of an error.
--
-- No FK (repo rule: relationships enforced in the application layer); the
-- handler deletes these rows when a listing is physically cleaned up.
CREATE TABLE marketplace_download_dedup (
    listing_id   uuid NOT NULL,
    workspace_id uuid NOT NULL,
    member_id    uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
