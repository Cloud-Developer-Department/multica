-- F-523 (CLO-617): marketplace listings. One row per publish action;
-- republishing the same source resource produces a new row, never an in-place
-- edit (PRD §2.2 / R3: publish is a snapshot).
--
-- Repo rules respected: no foreign keys (relationships enforced in the
-- application layer), and every standalone index — including the
-- (source_workspace_id, title) uniqueness — lives in its own single-statement
-- migration file using CREATE [UNIQUE] INDEX CONCURRENTLY (289-293, 296).
CREATE TABLE marketplace_listings (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind                text NOT NULL CHECK (kind IN ('agent','squad')),
    title               text NOT NULL,
    summary             text NOT NULL DEFAULT '',
    category            text NOT NULL DEFAULT 'other',
    tags                text[] NOT NULL DEFAULT '{}',
    version             text NOT NULL DEFAULT '1.0.0',
    author_id           uuid NOT NULL,
    author_display_name text NOT NULL,
    source_workspace_id uuid NOT NULL,
    template_id         text NOT NULL,
    template            jsonb NOT NULL,
    status              text NOT NULL DEFAULT 'published'
                        CHECK (status IN ('published','archived')),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
