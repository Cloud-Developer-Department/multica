-- A child issue maps to at most one workflow node (1:1) — enables the
-- UpdateIssue status-write-back lookup.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_workflow_node_issue
    ON workflow_node (issue_id);
