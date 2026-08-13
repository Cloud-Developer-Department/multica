-- name: CreateFeedback :one
-- Single feedback row insert. Used by both the legacy message-only submission
-- (desktop error reporting) and the Feedback Center submission; both map their
-- inputs onto creator_id/title/type/description in the handler layer.
INSERT INTO feedback (creator_id, workspace_id, title, type, description, metadata)
VALUES ($1, sqlc.narg(workspace_id), $2, $3, $4, $5)
RETURNING *;

-- name: CountRecentFeedbackByUser :one
SELECT count(*) FROM feedback
WHERE creator_id = $1 AND created_at > now() - interval '1 hour';

-- name: ListFeedbacks :many
-- Feedback Center list with optional type/keyword filters, aggregate vote and
-- comment counts, and the viewer's own vote flag (my_vote). Sort is driven by
-- the 'sort' param ('latest' | 'hot' | 'comments'); the two CASE columns are
-- NULL for every row when their sort mode is inactive, so ordering falls
-- through to created_at DESC as the stable secondary key.
SELECT f.id, f.workspace_id, f.creator_id, f.title, f.description, f.type,
       f.created_at, f.updated_at,
       u.name AS creator_name,
       u.avatar_url AS creator_avatar_url,
       COUNT(DISTINCT v.id) AS vote_count,
       COUNT(DISTINCT c.id) AS comment_count,
       EXISTS (SELECT 1 FROM feedback_vote vv WHERE vv.feedback_id = f.id AND vv.user_id = $2) AS my_vote
FROM feedback f
JOIN "user" u ON u.id = f.creator_id
LEFT JOIN feedback_vote v ON v.feedback_id = f.id
LEFT JOIN feedback_comment c ON c.feedback_id = f.id
WHERE f.workspace_id = $1
  AND (sqlc.narg('type')::text IS NULL OR f.type = sqlc.narg('type'))
  AND (sqlc.narg('keyword')::text IS NULL
       OR f.title ILIKE '%' || sqlc.narg('keyword') || '%'
       OR f.description ILIKE '%' || sqlc.narg('keyword') || '%')
GROUP BY f.id, u.name, u.avatar_url
ORDER BY
  CASE WHEN sqlc.narg('sort')::text = 'hot' THEN COUNT(DISTINCT v.id) END DESC NULLS LAST,
  CASE WHEN sqlc.narg('sort')::text = 'comments' THEN COUNT(DISTINCT c.id) END DESC NULLS LAST,
  f.created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountFeedbacks :one
-- Total row count for the same filter set as ListFeedbacks (pagination total).
SELECT count(*) FROM feedback f
WHERE f.workspace_id = $1
  AND (sqlc.narg('type')::text IS NULL OR f.type = sqlc.narg('type'))
  AND (sqlc.narg('keyword')::text IS NULL
       OR f.title ILIKE '%' || sqlc.narg('keyword') || '%'
       OR f.description ILIKE '%' || sqlc.narg('keyword') || '%');

-- name: GetFeedback :one
-- Feedback detail with aggregate counts and the viewer's vote flag, scoped to
-- the current workspace.
SELECT f.id, f.workspace_id, f.creator_id, f.title, f.description, f.type,
       f.created_at, f.updated_at,
       u.name AS creator_name,
       u.avatar_url AS creator_avatar_url,
       (SELECT count(*) FROM feedback_vote v WHERE v.feedback_id = f.id) AS vote_count,
       (SELECT count(*) FROM feedback_comment c WHERE c.feedback_id = f.id) AS comment_count,
       EXISTS (SELECT 1 FROM feedback_vote vv WHERE vv.feedback_id = f.id AND vv.user_id = $2) AS my_vote
FROM feedback f
JOIN "user" u ON u.id = f.creator_id
WHERE f.id = $1 AND f.workspace_id = $3;

-- name: GetFeedbackInWorkspace :one
-- Existence / ownership check used by the vote and comment endpoints.
SELECT id, workspace_id FROM feedback
WHERE id = $1 AND workspace_id = $2;

-- name: CreateFeedbackVote :exec
-- Idempotent: ON CONFLICT DO NOTHING means a duplicate vote is a no-op, backed
-- by the UNIQUE(feedback_id, user_id) constraint in migration 286.
INSERT INTO feedback_vote (feedback_id, user_id)
VALUES ($1, $2)
ON CONFLICT (feedback_id, user_id) DO NOTHING;

-- name: DeleteFeedbackVote :exec
-- Idempotent: deleting a vote that does not exist is a no-op.
DELETE FROM feedback_vote WHERE feedback_id = $1 AND user_id = $2;

-- name: CountFeedbackVotes :one
SELECT count(*) FROM feedback_vote WHERE feedback_id = $1;

-- name: ListFeedbackComments :many
-- Feedback comments in chronological order with author display info.
SELECT c.id, c.feedback_id, c.user_id, c.content, c.created_at, c.updated_at,
       u.name AS user_name,
       u.avatar_url AS user_avatar_url
FROM feedback_comment c
JOIN "user" u ON u.id = c.user_id
WHERE c.feedback_id = $1
ORDER BY c.created_at ASC, c.id ASC;

-- name: CountFeedbackComments :one
SELECT count(*) FROM feedback_comment WHERE feedback_id = $1;

-- name: CreateFeedbackComment :one
INSERT INTO feedback_comment (feedback_id, user_id, content)
VALUES ($1, $2, $3)
RETURNING *;
