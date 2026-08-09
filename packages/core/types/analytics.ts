// Engineering-analytics platform wire types (CLO-227 / CLO-228 / CLO-240).
//
// The `/{slug}/analytics` page (four tabs) is backed by the `/api/analytics/*`
// endpoints (API contract v2.0). This module mirrors the contract's fields,
// including its null/[]/0 empty-value semantics:
//
//   - `number | null` — metric uncomputable this window (no denominator /
//     no sample / data source not wired). Frontend renders `-`.
//   - `[]` — dimension simply has no data. Frontend renders the module's
//     empty-state copy.
//   - `0` — meaningful numeric zero, NOT an empty signal.
//   - `source_status.ready=false` — external data source not connected;
//     frontend renders the "data source not connected" guide state instead
//     of an empty state (API §0.7).
//
// Only A/B endpoints (Tab1/Tab2) accept `department_id`; G/D/L series carry
// a top-level `source_status` and never take a department param (§0.2).

/** External data-source readiness (API §0.7) — G/D/L endpoints only. */
export interface AnalyticsSourceStatus {
  /** Data source available. */
  ready: boolean;
  /** Human-readable reason when `ready=false`; null when available. */
  reason: string | null;
  /** Last sync/snapshot time; null when never synced. */
  updated_at: string | null;
}

// ---------------------------------------------------------------------------
// Tab1 — 活跃度与渗透 (Adoption & Activity), v1.0 A1–A5
// ---------------------------------------------------------------------------

/** A1 `GET /api/analytics/activity/summary` — activity KPI row. */
export interface AnalyticsActivitySummary {
  window: { start: string; end: string };
  total_members: number;
  active_members: number;
  active_days_avg: number | null;
  dau: number;
  mau: number;
  dau_mau_ratio: number | null;
  active_user_ratio: number | null;
  total_issues: number;
  per_capita_issue_volume: number | null;
}

/** A2 `GET /api/analytics/activity/heatmap` — day×hour matrix. */
export interface AnalyticsHeatmapDayHour {
  hour: number;
  value: number | null;
}

export interface AnalyticsHeatmapDay {
  date: string;
  hours: AnalyticsHeatmapDayHour[];
}

export interface AnalyticsActivityHeatmap {
  metric: string;
  days: AnalyticsHeatmapDay[];
  max_value: number;
}

export type AnalyticsHeatmapMetric = "activity_events" | "issue_volume" | "active_days";

/** A3 `GET /api/analytics/activity/top-members` — activity ranking. */
export interface AnalyticsTopMember {
  member_id: string;
  name: string;
  active_days: number;
  issue_count: number;
  last_active_at: string | null;
}

/** A4 `GET /api/analytics/adoption/summary` — Agent penetration summary. */
export interface AnalyticsAdoptionSummary {
  total_issues: number;
  agent_assigned_issues: number;
  assignment_ratio: number | null;
  agent_covered_issues: number;
  coverage_ratio: number | null;
}

/** A5 `GET /api/analytics/adoption/trend` — daily penetration trend. */
export interface AnalyticsAdoptionTrendPoint {
  date: string;
  total_issues: number;
  assignment_ratio: number | null;
  coverage_ratio: number | null;
}

// ---------------------------------------------------------------------------
// Tab2 — Agent 效能 (Agent Performance), v1.0 B1–B6
// ---------------------------------------------------------------------------

/** B1 `GET /api/analytics/agents/funnel` — execution funnel. */
export interface AnalyticsFunnel {
  assign_count: number;
  issue_assigned_count: number;
  execute_count: number;
  execute_ratio: number | null;
  /** `null` = VCS not connected (guide state); `number` = connected; `0` = empty. */
  merged_count: number | null;
  merged_ratio: number | null;
}

/** B2 `GET /api/analytics/agents/performance` — execution quality KPI. */
export interface AnalyticsAgentPerformance {
  terminal_count: number;
  completed_count: number;
  failed_count: number;
  success_rate: number | null;
  avg_duration_seconds: number | null;
  p50_duration_seconds: number | null;
  p95_duration_seconds: number | null;
  failure_classes: { reason: string; count: number }[];
}

/** B3 `GET /api/analytics/agents/top` — agent ranking. */
export interface AnalyticsAgentTopItem {
  agent_id: string;
  name: string;
  completed_count: number;
  failed_count: number;
  success_rate: number | null;
  avg_duration_seconds: number | null;
}

/** B4 `GET /api/analytics/skills/overview` — skill accumulation graph. */
export interface AnalyticsSkillAccumulation {
  bucket: string;
  count: number;
}

export interface AnalyticsSkillTopReused {
  skill_id: string;
  name: string;
  reuse_count: number;
  bound_agents: number;
}

export interface AnalyticsSkillsOverview {
  total_skills: number;
  new_skills: number;
  accumulation: AnalyticsSkillAccumulation[];
  top_reused: AnalyticsSkillTopReused[];
}

/** B5 `GET /api/analytics/collaboration/summary` — human/agent collaboration. */
export interface AnalyticsCollaborationSummary {
  collab_issue_count: number;
  total_issues: number;
  collab_issue_ratio: number | null;
  interaction_frequency: number | null;
  blocker_avg_seconds: number | null;
  blocker_p50_seconds: number | null;
  blocker_p95_seconds: number | null;
  blocker_open_count: number;
}

/** B6 `GET /api/analytics/collaboration/blockers` — blocker detail list. */
export interface AnalyticsBlockerItem {
  issue_id: string;
  issue_title: string;
  blocked_at: string;
  resolved_at: string | null;
  response_seconds: number | null;
  status: "resolved" | "open";
}

// ---------------------------------------------------------------------------
// Tab3 — Git 贡献 (Git Contributions), v2.0 G1–G4
// ---------------------------------------------------------------------------

export type AnalyticsElocGroupBy = "member" | "agent";

/** G1 `GET /api/analytics/git/eloc` — ELOC ranking. */
export interface AnalyticsElocItem {
  entity_id: string;
  name: string;
  eloc: number;
  ratio: number | null;
  commit_count: number;
  repos: string[];
}

export interface AnalyticsEloc {
  source_status: AnalyticsSourceStatus;
  group_by: AnalyticsElocGroupBy;
  total_eloc: number;
  human_eloc: number;
  agent_eloc: number;
  items: AnalyticsElocItem[];
}

/** G2 `GET /api/analytics/git/quality` — commit-quality radar snapshot. */
export interface AnalyticsQuality {
  source_status: AnalyticsSourceStatus;
  repo: string;
  snapshot_at: string | null;
  coverage: number | null;
  vulnerabilities: number | null;
  duplication_rate: number | null;
}

/** G3 `GET /api/analytics/git/repos` — repository activity distribution. */
export interface AnalyticsRepoActivityItem {
  repo: string;
  active_commits: number;
  active_prs: number;
  activity: number;
  open_pr_backlog: number;
  mtm_p50_seconds: number | null;
  mtm_p95_seconds: number | null;
}

export interface AnalyticsRepoActivity {
  source_status: AnalyticsSourceStatus;
  items: AnalyticsRepoActivityItem[];
}

/** G4 `GET /api/analytics/git/prs` — PR detail drill-down. */
export interface AnalyticsPrItem {
  pr_number: number;
  title: string;
  state: string;
  author_login: string | null;
  pr_created_at: string;
  merged_at: string | null;
  closed_at: string | null;
  additions: number;
  deletions: number;
  html_url: string;
}

export interface AnalyticsPrs {
  source_status: AnalyticsSourceStatus;
  items: AnalyticsPrItem[];
}

// ---------------------------------------------------------------------------
// Tab4 — DORA 效能结果, v2.0 D1–D2
// ---------------------------------------------------------------------------

export type AnalyticsLeadTimeMetric = "deploy" | "merged";

/** D1 `GET /api/analytics/dora/lead-time` — delivery-cycle trend. */
export interface AnalyticsLeadTimePoint {
  week: string;
  p50_seconds: number | null;
  p95_seconds: number | null;
  sample_count: number;
}

export interface AnalyticsLeadTime {
  source_status: AnalyticsSourceStatus;
  metric: AnalyticsLeadTimeMetric;
  points: AnalyticsLeadTimePoint[];
}

/** D2 `GET /api/analytics/dora/deployments` — deployment frequency / failure. */
export interface AnalyticsDeploymentTrendPoint {
  week: string;
  deployments: number;
  failed: number;
  failure_rate: number | null;
  mttr_seconds: number | null;
}

export interface AnalyticsDeploymentFailure {
  deployment_id: string;
  app: string | null;
  failed_at: string;
  recovered_at: string | null;
  mttr_seconds: number | null;
  reason: string | null;
  status: "resolved" | "open";
}

export interface AnalyticsDeployments {
  source_status: AnalyticsSourceStatus;
  total_deployments: number;
  failed_deployments: number;
  deploy_frequency_weekly: number | null;
  change_failure_rate: number | null;
  mttr_seconds: number | null;
  trend: AnalyticsDeploymentTrendPoint[];
  failures: AnalyticsDeploymentFailure[];
}

// ---------------------------------------------------------------------------
// Tab1 ⑥⑦⑧ — 身份与部门 (LDAP), v2.0 L1–L2
// ---------------------------------------------------------------------------

export type AnalyticsLifecycleEventType = "onboard" | "offboard" | "cutoff";

/** L1 `GET /api/analytics/identity/lifecycle` — user lifecycle summary. */
export interface AnalyticsLifecycleEvent {
  event_type: AnalyticsLifecycleEventType;
  member_name: string;
  occurred_at: string;
  result: "success" | "failed" | null;
}

export interface AnalyticsLifecycle {
  source_status: AnalyticsSourceStatus;
  onboarding_rate: number | null;
  onboarded_members: number;
  new_hires_total: number;
  cutoff_events: number;
  cutoff_success_count: number;
  cutoff_success_rate: number | null;
  recent_events: AnalyticsLifecycleEvent[];
}

/** L2 `GET /api/analytics/identity/departments` — department list & mapping. */
export interface AnalyticsDepartment {
  department_id: string;
  name: string;
  member_count: number;
  active_members: number;
}

export interface AnalyticsDepartments {
  source_status: AnalyticsSourceStatus;
  items: AnalyticsDepartment[];
}
