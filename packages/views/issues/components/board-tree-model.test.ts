import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import {
  buildChildrenMap,
  collectSubtreeIds,
  flattenBoardTree,
  type BoardNodeInfo,
} from "./board-tree-model";

function mk(
  id: string,
  position: number,
  parent: string | null = null,
  created = "2025-01-01T00:00:00Z",
): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status: "todo",
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
    created_at: created,
    updated_at: created,
  };
}

describe("buildChildrenMap", () => {
  it("groups direct children by parent, position-sorted", () => {
    const issues = [
      mk("p", 1),
      mk("c2", 5, "p"),
      mk("c1", 2, "p"),
      mk("g", 3, "c1"),
      mk("orphan", 4, "missing-parent"),
    ];
    const map = buildChildrenMap(issues);
    expect(map.get("p")!.map((i) => i.id)).toEqual(["c1", "c2"]);
    expect(map.get("c1")!.map((i) => i.id)).toEqual(["g"]);
    // The "orphan" child buckets under its (absent) parent id — the parent
    // just isn't in the loaded set.
    expect(map.get("missing-parent")!.map((i) => i.id)).toEqual(["orphan"]);
    expect(map.get("orphan")).toBeUndefined();
  });

  it("leaves a parent with no loaded children out of the map", () => {
    const map = buildChildrenMap([mk("a", 1), mk("b", 2)]);
    expect(map.has("a")).toBe(false);
  });
});

describe("collectSubtreeIds", () => {
  it("collects direct and indirect descendants in pre-order", () => {
    const issues = [
      mk("root", 1),
      mk("a", 2, "root"),
      mk("b", 3, "root"),
      mk("b1", 4, "b"),
      mk("b2", 5, "b"),
      mk("a1", 6, "a"),
    ];
    const map = buildChildrenMap(issues);
    expect(collectSubtreeIds(map, "root")).toEqual(["a", "a1", "b", "b1", "b2"]);
    expect(collectSubtreeIds(map, "b")).toEqual(["b1", "b2"]);
    expect(collectSubtreeIds(map, "a")).toEqual(["a1"]);
    expect(collectSubtreeIds(map, "leaf")).toEqual([]);
  });
});

describe("flattenBoardTree", () => {
  it("flattens roots and expanded children into a pre-order sequence", () => {
    const issues = [
      mk("root", 1),
      mk("a", 2, "root"),
      mk("b", 3, "root"),
      mk("b1", 4, "b"),
    ];
    const childrenMap = buildChildrenMap(issues);
    const progress = new Map<string, { done: number; total: number }>([
      ["root", { done: 1, total: 2 }],
      ["b", { done: 0, total: 1 }],
    ]);
    const { ids, nodeInfo } = flattenBoardTree(
      [mk("root", 1)],
      childrenMap,
      progress,
      new Set(),
    );
    expect(ids).toEqual(["root", "a", "b", "b1"]);
    expect(infoOf(nodeInfo, "root")).toEqual({ depth: 0, hasChildren: true, collapsed: false });
    expect(infoOf(nodeInfo, "a")).toEqual({ depth: 1, hasChildren: false, collapsed: false });
    expect(infoOf(nodeInfo, "b")).toEqual({ depth: 1, hasChildren: true, collapsed: false });
    expect(infoOf(nodeInfo, "b1")).toEqual({ depth: 2, hasChildren: false, collapsed: false });
  });

  it("stops recursion at collapsed parents but still flags them as parents", () => {
    const issues = [
      mk("root", 1),
      mk("a", 2, "root"),
      mk("b", 3, "root"),
      mk("b1", 4, "b"),
    ];
    const childrenMap = buildChildrenMap(issues);
    const progress = new Map<string, { done: number; total: number }>([
      ["root", { done: 1, total: 2 }],
      ["b", { done: 0, total: 1 }],
    ]);
    const { ids, nodeInfo } = flattenBoardTree(
      [mk("root", 1)],
      childrenMap,
      progress,
      new Set(["root"]),
    );
    expect(ids).toEqual(["root"]);
    expect(infoOf(nodeInfo, "root")).toEqual({ depth: 0, hasChildren: true, collapsed: true });
  });

  it("flags hasChildren purely from the progress map when children are unloaded", () => {
    const progress = new Map<string, { done: number; total: number }>([
      ["p", { done: 0, total: 3 }],
    ]);
    const { ids, nodeInfo } = flattenBoardTree(
      [mk("p", 1)],
      new Map(),
      progress,
      new Set(),
    );
    expect(ids).toEqual(["p"]);
    expect(infoOf(nodeInfo, "p")).toEqual({ depth: 0, hasChildren: true, collapsed: false });
  });
});

function infoOf(map: Map<string, BoardNodeInfo>, id: string): BoardNodeInfo {
  const info = map.get(id);
  if (!info) throw new Error(`missing nodeInfo for ${id}`);
  return info;
}
