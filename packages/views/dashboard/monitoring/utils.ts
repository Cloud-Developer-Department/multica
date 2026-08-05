// Pure helpers for the data-monitoring dashboard (`/{slug}/dashboard`).
//
// The design spec (design/uiux/design-spec.md §2.1) defines a global status →
// colour mapping that deliberately differs from `STATUS_CONFIG` in
// @multica/core/issues (which maps in_review → success and done → info).
// This module is the dashboard's own mapping so every chart / tag / legend on
// the page agrees, without touching the issues surface's established colours.

import type { IssueStatus } from "@multica/core/types";
import type {
  MonitoringCommentPoint,
  MonitoringCompletionPoint,
  MonitoringProjectProgress,
  MonitoringStatusCounts,
} from "@multica/core/types";

// ---------------------------------------------------------------------------
// Status → colour mapping (design-spec §2.1, the one truth for this page)
// ---------------------------------------------------------------------------

/** Canonical display order — heaviest attention first. */
export const STATUS_DISPLAY_ORDER: IssueStatus[] = [
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "todo",
  "cancelled",
  "backlog",
];

/** Stacked-bar bottom-up order (design-spec §5.2). Backlog merges into
 *  the "other" bucket the server aggregates, so it never stacks. */
export const PROJECT_STACK_ORDER: IssueStatus[] = [
  "done",
  "in_progress",
  "in_review",
  "todo",
  "blocked",
  "cancelled",
];

/** Concrete chart colours from design-spec §2.1 — charts need real fill
 *  values, so these are the spec's hex values rather than token classes. */
export const STATUS_CHART_COLOR: Record<string, string> = {
  todo: "#64748B",
  in_progress: "#3B5BFF",
  in_review: "#8B5CF6",
  done: "#16A34A",
  blocked: "#EF4444",
  cancelled: "#9CA3AF",
  backlog: "#D1D5DB",
};

/** Status tag classes (10% tinted bg + matching text, per §2.1). */
export const STATUS_TAG_CLASS: Record<string, string> = {
  todo: "bg-muted text-muted-foreground",
  in_progress: "bg-brand/10 text-brand",
  in_review: "bg-violet/10 text-violet",
  done: "bg-success/10 text-success",
  blocked: "bg-destructive/10 text-destructive",
  cancelled: "bg-muted text-muted-foreground",
  backlog: "bg-muted text-muted-foreground",
};

export interface StatusSlice {
  status: string;
  count: number;
  color: string;
}

/** Collapse a status-count bag into ordered, non-zero slices for the donut.
 *  Unknown statuses (added by a newer backend) ride along with a neutral
 *  colour instead of being dropped. */
export function donutSlices(counts: MonitoringStatusCounts): StatusSlice[] {
  return STATUS_DISPLAY_ORDER.map((status) => ({
    status,
    count: counts[status] ?? 0,
    color: STATUS_CHART_COLOR[status] ?? "#94A3B8",
  })).filter((slice) => slice.count > 0);
}

export interface ProjectStackRow {
  id: string;
  name: string;
  total: number;
  /** Keyed by status for the stacked BarChart dataKey. */
  counts: Record<string, number>;
}

/** The "other" bucket's synthetic id. */
export const OTHER_PROJECTS_ID = "__other_projects__";

/** Top-N projects (by total issues) with the tail merged into one "Other"
 *  row so the stacked bar never runs past N columns (design-spec §5.2). */
export function bucketTopProjects(
  projects: MonitoringProjectProgress[],
  top = 8,
): ProjectStackRow[] {
  const sorted = projects
    .map((p) => ({
      id: p.id,
      name: p.name,
      total: p.total,
      counts: { ...p.status_counts },
    }))
    .toSorted((a, b) => b.total - a.total);

  const head = sorted.slice(0, top);
  const tail = sorted.slice(top);
  if (tail.length === 0) return head;

  const other: ProjectStackRow = {
    id: OTHER_PROJECTS_ID,
    name: "",
    total: 0,
    counts: {},
  };
  for (const row of tail) {
    other.total += row.total;
    for (const [status, count] of Object.entries(row.counts)) {
      other.counts[status] = (other.counts[status] ?? 0) + count;
    }
  }
  return [...head, other];
}

/** Keep the top N ranked rows; the tail is hidden behind a "show more" toggle
 *  rather than aggregated (these lists carry names, not numbers). */
export function topN<T>(rows: T[], n: number): T[] {
  return rows.slice(0, Math.max(0, n));
}

// ---------------------------------------------------------------------------
// Number / time formatting
// ---------------------------------------------------------------------------

/** 0–100 rate → "37%" (whole percent; design-spec tooltips use 占比). */
export function formatPercent(rate: number): string {
  if (!Number.isFinite(rate)) return "—";
  return `${Math.round(rate)}%`;
}

/** Signed delta vs the previous period → "+2.4%" / "−1.2%" / "+0.0%". */
export function formatDelta(delta: number | null | undefined): string {
  if (delta == null || !Number.isFinite(delta)) return "—";
  const sign = delta >= 0 ? "+" : "−";
  return `${sign}${Math.abs(delta).toFixed(1)}%`;
}

/** The "missing data" glyph used everywhere on this page. */
export const MISSING = "—";

/** HH:mm:ss for the "data updated" toolbar timestamp (design-spec §4.2). */
export function formatTimeHHMMSS(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

/** Format one comment-series bucket's ISO start into an axis label.
 *  Hourly buckets (24h range) render "HH:mm", daily buckets "MM-DD". */
export function formatCommentSeriesLabel(
  point: MonitoringCommentPoint,
  hourly: boolean,
  tz: string,
): string {
  const date = parseIsoInTz(point.time, tz);
  if (hourly) {
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  }
  return `${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

/** Format one completion-trend day bucket's ISO start into an axis label. */
export function formatCompletionSeriesLabel(
  point: MonitoringCompletionPoint,
  tz: string,
): string {
  const date = parseIsoInTz(point.time, tz);
  return `${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

/** Parse an ISO timestamp as a wall-clock time in `tz` (defensive: any
 *  unparseable value falls back to `new Date(0)` instead of crashing). */
function parseIsoInTz(iso: string, tz: string): Date {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return new Date(0);
  if (typeof Intl === "undefined") return t;
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: tz,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(t);
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((p) => p.type === type)?.value ?? "0";
  const hour = Number(part("hour")) % 24;
  return new Date(
    Number(part("year")),
    Number(part("month")) - 1,
    Number(part("day")),
    hour,
    Number(part("minute")),
  );
}
