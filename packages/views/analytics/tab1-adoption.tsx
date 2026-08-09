"use client";

// Tab1 — 活跃度与渗透 (Adoption & Activity). Five v1.0 modules (①–⑤) plus the
// stage-8 conditional blocks ⑥⑦⑧ (lifecycle / cutoff success / department
// slicer). ⑥⑦⑧ render only when L2 `source_status.ready=true` (E18); when
// LDAP is not connected they show the guide state and the department slicer
// hides. `departmentId` re-queries A1–A5 (and L1) with the department slice.

import { useMemo, useState } from "react";
import {
  Building2,
  CalendarRange,
  ChartLine,
  ShieldCheck,
  UserRoundPlus,
  Users,
  UsersRound,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { NumberFlow } from "@multica/ui/components/ui/number-flow";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  analyticsActivitySummaryOptions,
  analyticsHeatmapOptions,
  analyticsTopMembersOptions,
  analyticsAdoptionSummaryOptions,
  analyticsAdoptionTrendOptions,
  analyticsLifecycleOptions,
  analyticsDepartmentsOptions,
} from "@multica/core/analytics-dashboard";
import type { AnalyticsHeatmapMetric, AnalyticsLifecycle, AnalyticsLifecycleEvent } from "@multica/core/types";
import { useT } from "../i18n";
import { AppLink } from "../navigation";
import { ActorAvatar } from "../common/actor-avatar";
import { AnalyticsKpiRow } from "./components/kpi-row";
import { AnalyticsModuleCard } from "./components/module-card";
import { Segmented } from "./components/segmented";
import { SourceGuideState } from "./components/source-guide-state";
import { StatusBadge } from "./components/status-badge";
import { TokenProgress } from "./components/token-progress";
import { AnalyticsHeatmap, DualLineChart } from "./charts";
import { formatDateTime, formatPercent, formatPercentInt, MISSING } from "./utils";

const HEATMAP_METRICS: readonly { labelKey: "metric_issue_volume" | "metric_active_days" | "metric_activity_events"; value: AnalyticsHeatmapMetric }[] = [
  { labelKey: "metric_issue_volume", value: "issue_volume" },
  { labelKey: "metric_active_days", value: "active_days" },
  { labelKey: "metric_activity_events", value: "activity_events" },
];

export interface Tab1AdoptionProps {
  wsId: string;
  days: number;
  tz: string;
  departmentId: string | null;
  departmentLabel: string | null;
  departmentsReady: boolean;
  onDepartmentChange?: (departmentId: string) => void;
}

export function Tab1Adoption({
  wsId,
  days,
  tz,
  departmentId,
  departmentLabel,
  departmentsReady,
  onDepartmentChange,
}: Tab1AdoptionProps) {
  const { t } = useT("analytics");
  const [heatmapMetric, setHeatmapMetric] = useState<AnalyticsHeatmapMetric>("activity_events");

  const summaryQuery = useQuery(analyticsActivitySummaryOptions(wsId, days, tz, departmentId));
  const heatmapQuery = useQuery(analyticsHeatmapOptions(wsId, days, tz, departmentId, heatmapMetric));
  const topMembersQuery = useQuery(analyticsTopMembersOptions(wsId, days, tz, departmentId));
  const adoptionQuery = useQuery(analyticsAdoptionSummaryOptions(wsId, days, tz, departmentId));
  const adoptionTrendQuery = useQuery(analyticsAdoptionTrendOptions(wsId, days, tz, departmentId));
  const lifecycleQuery = useQuery(analyticsLifecycleOptions(wsId, days, tz, departmentId));
  const departmentsQuery = useQuery(analyticsDepartmentsOptions(wsId, tz));

  const adoption = adoptionQuery.data;
  const lifecycle = lifecycleQuery.data;
  const departments = departmentsQuery.data;

  const deptSuffix = departmentLabel
    ? t(($) => $.page.by_department, { dept: departmentLabel })
    : null;

  const kpiItems = useMemo(() => {
    const s = summaryQuery.data;
    return [
      {
        label: t(($) => $.kpi.active_user_ratio),
        value: formatPercent(s?.dau_mau_ratio),
        hint:
          s?.dau != null || s?.mau != null
            ? t(($) => $.kpi.active_user_ratio_hint, {
                dau: s?.dau ?? 0,
                mau: s?.mau ?? 0,
              })
            : undefined,
        accent: "brand" as const,
      },
      {
        label: t(($) => $.kpi.active_members),
        value: s ? (
          <NumberFlow value={s.active_members} format={{ maximumFractionDigits: 0 }} />
        ) : MISSING,
        hint:
          s != null
            ? t(($) => $.kpi.active_members_hint, { total: s.total_members })
            : undefined,
      },
      {
        label: t(($) => $.kpi.per_capita_issue),
        value:
          s?.per_capita_issue_volume != null ? (
            <NumberFlow
              value={s.per_capita_issue_volume}
              format={{ maximumFractionDigits: 1, minimumFractionDigits: 1 }}
            />
          ) : (
            MISSING
          ),
        hint:
          s != null
            ? t(($) => $.kpi.per_capita_issue_hint, { total: s.total_issues })
            : undefined,
      },
      {
        label: t(($) => $.kpi.agent_task_ratio),
        value: formatPercentInt(adoption?.assignment_ratio),
        hint:
          adoption != null
            ? t(($) => $.kpi.agent_task_ratio_hint, {
                assigned: adoption.agent_assigned_issues,
                total: adoption.total_issues,
              })
            : undefined,
      },
    ];
  }, [summaryQuery.data, adoption, t]);

  const trendSeries = useMemo(
    () =>
      (adoptionTrendQuery.data ?? []).map((p) => ({
        label: p.date.slice(5),
        a: p.assignment_ratio,
        b: p.coverage_ratio,
      })),
    [adoptionTrendQuery.data],
  );

  return (
    <div className="space-y-5">
      <AnalyticsKpiRow
        items={kpiItems}
        className={summaryQuery.isError || summaryQuery.isPending ? "opacity-60" : undefined}
      />

      {/* ② 活跃热力图 */}
      <AnalyticsModuleCard
        title={t(($) => $.heatmap.title)}
        description={deptSuffix ?? undefined}
        action={
          <Segmented
            value={heatmapMetric}
            onChange={setHeatmapMetric}
            options={HEATMAP_METRICS.map((m) => ({
              label: t(($) => $.heatmap[m.labelKey]),
              value: m.value,
            }))}
          />
        }
        loading={heatmapQuery.isPending}
        error={heatmapQuery.isError}
        empty={(heatmapQuery.data?.days.length ?? 0) === 0}
        emptyIcon={<CalendarRange className="size-8 text-muted-foreground" />}
        emptyTitle={t(($) => $.heatmap.empty_title)}
        emptyDescription={t(($) => $.heatmap.empty_desc)}
        onRetry={() => void heatmapQuery.refetch()}
      >
        <AnalyticsHeatmap
          days={heatmapQuery.data?.days ?? []}
          maxValue={heatmapQuery.data?.max_value ?? 0}
          metricLabel={(date, hour, value) =>
            value == null ? `${date} ${hour}:00 · 0` : `${date} ${hour}:00 · ${value}`
          }
        />
      </AnalyticsModuleCard>

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        {/* ③ 成员活跃排行 */}
        <AnalyticsModuleCard
          title={t(($) => $.top_members.title)}
          description={t(($) => $.top_members.desc)}
          loading={topMembersQuery.isPending}
          error={topMembersQuery.isError}
          empty={(topMembersQuery.data?.length ?? 0) === 0}
          emptyIcon={<Users className="size-8 text-muted-foreground" />}
          emptyTitle={t(($) => $.top_members.empty_title)}
          emptyDescription={t(($) => $.top_members.empty_desc)}
          onRetry={() => void topMembersQuery.refetch()}
        >
          <TopMembersList items={topMembersQuery.data ?? []} />
        </AnalyticsModuleCard>

        {/* ④ Agent 渗透率卡 */}
        <AnalyticsModuleCard
          title={t(($) => $.adoption.title)}
          action={
            <Badge variant="secondary">{t(($) => $.adoption.global_badge)}</Badge>
          }
          loading={adoptionQuery.isPending}
          error={adoptionQuery.isError}
          empty={(adoption?.total_issues ?? 0) === 0}
          emptyDescription={t(($) => $.adoption.empty_desc)}
          onRetry={() => void adoptionQuery.refetch()}
        >
          <AdoptionSummaryCard
            assignmentRatio={adoption?.assignment_ratio ?? null}
            coverageRatio={adoption?.coverage_ratio ?? null}
          />
        </AnalyticsModuleCard>
      </div>

      {/* ⑤ Agent 渗透率趋势 */}
      <AnalyticsModuleCard
        title={t(($) => $.adoption_trend.title)}
        description={t(($) => $.adoption_trend.desc)}
        loading={adoptionTrendQuery.isPending}
        error={adoptionTrendQuery.isError}
        empty={trendSeries.length === 0}
        emptyIcon={<ChartLine className="size-8 text-muted-foreground" />}
        emptyTitle={t(($) => $.adoption_trend.empty_title)}
        emptyDescription={t(($) => $.adoption_trend.empty_desc)}
        onRetry={() => void adoptionTrendQuery.refetch()}
      >
        <DualLineChart
          data={trendSeries}
          config={{
            a: { label: t(($) => $.adoption_trend.assignment), color: "var(--color-chart-1)" },
            b: { label: t(($) => $.adoption_trend.coverage), color: "var(--color-chart-2)" },
          }}
          formatValue={(v) => `${(v * 100).toFixed(0)}%`}
        />
      </AnalyticsModuleCard>

      {/* ⑥⑦⑧ — LDAP 条件区块（⑥⑦ always render; ⑧ only when departments ready） */}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <LifecycleCard
          loading={lifecycleQuery.isPending}
          error={lifecycleQuery.isError}
          onRetry={() => void lifecycleQuery.refetch()}
          data={lifecycle}
        />
        <CutoffSuccessCard
          loading={lifecycleQuery.isPending}
          error={lifecycleQuery.isError}
          onRetry={() => void lifecycleQuery.refetch()}
          data={lifecycle}
        />
      </div>
      {departmentsReady ? (
        <DepartmentSlicer
          items={departments?.items ?? []}
          value={departmentId ?? ""}
          onChange={onDepartmentChange}
        />
      ) : null}
    </div>
  );
}

function TopMembersList({
  items,
}: {
  items: { member_id: string; name: string; active_days: number; issue_count: number; last_active_at: string | null }[];
}) {
  const { t } = useT("analytics");
  const wsPaths = useWorkspacePaths();
  return (
    <div>
      <div className="grid grid-cols-[minmax(0,1.6fr)_auto_auto_auto] items-center gap-3 border-b px-4 py-2 text-xs font-medium text-muted-foreground">
        <span />
        <span className="tabular-nums">{t(($) => $.top_members.header_active_days)}</span>
        <span className="tabular-nums">{t(($) => $.top_members.header_issue)}</span>
        <span className="tabular-nums">{t(($) => $.top_members.header_last_active)}</span>
      </div>
      <div className="divide-y">
        {items.map((m) => (
          <div
            key={m.member_id}
            className="grid grid-cols-[minmax(0,1.6fr)_auto_auto_auto] items-center gap-3 px-4 py-2 hover:bg-muted"
          >
            <div className="flex min-w-0 items-center gap-2">
              <ActorAvatar actorType="member" actorId={m.member_id} size="sm" />
              <AppLink
                href={wsPaths.memberDetail(m.member_id)}
                newTabTitle={m.name}
                className="truncate text-sm font-medium hover:underline"
              >
                {m.name}
              </AppLink>
            </div>
            <span className="text-xs tabular-nums">{m.active_days}</span>
            <span className="text-xs tabular-nums">{m.issue_count}</span>
            <span className="text-xs tabular-nums text-muted-foreground">
              {m.last_active_at ? formatDateTime(m.last_active_at) : MISSING}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function AdoptionSummaryCard({
  assignmentRatio,
  coverageRatio,
}: {
  assignmentRatio: number | null;
  coverageRatio: number | null;
}) {
  const { t } = useT("analytics");
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <div className="flex items-baseline justify-between">
          <span className="text-xs text-muted-foreground">
            {t(($) => $.adoption.assignment_label)}
          </span>
          <span className="text-2xl font-medium tabular-nums">
            {formatPercentInt(assignmentRatio)}
          </span>
        </div>
        <TokenProgress
          value={assignmentRatio != null ? assignmentRatio * 100 : 0}
          indicatorClassName="bg-brand"
        />
        <p className="text-xs text-muted-foreground">
          {t(($) => $.adoption.assignment_desc)}
        </p>
      </div>
      <div className="space-y-2">
        <div className="flex items-baseline justify-between">
          <span className="text-xs text-muted-foreground">
            {t(($) => $.adoption.coverage_label)}
          </span>
          <span className="text-2xl font-medium tabular-nums">
            {formatPercentInt(coverageRatio)}
          </span>
        </div>
        <TokenProgress
          value={coverageRatio != null ? coverageRatio * 100 : 0}
          indicatorClassName="bg-chart-2"
        />
        <p className="text-xs text-muted-foreground">
          {t(($) => $.adoption.coverage_desc)}
        </p>
      </div>
    </div>
  );
}

function LifecycleCard({
  loading,
  error,
  onRetry,
  data,
}: {
  loading: boolean;
  error: boolean;
  onRetry: () => void;
  data: AnalyticsLifecycle | undefined;
}) {
  const { t } = useT("analytics");
  const [detailOpen, setDetailOpen] = useState(false);
  if (data?.source_status.ready === false) {
    return (
      <AnalyticsModuleCard
        title={t(($) => $.lifecycle.title)}
        action={<Badge variant="secondary">{t(($) => $.lifecycle.badge)}</Badge>}
        loading={false}
        error={false}
        empty={false}
        guide={
          <SourceGuideState
            icon={<UsersRound className="size-8 text-muted-foreground" />}
            badge={t(($) => $.guide.ldap.badge)}
            title={t(($) => $.guide.ldap.title)}
            description={t(($) => $.guide.ldap.desc)}
          />
        }
      />
    );
  }
  const events = data?.recent_events ?? [];
  return (
    <AnalyticsModuleCard
      title={t(($) => $.lifecycle.title)}
      action={
        <>
          {events.length > 0 ? (
            <Button variant="ghost" size="sm" onClick={() => setDetailOpen(true)}>
              {t(($) => $.lifecycle.view_detail)}
            </Button>
          ) : null}
          <Badge variant="secondary">{t(($) => $.lifecycle.badge)}</Badge>
        </>
      }
      loading={loading}
      error={error}
      empty={events.length === 0}
      emptyIcon={<UserRoundPlus className="size-8 text-muted-foreground" />}
      emptyTitle={t(($) => $.lifecycle.empty_title)}
      emptyDescription={t(($) => $.lifecycle.empty_desc)}
      onRetry={onRetry}
    >
      <LifecycleBody data={data} />
      <LifecycleDetailDialog
        open={detailOpen}
        onOpenChange={setDetailOpen}
        items={events}
      />
    </AnalyticsModuleCard>
  );
}

function LifecycleDetailDialog({
  open,
  onOpenChange,
  items,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: AnalyticsLifecycleEvent[];
}) {
  const { t } = useT("analytics");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.lifecycle.dialog_title)}</DialogTitle>
        </DialogHeader>
        {items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <UserRoundPlus className="size-8 text-muted-foreground" />
            <p className="text-sm font-medium">{t(($) => $.lifecycle.empty_title)}</p>
          </div>
        ) : (
          <div className="max-h-[60vh] overflow-y-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b text-left text-xs text-muted-foreground">
                  <th className="px-3 py-2 font-medium">{t(($) => $.lifecycle.col_member)}</th>
                  <th className="px-3 py-2 font-medium">{t(($) => $.lifecycle.col_type)}</th>
                  <th className="px-3 py-2 font-medium">{t(($) => $.lifecycle.col_time)}</th>
                  <th className="px-3 py-2 font-medium">{t(($) => $.lifecycle.col_result)}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((e, i) => (
                  <tr key={`${e.member_name}-${e.occurred_at}-${i}`} className="border-b last:border-0 hover:bg-muted">
                    <td className="px-3 py-2 text-xs font-medium">{e.member_name}</td>
                    <td className="px-3 py-2">
                      <EventBadge type={e.event_type} />
                    </td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">
                      {formatDateTime(e.occurred_at)}
                    </td>
                    <td className="px-3 py-2">
                      <ResultBadge result={e.result} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function LifecycleBody({
  data,
}: {
  data: AnalyticsLifecycle | undefined;
}) {
  const { t } = useT("analytics");
  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <div className="flex items-baseline justify-between">
          <span className="text-xs text-muted-foreground">
            {t(($) => $.lifecycle.onboarding_label)}
          </span>
          <span className="text-2xl font-medium tabular-nums">
            {formatPercent(data?.onboarding_rate)}
          </span>
        </div>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.lifecycle.onboarding_desc, {
            total: data?.new_hires_total ?? 0,
            onboarded: data?.onboarded_members ?? 0,
          })}
        </p>
      </div>
      <div className="space-y-1">
        <div className="flex items-baseline justify-between">
          <span className="text-xs text-muted-foreground">
            {t(($) => $.lifecycle.cutoff_label)}
          </span>
          <span className="text-2xl font-medium tabular-nums">
            {data?.cutoff_events ?? 0}
          </span>
        </div>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.lifecycle.cutoff_desc)}
        </p>
      </div>
      <div className="divide-y">
        {(data?.recent_events ?? []).slice(0, 5).map((e, i) => (
          <div key={`${e.member_name}-${e.occurred_at}-${i}`} className="flex items-center gap-2 py-2">
            <EventBadge type={e.event_type} />
            <span className="min-w-0 flex-1 truncate text-sm font-medium">
              {e.member_name}
            </span>
            <span className="text-xs text-muted-foreground">
              {formatDateTime(e.occurred_at)}
            </span>
            <ResultBadge result={e.result} />
          </div>
        ))}
      </div>
    </div>
  );
}

function EventBadge({ type }: { type: string }) {
  const { t } = useT("analytics");
  if (type === "onboard") {
    return <StatusBadge tone="success">{t(($) => $.lifecycle.event_onboard)}</StatusBadge>;
  }
  if (type === "offboard") {
    return <StatusBadge tone="secondary">{t(($) => $.lifecycle.event_offboard)}</StatusBadge>;
  }
  return <StatusBadge tone="warning">{t(($) => $.lifecycle.event_cutoff)}</StatusBadge>;
}

function ResultBadge({ result }: { result: string | null }) {
  const { t } = useT("analytics");
  if (result === "success") {
    return <StatusBadge tone="success">{t(($) => $.lifecycle.result_success)}</StatusBadge>;
  }
  if (result === "failed") {
    return <StatusBadge tone="destructive">{t(($) => $.lifecycle.result_failed)}</StatusBadge>;
  }
  return <span className="text-xs text-muted-foreground">{MISSING}</span>;
}

function CutoffSuccessCard({
  loading,
  error,
  onRetry,
  data,
}: {
  loading: boolean;
  error: boolean;
  onRetry: () => void;
  data: AnalyticsLifecycle | undefined;
}) {
  const { t } = useT("analytics");
  if (data?.source_status.ready === false) {
    return (
      <AnalyticsModuleCard
        title={t(($) => $.cutoff.title)}
        action={<Badge variant="secondary">{t(($) => $.lifecycle.badge)}</Badge>}
        loading={false}
        error={false}
        empty={false}
        guide={
          <SourceGuideState
            icon={<ShieldCheck className="size-8 text-muted-foreground" />}
            badge={t(($) => $.guide.ldap.badge)}
            title={t(($) => $.guide.ldap.title)}
            description={t(($) => $.guide.ldap.desc)}
          />
        }
      />
    );
  }
  return (
    <AnalyticsModuleCard
      title={t(($) => $.cutoff.title)}
      action={<Badge variant="secondary">{t(($) => $.lifecycle.badge)}</Badge>}
      loading={loading}
      error={error}
      empty={false}
      onRetry={onRetry}
    >
      <div className="space-y-3">
        <div className="flex items-baseline justify-between">
          <span className="text-xs text-muted-foreground">
            {t(($) => $.cutoff.label)}
          </span>
          <span className="text-2xl font-medium tabular-nums">
            {formatPercent(data?.cutoff_success_rate)}
          </span>
        </div>
        <TokenProgress
          value={data?.cutoff_success_rate != null ? data.cutoff_success_rate * 100 : 0}
          indicatorClassName="bg-success"
        />
        <p className="text-xs text-muted-foreground">
          {(data?.cutoff_events ?? 0) > 0
            ? t(($) => $.cutoff.desc, {
                success: data?.cutoff_success_count ?? 0,
                total: data?.cutoff_events ?? 0,
              })
            : t(($) => $.cutoff.no_events)}
        </p>
      </div>
    </AnalyticsModuleCard>
  );
}

function DepartmentSlicer({
  items,
  value,
  onChange,
}: {
  items: { department_id: string; name: string; member_count: number; active_members: number }[];
  value: string;
  onChange?: (departmentId: string) => void;
}) {
  const { t } = useT("analytics");
  const allLabel = t(($) => $.page.all_departments);
  const selected = items.find((d) => d.department_id === value);
  const selectedTitle = value === "" || !selected ? allLabel : selected.name;

  return (
    <Card>
      <CardHeader>
        <div className="min-w-0">
          <CardTitle>{t(($) => $.departments.title)}</CardTitle>
          <CardDescription>{t(($) => $.departments.desc)}</CardDescription>
        </div>
        {onChange ? (
          <CardAction>
            <Select
              items={[
                { value: "", label: allLabel },
                ...items.map((d) => ({ value: d.department_id, label: d.name })),
              ]}
              value={value}
              onValueChange={(v) => onChange(v ?? "")}
            >
              <SelectTrigger size="sm" className="min-w-[140px]">
                <SelectValue>
                  {() => (
                    <span className="flex items-center gap-1.5">
                      <Building2 className="size-3.5 text-muted-foreground" />
                      <span className="truncate">{selectedTitle}</span>
                    </span>
                  )}
                </SelectValue>
              </SelectTrigger>
              <SelectContent align="start" className="max-h-72">
                <SelectItem value="">
                  <span className="truncate">{allLabel}</span>
                </SelectItem>
                {items.map((d) => (
                  <SelectItem key={d.department_id} value={d.department_id}>
                    <span className="truncate">{d.name}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent>
        {items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <Building2 className="size-8 text-muted-foreground" />
            <p className="text-sm font-medium">{t(($) => $.departments.empty_title)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.departments.empty_desc)}
            </p>
          </div>
        ) : (
          <div className="divide-y">
            {items.map((d) => (
              <button
                key={d.department_id}
                type="button"
                onClick={() => onChange?.(d.department_id)}
                disabled={!onChange}
                className={`grid w-full grid-cols-[minmax(0,1.6fr)_auto] items-center gap-3 px-4 py-2 text-left ${
                  onChange ? "cursor-pointer hover:bg-muted" : "cursor-default"
                } ${d.department_id === value ? "bg-muted/50" : ""}`}
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{d.name}</p>
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.departments.member_hint, {
                      member: d.member_count,
                      active: d.active_members,
                    })}
                  </p>
                </div>
                <div className="flex w-24 items-center gap-2">
                  <TokenProgress
                    value={d.member_count > 0 ? (d.active_members / d.member_count) * 100 : 0}
                    indicatorClassName="bg-chart-4"
                  />
                  <span className="text-xs tabular-nums text-muted-foreground">
                    {d.member_count > 0
                      ? formatPercentInt(d.active_members / d.member_count)
                      : MISSING}
                  </span>
                </div>
              </button>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
