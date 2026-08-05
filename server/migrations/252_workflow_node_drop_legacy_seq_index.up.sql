-- CLO-146 deployment fix: environments that already applied migration 237
-- (before its columns were corrected) carry the legacy single-column index
-- idx_workflow_node_workflow_seq ON (workflow_id, seq). With per-stage seq
-- numbering this index is wrong and makes POST /api/workflows fail on the
-- second stage (duplicate key). Drop it; the composite index is installed by
-- 251 (or was already created by the corrected 237 on fresh installs).
DROP INDEX CONCURRENTLY IF EXISTS idx_workflow_node_workflow_seq;
