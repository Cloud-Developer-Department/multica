# Board 看板父子树状展示（CLO-196）交付件

**日期**: 2026-08-05
**特性**: Board 看板支持父子 issue 树状展示

## 交付物索引

| 子目录 | 阶段 | 说明 |
|--------|------|------|
| `01-research/` | Research | 现状调研报告（Board/Table hierarchy/拖拽/数据/部署/分支） |
| `02-design/` | Architect | 架构设计方案（数据模型/API 契约/前端改造/拖拽决策） |
| `03-backend/` | Backend-Coding | 后端改造（`?hierarchy=true` + `direct_child_count`，零迁移零破坏） |

## 关键结论

- 后端 `/api/issues/table/rows` 已完整支持 `hierarchy`（父子树形分页 + `direct_child_count`），Board 目前仅以 `hierarchy.enabled=false` 平铺消费该接口。
- Table 视图的展开/折叠是纯前端 store 状态（`tableCollapsedParents` / `tableHierarchy`），数据层 `direct_child_count`、`childProgressMap` 已就绪，可直接复用。
- Board 拖拽基于 dnd-kit，已支持跨列移动 + 排序，但**无任何父子联动逻辑**；拖父卡片不会带子项，子项作为独立卡片平铺。
- 子项跟随父列（跨组不跨列）语义在后端尚未实现：`TestIssueTableHierarchyDoesNotCrossGroups` 明确子项跨组时不跟随父项，而是成为自己组的 root。
- 部署：`make dev` / `make start`（Postgres + Go 后端 + Next.js 前端），无新增中间件。
- 基线分支 `Equipment_Department_Exploration`（origin 6d400f2d），仓库**无** `feature/board-tree` 分支。

## 分支

- 基线：`origin/Equipment_Department_Exploration` @ `6d400f2d`
- 唯一特性分支：`feature/board-tree`（从基线创建，合并 01-research + 02-design 交付件，Backend/Frontend Coding 共用）
- 各阶段交付件来源分支：Research `agent/sgd-magic-research/1808b523`、Architect `agent/sgd-architect-architecture/f4a706db`
