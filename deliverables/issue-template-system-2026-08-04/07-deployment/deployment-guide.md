# Issue 模板系统 — 本地部署与运维说明（CLO-169）

**日期**: 2026-08-04
**角色**: DevOps（姚明）
**Issue**: CLO-169 【部署】Issue 模板系统本地部署

---

## 一、一键部署（本地）

```bash
# 1. 准备环境文件（首次）
cp .env.example .env
# 编辑 .env：设置 APP_ENV=development、MULTICA_DEV_VERIFICATION_CODE、JWT_SECRET、DATABASE_URL、REMOTE_API_URL

# 2. 启动数据库（Docker Postgres 17）
docker compose up -d postgres

# 3. 安装前端依赖（首次）
pnpm install

# 4. 数据库迁移（含 232/233 issue_template）
cd server && go run ./cmd/migrate up && cd ..

# 5. 构建后端并运行
cd server && go build -o bin/server ./cmd/server && ./bin/server &   # 监听 :8080

# 6. 构建并运行前端
pnpm --filter @multica/web build
cd apps/web && npm run start -- -p 3000 &                            # 监听 :3000

# 7. 访问
#   前端   http://localhost:3000
#   后端   http://localhost:8080/health
```

> 提示：`REMOTE_API_URL=http://localhost:8080` 必须设置，否则生产模式前端不会代理 `/api/*` 到后端。

## 二、服务清单

| 服务 | 端口 | 健康检查 | 日志 |
|------|------|----------|------|
| PostgreSQL (pgvector/pg17) | 5432 | `pg_isready -U multica -d multica` | `docker logs multica-postgres-1` |
| 后端 (Go) | 8080 | `curl http://localhost:8080/health` | 启动时 stdout/stderr |
| 前端 (Next.js) | 3000 | `curl -I http://localhost:3000/login` | 启动时 stdout/stderr |

## 三、关键配置项

| 配置 | 说明 |
|------|------|
| `APP_ENV` | 本地开发设为 `development`（否则需真实邮件验证码） |
| `MULTICA_DEV_VERIFICATION_CODE` | 本地固定验证码（仅非 production 生效） |
| `JWT_SECRET` | 必须随机生成，禁止使用默认值 |
| `DATABASE_URL` | Postgres 连接串 |
| `REMOTE_API_URL` | 前端代理后端地址，如 `http://localhost:8080` |
| `FRONTEND_PORT` / `PORT` | 前端 / 后端端口，默认 3000 / 8080 |

## 四、新版本发布步骤

1. `git pull` 更新代码（含新迁移文件）。
2. `cd server && go run ./cmd/migrate up` 应用新迁移（幂等，只执行未应用的）。
3. `cd server && go build -o bin/server ./cmd/server`，重启后端。
4. `pnpm --filter @multica/web build`，重启前端。

## 五、监控与告警

本地部署建议观察：
- 后端 `/health` 探测（5s 间隔）。
- 数据库连接池压力日志（后端 stdout，`db pool pressure` 关键字）。
- 前端页面可用性（HTTP 200）。
- 后端错误日志关键字：`ERROR`、`WRN db pool`、`panic`。

## 六、常见故障

| 症状 | 排查 |
|------|------|
| 前端登录后 API 全 404 | 检查 `.env` 是否设置 `REMOTE_API_URL`，重启前端 |
| 后端连接数据库失败 | `docker compose up -d postgres`，确认 5432 可连 |
| 迁移失败 | `go run ./cmd/migrate up` 重试（幂等）；必要时用 down 回滚单条 |
| 预置模板缺失 | 预置模板仅「新建工作区」时播种；既有工作区需等后续 seeding 功能 |
| 验证码收不到 | 检查后端日志 `[DEV] Verification code for ...:` |

详见 `rollback.md` 回滚方案。
