import {
  pointerWithin,
  closestCenter,
  type CollisionDetection,
} from "@dnd-kit/core";
import type { Issue, IssueAssigneeType, IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import type { IssueGrouping } from "@multica/core/issues/stores/view-store";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import type { BoardColumnGroup } from "../components/board-column";
import type { ChildProgress } from "../components/list-row";
import {
  buildChildrenMap,
  flattenBoardTree,
  type BoardNodeInfo,
} from "../components/board-tree-model";

export type DragMoveTargetUpdates = Pick<
  UpdateIssueRequest,
  "status" | "assignee_type" | "assignee_id" | "position"
>;

export type DragMoveUpdates = DragMoveTargetUpdates & {
  before_id: string | null;
  after_id: string | null;
  /**
   * Set to `null` when a dragged sub-issue is dropped into another column and
   * detaches from its parent (becomes a top-level task, D4). Omitted for
   * ordinary moves.
   */
  parent_issue_id?: string | null;
};

const UNASSIGNED_GROUP_ID = "assignee:unassigned";

export function makeKanbanCollision(groupIds: Set<string>): CollisionDetection {
  return (args) => {
    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      const items = pointer.filter((c) => !groupIds.has(c.id as string));
      if (items.length > 0) return items;
      return pointer;
    }
    return closestCenter(args);
  };
}

export function statusGroupId(status: IssueStatus): string {
  return `status:${status}`;
}

export function propertyGroupId(propertyId: string, optionId: string | null): string {
  return `property:${propertyId}:${optionId ?? "none"}`;
}

export function assigneeGroupId(
  type: IssueAssigneeType | null,
  id: string | null,
): string {
  return type && id ? `assignee:${type}:${id}` : UNASSIGNED_GROUP_ID;
}

export function getIssueGroupId(
  issue: Issue,
  grouping: IssueGrouping,
  knownOptionIds?: ReadonlySet<string>,
): string {
  if (grouping === "status") return statusGroupId(issue.status);
  const propertyId = propertyIdFromViewKey(grouping);
  if (propertyId) {
    const value = issue.properties?.[propertyId];
    let optionId = typeof value === "string" ? value : null;
    // A value referencing an option no longer in the definition (removed
    // before the in-use guard existed, or by a newer server) must bucket
    // into the No-value column — an unmatched column id would silently drop
    // the issue from the board.
    if (optionId !== null && knownOptionIds && !knownOptionIds.has(optionId)) {
      optionId = null;
    }
    return propertyGroupId(propertyId, optionId);
  }
  return assigneeGroupId(issue.assignee_type, issue.assignee_id);
}

export function buildColumns(
  issues: Issue[],
  groups: BoardColumnGroup[],
  grouping: IssueGrouping,
  knownOptionIds?: ReadonlySet<string>,
): Record<string, string[]> {
  const cols: Record<string, string[]> = {};
  for (const group of groups) cols[group.id] = [];
  for (const issue of issues) {
    const gid = getIssueGroupId(issue, grouping, knownOptionIds);
    if (cols[gid]) cols[gid].push(issue.id);
  }
  return cols;
}

/**
 * Tree-ified column build for the board. Unlike {@link buildColumns} (which
 * buckets every issue by its OWN group value), children are pulled under
 * their parent's column: a child renders wherever its parent does, regardless
 * of the child's own status/assignee/property (Board's "follow the parent
 * column" semantics). Issues whose parent is absent from the loaded view are
 * roots and fall back to their own group value.
 *
 * The returned column mirror is still a flat pre-order id sequence per
 * column (useDragSettle-compatible); `nodeInfo` carries the per-card depth /
 * has-children / collapsed facts the column resolves by id when rendering.
 */
export function buildBoardTreeColumns(
  issues: Issue[],
  groups: BoardColumnGroup[],
  grouping: IssueGrouping,
  collapsedSet: ReadonlySet<string>,
  childProgressMap: ReadonlyMap<string, ChildProgress>,
  knownOptionIds?: ReadonlySet<string>,
): { columns: Record<string, string[]>; nodeInfo: Map<string, BoardNodeInfo> } {
  const cols: Record<string, string[]> = {};
  for (const group of groups) cols[group.id] = [];
  const nodeInfo = new Map<string, BoardNodeInfo>();
  const childrenMap = buildChildrenMap(issues);
  const loadedIds = new Set(issues.map((i) => i.id));
  // Roots = parentless issues plus issues whose parent is not part of the
  // currently loaded view (a child whose parent was filtered/paged out).
  const roots = issues.filter(
    (issue) => !issue.parent_issue_id || !loadedIds.has(issue.parent_issue_id),
  );
  for (const root of roots) {
    const gid = getIssueGroupId(root, grouping, knownOptionIds);
    if (!cols[gid]) continue;
    const { ids, nodeInfo: flatInfo } = flattenBoardTree(
      [root],
      childrenMap,
      childProgressMap,
      collapsedSet,
    );
    cols[gid].push(...ids);
    for (const [id, info] of flatInfo) nodeInfo.set(id, info);
  }
  return { columns: cols, nodeInfo };
}

/**
 * Contiguous subtree block for `activeId` in a flattened column sequence:
 * the dragged card plus every node that currently renders under it (its
 * expanded descendants, which always follow it in pre-order). Collapsed
 * children are absent from the sequence, so the block never leaks them.
 */
export function getSubtreeBlock(
  ids: readonly string[],
  activeId: string,
  nodeInfo: ReadonlyMap<string, BoardNodeInfo>,
): string[] {
  const start = ids.indexOf(activeId);
  if (start === -1) return [activeId];
  const depth = nodeInfo.get(activeId)?.depth ?? 0;
  let end = start + 1;
  while (end < ids.length && (nodeInfo.get(ids[end]!)?.depth ?? 0) > depth) {
    end += 1;
  }
  return ids.slice(start, end);
}

/**
 * Move/insert a subtree block (contiguous run of ids) so that it lands in the
 * slot `overId` currently occupies. Returns a new sequence; the block keeps
 * its internal order.
 */
export function moveBlockInto(
  ids: readonly string[],
  block: readonly string[],
  overId: string,
): string[] {
  const blockSet = new Set(block);
  const rest = ids.filter((id) => !blockSet.has(id));
  const overIndex = rest.indexOf(overId);
  const insertAt = overIndex >= 0 ? overIndex : rest.length;
  return [...rest.slice(0, insertAt), ...block, ...rest.slice(insertAt)];
}

/**
 * Relocate a subtree block within a single column so the block ends up at the
 * slot `overId` occupies (same-column reorder). Mirrors the old
 * `arrayMove(ids, oldIndex, newIndex)` placement but for a multi-id block.
 * Dropping a parent onto one of its own descendants is a no-op.
 */
export function moveBlockWithin(
  ids: readonly string[],
  block: readonly string[],
  overId: string,
): string[] {
  if (block.includes(overId)) return [...ids];
  const start = ids.indexOf(block[0]!);
  const blockLen = block.length;
  if (start === -1) return [...ids];
  const overIndex = ids.indexOf(overId);
  const newStart =
    overIndex <= start ? overIndex : overIndex - blockLen + 1;
  if (newStart === start) return [...ids];
  const next = [...ids];
  next.splice(start, blockLen);
  next.splice(newStart, 0, ...block);
  return next;
}

/**
 * Anchors for a subtree block move: the ids immediately OUTSIDE the block's
 * boundaries in `ids`, which is what the parent's position should anchor to.
 */
export function getSubtreeMoveAnchors(
  ids: readonly string[],
  block: readonly string[],
): Pick<DragMoveUpdates, "before_id" | "after_id"> {
  const start = ids.indexOf(block[0]!);
  if (start === -1) return { before_id: null, after_id: null };
  const end = start + block.length;
  return {
    before_id: start > 0 ? ids[start - 1]! : null,
    after_id: end < ids.length ? ids[end]! : null,
  };
}

/**
 * Position for a subtree block placed in `ids`: midpoint between the ids that
 * bracket the block. Same formula as {@link computePosition} but for a block
 * (the parent's position only — descendants keep theirs).
 */
export function computeBlockPosition(
  ids: readonly string[],
  block: readonly string[],
  issueMap: Map<string, Issue>,
): number {
  const start = ids.indexOf(block[0]!);
  if (start === -1) return 0;
  const getPos = (id: string) => issueMap.get(id)?.position ?? 0;
  const end = start + block.length;
  if (block.length === ids.length) return getPos(block[0]!);
  if (start === 0) return end < ids.length ? getPos(ids[end]!) - 1 : getPos(block[0]!);
  if (end === ids.length) return getPos(ids[start - 1]!) + 1;
  return (getPos(ids[start - 1]!) + getPos(ids[end]!)) / 2;
}

/**
 * Group-field updates to push to a subtree's descendants during a cross-column
 * move. Only status/assignee are syncable via batch-update; property values
 * are intentionally omitted (R5: property cascade is not supported, the parent
 * moves alone).
 */
export function getSubtreeSyncUpdates(
  group: BoardColumnGroup,
): Pick<UpdateIssueRequest, "status" | "assignee_type" | "assignee_id"> | null {
  if (group.status) return { status: group.status };
  if (group.propertyId !== undefined) return null;
  return {
    assignee_type: group.assigneeType ?? null,
    assignee_id: group.assigneeId ?? null,
  };
}

export function computePosition(ids: string[], activeId: string, issueMap: Map<string, Issue>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => issueMap.get(id)?.position ?? 0;
  if (ids.length === 1) return issueMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

export function getMoveAnchors(
  ids: readonly string[],
  activeId: string,
): Pick<DragMoveUpdates, "before_id" | "after_id"> {
  const index = ids.indexOf(activeId);
  return {
    before_id: index > 0 ? ids[index - 1]! : null,
    after_id: index >= 0 && index < ids.length - 1 ? ids[index + 1]! : null,
  };
}

/**
 * Insert `id` into `ids` at the slot implied by `position ASC`, reading each
 * id's position from `issueMap`. Mirrors `insertByPosition` in
 * `@multica/core/issues/cache-helpers` so the board's optimistic placement on
 * drop matches the cache the settle reconcile rebuilds from — otherwise the
 * card would land in one slot, then jump when local columns re-derive from TQ.
 */
export function insertIdByPosition(
  ids: string[],
  id: string,
  position: number,
  issueMap: Map<string, Issue>,
): string[] {
  const idx = ids.findIndex((existing) => {
    const p = issueMap.get(existing)?.position;
    return p !== undefined && p > position;
  });
  if (idx === -1) return [...ids, id];
  return [...ids.slice(0, idx), id, ...ids.slice(idx)];
}

export function findColumn(
  columns: Record<string, string[]>,
  id: string,
  columnIds: Set<string>,
): string | null {
  if (columnIds.has(id)) return id;
  for (const [columnId, ids] of Object.entries(columns)) {
    if (ids.includes(id)) return columnId;
  }
  return null;
}

export function issueMatchesGroup(issue: Issue, group: BoardColumnGroup): boolean {
  if (group.status) return issue.status === group.status;
  if (group.propertyId !== undefined) {
    const value = issue.properties?.[group.propertyId];
    const optionId = typeof value === "string" ? value : null;
    return optionId === (group.propertyOptionId ?? null);
  }
  return (
    (issue.assignee_type ?? null) === (group.assigneeType ?? null) &&
    (issue.assignee_id ?? null) === (group.assigneeId ?? null)
  );
}

export function getMoveUpdates(
  group: BoardColumnGroup,
  position: number,
): DragMoveTargetUpdates {
  if (group.status) return { status: group.status, position };
  // Property columns: the value change is not part of UpdateIssueRequest —
  // the board applies it through useSetIssueProperty after the position move.
  if (group.propertyId !== undefined) return { position };
  return {
    assignee_type: group.assigneeType ?? null,
    assignee_id: group.assigneeId ?? null,
    position,
  };
}
