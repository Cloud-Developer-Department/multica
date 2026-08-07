-- Personal-dimension usage dashboard queries (CLO-206 / CLO-212).
--
-- Eight endpoints back the developer's personal dashboard page
-- (`GET /api/dashboard/personal/*`). Unlike the workspace rollups, these
-- aggregate RAW `task_usage` joined to `agent_task_queue` -- the hourly
-- rollup (`task_usage_hourly`) carries no user dimension, and local
-- deployment scale makes a raw scan cheap (CLO-206 §6).
--
-- 口径 (definitions of record -- data spec CALC-CLO-211):
--   * Attribution: a task belongs to the user who INITIATED it, via
--     `agent_task_queue.initiator_user_id`. NULL initiators (autopilot /
--     system runs) are never attributed, and never join the team ranking.
--   * Scope: terminal tasks only (`status IN ('completed','failed')`),
--     windowed on `completed_at` -- the same anchor the workspace
--     run-time / failure rollups use, so every panel on the page agrees.
--   * Tokens / cost: only the `task_usage` rows of those terminal tasks.
--     Cost is the split convention from migration 213:
--     `cost_usd_ticks` (provider-reported, 1e-10 USD) + the
--     `uncosted_*_tokens` (rows the provider did not price, to be
--     estimated client-side from the static rate table).
--   * Success rate denominator: completed + failed terminal tasks.
--     Tasks that expired in the queue without starting are still
--     `failed`, so they DO count (same as ListDashboardFailuresDaily).
--   * Run time: terminal tasks with BOTH started_at and completed_at;
--     queued-expired tasks have no duration and only count as failures.
--
-- All personal queries are scoped to the caller's workspace via the
-- `agent` join, so "我的数据" and "团队排名" stay on the same team
-- population.

-- name: GetPersonalUsageSummary :one
-- Token + cost split over the current user's terminal tasks in [since, until).
-- Returns exactly one row (all zeros when the window is empty) so the handler
-- can build a KPI payload without special-casing ErrNoRows.
SELECT
    COALESCE(SUM(tu.input_tokens), 0)::bigint       AS input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint      AS output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
    COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
JOIN task_usage tu ON tu.task_id = atq.id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz;

-- name: GetPersonalTaskSummary :one
-- Task-count / run-time / success-rate components over the current user's
-- terminal tasks in [since, until). Computed off `agent_task_queue` alone so
-- terminal tasks without a `task_usage` row still count. Run time only
-- accumulates tasks that actually started (queued-expired tasks have no
-- duration) — SUM over a NULL delta is NULL, so a window of only
-- never-started tasks yields 0.
SELECT
    COUNT(*)::bigint AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'completed')::bigint AS completed_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::bigint AS failed_count,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS runtime_seconds
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz;

-- name: ListPersonalUsageTrendDaily :many
-- Per-calendar-day (viewer tz) token + cost split over the current user's
-- terminal tasks. Powers the token/cost trend for `week` / `month` ranges.
SELECT
    DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) AS date,
    COALESCE(SUM(tu.input_tokens), 0)::bigint       AS input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint      AS output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
    COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
    COUNT(DISTINCT atq.id)::bigint AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
JOIN task_usage tu ON tu.task_id = atq.id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: ListPersonalUsageTrendHourly :many
-- Per-hour (viewer tz) token + cost split over the current user's terminal
-- tasks. Powers the token/cost trend for the `today` range. The bucket comes
-- back as a local wall-clock timestamp; the handler reinterprets it in the
-- viewer tz before emitting (same convention as monitoring.go).
SELECT
    DATE_TRUNC('hour', atq.completed_at AT TIME ZONE sqlc.arg('tz')::text)::timestamp AS bucket,
    COALESCE(SUM(tu.input_tokens), 0)::bigint       AS input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint      AS output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
    COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
    COUNT(DISTINCT atq.id)::bigint AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
JOIN task_usage tu ON tu.task_id = atq.id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: ListPersonalUsageModels :many
-- Per-(provider, model) token + cost split over the current user's terminal
-- tasks in the window. Provider rides along (LOWER-normalised) so bare model
-- ids that collide across providers stay disambiguable, matching the
-- workspace dashboard convention.
SELECT
    LOWER(tu.provider) AS provider,
    tu.model,
    COALESCE(SUM(tu.input_tokens), 0)::bigint       AS input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint      AS output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
    COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
    COUNT(DISTINCT atq.id)::bigint AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
JOIN task_usage tu ON tu.task_id = atq.id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY LOWER(tu.provider), tu.model
ORDER BY SUM(tu.input_tokens + tu.output_tokens + tu.cache_read_tokens + tu.cache_write_tokens) DESC,
         LOWER(tu.provider), tu.model;

-- name: ListPersonalTaskDurations :many
-- Single-task run-time histogram buckets (1..5) over the current user's
-- terminal tasks in the window. Tasks that never started (started_at NULL)
-- have no duration and are excluded — they still show up in the summary's
-- failure count. Bucket mapping (handled by the handler):
--   1 = <60s, 2 = 60-300s, 3 = 300-900s, 4 = 900-1800s, 5 = >1800s
SELECT
    CASE
        WHEN EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)) < 60    THEN 1
        WHEN EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)) < 300   THEN 2
        WHEN EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)) < 900   THEN 3
        WHEN EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)) < 1800  THEN 4
        ELSE 5
    END AS bucket,
    COUNT(*)::bigint AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: ListPersonalRuntimeTrendDaily :many
-- Per-calendar-day (viewer tz) total run time + task counts over the current
-- user's terminal tasks. Powers the daily/weekly total-run-time trend.
SELECT
    DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) AS date,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::bigint AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: ListPersonalRuntimeTrendHourly :many
-- Per-hour (viewer tz) total run time + task counts over the current user's
-- terminal tasks. Powers the total-run-time trend for the `today` range.
SELECT
    DATE_TRUNC('hour', atq.completed_at AT TIME ZONE sqlc.arg('tz')::text)::timestamp AS bucket,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::bigint AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: ListPersonalFailuresTrendDaily :many
-- Per-calendar-day (viewer tz) completed/failed counts over the current
-- user's terminal tasks. Feeds the failure-rate trend line (interface 6);
-- the day axis must line up with the usage / runtime trends, so the same
-- window, tz and completed_at anchor apply.
SELECT
    DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) AS date,
    COUNT(*) FILTER (WHERE atq.status = 'completed')::bigint AS completed_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::bigint AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: ListPersonalFailuresTrendHourly :many
-- Per-hour (viewer tz) completed/failed counts over the current user's
-- terminal tasks. Feeds the failure-rate trend for the `today` range.
SELECT
    DATE_TRUNC('hour', atq.completed_at AT TIME ZONE sqlc.arg('tz')::text)::timestamp AS bucket,
    COUNT(*) FILTER (WHERE atq.status = 'completed')::bigint AS completed_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::bigint AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY 1
ORDER BY 1;

-- name: ListPersonalErrors :many
-- Per-failure-reason terminal-task counts over the current user's tasks in
-- the window. Same succeeded-bucket convention as the workspace failure
-- rollups: `failure_reason = ''` carries the completed count (the
-- denominator the client needs for an error rate). Failed rows with a NULL /
-- empty reason collapse into `unclassified`.
SELECT
    CASE
        WHEN atq.status = 'failed'
            THEN COALESCE(NULLIF(atq.failure_reason, ''), 'unclassified')
        ELSE ''
    END AS failure_reason,
    COUNT(*)::bigint AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.initiator_user_id = sqlc.arg('user_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
GROUP BY 1
ORDER BY task_count DESC, failure_reason;

-- name: ListPersonalRankMembers :many
-- Every workspace initiator with ≥1 terminal task in the window and their
-- token total. The handler computes the caller's competition rank, the
-- "beat X% of members" figure, and the anonymous team average / median in
-- Go. Privacy boundary (CLO-206 §5): this row set stays server-side — the
-- wire response carries ONLY the caller's own numbers plus anonymous
-- aggregates, never another member's identity or totals.
SELECT
    atq.initiator_user_id AS user_id,
    COALESCE(SUM(tu.input_tokens + tu.output_tokens + tu.cache_read_tokens + tu.cache_write_tokens), 0)::bigint AS tokens
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN task_usage tu ON tu.task_id = atq.id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND atq.completed_at <  sqlc.arg('until')::timestamptz
  AND atq.initiator_user_id IS NOT NULL
GROUP BY atq.initiator_user_id
ORDER BY tokens DESC, atq.initiator_user_id;
