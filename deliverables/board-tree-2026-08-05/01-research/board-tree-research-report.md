# Board 看板父子树状展示 — 现状调研报告

**调研人**: Research（魔术师） · **日期**: 2026-08-05 · **基线**: `Equipment_Department_Exploration` @ `6d400f2d`
**服务对象**: CLO-196 Board 看板支持父子 issue 树状展示（Architect → Backend-Coding / Frontend-Coding）

---

## 0. 需求理解

CLO-196 要求在 Board/Kanban 视图内，将父子 issue 从当前**平铺展示**改为**树状展示**：

1. 每列内父子以树形展示：父卡片可展开/折叠，子项缩进显示在父卡片下方；
2. 父卡片保留现有子任务进度环（x/y done）；
3. 明确并实现拖拽语义：拖父卡片移动整个子任务组，拖子卡片单独移动（或由方案方给定更合理语义）；
4. 子项状态与父不同列时，跟随父列显示（与 Table 视图 hierarchy 语义保持一致）；
5. 优先复用 Table 视图已就绪的 `direct_child_count`、`childProgressMap` 与展开/折叠实现。

本报告回答：这些能力分别在哪里实现、现在长什么样、Board 离目标还差什么、后端要补什么、本地怎么部署。

---

## 1. Board/Kanban 视图实现位置

### 1.1 组件树与文件

| 文件 | 职责 |
|------|------|
| `packages/views/issues/components/board-view.tsx` | 整个 Board 视图：列构建（`buildGroups`/`buildColumns`）、dnd 上下文（`DndContext`/`DragOverlay`）、`handleDragStart/Over/End`、每列的加载更多（`useLoadMoreByStatus` / `useLoadMoreByAssigneeGroup` / `useIssueGroupBranches`）、属性分组列。导出 `BoardView`（memo）。 |
| `packages/views/issues/components/board-column.tsx` | 单列：`useDroppable` + `SortableContext(verticalListSortingStrategy)` + Virtuoso 虚拟滚动（小列阈值 30 走平铺渲染），卡片条目 `DraggableBoardCard`。列 id = `status:<s>` / `assignee:<t>:<id>` / `property:<pid>:<optionId>`。 |
| `packages/views/issues/components/board-card.tsx` | 卡片本体：`BoardCardContent`（渲染优先级/标题/描述/项目/标签/自定义属性/进度环）+ `DraggableBoardCard`（`useSortable`，卡片级拖拽）。 |
| `packages/views/issues/utils/drag-utils.ts` | 拖拽纯函数：`buildColumns`、`findColumn`、`computePosition`、`getMoveAnchors`、`insertIdByPosition`、`getMoveUpdates`、`issueMatchesGroup`、`makeKanbanCollision`。 |
| `packages/views/issues/surface/use-issue-surface-controller.ts` | 视图控制器：决定哪些分支数据源启用（`usesServerStatusSurface` = list/status-board，`usesServerGroupSurface` = assignee/property board/swimlane）。 |
| `packages/views/issues/surface/use-issue-status-branches.ts` | status 分组的 Board/List 数据：按 status 独立 cursor 分页 `/table/rows`。 |
| `packages/views/issues/surface/use-issue-group-branches.ts` | assignee/property 分组 Board/Swimlane 数据：`/table/groups` 枚举列 + 每列 `/table/rows`。 |
| `packages/views/issues/surface/issue-surface.tsx` | 按 `viewMode` 分发：`board` → `BoardView`；`table` → `TableView`。 |

### 1.2 当前父子 issue 如何"平铺展示"

- Board 数据查询在 `use-issue-surface-controller.ts:338` 构建 `tableQuerySpec`，其中 `filters.include_sub_issues = showSubIssues`（`view-store` 默认 `showSubIssues=true`，issues-header 有开关）。
- 后端 `issue_table_query.go:615`：`include_sub_issues=false` 时追加 `i.parent_issue_id IS NULL`，为 true（默认）时子项包含在平铺结果里。
- Board 的列构建 `buildColumns(groupedIssues, groups, grouping, …)` 对每张卡片按其**自身** `status`（或 assignee/property）归类——**子项与父项互不关联**，父项/子项各自是独立卡片，这就是"平铺"的机制根源。
- 卡片上的进度环（x/y done）已存在：`board-card.tsx` 通过 `childProgressMap.get(issue.id)` + `ProgressRing` 渲染，受 `cardProperties.childProgress` 显示开关控制。→ 需求 2 的前端展示基础已具备。

---

## 2. Table 视图 hierarchy 已有实现

> 这是 CLO-196 明确要求"优先复用"的现成机制。

### 2.1 数据层来源

- **`direct_child_count`**：后端 `server/internal/handler/issue_table_rows.go`：
  - 响应行结构 `issueTableRowResponse{ Issue, DirectChildCount }`（行 18-21），JSON 字段 `direct_child_count`。
  - SQL 中当 `hierarchy.enabled=true` 时计算 `childCountExpr = (SELECT COUNT(*)::bigint FROM membership child WHERE child.parent_issue_id = i.id)`（行 343-346），否则恒为 `0`。
  - 前端类型 `IssueTableDisplayRow`（`table-view-model.ts:21`）在 `kind:"issue"` 行带 `depth` / `hasChildren` / `collapsed`；`hasChildren = tableHierarchy && row.direct_child_count > 0`（`table-view.tsx:1743`）。
- **`childProgressMap`**：
  - 来源 `use-issue-surface-data.ts:433` `useQuery(childIssueProgressOptions(wsId))` → `packages/core/issues/queries.ts:766` → `api.getChildIssueProgress()` → `GET /api/issues/child-progress`（`router.go:1091`）。
  - 后端 `issue.go:2012 ChildIssueProgress` → SQL `ChildIssueProgress`（`server/pkg/db/generated/issue.sql.go:14`）：
    ```sql
    SELECT parent_issue_id,
           COUNT(*)::bigint AS total,
           COUNT(*) FILTER (WHERE status IN ('done','cancelled'))::bigint AS done
    FROM issue
    WHERE workspace_id = $1 AND parent_issue_id IS NOT NULL
    GROUP BY parent_issue_id
    ```
  - 前端组装为 `Map<parent_issue_id, {done,total}>`。Board/Table/List/Swimlane 共用同一份 `childProgressMap`（`use-issue-surface-data.ts` 返回，controller 透传）。

### 2.2 后端树形分页协议（已就绪，Board 可复用）

`POST /api/issues/table/rows`（`issue_table_rows.go:202 ListIssueTableRows`）请求体含：

- `group`（`status`/`assignee`/`property`/`compound`/`none`）、`group_key`；
- `hierarchy.enabled`（bool）；
- `parent_id`（null = root 分支；非空 = 某父项的子分支）；
- `page.{limit,cursor}`（keyset 游标分页）。

`issue_table_rows.go:276-291` 的 branch predicate：

- root 分支：`i.parent_issue_id IS NULL OR 父不在 membership 中`；
- 子分支：`i.parent_issue_id = $parent AND EXISTS(parent ∈ membership)`。

→ **后端已经具备「按父节点惰性拉取子树 + 每父 direct_child_count」的完整能力**，只是 Board 当前请求全部 `hierarchy.enabled=false`。

### 2.3 前端展开/折叠实现方式

- 折叠状态在 `packages/core/issues/stores/view-store.ts`：
  - `tableCollapsedParents: string[]`（按父 issue id 记录折叠集合）+ `toggleTableParentCollapsed(issueId)`（行 459-464）；
  - `tableHierarchy: boolean`（默认 `true`，header 里 `toggleTableHierarchy` 开关）；
  - `tableCollapsedGroups: string[]`（按分组头折叠，分组场景）。
- 渲染逻辑 `table-view.tsx:1698 serverDisplayRows`：DFS `appendBranch(groupKey, parentId, depth, ancestors)` 递归：
  - 有子且未折叠 → 追加 `appendBranch(groupKey, row.issue.id, depth+1, ...)` 拉该父的子分支；
  - 展开的父项如果尚未注册分支，前端先渲染 `kind:"load_more"` 占位（`activate` 态）通过 `InfiniteScrollSentinel` 触发 `activateServerBranch`。
- 行缩进：`InlineTitle` 用 `style={{ paddingLeft: row.depth * 18 }}`（`table-view.tsx:632`）。
- 折叠按钮：`row.hasChildren` 时渲染 Chevron 切换 `onToggleParent`（`table-view.tsx:648-666`）。

### 2.4 关键语义：跨组是否跟随父列（与需求 4 直接相关）

- 后端现有语义是**跨组不跨列**：`TestIssueTableHierarchyDoesNotCrossGroups`（`issue_table_query_test.go:1224`）明确验证：
  - 一个 `done` 子项挂在 `todo` 父项下，子项在 `status:done` 分组中成为**独立 root**，不会出现在父项（todo）下；
  - 父项的 `direct_child_count` 在 todo 组里为 0（跨组子项不计入）。
- 也就是说，Table 的 hierarchy 是"**组内**的父子嵌套"，子项只有在与父项同组（同 status/assignee/property）时才折叠进父项下方。
- CLO-196 需求 4 要求"子项状态与父不同列时，跟随父列显示"——**这是当前后端尚未实现的新语义**，如果采纳，会影响 `issue_table_rows.go` 的 branchPredicate（root 分支、子分支都要改为「以父为准」而非「以自身 group 为准」），且与 `TestIssueTableHierarchyDoesNotCrossGroups` 现有行为冲突，需要 Architect 决策：
  - 方案 A：Board 复用 Table 现有"组内跟随"语义（改动小，但子项跨状态时仍会在自己状态的列里单飞）；
  - 方案 B：新增"子项强制跟随父列"查询语义（后端 + 测试改动，满足需求字面 4）。

---

## 3. Board 拖拽现状

### 3.1 库与基础设施

- `packages/views/package.json:60-62`：`@dnd-kit/core ^6.3.1`、`@dnd-kit/sortable ^10.0.0`、`@dnd-kit/utilities ^3.2.2`（apps/web 同）。
- `board-view.tsx`：`DndContext` + `PointerSensor(activationConstraint:{distance:5})` + `makeKanbanCollision`（pointerWithin → 卡片优先，否则 closestCenter，`drag-utils.ts:23`）+ `DragOverlay`。
- 列级 `useDroppable({id: group.id})`；卡片级 `useSortable({id: issue.id, data:{status}})`（`board-card.tsx:339`）。
- 拖拽期间本地列镜像 `useDragSettle`（`use-drag-settle.ts`，与 List/Swimlane 共享），冻结/回滚由它管。

### 3.2 移动语义与落库

- 同列排序（`sortBy=position` 时）：`arrayMove` + `computePosition` 计算新 position，`getMoveAnchors` 产出 `before_id/after_id`。
- 跨列移动：`getMoveUpdates(group, position)`（`drag-utils.ts:154`）按目标列产出 `{status}` 或 `{assignee_type, assignee_id}` 或 `{position}`（属性列）；属性列值变更走 `useSetIssueProperty`。
- 落库：`onMoveIssue(issueId, updates, onSettled)` → `use-issue-surface-actions.ts:78 moveIssue` → `useUpdateIssue` mutation（`mutations.ts:231`）：
  - 无 `move_intent` → `api.updateIssue(id, data)`（`PUT /api/issues/:id`）；
  - 有 `move_intent{before_id,after_id}` → `api.moveIssue(id, {..., before_id, after_id})`（`POST /api/issues/:id/move`，`client.ts:856`）。
- 后端 `issue_move.go`：校验字段白名单（status/assignee_type/assignee_id/parent_issue_id/project_id/before_id/after_id），解析锚点算 position，再委派 `UpdateIssue`（单条写路径，含实时/任务触发/父通知副作用）。

### 3.3 父子相关现状

- **没有任何"拖父联动子项"逻辑**：`handleDragEnd`（`board-view.tsx:485`）只对 `activeId` 单卡片调用 `onMoveIssue`，不会收集子树、不会级联更新。
- 子项本身支持单独拖拽（作为独立卡片），符合需求 3 的一半。
- Swimlane 有**父分组**语义可参考：`swimlane-view.tsx:259 buildParentLanes` 按 `parent_issue_id` 建泳道，卡片拖到某父泳道时 `moveUpdates: { parent_issue_id }`，即**单个子项变更父级**的能力已存在（走 `UpdateIssue`/`MoveIssue` 的 `parent_issue_id` 字段，后端 `issue.go:2808` 有父级变更 + 自引用/环检测）。但这是"子找父"，不是"父带子"。

### 3.4 Board 树形化将触及的拖拽改动点

- `board-column.tsx` 的 `SortableContext` 目前是「列内平铺 items = 全部卡片 id」。树形后需要：父卡=可折叠节点，子卡嵌套在父卡下方（类似 Table 的 depth 缩进），排序范围要能区分「整组移动」与「组内子卡移动」。
- `board-view.tsx` 的 `findColumn`/`computePosition`/`getMoveAnchors` 均基于平铺 id 数组，树形后需扩展为树结构上的定位（组内排序锚点、跨组整组移动锚点）。
- 后端 `MoveIssue`/`UpdateIssue` 是**单 issue** 写接口。若采纳"拖父联动子项"，需要前端对父+全部子项做批量写（可用现有 `POST /api/issues/batch-update`，`router.go:1101`），或后端新增级联语义。

---

## 4. 数据/接口清单

### 4.1 Board 相关查询

| 接口 | 方法/路径 | 用途 | 位置 |
|------|-----------|------|------|
| 表格行（Board/Table/List 共用） | `POST /api/issues/table/rows` | 每列/每父 cursor 分页拉 issue 行，含 `direct_child_count`、`parent_id`、`hierarchy` | `router.go:1088`，`issue_table_rows.go` |
| 分组枚举 | `POST /api/issues/table/groups` | assignee/property/compound 分组的列清单与计数 | `router.go:1087`，`issue_table_group.go` |
| 分面计数 | `POST /api/issues/table/facets` | status 等分面计数（列头总数） | `router.go:1089`，`issue_table_facets.go` |
| 子任务进度 | `GET /api/issues/child-progress` | `Map<parent, {done,total}>` | `router.go:1091`，`issue.go:2012` |
| 按父批量取子 | `GET /api/issues/children?parent_ids=` | `ListChildrenByParents`（detail 面板用） | `router.go:1092` |
| 单 issue 子列表 | `GET /api/issues/{id}/children` | `ListChildIssues` | `router.go:1123` |
| 平铺 issue 列表 | `GET /api/issues` | 旧 list 路径（部分 scope） | `router.go:1094` |
| 分组列表 | `GET /api/issues/grouped` | 旧 assignee 分组 | `router.go:1093` |

### 4.2 状态 / 关系更新

| 接口 | 方法/路径 | 说明 |
|------|-----------|------|
| 更新 issue | `PUT /api/issues/{id}` | 支持 `status`、`parent_issue_id`（含自引用 + 环检测）、`position`、assignee、project、stage、自定义属性值等；`include_sub_issues` 属查询参数不在写入侧。 |
| 移动 issue | `POST /api/issues/{id}/move` | 锚点(before/after) 相对定位，白名单字段，委托 `UpdateIssue`。 |
| 批量更新 | `POST /api/issues/batch-update` | 拖父联动子项的候选写路径。 |
| 自定义属性 | `PUT/DELETE /api/issues/{id}/properties/{propertyId}` | 属性列拖拽赋值。 |

### 4.3 parent-child 字段现状

- `Issue` 类型含 `parent_issue_id: string | null`（`packages/core/types/issue.ts:49`）。
- `UpdateIssueRequest` 含 `parent_issue_id`；`MoveIssueRequest` 也透传 `parent_issue_id`（`types/api.ts:58`）。
- 后端 `issue.go:2808-2846`：父级变更会校验「同 workspace 存在」「不能自指」「10 层环检测」。

### 4.4 后端要补什么（判断）

- **「子项跟随父列」若采纳**：需要修改 `/table/rows` 的 hierarchy branchPredicate，使子分支查询「按父所在组」而非「子自身 group」，并相应调整 `direct_child_count` 口径与 `TestIssueTableHierarchyDoesNotCrossGroups`。这是**唯一明确的后端改动点**。
- **「拖父联动子项」若采纳**：写路径复用 `batch-update` 或新增级联；若只要求前端遍历子树逐条 `moveIssue`，则后端无新接口。
- 其余（hierarchy 分页、direct_child_count、childProgressMap、折叠状态持久化）**前端即可复用现有能力，无需后端新增**。

---

## 5. 本地部署方式

### 5.1 一键命令

| 命令 | 作用 |
|------|------|
| `make dev` | 自动：检测工具 → 建 `.env`/`.env.worktree` → `pnpm install` → `ensure-postgres.sh` → `go run ./cmd/migrate up` → 并行起 `go run ./cmd/server` + `pnpm dev:web` |
| `make setup` | 依赖安装 + 建库 + 迁移（不起服务） |
| `make start` | 迁移 + 起前后端 |
| `make server` | 只起 Go 后端 |
| `make test` | Go 测试（`--race`），先确保 DB + 迁移 |
| `make check` | typecheck + TS tests + Go tests + Playwright E2E |
| `pnpm typecheck` / `pnpm test` / `pnpm build` | 前端 Turbo 命令 |
| `make db-up` / `db-down` / `db-reset` | Postgres 容器启停 / 重建 |

### 5.2 依赖与 Postgres

- 前置：Node v20+、pnpm v10.28.2、Go v1.26+、Docker（`scripts/dev.sh` 检测）。
- Postgres 由 docker compose 的 `postgres` 服务提供（`docker-compose.yml`）；默认连接 `postgres://multica:multica@localhost:5432/multica?sslmode=disable`（Makefile 默认值，`.env.example` 同）。
- 环境文件：`.env`（主 checkout）/ `.env.worktree`（worktree，`make worktree-env` 生成唯一 DB/端口），`scripts/ensure-postgres.sh` 负责拉起容器并等待就绪。

### 5.3 前后端启动细节

- 后端：`server/` 目录 `go run ./cmd/server`（Chi 路由 + sqlc + pgx；WebSocket 在 `/ws`）。端口 `PORT`（默认 8080）。
- 前端：`pnpm dev:web` → `turbo dev --filter=@multica/web`，Next.js 默认 3000。`NEXT_PUBLIC_API_URL`/`NEXT_PUBLIC_WS_URL` 指向后端。
- 登录：无 `RESEND_API_KEY` 时验证码打印到后端 stdout；或设置 `MULTICA_DEV_VERIFICATION_CODE=888888`（APP_ENV 非 production）固定验证码（`.env.example` 注释）。
- 迁移：`server/pkg/db` sqlc 生成 + `go run ./cmd/migrate up`（每个索引 `CREATE INDEX CONCURRENTLY` 独立迁移文件，AGENTS.md 硬规则）。

### 5.4 验证命令（供 Validation/Review）

- `make check`（全量）或 `pnpm typecheck && make test`。
- Board/Table 相关前端单测：`packages/views/issues/components/*.test.tsx`（board/swimlane/table 均有测试）。
- 后端表查询测试：`server/internal/handler/issue_table_query_test.go`（含 hierarchy 跨组测试）。

---

## 6. 分支现状

- **基线分支**：`Equipment_Department_Exploration`。
  - remote 最新 = `6d400f2d`（2026-08-05 Merge PR #4），本调研基于该 commit。
  - 注意：本地 checkout 的 `Equipment_Department_Exploration` 曾停在 `67e58d0f`（上游 main 同步点），调研时已 `git reset --hard origin/Equipment_Department_Exploration` 校正到 `6d400f2d`。
- **`feature/board-tree`**：
  - `git ls-remote --heads origin | grep board-tree` → 无；
  - 本地 `git branch -a | grep board-tree` → 无。
  - 结论：**仓库无同名 `feature/board-tree` 分支，可放心创建唯一特性分支**（由后续 Coding/Orchestrator 从基线创建，CLO-196 已要求所有角色共用）。

---

## 7. 风险分析

1. **语义冲突（高）**：需求 4「子项跟随父列」与后端现有「跨组不跨列」语义相悖，且被测试固化。若实现需求字面语义，需后端改动 + 测试重写，且可能影响 Table 现有行为（两个视图共用 `/table/rows`）。→ 必须先由 Architect 定夺，Research 建议：Board 单独请求时开启新语义、Table 保持现状（或两者统一但回归成本高）。
2. **Board 数据量大**：`board-column.tsx` 用 Virtuoso 虚拟滚动，树形嵌套后每列项数 = 节点数（父+子），且展开需惰性加载子分支（复用 Table 的 load_more/activate 模式），需防每列全量展开导致 N+1 请求。Table 已有成熟的惰性分支激活机制可照搬。
3. **拖拽复杂度（中-高）**：现有拖拽基于平铺 id 数组；树形后排序/移动锚点、DragOverlay 内容、同组 vs 跨组 vs 整组移动语义都要重做，且要与 `useDragSettle` 的乐观更新/回滚兼容。建议 Architect 先出拖拽状态机方案。
4. **批量写一致性（中）**：若「拖父联动子项」用 `batch-update`，需保证部分失败回滚语义与现有乐观 UI 一致；后端 `UpdateIssue` 单条路径已有的副作用（realtime、任务触发、父通知）在批量路径是否逐个触发需验证。
5. **进度环口径（低）**：`child-progress` 的 done 统计为 `status IN ('done','cancelled')`，与需求「x/y done」一致；树形后父卡进度环沿用现有 `childProgressMap` 即可。
6. **回归面（中）**：`/table/rows` 同时服务 Table/List/Board/Swimlane，任何 hierarchy 语义改动都会影响 Table 的 `TestIssueTableHierarchyDoesNotCrossGroups` 与多视图。需保证 Board 树形以「参数化」方式开关，避免破坏 Table。

---

## 8. 建议修改方案（供 Architect 决策）

1. **数据**：Board 树形直接复用 `/table/rows` 的 `hierarchy.enabled=true` + `parent_id` 惰性分支，加上 `direct_child_count`；不要在前端对平铺数据自建树。
2. **前端状态**：为 Board 增加与 `tableCollapsedParents` 同构的 `boardCollapsedParents`（或复用同一 store 字段），展开/折叠 UI 参考 `table-view.tsx` 的 `InlineTitle` Chevron + depth 缩进。
3. **后端**：
   - 若采纳「子项跟随父列」→ 新增/调整 hierarchy 查询语义（参数化，不影响 Table 现状）；
   - 若采纳「拖父联动子项」→ 提供批量级联写（batch-update 或新端点）。
4. **拖拽**：Architect 出树形拖拽状态机（整组移动 vs 子卡单独移动），drag payload 需含子树 id 集合；`DragOverlay` 可显示「父卡 + N 子」占位。
5. **测试**：后端补「子项跨组跟随父列」的语义测试（若采纳）；前端补 Board 树形展开/折叠/拖拽组件测试；Validation 覆盖 Table 回归。

## 9. 缺失信息

- 需求 3 的最终拖拽语义（整组 vs 单独）与需求 4 的「跟随父列」最终口径，需要 Architect 结合产品期望裁定后，Coding 才能动手。
- 是否允许 Board 与 Table 的 hierarchy 语义分叉（Board 跟随父列、Table 保持跨组不跨列），影响后端参数化方案，需 Orchestrator/Architect 明确。
