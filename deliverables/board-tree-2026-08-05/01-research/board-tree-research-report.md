# Board 看板父子树状展示 — 现状调研与 Table hierarchy 复用分析

**调研人**: Research（魔术师） · **日期**: 2026-08-05 · **基线**: `main` @ `67e58d0f`（本地 checkout `multica/`）
**服务对象**: CLO-218（父）→ CLO-220（本次调研）→ CLO-221（Architect 设计）→ CLO-22x（Coding）

---

## 0. 需求理解

CLO-218 要求将 Board/Kanban 视图中的父子 issue 从**平铺展示**改为**树状展示**：

1. 每列内父子以树形展示：父卡片可展开/折叠，子项缩进显示在父卡片下方；
2. 父卡片保留现有子任务进度环（x/y done）；
3. 明确并实现拖拽语义：拖父卡片移动整个子任务组，拖子卡片单独移动（或由设计方定更合理语义）；
4. 子项状态与父不同列时，跟随父列显示（与 Table 视图 hierarchy 语义保持一致）；
5. 优先复用 Table 视图已就绪的 `direct_child_count`、`childProgressMap` 与展开/折叠实现。

本报告回答：这些能力分别在哪实现、当前长什么样、Board 离目标差什么、有哪些可复用资产、有哪些风险、建议怎么做。

---

## 1. Board/Kanban 视图实现现状

### 1.1 组件树与文件

| 文件 | 职责 |
|------|------|
| `packages/views/issues/components/board-view.tsx` | 整个 Board 视图：列构建（`buildGroups`/`buildColumns`）、dnd 上下文（`DndContext`/`DragOverlay`）、`handleDragStart/Over/End`、列分页（`useLoadMoreByStatus` / `useLoadMoreByAssigneeGroup` / `useIssueGroupBranches`）、属性分组列、隐藏列面板。导出 `BoardView = memo(BoardViewImpl)`。 |
| `packages/views/issues/components/board-column.tsx` | 单列：`useDroppable(group.id)` + `SortableContext(verticalListSortingStrategy)` + Virtuoso 虚拟滚动（阈值 30，小列平铺渲染），卡片条目 `DraggableBoardCard`。列 id = `status:<s>` / `assignee:<t>:<id>` / `property:<pid>:<optionId>`。 |
| `packages/views/issues/components/board-card.tsx` | `BoardCardContent`（渲染优先级/标题/描述/项目/标签/自定义属性/进度环）+ `DraggableBoardCard`（`useSortable`，卡片级拖拽）。 |
| `packages/views/issues/utils/drag-utils.ts` | 拖拽纯函数：`buildColumns`、`findColumn`、`computePosition`、`getMoveAnchors`、`insertIdByPosition`、`getMoveUpdates`、`issueMatchesGroup`、`makeKanbanCollision`。 |
| `packages/views/issues/components/use-drag-settle.ts` | Board/List 共享的拖拽状态机（本地列镜像 + 冻结/回滚 + settleVersion）。 |
| `packages/views/issues/surface/use-issue-surface-controller.ts` | 视图控制器：决定数据源（`usesServerStatusSurface` = list/status-board；`usesServerGroupSurface` = assignee/property board/swimlane）。 |
| `packages/views/issues/surface/use-issue-status-branches.ts` | status 分组 Board/List 数据：按 status 独立 cursor 分页 `/table/rows`。 |
| `packages/views/issues/surface/use-issue-group-branches.ts` | assignee/property 分组 Board/Swimlane 数据：`/table/groups` 枚举列 + 每列 `/table/rows`。 |
| `packages/views/issues/surface/issue-surface.tsx` | 按 `viewMode` 分发：`board` → `BoardView`；`table` → `TableView`。 |

### 1.2 当前父子 issue 如何"平铺展示"

- Board 数据查询在 `use-issue-surface-controller.ts:338` 构建 `tableQuerySpec`，其中 `filters.include_sub_issues = showSubIssues`（`view-store` 默认 `showSubIssues=true`，`issues-header.tsx:1660` 有显示开关）。
- 后端 `issue_table_query.go:615`：`include_sub_issues=false` 时追加 `i.parent_issue_id IS NULL`；为 `true`（默认）时子项包含在平铺结果里。
- Board 列构建 `buildColumns(groupedIssues, groups, grouping, …)`（`drag-utils.ts:72`）对每张卡片按其**自身** `status`/`assignee`/`property` 归类 —— 子项与父项互不关联，各自是独立卡片，这就是"平铺"的机制根源。
- 卡片上的进度环（x/y done）已存在：`board-card.tsx:293-300` 通过 `childProgressMap.get(issue.id)` + `ProgressRing` 渲染，受 `cardProperties.childProgress` 显示开关控制。→ 需求 2 的前端展示基础已具备。
- 因此：**平铺不是"数据结构"问题，而是"列内渲染顺序/嵌套"问题**。子项已在数据中，缺的是把子项排到父卡片下方并缩进 + 折叠能力。

### 1.3 拖拽机制（dnd-kit + useDragSettle）

- 依赖：`@dnd-kit/core ^6.3.1`、`@dnd-kit/sortable ^10.0.0`、`@dnd-kit/utilities ^3.2.2`（`packages/views/package.json`）。
- `board-view.tsx`：`DndContext` + `PointerSensor(activationConstraint:{distance:5})` + `makeKanbanCollision`（pointerWithin → 卡片优先，否则 closestCenter，`drag-utils.ts:23`）+ `DragOverlay`。
- 列级 `useDroppable({id: group.id})`；卡片级 `useSortable({id: issue.id, data:{status}})`（`board-card.tsx:339`）。
- 拖拽期间本地列镜像由 `useDragSettle`（`use-drag-settle.ts`）托管，与 List 共享（`list-view.tsx:138`）；Swimlane 是**自带的一份复制实现**（`swimlane-view.tsx:999-1005`，未复用该 hook）。
- 移动语义与落库：
  - 同列排序（`sortBy=position` 时）：`arrayMove` + `computePosition` 计算新 position，`getMoveAnchors` 产出 `before_id/after_id`。
  - 跨列移动：`getMoveUpdates(group, position)`（`drag-utils.ts:154`）按目标列产出 `{status}` / `{assignee_type,assignee_id}` / `{position}`（属性列）；属性列值走 `useSetIssueProperty`。
  - 落库：`onMoveIssue(issueId, updates, onSettled)` → `use-issue-surface-actions.ts:78 moveIssue` → `useUpdateIssue` mutation：
    - 有 `move_intent{before_id,after_id}` → `api.moveIssue`（`POST /api/issues/:id/move`，`client.ts:856`）；后端 `issue_move.go` 白名单字段含 `status/assignee_type/assignee_id/parent_issue_id/project_id/before_id/after_id`，由锚点算 position 再委托 `UpdateIssue`。
- **父子相关现状**：`handleDragEnd`（`board-view.tsx:485`）只对 `activeId` 单卡片调用 `onMoveIssue` —— **没有任何"拖父联动子项"逻辑**。子项本身可单独拖拽（作为独立卡片）。

---

## 2. Table 视图 hierarchy 参考实现（重点复用对象）

> 这是 CLO-218 明确要求"优先复用"的现成机制。

### 2.1 数据层

- **`direct_child_count`**：
  - 后端 `server/internal/handler/issue_table_rows.go`：响应行结构含 `DirectChildCount int64`（行 20），JSON 字段 `direct_child_count`。
  - SQL 在 `hierarchy.enabled=true` 时计算 `childCountExpr = (SELECT COUNT(*)::bigint FROM membership child WHERE child.parent_issue_id = i.id)`（行 343-346），否则恒为 `0`。
  - 前端 schema：`packages/core/api/schemas.ts:687` `IssueTableRowSchema { issue, direct_child_count }`；类型 `packages/core/types/api.ts:364`。
  - 前端行类型：`table-view-model.ts:21` `IssueTableDisplayRow`（`kind:"issue"` 带 `depth`/`hasChildren`/`collapsed`）；`hasChildren = tableHierarchy && row.direct_child_count > 0`（`table-view.tsx:1743`）。
- **`childProgressMap`**（见 §3 数据来源）：`Map<parent_issue_id, {done,total}>`，Board/Table/List/Swimlane 共用同一份。

### 2.2 服务端树形分页协议（已就绪，Board 可直接复用）

`POST /api/issues/table/rows`（`router.go:1088`，handler `issue_table_rows.go:202 ListIssueTableRows`）请求体：

- `group`（status/assignee/property/compound/none）、`group_key`；
- `hierarchy.enabled`（bool）；
- `parent_id`（null = root 分支；非空 = 某父项的子分支）；
- `page.{limit,cursor}`（keyset 游标分页，每页 50）。

branch predicate（`issue_table_rows.go:276-291`）：

- root 分支：`i.parent_issue_id IS NULL OR 父不在 membership 中`；
- 子分支：`i.parent_issue_id = $parent AND EXISTS(parent ∈ membership)`。

→ 后端已具备"按父节点惰性拉取子树 + 每父 direct_child_count"的完整能力。**Board 当前所有请求都是 `hierarchy.enabled=false`**（`use-issue-status-branches.ts:189`、`use-issue-group-branches.ts:208`、controller export 路径 `use-issue-surface-controller.ts:616`）。

### 2.3 前端展开/折叠状态管理（view-store 持久化）

`packages/core/issues/stores/view-store.ts`：

- `tableCollapsedParents: string[]`（按父 issue id 记录折叠集合）+ `toggleTableParentCollapsed(issueId)`（行 459-464）；
- `tableHierarchy: boolean`（默认 `true`，行 277；`toggleTableHierarchy` 行 465-466）；
- `tableCollapsedGroups: string[]`（分组头折叠，行 196/453-458）；
- 全部经 `viewStorePersistOptions`（行 470-515）持久化到 localStorage（`createWorkspaceAwareStorage`），并做 merge 兼容（行 565-570）。

### 2.4 前端展开/折叠渲染逻辑

`table-view.tsx:1698 serverDisplayRows` —— DFS 递归 `appendBranch(groupKey, parentId, depth, ancestors)`：

1. 按 group/root 依次 append；
2. `kind:"issue"` 行带 `depth`、`hasChildren`、`collapsed`；
3. `if (tableHierarchy && row.direct_child_count > 0 && !collapsed)` → 递归 `appendBranch(groupKey, row.issue.id, depth+1, [...ancestors, row.issue.id])`（行 1746-1751）；
4. 已折叠或未激活的父分支不拉数据：分支查询 `enabled` 受 `collapsedGroupSet`/`collapsedParentSet` 控制（行 1403-1407）；
5. 未注册分支先渲染 `kind:"load_more"`（`activate` 态）由 `InfiniteScrollSentinel` 触发 `activateServerBranch`（行 1708-1722）。

渲染细节：

- 行缩进：`InlineTitle` 用 `style={{ paddingLeft: row.depth * 18 }}`（`table-view.tsx:632`）；
- 折叠按钮：`row.hasChildren` 时渲染 Chevron，点击 `onToggleParent`（`table-view.tsx:648-666`），标题 cell 的 `meta.toggleTableParentCollapsed(issue.id)`（行 1049）；
- 分组头折叠：`IssueTableGroupRow`（行 783-811），`onToggle={() => toggleTableGroupCollapsed(row.original.key)}`（行 2311）；
- Header 里的 hierarchy 开关只在 `viewMode === "table"` 显示（`issues-header.tsx:1590`），`Switch` 绑 `toggleTableHierarchy`。

### 2.5 关键语义：跨组是否跟随父列（与需求 4 直接相关）

- 后端现有语义是**跨组不跨列**：`TestIssueTableHierarchyDoesNotCrossGroups`（`issue_table_query_test.go:1224`）验证：
  - 一个 `done` 子项挂在 `todo` 父项下，子项在 `status:done` 分组中成为**独立 root**，不会出现在父项（todo）下；
  - 父项的 `direct_child_count` 在 todo 组里为 0（跨组子项不计入）。
- 即 Table 的 hierarchy 是"**组内**的父子嵌套"：子项只有在与父项同组（同 status/assignee/property）时才折叠进父项下方。
- CLO-218 需求 4 要求"子项状态与父不同列时，跟随父列显示"——**这是当前后端尚未实现的新语义**，若采纳会影响 `issue_table_rows.go` 的 branchPredicate，且与 `TestIssueTableHierarchyDoesNotCrossGroups` 现有行为冲突，需 Architect 决策（见 §6 风险 / §7 建议）。

---

## 3. 拖拽 hooks / 组件复用点 & childProgressMap 数据来源

### 3.1 可复用 hooks / 组件

| 资产 | 位置 | 复用方式 |
|------|------|---------|
| `useDragSettle` | `packages/views/issues/components/use-drag-settle.ts` | Board/List 已共享；树形拖拽继续用它做本地列镜像/冻结/回滚，只需把"列内 items"从平铺数组换成树形节点序列。 |
| `drag-utils` 纯函数 | `packages/views/issues/utils/drag-utils.ts` | `findColumn`/`computePosition`/`getMoveAnchors`/`insertIdByPosition`/`getMoveUpdates` 基于平铺 id 数组，树形后需要扩展（见 §7）。 |
| `ProgressRing` | `packages/views/issues/components/progress-ring.tsx` | 父卡片进度环直接复用（board-card 已用）。 |
| `ChildProgress` 类型 | `packages/views/issues/components/list-row.tsx:28` | `{done,total}`。 |
| `childrenByParentsOptions` | `packages/core/issues/queries.ts:847` | 批量按父取子 + 逐父 hydrate 缓存（Swimlane 父泳道用），Board 树形"展开未加载子项"可复用。 |
| `issueKeys.children` | `packages/core/issues/queries.ts:88` | 每父子列表缓存键。 |
| `view-store` collapsed 字段模式 | `packages/core/issues/stores/view-store.ts:196-198` | 为 Board 增加同构的 `boardCollapsedParents`（或复用 table 字段）。 |

### 3.2 childProgressMap 数据来源（前端 + 后端全链路）

- 前端：`use-issue-surface-data.ts:433-437`：
  ```ts
  const { data: childProgressData } = useQuery(childIssueProgressOptions(wsId));
  const childProgressMap = childProgressData ?? EMPTY_CHILD_PROGRESS;
  ```
- `childIssueProgressOptions`（`packages/core/issues/queries.ts:766`）→ `api.getChildIssueProgress()`（`client.ts:875`）→ `GET /api/issues/child-progress`（`router.go:1091`）。
- 后端：`server/internal/handler/issue.go:2012 ChildIssueProgress` → SQL `ChildIssueProgress`（`server/pkg/db/generated/issue.sql.go`，源 `server/pkg/db/queries/issue.sql:353`）：
  ```sql
  SELECT parent_issue_id,
         COUNT(*)::bigint AS total,
         COUNT(*) FILTER (WHERE status IN ('done','cancelled'))::bigint AS done
  FROM issue
  WHERE workspace_id = $1 AND parent_issue_id IS NOT NULL
  GROUP BY parent_issue_id;
  ```
- 前端 `select` 组装为 `Map<parent_issue_id, {done,total}>`（`queries.ts:770-776`）。
- **注意**：该接口是**工作区全量**进度（不受当前过滤器/分页限制），父卡片的 "x/y done" 语义与它一致；`done` 统计含 `cancelled`。

---

## 4. 调用关系总览（Board 树形化将涉及的数据流）

```
issues-header.tsx (view-mode/显示开关)
   │  viewMode / grouping / showSubIssues / cardProperties
   ▼
use-issue-surface-controller.ts ── tableQuerySpec(filters.include_sub_issues)
   │  ├── useIssueStatusBranches ── /table/rows (group=status, hierarchy=false, parent_id=null)
   │  └── useIssueGroupBranches  ── /table/groups + /table/rows (每列, hierarchy=false)
   │
   ├── useIssueSurfaceData
   │     ├── childProgressMap ← /issues/child-progress
   │     └── issues (平铺 Issue[])
   ▼
issue-surface.tsx → BoardView
   ├── buildGroups / buildColumns → columns: Record<groupId, issueId[]>
   ├── useDragSettle(columns) ── 本地列镜像
   ├── DndContext → handleDragStart/Over/End
   ├── BoardColumn(issueIds, childProgressMap)
   │     ├── useDroppable + SortableContext(verticalList)
   │     └── Virtuoso → DraggableBoardCard
   │           └── BoardCardContent(childProgress ← childProgressMap.get(id))
   └── DragOverlay(BoardCardContent)
落库: onMoveIssue → moveIssue → useUpdateIssue → PUT /issues/:id 或 POST /issues/:id/move
```

---

## 5. 配置影响

- **前端显示开关**：`showSubIssues`（`view-store.ts:180/266/398`）默认 `true`，控制子项是否平铺出现。树形化后建议：展开态显示子项、折叠态隐藏子项，与 Table `tableCollapsedParents` 语义对齐；`showSubIssues=false` 仍应完全隐藏子项（沿用 `filter.ts:114/120`）。
- **hierarchy 开关**：`tableHierarchy` 目前仅 Table 显示开关（`issues-header.tsx:1590`）。Board 树形是否也接同一 store 字段、还是新增 `boardHierarchy`，需 Architect 定。
- **新增持久化字段**（若采用）：`boardCollapsedParents: string[]` + toggle action，需同步 `viewStorePersistOptions.partialize`（行 495-507）与 `mergeViewStatePersisted`（行 565-570）。
- **后端**：若采纳"跟随父列"语义，`/table/rows` 的 hierarchy branchPredicate 需参数化（建议新增开关，默认保持现状），避免破坏 Table/List/Swimlane。

---

## 6. 风险分析

1. **语义冲突（高）**：需求 4「子项跟随父列」与后端现有「跨组不跨列」语义相悖且被测试固化（`TestIssueTableHierarchyDoesNotCrossGroups`）。若实现需求字面语义，需后端改动 + 测试调整，且 `/table/rows` 同时服务 Table/List/Board/Swimlane，回归面大。→ 必须先由 Architect 决策（方案 A 组内跟随 / 方案 B 参数化新增语义）。
2. **Board 数据量大 + 虚拟滚动**：`board-column.tsx` 用 Virtuoso（阈值 30）。树形嵌套后每列节点数 = 父+子，展开需惰性加载子分支（复用 Table 的 `activateServerBranch` + `load_more` 模式），防全量展开造成 N+1 请求。Virtuoso 的 `computeItemKey`（`board-column.tsx:158`）用 `issue.id`，树形节点 key 需保持稳定。
3. **拖拽复杂度（中-高）**：现有拖拽基于平铺 id 数组（`findColumn`/`computePosition`/`getMoveAnchors`）。树形后：
   - 整组移动（拖父）需在 drag payload 携带子树 id 集合；
   - 组内子卡移动、跨列整组移动的锚点（before/after）语义要重定义；
   - 乐观更新/回滚要与 `useDragSettle` 的"列 id → id[]"模型兼容（树形可能要升级为节点序列）。
4. **批量写一致性（中）**：若"拖父联动子项"采纳，写路径可用现有 `POST /api/issues/batch-update`（`router.go:1101`，`BatchUpdateIssues`）。需保证：部分失败回滚语义、`move_intent` 在 batch 路径的可用性、realtime/任务触发/父通知副作用是否逐条触发（`UpdateIssue` 单条路径已有，batch 是否复用需验证）。
5. **进度环口径（低）**：`child-progress` 的 done 统计为 `status IN ('done','cancelled')`，与需求 "x/y done" 一致；树形后父卡进度环沿用现有 `childProgressMap` 即可，无需后端改动。
6. **回归面（中）**：`/table/rows` 同时服务 Table/List/Board/Swimlane。任何 hierarchy 语义改动都影响 Table 测试（`issue_table_query_test.go` 系列）与多视图。Board 树形必须以"参数化"方式开关，避免破坏 Table 现状。
7. **Swimlane 拖拽状态机复制（低-中）**：`swimlane-view.tsx` 自带一份 drag-settle 实现（未复用 `use-drag-settle`）。若 Board 树形升级共享 hook，Swimlane 不跟会继续分叉（既有风险，非本次新增）。

---

## 7. 建议修改方案（供 Architect 决策）

1. **数据**：Board 树形优先复用 `/table/rows` 的 `hierarchy.enabled=true` + `parent_id` 惰性分支 + `direct_child_count`，不要在前端对平铺数据自建树；子项补充可用 `childrenByParentsOptions`（Swimlane 已验证）。若不想动 Board 查询，也可用 `childProgressMap` 判断"有子"（`total>0`），但缺少跨分页子项完整性，建议前者。
2. **前端状态**：新增 `boardCollapsedParents`（或复用 `tableCollapsedParents`），展开/折叠 UI 参考 `table-view.tsx` 的 `InlineTitle` Chevron + `depth * 18` 缩进；header 的 hierarchy/子项显示开关复用 `issues-header.tsx` 模式。
3. **渲染**：`board-column.tsx` 的 `itemContent` 需要"树形渲染器"：父卡片 = 可折叠卡片 + 缩进子区，子卡缩进（`paddingLeft: depth * 18` 或更小间距）。Virtuoso 的 data 从平铺 `Issue[]` 换成树形节点数组（每个节点携带 issue + depth + children 数组）。
4. **后端**：
   - 若采纳「子项跟随父列」→ 新增/调整 hierarchy 查询语义（**参数化**，不影响 Table 现状；`issue_table_rows.go:276-291` branchPredicate + `direct_child_count` 口径）。
   - 若采纳「拖父联动子项」→ 前端用 `batch-update` 批量写（父 status/position + 全部子项 status），后端复用现有单条副作用；或后端新增级联语义（改动更大）。
5. **拖拽**：Architect 出树形拖拽状态机——drag payload 含子树 id 集合；整组移动 = 批量更新，子卡单独移动 = 单条更新；`DragOverlay` 可显示「父卡 + N 子」占位。与 `useDragSettle` 兼容：建议把列镜像从 `Record<groupId, id[]>` 升级为 `Record<groupId, TreeNode[]>`（或保留平铺数组 + 单独 tree 结构）。
6. **测试**：后端补「子项跨组跟随父列」语义测试（若采纳方案 B）；前端补 Board 树形展开/折叠/整组拖拽组件测试（参考 `table-view-editing.test.tsx` / `swimlane-view.test.tsx` 已有覆盖）；Validation 覆盖 Table 回归。

---

## 8. 缺失信息 / 待确认

1. 需求 3 的最终拖拽语义（整组 vs 单独）与需求 4 的「跟随父列」最终口径 → **Architect 裁定**（方案 A：组内跟随，改动小；方案 B：子项强制跟随父列，后端参数化）。
2. Board 与 Table 的 hierarchy 语义是否允许分叉（Board 跟随父列、Table 保持跨组不跨列）→ 影响后端参数化方案。
3. 父卡片折叠后是否连同其子项一起不参与排序/拖拽目标（Table 是折叠即隐藏，Board 折叠态是否保留父卡拖拽能力）。
4. `batch-update` 是否支持 `move_intent`（before/after 锚点）与单条一致的副作用（需 Coding 验证或后端补齐）。

---

## 附：已存在的相关资产（分支 feature/board-tree，不在本次基线 main 上）

本地仓库存在 `feature/board-tree` 分支（合并了上一轮 CLO-196/198/200 的交付件）：

- `deliverables/board-tree-2026-08-05/01-research/board-tree-research-report.md`（CLO-200，上一轮调研）
- `deliverables/board-tree-2026-08-05/02-design/board-tree-architecture.md`（CLO-196，上一轮架构设计）
- `deliverables/board-tree-2026-08-05/03-backend/README.md`（CLO-198，后端 `GET /api/issues?hierarchy=true` + `direct_child_count`，零迁移零破坏）

本轮调研基于 `main`（67e58d0f）重新产出现状分析；如需沿用上一轮后端改动（`?hierarchy=true` 平铺接口附带 direct_child_count），可让 Architect 在方案中直接引用 `feature/board-tree` 的 `f8d9eef2` 提交。本报告文件与上一轮同名同路径，若两轮交付件需共存，请 Architect/Orchestrator 明确以本轮为准并归档旧版。
