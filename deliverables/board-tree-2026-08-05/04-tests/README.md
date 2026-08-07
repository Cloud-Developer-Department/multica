# Board 看板树状结构 — 测试报告 v2（CLO-223 最终 tip 重跑）

**Issue**: CLO-223（父 CLO-218）
**日期**: 2026-08-05（首次）/ 2026-08-06（最终 tip 重跑，本版）
**角色**: Validation（杜坎）
**被测**: `feature/board-tree` @ **`a72746d7`**（代码提交 `23c6ed5c`，审查报告提交 `a72746d7`，二者代码一致）
**基线**: `main`/`Equipment_Department_Exploration` @ `67e58d0f`（merge-base）

> 本版为 Review（CLO-224）B1 流程 blocker 要求的最终 tip 重跑。首次验证对象 `f9b602d6`/`2f2ac77f` 已失效（分支被重写为 `23c6ed5c`）。

---

## 0. 结论摘要

| 维度 | 结果 |
|------|------|
| **Build** | ✅ 成功（`@multica/web`，4m54s） |
| **Compile（typecheck）** | ✅ 成功（core/ui/views force 全量） |
| **Unit Test** | ✅ board-tree 专项 30/30；core 1066；views 3074 通过 / 4 存量失败 |
| **Regression** | ✅ Table hierarchy / swimlane / drag-settle / view-store 全绿 |
| **Lint** | ✅ board-tree 涉及文件 0 error / 0 warning |
| **风险等级** | 🟢（4 个存量失败为基线上游 share-link 引入，与本次改动无关） |
| **建议** | ✅ 可以交付 Stage 6/7（Review B1 已闭环）；W2 组件测试缺口需 Coding 补回 |

---

## 1. Build（构建）

| 任务 | 命令 | 结果 |
|------|------|------|
| Typecheck（force） | `turbo typecheck --filter=@multica/views --filter=@multica/core --force` | ✅ core/ui/views 全绿 |
| Web 应用构建 | `turbo build --filter=@multica/web --force` | ✅ 成功（4m54s） |

> packages 无独立 build 脚本，board-tree 代码在 `@multica/web` 中实际编译；Web build 通过即为有效构建验证。

---

## 2. Unit Test（单元测试）

### 2.1 board-tree 专项（最终 tip）

| 文件 | 用例数 | 覆盖点 |
|------|-------|--------|
| `board-tree-model.test.ts` | 14 | `buildChildrenMap`（position+created_at 排序）、`collectSubtreeIds`（DFS 全部后代）、`flattenBoardTree`（深度 / hasChildren / collapsed / 折叠隐藏子树 / nodeInfo 填充） |
| `drag-utils.test.ts` | 16 | `buildBoardTreeColumns`（根按自身分组值落列 / **子跟随父列** / 折叠父子出列 / 父不可见子提升 / 孙 DFS）、`getSubtreeBlock`（连续子树块）、`moveBlockInto` / `moveBlockWithin`（拖到自身后代 no-op）、`getSubtreeMoveAnchors`（块边界外锚点）、`computeBlockPosition`（块首/块尾/中间）、`getSubtreeSyncUpdates`（status/assignee 同步、property 列 null） |
| **小计** | **30** | ✅ 全部通过 |

**需求覆盖映射**：

- **展开/折叠**：`flattenBoardTree` 折叠隐藏子树 + `hasChildren && collapsedSet` 状态 ✅
- **拖拽语义（父拖整组/子拖单个）**：`collectSubtreeIds`（父=全部后代）+ `getSubtreeBlock`（展平块）+ `moveBlockWithin`（整块重排，防自身后代循环）✅
- **子 issue 跟随父列**：`buildBoardTreeColumns` 子列由父卡决定（子自身 status 异于父仍在父列）✅
- **进度环显示**：父卡 `childProgress` 沿用存量 `BoardCardContent` 渲染路径（未改动）✅

### 2.2 全量包测试

| 包 | 文件 | 通过 | 失败 |
|----|------|------|------|
| `@multica/core` | 101 | 1066 | 0 |
| `@multica/views` | 264 | 3074 | 4 |

### 2.3 4 个失败用例分析（存量，与本次改动无关）

全部位于 `packages/views/locales/parity.test.ts`：

| 用例 | 命名空间 | 缺失语言 |
|------|---------|---------|
| settings: ko covers every EN key | settings | ko |
| settings: ja covers every EN key | settings | ja |
| members: ko covers every EN key | members | ko |
| members: ja covers every EN key | members | ja |

**根因与判定（复现步骤）**：
1. 基线 `/tmp/multica-main-check`（`67e58d0f`，与 feature merge-base 完全一致）运行同一 `parity.test.ts` → **同样 4 个用例失败**（缺失 key 列表完全一致）。
2. 本特性提交只改动了 4 个 locale 的 `issues.json`（board 新增 key 在 en/ja/ko/zh-Hans 四语齐全；issues 命名空间 parity 全部通过）。
3. 根因是上游 `67e58d0f`（share-link invite 系统）给 en/zh-Hans 增加 settings/members key 未同步 ja/ko。

**结论**：4 个失败为基线存量欠债，非 CLO-222 引入，不阻塞交付。

---

## 3. Regression（回归）

### 3.1 关键交互回归（与 Table 视图 hierarchy 一致性）

| 测试 | 用例数 | 结果 |
|------|-------|------|
| `table-view-model.test.ts` / `table-view-editing.test.tsx` / `table-group-row.test.tsx` / `table-inline-title.test.tsx` / `table-issue-search.test.tsx` / `table-column-picker.test.tsx` | — | ✅ 全通过 |

### 3.2 共享拖拽原语 / 周边视图 / 状态管理

| 测试 | 用例数 | 结果 |
|------|-------|------|
| `swimlane-view.test.tsx` | — | ✅ |
| `use-drag-settle.test.tsx` | — | ✅ |
| `surface-view-store.test.tsx` / `my-issues-view-store.test.ts` | 10 | ✅ |
| **Regression 小计** | **87** | ✅ |

- `list-view` / `swimlane-view` 仍使用未改动的 `buildColumns`，与 board 的 `buildBoardTreeColumns` 互不影响。
- 后端零改动（`git diff 67e58d0f..tip -- server/` 为空）。

### 3.3 拖拽落库语义验证（代码走查级）

- move 端点白名单含 `parent_issue_id`（`server/internal/handler/issue_move.go:21`）；`UpdateIssue` 对显式 null 正确处理清父 + 同工作区校验 + 环检测（`issue.go`）。
- **D4 修复确认**：`board-view.tsx` 的 `isDetach` 增加 `map.has(currentIssue.parent_issue_id)` 条件（第 664-666 / 698-701 行）——父不可见时（已渲染为 root 的子）拖拽不会误清父子链，修正了首次实现的边界缺陷。
- **同列排序修复确认**：`sortBy === "position"` 同列重排只写 `position`，绝不写 group 字段（第 711-714 行）——避免把跟随父列的子卡 group 值改掉，与 follow-parent 语义一致。
- `childrenByParentsOptions` 在 `parentIds` 为空时 `enabled:false`，无空查询。
- `boardCollapsedParents` 持久化遵循 `tableCollapsedParents` 同款模式（partialize + merge 数组守卫）。

---

## 4. Lint / 静态分析

- board-tree 涉及文件（`board-tree-model.ts`、`board-tree-model.test.ts`、`board-column.tsx`、`board-card.tsx`、`board-view.tsx`、`drag-utils.ts`、`drag-utils.test.ts`）逐文件 eslint：**0 error / 0 warning**。
- `@multica/views` 全量 lint 报 4 个 error 均在 `settings/components/members-tab.tsx`（i18next/no-literal-string，share-link 存量代码），基线已复现。

---

## 5. Review 意见复核（CLO-224）

| # | 意见 | 复核结果 |
|---|------|---------|
| B1 | 最终 tip 重跑验证 | ✅ 本版完成（见 §1-4） |
| W2 | 组件级测试 `board-column-tree.test.tsx` 在重写中被删除 | ⚠️ **仍缺失**（最终 tip 无该文件）。代码层已由 drag-utils/board-tree-model 纯函数单测覆盖核心逻辑，但折叠/展开渲染、DragOverlay 徽章、Chevron 交互无自动化回归保护。**需 Coding 补回**（Review 建议 P1，已转交） |
| W3 | `hasChildren` 信号 = `childProgressMap.total>0 \|\| childrenMap.length>0`，偏离设计 D6 | ✅ 已在 `board-tree-model.ts:68-70` 确认实现如此。行为合理（子已加载但进度未加载时仍显示 chevron），建议 Review/文档采纳该 OR 语义 |
| S4 | `currentIssuePos` 命名误导 | ✅ 已确认存在（`board-view.tsx:687`），为可读性建议，非功能问题 |
| S5 | `boardCollapsedParents` 无单测 | ⚠️ 仍无专门单测（与 `tableCollapsedParents` 同款，风险低） |
| S6 | 两阶段同步 fire-and-forget | ✅ 已确认 `void batchUpdate(...).catch(toast)`，与设计 R4 一致 |

---

## 6. 风险与建议

- **风险等级：🟢 低**。
- 无 Build/Compile/Test 阻塞问题；4 个 parity 失败与 4 个 lint error 均为基线存量。
- **必须满足（B1 已闭环）**：DevOps（CLO-226）PR 必须基于 `a72746d7`（含代码 `23c6ed5c`），不得基于旧的 `f9b602d6`。
- **建议**：
  - 可以推进 Stage 6（文档 CLO-225）/ Stage 7（发布 CLO-226）。
  - **W2 建议合入前补回组件测试**（折叠/展开 + follow-parent + DragOverlay 徽章/detach），由 Coding 以 CLO-222 小补丁完成；若团队接受风险先合入再补，需 Orchestrator 确认（Review 已注明不建议）。

---

## 7. 复现命令

```bash
# 环境（最终 tip a72746d7）
git checkout a72746d7 && pnpm install

# Typecheck（force 全量）
pnpm turbo typecheck --filter=@multica/views --filter=@multica/core --force

# 专项单测
pnpm -C packages/views vitest run issues/components/board-tree-model.test.ts \
  issues/utils/drag-utils.test.ts

# 全量（parity 4 个存量失败预期）
pnpm -C packages/core test
pnpm -C packages/views test

# 构建
pnpm turbo build --filter=@multica/web --force

# 存量失败基线复现（67e58d0f）
pnpm -C packages/views vitest run locales/parity.test.ts
```
