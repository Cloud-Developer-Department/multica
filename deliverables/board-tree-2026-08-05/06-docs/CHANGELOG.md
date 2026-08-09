# ChangeLog — 看板树状结构（2026-08-06）

> 本条目为 `feature/board-tree` 变更记录，发布后并入主仓库 changelog（`apps/web/features/landing/i18n/*.ts` 或 docs 站 changelog 页）。

## [Unreleased] — Board 看板树状结构

### Added（新增）
- 看板列内父子 issue 树形展示：父卡 Chevron 展开/折叠、子卡按深度缩进（`depth * 18px`）、支持多级嵌套。
- 统一「子树移动」拖拽语义：拖父整组、拖叶子子单独、拖子跨列脱离父转顶层。
- 拖拽预览徽章：`子任务 +N` 与「将转为顶层任务」（detach）提示。
- 子任务跟随父列显示（与 Table hierarchy 语义一致）。
- 折叠状态 `boardCollapsedParents` 持久化，独立于表格视图的 `tableCollapsedParents`。
- 展开父卡时经 `childrenByParentsOptions` 惰性补拉分页边界缺失的子项。

### Changed（变更）
- `buildBoardTreeColumns` 替代平铺 `buildColumns`：子项归属父列，父不可见时按自身值落列。
- 跨列拖父：父 `move` + 后代 `batch-update` 两阶段同步 status/assignee。
- 同列重排只写 `position`，绝不写分组字段（保护跟随父列的子卡）。

### Fixed（修复）
- 拖子跨列仅在父卡当前可见时置 `parent_issue_id: null`，避免误清父不可见子卡的父子链。
- 拖到自身后代为 no-op，防循环引用。

### Removed（移除）
- 无。

### 技术说明
- 后端零改动、零迁移；`packages/core/issues/stores/view-store.ts` 新增 `boardCollapsedParents` + `toggleBoardParentCollapsed`（persist/merge 带数组守卫）。
- 新增纯函数模块 `board-tree-model.ts`（`buildChildrenMap` / `collectSubtreeIds` / `flattenBoardTree`）。

### 涉及文件
- `packages/views/issues/components/{board-view,board-column,board-card,board-tree-model}.tsx/.ts`
- `packages/views/issues/utils/drag-utils.ts`
- `packages/core/issues/stores/view-store.ts`
- 4 个 locale `issues.json`（en/ja/ko/zh-Hans）：`expand_subtree` / `collapse_subtree` / `subtree_count` / `detach_hint` / `subtree_sync_failed`

### 已知限制（v1）
- 属性列整组不级联子属性值（R5）。
- 两阶段子树同步非原子，失败有 toast 提示（R4）。
- batch-update 不支持 move_intent，后代 position 不精确（R6）。
