"use client";

// Data-monitoring dashboard (`/{slug}/dashboard`), CLO-166 stage 3.
//
// Layout (design-spec §3 / handoff §2): sticky toolbar (range / refresh /
// auto-refresh / timestamp) → 6 KPI tiles → 6 module cards in a 2-column
// grid (1 column <1280px) → full-width runtime list. Every module renders
// loading / empty / per-module error inside its own card shell so the grid
// never collapses; a full-page error replaces the grid only when every data
// module is down; "—" covers partial-missing fields; an unchanged poll cycle
// fires the "data already up to date" toast instead of a pointless re-render.
//
// Data source: `monitoring*Options` in packages/core (4 endpoints). The
// backend endpoints are implemented server-side by the Go team — this page
// degrades to per-module error states until they land.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  BarChart3,
  Clock,
  Inbox,
  RefreshCw,
  Wifi,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Switch } from "@multica/ui/components/ui/switch";
import { Empty, EmptyMedia, EmptyTitle, EmptyDescription } from "@multica/ui/components/ui/empty";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  monitoringIssueDistributionOptions,
  monitoringActivityOptions,
  monitoringCommentsOptions,
  monitoringCompletionOptions,
} from "@multica/core/monitoring";
import { runtimeListOptions, deriveRuntimeHealth } from "@multica/core/runtimes";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { RuntimeStatusListCard } from "./runtime-status-list";
import {
  issueDonutConfig,
  IssueDistributionDonut,
  DonutLegend,
  ProjectProgressStackedBar,
  CommentsAreaChart,
  CompletionTrendChart,
  TopNBarList,
  type TopNRow,
} from "./charts";
import {
  MISSING,
  OTHER_PROJECTS_ID,
  bucketTopProjects,
  donutSlices,
  formatCommentSeriesLabel,
  formatCompletionSeriesLabel,
  formatDelta,
  formatPercent,
  formatTimeHHMMSS,
  topN,
} from "./utils";

const RANGES = [
  { days: 1 },
  { days: 7 },
  { days: 30 },
] as const;

const AUTO_REFRESH_MS = 60 * 1000;
const MANUAL_THROTTLE_MS = 2000;
const TOP_N = 8;

export function MonitoringDashboardPage() {
  const wsId = useWorkspaceId();
  const tz = useViewingTimezone();
  const { t } = useT("monitoring");
  const { t: tIssues } = useT("issues");
  const wsPaths = useWorkspacePaths();

  const [days, setDays] = useState(7);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [autoPaused, setAutoPaused] = useState(false);
  const [noUpdateNotice, setNoUpdateNotice] = useState(false);

  const issueQuery = useQuery(monitoringIssueDistributionOptions(wsId, days, tz));
  const activityQuery = useQuery(monitoringActivityOptions(wsId, days, tz));
  const commentsQuery = useQuery(monitoringCommentsOptions(wsId, days, tz));
  const completionQuery = useQuery(monitoringCompletionOptions(wsId, days, tz));
  const runtimesQuery = useQuery(runtimeListOptions(wsId));

  const lastManualRef = useRef(0);
  const consecutiveFailuresRef = useRef(0);
  const pausedNotifiedRef = useRef(false);

  const statusLabel = useCallback(
    (status: string) =>
      tIssues(($) => ($.status as Record<string, string>)[status] ?? status),
    [tIssues],
  );

  // --- derived module data --------------------------------------------------

  const distribution = issueQuery.data;
  const activity = activityQuery.data;
  const comments = commentsQuery.data;
  const completion = completionQuery.data;

  const slices = useMemo(
    () => donutSlices(distribution?.status_counts ?? {}),
    [distribution],
  );
  const total = distribution?.total ?? 0;

  const projectRows = useMemo(
    () =>
      bucketTopProjects(distribution?.projects ?? [], TOP_N).map((row) =>
        row.id === OTHER_PROJECTS_ID
          ? { ...row, name: t(($) => $.module.other) }
          : row,
      ),
    [distribution, t],
  );

  const agentRows: TopNRow[] = useMemo(
    () =>
      topN(
        (activity?.agent_workload ?? []).map((a) => ({
          id: a.id,
          name: a.name,
          value: a.load,
        })),
        TOP_N,
      ),
    [activity],
  );

  const teamRows: TopNRow[] = useMemo(
    () =>
      topN(
        (activity?.team_activity ?? []).map((a) => ({
          id: a.id,
          name: a.name,
          value: a.activity,
        })),
        TOP_N,
      ),
    [activity],
  );

  const commentSeries = useMemo(
    () =>
      (comments?.series ?? []).map((point) => ({
        time: point.time,
        count: point.count,
        label: formatCommentSeriesLabel(point, days === 1, tz),
      })),
    [comments, days, tz],
  );

  const completionSeries = useMemo(
    () =>
      (completion?.trend ?? []).map((point) => ({
        time: point.time,
        completion: point.completion,
        delay: point.delay,
        label: formatCompletionSeriesLabel(point, tz),
      })),
    [completion, tz],
  );

  const runtimes = runtimesQuery.data ?? [];
  const now = useMemo(() => Date.now(), []);
  const onlineRuntimes = runtimes.filter(
    (r) => deriveRuntimeHealth(r, now) === "online",
  ).length;
  const onlineRate =
    runtimes.length > 0 ? (onlineRuntimes / runtimes.length) * 100 : null;

  // --- auto-refresh / error handling ---------------------------------------

  const moduleErrorCount = useMemo(() => {
    let n = 0;
    if (issueQuery.isError) n += 2;
    if (activityQuery.isError) n += 2;
    if (commentsQuery.isError) n += 1;
    if (completionQuery.isError) n += 1;
    return n;
  }, [
    issueQuery.isError,
    activityQuery.isError,
    commentsQuery.isError,
    completionQuery.isError,
  ]);

  // ≥3 modules failing = the backend is effectively unreachable → stop the
  // 60s loop and pin the "auto-refresh paused" badge (design-spec §4.12).
  useEffect(() => {
    if (moduleErrorCount >= 3 && !autoPaused && !pausedNotifiedRef.current) {
      pausedNotifiedRef.current = true;
      setAutoPaused(true);
      toast.warning(t(($) => $.toolbar.paused_toast));
    }
  }, [moduleErrorCount, autoPaused, t]);

  const refreshAll = useCallback(
    async (source: "manual" | "auto" | "visibility") => {
      if (source === "manual") {
        const nowTs = Date.now();
        if (nowTs - lastManualRef.current < MANUAL_THROTTLE_MS) return;
        lastManualRef.current = nowTs;
        setRefreshing(true);
      }

      const before = JSON.stringify([
        issueQuery.data,
        activityQuery.data,
        commentsQuery.data,
        completionQuery.data,
      ]);

      const results = await Promise.allSettled([
        issueQuery.refetch(),
        activityQuery.refetch(),
        commentsQuery.refetch(),
        completionQuery.refetch(),
      ]);
      const failures = results.filter((r) => r.status === "rejected").length;
      const fresh = results.map((r) =>
        r.status === "fulfilled" ? r.value?.data : undefined,
      );

      if (failures === 0) {
        consecutiveFailuresRef.current = 0;
        if (JSON.stringify(fresh) === before) {
          setNoUpdateNotice(true);
          window.setTimeout(() => setNoUpdateNotice(false), 6000);
          toast.info(
            t(($) => $.toolbar.up_to_date, {
              time: formatTimeHHMMSS(new Date()),
            }),
          );
        }
      } else {
        consecutiveFailuresRef.current += 1;
        if (source === "manual" && failures < 4) {
          toast.error(t(($) => $.toolbar.refresh_partial_failed));
        }
      }

      if (source === "manual") setRefreshing(false);
    },
    [issueQuery, activityQuery, commentsQuery, completionQuery, t],
  );

  // Visibility: pause the loop while hidden, refresh immediately on return.
  useEffect(() => {
    const onVisibility = () => {
      if (document.visibilityState === "visible") void refreshAll("visibility");
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, [refreshAll]);

  useEffect(() => {
    if (!autoRefresh || autoPaused) return;
    const id = window.setInterval(() => {
      if (document.visibilityState === "visible") void refreshAll("auto");
    }, AUTO_REFRESH_MS);
    return () => window.clearInterval(id);
  }, [autoRefresh, autoPaused, refreshAll]);

  const handleAutoRefreshChange = (checked: boolean) => {
    setAutoRefresh(checked);
    if (checked) {
      pausedNotifiedRef.current = false;
      setAutoPaused(false);
      toast.info(t(($) => $.toolbar.enabled));
    } else {
      toast.info(t(($) => $.toolbar.disabled));
    }
  };

  const handleRangeChange = (next: number) => {
    if (next === days) return;
    setDays(next);
  };

  const latestDataTime = Math.max(
    issueQuery.dataUpdatedAt,
    activityQuery.dataUpdatedAt,
    commentsQuery.dataUpdatedAt,
    completionQuery.dataUpdatedAt,
    0,
  );

  // --- full-page error ------------------------------------------------------

  const allModulesError =
    issueQuery.isError &&
    activityQuery.isError &&
    commentsQuery.isError &&
    completionQuery.isError;

  const commentsConfig = useMemo(
    () => ({
      count: {
        label: t(($) => $.module.comments_series),
        color: "var(--brand)",
      },
    }),
    [t],
  );

  const trendConfig = useMemo(
    () => ({
      completion: {
        label: t(($) => $.module.completion_series),
        color: "var(--success)",
      },
      delay: {
        label: t(($) => $.module.delay_series),
        color: "var(--destructive)",
      },
    }),
    [t],
  );

  const statusConfig = useMemo(() => issueDonutConfig(statusLabel), [statusLabel]);

  const projectConfig = statusConfig;

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="h-auto min-h-12 flex-wrap justify-between gap-y-1.5 px-5 py-1.5 sm:py-0">
        <div className="flex min-w-0 items-center gap-2">
          <BarChart3 className="h-4 w-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-medium">{t(($) => $.page.title)}</h1>
        </div>
      </PageHeader>

      {/* Toolbar — sticky under the shell header (design-spec §4.2). */}
      <div className="sticky top-0 z-20 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b bg-background/95 px-5 py-2.5 backdrop-blur">
        <Segmented
          value={days}
          onChange={handleRangeChange}
          options={RANGES.map((r) => ({
            label: t(($) => $.range[`${r.days}`]),
            value: r.days,
          }))}
        />

        <div className="flex flex-wrap items-center gap-3">
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Clock className="h-3.5 w-3.5" />
            {t(($) => $.toolbar.data_updated)}
            <span className="tabular-nums">
              {latestDataTime > 0
                ? formatTimeHHMMSS(new Date(latestDataTime))
                : "--:--:--"}
            </span>
            {noUpdateNotice && (
              <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
                {t(($) => $.toolbar.no_update)}
              </span>
            )}
          </span>

          <label className="flex cursor-pointer items-center gap-1.5 text-xs text-muted-foreground">
            <Switch
              size="sm"
              checked={autoRefresh}
              onCheckedChange={(checked) => handleAutoRefreshChange(checked)}
            />
            {t(($) => $.toolbar.auto_refresh)}
          </label>

          <Button
            size="sm"
            variant="default"
            disabled={refreshing}
            onClick={() => void refreshAll("manual")}
          >
            {refreshing ? (
              <RefreshCw className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" />
            )}
            {refreshing
              ? t(($) => $.toolbar.refreshing)
              : t(($) => $.toolbar.refresh)}
          </Button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[1440px] space-y-4 p-4 sm:p-6">
          {allModulesError ? (
            <PageError
              onReload={() => void refreshAll("manual")}
              reloading={refreshing}
            />
          ) : (
            <>
              {/* KPI tiles — 2 / 3 / 6 columns by breakpoint (§3.2). */}
              <section
                aria-label={t(($) => $.kpi.section)}
                className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6"
              >
                <KpiTile
                  label={t(($) => $.kpi.total_label)}
                  value={issueQuery.isError ? MISSING : issueQuery.isPending ? "--" : total.toLocaleString()}
                />
                <KpiTile
                  label={t(($) => $.kpi.in_progress_label)}
                  value={
                    issueQuery.isError
                      ? MISSING
                      : issueQuery.isPending
                        ? "--"
                        : (distribution?.status_counts.in_progress ?? 0).toLocaleString()
                  }
                  sub={t(($) => $.kpi.in_progress_sub, {
                    pct: formatPercent(
                      total > 0 ? ((distribution?.status_counts.in_progress ?? 0) / total) * 100 : 0,
                    ),
                  })}
                />
                <KpiTile
                  label={t(($) => $.kpi.completion_label)}
                  value={
                    completionQuery.isError || completion == null
                      ? MISSING
                      : completionQuery.isPending
                        ? "--"
                        : formatPercent(completion.completion_rate)
                  }
                  sub={
                    completion == null
                      ? undefined
                      : formatDelta(completion.completion_delta)
                  }
                />
                <KpiTile
                  label={t(($) => $.kpi.delay_label)}
                  value={
                    completion == null || completion.delay_rate == null
                      ? MISSING
                      : formatPercent(completion.delay_rate)
                  }
                  sub={
                    completion == null || completion.delay_delta == null
                      ? completion?.has_due_date_tasks === false
                        ? t(($) => $.kpi.delay_no_data)
                        : undefined
                      : formatDelta(completion.delay_delta)
                  }
                />
                <KpiTile
                  label={t(($) => $.kpi.online_runtime_label)}
                  value={
                    runtimesQuery.isError
                      ? MISSING
                      : runtimesQuery.isPending
                        ? "--"
                        : String(onlineRuntimes)
                  }
                  sub={
                    runtimes.length > 0
                      ? t(($) => $.kpi.online_runtime_sub, { total: runtimes.length })
                      : undefined
                  }
                  accentDot="bg-success"
                />
                <KpiTile
                  label={t(($) => $.kpi.online_rate_label)}
                  value={
                    runtimesQuery.isError || onlineRate == null
                      ? MISSING
                      : runtimesQuery.isPending
                        ? "--"
                        : formatPercent(onlineRate)
                  }
                  valueTone={onlineRateTone(onlineRate)}
                  sub={
                    runtimes.length > 0
                      ? t(($) => $.kpi.online_rate_sub, {
                          online: onlineRuntimes,
                          total: runtimes.length,
                        })
                      : undefined
                  }
                />
              </section>

              {/* Module grid — 1 column <1280, 2 columns above (§5 / handoff). */}
              <section
                aria-label={t(($) => $.module.section)}
                className="grid grid-cols-1 gap-4 xl:grid-cols-2"
              >
                <ModuleCard
                  title={t(($) => $.module.issue_distribution_title)}
                  paused={!autoRefresh || autoPaused}
                  error={issueQuery.isError}
                  loading={issueQuery.isPending}
                  empty={slices.length === 0}
                  emptyIcon={<Inbox className="h-5 w-5" />}
                  emptyTitle={t(($) => $.module.issue_distribution_empty)}
                  emptyDescription={t(($) => $.module.issue_distribution_empty_desc)}
                  onRetry={() => void issueQuery.refetch()}
                >
                  <IssueDistributionDonut slices={slices} total={total} config={statusConfig} />
                  <DonutLegend slices={slices} total={total} statusLabel={statusLabel} />
                </ModuleCard>

                <ModuleCard
                  title={t(($) => $.module.project_progress_title)}
                  paused={!autoRefresh || autoPaused}
                  error={issueQuery.isError}
                  loading={issueQuery.isPending}
                  empty={projectRows.length === 0}
                  emptyIcon={<BarChart3 className="h-5 w-5" />}
                  emptyTitle={t(($) => $.module.project_progress_empty)}
                  emptyDescription={t(($) => $.module.project_progress_empty_desc)}
                  onRetry={() => void issueQuery.refetch()}
                >
                  <ProjectProgressStackedBar rows={projectRows} config={projectConfig} />
                </ModuleCard>

                <ModuleCard
                  title={t(($) => $.module.agent_workload_title)}
                  paused={!autoRefresh || autoPaused}
                  error={activityQuery.isError}
                  loading={activityQuery.isPending}
                  empty={agentRows.length === 0}
                  emptyIcon={<Wifi className="h-5 w-5" />}
                  emptyTitle={t(($) => $.module.agent_workload_empty)}
                  onRetry={() => void activityQuery.refetch()}
                >
                  <TopNBarList
                    rows={agentRows}
                    maxValue={Math.max(...agentRows.map((r) => r.value), 0)}
                    color="#3B5BFF"
                    hrefFor={(id) => wsPaths.agentDetail(id)}
                    label={t(($) => $.module.agent_workload_title)}
                  />
                </ModuleCard>

                <ModuleCard
                  title={t(($) => $.module.team_activity_title)}
                  paused={!autoRefresh || autoPaused}
                  error={activityQuery.isError}
                  loading={activityQuery.isPending}
                  empty={teamRows.length === 0}
                  emptyIcon={<Wifi className="h-5 w-5" />}
                  emptyTitle={t(($) => $.module.team_activity_empty)}
                  onRetry={() => void activityQuery.refetch()}
                >
                  <TopNBarList
                    rows={teamRows}
                    maxValue={Math.max(...teamRows.map((r) => r.value), 0)}
                    color="#06B6D4"
                    hrefFor={(id) => wsPaths.squadDetail(id)}
                    label={t(($) => $.module.team_activity_title)}
                  />
                </ModuleCard>

                <ModuleCard
                  title={t(($) => $.module.comments_title)}
                  subtitle={
                    comments != null
                      ? `${t(($) => $.module.comments_total, { count: comments.total })} · ${t(($) => $.module.comments_today, { count: comments.today })}`
                      : undefined
                  }
                  paused={!autoRefresh || autoPaused}
                  error={commentsQuery.isError}
                  loading={commentsQuery.isPending}
                  empty={commentSeries.length === 0}
                  emptyIcon={<Inbox className="h-5 w-5" />}
                  emptyTitle={t(($) => $.module.comments_empty)}
                  emptyDescription={t(($) => $.module.comments_empty_desc)}
                  onRetry={() => void commentsQuery.refetch()}
                >
                  <CommentsAreaChart data={commentSeries} config={commentsConfig} />
                </ModuleCard>

                <ModuleCard
                  title={t(($) => $.module.completion_title)}
                  paused={!autoRefresh || autoPaused}
                  error={completionQuery.isError}
                  loading={completionQuery.isPending}
                  empty={completionSeries.length === 0}
                  emptyIcon={<BarChart3 className="h-5 w-5" />}
                  emptyTitle={t(($) => $.module.completion_empty)}
                  emptyDescription={t(($) => $.module.completion_empty_desc)}
                  onRetry={() => void completionQuery.refetch()}
                >
                  <CompletionTrendChart data={completionSeries} config={trendConfig} />
                </ModuleCard>
              </section>

              {/* Runtime status — full-width table (§4.8). */}
              <RuntimeStatusListCard wsId={wsId} />
            </>
          )}
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Local building blocks
// ---------------------------------------------------------------------------

function Segmented<T extends string | number>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly { label: string; value: T }[];
}) {
  return (
    <div className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          onClick={() => onChange(o.value)}
          aria-pressed={o.value === value}
          className={`rounded-sm px-2.5 py-1 text-xs font-medium transition-colors ${
            o.value === value
              ? "bg-brand text-white shadow-sm"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

function onlineRateTone(rate: number | null): string | undefined {
  if (rate == null) return undefined;
  if (rate >= 98) return "text-success";
  if (rate >= 90) return "text-warning";
  return "text-destructive";
}

function KpiTile({
  label,
  value,
  sub,
  valueTone,
  accentDot,
}: {
  label: string;
  value: string;
  sub?: string;
  valueTone?: string;
  accentDot?: string;
}) {
  return (
    <div className="flex flex-col justify-between gap-2 rounded-lg border bg-card p-4 shadow-sm transition-shadow hover:shadow-md">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {accentDot && <span className={`h-1.5 w-1.5 rounded-full ${accentDot}`} />}
        {label}
      </div>
      <div
        className={`text-2xl font-bold tabular-nums ${valueTone ?? ""} ${
          value === "--" || value === MISSING ? "text-muted-foreground" : ""
        }`}
      >
        {value}
      </div>
      {sub ? <div className="text-xs text-muted-foreground">{sub}</div> : null}
    </div>
  );
}

function ModuleCard({
  title,
  subtitle,
  paused,
  error,
  loading,
  empty,
  emptyIcon,
  emptyTitle,
  emptyDescription,
  onRetry,
  children,
}: {
  title: string;
  subtitle?: string;
  paused: boolean;
  error: boolean;
  loading: boolean;
  empty: boolean;
  emptyIcon: React.ReactNode;
  emptyTitle: string;
  emptyDescription?: string;
  onRetry: () => void;
  children: React.ReactNode;
}) {
  const { t } = useT("monitoring");
  return (
    <div className="rounded-lg border bg-card p-5 shadow-sm">
      <div className="mb-4 flex min-h-6 items-start justify-between gap-3">
        <div>
          <h3 className="text-base font-semibold">{title}</h3>
          {subtitle && (
            <p className="mt-0.5 text-xs text-muted-foreground">{subtitle}</p>
          )}
        </div>
        {paused && (
          <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
            {t(($) => $.toolbar.auto_refresh_paused_badge)}
          </span>
        )}
      </div>

      {error ? (
        <div className="flex flex-col items-center justify-center gap-3 py-8 text-center">
          <AlertCircle className="h-6 w-6 text-destructive" />
          <p className="text-sm font-medium">{t(($) => $.error.module_title)}</p>
          <Button size="sm" variant="outline" onClick={onRetry}>
            {t(($) => $.error.retry)}
          </Button>
        </div>
      ) : loading ? (
        <ModuleSkeleton />
      ) : empty ? (
        <Empty className="py-6">
          <EmptyMedia variant="icon">{emptyIcon}</EmptyMedia>
          <EmptyTitle className="text-sm font-medium">{emptyTitle}</EmptyTitle>
          {emptyDescription && (
            <EmptyDescription className="text-xs">{emptyDescription}</EmptyDescription>
          )}
        </Empty>
      ) : (
        children
      )}
    </div>
  );
}

function ModuleSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-[220px] w-full rounded-lg" />
      <div className="flex items-center justify-center gap-2">
        <Skeleton className="h-2 w-2 rounded-full" />
        <Skeleton className="h-2 w-2 rounded-full" />
        <Skeleton className="h-2 w-2 rounded-full" />
      </div>
    </div>
  );
}

function PageError({
  onReload,
  reloading,
}: {
  onReload: () => void;
  reloading: boolean;
}) {
  const { t } = useT("monitoring");
  return (
    <div className="flex flex-col items-center justify-center gap-4 rounded-lg border bg-card py-20 text-center">
      <AlertCircle className="h-10 w-10 text-destructive" />
      <div>
        <p className="text-base font-semibold">{t(($) => $.error.page_title)}</p>
        <p className="mt-1 text-xs text-muted-foreground">{t(($) => $.error.page_desc)}</p>
      </div>
      <Button variant="outline" onClick={onReload} disabled={reloading}>
        {reloading ? (
          <RefreshCw className="h-3.5 w-3.5 animate-spin" />
        ) : (
          <RefreshCw className="h-3.5 w-3.5" />
        )}
        {t(($) => $.error.reload)}
      </Button>
    </div>
  );
}
