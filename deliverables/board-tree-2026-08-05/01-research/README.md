# 01-Research 现状调研交付（CLO-200）

**日期**: 2026-08-05
**角色**: Research（魔术师）
**Issue**: CLO-200 【调研】Board 看板父子树现状调研
**父 Issue**: CLO-196 Board 看板支持父子 issue 树状展示

## 交付物清单

| 文件 | 说明 |
|------|------|
| `README.md` | 本文件（交付清单 + 关键结论） |
| `board-tree-research-report.md` | 调研报告正文（Board 实现 / Table hierarchy / 拖拽 / 数据接口 / 部署 / 分支） |

## 关键结论（速览）

1. **Board 视图**：`packages/views/issues/components/board-view.tsx`（列构建 + dnd 逻辑）、`board-column.tsx`（列/Virtuoso 虚拟列表）、`board-card.tsx`（卡片 + useSortable）。当前父子 issue 全部平铺为独立卡片，按各自 status/assignee/property 落入列。
2. **Table hierarchy**：`packages/views/issues/components/table-view.tsx` 用「服务端每父节点一个分支」的方式递归拉取子树（`/table/rows` + `parent_id` + `hierarchy.enabled=true`），折叠状态在 `view-store` 的 `tableCollapsedParents`，数据字段 `direct_child_count` 由后端 `/table/rows` 返回。`childProgressMap` 来自 `GET /api/issues/child-progress`。
3. **拖拽**：dnd-kit（@dnd-kit/core 6.3 / sortable 10.0），`drag-utils.ts` 负责列归属/位置计算，落库走 `PUT /api/issues/:id`（带 `move_intent`→ 实际 `POST /move` 由锚点算 position）。**无父子联动**。
4. **数据/接口**：Board 数据来自 `/api/issues/table/rows`（group=status/assignee/property + cursor 分页）与 `/api/issues/table/groups`；状态/父级更新走 `UpdateIssue`（支持 `parent_issue_id` 变更，含循环检测）。后端已具备父子树查询能力，**子项跟随父列（跨组）是未实现的新语义**。
5. **部署**：Postgres（docker compose `postgres` 服务）+ Go 后端（`go run ./cmd/server`）+ Next.js 前端（`pnpm dev:web`），`make dev` 一键；env 模板 `.env.example`，登录可用 `MULTICA_DEV_VERIFICATION_CODE` 固定验证码。
6. **分支**：基线 `Equipment_Department_Exploration`（remote @ 6d400f2d）；确认无 `feature/board-tree`。

## 给 Architect 的核心提示

- **子项跟随父列**是需求第 4 条，但后端现有语义是「跨组不跨列」（子项在自己组成为 root，有测试兜底）。需 Architect 决定：复用现有「同组才跟随」语义，还是要新增「子项强制跟随父列」的查询语义（影响 `/table/rows` branchPredicate）。
- Board 若要树形，最省力路径是复用 Table 的「服务端 parent_id 分支 + direct_child_count + collapsed store」机制，而不是前端从平铺数据自己搭树。
- 拖父联动子项目前无任何代码基础，需新设计 drag payload（一次 `parent`/`status` 更新带动全部子项）。

## 分支

- 基线：`origin/Equipment_Department_Exploration` @ `6d400f2d`（2026-08-05）
- 本调研提交：分支 `agent/sgd-magic-research/1808b523`（只读，无代码改动）
