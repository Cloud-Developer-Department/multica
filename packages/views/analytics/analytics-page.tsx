"use client";

// Engineering analytics platform — `/{slug}/analytics` (CLO-240).
//
// Four tabs (活跃度与渗透 / Agent效能 / Git贡献 / DORA), all live data
// against `/api/analytics/*` (CLO-228 API contract v2.0). A single toolbar
// drives every module: time window (7/30/90/180 days), optional department
// filter (rendered only when L2 `source_status.ready=true`, E18), and the
// viewing timezone. Department selection only refetches Tab1 (A1–A5) and
// Tab2 (B1); Tab3/Tab4 never accept a department param (PRD §4.6).

import { useMemo, useState } from "react";
import { Gauge } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  analyticsDepartmentsOptions,
} from "@multica/core/analytics-dashboard";
import { useViewingTimezone } from "../common/use-viewing-timezone";
import { PageHeader } from "../layout/page-header";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { Segmented } from "./components/segmented";
import { Tab1Adoption } from "./tab1-adoption";
import { Tab2Agent } from "./tab2-agent";
import { Tab3Git } from "./tab3-git";
import { Tab4Dora } from "./tab4-dora";

export type AnalyticsDays = 7 | 30 | 90 | 180;

export type AnalyticsTab = "adoption" | "agent" | "git" | "dora";

const DAYS_OPTIONS: readonly { label: string; value: AnalyticsDays }[] = [
  { label: "7d", value: 7 },
  { label: "30d", value: 30 },
  { label: "90d", value: 90 },
  { label: "180d", value: 180 },
];

const TAB_KEYS: readonly AnalyticsTab[] = ["adoption", "agent", "git", "dora"];

const ALL_DEPARTMENTS = "all";

export function AnalyticsPage() {
  const { t } = useT("analytics");
  const wsId = useWorkspaceId();
  const tz = useViewingTimezone();
  const navigation = useNavigation();

  const [days, setDays] = useState<AnalyticsDays>(30);
  const [departmentId, setDepartmentId] = useState<string>(ALL_DEPARTMENTS);

  const activeTabParam = navigation.searchParams.get("tab");
  const activeTab: AnalyticsTab = TAB_KEYS.includes(activeTabParam as AnalyticsTab)
    ? (activeTabParam as AnalyticsTab)
    : "adoption";

  // L2 — drives the department filter + Tab1 ⑥⑦⑧ conditional blocks.
  const departmentsQuery = useQuery(analyticsDepartmentsOptions(wsId, tz));
  const departments = departmentsQuery.data;
  const departmentsReady = departments?.source_status.ready === true;
  const departmentList = departments?.items ?? [];

  const effectiveDepartment =
    departmentId === ALL_DEPARTMENTS || !departmentsReady ? null : departmentId;

  const handleTabChange = (next: string) => {
    if (next === activeTab) return;
    const url = new URL(navigation.pathname, "http://local");
    url.searchParams.set("tab", next);
    navigation.push(`${url.pathname}?${url.searchParams.toString()}`);
  };

  const departmentLabel = useMemo(() => {
    if (!effectiveDepartment) return null;
    return departmentList.find((d) => d.department_id === effectiveDepartment)?.name ?? null;
  }, [effectiveDepartment, departmentList]);

  const tabOptions = useMemo(
    () => [
      { label: t(($) => $.page.tab_adoption), value: "adoption" as const },
      { label: t(($) => $.page.tab_agent), value: "agent" as const },
      { label: t(($) => $.page.tab_git), value: "git" as const },
      { label: t(($) => $.page.tab_dora), value: "dora" as const },
    ],
    [t],
  );

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="h-auto min-h-12 flex-wrap justify-between gap-y-1.5 px-5 py-1.5 sm:py-0">
        <div className="flex min-w-0 items-center gap-2">
          <Gauge className="h-4 w-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-medium">{t(($) => $.page.title)}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Segmented
            value={days}
            onChange={setDays}
            options={DAYS_OPTIONS.map((o) => ({
              label: t(($) => $.page.range[o.label as keyof typeof $.page.range]),
              value: o.value,
            }))}
          />
          {departmentsReady ? (
            <DepartmentFilter
              items={departmentList}
              value={departmentId}
              onChange={setDepartmentId}
            />
          ) : null}
        </div>
      </PageHeader>

      <div className="shrink-0 border-b px-5">
        <Tabs value={activeTab} onValueChange={handleTabChange}>
          <TabsList variant="line">
            {tabOptions.map((o) => (
              <TabsTrigger key={o.value} value={o.value}>
                {o.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-6xl space-y-5 p-6">
          {activeTab === "adoption" ? (
            <Tab1Adoption
              wsId={wsId}
              days={days}
              tz={tz}
              departmentId={effectiveDepartment}
              departmentLabel={departmentLabel}
              departmentsReady={departmentsReady}
              onDepartmentChange={(id) => setDepartmentId(id)}
            />
          ) : activeTab === "agent" ? (
            <Tab2Agent
              wsId={wsId}
              days={days}
              tz={tz}
              departmentId={effectiveDepartment}
            />
          ) : activeTab === "git" ? (
            <Tab3Git wsId={wsId} days={days} tz={tz} />
          ) : (
            <Tab4Dora wsId={wsId} days={days} tz={tz} />
          )}
        </div>
      </div>
    </div>
  );
}

function DepartmentFilter({
  items,
  value,
  onChange,
}: {
  items: { department_id: string; name: string }[];
  value: string;
  onChange: (v: string) => void;
}) {
  const { t } = useT("analytics");
  const allLabel = t(($) => $.page.all_departments);
  const selected = items.find((d) => d.department_id === value);
  const selectedTitle = value === ALL_DEPARTMENTS ? allLabel : selected?.name ?? allLabel;

  return (
    <Select
      items={[
        { value: ALL_DEPARTMENTS, label: allLabel },
        ...items.map((d) => ({ value: d.department_id, label: d.name })),
      ]}
      value={value}
      onValueChange={(v) => onChange(v ?? ALL_DEPARTMENTS)}
    >
      <SelectTrigger size="sm" className="min-w-[140px]">
        <SelectValue>
          {() => (
            <span className="flex items-center gap-1.5">
              <span className="text-xs text-muted-foreground">
                {t(($) => $.page.department_filter)}
              </span>
              <span className="truncate font-medium">{selectedTitle}</span>
            </span>
          )}
        </SelectValue>
      </SelectTrigger>
      <SelectContent align="start" className="max-h-72">
        <SelectItem value={ALL_DEPARTMENTS}>
          <span className="truncate">{allLabel}</span>
        </SelectItem>
        {items.map((d) => (
          <SelectItem key={d.department_id} value={d.department_id}>
            <span className="truncate">{d.name}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
