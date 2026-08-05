"use client";

// Tab2 — Agent 效能 (Agent Performance). KPI row (B1/B2) + 执行漏斗 (B1,
// Merged 段三态) + 技能积累图谱 (B4) + 技能 Top 复用 (B4) + 人机协作指数 (B5)
// + Blocker 响应时长分布 (B6).

import { useMemo, useState } from "react";
import {
  BookOpen,
  Clock,
  Filter,
  Lightbulb,
  MessagesSquare,
  TrendingUp,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { NumberFlow } from "@multica/ui/components/ui/number-flow";
import { Badge } from "@multica/ui/components/ui/badge";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  analyticsFunnelOptions,
  analyticsAgentPerformanceOptions,
  analyticsSkillsOverviewOptions,
  analyticsCollaborationSummaryOptions,
  analyticsBlockersOptions,
} from "@multica/core/analytics-dashboard";
import { useT } from "../i18n";
import { AppLink } from "../navigation";
import { AnalyticsKpiRow } from "./components/kpi-row";
import { AnalyticsModuleCard } from "./components/module-card";
import { Segmented } from "./components/segmented";
import { StatusBadge } from "./components/status-badge";
import { DualLineChart } from "./charts";
import { formatDateTime, formatDurationEn, formatPercent, formatPercentInt, MISSING } from "./utils";

export interface Tab2AgentProps {
  wsId: string;
  days: number;
  tz: string;
  departmentId: string | null;
}

type BlockerFilter = "all" | "resolved" | "open";

export function Tab2Agent({ wsId, days, tz, departmentId }: Tab2AgentProps) {
  const { t } = useT("analytics");

  const funnelQuery = useQuery(analyticsFunnelOptions(wsId, days, tz, departmentId));
  const performanceQuery = useQuery(analyticsAgentPerformanceOptions(wsId, days, tz));
  const skillsQuery = useQuery(analyticsSkillsOverviewOptions(wsId, days, tz));
  const collaborationQuery = useQuery(analyticsCollaborationSummaryOptions(wsId, days, tz));
  const blockersQuery = useQuery(analyticsBlockersOptions(wsId, days, tz));

  const funnel = funnelQuery.data;
  const skills = skillsQuery.data;
  const collaboration = collaborationQuery.data;
  const blockers = blockersQuery.data ?? [];

  const kpiItems = useMemo(() => {
    const f = funnelQuery.data;
    const p = performanceQuery.data;
    return [
      {
        label: t(($) => $.kpi.assign_tasks),
        value: f ? (
          <NumberFlow value={f.assign_count} format={{ maximumFractionDigits: 0 }} />
        ) : MISSING,
        hint:
          f != null
            ? t(($) => $.kpi.assign_tasks_hint, { count: f.issue_assigned_count })
            : undefined,
      },
      {
        label: t(($) => $.kpi.execute_tasks),
        value: f ? (
          <NumberFlow value={f.execute_count} format={{ maximumFractionDigits: 0 }} />
        ) : MISSING,
        hint: formatPercent(f?.execute_ratio),
      },
      {
        label: t(($) => $.kpi.success_rate),
        value: formatPercent(p?.success_rate),
        hint:
          p != null
            ? t(($) => $.kpi.success_rate_hint, {
                completed: p.completed_count,
                terminal: p.terminal_count,
              })
            : undefined,
        accent: p?.success_rate != null ? ("success" as const) : undefined,
      },
      {
        label: t(($) => $.kpi.agent_duration_p50),
        value:
          p?.p50_duration_seconds != null ? (
            <NumberFlow value={p.p50_duration_seconds} format={{ maximumFractionDigits: 0 }} />
          ) : (
            MISSING
          ),
        hint:
          p?.avg_duration_seconds != null
            ? t(($) => $.kpi.agent_duration_p50_hint, {
                avg: formatDurationEn(p.avg_duration_seconds),
              })
            : undefined,
      },
    ];
  }, [funnelQuery.data, performanceQuery.data, t]);

  return (
    <div className="space-y-5">
      <AnalyticsKpiRow items={kpiItems} />

      {/* ② 执行漏斗（Merged 三态） */}
      <ExecutionFunnel
        loading={funnelQuery.isPending}
        error={funnelQuery.isError}
        onRetry={() => void funnelQuery.refetch()}
        assignCount={funnel?.assign_count ?? 0}
        issueAssignedCount={funnel?.issue_assigned_count ?? 0}
        executeCount={funnel?.execute_count ?? 0}
        executeRatio={funnel?.execute_ratio ?? null}
        mergedCount={funnel?.merged_count ?? null}
        mergedRatio={funnel?.merged_ratio ?? null}
      />

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        {/* ③ 技能积累图谱 */}
        <AnalyticsModuleCard
          title={t(($) => $.skills.title)}
          description={t(($) => $.skills.desc)}
          loading={skillsQuery.isPending}
          error={skillsQuery.isError}
          empty={skills?.total_skills === 0 && (skills?.accumulation.length ?? 0) === 0}
          emptyIcon={<Lightbulb className="size-8 text-muted-foreground" />}
          emptyTitle={t(($) => $.skills.empty_title)}
          emptyDescription={t(($) => $.skills.empty_desc)}
          onRetry={() => void skillsQuery.refetch()}
        >
          <SkillsAccumulation data={skills} />
        </AnalyticsModuleCard>

        {/* ④ 技能 Top 复用 */}
        <AnalyticsModuleCard
          title={t(($) => $.skills_top.title)}
          description={t(($) => $.skills_top.desc)}
          loading={skillsQuery.isPending}
          error={skillsQuery.isError}
          empty={(skills?.top_reused.length ?? 0) === 0}
          emptyIcon={<BookOpen className="size-8 text-muted-foreground" />}
          emptyTitle={t(($) => $.skills_top.empty_title)}
          emptyDescription={t(($) => $.skills_top.empty_desc)}
          onRetry={() => void skillsQuery.refetch()}
        >
          <SkillsTopList items={skills?.top_reused ?? []} />
        </AnalyticsModuleCard>
      </div>

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        {/* ⑤ 人机协作指数 */}
        <AnalyticsModuleCard
          title={t(($) => $.collaboration.title)}
          loading={collaborationQuery.isPending}
          error={collaborationQuery.isError}
          empty={(collaboration?.collab_issue_count ?? 0) === 0}
          emptyIcon={<MessagesSquare className="size-8 text-muted-foreground" />}
          emptyTitle={t(($) => $.collaboration.empty_title)}
          emptyDescription={t(($) => $.collaboration.empty_desc)}
          onRetry={() => void collaborationQuery.refetch()}
        >
          <CollaborationSummary data={collaboration} />
        </AnalyticsModuleCard>

        {/* ⑥ Blocker 响应时长分布 */}
        <BlockerTable
          loading={blockersQuery.isPending}
          error={blockersQuery.isError}
          onRetry={() => void blockersQuery.refetch()}
          items={blockers}
          collaboration={collaboration}
        />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// 执行漏斗
// ---------------------------------------------------------------------------

function ExecutionFunnel({
  loading,
  error,
  onRetry,
  assignCount,
  issueAssignedCount,
  executeCount,
  executeRatio,
  mergedCount,
  mergedRatio,
}: {
  loading: boolean;
  error: boolean;
  onRetry: () => void;
  assignCount: number;
  issueAssignedCount: number;
  executeCount: number;
  executeRatio: number | null;
  mergedCount: number | null;
  mergedRatio: number | null;
}) {
  const { t } = useT("analytics");
  const mergedGuide = mergedCount == null;
  const mergedEmpty = mergedCount === 0;

  return (
    <AnalyticsModuleCard
      title={t(($) => $.funnel.title)}
      description={t(($) => $.funnel.desc)}
      headerNote={<Badge variant="secondary">{t(($) => $.funnel.note)}</Badge>}
      loading={loading}
      error={error}
      empty={assignCount === 0 && executeCount === 0}
      emptyIcon={<Filter className="size-8 text-muted-foreground" />}
      emptyTitle={t(($) => $.funnel.empty_title)}
      emptyDescription={t(($) => $.funnel.empty_desc)}
      onRetry={onRetry}
    >
      <div className="space-y-4">
        <div className="flex flex-col gap-3 md:flex-row md:items-center">
          <FunnelStage
            label={t(($) => $.funnel.assign)}
            count={assignCount}
            className="bg-chart-1/80"
            note={
              issueAssignedCount > 0
                ? t(($) => $.funnel.issue_level, { count: issueAssignedCount })
                : undefined
            }
          />
          <FunnelArrow ratio={executeRatio} />
          <FunnelStage
            label={t(($) => $.funnel.execute)}
            count={executeCount}
            className="bg-chart-2/80"
          />
          <FunnelArrow ratio={mergedRatio} placeholder={mergedGuide || mergedEmpty} />
          <FunnelStage
            label={t(($) => $.funnel.merged)}
            count={mergedCount}
            guide={mergedGuide}
            empty={mergedEmpty}
            className="border-2 border-dashed border-border bg-muted/20"
          />
        </div>
        {mergedGuide ? (
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Clock className="size-3.5" />
            {t(($) => $.funnel.merged_placeholder)}
          </p>
        ) : null}
      </div>
    </AnalyticsModuleCard>
  );
}

function FunnelStage({
  label,
  count,
  className,
  note,
  guide,
  empty,
}: {
  label: string;
  count: number | null;
  className: string;
  note?: string;
  guide?: boolean;
  empty?: boolean;
}) {
  const { t } = useT("analytics");
  const text = count == null
    ? t(($) => $.funnel.merged_placeholder)
    : `${label} · ${count}`;
  return (
    <div className="min-w-0 flex-1">
      <div
        className={`flex h-10 items-center justify-center rounded-md ${className} ${
          guide || empty ? "opacity-60" : ""
        } ${guide ? "text-muted-foreground" : "font-medium text-foreground"}`}
      >
        {guide ? (
          <span className="flex items-center gap-1.5 text-xs">
            {text}
            <Badge variant="secondary">{t(($) => $.funnel.merge_badge)}</Badge>
          </span>
        ) : (
          <span className="text-sm">{text}</span>
        )}
      </div>
      {note ? <p className="mt-1 text-center text-xs text-muted-foreground">{note}</p> : null}
    </div>
  );
}

function FunnelArrow({ ratio, placeholder }: { ratio: number | null; placeholder?: boolean }) {
  if (placeholder) {
    return (
      <span className="shrink-0 px-1 text-xs font-medium text-muted-foreground">—</span>
    );
  }
  return (
    <span className="flex shrink-0 items-center gap-1 px-1 text-xs font-medium text-foreground">
      <TrendingUp className="size-4 text-success" />
      {formatPercent(ratio)}
    </span>
  );
}

// ---------------------------------------------------------------------------
// 技能积累图谱
// ---------------------------------------------------------------------------

function SkillsAccumulation({
  data,
}: {
  data: { new_skills: number; total_skills: number; accumulation: { bucket: string; count: number }[] } | undefined;
}) {
  const { t } = useT("analytics");
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-4">
        <div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.skills.new_skills, { count: data?.new_skills ?? 0 })}
          </div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground">
            {t(($) => $.skills.total_skills, { count: data?.total_skills ?? 0 })}
          </div>
        </div>
      </div>
      <SkillsAccumulationChart points={data?.accumulation ?? []} />
    </div>
  );
}

function SkillsAccumulationChart({ points }: { points: { bucket: string; count: number }[] }) {
  const data = points.map((p) => ({ label: p.bucket, a: p.count, b: null }));
  if (data.length === 0) return null;
  return (
    <DualLineChart
      data={data}
      config={{ a: { label: "new", color: "var(--color-chart-3)" } }}
      aKey="a"
      formatValue={(v) => String(Math.round(v))}
      className="aspect-[3/1]"
    />
  );
}

// ---------------------------------------------------------------------------
// 技能 Top 复用
// ---------------------------------------------------------------------------

function SkillsTopList({
  items,
}: {
  items: { skill_id: string; name: string; reuse_count: number; bound_agents: number }[];
}) {
  const { t } = useT("analytics");
  const wsPaths = useWorkspacePaths();
  return (
    <div>
      <div className="grid grid-cols-[minmax(0,1.6fr)_4rem_3rem] items-center gap-3 border-b px-4 py-2 text-xs font-medium text-muted-foreground">
        <span />
        <span className="text-right">{t(($) => $.skills_top.header_reuse)}</span>
        <span className="text-right">{t(($) => $.skills_top.header_agents)}</span>
      </div>
      <div className="divide-y">
        {items.map((s) => (
          <div
            key={s.skill_id}
            className="grid grid-cols-[minmax(0,1.6fr)_4rem_3rem] items-center gap-3 px-4 py-2 hover:bg-muted"
          >
            <div className="flex min-w-0 items-center gap-2">
              <AppLink
                href={wsPaths.skillDetail(s.skill_id)}
                newTabTitle={s.name}
                className="truncate text-sm font-medium hover:underline"
              >
                {s.name}
              </AppLink>
              <Badge variant="secondary">{t(($) => $.skills_top.proxy_badge)}</Badge>
            </div>
            <span className="text-right text-xs tabular-nums">{s.reuse_count}</span>
            <span className="text-right text-xs tabular-nums text-muted-foreground">
              {s.bound_agents}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// 人机协作指数
// ---------------------------------------------------------------------------

function CollaborationSummary({
  data,
}: {
  data: {
    interaction_frequency: number | null;
    collab_issue_ratio: number | null;
    blocker_avg_seconds: number | null;
    blocker_open_count: number;
  } | undefined;
}) {
  const { t } = useT("analytics");
  return (
    <div className="space-y-4">
      <CollabMetric
        label={t(($) => $.collaboration.interaction_label)}
        value={data?.interaction_frequency != null ? data.interaction_frequency.toFixed(1) : MISSING}
        unit="条/协作Issue"
        desc={t(($) => $.collaboration.interaction_desc)}
      />
      <CollabMetric
        label={t(($) => $.collaboration.ratio_label)}
        value={formatPercentInt(data?.collab_issue_ratio)}
        desc={t(($) => $.collaboration.ratio_desc)}
      />
      <CollabMetric
        label={t(($) => $.collaboration.blocker_label)}
        value={formatDurationEn(data?.blocker_avg_seconds)}
        desc={
          data?.blocker_avg_seconds == null
            ? t(($) => $.collaboration.no_blocker)
            : t(($) => $.collaboration.blocker_desc)
        }
      >
        {data?.blocker_open_count != null && data.blocker_open_count > 0 ? (
          <StatusBadge tone="warning">
            {t(($) => $.collaboration.blocker_open, { count: data.blocker_open_count })}
          </StatusBadge>
        ) : null}
      </CollabMetric>
    </div>
  );
}

function CollabMetric({
  label,
  value,
  unit,
  desc,
  children,
}: {
  label: string;
  value: string;
  unit?: string;
  desc?: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="space-y-1">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-xs text-muted-foreground">{label}</span>
        <span className="flex items-center gap-2 text-sm font-medium tabular-nums">
          {value}
          {unit ? <span className="text-xs text-muted-foreground">{unit}</span> : null}
          {children}
        </span>
      </div>
      {desc ? <p className="text-xs text-muted-foreground">{desc}</p> : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Blocker 响应时长分布
// ---------------------------------------------------------------------------

function BlockerTable({
  loading,
  error,
  onRetry,
  items,
  collaboration,
}: {
  loading: boolean;
  error: boolean;
  onRetry: () => void;
  items: {
    issue_id: string;
    issue_title: string;
    blocked_at: string;
    resolved_at: string | null;
    response_seconds: number | null;
    status: "resolved" | "open";
  }[];
  collaboration: {
    blocker_p50_seconds: number | null;
    blocker_p95_seconds: number | null;
    blocker_open_count: number;
  } | undefined;
}) {
  const { t } = useT("analytics");
  const [filter, setFilter] = useState<BlockerFilter>("all");

  const filtered = useMemo(
    () =>
      items.filter((i) =>
        filter === "all" ? true : filter === "resolved" ? i.status === "resolved" : i.status === "open",
      ),
    [items, filter],
  );

  const empty =
    items.length === 0 && (collaboration?.blocker_p50_seconds ?? 0) === 0;

  return (
    <AnalyticsModuleCard
      title={t(($) => $.blockers.title)}
      action={
        <Segmented
          value={filter}
          onChange={setFilter}
          options={[
            { label: t(($) => $.blockers.filter_all), value: "all" as const },
            { label: t(($) => $.blockers.filter_resolved), value: "resolved" as const },
            { label: t(($) => $.blockers.filter_open), value: "open" as const },
          ]}
        />
      }
      loading={loading}
      error={error}
      empty={empty}
      emptyIcon={<Clock className="size-8 text-muted-foreground" />}
      emptyTitle={t(($) => $.blockers.empty_title)}
      emptyDescription={t(($) => $.blockers.empty_desc)}
      onRetry={onRetry}
    >
      <div className="space-y-3">
        <div className="grid grid-cols-3 divide-x border-b pb-2">
          <BlockerStat
            label="P50"
            value={formatDurationEn(collaboration?.blocker_p50_seconds)}
          />
          <BlockerStat
            label="P95"
            value={formatDurationEn(collaboration?.blocker_p95_seconds)}
          />
          <BlockerStat
            label={t(($) => $.blockers.filter_open)}
            value={String(collaboration?.blocker_open_count ?? 0)}
          />
        </div>
        <div className="divide-y">
          {filtered.map((b) => (
            <BlockerRow key={b.issue_id} item={b} />
          ))}
        </div>
      </div>
    </AnalyticsModuleCard>
  );
}

function BlockerRow({
  item,
}: {
  item: {
    issue_id: string;
    issue_title: string;
    blocked_at: string;
    resolved_at: string | null;
    response_seconds: number | null;
    status: "resolved" | "open";
  };
}) {
  const { t } = useT("analytics");
  const wsPaths = useWorkspacePaths();
  return (
    <div className="grid grid-cols-[minmax(0,1.6fr)_auto_auto_auto] items-center gap-3 px-4 py-2 hover:bg-muted">
      <AppLink
        href={wsPaths.issueDetail(item.issue_id)}
        newTabTitle={item.issue_title}
        className="truncate text-sm font-medium hover:underline"
      >
        {item.issue_title}
      </AppLink>
      <span className="text-xs tabular-nums text-muted-foreground">
        {formatDateTime(item.blocked_at)}
      </span>
      <span className="text-xs tabular-nums text-muted-foreground">
        {item.resolved_at ? formatDateTime(item.resolved_at) : t(($) => $.blockers.filter_open)}
      </span>
      <span className="text-xs tabular-nums">{formatDurationEn(item.response_seconds)}</span>
      <span />
      <BlockerStatusBadge status={item.status} />
    </div>
  );
}

function BlockerStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="px-3">
      <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      <div className="text-sm font-medium tabular-nums">{value}</div>
    </div>
  );
}

function BlockerStatusBadge({ status }: { status: "resolved" | "open" }) {
  const { t } = useT("analytics");
  return status === "resolved" ? (
    <StatusBadge tone="success">{t(($) => $.blockers.status_resolved)}</StatusBadge>
  ) : (
    <StatusBadge tone="warning">{t(($) => $.blockers.status_open)}</StatusBadge>
  );
}
