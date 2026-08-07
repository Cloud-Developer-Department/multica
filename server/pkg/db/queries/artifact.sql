-- Artifact queries for the Artifact review-loop domain (CLO-146).
--
-- No foreign keys (repo convention); the application-layer ArtifactService
-- owns consistency. Artifact review uses a compare-and-swap status update
-- (`status = 'submitted'` guard) to prevent duplicate reviews under
-- concurrency (R4).

-- name: CreateArtifact :one
INSERT INTO artifact (
    workspace_id, workflow_id, node_id, issue_id, type, title,
    content, content_type, file_attachment_id, version, status,
    author_type, author_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
) RETURNING *;

-- name: GetArtifact :one
SELECT * FROM artifact
WHERE id = $1 AND workspace_id = $2;

-- name: GetArtifactsByNodeType :many
SELECT * FROM artifact
WHERE node_id = $1 AND type = $2
ORDER BY version ASC;

-- name: GetArtifactByNodeTypeVersion :one
SELECT * FROM artifact
WHERE node_id = $1 AND type = $2 AND version = $3;

-- name: GetNextArtifactVersion :one
SELECT COALESCE(MAX(version), 0) + 1 AS next_version
FROM artifact
WHERE node_id = $1 AND type = $2;

-- name: HasApprovedArtifact :one
-- P1-1: review gate for the Q4 write-back hook. A review-required node may
-- only be mirrored to 'done' when at least one of its artifacts was approved;
-- otherwise an agent could set the child issue straight to done and bypass
-- the human review loop (AC-REV).
SELECT EXISTS(
    SELECT 1 FROM artifact
    WHERE node_id = $1 AND status = 'approved'
    LIMIT 1
) AS approved;

-- name: ListArtifacts :many
SELECT * FROM artifact
WHERE workspace_id = $1
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('node_id')::uuid IS NULL OR node_id = sqlc.narg('node_id'))
  AND (sqlc.narg('workflow_id')::uuid IS NULL OR workflow_id = sqlc.narg('workflow_id'))
  AND (sqlc.narg('author_id')::uuid IS NULL OR author_id = sqlc.narg('author_id'))
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountArtifacts :one
SELECT COUNT(*) FROM artifact
WHERE workspace_id = $1
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('node_id')::uuid IS NULL OR node_id = sqlc.narg('node_id'))
  AND (sqlc.narg('workflow_id')::uuid IS NULL OR workflow_id = sqlc.narg('workflow_id'))
  AND (sqlc.narg('author_id')::uuid IS NULL OR author_id = sqlc.narg('author_id'));

-- name: UpdateArtifactStatus :one
UPDATE artifact SET
    status = $2,
    updated_at = now()
WHERE id = $1 AND workspace_id = $3
RETURNING *;

-- name: UpdateArtifactStatusFrom :one
-- CAS guard on the expected current status. Used by review so a concurrent
-- duplicate review hits zero rows -> already_reviewed (R4).
UPDATE artifact SET
    status = sqlc.arg('new_status'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: SupersedeArtifactVersions :exec
-- Marks every other version of the same (node, type) artifact as superseded
-- when a new version is submitted (AC-AR2).
UPDATE artifact SET
    status = 'superseded',
    updated_at = now()
WHERE node_id = $1 AND type = $2 AND id <> $3 AND status <> 'superseded';

-- name: CreateArtifactReview :one
INSERT INTO artifact_review (
    artifact_id, reviewer_type, reviewer_id, action, comment
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: ListArtifactReviews :many
SELECT * FROM artifact_review
WHERE artifact_id = $1
ORDER BY created_at ASC;

-- name: ListReviewQueue :many
SELECT * FROM artifact
WHERE workspace_id = $1 AND status = 'submitted'
ORDER BY created_at ASC
LIMIT $2 OFFSET $3;

-- name: CountReviewQueue :one
SELECT COUNT(*) FROM artifact
WHERE workspace_id = $1 AND status = 'submitted';

-- name: ListArtifactsInRange :many
SELECT * FROM artifact
WHERE workspace_id = $1
  AND created_at >= sqlc.arg('from_created_at') AND created_at <= sqlc.arg('to_created_at')
ORDER BY created_at ASC;

-- name: ListArtifactReviewsInRange :many
-- Stats support (Q6): every review whose artifact belongs to the workspace,
-- within the time window. avg_review_duration is computed from
-- reviewed_at - artifact_created_at in the service.
SELECT ar.artifact_id, ar.action, ar.comment, ar.created_at AS reviewed_at,
       a.created_at AS artifact_created_at, a.type AS artifact_type
FROM artifact_review ar
JOIN artifact a ON a.id = ar.artifact_id
WHERE a.workspace_id = $1
  AND ar.created_at >= sqlc.arg('from_created_at') AND ar.created_at <= sqlc.arg('to_created_at')
ORDER BY ar.created_at ASC;
