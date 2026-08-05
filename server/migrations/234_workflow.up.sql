-- Workflow state machine for the AI software R&D pipeline (CLO-146).
--
-- workflow: a workflow instance derived from a source_issue (the requirement
--   issue). Its status reuses the issue status set (Q1) and its definition
--   column is a JSONB snapshot of the template that created it.
-- workflow_node: a node in the workflow. Each node maps 1:1 to a child issue
--   (Q4) so the existing agent-task trigger / daemon / write-back chain is
--   reused unchanged. Node status reuses the issue status set (Q1), with
--   'backlog' meaning "not yet activated".
-- workflow_transition_log: append-only status-transition audit.
--
-- No FOREIGN KEY constraints anywhere (repo convention): relationships are
-- validated and cleaned up in application-layer transactions.
CREATE TABLE workflow (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    source_issue_id UUID NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT,
    definition      JSONB NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT NOT NULL DEFAULT 'todo'
        CHECK (status IN ('todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled')),
    current_stage   INT NOT NULL DEFAULT 1
        CHECK (current_stage >= 1),
    created_by_type TEXT NOT NULL
        CHECK (created_by_type IN ('member', 'agent')),
    created_by_id   UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workflow_node (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id     UUID NOT NULL,
    seq             INT NOT NULL,
    stage           INT NOT NULL
        CHECK (stage >= 1),
    type            TEXT NOT NULL
        CHECK (type IN ('requirements', 'architecture', 'development', 'testing', 'code_review', 'security', 'documentation', 'deployment', 'other')),
    name            TEXT NOT NULL,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'backlog'
        CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled')),
    issue_id        UUID NOT NULL,
    assignee_type   TEXT
        CHECK (assignee_type IN ('member', 'agent', 'squad')),
    assignee_id     UUID,
    review_required BOOLEAN NOT NULL DEFAULT true,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workflow_transition_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL,
    node_id     UUID,
    from_status TEXT,
    to_status   TEXT NOT NULL,
    actor_type  TEXT NOT NULL
        CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id    UUID,
    reason      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
