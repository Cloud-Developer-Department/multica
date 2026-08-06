import { describe, it, expect, beforeEach, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

// The monitoring page derives every query key from (workspace, range, viewer
// timezone). This test pins the timezone + range chain: when the stored
// timezone or the selected range changes, the keys must change so TanStack
// Query refetches under the new scope. It also locks the six KPI tiles, the
// six module cards, and the single-module / full-page error states.

const queryKeys = vi.hoisted(() => [] as unknown[][]);
const tzRef = vi.hoisted(() => ({ current: "UTC" as string }));
// Module-level failures: keyed by queryKey[2] (the module segment).
const errorRef = vi.hoisted(() => ({ current: new Set<string>() }));
const emptyRef = vi.hoisted(() => ({ current: new Set<string>() }));

const NOW_ISO = "2026-08-04T07:00:00.000Z";

// Successful-but-empty shapes, so the module empty states are reachable.
function emptyFixtureFor(kind: string) {
  switch (kind) {
    case "issue-distribution":
      return { total: 0, status_counts: {}, projects: [] };
    case "activity":
      return { agent_workload: [], team_activity: [] };
    case "comments":
      return { total: 0, today: 0, series: [] };
    case "completion":
      return {
        completion_rate: 0,
        completion_delta: 0,
        delay_rate: null,
        delay_delta: null,
        has_due_date_tasks: false,
        trend: [],
      };
    default:
      return [];
  }
}

function fixtureFor(kind: string) {
  switch (kind) {    case "issue-distribution":
      return {
        total: 100,
        status_counts: {
          in_progress: 30,
          in_review: 5,
          done: 40,
          blocked: 5,
          todo: 20,
        },
        projects: [
          { id: "p1", name: "Proj A", total: 60, status_counts: { done: 40, in_progress: 20 } },
          { id: "p2", name: "Proj B", total: 40, status_counts: { in_progress: 10, done: 0 } },
        ],
      };
    case "activity":
      return {
        agent_workload: [{ id: "a1", name: "Agent One", load: 12, activity: 30 }],
        team_activity: [{ id: "s1", name: "Squad One", load: 5, activity: 20 }],
      };
    case "comments":
      return {
        total: 240,
        today: 12,
        series: [
          { time: "2026-08-04T01:00:00.000Z", count: 3 },
          { time: "2026-08-04T02:00:00.000Z", count: 5 },
        ],
      };
    case "completion":
      return {
        completion_rate: 72.5,
        completion_delta: 2.4,
        delay_rate: 12.3,
        delay_delta: -1.2,
        has_due_date_tasks: true,
        trend: [
          { time: "2026-08-03T00:00:00.000Z", completion: 70, delay: 10 },
          { time: "2026-08-04T00:00:00.000Z", completion: 75, delay: 8 },
        ],
      };
    case "list":
      return [
        {
          id: "r1",
          name: "Codex (host)",
          custom_name: null,
          provider: "codex",
          runtime_mode: "local",
          status: "online",
          last_seen_at: NOW_ISO,
        },
        {
          id: "r2",
          name: "claude (host)",
          custom_name: null,
          provider: "claude",
          runtime_mode: "cloud",
          status: "offline",
          last_seen_at: "2026-08-01T07:00:00.000Z",
        },
      ];
    default:
      return undefined;
  }
}

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (opts: { queryKey: unknown[] }) => {
      queryKeys.push(opts.queryKey);
      const kind = opts.queryKey[2];
      if (errorRef.current.has(String(kind))) {
        return {
          data: undefined,
          isError: true,
          isPending: false,
          isLoading: false,
          isSuccess: false,
          dataUpdatedAt: 0,
          refetch: vi.fn().mockResolvedValue({ data: undefined }),
        };
      }
      if (emptyRef.current.has(String(kind))) {
        const data = emptyFixtureFor(String(kind));
        return {
          data,
          isError: false,
          isPending: false,
          isLoading: false,
          isSuccess: true,
          dataUpdatedAt: NOW_ISO.length,
          refetch: vi.fn().mockResolvedValue({ data }),
        };
      }
      return {
        data: fixtureFor(String(kind)),
        isError: false,
        isPending: false,
        isLoading: false,
        isSuccess: true,
        dataUpdatedAt: NOW_ISO.length,
        refetch: vi.fn().mockResolvedValue({ data: fixtureFor(String(kind)) }),
      };
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/acme/agents/${id}`,
    squadDetail: (id: string) => `/acme/squads/${id}`,
    runtimeDetail: (id: string) => `/acme/runtimes/${id}`,
  }),
}));

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: { timezone: string } | null };
  const state = (): AuthState => ({ user: { timezone: tzRef.current } });
  const useAuthStore = Object.assign(
    (sel?: (s: AuthState) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});

import { MonitoringDashboardPage } from "./monitoring-dashboard";

const navAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/dashboard",
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => `https://example.test${path}`,
};

function renderDashboard() {
  return renderWithI18n(
    <NavigationProvider value={navAdapter}>
      <MonitoringDashboardPage />
    </NavigationProvider>,
  );
}

function monitoringKeys() {
  return queryKeys.filter((k) => k[0] === "monitoring");
}

describe("MonitoringDashboardPage — scope drives the query keys", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    errorRef.current.clear();
    emptyRef.current.clear();
    tzRef.current = "UTC";
    cleanup();
  });

  it("uses the stored timezone and default 7-day range in every key", () => {
    tzRef.current = "America/Los_Angeles";
    renderDashboard();

    const keys = monitoringKeys();
    expect(keys.length).toBeGreaterThan(0);
    for (const key of keys) {
      expect(key[key.length - 1]).toBe("America/Los_Angeles");
    }
    expect(keys.every((k) => k[3] === 7)).toBe(true);
  });

  it("flips the days segment when the range selector changes", async () => {
    const user = userEvent.setup();
    renderDashboard();

    await user.click(screen.getByRole("button", { name: "Last 30 days" }));

    // The initial render captured days=7 keys; after the click a fresh render
    // appends days=30 keys. The last monitoring key reflects the new range.
    const keys = monitoringKeys();
    const last = keys[keys.length - 1]!;
    expect(last[3]).toBe(30);
  });
});

describe("MonitoringDashboardPage — layout", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    errorRef.current.clear();
    emptyRef.current.clear();
    tzRef.current = "UTC";
    cleanup();
  });

  it("renders all six KPI tiles with fixture values", () => {
    renderDashboard();

    // KPI labels can repeat in chart legends, so scope to the KPI section
    // (aria-label on <section> → role region) before asserting.
    const kpi = within(screen.getByRole("region", { name: "Key metrics" }));

    expect(kpi.getByText("Total issues")).toBeInTheDocument();
    expect(kpi.getByText("In progress")).toBeInTheDocument();
    expect(kpi.getByText("Completion rate")).toBeInTheDocument();
    expect(kpi.getByText("Delay rate")).toBeInTheDocument();
    expect(kpi.getByText("Online runtimes")).toBeInTheDocument();
    expect(kpi.getByText("Online rate")).toBeInTheDocument();

    // 总 Issue = 100, 进行中 = 30, 完成率 72.5 → "73%", 延期率 12.3 → "12%",
    // 在线运行时 = 1, 在线率 50 → "50%".
    expect(kpi.getByText("100")).toBeInTheDocument();
    expect(kpi.getByText("73%")).toBeInTheDocument();
    expect(kpi.getByText("12%")).toBeInTheDocument();
  });

  it("renders all six module cards and the runtime table summary", () => {
    renderDashboard();

    const modules = within(screen.getByRole("region", { name: "Data modules" }));
    expect(modules.getByText("Issue status distribution")).toBeInTheDocument();
    expect(modules.getByText("Project progress")).toBeInTheDocument();
    expect(modules.getByText("Agent workload")).toBeInTheDocument();
    expect(modules.getByText("Team activity")).toBeInTheDocument();
    expect(modules.getByText("Comment activity")).toBeInTheDocument();
    expect(modules.getByText("Completion / delay trend")).toBeInTheDocument();

    expect(screen.getByText("Runtime status")).toBeInTheDocument();
    expect(screen.getByText("1 online / 2 total")).toBeInTheDocument();
  });

  it("shows a module-level error with a retry action for a failed module", () => {
    errorRef.current.add("comments");
    renderDashboard();

    const retryButtons = screen.getAllByRole("button", { name: "Retry" });
    expect(retryButtons.length).toBeGreaterThan(0);
    expect(screen.getAllByText("Failed to load data").length).toBeGreaterThan(0);
    // Other modules still render their titles.
    expect(screen.getByText("Issue status distribution")).toBeInTheDocument();
  });

  it("shows the full-page error when every data module fails", () => {
    errorRef.current.add("issue-distribution");
    errorRef.current.add("activity");
    errorRef.current.add("comments");
    errorRef.current.add("completion");
    renderDashboard();

    expect(screen.getByText("Failed to load data")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reload" })).toBeInTheDocument();
    // No fake KPI values when everything is down.
    expect(screen.queryByText("Issue status distribution")).toBeNull();
  });
});

describe("MonitoringDashboardPage — empty module state", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    errorRef.current.clear();
    emptyRef.current.clear();
    tzRef.current = "UTC";
    cleanup();
  });

  it("renders the module empty text when a module has no data", () => {
    emptyRef.current.add("comments");
    renderDashboard();

    expect(screen.getByText("No comment data yet")).toBeInTheDocument();
  });
});
