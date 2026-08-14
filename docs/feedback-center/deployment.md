# 反馈中心 — 部署说明

> 对应实现：反馈中心 v1.0（2026-08-13）。反馈中心是 Multica 前端/后端的一个功能模块，**不引入任何新的服务、依赖或第三方组件**，随 Multica 整体一起构建与部署。

## 1. 部署概述

反馈中心完全运行在 Multica 现有架构内：

- 后端：Go（chi router + sqlc + pgx），新增 `feedback_center.go` handler 与 3 个数据库迁移；
- 前端：Next.js（web）/ Electron（desktop），复用现有 `@multica/views` 与 `@multica/core` 包；
- 数据库：PostgreSQL，仅新增 3 张/改造 1 张表；
- **无新增环境变量、无新增镜像、无新增第三方依赖**（本次改动 `go.mod`/`go.sum` 无变化，前端仅新增一个包导出路径）。

因此部署流程与 Multica 常规部署完全一致，只需确保**数据库迁移已应用**。

## 2. 数据库迁移

反馈中心涉及 3 个迁移文件（`server/migrations/`）：

| 迁移 | 内容 |
|---|---|
| `285_feedback_center.up.sql` / `.down.sql` | 改造 `feedback` 表：`user_id→creator_id`、`message→description`，新增 `title`/`type`/`updated_at`，存量回填（title 截断 / `(no title)`、type=`other`），新增 `(workspace_id, created_at DESC)` 索引 |
| `286_feedback_vote.up.sql` / `.down.sql` | 新建 `feedback_vote` 表，**`UNIQUE(feedback_id, user_id)`** 硬约束 |
| `287_feedback_comment.up.sql` / `.down.sql` | 新建 `feedback_comment` 表 + 索引 |

迁移 up/down 成对、可回滚。**在部署服务前务必应用迁移**：

```bash
# 本地 / 开发
make migrate-up

# 或直接运行
cd server && go run ./cmd/migrate up
```

> 反馈中心上线后，请勿直接执行 `migrate down` 回滚 285/286/287，除非确认不需要保留反馈中心数据。

## 3. 本地开发部署

```bash
# 一键：创建 env、确保 DB、应用迁移、启动全部服务
make dev
```

`make dev` 会自动执行数据库迁移（`migrate up`），无需手动步骤。启动后：

- 前端：`http://localhost:3000`
- 后端：`http://localhost:8080`
- 数据库：PostgreSQL `localhost:5432/multica`

登录任一工作区后，通过左侧导航「💬 反馈」即可访问反馈中心。

## 4. 自托管 / Docker Compose 部署

### 4.1 使用官方镜像（推荐）

```bash
make selfhost
```

自动创建 `.env`、生成随机 `JWT_SECRET` 并启动全部服务（镜像从 GHCR 拉取）。启动时容器会自动应用数据库迁移。

### 4.2 从当前代码构建

如果所选 GHCR 标签尚未发布，从当前 checkout 构建：

```bash
make selfhost-build
```

或手动执行：

```bash
docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build
```

### 4.3 手动 Compose

```bash
docker compose -f docker-compose.selfhost.yml pull
docker compose -f docker-compose.selfhost.yml up -d
```

> 详情参见根目录 [SELF_HOSTING.md](../SELF_HOSTING.md)。

## 5. 环境变量

反馈中心**不新增任何环境变量**。复用现有变量：

| 变量 | 作用 | 备注 |
|---|---|---|
| `DATABASE_URL` | 数据库连接 | 迁移与运行必需 |
| `JWT_SECRET` | 认证令牌签名 | 生产环境必须改为强随机值 |
| `PORT` | 后端端口 | 默认 8080 |
| `FRONTEND_PORT` / `FRONTEND_ORIGIN` | 前端端口与源 | 默认 3000 |

## 6. 部署后验证

部署完成后，按以下步骤验证反馈中心可用：

1. **导航**：登录后左侧导航出现「💬 反馈」一级入口（MessageCircle 图标）；底部帮助菜单中的「反馈」也能跳转同一页面。
2. **列表 API**：

```bash
curl -H "Authorization: Bearer <token>" -H "X-Workspace-Slug: <slug>" \
  "http://localhost:8080/api/feedbacks?page=1&page_size=20"
```

期望返回 `200` 与 `{items,total,page,page_size,has_more}`。

3. **详情 / 评论 / 点赞 API**：创建一条反馈（`POST /api/feedbacks`）、点赞（`POST /api/feedbacks/{id}/vote`）、发表评论（`POST /api/feedbacks/{id}/comments`），确认均在 UI 上可见。
4. **权限**：未登录调用写接口应返回 `401`。

## 7. 运维注意

- **限流**：提交反馈沿用现有每小时 10 条/用户的限流；评论与点赞本期未限流（安全审计低危建议项，可后续迭代）。
- **低危加固建议**（来自安全审计，非阻断）：`ListFeedbacks` 的 `page` 参数无上限，极端值可能使 offset 溢出——建议后续对 `page` 加上限；评论接口可增加按用户限流。
- **数据保留**：workspace 删除时 `feedback.workspace_id` 被 SET NULL 脱离（保留行为不变）；`feedback_vote` / `feedback_comment` 在 workspace 删除清单中按 KEEP 保留。
