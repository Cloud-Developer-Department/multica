# Board 看板树状结构 — 代码审查报告（CLO-224）

**审查人**: 保罗（Review / sgd-paul）
**日期**: 2026-08-05
**审查对象**: `feature/board-tree` @ `23c6ed5c`（CLO-222）
**基线**: `Equipment_Department_Exploration` @ `67e58d0f`
**依据**: Code Diff（`23c6ed5c` vs `67e58d0f`）、设计契约 `02-design/board-tree-design.md`、独立重跑 typecheck/lint/单测

---

## 0. 审查范围

| 文件 | 行数 | 说明 |
|------|------|------|
| `components/board-tree-model.ts` | 82（新增） | 纯函数：buildChildrenMap / collectSubtreeIds / flattenBoardTree |
| `components/board-tree-model.test.ts` | 152（新增） | 纯函数单测 |
| `components/board-view.tsx` | 1197（+325） | 树形列构建、拖拽语义、惰性补拉、DragOverlay |
| `components/board-column.tsx` | 390（+46） | nodeInfo 透传、depth/hasChildren/collapsed 渲染 |
| `components/board-card.tsx` | 428（+60） | Chevron 折叠按钮、depth*18 缩进 |
| `utils/drag-utils.ts` | 341（+174） | buildBoardTreeColumns / 子树块锚点 / 批量同步 |
| `utils/drag-utils.test.ts` | 294（+168） | 树形列构建 + 子树块数学单测 |
| `core/issues/stores/view-store.ts` | +15 | boardCollapsedParents + toggle + persist + merge |
| 4 个 locale `issues.json` | +6 行/语言 | expand/collapse/subtree_count/detach_hint/subtree_sync_failed |

---

## 1. Review Summary

设计契约（D1–D6、R1–R7）被忠实实现，架构干净：前端组树、列镜像保持展平 id 数组（`useDragSettle` 零改动）、后端零变更、Table/List/Swimlane 零影响。代码可读、职责单一、安全面无新增风险。独立重跑 typecheck/lint/135 个相关单测全绿。

**但有一个必须处理的流程问题**：`feature/board-tree` 在 Validation（CLO-223）完成后被重写（`f9b602d6` → `23c6ed5c`，~1100 行差异），导致 CLO-223 测试报告所验证的代码与即将合入的代码不一致；重写还删除了组件级测试 `board-column-tree.test.tsx`。代码本身经我独立复验是健康的，但**合入前必须在最终 tip 上重跑 Validation**。

---

## 2. Good（值得保留）

1. **架构对齐设计**：前端组树复用平铺查询 + `childProgressMap` + `childrenByParentsOptions` 惰性补拉；列镜像仍为 `Record<groupId, string[]>`，`useDragSettle` 完全未改——树形逻辑集中在 `board-tree-model.ts`（纯函数）+ `drag-utils.ts`（树形列构建），分层清晰、可测。
2. **拖拽语义统一且正确**：D3「子树移动」一致规则——拖父=整组（`getSubtreeBlock` 取展平序列中深度 > active 的连续块），拖叶子=单独（块长 1）；`moveBlockWithin` 对「拖到自身后代」做 no-op（`block.includes(overId)`）防循环；D4 拖子跨列置 `parent_issue_id: null`，且仅在父可见时触发（`map.has(parent_issue_id)`），父不可见的子本就是 root，不会误清父子链。
3. **子跟随父列（D2）**：`buildBoardTreeColumns` 以 root 的分组值落列，子项随父 flatten 进同一列，子自身 status/assignee/property 不影响其列归属；父不可见时子按自身值成为 root——与设计一致。
4. **折叠状态隔离（D5）**：`boardCollapsedParents` 独立于 `tableCollapsedParents`，persist partialize + `mergeViewStatePersisted` 的 `Array.isArray` 兼容守卫与 table 同款，旧快照无此键 → `[]`。
5. **property 级联正确跳过（R5）**：`getSubtreeSyncUpdates` 对 property 列返回 null，仅 status/assignee 走 batch-update；父属性值走既有 `useSetIssueProperty` 单条路径。
6. **安全**：无 SQL/命令注入面（无原始 SQL、无 `dangerouslySetInnerHTML`）；`parent_issue_id: null` 经类型化 `UpdateIssueRequest` 透传；`descriptionPreview` 仅做正则文本裁剪后作为 React 文本节点渲染（无 XSS）；无密钥/Token 泄露。
7. **性能**：树构建 O(n) 全在 `useMemo`；列内 Virtuoso 虚拟化不变，`computeItemKey=issue.id` 稳定；拖拽期 `issueMapRef/nodeInfoRef/childrenMapRef` 冻结，props 引用稳定；`childrenByParentsOptions` 有 `enabled: parentIds.length > 0` 守卫，无空查询。
8. **锚点数学正确**：`getSubtreeMoveAnchors` 取子树块边界外的相邻 id（`before_id=ids[start-1]`、`after_id=ids[end]`），`computeBlockPosition` 在块首/块尾/中间三种位置与单卡 `computePosition` 同公式——与后端 move 端点契约一致。

---

## 3. Risks（问题 / 原因 / 影响）

### 🔴 B1 — 分支在 Validation 后被重写，测试报告不适用（流程 Blocker）
- **现象**：CLO-223 报告验证对象为 `f9b602d6`（代码提交 `2f2ac77f`），但 `feature/board-tree` 当前 tip 为 `23c6ed5c`（单一新 commit，时间戳晚于 Validation 4 分钟）。`git diff f9b602d6 23c6ed5c -- packages/` 显示 13 文件 ~1100 行差异。
- **原因**：Coding 在 Validation 通过后重写了实现（squash + 重构），未重新触发 Validation。
- **影响**：CLO-223 报告中的「39/39 board-tree、16 board-column 用例、build 绿」结论对应的是旧代码，不能作为合入依据。若 DevOps 直接基于当前 tip 提 PR，等于未经验证合入。
- **复验**：我已在 `23c6ed5c` 上独立重跑：`@multica/views` typecheck 绿、5 个改动文件 eslint 0 error/0 warning、board-tree-model+drag-utils 30/30、swimlane+table-view-editing+table-view-model 95/95、view-store 10/10 全绿。代码本身健康。
- **要求**：合入前由 Validation（CLO-223）在 `23c6ed5c` 上重跑一次完整验证（build + 全量单测 + 回归），更新测试报告。DevOps 的 PR 必须基于 `23c6ed5c`（或其后续 review 提交），不得基于 `f9b602d6`。

### 🟡 W2 — 组件级测试在重写中被删除（覆盖回退）
- **现象**：`2f2ac77f` 新增的 `board-column-tree.test.tsx`（208 行，16 用例，覆盖 BoardColumn×树形集成：折叠切换、depth 缩进、DragOverlay、follow-parent-column）在 `23c6ed5c` 中不存在。
- **原因**：重写时未保留该组件测试文件。
- **影响**：当前 Board 树形只有纯函数单测（`board-tree-model`）+ drag-utils 数学单测，**组件交互层零覆盖**。设计 §6.7 明确要求「组件测试参考 swimlane-view.test.tsx / table-view-editing.test.tsx」。拖拽两阶段写入、Chevron 交互、DragOverlay 徽章均无自动化回归保护。
- **建议**：合入前补一个 `board-column-tree.test.tsx`（或 `board-view-tree.test.tsx`）覆盖：折叠/展开、子跟随父列渲染、DragOverlay +N 徽章与 detach 提示。优先级 P1。

### 🟡 W3 — `hasChildren` 信号偏离设计 D6
- **现象**：`board-tree-model.ts:68-70`：
  ```ts
  const hasChildren =
    (childProgressMap.get(issue.id)?.total ?? 0) > 0 ||
    (childrenMap.get(issue.id)?.length ?? 0) > 0;
  ```
  设计 D6 规定 `hasChildren = childProgressMap.total > 0`（唯一权威信号），实现额外 OR 了 `childrenMap.length > 0`。
- **原因**：可能是为「childProgressMap 尚未加载但子已在场」加的兜底。
- **影响**：在 childProgressMap 报告 total=0 但存在陈旧已加载子的边界下会多显示一个 Chevron；偏离文档化契约，后续维护者会对「权威信号」产生歧义。
- **建议**：要么更新 D6 文档承认 OR 兜底并补一条单测，要么去掉 `childrenMap.length` 分支。优先级 P2。

### 🟢 S4 — `currentIssuePos` 命名误导（可读性）
- `board-view.tsx:687` `const currentIssuePos = map.get(activeId)` 实为 `Issue` 对象，下方读 `currentIssuePos.position`。与 609 行的 `currentIssue` 同作用域异义。建议改名 `activeIssueRecord` 并与 609 行统一。P2。

### 🟢 S5 — `boardCollapsedParents` 持久化无单测
- view-store 新增 toggle/persist/merge 守卫无对应单测（CLO-223 已提为非阻塞）。merge 守卫与已测的 `tableCollapsedParents` 同款，风险低。建议补一条 toggle + 旧快照合并测试。P2。

### 🟢 S6 — 两阶段子树同步为 fire-and-forget
- `board-view.tsx:678` `void surfaceActions?.batchUpdate(...).catch(...)` 未 await；父 `beginSettle()` 锁在父 move 的 onSettled 释放，可能在 batch 完成前释放。与设计 R4（非原子、失败 toast）一致，v1 可接受；batch 失败时子留在原列直到下次 refetch，toast 为唯一信号。v1.1 演进 cascade 端点时再收口。P2。

---

## 4. Suggestions（建议 / 优先级）

| # | 建议 | 优先级 |
|---|------|--------|
| 1 | 合入前在 `23c6ed5c` 重跑完整 Validation（build + 全量单测 + 回归），更新 CLO-223 报告 | P0 |
| 2 | 补 `board-column-tree.test.tsx` 组件级测试（折叠/展开、follow-parent 渲染、DragOverlay 徽章/detach） | P1 |
| 3 | 对齐 D6：更新文档或去掉 `childrenMap.length` 兜底分支，补单测 | P2 |
| 4 | `currentIssuePos` 改名 | P2 |
| 5 | `boardCollapsedParents` toggle + merge 单测 | P2 |
| 6 | 两阶段同步 await 收口（v1.1 cascade 端点配套） | P2 |

---

## 5. 验证复验记录（审查人独立执行）

| 命令 | 结果 |
|------|------|
| `pnpm --filter @multica/views typecheck` | ✅ 绿 |
| `eslint` 5 个改动文件 | ✅ 0 error / 0 warning |
| `vitest board-tree-model.test.ts drag-utils.test.ts` | ✅ 30/30 |
| `vitest swimlane + table-view-editing + table-view-model` | ✅ 95/95（回归） |
| `vitest surface-view-store + my-issues-view-store` | ✅ 10/10 |

> 未跑全量套件（时间约束）；上述已覆盖改动直接涉及的全部模块 + Table/Swimlane 回归面。全量复验交由 Validation 重跑（见 B1）。

---

## 6. Final Decision

**🟡 Approve with Suggestions** — 代码质量达标，架构与设计契约一致，安全/性能无阻塞问题。

**合入前置条件（必须满足）**：
1. **B1**：Validation 在最终 tip `23c6ed5c`（含本审查提交）上重跑完整验证通过；DevOps PR 基于该 tip。
2. **W2**：补回组件级测试（至少覆盖折叠/展开 + follow-parent + DragOverlay）。

满足上述两项后即可推进 Stage 6（文档）。若团队选择先合入再补测试，需由 Orchestrator 确认接受该风险——不建议。

**不返回 Coding**：代码本身无需返工，B1/W2 属验证与测试补充，分别交由 Validation（CLO-223 重跑）与 Coding（补组件测试，可作为 CLO-222 的小补丁或新建子任务）。
