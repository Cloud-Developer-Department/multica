-- CLO-146 deployment fix: ensure the correct composite unique index
-- (workflow_id, stage, seq) exists on already-applied environments where the
-- original migration 237 installed the wrong single-column index. IF NOT
-- EXISTS makes this a no-op on fresh installs (corrected 237 already created
-- it).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workflow_node_workflow_stage_seq
    ON workflow_node (workflow_id, stage, seq);
