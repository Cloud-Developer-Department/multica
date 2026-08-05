# Issue 模板正文带出修复 — 本地复部署报告（CLO-191）

**日期**: 2026-08-05
**角色**: DevOps（姚明）
**Issue**: CLO-191 【复部署】Bug 修复后本地重新部署验证
**父 Issue**: CLO-159 Issue 模板系统
**前置**: CLO-192 复审通过（PR #4，Review 角色保罗）

---

## 一、部署方式

与 CLO-169 一致：本地部署（用户要求，不走云环境）— Docker Postgres 17 + 本机编译后端（:8080）+ 前端生产构建（:3000）。

## 二、部署代码

| 项 | 值 |
|----|----|
| 部署分支 | `Equipment_Department_Exploration`（特性分支） |
| 合入修复 | PR [#4](https://github.com/patrickstar179/multica/pull/4) `fe830ab9`（merge commit `6d400f2d`） |
| 修复内容 | `packages/views/modals/create-issue.tsx` — `ContentEditor` 补挂 `key={formResetKey}`，使模板正文随重挂载带出（1 行）+ 回归测试 |

PR #4 复审（CLO-192）通过后，由 DevOps 合并入特性分支，再基于合并后的最新代码重建并复部署。

## 三、部署过程

1. 合并 PR #4 → `Equipment_Department_Exploration`（merge commit `6d400f2d`），`git fetch` 同步本地。
2. 复用 CLO-169 的 `.env`（`APP_ENV=development`、`MULTICA_DEV_VERIFICATION_CODE=888888`、`JWT_SECRET`、`DATABASE_URL`、`REMOTE_API_URL=http://localhost:8080`）。
3. `docker compose up -d postgres` 复用运行中的 pgvector/pg17（:5432）。
4. `cd server && go run ./cmd/migrate up` — 全部迁移已应用（含 232/233 `issue_template`），幂等 skip。
5. `cd server && go build -o bin/server ./cmd/server` — 编译通过（43.8MB 二进制）。
6. `pnpm install --frozen-lockfile`（工作区依赖）→ `pnpm --filter @multica/web build` — 生产构建成功。
7. 停掉 CLO-169 旧进程（旧工作区 `bed6abb2` 的 pre-fix 构建），启动新构建：
   - 后端：`./server/bin/server`（监听 :8080，`.env` 已 source，固定验证码生效）
   - 前端：`next start -p 3000`（监听 :3000，API 代理指向 :8080）

## 四、部署后健康检查

| 检查项 | 结果 |
|--------|------|
| 后端 `/health` | ✅ `{"status":"ok"}` |
| 后端进程 | ✅ 持续运行，无重启，无 panic/ERROR，RSS 约 36MB |
| 前端 `/login` | ✅ 200 |
| 前端 `/api/config`（经代理） | ✅ 200 |
| `/api/issue-templates`（经前端代理 :3000） | ✅ 200，返回数据 |
| 前端 WebSocket | ✅ 正常连接/断开（无异常日志） |
| 数据库 | ✅ `pg_isready` accepting connections，池压力正常（`avg_acquire_ms=0`） |

## 五、功能验证（含 Bug 修复专项）

### 5.1 认证（dev 固定验证码）

- `POST /auth/send-code` → ✅ `Verification code sent`
- `POST /auth/verify-code`（code=`888888`）→ ✅ 返回 JWT（固定码已在 `.env` 生效）

### 5.2 模板 CRUD（API 回归）

| 场景 | 结果 |
|------|------|
| 新建工作区自动播种 4 个预置模板 | ✅ 4 条（Bug 修复/周报月报/特性开发/需求分析），均含 `body_template` |
| `GET /api/issue-templates` 列表 | ✅ 200 |
| 创建自定义模板 | ✅ 201（`部署检查单`，body_template 写入） |
| 更新模板 | ✅ 200（name/body 更新生效） |
| 删除自定义模板 | ✅ 204 → 再 GET 404 |
| 删除预置模板 | ✅ 409（保护生效） |

### 5.3 Bug 修复专项：模板正文带出（核心验证）

**单元/组件测试**：`create-issue.test.tsx` 37/37 通过，其中回归用例「pre-fills the title and body when a template is applied」通过（独立运行 1 passed / 36 skipped 确认该用例有效）。

**生产环境端到端验证**（Playwright + Chromium 真实浏览器，访问 :3000 生产构建）：

1. 登录 → 进入工作区 Issues 页（真实 UI）。
2. 打开「New Issue」对话框 → 切换到「Create manually」。
3. 点击模板下拉 → 选择「Bug 修复」模板（`body_template` = `## 复现步骤 / 1. / ## 期望行为 / ## 实际行为 / ## 环境`）。
4. **验证通过**：选择模板后对话框内同时出现：
   - 标题带出 `【Bug】`
   - 优先级带出 `High`
   - **正文带出 `复现步骤 | 期望行为 | 实际行为 | 环境`（模板 body_template 全文出现在正文编辑器）** ← 修复前该内容为空，修复后正确带出。
5. 截图留证：`01-issues-page.png` / `02-dialog.png` / `03-after-template.png`（本任务工作区，未入库）。

**验证结论**：CLO-187 所述缺陷（模板正文不随重挂载带出）在最新生产构建中已修复，标题、正文、优先级均按模板预填。

### 5.4 前端构建产物确认

- 生产 bundle 编译产物（`.next/cache/webpack/client-production/*.pack`）包含 `formResetKey` 关联逻辑，确认构建源为合入修复后的代码。

## 六、观察（部署后）

| 项 | 观察结果 |
|----|---------|
| Error rate | ✅ 后端日志无 ERROR/panic；前端日志无 error/failed |
| CPU / Memory | ✅ 后端 RSS ~36MB、前端 ~62MB，稳定无增长 |
| 日志 | ✅ 请求日志正常，无连接池压力（`avg_acquire_ms=0`） |
| 成功率 | ✅ 所有接口调用成功（200/201/204 符合预期） |

## 七、风险与回滚

- **风险**：🟡 与 CLO-169 一致 —— 既有工作区不会自动播种预置模板（仅新建工作区播种）；`project_id` 未校验 workspace 归属（创建 Issue 时另有兜底）。均为已知非阻塞项。
- **回滚方案**：与 CLO-169 `rollback.md` 一致 —— 前端回退到合并前构建、后端回退到 `7b36da4b`（pre-fix 提交）重新编译即可；数据库无新增迁移，无需回滚数据。

## 八、结论

Bug 修复（PR #4）已合入特性分支并完成本地复部署。后端、前端、数据库全链路健康，模板 CRUD 回归通过，**模板正文带出缺陷已在生产构建中修复并经真实浏览器端到端确认**。复部署通过，可收口。
