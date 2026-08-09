"use client";

// Tab3 — Git 贡献 (Git Contributions, v2.0 G1–G4). KPI row + ELOC ranking
// (group_by switch) + commit-quality radar (repo select) + repository activity
// table (sortable columns, PR drill-down dialog). Every module reads its own
// `source_status` — not-connected modules render the guide state while the
// rest of the tab stays live (E8 / E11 / E13 / E14).

import { useMemo, useState } from "react";
import { Code2, GitBranch, GitCommitHorizontal, ScanLine } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { NumberFlow } from "@multica/ui/components/ui/number-flow";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Badge } from "@multica/ui/components/ui/badge";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  analyticsElocOptions,
  analyticsQualityOptions,
  analyticsRepoActivityOptions,
  analyticsPrsOptions,
} from "@multica/core/analytics-dashboard";
import type { AnalyticsElocGroupBy, AnalyticsRepoActivityItem } from "@multica/core/types";
import { useT } from "../i18n";
import { AppLink } from "../navigation";
import { ActorAvatar } from "../common/actor-avatar";
import { AnalyticsKpiRow } from "./components/kpi-row";
import { AnalyticsModuleCard } from "./components/module-card";
import { Segmented } from "./components/segmented";
import { SourceGuideState } from "./components/source-guide-state";
import { StatusBadge } from "./components/status-badge";
import { QualityRadarChart } from "./charts";
import { formatCompact, formatDateShort, formatDurationEn, MISSING } from "./utils";

export interface Tab3GitProps {
  wsId: string;
  days: number;
  tz: string;
}

const ALL_REPOS = "all";

export function Tab3Git({ wsId, days, tz }: Tab3GitProps) {
  const { t } = useT("analytics");
  const [groupBy, setGroupBy] = useState<AnalyticsElocGroupBy>("member");
  const [repo, setRepo] = useState<string>(ALL_REPOS);

  const elocQuery = useQuery(analyticsElocOptions(wsId, days, tz, groupBy));
  const qualityQuery = useQuery(analyticsQualityOptions(wsId, days, tz, repo));
  const repoActivityQuery = useQuery(analyticsRepoActivityOptions(wsId, days, tz));

  const eloc = elocQuery.data;
  const quality = qualityQuery.data;
  const repos = repoActivityQuery.data;
  const reposReady = repos?.source_status.ready === true;
  const repoList = repos?.items ?? [];

  const kpiItems = useMemo(() => {
    const activeRepos = repoList.filter((r) => r.activity > 0).length;
    const prBacklog = repoList.reduce((sum, r) => sum + r.open_pr_backlog, 0);
    const mtmValues = repoList
      .map((r) => r.mtm_p50_seconds)
      .filter((v): v is number => v != null)
      .sort((a, b) => a - b);
    const mtmMedian =
      mtmValues.length > 0
        ? mtmValues.length % 2 === 1
          ? (mtmValues[Math.floor(mtmValues.length / 2)] ?? null)
          : (((mtmValues[mtmValues.length / 2 - 1] ?? 0) + (mtmValues[mtmValues.length / 2] ?? 0)) / 2)
        : null;

    const gitReady = repos?.source_status.ready === true;

    return [
      {
        label: t(($) => $.kpi.active_repos),
        value: gitReady ? (
          <NumberFlow value={activeRepos} format={{ maximumFractionDigits: 0 }} />
        ) : (
          MISSING
        ),
        hint:
          gitReady && repos
            ? t(($) => $.kpi.active_repos_hint, { count: repoList.length })
            : undefined,
        accent: "brand" as const,
      },
      {
        label: t(($) => $.kpi.pr_backlog),
        value: gitReady ? (
          <NumberFlow value={prBacklog} format={{ maximumFractionDigits: 0 }} />
        ) : (
          MISSING
        ),
        hint: t(($) => $.kpi.pr_backlog_hint),
      },
      {
        label: t(($) => $.kpi.mtm_p50),
        value: formatDurationEn(mtmMedian),
        hint: t(($) => $.kpi.mtm_p50_hint),
      },
      {
        label: t(($) => $.kpi.eloc_total),
        value: eloc ? formatCompact(eloc.total_eloc) : MISSING,
        hint:
          eloc && eloc.total_eloc > 0 ? (
            <span className="flex items-center gap-1.5">
              <span className="size-2.5 rounded-full bg-chart-1" />
              <span>
                {t(($) => $.eloc.human)} {formatCompact(eloc.human_eloc)}
              </span>
              <span className="size-2.5 rounded-full bg-chart-2" />
              <span>
                {t(($) => $.eloc.agent)} {formatCompact(eloc.agent_eloc)}
              </span>
            </span>
          ) : undefined,
        accent: "brand" as const,
      },
    ];
  }, [repoList, repos, eloc, t]);

  return (
    <div className="space-y-5">
      {repos?.source_status.ready === false ? (
        <div className="flex items-center gap-3 rounded-lg border border-border bg-muted/40 px-4 py-2.5">
          <GitBranch className="size-4 shrink-0 text-muted-foreground" />
          <p className="text-xs text-muted-foreground">{t(($) => $.guide.git.desc)}</p>
        </div>
      ) : null}
      <AnalyticsKpiRow items={kpiItems} />

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        {/* ② ELOC 排名 */}
        <AnalyticsModuleCard
          title={t(($) => $.eloc.title)}
          action={
            <Segmented
              value={groupBy}
              onChange={setGroupBy}
              options={[
                { label: t(($) => $.eloc.group_member), value: "member" as const },
                { label: t(($) => $.eloc.group_agent), value: "agent" as const },
              ]}
            />
          }
          headerNote={
            <>
              {eloc?.source_status.updated_at ? (
                <Badge variant="secondary">
                  {t(($) => $.common.as_of, { date: formatDateShort(eloc.source_status.updated_at) })}
                </Badge>
              ) : null}
              <Badge variant="secondary">{t(($) => $.eloc.badge_tool)}</Badge>
            </>
          }
          loading={elocQuery.isPending}
          error={elocQuery.isError}
          guide={
            eloc?.source_status.ready === false ? (
              <SourceGuideState
                icon={<Code2 className="size-8 text-muted-foreground" />}
                badge={t(($) => $.guide.eloc.badge)}
                title={t(($) => $.guide.eloc.title)}
                description={t(($) => $.guide.eloc.desc)}
                updatedAt={eloc.source_status.updated_at}
              />
            ) : undefined
          }
          empty={(eloc?.items.length ?? 0) === 0}
          emptyIcon={<GitCommitHorizontal className="size-8 text-muted-foreground" />}
          emptyTitle={t(($) => $.eloc.empty_title)}
          emptyDescription={t(($) => $.eloc.empty_desc)}
          onRetry={() => void elocQuery.refetch()}
        >
          <ElocRankingList
            data={eloc}
            groupBy={groupBy}
          />
        </AnalyticsModuleCard>

        {/* ③ 提交质量雷达 */}
        <AnalyticsModuleCard
          title={t(($) => $.quality.title)}
          action={
            <RepoSelect
              items={repoList.map((r) => r.repo)}
              value={repo}
              onChange={setRepo}
            />
          }
          headerNote={
            quality?.snapshot_at ? (
              <Badge variant="secondary">
                {t(($) => $.common.as_of, { date: formatDateShort(quality.snapshot_at) })}
              </Badge>
            ) : undefined
          }
          loading={qualityQuery.isPending}
          error={qualityQuery.isError}
          guide={
            quality?.source_status.ready === false ? (
              <SourceGuideState
                icon={<ScanLine className="size-8 text-muted-foreground" />}
                badge={t(($) => $.guide.scan.badge)}
                title={t(($) => $.guide.scan.title)}
                description={t(($) => $.guide.scan.desc)}
                updatedAt={quality.source_status.updated_at}
              />
            ) : undefined
          }
          empty={quality?.source_status.ready === true && quality.snapshot_at == null}
          emptyIcon={<ScanLine className="size-8 text-muted-foreground" />}
          emptyTitle={t(($) => $.quality.empty_title)}
          emptyDescription={t(($) => $.quality.empty_desc)}
          onRetry={() => void qualityQuery.refetch()}
        >
          {quality?.source_status.ready === true ? (
            <QualityRadarChart
              data={{
                coverage: quality.coverage,
                vulnerabilities: quality.vulnerabilities,
                duplication: quality.duplication_rate,
              }}
            />
          ) : null}
        </AnalyticsModuleCard>
      </div>

      {/* ④ 代码仓活跃分布 */}
      <RepoActivityTable
        loading={repoActivityQuery.isPending}
        error={repoActivityQuery.isError}
        onRetry={() => void repoActivityQuery.refetch()}
        data={repoActivityQuery.data}
        repoReady={reposReady}
        wsId={wsId}
        days={days}
        tz={tz}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// ELOC 排名
// ---------------------------------------------------------------------------

function ElocRankingList({
  data,
  groupBy,
}: {
  data: { total_eloc: number; human_eloc: number; agent_eloc: number; items: { entity_id: string; name: string; eloc: number; ratio: number | null; commit_count: number; repos: string[] }[] } | undefined;
  groupBy: AnalyticsElocGroupBy;
}) {
  const { t } = useT("analytics");
  const items = data?.items ?? [];
  const humanShare =
    data && data.total_eloc > 0 ? (data.human_eloc / data.total_eloc) * 100 : null;
  const agentShare =
    data && data.total_eloc > 0 ? (data.agent_eloc / data.total_eloc) * 100 : null;

  return (
    <div>
      {/* 汇总条 */}
      <div className="grid grid-cols-3 divide-x border-b">
        <div className="px-4 py-2">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="size-2.5 rounded-full bg-chart-1" />
            {t(($) => $.eloc.human)}
          </div>
          <div className="mt-0.5 text-sm font-medium tabular-nums">
            {formatCompact(data?.human_eloc)}
            {humanShare != null ? (
              <span className="ml-1 text-xs text-muted-foreground">{humanShare.toFixed(0)}%</span>
            ) : null}
          </div>
        </div>
        <div className="px-4 py-2">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="size-2.5 rounded-full bg-chart-2" />
            {t(($) => $.eloc.agent)}
          </div>
          <div className="mt-0.5 text-sm font-medium tabular-nums">
            {formatCompact(data?.agent_eloc)}
            {agentShare != null ? (
              <span className="ml-1 text-xs text-muted-foreground">{agentShare.toFixed(0)}%</span>
            ) : null}
          </div>
        </div>
        <div className="px-4 py-2">
          <div className="text-xs text-muted-foreground">{t(($) => $.eloc.total)}</div>
          <div className="mt-0.5 text-sm font-medium tabular-nums">
            {formatCompact(data?.total_eloc)}
          </div>
        </div>
      </div>

      {/* 排行 */}
      <div className="divide-y">
        {items.map((item, i) => (
          <ElocRow key={item.entity_id} item={item} rank={i + 1} groupBy={groupBy} />
        ))}
      </div>
    </div>
  );
}

function ElocRow({
  item,
  rank,
  groupBy,
}: {
  item: { entity_id: string; name: string; eloc: number; ratio: number | null; commit_count: number; repos: string[] };
  rank: number;
  groupBy: AnalyticsElocGroupBy;
}) {
  const { t } = useT("analytics");
  const wsPaths = useWorkspacePaths();
  const isUnmapped = item.entity_id === "unmapped";
  const isAgent = groupBy === "agent";
  const ratioPct = item.ratio != null ? item.ratio * 100 : 0;

  const nameNode = isUnmapped ? (
    <span className="truncate text-sm font-medium">
      {t(($) => $.eloc.unmapped)}
    </span>
  ) : isAgent ? (
    <span className="flex min-w-0 items-center gap-1.5">
      <ActorAvatar actorType="agent" actorId={item.entity_id} size="sm" />
      <AppLink
        href={wsPaths.agentDetail(item.entity_id)}
        newTabTitle={item.name}
        className="truncate text-sm font-medium hover:underline"
      >
        {item.name}
      </AppLink>
      <Badge variant="secondary" className="shrink-0">{t(($) => $.eloc.bot_badge)}</Badge>
    </span>
  ) : (
    <span className="flex min-w-0 items-center gap-1.5">
      <ActorAvatar actorType="member" actorId={item.entity_id} size="sm" />
      <AppLink
        href={wsPaths.memberDetail(item.entity_id)}
        newTabTitle={item.name}
        className="truncate text-sm font-medium hover:underline"
      >
        {item.name}
      </AppLink>
    </span>
  );

  const row = (
    <div className="grid grid-cols-[minmax(0,1.6fr)_84px_70px] items-center gap-3 px-4 py-2 hover:bg-muted">
      <div className="flex min-w-0 items-center gap-2">
        <span className="w-4 shrink-0 text-center text-xs tabular-nums text-muted-foreground">
          {rank}
        </span>
        {nameNode}
        {isUnmapped ? <Badge variant="secondary">{t(($) => $.eloc.unmapped)}</Badge> : null}
      </div>
      <span className="text-right text-xs tabular-nums">{formatCompact(item.eloc)}</span>
      <span className="text-right text-xs text-muted-foreground">
        {item.ratio != null ? `${ratioPct.toFixed(1)}%` : MISSING}
      </span>
    </div>
  );

  const rowWithHover = item.repos.length > 0 ? (
    <HoverCard>
      <HoverCardTrigger delay={120} render={row} />
      <HoverCardContent side="bottom" align="start" className="max-w-xs">
        <div className="space-y-1.5 p-1">
          <p className="text-xs font-medium text-muted-foreground">
            {t(($) => $.eloc.commit_detail)}
          </p>
          <div className="flex flex-wrap gap-1">
            {item.repos.map((r) => (
              <Badge key={r} variant="secondary" className="font-mono text-[10px]">
                {r}
              </Badge>
            ))}
          </div>
        </div>
      </HoverCardContent>
    </HoverCard>
  ) : (
    row
  );

  return (
    <div className={isUnmapped ? "opacity-60" : undefined}>
      {rowWithHover}
      {item.ratio != null ? (
        <div className="mx-4 -mt-1 mb-1 h-1.5 overflow-hidden rounded-full bg-muted">
          <div
            className={`h-full rounded-full ${isAgent ? "bg-chart-2" : "bg-chart-1"}`}
            style={{ width: `${Math.max(0, Math.min(100, ratioPct))}%` }}
          />
        </div>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// 仓库下拉
// ---------------------------------------------------------------------------

function RepoSelect({
  items,
  value,
  onChange,
}: {
  items: string[];
  value: string;
  onChange: (v: string) => void;
}) {
  const { t } = useT("analytics");
  const allLabel = t(($) => $.quality.all_repos);
  const selected = items.find((r) => r === value);
  const selectedTitle = value === ALL_REPOS ? allLabel : selected ?? allLabel;

  return (
    <Select
      items={[{ value: ALL_REPOS, label: allLabel }, ...items.map((r) => ({ value: r, label: r }))]}
      value={value}
      onValueChange={(v) => onChange(v ?? ALL_REPOS)}
    >
      <SelectTrigger size="sm" className="min-w-[140px]">
        <SelectValue>
          {() => (
            <span className="flex items-center gap-1.5">
              <GitBranch className="size-3.5 text-muted-foreground" />
              <span className="truncate">{selectedTitle}</span>
            </span>
          )}
        </SelectValue>
      </SelectTrigger>
      <SelectContent align="start" className="max-h-72">
        <SelectItem value={ALL_REPOS}>
          <span className="truncate">{allLabel}</span>
        </SelectItem>
        {items.map((r) => (
          <SelectItem key={r} value={r}>
            <span className="truncate font-mono text-xs">{r}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// ---------------------------------------------------------------------------
// 代码仓活跃分布（G3 + G4 下钻）
// ---------------------------------------------------------------------------

type RepoSortKey = "repo" | "activity" | "open_pr_backlog" | "mtm_p50_seconds" | "mtm_p95_seconds";

function RepoActivityTable({
  loading,
  error,
  onRetry,
  data,
  repoReady,
  wsId,
  days,
  tz,
}: {
  loading: boolean;
  error: boolean;
  onRetry: () => void;
  data: { source_status: { ready: boolean; updated_at: string | null }; items: AnalyticsRepoActivityItem[] } | undefined;
  repoReady: boolean;
  wsId: string;
  days: number;
  tz: string;
}) {
  const { t } = useT("analytics");
  const [sortKey, setSortKey] = useState<RepoSortKey>("activity");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");
  const [drilldownRepo, setDrilldownRepo] = useState<AnalyticsRepoActivityItem | null>(null);

  const prsQuery = useQuery({
    ...analyticsPrsOptions(wsId, days, tz, drilldownRepo?.repo ?? ""),
    enabled: drilldownRepo != null,
  });

  const sorted = useMemo(() => {
    const items = [...(data?.items ?? [])];
    items.sort((a, b) => {
      const av = a[sortKey];
      const bv = b[sortKey];
      const aNum = typeof av === "string" ? NaN : (av ?? -1);
      const bNum = typeof bv === "string" ? NaN : (bv ?? -1);
      const cmp = Number.isNaN(aNum)
        ? String(av).localeCompare(String(bv))
        : aNum - bNum;
      return sortDir === "asc" ? cmp : -cmp;
    });
    return items;
  }, [data?.items, sortKey, sortDir]);

  const toggleSort = (key: RepoSortKey) => {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  const columnHeader = (key: RepoSortKey, label: string, numeric = false) => (
    <button
      type="button"
      onClick={() => toggleSort(key)}
      className={`inline-flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground ${
        numeric ? "justify-end" : ""
      }`}
    >
      {label}
      {sortKey === key ? <span className="text-[10px]">{sortDir === "asc" ? "↑" : "↓"}</span> : null}
    </button>
  );

  return (
    <AnalyticsModuleCard
      title={t(($) => $.repos.title)}
      description={t(($) => $.repos.desc)}
      loading={loading}
      error={error}
      guide={
        data?.source_status.ready === false ? (
          <SourceGuideState
            icon={<GitBranch className="size-8 text-muted-foreground" />}
            badge={t(($) => $.guide.git.badge)}
            title={t(($) => $.guide.git.title)}
            description={t(($) => $.guide.git.desc)}
            updatedAt={data.source_status.updated_at}
          />
        ) : undefined
      }
      empty={repoReady && (data?.items.length ?? 0) === 0}
      emptyIcon={<GitBranch className="size-8 text-muted-foreground" />}
      emptyTitle={t(($) => $.repos.empty_title)}
      emptyDescription={t(($) => $.repos.empty_desc)}
      onRetry={onRetry}
    >
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead>
            <tr className="border-b">
              <th className="px-4 py-2 text-left">{columnHeader("repo", t(($) => $.repos.col_repo))}</th>
              <th className="px-4 py-2 text-right">{columnHeader("activity", t(($) => $.repos.col_activity), true)}</th>
              <th className="px-4 py-2 text-right">{columnHeader("open_pr_backlog", t(($) => $.repos.col_backlog), true)}</th>
              <th className="px-4 py-2 text-right">{columnHeader("mtm_p50_seconds", t(($) => $.repos.col_p50), true)}</th>
              <th className="px-4 py-2 text-right">{columnHeader("mtm_p95_seconds", t(($) => $.repos.col_p95), true)}</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((row) => (
              <tr
                key={row.repo}
                className="cursor-pointer border-b last:border-0 hover:bg-muted"
                onClick={() => setDrilldownRepo(row)}
              >
                <td className="px-4 py-2">
                  <span className="flex items-center gap-2 font-mono text-xs">
                    <GitBranch className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className="truncate">{row.repo}</span>
                  </span>
                </td>
                <td className="px-4 py-2 text-right">
                  <span className="text-sm font-medium tabular-nums">{row.activity}</span>
                  <span className="ml-1.5 text-[10px] text-muted-foreground">
                    {t(($) => $.repos.activity_hint, {
                      commits: row.active_commits,
                      prs: row.active_prs,
                    })}
                  </span>
                </td>
                <td className={`px-4 py-2 text-right text-xs tabular-nums ${row.open_pr_backlog === 0 ? "text-muted-foreground" : ""}`}>
                  {row.open_pr_backlog}
                </td>
                <td className="px-4 py-2 text-right text-xs tabular-nums">
                  {formatDurationEn(row.mtm_p50_seconds)}
                </td>
                <td className="px-4 py-2 text-right text-xs tabular-nums">
                  {formatDurationEn(row.mtm_p95_seconds)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <PrDrilldownDialog
        open={drilldownRepo != null}
        onOpenChange={(open) => {
          if (!open) setDrilldownRepo(null);
        }}
        repo={drilldownRepo?.repo ?? ""}
        loading={prsQuery.isPending}
        error={prsQuery.isError}
        onRetry={() => void prsQuery.refetch()}
        items={prsQuery.data?.items ?? []}
      />
    </AnalyticsModuleCard>
  );
}

function PrDrilldownDialog({
  open,
  onOpenChange,
  repo,
  loading,
  error,
  onRetry,
  items,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repo: string;
  loading: boolean;
  error: boolean;
  onRetry: () => void;
  items: { pr_number: number; title: string; state: string; author_login: string | null; pr_created_at: string; merged_at: string | null; closed_at: string | null; html_url: string }[];
}) {
  const { t } = useT("analytics");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.repos.pr_dialog_title, { repo })}</DialogTitle>
          <DialogDescription>
            {t(($) => $.repos.pr_dialog_desc, { count: items.length })}
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <div className="flex flex-col items-center gap-3 py-6 text-center">
            <p className="text-sm font-medium">{t(($) => $.common.load_failed)}</p>
            <Button variant="outline" size="sm" onClick={onRetry}>
              {t(($) => $.common.retry)}
            </Button>
          </div>
        ) : loading ? (
          <div className="flex justify-center py-8">
            <span className="size-4 animate-spin rounded-full border-2 border-muted border-t-foreground" />
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <GitBranch className="size-8 text-muted-foreground" />
            <p className="text-sm font-medium">{t(($) => $.repos.pr_empty)}</p>
          </div>
        ) : (
          <div className="max-h-[60vh] overflow-y-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b text-left text-xs text-muted-foreground">
                  <th className="px-3 py-2 font-medium">{t(($) => $.repos.pr_col_number)}</th>
                  <th className="px-3 py-2 font-medium">{t(($) => $.repos.pr_col_title)}</th>
                  <th className="px-3 py-2 font-medium">{t(($) => $.repos.pr_col_state)}</th>
                  <th className="px-3 py-2 font-medium">{t(($) => $.repos.pr_col_time)}</th>
                  <th className="px-3 py-2 font-medium">{t(($) => $.repos.pr_col_author)}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((pr) => (
                  <tr key={pr.pr_number} className="border-b last:border-0 hover:bg-muted">
                    <td className="px-3 py-2">
                      <AppLink
                        href={pr.html_url}
                        newTabTitle={pr.title}
                        className="font-mono text-xs text-foreground hover:underline"
                      >
                        #{pr.pr_number}
                      </AppLink>
                    </td>
                    <td className="max-w-[16rem] truncate px-3 py-2 text-xs">{pr.title}</td>
                    <td className="px-3 py-2">
                      <PrStateBadge state={pr.state} />
                    </td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">
                      {formatDateShort(pr.merged_at ?? pr.closed_at ?? pr.pr_created_at)}
                    </td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">
                      {pr.author_login ?? MISSING}
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

function PrStateBadge({ state }: { state: string }) {
  const { t } = useT("analytics");
  switch (state) {
    case "merged":
      return <StatusBadge tone="success">{t(($) => $.repos.pr_state_merged)}</StatusBadge>;
    case "open":
      return <StatusBadge tone="warning">{t(($) => $.repos.pr_state_open)}</StatusBadge>;
    case "closed":
      return <StatusBadge tone="secondary">{t(($) => $.repos.pr_state_closed)}</StatusBadge>;
    default:
      return <StatusBadge tone="secondary">{t(($) => $.repos.pr_state_draft)}</StatusBadge>;
  }
}
