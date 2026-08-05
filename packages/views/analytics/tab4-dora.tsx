"use client";

// Tab4 — DORA 效能结果 (v2.0 D1–D2). KPI row + 交付周期趋势 (口径切换 + 自动
// 降级) + 部署频率柱状图 + 变更失败率与 MTTR + 失败明细表. Deploy-not-connected
// modules show the guide state; the lead-time chart auto-falls back to the
// Merged metric with a "替代口径" badge when `metric=deploy` is not ready
// (E16).

import { useEffect, useMemo, useState } from "react";
import { Rocket, Timer, TriangleAlert } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { NumberFlow } from "@multica/ui/components/ui/number-flow";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import {
  analyticsLeadTimeOptions,
  analyticsDeploymentsOptions,
} from "@multica/core/analytics-dashboard";
import type { AnalyticsDeployments, AnalyticsLeadTimeMetric } from "@multica/core/types";
import { useT } from "../i18n";
import { AnalyticsKpiRow } from "./components/kpi-row";
import { AnalyticsModuleCard } from "./components/module-card";
import { Segmented } from "./components/segmented";
import { SourceGuideState } from "./components/source-guide-state";
import { StatusBadge } from "./components/status-badge";
import { DeployFrequencyChart, DualLineChart } from "./charts";
import { formatDateTime, formatDurationEn, formatPercent, MISSING } from "./utils";

export interface Tab4DoraProps {
  wsId: string;
  days: number;
  tz: string;
}

type FailureFilter = "all" | "resolved" | "open";

export function Tab4Dora({ wsId, days, tz }: Tab4DoraProps) {
  const { t } = useT("analytics");
  const [requestedMetric, setRequestedMetric] = useState<AnalyticsLeadTimeMetric>("deploy");
  const [activeMetric, setActiveMetric] = useState<AnalyticsLeadTimeMetric>("deploy");
  const [degraded, setDegraded] = useState(false);

  const leadTimeQuery = useQuery(analyticsLeadTimeOptions(wsId, days, tz, activeMetric));
  const deploymentsQuery = useQuery(analyticsDeploymentsOptions(wsId, days, tz));

  const leadTime = leadTimeQuery.data;

  // Auto-degrade: a `deploy` request that isn't ready falls back to `merged`
  // and marks the card with the "替代口径" badge (E16).
  useEffect(() => {
    if (leadTimeQuery.isError) return;
    const status = leadTime?.source_status;
    if (!status || status.ready) {
      setDegraded(false);
      return;
    }
    if (requestedMetric === "deploy" && activeMetric === "deploy") {
      setActiveMetric("merged");
      setDegraded(true);
    } else if (requestedMetric === "merged" && activeMetric === "merged") {
      setDegraded(false);
    }
  }, [leadTime, leadTimeQuery.isError, requestedMetric, activeMetric]);

  const handleMetricChange = (next: AnalyticsLeadTimeMetric) => {
    setRequestedMetric(next);
    setActiveMetric(next);
    setDegraded(false);
  };

  const deployments = deploymentsQuery.data;
  const doraReady = deployments?.source_status.ready === true;
  const leadTimeReady = leadTime?.source_status.ready === true;
  const lastWeekP50 = useMemo(() => {
    const points = leadTime?.points ?? [];
    return points.length > 0 ? points[points.length - 1]?.p50_seconds ?? null : null;
  }, [leadTime?.points]);

  const kpiItems = useMemo(() => {
    const d = deploymentsQuery.data;
    const dReady = d?.source_status.ready === true;
    return [
      {
        label: t(($) => $.kpi.deploy_frequency),
        value:
          d?.deploy_frequency_weekly != null ? (
            <span className="flex items-baseline gap-1">
              <NumberFlow
                value={d.deploy_frequency_weekly}
                format={{ maximumFractionDigits: 1, minimumFractionDigits: 1 }}
              />
              <span className="text-xs text-muted-foreground">
                {t(($) => $.kpi.deploy_frequency_unit)}
              </span>
            </span>
          ) : (
            MISSING
          ),
        hint:
          dReady && d
            ? t(($) => $.kpi.deploy_frequency_hint, { count: d.total_deployments })
            : undefined,
        accent: "brand" as const,
      },
      {
        label: t(($) => $.kpi.change_failure_rate),
        value: d?.change_failure_rate != null ? (
          <span className="text-destructive">{formatPercent(d.change_failure_rate)}</span>
        ) : (
          MISSING
        ),
        hint:
          dReady && d
            ? t(($) => $.kpi.change_failure_rate_hint, {
                failed: d.failed_deployments,
                total: d.total_deployments,
              })
            : undefined,
      },
      {
        label: t(($) => $.kpi.mttr),
        value: formatDurationEn(d?.mttr_seconds),
        hint: t(($) => $.kpi.mttr_hint),
      },
      {
        label: t(($) => $.kpi.lead_time_p50),
        value: formatDurationEn(lastWeekP50),
        hint: t(($) => $.kpi.lead_time_p50_hint),
      },
    ];
  }, [deploymentsQuery.data, lastWeekP50, t]);

  const leadTimeSeries = useMemo(
    () =>
      (leadTime?.points ?? []).map((p) => ({
        label: p.week.slice(5),
        a: p.p50_seconds,
        b: p.p95_seconds,
        samples: p.sample_count,
      })),
    [leadTime?.points],
  );

  const deploySeries = useMemo(
    () =>
      (deployments?.trend ?? []).map((p) => ({
        label: p.week.slice(5),
        deployments: p.deployments,
        failed: p.failed,
      })),
    [deployments?.trend],
  );

  const failureSeries = useMemo(
    () =>
      (deployments?.trend ?? []).map((p) => ({
        label: p.week.slice(5),
        a: p.failure_rate,
        b: p.mttr_seconds,
      })),
    [deployments?.trend],
  );

  return (
    <div className="space-y-5">
      {/* 页面顶部说明条（deploy 未接入但 merged 可用时） */}
      {degraded ? (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-warning/30 bg-warning/10 px-4 py-2.5">
          <p className="text-xs text-foreground">
            {t(($) => $.guide.dora.desc)}
          </p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleMetricChange("deploy")}
          >
            {t(($) => $.common.configure)}
          </Button>
        </div>
      ) : null}

      <AnalyticsKpiRow items={kpiItems} />

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        {/* ② 交付周期趋势 */}
        <AnalyticsModuleCard
          title={t(($) => $.lead_time.title)}
          action={
            <Segmented
              value={activeMetric}
              onChange={handleMetricChange}
              options={[
                { label: t(($) => $.lead_time.metric_deploy), value: "deploy" as const },
                { label: t(($) => $.lead_time.metric_merged), value: "merged" as const },
              ]}
            />
          }
          headerNote={
            degraded ? (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <StatusBadge tone="warning">{t(($) => $.lead_time.alt_badge)}</StatusBadge>
                    }
                  />
                  <TooltipContent>{t(($) => $.lead_time.alt_tooltip)}</TooltipContent>
                </Tooltip>
              </TooltipProvider>
            ) : undefined
          }
          loading={leadTimeQuery.isPending}
          error={leadTimeQuery.isError}
          guide={
            leadTime?.source_status.ready === false && !degraded ? (
              <SourceGuideState
                icon={<Timer className="size-8 text-muted-foreground" />}
                badge={t(($) => $.guide.dora.badge)}
                title={t(($) => $.guide.dora.title)}
                description={t(($) => $.guide.dora.desc)}
                updatedAt={leadTime.source_status.updated_at}
              />
            ) : undefined
          }
          empty={leadTimeReady && leadTimeSeries.length === 0}
          emptyIcon={<Timer className="size-8 text-muted-foreground" />}
          emptyTitle={t(($) => $.lead_time.empty_title)}
          emptyDescription={t(($) => $.lead_time.empty_desc)}
          onRetry={() => void leadTimeQuery.refetch()}
        >
          {leadTimeReady ? (
            <DualLineChart
              data={leadTimeSeries}
              config={{
                a: { label: t(($) => $.lead_time.p50), color: "var(--color-chart-1)" },
                b: { label: t(($) => $.lead_time.p95), color: "var(--color-chart-4)" },
              }}
              formatValue={(v) => formatDurationEn(v)}
            />
          ) : null}
        </AnalyticsModuleCard>

        {/* ③ 部署频率趋势 */}
        <AnalyticsModuleCard
          title={t(($) => $.deploy_freq.title)}
          description={t(($) => $.deploy_freq.desc)}
          loading={deploymentsQuery.isPending}
          error={deploymentsQuery.isError}
          guide={
            deployments?.source_status.ready === false ? (
              <SourceGuideState
                icon={<Rocket className="size-8 text-muted-foreground" />}
                badge={t(($) => $.guide.dora.badge)}
                title={t(($) => $.guide.dora.title)}
                description={t(($) => $.guide.dora.desc)}
                updatedAt={deployments.source_status.updated_at}
              />
            ) : undefined
          }
          empty={doraReady && deploySeries.length === 0}
          emptyIcon={<Rocket className="size-8 text-muted-foreground" />}
          emptyTitle={t(($) => $.deploy_freq.empty_title)}
          emptyDescription={t(($) => $.deploy_freq.empty_desc)}
          onRetry={() => void deploymentsQuery.refetch()}
        >
          {doraReady ? <DeployFrequencyChart data={deploySeries} /> : null}
        </AnalyticsModuleCard>
      </div>

      {/* ④ 变更失败率与 MTTR */}
      <FailureModule
        loading={deploymentsQuery.isPending}
        error={deploymentsQuery.isError}
        onRetry={() => void deploymentsQuery.refetch()}
        data={deployments}
        ready={doraReady}
        failureSeries={failureSeries}
      />
    </div>
  );
}

function FailureModule({
  loading,
  error,
  onRetry,
  data,
  ready,
  failureSeries,
}: {
  loading: boolean;
  error: boolean;
  onRetry: () => void;
  data: AnalyticsDeployments | undefined;
  ready: boolean;
  failureSeries: { label: string; a: number | null; b: number | null }[];
}) {
  const { t } = useT("analytics");
  const [filter, setFilter] = useState<FailureFilter>("all");

  const failures = data?.failures ?? [];
  const filtered = useMemo(
    () =>
      failures.filter((f) =>
        filter === "all" ? true : filter === "resolved" ? f.status === "resolved" : f.status === "open",
      ),
    [failures, filter],
  );

  const hasDeployments = (data?.total_deployments ?? 0) > 0;
  const emptyDeploy = !hasDeployments;
  const emptyNoFailures = hasDeployments && (data?.failed_deployments ?? 0) === 0;

  const chartVisible = ready && failureSeries.length > 0;

  return (
    <AnalyticsModuleCard
      title={t(($) => $.failure.title)}
      action={
        <Segmented
          value={filter}
          onChange={setFilter}
          options={[
            { label: t(($) => $.failure.filter_all), value: "all" as const },
            { label: t(($) => $.failure.filter_resolved), value: "resolved" as const },
            { label: t(($) => $.failure.filter_open), value: "open" as const },
          ]}
        />
      }
      loading={loading}
      error={error}
      guide={
        data?.source_status.ready === false ? (
          <SourceGuideState
            icon={<Rocket className="size-8 text-muted-foreground" />}
            badge={t(($) => $.guide.dora.badge)}
            title={t(($) => $.guide.dora.title)}
            description={t(($) => $.guide.dora.desc)}
            updatedAt={data.source_status.updated_at}
          />
        ) : undefined
      }
      empty={ready && emptyDeploy}
      emptyIcon={<Rocket className="size-8 text-muted-foreground" />}
      emptyTitle={t(($) => $.failure.empty_title)}
      emptyDescription={t(($) => $.failure.empty_desc)}
      onRetry={onRetry}
    >
      {chartVisible ? (
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <div className="text-xs text-muted-foreground">{t(($) => $.failure.failure_rate)}</div>
              <div className="text-2xl font-medium tabular-nums">
                {formatPercent(data?.change_failure_rate)}
              </div>
            </div>
            <div>
              <div className="text-xs text-muted-foreground">{t(($) => $.failure.mttr)}</div>
              <div className="text-2xl font-medium tabular-nums">
                {formatDurationEn(data?.mttr_seconds)}
              </div>
            </div>
          </div>
          <DualLineChart
            data={failureSeries}
            config={{
              a: { label: t(($) => $.failure.failure_rate), color: "var(--color-chart-1)" },
              b: { label: t(($) => $.failure.mttr), color: "var(--color-chart-2)" },
            }}
            formatValue={(v, key) => (key === "a" ? `${(v * 100).toFixed(1)}%` : formatDurationEn(v))}
          />
        </div>
      ) : null}

      {emptyNoFailures ? (
        <div className="flex flex-col items-center gap-2 py-8 text-center">
          <TriangleAlert className="size-8 text-muted-foreground" />
          <p className="text-sm font-medium">{t(($) => $.failure.empty_no_failures)}</p>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.failure.empty_no_failures_desc)}
          </p>
        </div>
      ) : failures.length > 0 ? (
        <FailureDetailTable items={filtered} />
      ) : null}
    </AnalyticsModuleCard>
  );
}

function FailureDetailTable({
  items,
}: {
  items: { deployment_id: string; app: string | null; failed_at: string; recovered_at: string | null; mttr_seconds: number | null; reason: string | null; status: "resolved" | "open" }[];
}) {
  const { t } = useT("analytics");
  const [mttrDesc, setMttrDesc] = useState(true);

  const sorted = useMemo(() => {
    const copy = [...items];
    copy.sort((a, b) => {
      const av = a.mttr_seconds ?? -1;
      const bv = b.mttr_seconds ?? -1;
      return mttrDesc ? bv - av : av - bv;
    });
    return copy;
  }, [items, mttrDesc]);

  return (
    <div className="overflow-x-auto">
      <table className="w-full">
        <thead>
          <tr className="border-b text-left text-xs text-muted-foreground">
            <th className="px-3 py-2 font-medium">{t(($) => $.failure.col_id)}</th>
            <th className="px-3 py-2 font-medium">{t(($) => $.failure.col_app)}</th>
            <th className="px-3 py-2 font-medium">{t(($) => $.failure.col_failed_at)}</th>
            <th className="px-3 py-2 font-medium">{t(($) => $.failure.col_recovered_at)}</th>
            <th className="px-3 py-2 text-right font-medium">
              <button
                type="button"
                onClick={() => setMttrDesc((d) => !d)}
                className="inline-flex items-center gap-1 hover:text-foreground"
              >
                {t(($) => $.failure.col_mttr)}
                <span className="text-[10px]">{mttrDesc ? "↓" : "↑"}</span>
              </button>
            </th>
            <th className="px-3 py-2 font-medium">{t(($) => $.failure.col_reason)}</th>
            <th className="px-3 py-2 font-medium">{t(($) => $.failure.col_status)}</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((f) => (
            <tr key={f.deployment_id} className="border-b last:border-0 hover:bg-muted">
              <td className="px-3 py-2 font-mono text-xs">{f.deployment_id}</td>
              <td className="px-3 py-2 text-xs">{f.app ?? MISSING}</td>
              <td className="px-3 py-2 text-xs text-muted-foreground">
                {formatDateTime(f.failed_at)}
              </td>
              <td className="px-3 py-2 text-xs text-muted-foreground">
                {f.recovered_at ? formatDateTime(f.recovered_at) : t(($) => $.failure.ongoing)}
              </td>
              <td className="px-3 py-2 text-right text-xs tabular-nums">
                {formatDurationEn(f.mttr_seconds)}
              </td>
              <td className="max-w-[16rem] truncate px-3 py-2 text-xs">
                {f.reason ?? MISSING}
              </td>
              <td className="px-3 py-2">
                {f.status === "resolved" ? (
                  <StatusBadge tone="success">{t(($) => $.failure.status_resolved)}</StatusBadge>
                ) : (
                  <StatusBadge tone="warning">{t(($) => $.failure.status_open)}</StatusBadge>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
