-- CLO-248: idempotent apply log for resource templates.
--
-- One row per successful apply that carried an idempotency_key, storing the
-- full response payload so a retry with the same (workspace, key) replays the
-- first result instead of re-creating resources.
--
-- Deliberately NO foreign keys: the template_id / created resource ids are
-- provenance handles, not relationships the database should enforce (repo
-- rule: relationships are maintained in the application layer).
--
-- The unique index lives in its own migration file (233) and uses CREATE
-- UNIQUE INDEX CONCURRENTLY per the repository's index rule.
CREATE TABLE template_apply_log (
    id              BIGSERIAL PRIMARY KEY,
    workspace_id    UUID NOT NULL,
    idempotency_key TEXT NOT NULL,
    template_id     TEXT NOT NULL,
    template_version TEXT NOT NULL,
    result          JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
