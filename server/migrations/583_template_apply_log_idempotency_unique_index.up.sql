-- Single statement: CREATE UNIQUE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a multi-command migration file (repo rule).
--
-- Idempotency scope is per (workspace, caller-supplied key): the same key
-- replayed in a different workspace must not collide, and two different
-- callers in one workspace may not share a key.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS template_apply_log_workspace_key_unique
    ON template_apply_log (workspace_id, idempotency_key);
