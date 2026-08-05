-- Artifact lifecycle for the AI software R&D pipeline (CLO-146).
--
-- artifact: one row per version. Content-style artifacts store their text in
--   `content`; file-style artifacts register the attachment id returned by
--   /api/upload-file (Q2: file in the designated folder + database record).
--   Versions increment per (node_id, type); superseded versions are kept so
--   history and diff remain available (Q5 / AC-AR2).
-- artifact_review: append-only review record. Reviewer must be a member
--   (Q3): the source_issue creator, or a workspace owner/admin override.
--
-- No FOREIGN KEY constraints anywhere (repo convention): relationships are
-- validated in the application layer.
CREATE TABLE artifact (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       UUID NOT NULL,
    workflow_id        UUID NOT NULL,
    node_id            UUID NOT NULL,
    issue_id           UUID,
    type               TEXT NOT NULL
        CHECK (type IN ('requirements', 'architecture', 'development', 'testing', 'code_review', 'security', 'documentation', 'deployment', 'other')),
    title              TEXT NOT NULL,
    content            TEXT,
    content_type       TEXT NOT NULL DEFAULT 'markdown'
        CHECK (content_type IN ('markdown', 'json', 'text', 'file')),
    file_attachment_id UUID,
    version            INT NOT NULL DEFAULT 1,
    status             TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'submitted', 'approved', 'rejected', 'superseded')),
    author_type        TEXT NOT NULL
        CHECK (author_type IN ('member', 'agent')),
    author_id          UUID NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE artifact_review (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id   UUID NOT NULL,
    reviewer_type TEXT NOT NULL
        CHECK (reviewer_type IN ('member')),
    reviewer_id   UUID NOT NULL,
    action        TEXT NOT NULL
        CHECK (action IN ('approved', 'rejected')),
    comment       TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
