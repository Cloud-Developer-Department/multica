// Data-monitoring dashboard wire types (CLO-166 / CLO-171).
//
// These power the workspace monitoring page at `/{slug}/dashboard`. The
// backend aggregates each module server-side (see PRD §5) and the client
// renders whatever shape arrives; every field defaults so a partial or
// older backend degrades to the page's "partial data missing" state
// instead of a white screen.

/** Status-count bag keyed by issue status id. Kept as a Record so new
 *  statuses added by the backend survive the client schema untouched. */
export type MonitoringStatusCounts = Record<string, number>;

export interface MonitoringIssueDistribution {
  /** Total issues in the window (all statuses). */
  total: number;
  /** Issue counts grouped by status id. */
  status_counts: MonitoringStatusCounts;
  /** Per-project rollups for the stacked project-progress chart. */
  projects: MonitoringProjectProgress[];
}

export interface MonitoringProjectProgress {
  id: string;
  name: string;
  total: number;
  status_counts: MonitoringStatusCounts;
}

export interface MonitoringActivityEntity {
  id: string;
  name: string;
  /** in_progress + in_review issue count ("current load"). */
  load: number;
  /** status-change + comment + run sum ("activity score"). */
  activity: number;
}

export interface MonitoringActivity {
  agent_workload: MonitoringActivityEntity[];
  team_activity: MonitoringActivityEntity[];
}

export interface MonitoringCommentPoint {
  /** ISO timestamp of the bucket's start (hour for 24h, day for 7/30d). */
  time: string;
  count: number;
}

export interface MonitoringComments {
  total: number;
  today: number;
  series: MonitoringCommentPoint[];
}

export interface MonitoringCompletionPoint {
  /** ISO timestamp of the day boundary. */
  time: string;
  /** Completion rate 0-100 for that day. */
  completion: number;
  /** Delay rate 0-100 for that day, or null when no due-date tasks. */
  delay: number | null;
}

export interface MonitoringCompletion {
  completion_rate: number;
  completion_delta: number;
  delay_rate: number | null;
  delay_delta: number | null;
  /** False when no due-date tasks exist (drives the "—" missing state). */
  has_due_date_tasks: boolean;
  trend: MonitoringCompletionPoint[];
}
