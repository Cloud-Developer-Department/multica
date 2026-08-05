-- Engineering analytics platform queries (CLO-239).
--
-- Backs the four-dashboard platform under GET /api/analytics/*:
--   Tab1 Adoption & Activity  (A1-A5): activity/summary, activity/heatmap,
--                                    activity/top-members, adoption/summary,
--                                    adoption/trend
--   Tab2 Agent Performance    (B1-B6): agents/funnel, agents/performance,
--                                    agents/top, skills/overview,
--                                    collaboration/summary,
--                                    collaboration/blockers
--   Tab3 Git Contributions    (G1-G4): git/eloc, git/quality, git/repos,
--                                    git/prs
--   Tab4 DORA                 (D1-D2): dora/lead-time, dora/deployments
--   Identity & departments    (L1-L2): identity/lifecycle,
--                                    identity/departments
--
-- 口径 follows the data spec (数据口径-CLO-228 v2.0). Key conventions:
--   * Every time-bucketed query buckets by the viewer tz ($2::text) so a
--     natural-day window uses the viewer's local midnight as the day boundary.
--   * "active member" = a member (via user_id) that created an issue,
--     authored a comment, or initiated an agent task inside the window.
--   * Optional filters are passed as sentinel defaults, never NULL casts that
--     could vary by driver: department = '' means "all departments",
--     project_id = NULL means "whole workspace" (uuid columns allow NULL).
--   * Ratios are computed in the handler (0-1), never in SQL.
--   * G/D/L readiness is decided by the handler via vcs_connection /
--     deployment_event / identity_import existence; these queries return
--     empty sets when the underlying source has no rows.

-- name: CountAnalyticsMembers :one
-- Total workspace members (optional department slice).
SELECT COUNT(*)::bigint AS count
FROM member m
WHERE m.workspace_id = $1
  AND ($2::text = '' OR m.department = $2);

-- name: CountAnalyticsMembersByDepartment :many
-- Per-department member counts (L2).
SELECT
    m.department::text AS department,
    COUNT(*)::bigint AS member_count
FROM member m
WHERE m.workspace_id = $1 AND m.department IS NOT NULL AND m.department <> ''
GROUP BY m.department
ORDER BY member_count DESC;

-- name: CountAnalyticsMembersActive :one
-- Distinct members with activity at/after `since` (A1 dau/mau/active_members).
-- `project_id` NULL = whole workspace; else activity scoped to that project's
-- issues (comments/tasks are attributed through their issue's project).
SELECT COUNT(*)::bigint AS count
FROM (
    SELECT i.creator_id AS user_id
    FROM issue i
    WHERE i.workspace_id = $1 AND i.creator_type = 'member' AND i.created_at >= $2
      AND ($3::text = '' OR i.creator_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
      AND ($4::uuid IS NULL OR i.project_id = $4)
    UNION
    SELECT c.author_id AS user_id
    FROM comment c
    WHERE c.workspace_id = $1 AND c.author_type = 'member' AND c.created_at >= $2
      AND ($3::text = '' OR c.author_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
      AND ($4::uuid IS NULL OR c.issue_id IN (SELECT p.id FROM issue p WHERE p.workspace_id = $1 AND p.project_id = $4))
    UNION
    SELECT atq.initiator_user_id AS user_id
    FROM agent_task_queue atq
    JOIN issue atqi ON atqi.id = atq.issue_id
    WHERE atqi.workspace_id = $1 AND atq.initiator_user_id IS NOT NULL AND atq.created_at >= $2
      AND ($3::text = '' OR atq.initiator_user_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
      AND ($4::uuid IS NULL OR atqi.project_id = $4)
) members_active;

-- name: ListAnalyticsMemberActivityDays :many
-- Per-member activity: distinct active days, last active timestamp (A1/A3).
-- `since` is the window cutoff; activity sources = issue created by member,
-- comment authored by member, agent task initiated by member.
WITH acts AS (
    SELECT i.creator_id AS user_id,
           (i.created_at AT TIME ZONE $2::text)::date AS day,
           i.created_at AS ts
    FROM issue i
    WHERE i.workspace_id = $1 AND i.creator_type = 'member' AND i.created_at >= $3
      AND ($4::text = '' OR i.creator_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR i.project_id = $5)
    UNION ALL
    SELECT c.author_id AS user_id,
           (c.created_at AT TIME ZONE $2::text)::date AS day,
           c.created_at AS ts
    FROM comment c
    WHERE c.workspace_id = $1 AND c.author_type = 'member' AND c.created_at >= $3
      AND ($4::text = '' OR c.author_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR c.issue_id IN (SELECT p.id FROM issue p WHERE p.workspace_id = $1 AND p.project_id = $5))
    UNION ALL
    SELECT atq.initiator_user_id AS user_id,
           (atq.created_at AT TIME ZONE $2::text)::date AS day,
           atq.created_at AS ts
    FROM agent_task_queue atq
    JOIN issue atqi ON atqi.id = atq.issue_id
    WHERE atqi.workspace_id = $1 AND atq.initiator_user_id IS NOT NULL AND atq.created_at >= $3
      AND ($4::text = '' OR atq.initiator_user_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR atqi.project_id = $5)
)
SELECT
    a.user_id,
    u.name AS member_name,
    COUNT(DISTINCT a.day)::bigint AS active_days,
    MAX(a.ts)::timestamptz AS last_active_at
FROM acts a
JOIN "user" u ON u.id = a.user_id
GROUP BY a.user_id, u.name;

-- name: CountAnalyticsIssuesCreated :one
-- Issues created in the window (optional department slice on the creator,
-- optional project scope).
SELECT COUNT(*)::bigint AS count
FROM issue i
WHERE i.workspace_id = $1 AND i.created_at >= $2
  AND ($3::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
  AND ($4::uuid IS NULL OR i.project_id = $4);

-- name: ListAnalyticsMemberIssueCounts :many
-- Per-member created-issue counts in the window (A1/A3).
SELECT i.creator_id AS user_id, COUNT(*)::bigint AS issue_count
FROM issue i
WHERE i.workspace_id = $1 AND i.creator_type = 'member' AND i.created_at >= $2
  AND ($3::text = '' OR i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
  AND ($4::uuid IS NULL OR i.project_id = $4)
GROUP BY i.creator_id;

-- name: ListAnalyticsActivityHeatmap :many
-- Activity-event counts per (local day, local hour) in the viewer tz (A2
-- metric=activity_events). Events = issue created by member, comment by
-- member, or agent task initiated by member. $2 = tz, $3 = since.
WITH acts AS (
    SELECT i.created_at AS ts
    FROM issue i
    WHERE i.workspace_id = $1 AND i.creator_type = 'member' AND i.created_at >= $3
      AND ($4::text = '' OR i.creator_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR i.project_id = $5)
    UNION ALL
    SELECT c.created_at AS ts
    FROM comment c
    WHERE c.workspace_id = $1 AND c.author_type = 'member' AND c.created_at >= $3
      AND ($4::text = '' OR c.author_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR c.issue_id IN (SELECT p.id FROM issue p WHERE p.workspace_id = $1 AND p.project_id = $5))
    UNION ALL
    SELECT atq.created_at AS ts
    FROM agent_task_queue atq
    JOIN issue atqi ON atqi.id = atq.issue_id
    WHERE atqi.workspace_id = $1 AND atq.initiator_user_id IS NOT NULL AND atq.created_at >= $3
      AND ($4::text = '' OR atq.initiator_user_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR atqi.project_id = $5)
)
SELECT
    (ts AT TIME ZONE $2::text)::date AS day,
    EXTRACT(HOUR FROM ts AT TIME ZONE $2::text)::int AS hour,
    COUNT(*)::bigint AS value
FROM acts
GROUP BY day, hour
ORDER BY day, hour;

-- name: ListAnalyticsIssueVolumeHeatmap :many
-- Issue-creation counts per (local day, local hour) (A2 metric=issue_volume).
SELECT
    (i.created_at AT TIME ZONE $2::text)::date AS day,
    EXTRACT(HOUR FROM i.created_at AT TIME ZONE $2::text)::int AS hour,
    COUNT(*)::bigint AS value
FROM issue i
WHERE i.workspace_id = $1 AND i.created_at >= $3
  AND ($4::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
  AND ($5::uuid IS NULL OR i.project_id = $5)
GROUP BY day, hour
ORDER BY day, hour;

-- name: ListAnalyticsActiveDaysHeatmap :many
-- Distinct active members per (local day, local hour) (A2 metric=active_days).
-- $2 = tz, $3 = since.
WITH acts AS (
    SELECT i.creator_id AS user_id, i.created_at AS ts
    FROM issue i
    WHERE i.workspace_id = $1 AND i.creator_type = 'member' AND i.created_at >= $3
      AND ($4::text = '' OR i.creator_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR i.project_id = $5)
    UNION ALL
    SELECT c.author_id AS user_id, c.created_at AS ts
    FROM comment c
    WHERE c.workspace_id = $1 AND c.author_type = 'member' AND c.created_at >= $3
      AND ($4::text = '' OR c.author_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR c.issue_id IN (SELECT p.id FROM issue p WHERE p.workspace_id = $1 AND p.project_id = $5))
    UNION ALL
    SELECT atq.initiator_user_id AS user_id, atq.created_at AS ts
    FROM agent_task_queue atq
    JOIN issue atqi ON atqi.id = atq.issue_id
    WHERE atqi.workspace_id = $1 AND atq.initiator_user_id IS NOT NULL AND atq.created_at >= $3
      AND ($4::text = '' OR atq.initiator_user_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
      AND ($5::uuid IS NULL OR atqi.project_id = $5)
)
SELECT
    (ts AT TIME ZONE $2::text)::date AS day,
    EXTRACT(HOUR FROM ts AT TIME ZONE $2::text)::int AS hour,
    COUNT(DISTINCT user_id)::bigint AS value
FROM acts
GROUP BY day, hour
ORDER BY day, hour;

-- name: GetAnalyticsAdoptionSummary :one
-- Agent-adoption numerator/denominator (A4): issues updated in the window
-- (口径: 按当前指派，窗口按 updated_at 归属) and how many are agent-assigned.
SELECT
    COUNT(*)::bigint AS total_issues,
    COUNT(*) FILTER (WHERE i.assignee_type = 'agent')::bigint AS agent_assigned
FROM issue i
WHERE i.workspace_id = $1 AND i.updated_at >= $2
  AND ($3::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
  AND ($4::uuid IS NULL OR i.project_id = $4);

-- name: GetAnalyticsAdoptionCoverage :one
-- Issues with any agent participation (assigned to an agent, agent comment,
-- or agent task row) in the window (A4 coverage numerator).
SELECT COUNT(*)::bigint AS agent_covered_issues
FROM issue i
WHERE i.workspace_id = $1 AND i.updated_at >= $2
  AND (i.assignee_type = 'agent'
       OR EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND c.author_type = 'agent')
       OR EXISTS (SELECT 1 FROM agent_task_queue atq WHERE atq.issue_id = i.id))
  AND ($3::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
  AND ($4::uuid IS NULL OR i.project_id = $4);

-- name: ListAnalyticsAdoptionTrend :many
-- Per-day total + agent-assigned issues (A5, by updated_at day).
SELECT
    (i.updated_at AT TIME ZONE $2::text)::date AS day,
    COUNT(*)::bigint AS total_issues,
    COUNT(*) FILTER (WHERE i.assignee_type = 'agent')::bigint AS agent_assigned
FROM issue i
WHERE i.workspace_id = $1 AND i.updated_at >= $3
  AND ($4::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
  AND ($5::uuid IS NULL OR i.project_id = $5)
GROUP BY day
ORDER BY day;

-- name: ListAnalyticsAdoptionTrendCoverage :many
-- Per-day agent-covered issue counts (A5, by updated_at day).
SELECT
    (i.updated_at AT TIME ZONE $2::text)::date AS day,
    COUNT(*)::bigint AS covered
FROM issue i
WHERE i.workspace_id = $1 AND i.updated_at >= $3
  AND (i.assignee_type = 'agent'
       OR EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND c.author_type = 'agent')
       OR EXISTS (SELECT 1 FROM agent_task_queue atq WHERE atq.issue_id = i.id))
  AND ($4::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $4))
  AND ($5::uuid IS NULL OR i.project_id = $5)
GROUP BY day
ORDER BY day;

-- =============================================================
-- Tab2 Agent performance (B1-B6)
-- =============================================================

-- name: CountAnalyticsAssign :one
-- Assign stage = agent_task_queue rows created in the window (B1). Optional
-- department slice attributes a task to the department of its issue creator.
SELECT COUNT(*)::bigint AS assign_count
FROM agent_task_queue atq
JOIN issue i ON i.id = atq.issue_id
WHERE i.workspace_id = $1
  AND atq.created_at >= $2
  AND ($3::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
  AND ($4::uuid IS NULL OR i.project_id = $4);

-- name: CountAnalyticsExecute :one
-- Execute stage = atq rows whose started_at falls in the window (B1).
SELECT COUNT(*)::bigint AS execute_count
FROM agent_task_queue atq
JOIN issue i ON i.id = atq.issue_id
WHERE i.workspace_id = $1
  AND atq.started_at >= $2
  AND ($3::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
  AND ($4::uuid IS NULL OR i.project_id = $4);

-- name: CountAnalyticsIssueAssignedToAgent :one
-- Issue-level agent assignment (B1): issues currently assigned to an agent,
-- windowed by updated_at.
SELECT COUNT(*)::bigint AS issue_assigned_count
FROM issue i
WHERE i.workspace_id = $1 AND i.assignee_type = 'agent' AND i.updated_at >= $2
  AND ($3::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
  AND ($4::uuid IS NULL OR i.project_id = $4);

-- name: CountAnalyticsVCSConnections :one
-- Existence of a VCS connection for the workspace (B1 merged / G readiness).
SELECT COUNT(*)::bigint AS count
FROM vcs_connection
WHERE workspace_id = $1;

-- name: GetAnalyticsWorkspaceRepos :one
-- Workspace-repo config for the workspace (G readiness: local repo analysis
-- counts as a connected Git source even without a VCS connection).
SELECT COALESCE(w.repos, '[]'::jsonb)::text AS repos
FROM workspace w
WHERE w.id = $1;

-- name: CountAnalyticsMergedIssues :one
-- Merged stage (B1): distinct issues linked to a merged PR whose merged_at
-- falls in the window. Requires VCS connection (caller gates on it).
-- Optional department slice attributes the issue to the department of its
-- creator (member), and project_id scopes to a project (P2-01) so the funnel's
-- three stages share the same filters.
SELECT COUNT(DISTINCT ipr.issue_id)::bigint AS merged_count
FROM vcs_pull_request pr
JOIN issue_vcs_pull_request ipr ON ipr.pull_request_id = pr.id AND NOT ipr.reference_only
JOIN issue i ON i.id = ipr.issue_id
WHERE pr.workspace_id = $1 AND pr.state = 'merged' AND pr.merged_at >= $2
  AND ($3::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
      SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
  AND ($4::uuid IS NULL OR i.project_id = $4);

-- name: GetAnalyticsAgentPerformance :one
-- Terminal-task aggregate (B2): completed/failed within the window, windowed
-- on completed_at.
SELECT
    COUNT(*)::bigint AS terminal_count,
    COUNT(*) FILTER (WHERE atq.status = 'completed')::bigint AS completed_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::bigint AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= $2;

-- name: ListAnalyticsTaskDurations :many
-- Completed-task durations in seconds (B2/B3).
SELECT EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at))::float8 AS duration_seconds
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.status = 'completed'
  AND atq.started_at IS NOT NULL
  AND atq.completed_at >= $2;

-- name: ListAnalyticsFailureClasses :many
-- Failure-reason distribution over failed terminal tasks in the window (B2).
SELECT
    COALESCE(NULLIF(atq.failure_reason, ''), '(unclassified)')::text AS reason,
    COUNT(*)::bigint AS count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.status = 'failed'
  AND atq.completed_at >= $2
GROUP BY reason
ORDER BY count DESC;

-- name: ListAnalyticsAgentPerformance :many
-- Per-agent terminal-task performance (B3).
SELECT
    a.id AS agent_id,
    a.name AS agent_name,
    COUNT(*) FILTER (WHERE atq.status = 'completed')::bigint AS completed_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::bigint AS failed_count,
    COALESCE(
        AVG(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))
            FILTER (WHERE atq.status = 'completed' AND atq.started_at IS NOT NULL),
        0
    )::float8 AS avg_duration_seconds
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= $2
GROUP BY a.id, a.name
ORDER BY completed_count DESC
LIMIT $3;

-- name: GetAnalyticsSkillsSummary :one
-- Skill totals + window new-skill count (B4).
SELECT
    (SELECT COUNT(*)::bigint FROM skill s1 WHERE s1.workspace_id = $1) AS total_skills,
    (SELECT COUNT(*)::bigint FROM skill s2 WHERE s2.workspace_id = $1 AND s2.created_at >= $2) AS new_skills;

-- name: ListAnalyticsSkillAccumulation :many
-- New-skill accumulation per month in the viewer tz (B4).
SELECT
    TO_CHAR(s.created_at AT TIME ZONE $2::text, 'YYYY-MM') AS bucket,
    COUNT(*)::bigint AS count
FROM skill s
WHERE s.workspace_id = $1 AND s.created_at >= $3
GROUP BY bucket
ORDER BY bucket;

-- name: ListAnalyticsTopReusedSkills :many
-- Top reused skills (B4). reuse_count = completed tasks across the skill's
-- bound agents within the window (代理口径, 数据口径 §2.4.1).
SELECT
    s.id AS skill_id,
    s.name AS skill_name,
    COUNT(DISTINCT atq.id)::bigint AS reuse_count,
    COUNT(DISTINCT a.id)::bigint AS bound_agents
FROM skill s
JOIN agent_skill ags ON ags.skill_id = s.id
JOIN agent a ON a.id = ags.agent_id
LEFT JOIN agent_task_queue atq
    ON atq.agent_id = a.id AND atq.status = 'completed' AND atq.completed_at >= $2
WHERE s.workspace_id = $1
GROUP BY s.id, s.name
ORDER BY reuse_count DESC, s.name
LIMIT $3;

-- name: GetAnalyticsCollabSummary :one
-- Collaboration summary (B5): issues with both member and agent comments,
-- plus the comment total across collab issues, over issues created in window.
SELECT
    COUNT(*) FILTER (WHERE has_member AND has_agent)::bigint AS collab_issue_count,
    COUNT(*)::bigint AS total_issues,
    COALESCE(SUM(comment_total) FILTER (WHERE has_member AND has_agent), 0)::bigint AS collab_comment_total
FROM (
    SELECT i.id,
           EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND c.author_type = 'member') AS has_member,
           EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND c.author_type = 'agent') AS has_agent,
           (SELECT COUNT(*) FROM comment c WHERE c.issue_id = i.id)::bigint AS comment_total
    FROM issue i
    WHERE i.workspace_id = $1 AND i.created_at >= $2
      AND ($3::text = '' OR i.creator_type = 'member' AND i.creator_id IN (
          SELECT m.user_id FROM member m WHERE m.workspace_id = $1 AND m.department = $3))
      AND ($4::uuid IS NULL OR i.project_id = $4)
) issues;

-- name: ListAnalyticsBlockers :many
-- Issues that entered blocked state inside the window (B5/B6). blocked_at =
-- first status_changed activity with details->>'to' = 'blocked'.
SELECT
    al.issue_id AS issue_id,
    i.title AS issue_title,
    MIN(al.created_at)::timestamptz AS blocked_at
FROM activity_log al
JOIN issue i ON i.id = al.issue_id
WHERE al.workspace_id = $1
  AND al.action = 'status_changed'
  AND al.details->>'to' = 'blocked'
  AND al.created_at >= $2
GROUP BY al.issue_id, i.title
ORDER BY blocked_at DESC
LIMIT $3;

-- name: GetAnalyticsBlockerResolution :one
-- Blocker resolution instant for an issue: min of (first status_changed away
-- from blocked, first agent comment) at/after blocked_at. NULL = still open.
SELECT MIN(t)::timestamptz AS resolved_at
FROM (
    SELECT MIN(al.created_at) AS t
    FROM activity_log al
    WHERE al.issue_id = $1 AND al.action = 'status_changed'
      AND al.details->>'to' <> 'blocked' AND al.created_at >= $2
    UNION ALL
    SELECT MIN(c.created_at) AS t
    FROM comment c
    WHERE c.issue_id = $1 AND c.author_type = 'agent' AND c.created_at >= $2
) resolutions;

-- name: ListAnalyticsBlockerResolutions :many
-- Batched blocker resolutions for every issue that entered blocked state in the
-- window (replaces the per-issue GetAnalyticsBlockerResolution N+1 — P2-02).
-- Each row carries the issue's first blocked_at (the floor for its resolution)
-- and the resolved_at when one exists; resolved_at IS NULL means still open.
SELECT
    b.issue_id AS issue_id,
    b.blocked_at AS blocked_at,
    r.resolved_at AS resolved_at
FROM (
    SELECT
        al.issue_id AS issue_id,
        MIN(al.created_at)::timestamptz AS blocked_at
    FROM activity_log al
    WHERE al.workspace_id = $1
      AND al.action = 'status_changed'
      AND al.details->>'to' = 'blocked'
      AND al.created_at >= $2
    GROUP BY al.issue_id
) b
LEFT JOIN LATERAL (
    SELECT MIN(t)::timestamptz AS resolved_at
    FROM (
        SELECT MIN(al2.created_at) AS t
        FROM activity_log al2
        WHERE al2.issue_id = b.issue_id AND al2.action = 'status_changed'
          AND al2.details->>'to' <> 'blocked' AND al2.created_at >= b.blocked_at
        UNION ALL
        SELECT MIN(c.created_at) AS t
        FROM comment c
        WHERE c.issue_id = b.issue_id AND c.author_type = 'agent' AND c.created_at >= b.blocked_at
    ) resolutions
) r ON true
ORDER BY b.blocked_at DESC;

-- =============================================================
-- Tab3 Git contributions (G1-G4)
-- =============================================================

-- name: GetAnalyticsGitLastSync :one
-- Latest PR sync timestamp for the workspace (G source_status.updated_at).
SELECT MAX(pr.pr_updated_at)::timestamptz AS updated_at
FROM vcs_pull_request pr
WHERE pr.workspace_id = $1;

-- name: ListAnalyticsElocByAuthor :many
-- ELOC proxy by PR author (G1): summed additions+deletions across the
-- window's PRs, with repo list. Author attribution is applied in the handler
-- via vcs_author_mapping; unmapped authors fold into "unmapped".
SELECT
    COALESCE(pr.author_login, '') AS author,
    COUNT(*)::bigint AS pr_count,
    COALESCE(SUM(pr.additions + pr.deletions), 0)::bigint AS eloc,
    COALESCE(array_agg(DISTINCT pr.repo_owner || '/' || pr.repo_name), '{}')::text[ ] AS repos
FROM vcs_pull_request pr
WHERE pr.workspace_id = $1 AND pr.merged_at >= $2
GROUP BY pr.author_login
ORDER BY eloc DESC;

-- name: ListAnalyticsAuthorMappings :many
-- Author -> member/agent mappings for the workspace (G1 attribution).
SELECT vam.author, vam.entity_type, vam.entity_id
FROM vcs_author_mapping vam
WHERE vam.workspace_id = $1;

-- name: ListAnalyticsMemberNames :many
-- Workspace member id (user_id) + name (G1 attribution to members).
SELECT m.user_id, u.name
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1;

-- name: ListAnalyticsAgentNames :many
-- Workspace agent id + name (G1 attribution to agents).
SELECT a.id, a.name FROM agent a WHERE a.workspace_id = $1 AND a.archived_at IS NULL;

-- name: ListAnalyticsQualitySnapshots :many
-- Latest quality snapshot per repo (G2). Empty table => guide state.
-- coverage / duplication_rate are nullable NUMERIC columns; selecting them
-- raw keeps NULL semantics (a partially-populated snapshot must not turn the
-- endpoint into a 500 — P1-03). Handler skips nulls in aggregates.
SELECT DISTINCT ON (rq.repo)
    rq.repo,
    rq.coverage AS coverage,
    rq.vulnerabilities AS vulnerabilities,
    rq.duplication_rate AS duplication_rate,
    rq.snapshot_at
FROM repo_quality_snapshot rq
WHERE rq.workspace_id = $1
ORDER BY rq.repo, rq.snapshot_at DESC;

-- name: ListAnalyticsRepoActivity :many
-- Per-repo PR activity + open backlog (G3), across the workspace's PRs.
SELECT
    (pr.repo_owner || '/' || pr.repo_name)::text AS repo,
    COUNT(*) FILTER (WHERE pr.pr_created_at >= $2 OR pr.pr_updated_at >= $2)::bigint AS active_prs,
    COUNT(*) FILTER (WHERE pr.state = 'open')::bigint AS open_pr_backlog
FROM vcs_pull_request pr
WHERE pr.workspace_id = $1
GROUP BY repo
ORDER BY active_prs DESC;

-- name: ListAnalyticsRepoMerged :many
-- Merge-cycle durations per repo for PRs merged in the window (G3 MTTM).
SELECT
    (pr.repo_owner || '/' || pr.repo_name)::text AS repo,
    EXTRACT(EPOCH FROM (pr.merged_at - pr.pr_created_at))::float8 AS mtm_seconds
FROM vcs_pull_request pr
WHERE pr.workspace_id = $1 AND pr.state = 'merged' AND pr.merged_at >= $2;

-- name: ListAnalyticsPRs :many
-- PR detail list for a repo (G4).
SELECT
    pr.pr_number, pr.title, pr.state, pr.author_login, pr.pr_created_at,
    pr.merged_at, pr.closed_at, pr.additions, pr.deletions, pr.html_url
FROM vcs_pull_request pr
WHERE pr.workspace_id = $1
  AND pr.repo_owner || '/' || pr.repo_name = $2
  AND ($3::text = '' OR pr.state = $3)
ORDER BY pr.pr_created_at DESC
LIMIT $4;

-- =============================================================
-- Tab4 DORA (D1-D2)
-- =============================================================

-- name: CountAnalyticsDeployments :one
-- Existence of deployment events for the workspace (D readiness).
SELECT COUNT(*)::bigint AS count FROM deployment_event WHERE workspace_id = $1;

-- name: GetAnalyticsDeploymentLastSync :one
-- Most recent deployment finished_at for the workspace (D source_status.updated_at).
SELECT MAX(de.finished_at)::timestamptz AS updated_at
FROM deployment_event de
WHERE de.workspace_id = $1;

-- name: ListAnalyticsLeadTimeDeploy :many
-- Issue->deploy lead times for successful deployments linked to issues in
-- the window (D1 metric=deploy). Empty table => guide state.
SELECT
    DATE_TRUNC('week', de.finished_at AT TIME ZONE $2::text)::date AS week,
    EXTRACT(EPOCH FROM (de.finished_at - i.created_at))::float8 AS lead_seconds
FROM deployment_event de
CROSS JOIN LATERAL jsonb_array_elements_text(de.issue_ids) AS iid
JOIN issue i ON i.id::text = iid
WHERE de.workspace_id = $1 AND de.result = 'success' AND de.finished_at >= $3;

-- name: ListAnalyticsLeadTimeMerged :many
-- Issue->merged lead times for merged PRs in the window (D1 metric=merged).
SELECT
    DATE_TRUNC('week', pr.merged_at AT TIME ZONE $2::text)::date AS week,
    EXTRACT(EPOCH FROM (pr.merged_at - i.created_at))::float8 AS lead_seconds
FROM vcs_pull_request pr
JOIN issue_vcs_pull_request ipr ON ipr.pull_request_id = pr.id AND NOT ipr.reference_only
JOIN issue i ON i.id = ipr.issue_id
WHERE pr.workspace_id = $1 AND pr.state = 'merged' AND pr.merged_at >= $3;

-- name: GetAnalyticsDeploymentSummary :one
-- Deployment totals + failures in the window (D2). Empty table => guide.
SELECT
    COUNT(*)::bigint AS total_deployments,
    COUNT(*) FILTER (WHERE result = 'failed')::bigint AS failed_deployments
FROM deployment_event
WHERE workspace_id = $1 AND finished_at >= $2;

-- name: ListAnalyticsDeploymentTrend :many
-- Per-week deployment counts + failures (D2 trend).
SELECT
    DATE_TRUNC('week', de.finished_at AT TIME ZONE $2::text)::date AS week,
    COUNT(*)::bigint AS deployments,
    COUNT(*) FILTER (WHERE de.result = 'failed')::bigint AS failed
FROM deployment_event de
WHERE de.workspace_id = $1 AND de.finished_at >= $3
GROUP BY week
ORDER BY week;

-- name: ListAnalyticsDeploymentTrendMttr :many
-- Per-week recovered-failure MTTR (P50 of recovered_at - finished_at, seconds)
-- for the D2 trend[].mttr_seconds field. Weeks with no recovered failures are
-- simply absent; the handler leaves their mttr_seconds as null.
SELECT
    DATE_TRUNC('week', de.finished_at AT TIME ZONE $2::text)::date AS week,
    PERCENTILE_CONT(0.5) WITHIN GROUP (
        ORDER BY EXTRACT(EPOCH FROM (de.recovered_at - de.finished_at))
    )::float8 AS mttr_seconds
FROM deployment_event de
WHERE de.workspace_id = $1 AND de.result = 'failed'
  AND de.recovered_at IS NOT NULL AND de.finished_at >= $3
GROUP BY week
ORDER BY week;

-- name: ListAnalyticsDeploymentFailures :many
-- Failed deployments in the window, newest first (D2 failures detail).
SELECT
    de.deployment_id, de.app, de.finished_at AS failed_at, de.recovered_at, de.reason
FROM deployment_event de
WHERE de.workspace_id = $1 AND de.result = 'failed' AND de.finished_at >= $2
ORDER BY de.finished_at DESC
LIMIT $3;

-- =============================================================
-- Identity & departments (L1-L2)
-- =============================================================

-- name: CountAnalyticsIdentityEvents :one
-- Existence of manual identity events (L1 readiness).
SELECT COUNT(*)::bigint AS count FROM identity_import WHERE workspace_id = $1;

-- name: GetAnalyticsIdentitySummary :one
-- Lifecycle aggregates over identity events in the window (L1). Optional
-- department slice on the event's department.
SELECT
    COUNT(*) FILTER (WHERE ii.event_type = 'onboard' AND ii.result = 'success')::bigint AS onboarded_members,
    COUNT(*) FILTER (WHERE ii.event_type = 'onboard')::bigint AS new_hires_total,
    COUNT(*) FILTER (WHERE ii.event_type = 'cutoff')::bigint AS cutoff_events,
    COUNT(*) FILTER (WHERE ii.event_type = 'cutoff' AND ii.result = 'success')::bigint AS cutoff_success_count
FROM identity_import ii
WHERE ii.workspace_id = $1 AND ii.occurred_at >= $2
  AND ($3::text = '' OR ii.department = $3);

-- name: ListAnalyticsIdentityEvents :many
-- Recent identity events (L1 recent_events). Optional department slice.
SELECT ii.event_type, ii.member_name, ii.occurred_at, ii.result
FROM identity_import ii
WHERE ii.workspace_id = $1 AND ii.occurred_at >= $2
  AND ($3::text = '' OR ii.department = $3)
ORDER BY ii.occurred_at DESC
LIMIT $4;

-- name: CountAnalyticsMembersWithDepartment :one
-- Distinct departments configured for the workspace (L2 readiness).
SELECT COUNT(DISTINCT m.department)::bigint AS count
FROM member m
WHERE m.workspace_id = $1 AND m.department IS NOT NULL AND m.department <> '';

-- name: ListAnalyticsActiveMembersByDepartment :many
-- Active members per department in the window (L2 active_members).
SELECT m.department::text AS department, COUNT(DISTINCT m.user_id)::bigint AS active_members
FROM (
    SELECT m1.user_id FROM member m1 WHERE m1.workspace_id = $1
    UNION
    SELECT i.creator_id AS user_id FROM issue i WHERE i.workspace_id = $1 AND i.creator_type = 'member' AND i.created_at >= $2
    UNION
    SELECT c.author_id AS user_id FROM comment c WHERE c.workspace_id = $1 AND c.author_type = 'member' AND c.created_at >= $2
    UNION
    SELECT atq.initiator_user_id AS user_id FROM agent_task_queue atq WHERE atq.initiator_user_id IS NOT NULL AND atq.created_at >= $2
) active_members
JOIN member m ON m.user_id = active_members.user_id
WHERE m.workspace_id = $1 AND m.department IS NOT NULL AND m.department <> ''
GROUP BY m.department
ORDER BY active_members DESC;
