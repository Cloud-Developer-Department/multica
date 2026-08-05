# 复部署验证报告 — Issue 模板正文带出（CLO-191）

**日期**: 2026-08-05
**角色**: DevOps（姚明）
**Issue**: CLO-191 【复部署】Bug 修复后本地重新部署验证
**父 Issue**: CLO-159 Issue 模板系统

## 结论

基于已合并的 PR #4（`fe830ab9`，`Equipment_Department_Exploration` HEAD `6d400f2d`）重新本地部署，**「从模板创建 Issue 时正文带出」验证通过**。

## 部署环境

| 服务 | 版本/端口 | 健康状态 |
|------|-----------|----------|
| PostgreSQL | pgvector/pg17，:5432 | `pg_isready` 正常，迁移 232/233 已应用，`issue_template` 表 40 行 |
| 后端 | Go 1.25，:8080 | `/health` → `{"status":"ok"}`，无 panic/错误日志 |
| 前端 | Next.js 16，:3000 | `/login` 200，生产构建，`REMOTE_API_URL` 代理生效 |

## 验证过程

### 1. 代码确认
- 分支 `Equipment_Department_Exploration` 已含 PR #4 合并提交（`git log` 确认 `6d400f2d Merge pull request #4`）
- 修复行存在：`packages/views/modals/create-issue.tsx:882` `key={formResetKey}`（`ContentEditor`），与 `TitleEditor`（:867）一致

### 2. 回归测试
- 用例「pre-fills the title and body when a template is applied」**通过**
- create-issue 全量套件 **37/37 通过**（4.17s）
- 用例断言：应用模板后正文编辑器 value == 模板 `body_template`；提交 create-issue 请求携带 `description` == 模板正文

### 3. 运行产物一致性
- 线上加载的前端 chunk（`25106-f13677104fbcc3d4.js`，含 `resetKey`）SHA-256 与本地构建产物一致（`05fe7f1f...`），确认运行的就是含修复的 bundle

### 4. 端到端 API
1. `POST /auth/send-code` → ok
2. `POST /auth/verify-code`（dev 码 `888888`）→ 返回 JWT
3. `POST /api/workspaces` → 创建成功（播种 4 预置模板）
4. `GET /api/issue-templates?workspace_id=...` → 返回 4 个预置模板，`body_template`/`title_template` 均非空（Bug 修复 body_len=41，周报/月报=36，特性开发=32，需求分析=52）

## 稳定性观察

部署后持续观察：后端无重启、无错误日志（无 `panic`/`ERROR`/`db pool pressure`），CPU/RSS 稳定，`/health` 持续 ok。

## 风险

- 无阻塞项。修复仅前端 1 行（`ContentEditor` 挂 `key`），不涉及后端/数据面改动。

## 交付物

| 文件 | 说明 |
|------|------|
| `07-deployment/redeploy-verification.md` | 本报告 |
| 上轮 | `03-code/bugfix-report.md`、`04-tests/verification.md`（CLO-187） |
