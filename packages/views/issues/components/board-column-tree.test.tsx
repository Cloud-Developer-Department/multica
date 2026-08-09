import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import type { Issue } from "@multica/core/types";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

vi.mock("@dnd-kit/core", () => ({
  useDroppable: () => ({ setNodeRef: vi.fn(), isOver: false }),
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: any) => children,
  verticalListSortingStrategy: {},
}));

// The column is under the virtualization threshold in these tests, so the
// plain (non-virtualized) Virtuoso path never mounts.
vi.mock("react-virtuoso", () => ({
  Virtuoso: () => null,
}));

const mockDraggableBoardCard = vi.hoisted(() => vi.fn());
vi.mock("./board-card", () => ({
  DraggableBoardCard: (props: any) => {
    mockDraggableBoardCard(props);
    return <div data-testid={`card-${props.issue.id}`}>card</div>;
  },
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: any) => children,
  DropdownMenuTrigger: ({ children, render }: any) => render ?? children,
  DropdownMenuContent: ({ children }: any) => children,
  DropdownMenuItem: ({ children }: any) => children,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, ...props }: any) => <button {...props}>{children}</button>,
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStoreApi: () => ({ getState: () => ({ hideStatus: vi.fn() }) }),
}));

vi.mock("@multica/core/issues/config", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/issues/config")>();
  return {
    ...actual,
    STATUS_CONFIG: {
      todo: { label: "Todo", iconColor: "text-foreground", hoverBg: "", columnBg: "bg-muted/40" },
      done: { label: "Done", iconColor: "text-foreground", hoverBg: "", columnBg: "bg-muted/40" },
    },
  };
});

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("../../platform", () => ({
  useRestoredScrollOffset: () => undefined,
  useRestoredScrollRef: () => () => {},
}));

vi.mock("../../common/deferred-popup", () => ({
  DeferredPopup: ({ children }: any) => children(false, vi.fn()),
}));

vi.mock("../../common/deferred-tooltip", () => ({
  DeferredTooltip: () => null,
}));

import { BoardColumn, type BoardColumnGroup } from "./board-column";
import { ActiveSubtreeBadge } from "./board-view";
import { buildBoardTreeColumns } from "../utils/drag-utils";
import type { BoardNodeInfo } from "./board-tree-model";
import type { ChildProgress } from "./list-row";

function mkIssue(id: string, overrides: Partial<Issue> = {}): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: `Issue ${id}`,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    labels: [],
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

const STATUS_GROUPS: BoardColumnGroup[] = [
  { id: "status:todo", title: "Todo", status: "todo" },
  { id: "status:done", title: "Done", status: "done" },
];

function renderColumn(
  group: BoardColumnGroup,
  issueIds: string[],
  issueMap: Map<string, Issue>,
  nodeInfo: ReadonlyMap<string, BoardNodeInfo>,
  opts: { onToggleCollapsed?: (issueId: string) => void } = {},
) {
  return render(
    <I18nProvider resources={TEST_RESOURCES} locale="en">
      <BoardColumn
        group={group}
        issueIds={issueIds}
        issueMap={issueMap}
        nodeInfo={nodeInfo}
        onToggleCollapsed={opts.onToggleCollapsed}
        childProgressMap={new Map<string, ChildProgress>()}
        projectMap={new Map()}
      />
    </I18nProvider>,
  );
}

function propsOf(id: string) {
  const call = mockDraggableBoardCard.mock.calls.find(
    ([props]: any) => props.issue.id === id,
  );
  return call?.[0];
}

describe("BoardColumn tree rendering", () => {
  beforeEach(() => {
    mockDraggableBoardCard.mockClear();
  });

  it("passes depth/hasChildren/collapsed/onToggleCollapsed to each card", () => {
    const p = mkIssue("p");
    const c = mkIssue("c", { parent_issue_id: "p" });
    const nodeInfo = new Map<string, BoardNodeInfo>([
      ["p", { depth: 0, hasChildren: true, collapsed: true }],
      ["c", { depth: 1, hasChildren: false, collapsed: false }],
    ]);
    const issueMap = new Map([["p", p], ["c", c]]);
    const onToggleCollapsed = vi.fn();

    renderColumn(STATUS_GROUPS[0]!, ["p", "c"], issueMap, nodeInfo, { onToggleCollapsed });

    const pProps = propsOf("p")!;
    const cProps = propsOf("c")!;
    expect(pProps.depth).toBe(0);
    expect(pProps.hasChildren).toBe(true);
    expect(pProps.collapsed).toBe(true);
    // Toggle is wired for parents only.
    expect(typeof pProps.onToggleCollapsed).toBe("function");
    expect(cProps.depth).toBe(1);
    expect(cProps.hasChildren).toBe(false);
    expect(cProps.collapsed).toBe(false);
    expect(cProps.onToggleCollapsed).toBeUndefined();

    pProps.onToggleCollapsed();
    expect(onToggleCollapsed).toHaveBeenCalledWith("p");
  });

  it("renders the visible flattened order (parent then child)", () => {
    const p = mkIssue("p");
    const c = mkIssue("c", { parent_issue_id: "p" });
    const nodeInfo = new Map<string, BoardNodeInfo>([
      ["p", { depth: 0, hasChildren: true, collapsed: false }],
      ["c", { depth: 1, hasChildren: false, collapsed: false }],
    ]);
    const issueMap = new Map([["p", p], ["c", c]]);

    renderColumn(STATUS_GROUPS[0]!, ["p", "c"], issueMap, nodeInfo);

    const rendered = mockDraggableBoardCard.mock.calls.map(
      ([props]: any) => props.issue.id,
    );
    expect(rendered.slice(0, 2)).toEqual(["p", "c"]);
  });

  it("hides a collapsed parent's subtree (children dropped from the visible ids)", () => {
    // buildBoardTreeColumns with `collapsedSet = {"p"}` flattens only the
    // parent — the child is not part of the column's visible id sequence.
    const p = mkIssue("p");
    const c = mkIssue("c", { parent_issue_id: "p" });
    const { columns, nodeInfo } = buildBoardTreeColumns(
      [p, c],
      STATUS_GROUPS,
      "status",
      new Set(["p"]),
      new Map(),
    );
    expect(columns["status:todo"]).toEqual(["p"]);

    const issueMap = new Map([["p", p], ["c", c]]);
    renderColumn(STATUS_GROUPS[0]!, columns["status:todo"]!, issueMap, nodeInfo);

    expect(propsOf("p")).toBeDefined();
    expect(propsOf("p")!.collapsed).toBe(true);
    expect(propsOf("p")!.hasChildren).toBe(true);
    expect(propsOf("c")).toBeUndefined();
  });

  it("follows the parent column: a child whose own status differs renders under its parent with depth 1", () => {
    // The child's own status is `done`, but it must render in the parent's
    // `todo` column (follow-parent) — and NOT in its own-status column.
    const p = mkIssue("p");
    const c = mkIssue("c", { parent_issue_id: "p", status: "done" });
    const { columns, nodeInfo } = buildBoardTreeColumns(
      [p, c],
      STATUS_GROUPS,
      "status",
      new Set(),
      new Map(),
    );
    expect(columns["status:todo"]).toEqual(["p", "c"]);
    expect(columns["status:done"]).toEqual([]);

    const issueMap = new Map([["p", p], ["c", c]]);
    renderColumn(STATUS_GROUPS[0]!, columns["status:todo"]!, issueMap, nodeInfo);

    expect(propsOf("p")!.depth).toBe(0);
    expect(propsOf("c")!.depth).toBe(1);
  });
});

describe("ActiveSubtreeBadge (DragOverlay)", () => {
  it("renders the +N sub-issues badge when the dragged card has children", () => {
    const { getByText } = render(
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <ActiveSubtreeBadge count={3} isDetaching={false} />
      </I18nProvider>,
    );
    expect(getByText("+3 sub-issues")).toBeTruthy();
  });

  it("renders nothing when the dragged card has no descendants", () => {
    const { container } = render(
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <ActiveSubtreeBadge count={0} isDetaching={false} />
      </I18nProvider>,
    );
    expect(container.querySelector("[class*='rounded-full']")).toBeNull();
    expect(container.textContent).toBe("");
  });

  it("shows the detach hint when a sub-issue is dragged cross-column", () => {
    const { getByText } = render(
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <ActiveSubtreeBadge count={1} isDetaching={true} />
      </I18nProvider>,
    );
    expect(getByText("Will become a top-level issue")).toBeTruthy();
  });

  it("omits the detach hint when the dragged card is not detaching", () => {
    const { container } = render(
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <ActiveSubtreeBadge count={2} isDetaching={false} />
      </I18nProvider>,
    );
    expect(container.textContent).toBe("+2 sub-issues");
  });
});
