-- name: ListIssueTemplates :many
SELECT * FROM issue_template
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY is_preset DESC, LOWER(name) ASC;

-- name: GetIssueTemplate :one
SELECT * FROM issue_template
WHERE id = $1 AND workspace_id = $2;

-- name: CreateIssueTemplate :one
INSERT INTO issue_template (
    workspace_id, name, description, title_template, body_template,
    status, priority, assignee_type, assignee_id, project_id, stage,
    label_ids, icon, category, is_preset, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
) RETURNING *;

-- name: UpdateIssueTemplate :one
-- COALESCE pattern mirrors UpdateIssue: nil fields keep the stored value.
-- assignee_type/assignee_id/project_id/stage use the raw narg form (NULL
-- clears) because templates may legitimately unset an assignee or project.
UPDATE issue_template SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    title_template = COALESCE(sqlc.narg('title_template'), title_template),
    body_template = COALESCE(sqlc.narg('body_template'), body_template),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    project_id = sqlc.narg('project_id'),
    stage = sqlc.narg('stage'),
    label_ids = COALESCE(sqlc.narg('label_ids'), label_ids),
    icon = COALESCE(sqlc.narg('icon'), icon),
    category = COALESCE(sqlc.narg('category'), category),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteIssueTemplate :one
-- :one RETURNING id so the handler distinguishes pgx.ErrNoRows (→ 404) from
-- infrastructure errors (→ 500), and avoids a TOCTOU precheck.
DELETE FROM issue_template
WHERE id = $1 AND workspace_id = $2
RETURNING id;

-- name: CountIssueTemplates :one
SELECT COUNT(*) FROM issue_template
WHERE workspace_id = $1;
