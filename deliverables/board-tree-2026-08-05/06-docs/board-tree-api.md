# 看板树状结构 — API / 数据契约说明（开发向）

**读者**: 对接看板数据、维护看板代码的开发者
**日期**: 2026-08-06
**核心结论**: **本次零后端改动、零新增端点、零破坏性变更**。树形展示、跟随父列、子树拖拽全部在前端实现。

---

## 1. 一句话

看板树状结构 = **前端用平铺查询结果按 `parent_issue_id` 组树** + 复用现成的 `childProgressMap` 做「有子」信号与进度环 + 复用 `childrenByParentsOptions` 惰性补拉分页边界缺失的子项。列镜像仍是 `Record<groupId, string[]>`（展平 id 序列），拖拽状态机 `useDragSettle` 未改动。

## 2. 复用的后端端点（契约不变）

| 端点 | 用途 | 说明 |
|------|------|------|
| `POST /api/issues/table/rows` | 列内平铺数据 | 仍以 `include_sub_issues=true` 平铺返回父子；`hierarchy.enabled` 保持 `false`（Board 不启用服务端 hierarchy 分支） |
| `GET /api/issues/child-progress` | 进度环 + 「有子」信号 | 全量 `Map<parent_id, {done,total}>`，Board 已消费 |
| `GET /api/issues/children?parent_ids=…` | 展开时补拉缺失的直接子 | `childrenByParentsOptions`；`parent_ids` 为空时 `enabled:false`，无空查询 |
| `POST /api/issues/:id/move` | 父/子移动 | 锚点 `before_id/after_id` 定位 position；拖子跨列带 `parent_issue_id: null`（白名单已含） |
| `POST /api/issues/batch-update` | 后代批量同步分组字段 | `{issue_ids, updates:{status/assignee_*}}`；不支持 property 值、不支持 move_intent |

## 3. 前端新增/改动状态（view-store）

### 3.1 新增持久化字段 `boardCollapsedParents`

```typescript
// packages/core/issues/stores/view-store.ts
boardCollapsedParents: string[];                    // 看板中已折叠的父 issue id
toggleBoardParentCollapsed: (issueId: string) => void;
```

- **独立于 Table**：与 `tableCollapsedParents` 互不影响，切换视图不串扰。
- **持久化**：`viewStorePersistOptions.partialize` 增补该键；`mergeViewStatePersisted` 增加 `Array.isArray` 守卫，旧快照无此键 → 默认 `[]`（向前兼容）。
- 展开/折叠的「有子」信号：`hasChildren = childProgressMap.total > 0 || childrenMap.length > 0`（后者是 `childProgressMap` 尚未加载时子已在场的兜底，W3 已确认采纳该 OR 语义）。

### 3.2 无新增开关

- 不新增 `boardHierarchy`：复用现有 `showSubIssues`（默认 `true`）。`showSubIssues=false` → 查询 `include_sub_issues=false`，仅根节点，且不渲染 Chevron。

## 4. 新增前端模块与文件

| 文件 | 类型 | 职责 |
|------|------|------|
| `packages/views/issues/components/board-tree-model.ts` | **新增** | 纯函数：`buildChildrenMap`（按父分组 + position/created_at 排序）、`collectSubtreeIds`（DFS 全部后代）、`flattenBoardTree`（展平 + nodeInfo：depth/hasChildren/collapsed） |
| `packages/views/issues/utils/drag-utils.ts` | 改 | 新增 `buildBoardTreeColumns`（子跟随父列）、`getSubtreeBlock`/`moveBlockInto`/`moveBlockWithin`/`getSubtreeMoveAnchors`/`computeBlockPosition`（子树块移动数学）、`getSubtreeSyncUpdates`（status/assignee 同步、property 列返回 null） |
| `packages/views/issues/components/board-view.tsx` | 改 | 树形列构建、`handleDragOver/End` 子树移动、DragOverlay「+N 子 / 转顶层」徽章、`childrenByParentsOptions` 惰性补拉 |
| `packages/views/issues/components/board-column.tsx` | 改 | 接收 `nodeInfo`，卡片按 `depth` 缩进渲染 |
| `packages/views/issues/components/board-card.tsx` | 改 | `depth/hasChildren/collapsed/onToggleCollapsed`；父卡 Chevron 按钮（`ChevronDown/Right`） |
| `packages/core/issues/stores/view-store.ts` | 改 | `boardCollapsedParents` + toggle + persist/merge 守卫 |

## 5. 拖拽落库序列

### 5.1 同列排序（sortBy=position）

```
仅父 moveIssue（锚点取子树块边界外相邻 id）：{ before_id, after_id, position }
→ 无 batch-update。子 position 不变、渲染跟随父。
```

### 5.2 跨列移动（status / assignee 分组）

```
1. POST /api/issues/:id/move { status|assignee_*, before_id, after_id }  → 父定位+分组
2. POST /api/issues/batch-update { issue_ids: [全部直接/间接后代], updates: { status|assignee_* } }
```

- 后代集合来自前端 `childrenMap` 递归（一次 batch，无 N+1）。
- **两阶段非原子**（设计 R4）：步骤 2 失败 → toast「部分子任务状态未同步」，父已到位、子留在原列，可重试。

### 5.3 拖子跨列（脱离父 → 顶层）

```
POST /api/issues/:id/move { status|assignee_*, parent_issue_id: null, before_id, after_id }
```

- 仅当**父卡在当前视图可见**时触发 detach（`map.has(parent_issue_id)`）。父不可见的子本就按 root 渲染，拖拽不会误清父子链。
- 子自身有后代时，其子树仍跟随该子。

### 5.4 属性（select）分组列

- 整组跨列：仅父 `moveIssue` + 属性值走既有 `useSetIssueProperty` 单条路径；`getSubtreeSyncUpdates` 返回 null，**后代不级联改属性**（设计 R5，v1 接受）。

## 6. 错误码（复用现有）

| 场景 | 状态 | 说明 |
|------|------|------|
| 拖子跨列设置自身为父（cycle） | 400 | 现有 `updateIssue` / batch cycle 检测 |
| 锚点冲突/过密 | 409 | 现有 move 端点 |
| batch 部分成功 | 200 `{updated<N}` | 前端对比后 toast |
| 属性列整组级联不支持 | 前端降级 | 仅父移动 + 提示 |

## 7. 已知限制（v1）

| # | 限制 | 演进方向 |
|---|------|---------|
| R4 | 两阶段同步非原子（父成子败） | v1.1 `POST /:id/cascade-move` 事务端点 |
| R5 | property 列整组不级联子属性 | v1.1 循环 `setIssueProperty` 或后端级联 |
| R6 | batch-update 不支持 move_intent → 后代 position 不精确 | v1 接受（子按新列自然排序/末尾） |
