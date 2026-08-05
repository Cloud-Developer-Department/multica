# Board 看板树状结构 — 前端实现报告（CLO-222）

**日期**: 2026-08-05
**角色**: Frontend-Coding（科比）
**依据**: CLO-221 设计 `deliverables/board-tree-2026-08-05/02-design/board-tree-design.md`
**分支**: `feature/board-tree`（基线 `main` @ `67e58d0f`）

---

## 0. 结论

按 Architect 设计完整实现看板树状结构：**前端 7 处文件改造 + 新增 `board-tree-model.ts`，后端/数据库/API 零变更**。`pnpm typecheck` 全包通过，单测新增/扩展全部通过（仅 4 个既有 locale parity 用例失败，基线同样失败，与本改动无关）。

---

## 1. 实现内容

### 1.1 新增 `board-tree-model.ts`（纯函数 + 类型）

```typescript
export interface BoardNodeInfo {
  depth: number;
  hasChildren: boolean;
  collapsed: boolean;
}

export function buildChildrenMap(issues): Map<string, Issue[]>   // 按父分组，position 升序
export function collectSubtreeIds(childrenMap, rootId): string[] // 递归取全部后代（整组移动 payload）
export function flattenBoardTree(roots, childrenMap, childProgressMap, collapsedSet)
  // → { ids, nodeInfo }：展平前序序列 + 每卡 depth/hasChildren/collapsed
```

- **列镜像仍为 `Record<groupId, string[]>`（展平前序 id 序列），`useDragSettle` 零改动**。
- 折叠父节点时其子节点从展平序列消失（但仍计 `hasChildren`，Chevron 保留）。
- `hasChildren` 信号 = `childProgressMap.total > 0`（D6，workspace 全量、与进度环口径一致）。

### 1.2 `drag-utils.ts` 树形化

| 新函数 | 作用 |
|--------|------|
| `buildBoardTreeColumns(issues, groups, grouping, collapsedSet, childProgressMap, knownOptionIds)` | 树形列构建：子归父列，父不可见时子按自身值落列；返回 `{ columns, nodeInfo }` |
| `getSubtreeBlock(ids, activeId, nodeInfo)` | 拖拽卡 + 其展平后代（连续块） |
| `moveBlockWithin` / `moveBlockInto` | 同列重排 / 跨列插入子树块 |
| `getSubtreeMoveAnchors(ids, block)` | 子树块边界外相邻节点作 before/after 锚点 |
| `computeBlockPosition(ids, block, map)` | 块的中点插入 position（仅父精确锚定） |
| `getSubtreeSyncUpdates(group)` | 后代 batch-update 的 group 字段（status/assignee；property 列返回 null → 仅父移动，R5） |

- **`buildColumns` 保持原样**，list-view / swimlane 继续使用，零回归。
- `DragMoveUpdates` 类型扩展 `parent_issue_id?: string | null` 支持拖子跨列脱离。

### 1.3 `board-view.tsx`

- **树形列构建**：`treeBuild = buildBoardTreeColumns(...)` 驱动 `useDragSettle` 初始值 + 重同步 effect；`nodeInfo` 经 ref 随列冻结。
- **惰性补拉（R2）**：对已展开但子集不全的父节点，用 `childrenByParentsOptions` 批量补拉完整子集并合入 `treeIssues` 再建树；`showSubIssues=false` 时跳过。
- **handleDragOver/End 子树块移动**：
  - 同列重排：`moveBlockWithin` 移动整块，**只写父 position + 边界锚点**，不写 group、无 batch。
  - 跨列移动：`moveBlockInto` 移块 → 父 `moveIssue`（status/assignee/position + 块锚点）+ 后代 `getSubtreeSyncUpdates` batch 两阶段。
  - 拖子跨列：`parent_issue_id: null` 脱离父转顶层任务（仅当父在当前视图可见，D2 语义对齐）。
  - property 列：仅父移动（R5）。
- **DragOverlay**：「+N 子」徽标 + 子卡拖拽时「将转为顶层任务」提示。
- 展开补拉失败的 batch → toast「部分子任务状态未同步」。

### 1.4 `board-column.tsx`

- 数据从平铺 `Issue[]` → 展平节点序列（仍以 `issueMap` 解析 id → Issue，Virtuoso `computeItemKey` 用 `issue.id` 保持稳定）。
- 新增 `nodeInfo` / `onToggleCollapsed` props，按 id 查树向卡片传 depth/hasChildren/collapsed。

### 1.5 `board-card.tsx`

- `BoardCardContent` 增可选 props：`depth`（`marginLeft: depth * 18` 缩进）、`hasChildren`、`collapsed`、`onToggleCollapsed`。
- 父卡渲染 Chevron（点击 stopPropagation，不触发拖拽/导航），子卡按深度缩进；进度环保留。
- 所有新增 props 可选，swimlane / DragOverlay 既有调用不受影响。

### 1.6 `view-store.ts`

- 新增 `boardCollapsedParents: string[]` + `toggleBoardParentCollapsed(issueId)`。
- persist `partialize` 增补；`mergeViewStatePersisted` 增 `Array.isArray` 兼容（旧快照无此键 → 默认 `[]`）。
- 独立于 Table 折叠状态；my-issues / actor-issues store 通过 `viewStoreSlice` 自动继承。

### 1.7 i18n

四个 locale（en/zh-Hans/ja/ko）`issues.json` 增补 `board.*` 5 键：展开/折叠子任务、子任务计数、转顶层提示、子任务同步失败。

---

## 2. 测试

| 文件 | 覆盖 |
|------|------|
| `board-tree-model.test.ts`（新增） | buildChildrenMap 分组/排序、collectSubtreeIds 递归、flattenBoardTree 展开/折叠/hasChildren 信号 |
| `drag-utils.test.ts`（扩展） | buildBoardTreeColumns 跟随父列/折叠/孤儿落列、getSubtreeBlock、moveBlock*、锚点、computeBlockPosition、getSubtreeSyncUpdates |

**验证命令与结果**：
- `pnpm typecheck` → 6/6 包通过（core/views/web/desktop 等）。
- `pnpm --filter @multica/views test` → 3074 passed；仅 4 个既有 locale parity 失败（settings/members ja·ko，基线即失败，与本改动无关）。
- `pnpm --filter @multica/core test` → 101 files / 1066 tests 全通过。

---

## 3. 影响范围

- **改动文件**：`board-view.tsx`、`board-column.tsx`、`board-card.tsx`、`drag-utils.ts`、`view-store.ts`、4×locale，新增 `board-tree-model.ts` + 2 个测试文件。
- **零改动**：后端全部、`use-drag-settle.ts`、`queries.ts`（复用 `childrenByParentsOptions`）、list-view/swimlane/table。
- **兼容**：`buildColumns` 未动；`BoardCardContent`/`DraggableBoardCard` 新 props 全部可选；`boardCollapsedParents` 为新增持久化字段带 merge 守卫。

---

## 4. 风险与建议测试点

| # | 风险 | 等级 | 现状/缓解 |
|---|------|------|-----------|
| R1 | 跟随父列 ≠ Table「跨组不跨列」 | 中 | 有意语义（需求 4）；不改后端、Table 零影响 |
| R2 | 分页边界子项缺失 | 中 | 展开时 `childrenByParentsOptions` 惰性补拉完整子集 |
| R4 | 整组移动两阶段非原子 | 中 | 后代 batch 失败 toast 提示，可重试 |
| R5 | property 列整组级联不支持 | 低 | 仅父移动 + 后代跟随父列渲染 |
| R7 | 拖子跨列改变父子结构 | 低 | DragOverlay「将转为顶层任务」提示 |

**建议 Validation（CLO-223）测试点**：
1. 展开/折叠：父卡 Chevron 展开显示缩进子卡，折叠隐藏；跨视图切换不串扰。
2. 拖拽语义：拖父=整组移动（含后代）；拖叶子子=单独；拖子跨列=脱离父转顶层。
3. 子项跟随父列：子状态与父不同时仍显示在父列；父不可见时子按自身值落列。
4. 进度环：父卡 x/y done 保留。
5. `showSubIssues=false` 仅根节点。
6. Table/List/Swimlane 行为不变（回归）。

---

## 5. Diff / PR 说明

- 本环节**零远端 push**（按团队规范）；代码提交在本地 `feature/board-tree` 分支。
- `frontend.diff` 为 `git diff 8dd51966..HEAD -- packages/` 仅前端文件产物。
- PR 由 DevOps（CLO-226）统一创建：base `Equipment_Department_Exploration`，head `feature/board-tree`。
