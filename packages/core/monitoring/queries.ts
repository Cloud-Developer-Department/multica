import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const monitoringKeys = {
  all: (wsId: string) => ["monitoring", wsId] as const,
  issueDistribution: (wsId: string, days: number, tz: string) =>
    [...monitoringKeys.all(wsId), "issue-distribution", days, tz] as const,
  activity: (wsId: string, days: number, tz: string) =>
    [...monitoringKeys.all(wsId), "activity", days, tz] as const,
  comments: (wsId: string, days: number, tz: string) =>
    [...monitoringKeys.all(wsId), "comments", days, tz] as const,
  completion: (wsId: string, days: number, tz: string) =>
    [...monitoringKeys.all(wsId), "completion", days, tz] as const,
};

// Same 60s background refetch cadence as the usage dashboard; the monitoring
// page's auto-refresh drives an explicit refetch on top of this.
const STALE_TIME = 60 * 1000;

// Range switches keep the previous module mounted so KPI cards and charts
// transition in place instead of flashing the full-page skeleton. Scoping
// parts (workspace, timezone) are deliberately excluded: carrying data across
// those would briefly display the wrong workspace / timezone.
function isSameMonitoringScope(
  previousKey: readonly unknown[] | undefined,
  nextKey: readonly unknown[],
): boolean {
  if (!previousKey || previousKey.length !== nextKey.length) return false;
  return previousKey.every(
    (part, index) => index === 2 || Object.is(part, nextKey[index]),
  );
}

export function monitoringIssueDistributionOptions(
  wsId: string,
  days: number,
  tz: string,
) {
  const queryKey = monitoringKeys.issueDistribution(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getMonitoringIssueDistribution({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameMonitoringScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function monitoringActivityOptions(
  wsId: string,
  days: number,
  tz: string,
) {
  const queryKey = monitoringKeys.activity(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getMonitoringActivity({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameMonitoringScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function monitoringCommentsOptions(
  wsId: string,
  days: number,
  tz: string,
) {
  const queryKey = monitoringKeys.comments(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getMonitoringComments({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameMonitoringScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function monitoringCompletionOptions(
  wsId: string,
  days: number,
  tz: string,
) {
  const queryKey = monitoringKeys.completion(wsId, days, tz);
  return queryOptions({
    queryKey,
    queryFn: () => api.getMonitoringCompletion({ days, tz }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    placeholderData: (previousData, previousQuery) =>
      isSameMonitoringScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}
