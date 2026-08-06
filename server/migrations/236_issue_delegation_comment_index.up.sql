-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file (repository hard rule).
--
-- Supports the delegation edit-idempotency lookup (find active child issues by
-- (delegation_comment_id, assignee_id), LIU-13 O5 / AC-6) and provenance
-- tracing. The partial predicate keeps the index off the non-delegated
-- majority of issue rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_delegation_comment
    ON issue (delegation_comment_id)
    WHERE delegation_comment_id IS NOT NULL;
