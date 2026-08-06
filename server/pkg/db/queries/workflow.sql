-- Workflow queries for the Workflow state machine domain (CLO-146).
--
-- Relationships are NOT enforced with foreign keys (repo convention); the
-- application-layer WorkflowService owns consistency.

-- name: CreateWorkflow :one
INSERT INTO workflow (
    workspace_id, source_issue_id, name, description, definition,
    status, current_stage, created_by_type, created_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetWorkflow :one
SELECT * FROM workflow
WHERE id = $1 AND workspace_id = $2;

-- name: GetWorkflowBySourceIssue :one
SELECT * FROM workflow
WHERE source_issue_id = $1 AND workspace_id = $2;

-- name: ListWorkflows :many
SELECT * FROM workflow
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountWorkflows :one
SELECT COUNT(*) FROM workflow
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'));

-- name: UpdateWorkflow :one
UPDATE workflow SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: UpdateWorkflowStatus :one
UPDATE workflow SET
    status = $2,
    updated_at = now()
WHERE id = $1 AND workspace_id = $3
RETURNING *;

-- name: AdvanceWorkflowStage :one
-- Compare-and-swap on current_stage prevents concurrent double-advance
-- (the CAS UPDATE pattern from the database design, AC-W3). The target
-- current_stage is the actual next stage to open ($4), which NextStageToOpen
-- may jump past already-completed stages (P2-1), not a fixed +1.
UPDATE workflow SET
    current_stage = $4,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND current_stage = $3
RETURNING *;

-- name: CreateWorkflowNode :one
INSERT INTO workflow_node (
    workflow_id, seq, stage, type, name, description, status,
    issue_id, assignee_type, assignee_id, review_required
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: GetWorkflowNode :one
SELECT * FROM workflow_node
WHERE id = $1 AND workflow_id = $2;

-- name: GetWorkflowNodeByIssue :one
SELECT * FROM workflow_node
WHERE issue_id = $1;

-- name: ListWorkflowNodes :many
SELECT * FROM workflow_node
WHERE workflow_id = $1
  AND (sqlc.narg('stage')::int IS NULL OR stage = sqlc.narg('stage'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY stage ASC, seq ASC;

-- name: UpdateWorkflowNodeStatus :one
UPDATE workflow_node SET
    status = $2,
    started_at = CASE WHEN $2 = 'in_progress' AND started_at IS NULL THEN now() ELSE started_at END,
    completed_at = CASE WHEN $2 = 'done' THEN now() ELSE completed_at END,
    updated_at = now()
WHERE id = $1 AND workflow_id = $3
RETURNING *;

-- name: CreateWorkflowTransitionLog :one
INSERT INTO workflow_transition_log (
    workflow_id, node_id, from_status, to_status, actor_type, actor_id, reason
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: ListWorkflowTransitions :many
SELECT * FROM workflow_transition_log
WHERE workflow_id = $1
  AND (sqlc.narg('node_id')::uuid IS NULL OR node_id = sqlc.narg('node_id'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountWorkflowTransitions :one
SELECT COUNT(*) FROM workflow_transition_log
WHERE workflow_id = $1
  AND (sqlc.narg('node_id')::uuid IS NULL OR node_id = sqlc.narg('node_id'));
