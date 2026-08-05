import { describe, it, expect } from "vitest";
import {
  STATUS_CHART_COLOR,
  STATUS_TAG_CLASS,
  donutSlices,
  bucketTopProjects,
  topN,
  formatPercent,
  formatDelta,
  MISSING,
  OTHER_PROJECTS_ID,
  formatCommentSeriesLabel,
} from "./utils";

describe("status → colour mapping", () => {
  it("pins the design-spec §2.1 colours", () => {
    expect(STATUS_CHART_COLOR).toEqual({
      todo: "#64748B",
      in_progress: "#3B5BFF",
      in_review: "#8B5CF6",
      done: "#16A34A",
      blocked: "#EF4444",
      cancelled: "#9CA3AF",
      backlog: "#D1D5DB",
    });
  });

  it("gives every known status a tag class", () => {
    for (const status of Object.keys(STATUS_CHART_COLOR)) {
      expect(STATUS_TAG_CLASS[status]).toBeTruthy();
    }
  });
});

describe("donutSlices", () => {
  it("orders slices canonically and drops zeros", () => {
    const slices = donutSlices({ done: 5, in_progress: 3 });
    expect(slices.map((s) => s.status)).toEqual(["in_progress", "done"]);
    expect(slices).toEqual([
      { status: "in_progress", count: 3, color: "#3B5BFF" },
      { status: "done", count: 5, color: "#16A34A" },
    ]);
  });

  it("returns an empty list for an empty bag", () => {
    expect(donutSlices({})).toEqual([]);
  });
});

describe("bucketTopProjects", () => {
  it("keeps the top N and merges the tail into Other", () => {
    const projects = Array.from({ length: 10 }, (_, i) => ({
      id: `p-${i}`,
      name: `Project ${i}`,
      total: i + 1,
      status_counts: { done: i + 1 },
    }));
    const rows = bucketTopProjects(projects, 8);

    expect(rows).toHaveLength(9);
    expect(rows.slice(0, 8).map((r) => r.name)).toEqual([
      "Project 9",
      "Project 8",
      "Project 7",
      "Project 6",
      "Project 5",
      "Project 4",
      "Project 3",
      "Project 2",
    ]);
    const other = rows[8]!;
    expect(other.id).toBe(OTHER_PROJECTS_ID);
    // Projects 1 and 0 sum to 3.
    expect(other.total).toBe(3);
    expect(other.counts.done).toBe(3);
  });

  it("returns projects unchanged when none fall outside the cap", () => {
    const projects = [
      { id: "a", name: "A", total: 2, status_counts: { done: 2 } },
      { id: "b", name: "B", total: 1, status_counts: { done: 1 } },
    ];
    expect(bucketTopProjects(projects, 8)).toHaveLength(2);
  });
});

describe("topN", () => {
  it("caps a long list without mutating it", () => {
    const rows = [1, 2, 3, 4, 5];
    expect(topN(rows, 3)).toEqual([1, 2, 3]);
    expect(rows).toHaveLength(5);
  });

  it("clamps negative limits to zero", () => {
    expect(topN([1, 2], -1)).toEqual([]);
  });
});

describe("formatting", () => {
  it("formats percentages as whole percents", () => {
    expect(formatPercent(37.4)).toBe("37%");
    expect(formatPercent(0)).toBe("0%");
    expect(formatPercent(NaN)).toBe(MISSING);
  });

  it("formats signed deltas", () => {
    expect(formatDelta(2.4)).toBe("+2.4%");
    expect(formatDelta(-1.2)).toBe("−1.2%");
    expect(formatDelta(0)).toBe("+0.0%");
    expect(formatDelta(null)).toBe(MISSING);
    expect(formatDelta(undefined)).toBe(MISSING);
  });

  it("labels hourly vs daily comment buckets", () => {
    const hourly = {
      time: "2026-08-04T14:00:00+00:00",
      count: 5,
    };
    const daily = {
      time: "2026-08-04T00:00:00+00:00",
      count: 5,
    };
    // UTC viewer: hour 14 → "14:00"; day 08-04 → "08-04".
    expect(formatCommentSeriesLabel(hourly, true, "UTC")).toBe("14:00");
    expect(formatCommentSeriesLabel(daily, false, "UTC")).toBe("08-04");
  });
});
