# Issue 模板系统 — 回滚方案（CLO-169）

**日期**: 2026-08-04
**角色**: DevOps（姚明）
**Issue**: CLO-169 【部署】Issue 模板系统本地部署

---

## 一、回滚原则

本地部署回滚分两层：**代码回滚**（后端/前端构建产物）与 **数据回滚**（数据库迁移）。代码回滚优先，数据回滚仅在必要时执行。

## 二、代码回滚（服务级）

### 后端
```bash
# 方式 A：回退到上一个可用提交并重新构建
cd server
git checkout <上一个可用 commit>
go build -o bin/server ./cmd/server

# 方式 B：保留当前代码，仅重启旧二进制（若 bin/server 未被覆盖）
# 备份：部署前先 cp bin/server bin/server.bak，回滚时直接恢复
cp bin/server.bak bin/server

# 重启后端
pkill -f "bin/server" && ./bin/server
```

### 前端
```bash
cd apps/web
# 重新构建旧版本（或恢复备份的 .next）
git checkout <上一个可用 commit>
cd ../.. && pnpm --filter @multica/web build
# 重启前端
pkill -f "next start" && npm run start -- -p 3000
```

## 三、数据库回滚（迁移级）

Issue 模板系统仅新增 2 对迁移（232/233），均为新增表/索引，**不影响任何既有数据**。

```bash
cd server
# 回滚 issue_template 相关迁移（down）
go run ./cmd/migrate down 232
go run ./cmd/migrate down 233
# 重跑可恢复
go run ./cmd/migrate up
```

> 回滚 232/233 只会删除 `issue_template` 表及其索引，不触碰既有业务表。预置模板播种发生在 CreateWorkspace 事务内，回滚后新建工作区会重新播种。

## 四、风险与缓解

| 场景 | 缓解 |
|------|------|
| 前端代理配置错误（API 404） | 仅需改 `.env` 的 `REMOTE_API_URL` 并重启前端，无需代码回滚 |
| 迁移中途失败 | 迁移幂等，重跑 `migrate up` 即可 |
| 预置模板误删/数据问题 | 预置模板受保护（DELETE 409），不会误删；自定义模板数据在 issue_template 表，可用 SQL 恢复 |

## 五、回滚验证

回滚后必须验证：
1. `curl http://localhost:8080/health` → `{"status":"ok"}`
2. 前端 `http://localhost:3000/login` → 200
3. `GET /api/issue-templates`（带认证 + X-Workspace-ID）→ 200
4. 确认 `issue_template` 表状态符合预期（`\d issue_template`）
