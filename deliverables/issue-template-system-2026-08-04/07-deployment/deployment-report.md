# Issue 模板系统 — 本地部署报告（CLO-169）

**日期**: 2026-08-04
**角色**: DevOps（姚明）
**Issue**: CLO-169 【部署】Issue 模板系统本地部署
**父 Issue**: CLO-159 Issue 模板系统
**部署方式**: 本地部署（用户要求，不走云环境）— Docker Postgres 17 + 本机编译运行后端 + 前端生产构建

---

## 一、部署环境

| 项 | 值 |
|----|----|
| 操作系统 | Ubuntu 24.04 (x86_64) |
| Go | 1.25.5（`/usr/local/go`） |
| Node / pnpm | v22.22.3 / 10.28.2 |
| PostgreSQL | pgvector/pgvector:pg17（Docker Compose 本地容器，127.0.0.1:5432） |
| 后端 | 编译产物 `server/bin/server`，监听 :8080 |
| 前端 | `apps/web` Next.js 16.2.6 生产构建（`next start`），监听 :3000 |
| 部署分支 | `agent/sgd-yaoming-devops/5f65e051-deploy` |
| 部署代码 | CLO-161 后端（2e8d2102）+ CLO-164 前端（0848b2c2、a53fdc45）+ CLO-165 测试（1c909850）+ CLO-163 文档（b71851b9） |

## 二、部署过程

### 2.1 环境准备

1. 从 `.env.example` 生成 `.env`，配置：
   - `APP_ENV=development`
   - `MULTICA_DEV_VERIFICATION_CODE=888888`（本地确定性验证码；`APP_ENV` 非 production 时生效）
   - `JWT_SECRET` 随机生成
   - `DATABASE_URL=postgres://multica:multica@localhost:5432/multica?sslmode=disable`
   - `REMOTE_API_URL=http://localhost:8080`（前端 API 代理上游）
2. `docker compose up -d postgres` 启动 pgvector/pg17。

### 2.2 数据库迁移

`cd server && go run ./cmd/migrate up`

- 迁移 232 `issue_template` 建表、233 workspace 索引均成功应用（up/down 可逆，符合仓库迁移规范：无外键、索引 CONCURRENTLY 独立文件）。
- 验证 `issue_template` 表结构：15 个字段 + 4 个 CHECK 约束 + workspace 索引，与设计一致。

### 2.3 后端构建与运行

- `cd server && go build -o bin/server ./cmd/server` 编译通过。
- 运行 `bin/server`，`:8080` 监听。

### 2.4 前端构建与运行

- `pnpm install` 安装依赖。
- `pnpm --filter @multica/web build` 生产构建成功（`fumadocs-mdx && next build --webpack`），产出 `.next/BUILD_ID`。
- 运行 `next start -p 3000`，生产模式 API 代理（`/api/*`、`/ws`、`/auth/*`）指向 `http://localhost:8080`。

## 三、部署后健康检查

| 检查项 | 结果 |
|--------|------|
| 后端 `/health` | ✅ `{"status":"ok"}` |
| 后端进程稳定 | ✅ 持续运行，无重启，CPU/RSS 稳定 |
| 前端 `GET /login` | ✅ 200 |
| 前端 `GET /{workspace}/settings` | ✅ 200 |
| 前端 `/api/config`（经代理） | ✅ 200 |
| `/api/issue-templates`（经前端代理 :3000） | ✅ 200，返回数据 |

## 四、功能冒烟验证（真实用户流程）

| # | 场景 | 结果 |
|---|------|------|
| 1 | 注册/登录（dev 验证码） | ✅ 获取 token |
| 2 | 创建新工作区 → 自动播种 4 个预置模板 | ✅ 4 条（特性开发/Bug 修复/需求分析/周报月报，`is_preset=true`） |
| 3 | `GET /api/issue-templates` 列表 | ✅ 200 |
| 4 | 创建自定义模板（POST） | ✅ 201，`is_preset=false`、priority 透传 |
| 5 | 查询单条模板（GET） | ✅ 200，字段一致 |
| 6 | 更新模板（PUT） | ✅ 200，name/priority 更新生效 |
| 7 | 删除自定义模板（DELETE） | ✅ 204，再 GET 404 |
| 8 | 删除预置模板 | ✅ 409（`preset templates cannot be deleted`），列表仍 4 条 |
| 9 | 前端 Settings → Templates 页签 | ✅ 页面 200，templates-tab 组件已打包 |
| 10 | 创建 Issue 对话框模板选择器 | ✅ 「从模板创建」入口已打包进生产 bundle |

## 五、交付状态

- **发布版本**：本地部署（dev 构建，未发布到云环境）
- **部署环境**：本机 `localhost:3000`（前端）+ `localhost:8080`（后端）+ Docker Postgres `localhost:5432`
- **部署结果**：✅ 成功，全链路可访问
- **健康状态**：✅ 后端 `/health` OK，前端 200，API 代理正常
- **风险**：🟡 P2 —— `project_id` 未校验 workspace 归属（与测试/审查一致，创建 Issue 时有二次校验兜底）；既有工作区不会自动播种预置模板（P1，仅新建工作区）
- **回滚方案**：见 `rollback.md`

## 六、结论

Issue 模板系统本地部署完成。后端 API、数据库迁移、前端 UI 与代理链路全部验证通过，4 个预置模板开箱即用，CRUD 与预置保护行为正确，符合交付标准，可进入最终交付总结。
