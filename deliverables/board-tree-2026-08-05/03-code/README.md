# 03-Code 前端实现 交付索引（本轮 CLO-222）

**Issue**: CLO-222 【前端】看板视图树状结构实现
**日期**: 2026-08-05
**角色**: Frontend-Coding（科比）
**父 Issue**: CLO-218 看板视图支持父子 issue 树状结构（展开/折叠 + 拖拽语义）
**依据**: `02-design/board-tree-design.md`（CLO-221 权威设计）
**分支**: `feature/board-tree`（基线 `main` @ `67e58d0f`）

## 交付物清单

| 文件 | 说明 | 状态 |
|------|------|------|
| `board-tree-implementation.md` | **本轮**前端实现报告（改动清单 / 关键实现 / 测试 / 影响范围 / 风险） | 权威，以本文件为准 |
| `frontend.diff` | 代码变更 Diff（`git diff 8dd51966..HEAD -- packages/`，仅前端文件） | 参考 |

## 实现范围（前端 7 处改造 + 1 新增）

| 文件 | 变更 | 说明 |
|------|------|------|
| `packages/views/issues/components/board-view.tsx` | 改 | `buildBoardTreeColumns` 树形列构建；`handleDragOver/End` 子树块移动；DragOverlay「+N 子」+ 脱离提示；`childrenByParentsOptions` 展开惰性补拉 |
| `packages/views/issues/components/board-column.tsx` | 改 | 数据从平铺 `Issue[]` → 展平节点序列；卡片承载缩进；Virtuoso `computeItemKey` 仍用 `issue.id`（稳定） |
| `packages/views/issues/components/board-card.tsx` | 改 | `DraggableBoardCard`/`BoardCardContent` 增 `depth/hasChildren/collapsed/onToggleCollapsed`；父卡 Chevron + 缩进；进度环保留 |
| `packages/views/issues/utils/drag-utils.ts` | 改 | `buildBoardTreeColumns`（子归属父列）；`getSubtreeBlock`/`moveBlockWithin`/`moveBlockInto`/`getSubtreeMoveAnchors`/`computeBlockPosition`/`getSubtreeSyncUpdates` |
| `packages/core/issues/stores/view-store.ts` | 改 | `boardCollapsedParents` + `toggleBoardParentCollapsed` + persist/merge 数组兼容（独立于 Table） |
| `packages/views/locales/{en,zh-Hans,ja,ko}/issues.json` | 改 | 新增 `board.expand_subtree` / `collapse_subtree` / `subtree_count` / `detach_hint` / `subtree_sync_failed` |
| `packages/views/issues/components/board-tree-model.ts` | **新增** | `BoardTreeNode` 类型 + `buildChildrenMap` / `flattenBoardTree` / `collectSubtreeIds` 纯函数 |
| 后端 | **零改动** | 无 DB / handler / API / 测试变更 |

## 关键实现点（与设计裁定对齐）

1. **前端组树（D1）**：`buildBoardTreeColumns` 复用现有平铺查询，按 `parent_issue_id` 建 childrenMap；根 = 无父或父不在当前视图；子归父列。展开父卡时 `childrenByParentsOptions` 惰性补拉完整子集并合入树构建（R2）。
2. **跟随父列（D2）**：子渲染列由父卡决定，无论子自身 status/assignee/property；父不可见时子按自身值落列成为 root。Table/List/Swimlane 零影响。
3. **拖拽语义（D3/D4）**：统一「子树移动」——`getSubtreeBlock` 取拖拽卡 + 其展平后代为一块；拖父=整组（父 move + 后代 `batch-update` 两阶段），拖叶子子=单独；拖子跨列=`parent_issue_id` 置 null 转顶层任务（仅当父可见）。同列重排只写 position，不写 group（子自身分组值可与列不同）。
4. **折叠状态（D5）**：`boardCollapsedParents` 持久化，独立于 Table，复用其状态管理模式；`showSubIssues=false` 时仅根节点、无箭头。
5. **「有子」信号（D6）**：`childProgressMap.total > 0` 驱动 Chevron 与「+N 子」徽标。

## 测试

- `packages/views/issues/components/board-tree-model.test.ts`（新增）：buildChildrenMap / flattenBoardTree / collectSubtreeIds 纯函数覆盖。
- `packages/views/issues/utils/drag-utils.test.ts`（扩展）：buildBoardTreeColumns 树形场景、getSubtreeBlock / moveBlock* / getSubtreeMoveAnchors / computeBlockPosition / getSubtreeSyncUpdates。
- 验证：`pnpm typecheck`（6/6 包通过）、`pnpm --filter @multica/views test`（3074 通过，仅 4 个既有 locale parity 用例失败——基线同样失败，与本改动无关）。
- 建议 Validation 补充：组件级展开/折叠交互、整组/单卡拖拽、拖子跨列脱离（参考 `swimlane-view.test.tsx` / `table-view-editing.test.tsx`）。

## 影响范围与风险

- **影响**：仅 Board 视图渲染与拖拽；Table/List/Swimlane 共享的 `buildColumns` 未改（新增 `buildBoardTreeColumns`，list-view 仍用旧函数），零回归。
- **风险对照**（详见设计 §5）：R1 跟随父列 ≠ Table「跨组不跨列」为有意语义；R2 分页边界补拉已实现；R4 两阶段非原子 → 后代 batch 失败 toast「部分子任务状态未同步」；R5 property 列整组级联不支持 → 仅父移动；R7 拖子跨列置 null → DragOverlay 明确提示「将转为顶层任务」。

## 下一环节

交给 Validation（邓肯）验证（CLO-223），建议重点回归：展开/折叠、拖拽语义（父拖整组 / 子拖单个 / 子跨列脱离）、子项跟随父列、进度环、Table 视图行为不变。
