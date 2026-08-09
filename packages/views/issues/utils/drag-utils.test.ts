import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import {
  getIssueGroupId,
  getMoveAnchors,
  getMoveUpdates,
  insertIdByPosition,
  issueMatchesGroup,
  propertyGroupId,
} from "./drag-utils";
import {
  buildBoardTreeColumns,
  computeBlockPosition,
  getSubtreeBlock,
  getSubtreeMoveAnchors,
  getSubtreeSyncUpdates,
  moveBlockInto,
  moveBlockWithin,
} from "./drag-utils";
import type { BoardColumnGroup } from "../components/board-column";
import type { BoardNodeInfo } from "../components/board-tree-model";

function mk(id: string, position: number, parent: string | null = null): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status: parent ? "todo" : "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: parent,
    project_id: null,
    position,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    labels: [],
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  };
}

function mapOf(...issues: Issue[]): Map<string, Issue> {
  return new Map(issues.map((i) => [i.id, i]));
}

describe("getMoveAnchors", () => {
  it("derives relative neighbors from the optimistic order", () => {
    expect(getMoveAnchors(["a", "moving", "b"], "moving")).toEqual({
      before_id: "a",
      after_id: "b",
    });
    expect(getMoveAnchors(["moving"], "moving")).toEqual({
      before_id: null,
      after_id: null,
    });
  });
});

describe("insertIdByPosition", () => {
  it("inserts the id at its position-sorted slot", () => {
    const map = mapOf(mk("a", 1), mk("c", 3), mk("b", 2));
    expect(insertIdByPosition(["a", "c"], "b", 2, map)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("appends when the position is the largest", () => {
    const map = mapOf(mk("a", 1), mk("z", 9));
    expect(insertIdByPosition(["a"], "z", 9, map)).toEqual(["a", "z"]);
  });

  it("prepends when the position is the smallest", () => {
    const map = mapOf(mk("b", 2), mk("a", 1));
    expect(insertIdByPosition(["b"], "a", 1, map)).toEqual(["a", "b"]);
  });

  it("appends into an empty target column", () => {
    const map = mapOf(mk("a", 5));
    expect(insertIdByPosition([], "a", 5, map)).toEqual(["a"]);
  });

  it("matches insertByPosition ordering so the settle rebuild is a no-op", () => {
    // Same scenario the board's optimistic drop and the cache patch both apply:
    // landing a card between two neighbours must produce the same order in the
    // id list (board) and the issue list (cache).
    const map = mapOf(mk("x", 1), mk("y", 3), mk("moved", 2));
    expect(insertIdByPosition(["x", "y"], "moved", 2, map)).toEqual([
      "x",
      "moved",
      "y",
    ]);
  });
});

describe("property grouping", () => {
  const propertyId = "prop-env";
  const withValue = { id: "A", properties: { [propertyId]: "opt-staging" } } as unknown as Issue;
  const withoutValue = { id: "B", properties: {} } as unknown as Issue;

  it("getIssueGroupId buckets by option id, no-value issues into the none column", () => {
    expect(getIssueGroupId(withValue, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, "opt-staging"),
    );
    expect(getIssueGroupId(withoutValue, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, null),
    );
  });

  it("issueMatchesGroup distinguishes option and no-value columns", () => {
    const optionColumn = { id: "c1", title: "Staging", propertyId, propertyOptionId: "opt-staging" };
    const noneColumn = { id: "c2", title: "No value", propertyId, propertyOptionId: null };
    expect(issueMatchesGroup(withValue, optionColumn)).toBe(true);
    expect(issueMatchesGroup(withValue, noneColumn)).toBe(false);
    expect(issueMatchesGroup(withoutValue, noneColumn)).toBe(true);
  });

  it("unknown option values bucket into the none column when the catalog is known", () => {
    const stale = { id: "C", properties: { [propertyId]: "opt-deleted" } } as unknown as Issue;
    const known = new Set(["opt-staging"]);
    expect(getIssueGroupId(stale, `property:${propertyId}`, known)).toBe(
      propertyGroupId(propertyId, null),
    );
    // Without the catalog, the raw bucket is preserved (caller may still map it).
    expect(getIssueGroupId(stale, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, "opt-deleted"),
    );
  });

  it("getMoveUpdates for property columns only carries position", () => {
    expect(getMoveUpdates({ id: "c1", title: "Staging", propertyId, propertyOptionId: "opt-staging" }, 5)).toEqual({ position: 5 });
  });
});

describe("buildBoardTreeColumns", () => {
  const statusGroups: BoardColumnGroup[] = [
    { id: "status:todo", title: "Todo", status: "todo" },
    { id: "status:done", title: "Done", status: "done" },
  ];
  const progressOf = (total: number) =>
    new Map<string, { done: number; total: number }>([["p1", { done: 0, total }]]);

  it("nests children under their parent regardless of the child's own status", () => {
    const childDone = mk("c1", 2, "p1");
    childDone.status = "done"; // child's own status differs from the parent's column
    const issues = [mk("p1", 1), childDone, mk("root2", 3)];
    const { columns, nodeInfo } = buildBoardTreeColumns(
      issues,
      statusGroups,
      "status",
      new Set(),
      progressOf(1),
    );
    // p1 + its done child land in the todo column (follow-the-parent); the
    // done child does NOT also appear in the done column.
    expect(columns["status:todo"]).toEqual(["p1", "c1", "root2"]);
    expect(columns["status:done"]).toEqual([]);
    expect(info(nodeInfo, "p1").depth).toBe(0);
    expect(info(nodeInfo, "c1").depth).toBe(1);
  });

  it("collapsed parents drop their children from the flattened sequence", () => {
    const issues = [mk("p1", 1), mk("c1", 2, "p1"), mk("root2", 3)];
    const { columns } = buildBoardTreeColumns(
      issues,
      statusGroups,
      "status",
      new Set(["p1"]),
      progressOf(1),
    );
    expect(columns["status:todo"]).toEqual(["p1", "root2"]);
  });

  it("orphans (parent not in view) become roots bucketed by their own value", () => {
    const issues = [mk("orphan", 1, "missing-parent")];
    const { columns } = buildBoardTreeColumns(
      issues,
      statusGroups,
      "status",
      new Set(),
      new Map(),
    );
    expect(columns["status:todo"]).toEqual(["orphan"]);
  });

  it("flags hasChildren from childProgressMap even when children are unloaded", () => {
    const { nodeInfo } = buildBoardTreeColumns(
      [mk("p1", 1)],
      statusGroups,
      "status",
      new Set(),
      progressOf(3),
    );
    expect(info(nodeInfo, "p1").hasChildren).toBe(true);
  });
});

describe("subtree block math", () => {
  const nodeInfo = new Map<string, BoardNodeInfo>([
    ["root", { depth: 0, hasChildren: true, collapsed: false }],
    ["a", { depth: 1, hasChildren: true, collapsed: false }],
    ["a1", { depth: 2, hasChildren: false, collapsed: false }],
    ["a2", { depth: 2, hasChildren: false, collapsed: false }],
    ["b", { depth: 1, hasChildren: false, collapsed: false }],
  ]);
  const seq = ["root", "a", "a1", "a2", "b"];

  it("getSubtreeBlock returns the card plus its flattened descendants", () => {
    expect(getSubtreeBlock(seq, "root", nodeInfo)).toEqual(["root", "a", "a1", "a2", "b"]);
    expect(getSubtreeBlock(seq, "a", nodeInfo)).toEqual(["a", "a1", "a2"]);
    expect(getSubtreeBlock(seq, "a2", nodeInfo)).toEqual(["a2"]);
    expect(getSubtreeBlock(seq, "b", nodeInfo)).toEqual(["b"]);
  });

  it("collapsed children are excluded because they are not in the sequence", () => {
    const collapsed = new Map<string, BoardNodeInfo>([
      ["root", { depth: 0, hasChildren: true, collapsed: true }],
    ]);
    expect(getSubtreeBlock(["root"], "root", collapsed)).toEqual(["root"]);
  });

  it("getSubtreeMoveAnchors uses the ids outside the block boundaries", () => {
    expect(getSubtreeMoveAnchors(seq, ["a", "a1", "a2"])).toEqual({
      before_id: "root",
      after_id: "b",
    });
    expect(getSubtreeMoveAnchors(seq, ["root"])).toEqual({ before_id: null, after_id: "a" });
    expect(getSubtreeMoveAnchors(seq, ["b"])).toEqual({ before_id: "a2", after_id: null });
  });

  it("moveBlockWithin relocates a block to overId's slot", () => {
    expect(moveBlockWithin(seq, ["a", "a1", "a2"], "b")).toEqual(["root", "b", "a", "a1", "a2"]);
    expect(moveBlockWithin(seq, ["root"], "a")).toEqual(["a", "root", "a1", "a2", "b"]);
  });

  it("moveBlockWithin treats dropping a parent onto its own descendant as a no-op", () => {
    expect(moveBlockWithin(seq, ["a", "a1", "a2"], "a1")).toEqual(seq);
  });

  it("moveBlockInto inserts a block into another sequence at overId", () => {
    const target = ["x", "y"];
    expect(moveBlockInto(target, ["a", "a1", "a2"], "y")).toEqual(["x", "a", "a1", "a2", "y"]);
    expect(moveBlockInto(target, ["a"], "missing")).toEqual(["x", "y", "a"]);
  });

  it("computeBlockPosition places the parent between its outside neighbors", () => {
    const map = mapOf(mk("root", 10), mk("a", 20), mk("a1", 21), mk("a2", 22), mk("b", 30));
    // root block [root] stays first → between (nothing before) and "a" → 20 - 1
    expect(computeBlockPosition(["root", "a", "a1", "a2", "b"], ["root"], map)).toBe(19);
    // a-block [a,a1,a2] sits at the end after "b" → before(30) + 1
    expect(computeBlockPosition(["root", "b", "a", "a1", "a2"], ["a", "a1", "a2"], map)).toBe(31);
  });
});

describe("getSubtreeSyncUpdates", () => {
  it("returns status for status columns", () => {
    expect(getSubtreeSyncUpdates({ id: "s1", title: "Done", status: "done" })).toEqual({
      status: "done",
    });
  });

  it("returns assignee fields for assignee columns", () => {
    expect(
      getSubtreeSyncUpdates({
        id: "a1",
        title: "Bob",
        assigneeType: "member",
        assigneeId: "user-1",
      }),
    ).toEqual({ assignee_type: "member", assignee_id: "user-1" });
  });

  it("returns null for property columns (no cascade, R5)", () => {
    expect(
      getSubtreeSyncUpdates({ id: "c1", title: "Staging", propertyId: "prop", propertyOptionId: "opt" }),
    ).toBeNull();
  });
});

function info(map: Map<string, BoardNodeInfo>, id: string): BoardNodeInfo {
  const value = map.get(id);
  if (!value) throw new Error(`missing nodeInfo for ${id}`);
  return value;
}
