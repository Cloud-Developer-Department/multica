# Issue 模板系统 — 本地部署与使用详细说明（CLO-169）

**日期**: 2026-08-04
**角色**: DevOps（姚明）
**适用对象**: 需要在本机启动并体验 Issue 模板系统的成员

---

## 一、系统组成

| 组件 | 技术 | 端口 | 说明 |
|------|------|------|------|
| 数据库 | PostgreSQL 17 (pgvector/pg17) | 5432 | Docker 容器，存放全部数据 |
| 后端 | Go 编译产物 `bin/server` | 8080 | REST API + WebSocket，含 Issue 模板 CRUD |
| 前端 | Next.js 16 生产构建 | 3000 | 浏览器访问界面 |

访问入口：**http://localhost:3000**（浏览器打开即可）

---

## 二、当前部署状态（本次已完成）

当前本机已处于运行状态，无需重复启动：

- ✅ 数据库：容器 `multica-postgres-1` 运行中（:5432）
- ✅ 后端：`bin/server` 运行中（:8080），`/health` 返回 `{"status":"ok"}`
- ✅ 前端：`next start` 运行中（:3000），页面与 API 代理正常

> 如果服务已停止，按第三节「如何启动」重启即可。

---

## 三、如何启动（完整步骤）

```bash
# 进入仓库目录
cd multica

# 1. 启动数据库（Docker Postgres 17）
docker compose up -d postgres

# 2. 数据库迁移（幂等，只执行未应用的迁移，含 232/233 issue_template）
cd server && go run ./cmd/migrate up && cd ..

# 3. 构建并启动后端
cd server
go build -o bin/server ./cmd/server
./bin/server &            # 监听 :8080

# 4. 构建并启动前端（首次构建较慢，约几分钟）
cd ..
pnpm --filter @multica/web build
cd apps/web
npm run start -- -p 3000 &   # 监听 :3000

# 5. 验证
curl http://localhost:8080/health    # 期望 {"status":"ok"}
# 浏览器打开 http://localhost:3000
```

### 首次部署前的 .env 配置

复制 `.env.example` 为 `.env`，关键项：

| 配置项 | 推荐值 | 说明 |
|--------|--------|------|
| `APP_ENV` | `development` | 允许使用固定本地验证码 |
| `MULTICA_DEV_VERIFICATION_CODE` | `888888` | 本地固定登录验证码（仅非 production 生效） |
| `JWT_SECRET` | 随机长字符串 | 用 `openssl rand -hex 32` 生成，禁止默认值 |
| `DATABASE_URL` | `postgres://multica:multica@localhost:5432/multica?sslmode=disable` | 数据库连接 |
| `REMOTE_API_URL` | `http://localhost:8080` | 前端 API 代理上游，必须设置 |

---

## 四、如何登录（重点：验证码收不到的原因与解决办法）

### 为什么收不到邮件验证码

本地部署**没有配置邮件服务**（`.env` 中 `RESEND_API_KEY` 为空、SMTP 未配置），因此系统不会真正发送邮件，邮箱自然收不到验证码。这是预期行为，不是故障。

### 解决办法（推荐）

使用**固定本地验证码 `888888`**：

1. 浏览器打开 http://localhost:3000/login
2. 邮箱栏输入任意地址（例如 `test@test.com`，无需真实存在）
3. 验证码栏输入 **`888888`**
4. 点击登录，进入系统后按引导创建或进入工作区

> 该固定码已实测可用（本次部署验证通过）。它只在 `APP_ENV` 非 production 时生效，不会影响生产环境安全性。

### 备选方法（从后端日志读取）

如果不使用固定码，每次登录请求后，后端日志会打印一次性验证码：

```
[DEV] Verification code for test@test.com: 235820
```

后端日志位置取决于启动方式（重定向的文件或启动终端 stdout），找到 `[DEV] Verification code for ...:` 行，把 6 位数字填入即可。

### 如需真实邮件验证码

在 `.env` 配置 `RESEND_API_KEY`（推荐）或 SMTP 参数（`SMTP_HOST`/`SMTP_PORT`/`SMTP_USERNAME`/`SMTP_PASSWORD`/`SMTP_FROM_EMAIL`），然后重启后端。

---

## 五、如何体验 Issue 模板系统

登录成功后：

1. **创建或进入工作区**：登录后按引导新建工作区，或进入已有工作区。
2. **预置模板自动生成**：新建工作区时自动播种 4 个预置模板：
   - 特性开发（engineering / sparkles）
   - Bug 修复（engineering / bug）
   - 需求分析（planning / lightbulb）
   - 周报/月报（planning / list-checks）
3. **从模板创建 Issue**：进入 Issues 页面 → 手动创建 Issue 对话框顶部「从模板创建」下拉，选择模板即自动预填标题、描述、状态、优先级、负责人、项目、阶段、标签。
4. **管理模板**：进入 Settings → Templates 页签，可查看/搜索/新建/编辑/删除模板；预置模板带锁定标记、受保护不可删除（删除返回 409），但可编辑内容。
5. **实时同步**：模板的新建/修改/删除会通过 WebSocket 实时同步到所有在线客户端。

---

## 六、常见问题排查

| 问题 | 原因 | 解决办法 |
|------|------|----------|
| 验证码收不到 | 本地未配置邮件服务 | 用固定码 `888888`，或从后端日志读取 |
| 登录后 API 全 404 | `.env` 未设置 `REMOTE_API_URL` | 设置 `REMOTE_API_URL=http://localhost:8080` 后重启前端 |
| 后端连不上数据库 | Postgres 容器未启动 | `docker compose up -d postgres` |
| 迁移失败 | 依赖未就绪 | 重跑 `go run ./cmd/migrate up`（幂等） |
| 预置模板没有 | 仅「新建工作区」时播种 | 新建一个工作区查看 |
| 端口被占用 | 8080/3000 已被其他程序占用 | `lsof -ti:8080 -ti:3000` 查看并停止占用进程 |

---

## 七、回滚方案（摘要）

- **代码回滚**：后端保留 `bin/server.bak` 备份，`cp bin/server.bak bin/server` 后重启即可；前端重构建旧版本。
- **数据库回滚**：`cd server && go run ./cmd/migrate down 233 && go run ./cmd/migrate down 232`，只会删除 issue_template 表，不影响既有数据。
- 详见交付件 `07-deployment/rollback.md`。

---

## 八、相关交付件（仓库内）

| 文档 | 路径 |
|------|------|
| 部署报告 | `deliverables/issue-template-system-2026-08-04/07-deployment/deployment-report.md` |
| 部署与运维指南 | `deliverables/issue-template-system-2026-08-04/07-deployment/deployment-guide.md` |
| 回滚方案 | `deliverables/issue-template-system-2026-08-04/07-deployment/rollback.md` |
| 功能说明 | `deliverables/issue-template-system-2026-08-04/06-docs/feature-guide.md` |
| API 参考 | `deliverables/issue-template-system-2026-08-04/06-docs/api.md` |
| 验证报告 | `deliverables/issue-template-system-2026-08-04/04-tests/validation-report.md` |
