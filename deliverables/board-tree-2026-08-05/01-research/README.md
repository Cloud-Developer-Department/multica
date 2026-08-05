# 01-Research 现状调研交付（CLO-220）

**日期**: 2026-08-05
**角色**: Research（魔术师）
**Issue**: CLO-220 【调研】看板视图现状与 Table 视图 hierarchy 实现分析
**父 Issue**: CLO-218 看板视图支持父子 issue 树状结构（展开/折叠 + 拖拽语义）

## 交付物清单

| 文件 | 说明 |
|------|------|
| `README.md` | 本文件（交付清单 + 关键结论） |
| `board-tree-research-report.md` | 调研报告正文（Board 现状 / Table hierarchy 复用分析 / 拖拽 / 数据接口 / 风险 / 建议） |

## 关键结论（速览）

1. **Board 视图**：`board-view.tsx`（列构建 + dnd 逻辑）、`board-column.tsx`（列/Virtuoso 虚拟列表）、`board-card.tsx`（卡片 + useSortable）。当前父子 issue 全部平铺为独立卡片，按各自 status/assignee/property 落入列；"平铺"本质是**渲染顺序/嵌套缺失**而非数据结构问题（子项已在数据里）。
2. **Table hierarchy 可整体复用**：后端 `/api/issues/table/rows` 已支持 `hierarchy.enabled` + `parent_id` 惰性拉取子树 + 每父 `direct_child_count`；前端 `view-store` 有 `tableCollapsedParents`/`tableHierarchy` 持久化折叠状态；`table-view.tsx` 的 DFS `appendBranch` + depth 缩进 + Chevron 折叠是现成参考。
3. **childProgressMap**：来自 `GET /api/issues/child-progress`（工作区全量 `Map<parent,{done,total}>`），Board/Table/List/Swimlane 共用，父卡进度环已存在（`board-card.tsx`）。
4. **拖拽**：dnd-kit + `useDragSettle`（Board/List 共享）。**无任何父子联动**——拖父不会带子项；拖父联动需用 `POST /api/issues/batch-update` 或前端遍历子树逐条 `moveIssue`。
5. **最大风险/待决策**：需求 4「子项跟随父列」与后端现有「跨组不跨列」语义冲突（`TestIssueTableHierarchyDoesNotCrossGroups` 固化），需 Architect 定方案（组内跟随 / 参数化新增语义）。
6. **相关资产**：本地 `feature/board-tree` 分支含上一轮 CLO-196/198/200 交付件（含后端 `GET /api/issues?hierarchy=true` 实现，提交 `f8d9eef2`），本轮可参考沿用。

## 给 Architect 的核心提示

- Board 树形最省力路径：复用 Table 的「服务端 parent_id 分支 + direct_child_count + collapsed store」机制，而非前端自建树。
- 拖拽语义（整组 vs 单独）、跟随父列口径（方案 A/B）、board/table 语义是否分叉——必须由 Architect 先裁定。
- `batch-update` 是否支持 `move_intent` 锚点与单条一致的副作用，需 Coding 验证。

## 分支

- 基线：`main` @ `67e58d0f`（2026-08-05）
- 本调研基于 `multica/` 本地 checkout，交付件写入本目录。
