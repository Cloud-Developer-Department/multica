import type { Issue } from "@multica/core/types";
import type { ChildProgress } from "./list-row";

/**
 * Front-end tree model for the board's parent/sub-issue nesting.
 *
 * The board keeps its flat column mirror (`Record<groupId, string[]>` —
 * flattened pre-order id sequence) so `useDragSettle` stays untouched. The
 * per-card tree facts (depth, has-children, collapsed) live in a separate
 * `nodeInfo` map the column resolves by id when rendering.
 */

export interface BoardNodeInfo {
  depth: number;
  hasChildren: boolean;
  collapsed: boolean;
}

/** Group a flat issue list into direct children per parent, position-sorted. */
export function buildChildrenMap(issues: readonly Issue[]): Map<string, Issue[]> {
  const map = new Map<string, Issue[]>();
  for (const issue of issues) {
    if (!issue.parent_issue_id) continue;
    const bucket = map.get(issue.parent_issue_id);
    if (bucket) bucket.push(issue);
    else map.set(issue.parent_issue_id, [issue]);
  }
  for (const bucket of map.values()) {
    bucket.sort(
      (a, b) => a.position - b.position || a.created_at.localeCompare(b.created_at),
    );
  }
  return map;
}

/** All descendant ids of `rootId` (direct and indirect), DFS pre-order. */
export function collectSubtreeIds(
  childrenMap: ReadonlyMap<string, Issue[]>,
  rootId: string,
): string[] {
  const ids: string[] = [];
  const visit = (id: string) => {
    for (const child of childrenMap.get(id) ?? []) {
      ids.push(child.id);
      visit(child.id);
    }
  };
  visit(rootId);
  return ids;
}

/**
 * Flatten one or more root issues into the pre-order id sequence the column
 * renders, populating `nodeInfo` with each card's depth / has-children /
 * collapsed flags. Collapsed parents stop recursion so their children drop
 * out of the flattened sequence (they still count as "has children" so the
 * chevron renders).
 */
export function flattenBoardTree(
  roots: readonly Issue[],
  childrenMap: ReadonlyMap<string, Issue[]>,
  childProgressMap: ReadonlyMap<string, ChildProgress>,
  collapsedSet: ReadonlySet<string>,
): { ids: string[]; nodeInfo: Map<string, BoardNodeInfo> } {
  const ids: string[] = [];
  const nodeInfo = new Map<string, BoardNodeInfo>();
  const visit = (issue: Issue, depth: number) => {
    const hasChildren =
      (childProgressMap.get(issue.id)?.total ?? 0) > 0 ||
      (childrenMap.get(issue.id)?.length ?? 0) > 0;
    const collapsed = hasChildren && collapsedSet.has(issue.id);
    nodeInfo.set(issue.id, { depth, hasChildren, collapsed });
    ids.push(issue.id);
    if (!collapsed) {
      for (const child of childrenMap.get(issue.id) ?? []) {
        visit(child, depth + 1);
      }
    }
  };
  for (const root of roots) visit(root, 0);
  return { ids, nodeInfo };
}
