-- Data-monitoring dashboard aggregation queries (CLO-170).
--
-- Four endpoints back the `/{slug}/dashboard` monitoring page:
--   issue-distribution, activity, comments, completion.
-- 口径 follows PRD prd-data-monitoring-dashboard.md §4. Statistic semantics:
--   * issue-distribution is a current-state snapshot of ALL workspace issues
--     (PRD §4.1 "平台内全部 Issue"), so it takes no time window.
--   * activity workload = in_progress+in_review assigned count (在办负荷);
--     activity score = status_changed + comment + run counts in the window.
--   * comments series is hourly for the 24h range, daily for 7d/30d.
--   * completion/delay rates are computed over issues created inside the
--     window (快照口径, PRD §4.5); deltas compare against the preceding
--     equal-length window; trend is a per-day cohort breakdown.

-- name: ListMonitoringIssueStatusCounts :many
-- Count every issue in the workspace by its current status.
SELECT
    status,
    COUNT(*)::bigint AS count
FROM issue
WHERE workspace_id = $1
GROUP BY status
ORDER BY status;

-- name: ListMonitoringProjectStatusCounts :many
-- Per-project status composition across the whole workspace. Issues without
-- a project collapse into a single row with an empty project_id/name so the
-- per-project totals always sum to the workspace total.
SELECT
    COALESCE(p.id::text, '')::text AS project_id,
    COALESCE(p.title, '')          AS project_name,
    i.status                       AS status,
    COUNT(*)::bigint               AS count
FROM issue i
LEFT JOIN project p ON p.id = i.project_id
WHERE i.workspace_id = $1
GROUP BY p.id, p.title, i.status
ORDER BY COALESCE(p.id::text, ''), i.status;

-- name: ListMonitoringAgentLoad :many
-- Current workload per agent: issues assigned to the agent that are
-- in_progress or in_review (在办负荷, PRD §4.3).
SELECT
    a.id,
    a.name,
    COUNT(i.id) FILTER (WHERE i.status IN ('in_progress', 'in_review'))::bigint AS load_count
FROM agent a
LEFT JOIN issue i ON i.assignee_type = 'agent' AND i.assignee_id = a.id
WHERE a.workspace_id = $1
  AND a.archived_at IS NULL
GROUP BY a.id, a.name;

-- name: CountMonitoringStatusChangesByAgent :many
-- status_changed activities authored by each agent within the window.
SELECT
    actor_id,
    COUNT(*)::bigint AS count
FROM activity_log
WHERE workspace_id = $1
  AND actor_type = 'agent'
  AND action = 'status_changed'
  AND created_at >= $2
GROUP BY actor_id;

-- name: CountMonitoringCommentsByAgent :many
-- Comments authored by each agent within the window.
SELECT
    author_id,
    COUNT(*)::bigint AS count
FROM comment
WHERE workspace_id = $1
  AND author_type = 'agent'
  AND created_at >= $2
GROUP BY author_id;

-- name: CountMonitoringRunsByAgent :many
-- Agent task queue runs (rows created) per agent within the window.
SELECT
    atq.agent_id,
    COUNT(*)::bigint AS count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.created_at >= $2
GROUP BY atq.agent_id;

-- name: ListMonitoringSquadLoad :many
-- Current workload per squad: issues assigned to the squad that are
-- in_progress or in_review.
SELECT
    s.id,
    s.name,
    COUNT(i.id) FILTER (WHERE i.status IN ('in_progress', 'in_review'))::bigint AS load_count
FROM squad s
LEFT JOIN issue i ON i.assignee_type = 'squad' AND i.assignee_id = s.id
WHERE s.workspace_id = $1
GROUP BY s.id, s.name;

-- name: ListMonitoringSquadMemberAgents :many
-- agent members of every squad in the workspace (used to roll squad-level
-- activity up from its member agents).
SELECT
    s.id       AS squad_id,
    sm.member_id AS agent_id
FROM squad s
JOIN squad_member sm ON sm.squad_id = s.id AND sm.member_type = 'agent'
WHERE s.workspace_id = $1;

-- name: CountMonitoringCommentsInWindow :one
-- Total comments in the workspace created at/after `since`.
SELECT COUNT(*)::bigint AS count
FROM comment
WHERE workspace_id = $1
  AND created_at >= $2;

-- name: ListMonitoringCommentsHourly :many
-- Comments bucketed by hour (bucket boundaries in the viewer's tz).
-- Bucket is the local hour start as a bare timestamp.
SELECT
    date_trunc('hour', created_at AT TIME ZONE $2::text)::timestamp AS bucket,
    COUNT(*)::bigint AS count
FROM comment
WHERE workspace_id = $1
  AND created_at >= $3
GROUP BY bucket
ORDER BY bucket;

-- name: ListMonitoringCommentsDaily :many
-- Comments bucketed by calendar day (day boundaries in the viewer's tz).
SELECT
    (created_at AT TIME ZONE $2::text)::date AS day,
    COUNT(*)::bigint AS count
FROM comment
WHERE workspace_id = $1
  AND created_at >= $3
GROUP BY day
ORDER BY day;

-- name: GetMonitoringCompletionOverview :one
-- Snapshot over issues created at/after `since`:
--   completion = done/total; delay = overdue-with-due-date / with-due-date,
-- where "overdue" means the due_date is before the viewer's local today and
-- the issue is not done.
SELECT
    COUNT(*)::bigint AS total,
    COUNT(*) FILTER (WHERE status = 'done')::bigint AS done,
    COUNT(*) FILTER (WHERE due_date IS NOT NULL)::bigint AS has_due,
    COUNT(*) FILTER (
        WHERE due_date IS NOT NULL
          AND due_date < (now() AT TIME ZONE $2::text)::date
          AND status <> 'done'
    )::bigint AS overdue
FROM issue
WHERE workspace_id = $1
  AND created_at >= $3;

-- name: ListMonitoringCompletionTrend :many
-- Per-day cohort breakdown over issues created at/after `since`: for each
-- local calendar day, how many were created, how many are done, how many
-- carry a due_date, and how many are overdue as of the viewer's local today.
SELECT
    (created_at AT TIME ZONE $2::text)::date AS day,
    COUNT(*)::bigint AS total,
    COUNT(*) FILTER (WHERE status = 'done')::bigint AS done,
    COUNT(*) FILTER (WHERE due_date IS NOT NULL)::bigint AS has_due,
    COUNT(*) FILTER (
        WHERE due_date IS NOT NULL
          AND due_date < (now() AT TIME ZONE $2::text)::date
          AND status <> 'done'
    )::bigint AS overdue
FROM issue
WHERE workspace_id = $1
  AND created_at >= $3
GROUP BY day
ORDER BY day;
