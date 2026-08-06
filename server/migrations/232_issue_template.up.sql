-- Issue templates (CLO-159): workspace-level presets that pre-fill an issue's
-- fields on creation. Structurally a sibling of issue_label / issue_property
-- (workspace-scoped catalog) but carries issue field presets instead of a
-- taxonomy or typed definition.
--
-- Field presets map 1:1 to CreateIssueRequest fields so the frontend can
-- apply a template by POSTing the template's fields to /api/issues. Labels
-- are stored as a JSONB array of label UUIDs (strings) — matching the
-- CreateIssueRequest.label_ids shape — so no junction table is needed and
-- template application stays a single round-trip.
--
-- is_preset marks the 4 built-in templates seeded per workspace. They are
-- editable (owners may tweak the preset content) but protected from deletion
-- so the catalog stays stable. No foreign keys per the repo migration rules;
-- assignee/project/label validity is enforced in the handler.

CREATE TABLE issue_template (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- Issue field presets.
    title_template TEXT NOT NULL DEFAULT '',
    body_template TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'todo'
        CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled')),
    priority TEXT NOT NULL DEFAULT 'none'
        CHECK (priority IN ('urgent', 'high', 'medium', 'low', 'none')),
    assignee_type TEXT CHECK (assignee_type IN ('member', 'agent', 'squad')),
    assignee_id UUID,
    project_id UUID,
    stage INT,
    -- JSONB array of label UUID strings. Validated against issue_label in the
    -- handler; stored loosely so label renames/deletes never cascade here.
    label_ids JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(label_ids) = 'array'),
    -- Display + grouping in the template picker UI.
    icon TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    -- Preset templates are seeded per workspace and protected from deletion.
    is_preset BOOLEAN NOT NULL DEFAULT false,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- All indexes live in follow-up, single-statement migrations because CREATE
-- INDEX CONCURRENTLY cannot share a migration with other statements. The
-- workspace filter index is in 233.
