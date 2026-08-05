-- Node ordering is unique per (workflow, stage): seq restarts at 1 inside each
-- stage (see WorkflowService.Create's per-stage numbering), so the unique key
-- must include stage. The original single-column (workflow_id, seq) index was
-- wrong for the 5-stage template (every stage has a seq=1 node) and made
-- POST /api/workflows fail with a duplicate-key on the second stage. Fresh
-- installs get the correct composite index here; already-applied environments
-- are repaired by migrations 250/251 (CLO-146 deployment fix).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workflow_node_workflow_stage_seq
    ON workflow_node (workflow_id, stage, seq);
