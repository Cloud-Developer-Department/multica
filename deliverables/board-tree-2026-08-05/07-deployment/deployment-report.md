# Board 看板树状结构 — 部署报告（CLO-226 / CLO-273）

**日期**: 2026-08-06
**角色**: DevOps（姚明）
**Issue**: CLO-226 【发布】看板树状结构 PR 与部署；CLO-273 【发布】合并 PR #12 并确认部署稳定
**父 Issue**: CLO-218 看板视图支持父子 issue 树状结构
**部署版本**: `feature/board-tree` @ `d14d3805`（含 `101ec0c6` 文档 + `d5f8245a` W2 测试）
**PR**: https://github.com/patrickstar179/multica/pull/12（**已合入**）

---

## 一、发布版本

| 项 | 值 |
|----|----|
| 分支 | `feature/board-tree`（head `d14d3805`，**已于 PR #12 合入主干**） |
| 基线 | `Equipment_Department_Exploration` |
| 核心提交 | `23c6ed5c`（CLO-222 前端实现）、`f8d9eef2`（CLO-198 后端 hierarchy）、`d5f8245a`（CLO-264 W2 测试）、`101ec0c6`（CLO-225 文档）、`d14d3805`（README 索引） |
| PR 状态 | **MERGED**（merge commit `0cd16cde`，2026-08-06 02:47Z） |

## 二、部署环境

| 项 | 值 |
|----|----|
| 操作系统 | Ubuntu 24.04 (x86_64) |
| Go | 1.25.5（`/usr/local/go/bin`） |
| Node / pnpm | v22.22.3 / 10.28.2 |
| PostgreSQL | pgvector/pgvector:pg17（Docker，127.0.0.1:5432，迁移版本 276/276） |
| 后端 | `server/bin/server`（:8080，PID 293881） |
| 前端 | `apps/web` Next.js 16.2.6 生产构建（:3000，PID 302745，CLO-273 重建后重启） |
| 部署目录 | `/home/patrick-sha/multica_workspaces/.../dc6284a5/workdir/multica` |

## 三、部署过程

1. **归并文档分支**：`agent/sgd-durant-documentation/673310c7`（`101ec0c6`）合并进 `feature/board-tree`（ort 策略，无冲突，8 文件 +341/-2）。
2. **README 索引**：`deliverables/board-tree-2026-08-05/README.md` 补 `06-docs/` 行（commit `d14d3805`）。
3. **W2 暂存核对**：`stash@{0}`（CLO-264 暂存的他人改动）全程保留未误提交。
4. **创建 PR #12**：base `Equipment_Department_Exploration`，head `feature/board-tree` @ `d14d3805`，标题含 CLO-218 关联，mergeable 状态验证通过。
5. **构建**：
   - 后端 `go build -o bin/server ./cmd/server` ✅（含 CLO-198 hierarchy）
   - 前端 `pnpm --filter @multica/web build` ✅（`X9ABPBYOvx9m_DwsYo1RS`）
6. **迁移**：本特性零新增迁移（DB 停在 233 + 上游 234-276，无需执行）。
7. **上线**：停旧进程 → 备份旧二进制（`/tmp/.../server.bin.bak-20260806`）→ 启动新后端/前端。

## 四、健康检查

| 检查项 | 结果 |
|--------|------|
| 后端 `/health` | ✅ `{"status":"ok"}` |
| 后端连接 DB | ✅ 276 迁移在册 |
| 前端 `GET /login` | ✅ 200 |
| 前端 `GET /` | ✅ 200 |
| 前端 `/api/config`（代理） | ✅ 200 |
| 静态 chunk | ✅ 200（`53796`/`36556`，board-tree 符号已编译进包） |
| 看板路由 `/issues/board` | ✅ 200 |

## 五、功能冒烟验证（真实 API 流程）

| # | 场景 | 结果 |
|---|------|------|
| 1 | dev 验证码登录（`/auth/send-code` + `/auth/verify-code`） | ✅ 获取 JWT |
| 2 | 创建工作区 `board-tree-smoke` | ✅ 201，issue_prefix `BOA` |
| 3 | 创建父任务 BOA-1 | ✅ `parent_issue_id` 空 |
| 4 | 创建子任务 BOA-2（`parent_issue_id` 指向 BOA-1） | ✅ 父子链路建立 |
| 5 | `POST /api/issues/table/rows?hierarchy=true` | ✅ 返回根行 BOA-1，`direct_child_count: 1`，子项不落根 |
| 6 | 带 `parent_id=BOA-1` 查询 | ✅ 返回 BOA-2（`direct_child_count: 0`） |

> 结论：hierarchy 数据层（CLO-198）在前端生产代理 + 后端全链路可用，Board 树状渲染所需数据就绪。

## 六、验证与测试结果（PR head `d14d3805`）

| 项 | 结果 |
|----|------|
| typecheck（core/ui/views force） | ✅ 3/3 |
| board-tree 专项单测 | ✅ **38/38**（board-column-tree 8 + board-tree-model 6 + drag-utils 24） |
| `@multica/core` 全量 | ✅ 1066/1066 |
| `@multica/views` 全量 | ✅ 3082 通过 / **4 失败**（`locales/parity.test.ts`，基线 `67e58d0f` 同 4 例复现，存量欠债与本次无关） |
| 回归（Table/Swimlane/view-store） | ✅ 全通过 |
| eslint（board-tree 涉及文件） | ✅ 0 error / 0 warning |
| `@multica/web` 生产构建 | ✅ 成功 |

## 七、风险

| 风险 | 等级 | 说明 |
|------|------|------|
| 4 例 parity 存量失败 | 🟢 | 基线 `67e58d0f` 已复现，非本次引入，已获授权放行 |
| 基线分支已前移 | 🟡 | `origin/Equipment_Department_Exploration` 已前移（84221403，含 squad-delegation/workflow）；PR #12 基于 merge-base `6d400f2d`，经 GitHub mergeable 检测为 MERGEABLE，无冲突 |
| 本地部署共享环境 | 🟢 | 已备份旧二进制，可快速回滚 |

## 八、回滚方案

```bash
# 后端回滚
kill $(lsof -ti:8080)
cp /tmp/multica-task-1129798533/opencode/server.bin.bak-20260806 <repo>/server/bin/server
cd <repo> && source .env && nohup ./server/bin/server &

# 前端回滚（旧 .next 已删除，需用旧分支重建）
git checkout <旧commit> && pnpm --filter @multica/web build
cd apps/web && nohup pnpm exec next start -p 3000 &
```

## 九、交付状态

- ✅ PR #12 已创建（MERGEABLE，待 Review 合入）
- ✅ 本地部署上线，健康检查通过
- ✅ 功能冒烟验证通过（hierarchy 全链路）
- ⏳ 观察期：部署后需持续观察 Error Rate / CPU / Memory / 告警（见 AGENTS.md 第五原则）

---

# 合入后复核（CLO-273，2026-08-06）

## 十、合入记录

- **PR #12 合入**：`0cd16cde Merge pull request #12 from patrickstar179/feature/board-tree`（2026-08-06 02:47:44Z，GitHub），base `Equipment_Department_Exploration`，head `feature/board-tree`。
- **主干最新 tip**：`origin/Equipment_Department_Exploration` @ `0cd16cde`（`gh pr view 12` state=**MERGED**，merge_commit=`0cd16cde`）。
- **冗余分支清理**：`feature/board-tree` 已合入，按规范删除远端分支 `git push origin --delete feature/board-tree` ✅（远端现仅剩 `Equipment_Department_Exploration` / `main` / `master`）。
- **本地部署基线**：部署 worktree HEAD `015452d4`，经 `git merge-base --is-ancestor` 确认已被主干包含，部署代码与合入内容一致。

## 十一、合入后部署健康检查

> 复核发现部署目录 `node_modules` / `.next` 被清理导致前端部分路由 500（`next/dist/compiled/cookie` MODULE_NOT_FOUND）。已按部署流程恢复：`pnpm install --frozen-lockfile`（4.9s，热 store）→ `pnpm --filter @multica/web build`（成功，含 board-tree 符号）→ 重启前端 `next start -p 3000`（PID 302745）。后端 PID 293881 未动。

| 检查项 | 结果 |
|--------|------|
| 后端 `/health` | ✅ 200 `{"status":"ok"}` |
| 前端 `GET /login` | ✅ 200 |
| 前端 `GET /` | ✅ 200 |
| 前端 `/api/config` | ✅ 200 |
| 看板路由 `/issues/board`（未登录 → 307 /login） | ✅ 重定向正常 |
| `/issues/board`（登录 + `last_workspace_slug`） | ✅ 307 → `/{slug}/issues/board` |
| `/board-smoke-ws/issues/board`（登录） | ✅ 200 |
| 静态 chunk（board-tree 符号） | ✅ `53796` / `36556` 含 `board-tree-model`、`boardCollapsedParents` |
| 前端日志 | ✅ 无 error / MODULE_NOT_FOUND |

## 十二、合入后功能冒烟（hierarchy 全链路复验）

| # | 场景 | 结果 |
|---|------|------|
| 1 | dev 验证码登录 | ✅ JWT |
| 2 | 创建工作区 `board-smoke-ws`（prefix `BSM`） | ✅ 201 |
| 3 | 创建父任务 BSM-1 | ✅ `parent_issue_id` 空 |
| 4 | 创建子任务 BSM-2（parent=BSM-1） | ✅ 父子链路建立 |
| 5 | `POST /api/issues/table/rows`（`hierarchy.enabled=true`，group=status） | ✅ 根行 BSM-1 `direct_child_count: 1`，子项不落根 |
| 6 | 带 `parent_id=BSM-1` 查询 | ✅ 返回 BSM-2（`direct_child_count: 0`） |
| 7 | 清理冒烟数据 | ✅ 204 删除 BSM-1/BSM-2 |

> 结论：合入主干后本地部署保持健康，Board 树状结构数据层与前端路由全部可用。

## 十三、合入后风险与回滚

| 项 | 说明 |
|----|------|
| 风险 | 🟢 部署环境共享目录曾出现 node_modules 被清理（外部环境行为），已重建并恢复；观察期继续监控 |
| 回滚 | 前端可 `git checkout 0cd16cde^`（或旧分支）重建；后端已有 `server.bin.bak-20260806` 备份，见第八节 |

## 十四、最终交付状态

- ✅ PR #12 已合入主干（`0cd16cde`）
- ✅ 远端冗余分支 `feature/board-tree` 已清理
- ✅ 部署报告更新经 PR #13 合入（`2f116d19`）
- ✅ 合入后冒烟全通过：`/health`、`/login`、`/api/config`、`/issues/board`（含 `/{slug}/issues/board` 200）
- ✅ hierarchy 数据层功能复验通过（BSM-1/BSM-2 父子链路）
- ⏳ 观察期：继续监控 Error Rate / CPU / Memory / 告警
