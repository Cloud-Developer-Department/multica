-- name: CreateSquad :one
-- CLO-250 DEF-4 (architect ruling): instructions is part of the portable
-- squad contract (SquadSpec) and the column already exists (088_squad_instructions),
-- but the insert previously omitted it, so template-apply squads could never
-- carry instructions; they only could be patched afterwards via UpdateSquad.
-- model / permission_mode are agent-level concepts and must NOT be added here.
-- upgrade_on_member_mention (LIU-13): COALESCE defaults to true so callers
-- that omit it keep the member-mention auto-upgrade behavior.
INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, avatar_url, instructions, upgrade_on_member_mention)
VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE(sqlc.narg('upgrade_on_member_mention'), true))
RETURNING *;

-- name: GetSquad :one
SELECT * FROM squad WHERE id = $1;

-- name: GetSquadInWorkspace :one
SELECT * FROM squad WHERE id = $1 AND workspace_id = $2;

-- name: ListSquads :many
SELECT * FROM squad WHERE workspace_id = $1 AND archived_at IS NULL ORDER BY created_at ASC;

-- name: ListSquadMemberPreviewRows :many
-- Static squad membership summary for list/hover previews. This deliberately
-- excludes derived runtime/task status; the squad detail members-status
-- endpoint owns live state.
SELECT
    sm.squad_id,
    sm.member_type,
    sm.member_id,
    sm.role
FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
WHERE s.workspace_id = $1 AND s.archived_at IS NULL
ORDER BY
    sm.squad_id ASC,
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id) DESC,
    sm.created_at ASC;

-- name: ListSquadMemberPreviewRowsBySquad :many
SELECT
    sm.squad_id,
    sm.member_type,
    sm.member_id,
    sm.role
FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
WHERE sm.squad_id = $1
ORDER BY
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id) DESC,
    sm.created_at ASC;

-- name: ListAllSquads :many
SELECT * FROM squad WHERE workspace_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquad :one
UPDATE squad SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    leader_id = COALESCE(sqlc.narg('leader_id'), leader_id),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    upgrade_on_member_mention = COALESCE(sqlc.narg('upgrade_on_member_mention'), upgrade_on_member_mention),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveSquad :one
UPDATE squad SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetSquadParent :execrows
-- Attach a squad under a parent squad (v1: one level — the handler validates
-- that the child is not already nested and belongs to the same workspace).
UPDATE squad SET parent_squad_id = $2, updated_at = now()
WHERE id = $1;

-- name: ClearSquadParent :execrows
-- Detach a single squad from its parent (no-op when it has no parent).
UPDATE squad SET parent_squad_id = NULL, updated_at = now()
WHERE id = $1 AND parent_squad_id IS NOT NULL;

-- name: ClearChildSquadParents :execrows
-- Detach every child when a parent squad is archived. Children become
-- independent squads again and are NOT archived along with the parent.
UPDATE squad SET parent_squad_id = NULL, updated_at = now()
WHERE parent_squad_id = $1;

-- name: ListSquadsByIdsInWorkspace :many
-- Fetch squads by id scoped to a workspace. Used to validate
-- included_squad_ids at create time (existence / same-workspace / archive /
-- one-level nesting).
SELECT * FROM squad WHERE id = ANY($1::uuid[]) AND workspace_id = $2;

-- name: ListChildSquadSummaries :many
-- Lightweight child-squad summary (id, name, member count) for a single
-- squad's detail response.
SELECT s.id, s.name, count(sm.id)::int AS member_count
FROM squad s
LEFT JOIN squad_member sm ON sm.squad_id = s.id
WHERE s.parent_squad_id = $1 AND s.archived_at IS NULL
GROUP BY s.id
ORDER BY s.created_at ASC;

-- name: ListChildSquadSummariesByWorkspace :many
-- Batch child-squad summary for the squad list response. One row per child
-- squad (carrying its parent id) so the handler can group children under
-- their parents in a single pass.
SELECT s.parent_squad_id AS parent_squad_id,
       s.id             AS id,
       s.name           AS name,
       count(sm.id)::int AS member_count
FROM squad s
LEFT JOIN squad_member sm ON sm.squad_id = s.id
WHERE s.workspace_id = $1
  AND s.parent_squad_id IS NOT NULL
  AND s.archived_at IS NULL
GROUP BY s.id
ORDER BY s.created_at ASC;

-- name: ListChildSquadMembers :many
-- Members of every non-archived child squad of a given squad, each row
-- annotated with its child squad's name so the leader roster can label the
-- origin squad. v1 nesting is one level, so child squads cannot have
-- children of their own.
SELECT sm.id               AS id,
       sm.squad_id         AS squad_id,
       sm.member_type      AS member_type,
       sm.member_id        AS member_id,
       sm.role             AS role,
       sm.created_at       AS created_at,
       s.name              AS child_squad_name
FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
WHERE s.parent_squad_id = $1 AND s.archived_at IS NULL
ORDER BY s.created_at ASC, sm.created_at ASC;

-- name: AddSquadMember :one
INSERT INTO squad_member (squad_id, member_type, member_id, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: RemoveSquadMember :execrows
DELETE FROM squad_member
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3;

-- name: ListSquadMembers :many
SELECT * FROM squad_member WHERE squad_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquadMemberRole :one
UPDATE squad_member SET role = $4
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
RETURNING *;

-- name: IsSquadMember :one
SELECT EXISTS(
    SELECT 1 FROM squad_member
    WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
) AS is_member;

-- name: CountSquadMembers :one
SELECT count(*) FROM squad_member WHERE squad_id = $1;

-- name: GetSquadByAssignee :one
-- Look up the squad when an issue is assigned to a squad.
SELECT s.* FROM squad s WHERE s.id = $1 AND s.workspace_id = $2;

-- name: ListSquadsByMember :many
-- Find all non-archived squads a given entity belongs to in a workspace.
-- Archived squads are excluded so the F3 member-mention uniqueness judgement
-- ("exactly one squad") counts only live squads, matching ListSquadsLedByAgent
-- (R2): a member of 1 active + 1 archived squad must still upgrade, and a
-- member of only archived squads must not.
SELECT s.* FROM squad s
JOIN squad_member sm ON sm.squad_id = s.id
WHERE s.workspace_id = $1 AND sm.member_type = $2 AND sm.member_id = $3
  AND s.archived_at IS NULL
ORDER BY s.created_at ASC;

-- name: ListSquadsLedByAgent :many
-- Squads an agent leads in a workspace (non-archived). Used by the SR3
-- auto-upgrade rule: a pure @agent mention becomes squad-level when the
-- mentioned agent leads exactly one non-archived squad here.
SELECT * FROM squad
WHERE leader_id = $1 AND workspace_id = $2 AND archived_at IS NULL
ORDER BY created_at ASC;

-- name: TransferSquadAssignees :exec
-- Transfer all issues assigned to a squad to the squad's leader agent.
UPDATE issue SET assignee_type = 'agent', assignee_id = $2, updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;

-- name: TransferSquadAutopilotsToLeader :exec
-- Mirrors TransferSquadAssignees for autopilot rows: when a squad is archived,
-- any autopilot still pointing at the squad would otherwise dangle and the
-- admission gate would skip every subsequent dispatch with "assignee squad
-- cannot be resolved". Rewrite the assignee in place to the leader agent so
-- the autopilot keeps firing under the same leader-only execution semantics
-- it had a moment before the archive (Path A from MUL-2429).
UPDATE autopilot
SET assignee_type = 'agent',
    assignee_id = $2,
    updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;

-- name: ListSquadMemberStatusRows :many
-- Per-row join used to build the squad-members status view. One row per
-- (squad_member × active_task); members with no active task return a
-- single row with NULL task_* columns. Human members and agent members
-- with no agent row also return one row with NULL agent_/runtime_ columns.
-- The handler aggregates rows by member_id.
SELECT
    sm.id              AS squad_member_id,
    sm.member_type     AS member_type,
    sm.member_id       AS member_id,
    a.archived_at      AS agent_archived_at,
    ar.status          AS runtime_status,
    ar.last_seen_at    AS runtime_last_seen_at,
    atq.id             AS task_id,
    atq.status         AS task_status,
    atq.issue_id       AS task_issue_id,
    atq.dispatched_at  AS task_dispatched_at,
    i.number           AS issue_number,
    i.title            AS issue_title,
    i.status           AS issue_status
FROM squad_member sm
LEFT JOIN agent a
       ON sm.member_type = 'agent' AND a.id = sm.member_id
LEFT JOIN agent_runtime ar
       ON ar.id = a.runtime_id
LEFT JOIN agent_task_queue atq
       ON sm.member_type = 'agent'
      AND atq.agent_id = sm.member_id
      AND atq.status IN ('dispatched', 'running', 'waiting_local_directory')
LEFT JOIN issue i
       ON i.id = atq.issue_id
WHERE sm.squad_id = $1
ORDER BY sm.created_at ASC, atq.dispatched_at DESC NULLS LAST;
