import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type {
  AnalyticsElocGroupBy,
  AnalyticsHeatmapMetric,
  AnalyticsLeadTimeMetric,
} from "../types";

// TanStack query options for the engineering-analytics platform
// (`/{slug}/analytics`, CLO-240). Each module queries its own endpoint so a
// failed request surfaces as that module's error state (E8) instead of taking
// the page down. Range switches keep the previous payload mounted via
// `keepPreviousData`; scoping parts (workspace, timezone) never carry data
// across — same policy as the dashboard/monitoring query modules.
//
// `department_id` is only ever passed for Tab1 (A1–A5) and Tab2 (B1). Tab3/
// Tab4 and the identity/department series (G/D/L) never accept it (API §0.2).

export const analyticsKeys = {
  all: (wsId: string) => ["analytics", wsId] as const,
  activitySummary: (wsId: string, days: number, tz: string, departmentId: string | null) =>
    [...analyticsKeys.all(wsId), "activity-summary", days, tz, departmentId] as const,
  heatmap: (
    wsId: string,
    days: number,
    tz: string,
    departmentId: string | null,
    metric: AnalyticsHeatmapMetric,
  ) => [...analyticsKeys.all(wsId), "heatmap", days, tz, departmentId, metric] as const,
  topMembers: (wsId: string, days: number, tz: string, departmentId: string | null) =>
    [...analyticsKeys.all(wsId), "top-members", days, tz, departmentId] as const,
  adoptionSummary: (wsId: string, days: number, tz: string, departmentId: string | null) =>
    [...analyticsKeys.all(wsId), "adoption-summary", days, tz, departmentId] as const,
  adoptionTrend: (wsId: string, days: number, tz: string, departmentId: string | null) =>
    [...analyticsKeys.all(wsId), "adoption-trend", days, tz, departmentId] as const,
  funnel: (wsId: string, days: number, tz: string, departmentId: string | null) =>
    [...analyticsKeys.all(wsId), "funnel", days, tz, departmentId] as const,
  agentPerformance: (wsId: string, days: number, tz: string) =>
    [...analyticsKeys.all(wsId), "agent-performance", days, tz] as const,
  topAgents: (wsId: string, days: number, tz: string) =>
    [...analyticsKeys.all(wsId), "top-agents", days, tz] as const,
  skillsOverview: (wsId: string, days: number, tz: string) =>
    [...analyticsKeys.all(wsId), "skills-overview", days, tz] as const,
  collaborationSummary: (wsId: string, days: number, tz: string) =>
    [...analyticsKeys.all(wsId), "collaboration-summary", days, tz] as const,
  blockers: (wsId: string, days: number, tz: string) =>
    [...analyticsKeys.all(wsId), "blockers", days, tz] as const,
  eloc: (wsId: string, days: number, tz: string, groupBy: AnalyticsElocGroupBy) =>
    [...analyticsKeys.all(wsId), "eloc", days, tz, groupBy] as const,
  quality: (wsId: string, days: number, tz: string, repo: string) =>
    [...analyticsKeys.all(wsId), "quality", days, tz, repo] as const,
  repoActivity: (wsId: string, days: number, tz: string) =>
    [...analyticsKeys.all(wsId), "repo-activity", days, tz] as const,
  prs: (wsId: string, days: number, tz: string, repo: string) =>
    [...analyticsKeys.all(wsId), "prs", days, tz, repo] as const,
  leadTime: (wsId: string, days: number, tz: string, metric: AnalyticsLeadTimeMetric) =>
    [...analyticsKeys.all(wsId), "lead-time", days, tz, metric] as const,
  deployments: (wsId: string, days: number, tz: string) =>
    [...analyticsKeys.all(wsId), "deployments", days, tz] as const,
  lifecycle: (wsId: string, days: number, tz: string, departmentId: string | null) =>
    [...analyticsKeys.all(wsId), "lifecycle", days, tz, departmentId] as const,
  departments: (wsId: string, tz: string) =>
    [...analyticsKeys.all(wsId), "departments", tz] as const,
};

const STALE_TIME = 60 * 1000;

function isSameAnalyticsScope(
  previousKey: readonly unknown[] | undefined,
  nextKey: readonly unknown[],
): boolean {
  if (!previousKey || previousKey.length !== nextKey.length) return false;
  // Index 0 is the `analytics` namespace prefix, index 1 the wsId — both are
  // scope boundaries. Everything else (days/tz/department/metric/repo) is
  // a within-page switch that may keep the previous payload.
  return previousKey.every((part, index) => {
    if (index < 2) return Object.is(part, nextKey[index]);
    return true;
  });
}

function scopePlaceholder<T>(queryKey: readonly unknown[]) {
  return (previousData: T | undefined, previousQuery: unknown) => {
    const prevKey = (previousQuery as { queryKey?: readonly unknown[] } | undefined)?.queryKey;
    return isSameAnalyticsScope(prevKey, queryKey) ? keepPreviousData(previousData) : undefined;
  };
}

// --- Tab1 活跃度与渗透 (A1–A5) ---------------------------------------------

export function analyticsActivitySummaryOptions(
  wsId: string,
  days: number,
  tz: string,
  departmentId: string | null,
) {
  const queryKey = analyticsKeys.activitySummary(wsId, days, tz, departmentId);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getAnalyticsActivitySummary({
        days,
        tz,
        department_id: departmentId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsHeatmapOptions(
  wsId: string,
  days: number,
  tz: string,
  departmentId: string | null,
  metric: AnalyticsHeatmapMetric,
) {
  const queryKey = analyticsKeys.heatmap(wsId, days, tz, departmentId, metric);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getAnalyticsActivityHeatmap({
        days,
        tz,
        department_id: departmentId ?? undefined,
        metric,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsTopMembersOptions(
  wsId: string,
  days: number,
  tz: string,
  departmentId: string | null,
) {
  const queryKey = analyticsKeys.topMembers(wsId, days, tz, departmentId);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getAnalyticsTopMembers({
        days,
        tz,
        department_id: departmentId ?? undefined,
        limit: 10,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsAdoptionSummaryOptions(
  wsId: string,
  days: number,
  tz: string,
  departmentId: string | null,
) {
  const queryKey = analyticsKeys.adoptionSummary(wsId, days, tz, departmentId);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getAnalyticsAdoptionSummary({
        days,
        tz,
        department_id: departmentId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsAdoptionTrendOptions(
  wsId: string,
  days: number,
  tz: string,
  departmentId: string | null,
) {
  const queryKey = analyticsKeys.adoptionTrend(wsId, days, tz, departmentId);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getAnalyticsAdoptionTrend({
        days,
        tz,
        department_id: departmentId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

// --- Tab2 Agent 效能 (B1–B6) ------------------------------------------------

export function analyticsFunnelOptions(
  wsId: string,
  days: number,
  tz: string,
  departmentId: string | null,
) {
  const queryKey = analyticsKeys.funnel(wsId, days, tz, departmentId);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getAnalyticsFunnel({
        days,
        tz,
        department_id: departmentId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsAgentPerformanceOptions(wsId: string, days: number, tz: string) {
  const queryKey = analyticsKeys.agentPerformance(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsAgentPerformance({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsTopAgentsOptions(wsId: string, days: number, tz: string) {
  const queryKey = analyticsKeys.topAgents(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsTopAgents({ days, tz, limit: 10 }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsSkillsOverviewOptions(wsId: string, days: number, tz: string) {
  const queryKey = analyticsKeys.skillsOverview(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsSkillsOverview({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsCollaborationSummaryOptions(wsId: string, days: number, tz: string) {
  const queryKey = analyticsKeys.collaborationSummary(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsCollaborationSummary({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsBlockersOptions(wsId: string, days: number, tz: string) {
  const queryKey = analyticsKeys.blockers(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsBlockers({ days, tz, limit: 20 }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

// --- Tab3 Git 贡献 (G1–G4) --------------------------------------------------

export function analyticsElocOptions(
  wsId: string,
  days: number,
  tz: string,
  groupBy: AnalyticsElocGroupBy,
) {
  const queryKey = analyticsKeys.eloc(wsId, days, tz, groupBy);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsEloc({ days, tz, group_by: groupBy }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsQualityOptions(
  wsId: string,
  days: number,
  tz: string,
  repo: string,
) {
  const queryKey = analyticsKeys.quality(wsId, days, tz, repo);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsQuality({ days, tz, repo }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsRepoActivityOptions(wsId: string, days: number, tz: string) {
  const queryKey = analyticsKeys.repoActivity(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsRepoActivity({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsPrsOptions(
  wsId: string,
  days: number,
  tz: string,
  repo: string,
) {
  const queryKey = analyticsKeys.prs(wsId, days, tz, repo);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsPrs({ days, tz, repo }),
    enabled: !!wsId && repo.length > 0,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

// --- Tab4 DORA (D1–D2) ------------------------------------------------------

export function analyticsLeadTimeOptions(
  wsId: string,
  days: number,
  tz: string,
  metric: AnalyticsLeadTimeMetric,
) {
  const queryKey = analyticsKeys.leadTime(wsId, days, tz, metric);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsLeadTime({ days, tz, metric }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsDeploymentsOptions(wsId: string, days: number, tz: string) {
  const queryKey = analyticsKeys.deployments(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsDeployments({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

// --- Tab1 ⑥⑦⑧ 身份与部门 (L1–L2) --------------------------------------------

export function analyticsLifecycleOptions(
  wsId: string,
  days: number,
  tz: string,
  departmentId: string | null,
) {
  const queryKey = analyticsKeys.lifecycle(wsId, days, tz, departmentId);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getAnalyticsLifecycle({
        days,
        tz,
        department_id: departmentId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: scopePlaceholder(queryKey),
  });
}

export function analyticsDepartmentsOptions(wsId: string, tz: string) {
  const queryKey = analyticsKeys.departments(wsId, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getAnalyticsDepartments({ tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}
