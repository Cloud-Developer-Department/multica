# Board Kanban Hierarchy — Architecture Design

**Issue**: CLO-196 / CLO-200  
**Date**: 2026-08-05  
**Author**: Architect (阿基米德)  
**Status**: Draft → Coding

---

## 1. 需求理解

为 Board 看板视图新增父子 issue 层级展示与拖拽联动能力。当前 Board 将父子 issue 全部平铺为独立卡片，需改为：

1. 层级渲染：子卡片缩进显示在父卡片下方，带折叠/展开切换
2. 子进度指示：父卡片显示子 issue 完成进度（done/total）
3. 拖父联动子项：拖拽父卡片时，子卡片跟随（同列排序 / 跨列换状态）
4. 子项跟随父列：父卡片拖到新列时，子卡片也变更状态到新列

---

## 2. 架构设计

### 2.1 模块分层

```
┌─────────────────────────────────────────────────────┐
│  board-view.tsx          (DndContext + 列构建)        │
│  ├── board-column.tsx    (Virtuoso + SortableContext) │
│  │   └── board-card.tsx  (DraggableBoardCard + 层级)  │
│  └── drag-utils.ts        (getMoveUpdates 扩展)       │
├─────────────────────────────────────────────────────┤
│  view-store.ts            (boardCollapsedParents)    │
│  issue-mutations.ts       (move + batch-update)      │
├─────────────────────────────────────────────────────┤
│  issue_list.go            (direct_child_count 扩展)   │
│  issue.go                 (batch-update 复用)         │
│  issue_move.go            (move 复用)                 │
└─────────────────────────────────────────────────────┘
```

### 2.2 调用关系

- **数据查询**：Board 仍使用现有 flat issue list 获取卡片数据，客户端侧按 `parent_issue_id` 构建树结构。后端 issue list API 新增可选 `direct_child_count` 字段（`?hierarchy=true` 时返回）。
- **子进度**：复用现有 `GET /api/issues/child-progress`（已存在，返回 `{parent_issue_id, total, done}`）。
- **同列拖拽排序**：仅移动父卡片（`POST /api/issues/:id/move`），子卡片按 `parent_issue_id` 原地跟随渲染。
- **跨列拖拽**：两阶段 —— `POST /api/issues/:id/move`（父卡片，含 status 变更 + 锚点定位），成功后 `POST /api/issues/batch-update`（所有直接子卡片，仅更新 status，不传 position）。
- **折叠状态**：view-store 新增 `boardCollapsedParents: string[]`，折叠时前端过滤不渲染对应子卡片。

---

## 3. 数据模型

### 3.1 数据库 (无变更)

现有 `issue` 表已包含层级所需字段：

| 字段 | 类型 | 语义 |
|------|------|------|
| `id` | UUID PK | issue 标识 |
| `parent_issue_id` | UUID? (FK→issue) | 父 issue，NULL=根节点 |
| `status` | TEXT | backlog/todo/in_progress/in_review/done/blocked/cancelled |
| `position` | FLOAT | 手动排序位置（支持中点插入） |
| `assignee_type` | TEXT? | member/agent |
| `assignee_id` | UUID? | 分配人 |

### 3.2 View-Store 新增状态

```typescript
// packages/core/issues/stores/view-store.ts

interface ViewState {
  // 现有
  tableCollapsedParents: string[];
  tableHierarchy: boolean;

  // 新增
  boardCollapsedParents: string[];   // Board 视图中已折叠的父 issue ID 列表
  boardHierarchy: boolean;           // Board 层级展示开关（默认 true）
}

// 新增方法
toggleBoardParentCollapsed: (issueId: string) => void;
// 实现：与 toggleTableParentCollapsed 对称 ——
//   issueId ∈ boardCollapsedParents → 移除
//   issueId ∉ boardCollapsedParents → 追加
```

### 3.3 Issue 响应增强

```typescript
// packages/core/types/issue.ts — IssueResponse

interface IssueResponse {
  // ... 现有字段 ...
  direct_child_count?: number;  // 新增：直接子 issue 数量
  // 仅当查询参数 ?hierarchy=true 时返回
}
```

**空值语义**：
- `direct_child_count` 为 `undefined`：未请求层级信息（默认）
- `direct_child_count` 为 `0`：该 issue 无子节点
- `direct_child_count` > 0：有子节点，渲染折叠切换 + 计数徽章

---

## 4. API 契约

### 4.1 现有端点（复用，不变更契约）

#### GET /api/issues/child-progress
```
Request:  GET /api/issues/child-progress?workspace_id=<uuid>
Response: [{ parent_issue_id: string, total: number, done: number }, ...]
```
用于父卡片进度指示器（`done/total`）。

#### POST /api/issues/:id/move
```
Request:  { status?, assignee_type?, assignee_id?, parent_issue_id?, project_id?,
            before_id: string|null, after_id: string|null }
Response: IssueResponse
```
用于父卡片跨列移动（含锚点位置解析）。

#### POST /api/issues/batch-update
```
Request:  { issue_ids: string[], updates: { status?, ... } }
Response: { updated: number }
```
用于子卡片批量状态同步。

### 4.2 新增/变更端点

#### Issue List API — 新增 `direct_child_count` 字段

路由：`GET /api/issues/list`（或 Board 使用的 issue 查询端点）

变更类型：**响应字段新增**（向后兼容）

```
Query:    ?workspace_id=<uuid>&...&hierarchy=true
Response: [{ ...issueFields..., direct_child_count?: number }, ...]
```

后端实现（伪代码）：

```go
// server/internal/handler/issue_list.go

type listIssuesRequest struct {
    // ... 现有字段 ...
    Hierarchy bool `query:"hierarchy"` // 新增
}

// 在 issue 列表查询的 SELECT 子句中：
if request.Hierarchy {
    selectCols = append(selectCols, 
        "(SELECT COUNT(*)::bigint FROM issue child WHERE child.parent_issue_id = i.id) AS direct_child_count")
}
```

**注意**：`direct_child_count` 统计的是该父节点下**所有 workspace 内**的直接子 issue，不按 Board 当前筛选条件过滤。原因：Board 的筛选逻辑复杂（状态、分配人、属性等），在子查询中同步过滤会产生性能问题和语义不一致。如需筛选感知的子计数，应在后续迭代中通过专用查询参数引入。

### 4.3 拖拽调用序列

#### 场景 A：同列内排序（父卡片在同列内上下移动）

```
前端：calc position delta = newPos - oldPos
     → 仅发送父卡片 move
  POST /api/issues/:parentId/move
      { before_id: "aboveId", after_id: "belowId" }
      // 不传 status（不换列）
     → 子卡片位置不变，渲染时自动跟随父卡片
```

**无需 batch-update**。子卡片通过 `parent_issue_id` 关联，在 Board 列渲染时排在父卡片之后，保持相对顺序。不改变子卡片底层 position 字段（降低写放大）。

#### 场景 B：跨列移动（父卡片拖到不同状态列）

```
前端：
  1. POST /api/issues/:parentId/move
       { status: "done", before_id: "...", after_id: "..." }
       → 父卡片变更状态 + 新位置

  2. POST /api/issues/batch-update
       { issue_ids: [child1Id, child2Id, ...], updates: { status: "done" } }
       → 所有直接子卡片变更状态
       → 不传 position（让子卡片在新列中自然排在末尾）
```

**此设计仅处理直接子 issue（一级），不递归到孙子节点。** 递归级联可在 v2 通过 `?recursive=true` 参数引入。

**失败处理**：
- 步骤 1 失败 → 拖拽回弹，无副作用
- 步骤 2 失败 → 父卡片已移动，子卡片留在旧列。前端可捕获错误并提示用户，或静默降级（子卡片下次刷新时仍在旧列，用户可手动再次拖拽）

### 4.4 错误码

| 场景 | HTTP 状态 | 错误码 | 说明 |
|------|-----------|--------|------|
| 子卡片 batch-update 中检测到循环引用 | 400 | `CYCLE_DETECTED` | 现有逻辑（batch-update 已有 cycle detection） |
| 父卡片 move 的目标列不存在 | 400 | `INVALID_STATUS` | 现有逻辑 |
| 拖拽自身为子节点的情况 | 400 | `SELF_PARENT` | 前端已阻止（guard），后端兜底 |
| batch-update 部分成功 | 200 | — | `updated` 字段 < `len(issue_ids)`，前端可对比 |

---

## 5. 前端改造方案

### 5.1 board-view.tsx — 列构建 + 层级数据准备

改造点：

1. **树结构构建**（新增函数 `buildBoardTree`）：
   ```
   输入：flatIssues: IssueResponse[]
   输出：BoardTreeNode[]  // 每个根节点中包含 children: BoardTreeNode[]
   
   Algorithm:
     1. 按 parent_issue_id 分组
     2. 根节点：parent_issue_id === null
     3. 子节点递归挂载到对应父节点下
     4. 每个节点保留原始 issue 数据 + children 数组
   ```

2. **列分配**：每个 BoardTreeNode 按其自身的 `status`（或 `assignee` / 分组属性）落列。子节点 **不** 因为父节点在列 A 就强制入列 A —— 子节点按自身属性入列，仅在渲染时通过缩进和折叠体现层级关系。

   注：当「拖父联动子项」生效后（跨列移动），子节点会因 batch-update 而更新 status，自然进入与父节点相同的列。

3. **折叠状态传递**：将 `boardCollapsedParents` 传给 `board-column.tsx`。

4. **DndContext 改造**：`handleDragEnd` 中检测被拖拽卡片是否有子节点。如有：
   - 同列排序 → 标准 move
   - 跨列移动 → move + batch-update（场景 B）

### 5.2 board-column.tsx — 虚拟列内层级渲染

改造点：

1. **接收层级数据**：不再接收 `IssueResponse[]`，改为 `BoardTreeNode[]`（含 children）。
2. **渲染展开**：将 BoardTreeNode 展平为渲染列表（flatten），类似 Table 的 `appendBranch` 逻辑：
   ```
   flatten(nodes, depth=0):
     for node in nodes:
       push { issue: node.issue, depth, hasChildren: node.children.length > 0, collapsed }
       if !collapsed:
         flatten(node.children, depth + 1)
   ```
3. **缩进样式**：`depth > 0` 的卡片添加 `paddingLeft: depth * 20px`（或使用固定缩进量）。
4. **折叠过滤**：`collapsed === true` 时跳过 push children 行。

### 5.3 board-card.tsx — 卡片层级 UI

改造点：

1. **新增 `DraggableBoardCard` props**：
   ```typescript
   interface BoardCardProps {
     issue: IssueResponse;
     depth: number;           // 新增
     hasChildren: boolean;    // 新增
     collapsed: boolean;      // 新增
     childProgress?: { total: number; done: number };  // 新增
     onToggleCollapse: (issueId: string) => void;       // 新增
   }
   ```

2. **折叠切换按钮**：在卡片标题左侧添加 ChevronRight/ChevronDown 图标（仅当 `hasChildren` 时显示），点击调用 `onToggleCollapse`。

3. **子计数徽章**：`{childProgress.done}/{childProgress.total}` 进度文字，显示在卡片标题右侧或副标题位置。

4. **拖拽预览**（DragOverlay 改造）：当被拖拽卡片 `hasChildren` 时，在 DragOverlay 中显示 "+N children" 提示，让用户感知联动效果。

5. **缩进渲染**：`depth > 0` 的卡片添加左缩进，视觉上与 Table 层级一致。

### 5.4 drag-utils.ts — 拖拽数据扩展

改造点：

```typescript
// 新增类型
interface DragPayload {
  issue: IssueResponse;
  hasChildren: boolean;      // 新增
  directChildIds: string[];   // 新增：用于 batch-update
}

// getMoveUpdates 扩展
function getMoveUpdates(...): DragMoveTargetUpdates & { childIds?: string[] }
```

### 5.5 进度指示器

父卡片的 `childProgressMap` 从 `GET /api/issues/child-progress` 获取，在 `board-view.tsx` 中 fetch 后通过 props 下传。

---

## 6. 后端改造方案

### 6.1 issue list API — 新增 `hierarchy` 查询参数

**文件**：`server/internal/handler/issue_list.go`（或 Board 使用的列表接口）

**变更**：
1. 请求结构体新增 `Hierarchy bool` 可选字段
2. 在 SQL SELECT 子句中新增 `direct_child_count` 子查询（当 `Hierarchy == true`）

**SQL 变更**（PostgreSQL）：
```sql
-- 在现有 issue list SELECT 中追加（条件编译）：
(SELECT COUNT(*)::bigint FROM issue child WHERE child.parent_issue_id = i.id) AS direct_child_count
```

**索引**：`idx_issue_parent`（已存在于 `(parent_issue_id)`），无需新增。

### 6.2 无需变更的端点

| 端点 | 说明 |
|------|------|
| `POST /:id/move` | 父卡片移动（含 status 变更 + 锚点定位），已支持 |
| `POST /batch-update` | 子卡片批量状态更新，已支持 `status`、`position`、`parent_issue_id` 等字段 |
| `GET /child-progress` | 子进度聚合查询，已支持 |
| `POST /table/rows` | Table 层级查询，**不需要**为 Board 修改 branchPredicate，Board 与 Table 使用不同数据路径 |

---

## 7. 两个待决策点 — 方案确定

### 决策 1：子项跟随父列（跨组）

**背景**：Table 视图有 `TestIssueTableHierarchyDoesNotCrossGroups` 测试，明确「跨组不跨列，子项在自己组成为 root」。该约束保护的是 Table 的**多维度复合分组**（status + assignee + ...），而 Board 的分组是**单一维度**（按 status 分列），语义不同。

**方案 A（Table 兼容）**：Board 也遵循「跨组不跨列」，父卡片拖到新列后子卡片留在旧列并成为 root。

**方案 B（Board 独立）**：Board 启用「子项跟随父列」，父卡片跨列移动时子卡片也更新 status。Table 的「跨组不跨列」不变。

**决策：采纳方案 B。** 理由：
- Board 的列 ≈ 状态流转（Todo → In Progress → Done），Kanban 用户的直觉预期是「把父任务做完 = 子任务也做完」
- Table 的跨组限制针对的是任意复合分组，语义不适用 Board
- 实现层面：Board 和 Table 使用不同数据路径 —— Board 用 flat issue list + client-side tree，Table 用 `/table/rows` + server-side branchPredicate。两者不共享分组逻辑，不会互相污染
- 不改动 `/table/rows` 的 branchPredicate，Table 保持现状

### 决策 2：拖父联动子项写路径

**方案 A（单一 batch-update）**：前端计算父卡片新位置，将父 + 所有子卡片打包为一次 `POST /batch-update`。

**方案 B（move + batch-update 两阶段）**：先用 `POST /:id/move` 处理父卡片（含锚点位置解析），再用 `POST /batch-update` 批量更新子卡片 status。

**方案 C（新增 cascade 端点）**：新增 `POST /:id/cascade-move`，后端在一次事务中完成父移动 + 子级联。

**决策：采纳方案 B（两阶段），后续演进到方案 C。** 理由：
- 方案 A 丢失了锚点位置解析能力（`before_id`/`after_id` → position），前端需要自己计算位置，容易产生冲突
- 方案 B 复用现有端点，开发量最小，且失败时优雅降级（子卡片留在旧列）
- 方案 C 是最终理想态（原子事务 + 位置一致性），但在 v1 中为两阶段实现可快速交付；如果 QA 发现两阶段的一致性问题，可在 v1.1 升级到方案 C

---

## 8. 影响范围与迁移

### 8.1 涉及文件

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `packages/views/issues/components/board-view.tsx` | 修改 | 树结构构建、DndContext 级联逻辑 |
| `packages/views/issues/components/board-column.tsx` | 修改 | 层级渲染、折叠过滤 |
| `packages/views/issues/components/board-card.tsx` | 修改 | 折叠按钮、子计数、缩进、拖拽预览 |
| `packages/views/issues/utils/drag-utils.ts` | 修改 | `getMoveUpdates` 扩展 childIds |
| `packages/core/issues/stores/view-store.ts` | 修改 | `boardCollapsedParents` + `boardHierarchy` |
| `packages/core/types/issue.ts` | 修改 | `IssueResponse` 新增 `direct_child_count?` |
| `server/internal/handler/issue_list.go` | 修改 | `hierarchy=true` 时附加 `direct_child_count` |
| `packages/core/api/client.ts` | 无变更 | batch-update client 已存在 |

### 8.2 迁移风险

| 风险 | 等级 | 降级 |
|------|------|------|
| issue list API 新增子查询影响列表性能 | 低 | 仅 `?hierarchy=true` 时启用；`idx_issue_parent` 已存在 |
| batch-update 部分失败导致数据不一致 | 中 | 两阶段优雅降级；v1.1 升级到 cascade 端点 |
| `boardCollapsedParents` 状态在视图切换时遗留 | 低 | 独立 key（非共享 `tableCollapsedParents`），视图切换时各视图管理自己的状态 |
| 1000+ 卡片树结构渲染性能 | 低 | Virtuoso 虚拟列表已在 Board 列中使用，仅渲染可见卡片 |

### 8.3 无需迁移

- 无数据库 schema 变更（`parent_issue_id`、`position`、`status` 均已存在）
- 无外键变更
- 无索引变更
- 无 API 契约破坏性变更（新增字段 `direct_child_count` 为 optional，向前兼容）

---

## 9. 风险与建议

### 风险清单

1. **batch-update 非原子**：两阶段 mutation（move + batch-update）不在同一事务中。如果 batch-update 失败，父卡片已在新列但子卡片仍在旧列。
   - 降级：前端捕获 batch-update 错误，toast 提示「子任务状态更新失败，请手动拖拽或刷新后重试」
   - 长期：v1.1 引入 `POST /:id/cascade-move` 事务端点

2. **深层递归级联**：当前设计仅处理直接子 issue（一级）。如果父卡片有 3 级深度（子 → 孙 → 曾孙），只有直接子会被更新。
   - 降级：v1 文档说明「仅迁移一级子任务」，v2 支持 `?recursive=true`

3. **拖拽性能**：当父卡片有 50+ 子卡片时，batch-update 请求体较大。
   - 降级：限制 batch-update 的 `issue_ids` 为 100 条（后端已有或需新增校验）

4. **Board 与 Table 折叠状态隔离**：`boardCollapsedParents` 和 `tableCollapsedParents` 是两个独立 key，但视图切换时不要交叉污染。

### 建议

1. **先实现「层级渲染 + 折叠展开」**，验证无误后再接入「拖拽联动」。这两块独立性强，可分阶段交付。
2. **新增 `direct_child_count` 的 issue list 改动最小**，如果后端改动延迟，前端可先用 `child-progress` API 的 `total` 字段作为近似计数（差异：`child-progress` 返回所有 workspace 内子节点，`direct_child_count` 可后续精细化）。
3. **Board 缩进层级最大支持 5 级**（与 Table 一致），防止超深嵌套导致渲染溢出。

---

## 10. 测试要点（供 Validation 角色参考）

- 父卡片在同列内拖拽排序，子卡片视觉跟随
- 父卡片跨列拖拽，父 + 所有直接子卡片进入目标列
- 子卡片不跟随父卡片（仅父卡片移动时联动子卡片，反之不联动）
- 折叠父卡片后，子卡片隐藏且不被 SortableContext 计算
- 展开折叠后，子卡片重新渲染
- batch-update 失败时 toast 提示
- 无子卡片的卡片不显示折叠按钮
- `direct_child_count` = 0 时不显示子计数徽章
- 拖拽 preview 中显示 "+N children"（N > 0 时）
- 禁用将卡片拖拽到其子卡片列为父（循环引用 guard）
