# Board 看板树状结构 — 架构设计方案

**Issue**: CLO-221（父 CLO-218）
**日期**: 2026-08-05
**角色**: Architect（阿基米德）
**依据**: CLO-220 调研报告（`deliverables/board-tree-2026-08-05/01-research/board-tree-research-report.md`）
**基线**: `main` @ `67e58d0f`；特性分支 `feature/board-tree`

---

## 0. 需求理解

> 将 Board/Kanban 视图中父子 issue 的「平铺展示」改为「树状展示」：每列内父卡可展开/折叠、子卡缩进显示；父卡保留子任务进度环；明确拖拽语义（拖父=整组、拖子=单独）；子项跟随父列显示；优先复用 Table 视图已就绪的 `direct_child_count`、`childProgressMap` 与展开/折叠实现。

一句话：**Board 列内父子以树形嵌套展示，交互与数据层对齐 Table hierarchy，拖拽语义以「子树移动」为统一规则。**

---

## 1. 架构设计

### 1.1 模块分层

```
┌────────────────────────────────────────────────────────────────┐
│  issues-surface.tsx (视图分发，无改动)                            │
│   └── board-view.tsx          DndContext + 列构建(树形化)          │
│         ├── board-column.tsx  列内展平树形渲染 + Virtuoso           │
│         │      └── board-card.tsx  深度缩进 + Chevron 折叠按钮      │
│         ├── use-drag-settle.ts 列镜像(展平 id 数组) —— 无改动        │
│         └── drag-utils.ts     树形列构建/子树块锚点/批量更新        │
├────────────────────────────────────────────────────────────────┤
│  core/issues/stores/view-store.ts   boardCollapsedParents + toggle │
│  core/issues/queries.ts              childrenByParentsOptions 复用 │
├────────────────────────────────────────────────────────────────┤
│  后端：零改动（复用 /issues/children、/issues/:id/move、batch-update）│
└────────────────────────────────────────────────────────────────┘
```

### 1.2 关键决策（Architect 裁定）

| # | 决策点 | 裁定 | 理由 |
|---|--------|------|------|
| D1 | 树形数据来源 | **前端组树**：复用 Board 现有平铺查询（`include_sub_issues=true`），按 `parent_issue_id` 前端建树；展开父卡时用 `childrenByParentsOptions` 惰性补拉完整子集 | 管线无后端阶段；改动集中在渲染层；`/table/rows` 子分支是「组内」语义，拿不到跨状态子项，仍需补拉，故不引入 hierarchy 分支重构 |
| D2 | 「子项跟随父列」 | **采纳方案 B 的纯前端等价实现**：子项渲染位置由父卡所在列决定，无论子自身 status/assignee/property；父不可见时子按自身分组值落列成为 root | 需求 4 明确；不改后端，Table/List/Swimlane 零影响；与后端「跨组不跨列」的差异是 Board 有意语义（见风险 R1） |
| D3 | 拖拽语义 | **统一「子树移动」**：拖拽任意卡片 = 移动该卡及其全部后代；叶子子卡即「单独移动」 | 需求 3 的「或更合理语义」裁量；父=整组、叶子子=单独是同一规则的两个极端；规则统一、无歧义 |
| D4 | 拖子跨列 | **子脱离父**：跨列拖子时 `parent_issue_id` 置 null，成为目标列独立任务 | 「跟随父列」与「拖子独立」的内在冲突，置 null 是最不歧义语义；拖拽预览提示「将转为顶层任务」 |
| D5 | 折叠状态 | view-store 新增 `boardCollapsedParents`（独立于 table），持久化 | 与 Table 状态管理同构；视图隔离 |
| D6 | 展开箭头「有子」信号 | `childProgressMap.total > 0`（已加载、workspace 全量、与进度环口径一致） | `direct_child_count` 在 `hierarchy=false` 下恒 0，Board 查询不开启 hierarchy；childProgressMap 已就绪零成本 |

### 1.3 调用关系（树形化后的数据流）

```
issues-header.tsx (showSubIssues / cardProperties)
   ▼
use-issue-surface-controller.ts ── tableQuerySpec(include_sub_issues=showSubIssues)
   │  ├── useIssueStatusBranches   ── /table/rows (group=status, hierarchy=false, 平铺含子)
   │  └── useIssueGroupBranches    ── /table/groups + /table/rows (每列平铺)
   │  └── childProgressMap         ← GET /issues/child-progress (全量 Map<parent,{done,total}>)
   ▼
BoardViewImpl
   ├── 前端组树: childrenMap = groupBy(parent_issue_id)
   │     · 根 = parent_issue_id 为 null 或 父不在当前视图的 issue
   │     · 子归属父列：子渲染列 = 父的列（父可见时）；否则按自身分组值
   ├── buildBoardTreeColumns(...) → 每列展平「树节点序列」(id 数组 + depth/children 表)
   ├── useDragSettle(展平 id 数组) ── 列镜像/冻结/回滚（复用）
   ├── DndContext → handleDragStart/Over/End（子树块移动）
   ├── BoardColumn(展平节点)
   │     └── Virtuoso → DraggableBoardCard(depth / hasChildren / collapsed / onToggle)
   └── DragOverlay(父卡 + 「+N 子」占位)
落库: 父 moveIssue(锚点) + 后代 batch-update(status) ｜ 拖子跨列 moveIssue(parent_issue_id=null)
```

---

## 2. 数据模型

### 2.1 数据库（无变更）

复用 `issue` 表现有字段，零迁移：

| 字段 | 语义 |
|------|------|
| `parent_issue_id` (UUID?) | 直接父，NULL=根 |
| `status` / `assignee_*` / `properties` | 分组维度（列归属）；跟随父列规则覆盖子项自身分组值 |
| `position` (FLOAT) | 手动排序位置（中点插入），树形整组移动时仅父精确锚定 |

### 2.2 前端树模型（新增，纯前端类型）

```typescript
// packages/views/issues/components/board-tree-model.ts（新增，纯函数+类型）
interface BoardTreeNode {
  issue: Issue;
  depth: number;             // 0=根
  hasChildren: boolean;      // childProgressMap.get(id)?.total > 0
  collapsed: boolean;        // boardCollapsedParents.includes(id)
  children: BoardTreeNode[]; // 直接子（按 position ASC）
}

// 列镜像仍为 Record<groupId, string[]>（展平 id 顺序），useDragSettle 零改动；
// 树结构（depth/children）由 board-view 单独维护，渲染列时按 id 查树。
```

**空值语义**：
- `parent_issue_id = null`：根节点，按自身分组值落列。
- 子项自身 `status/assignee/property` ≠ 父：**渲染时仍归属父列**（D2）；仅当父不可见（父被过滤 / 无父 / 父不在当前分页）时按自身值落列。
- `childrenMap` 缺项（分页边界）：展开父卡时 `childrenByParentsOptions` 补拉，不显示缺子状态。

### 2.3 展开/折叠状态（view-store 新增）

```typescript
// packages/core/issues/stores/view-store.ts
boardCollapsedParents: string[];                    // 折叠的父 issue id
toggleBoardParentCollapsed: (issueId: string) => void;

// persist partialize 增补 boardCollapsedParents；
// mergeViewStatePersisted 增补 Array.isArray 兼容（与 tableCollapsedParents 同款）
```

- **不新增 `boardHierarchy` 开关**：复用现有 `showSubIssues`（默认 true）控制树形 on/off；`showSubIssues=false` 时查询 `include_sub_issues=false` → 仅根节点（等价 Table 关闭子项显示）。

---

## 3. API 契约

**零新增端点，零破坏性变更。** 复用现有接口：

| 端点 | 用途 | 说明 |
|------|------|------|
| `POST /api/issues/table/rows` | 列平铺数据 | 现用 `hierarchy: {enabled:false}` 不变；子项靠 `include_sub_issues=true` 平铺返回 |
| `GET /api/issues/child-progress` | 进度环 + 「有子」信号 | 现有，Board 已消费 |
| `GET /api/issues/children?parent_ids=…` | 展开补拉直接子 | `childrenByParentsOptions`（Swimlane 已验证），全量子项不过滤状态 |
| `POST /api/issues/:id/move` | 父/子移动 | 锚点 `before_id/after_id` 定位 position；拖子跨列带 `parent_issue_id: null`（白名单已含） |
| `POST /api/issues/batch-update` | 后代批量同步 | `{issue_ids, updates:{status/assignee_*}}`；不支持 move_intent → 后代 position 不精确（v1 接受） |

### 3.1 拖拽调用序列

**场景 A：同列排序（sortBy=position）**
- 拖父（有子）：整块重排。写：仅父 `moveIssue`（锚点取子树块边界外的相邻节点）；子 position 不变、渲染跟随父。**无 batch**。
- 拖子（叶子）：仅子 `moveIssue`（子树块=自身）。

**场景 B：跨列移动（status / assignee 分组）**
```
1. 父: POST /api/issues/:id/move { status:"done", before_id, after_id }   → 父定位+状态
2. 后代: POST /api/issues/batch-update
          { issue_ids: [全部直接/间接后代], updates: { status: "done" } }  → 子状态同步
```
- 后代集合来自前端 childrenMap（递归）；一次 batch 请求，无 N+1。
- 失败降级：步骤 1 失败 → 回弹（现有 settle 逻辑）；步骤 2 失败 → toast「部分子任务状态未同步」，父已到位、子留在原列，可重试（两阶段非原子，v1.1 可演进 cascade 端点）。

**场景 C：拖子跨列（脱离父，转顶层任务）**
```
POST /api/issues/:id/move { status:"done", parent_issue_id: null, before_id, after_id }
```
- 拖拽预览显示「将转为顶层任务」；子自身有后代时，其子树仍跟随该子（同 D3 递归规则）。

**场景 D：property 分组列（v1 限制）**
- 整组跨列：仅父 `moveIssue` + 子 batch-update 无法写属性值（batch-update 不支持 property）。v1 裁定：property 列整组跨列移动**仅移动父**（属性值走既有 `useSetIssueProperty` 单条路径），后代跟随父列渲染但不改属性；如需后代属性级联，循环 `setIssueProperty`（v1.1）。记录于风险 R5。

### 3.2 错误码（复用现有）

| 场景 | 状态 | 说明 |
|------|------|------|
| 拖子跨列设置自身为父（cycle） | 400 | 现有 `updateIssue` / batch cycle 检测 |
| 锚点冲突/过密 | 409 | 现有 move 端点 |
| batch 部分成功 | 200 `{updated<N}` | 前端对比后 toast 提示 |
| 属性列整组级联不支持 | 前端降级 | 仅父移动 + 提示 |

---

## 4. 影响范围与迁移

### 4.1 涉及文件

| 文件 | 变更 | 说明 |
|------|------|------|
| `packages/views/issues/components/board-view.tsx` | 改 | `buildBoardTreeColumns` 树形列构建、`handleDragEnd` 子树块移动、DragOverlay「+N 子」、展开补拉 `childrenByParentsOptions` |
| `packages/views/issues/components/board-column.tsx` | 改 | 数据从平铺 `Issue[]` → 展平节点序列；缩进由卡片承载；Virtuoso `computeItemKey` 仍用 `issue.id`（稳定） |
| `packages/views/issues/components/board-card.tsx` | 改 | `DraggableBoardCard` 增 `depth/hasChildren/collapsed/onToggle`；父卡 Chevron + 缩进；进度环保留 |
| `packages/views/issues/utils/drag-utils.ts` | 改 | `buildColumns` → 树形化（子归属父列）；`getSubtreeBlock`/子树锚点；整组移动 updates 构造 |
| `packages/views/issues/components/use-drag-settle.ts` | 无改动 | 列镜像仍 `Record<groupId, string[]>`（展平 id 序列） |
| `packages/core/issues/stores/view-store.ts` | 改 | `boardCollapsedParents` + toggle + persist/merge 数组兼容 |
| `packages/core/issues/queries.ts` | 无改动 | `childrenByParentsOptions` 已存在，直接复用 |
| `packages/views/issues/components/board-tree-model.ts` | **新增** | `BoardTreeNode` 类型 + `buildChildrenMap`/`flattenBoardTree`/`collectSubtreeIds` 纯函数 |
| `packages/views/issues/components/issues-header.tsx` | 可省略 | 树形文案/提示（如需） |
| 后端 | **零改动** | 无 DB、无 handler、无测试改动 |

### 4.2 迁移与兼容

- **无数据库迁移**、无外键、无索引变更。
- **无 API 破坏**：无新增/变更端点。
- **向前兼容**：`boardCollapsedParents` 为新增持久化字段，`mergeViewStatePersisted` 加数组守卫，旧快照无此键 → 默认 `[]`。
- **视图隔离**：折叠状态独立于 Table，切换视图不串扰。
- **特性分支**：本轮交付并入 `feature/board-tree`（base `Equipment_Department_Exploration`），PR 由 DevOps 创建。

---

## 5. 风险与建议

| # | 风险 | 等级 | 缓解/降级 |
|---|------|------|-----------|
| R1 | **Board「跟随父列」≠ Table「跨组不跨列」**：语义分叉 | 中 | 需求 4 明确要求，属有意语义；不改后端、Table 零影响。文档+Review 确认；v1.1 可在 Table 侧评估是否跟进 |
| R2 | **分页边界子项缺失**：父在 page1、子跨页 | 中 | 展开父卡时 `childrenByParentsOptions` 惰性补拉全量子项；`childProgressMap.total>0` 保证箭头出现 |
| R3 | **大量子项渲染性能**：展开大树节点暴增 | 中 | 列内 Virtuoso 虚拟化不变；`computeItemKey=issue.id` 稳定；子树数据 `useMemo`；childProgressMap 无额外请求 |
| R4 | **整组移动两阶段非原子**（父成子败） | 中 | 步骤 2 失败 toast + 重试；v1.1 演进 `POST /:id/cascade-move` 事务端点（记录为演进项） |
| R5 | **property 分组整组级联不支持** | 低 | v1 仅父移动 + 提示；v1.1 循环 `setIssueProperty` 或后端级联 |
| R6 | **batch-update 不支持 move_intent**：后代 position 不精确 | 低 | v1 接受（子在新列按 position 自然排序或末尾）；Coding 先行验证 batch 副作用与单条一致（research §8.4） |
| R7 | 拖子跨列置 null 改变父子结构（与「跟随父列」直觉冲突） | 低 | 拖拽预览明确「将转为顶层任务」；文档 + Validation 重点验证该交互 |

---

## 6. 交付验收（给 Coding / Validation）

1. 每列内父子树形展示：父卡 Chevron 展开/折叠、子卡缩进（`depth * 18px`）、折叠后子隐藏。
2. 父卡进度环（`x/y done`）在树形下保留，`childProgressMap` 驱动。
3. 拖拽：拖父=整组（含全部后代）跨列/排序；拖叶子子=单独；拖子跨列=脱离父转顶层。
4. 子项状态与父不同列时跟随父列显示；父不可见时子按自身值落列。
5. `showSubIssues=false` 时仅根节点。
6. Table 视图行为不变（回归）。
7. 新增单测：`board-tree-model.test.ts`（buildChildrenMap/flatten/collectSubtreeIds）、`drag-utils.test.ts` 树形场景；组件测试参考 `swimlane-view.test.tsx` / `table-view-editing.test.tsx`。
