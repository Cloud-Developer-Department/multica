# Issue 模板系统 — 后端 CRUD API 交付（CLO-161）

**日期**: 2026-08-04
**角色**: Coding（库里）
**Issue**: CLO-161 【后端】Issue 模板 CRUD API 实现
**父 Issue**: CLO-159 Issue 模板系统

---

## 交付物清单

| 文件 | 说明 |
|------|------|
| `03-code/backend-crud.diff` | 修改文件的 git diff |
| `03-code/new-files.txt` | 新增文件清单 |
| `03-code/implementation.md` | 实现说明（本文件） |

---

## 修改内容

### 新增文件

| 文件 | 说明 |
|------|------|
| `server/migrations/232_issue_template.up.sql` | issue_template 表建表迁移 |
| `server/migrations/232_issue_template.down.sql` | 回滚 |
| `server/migrations/233_issue_template_workspace_index.up.sql` | workspace_id 索引（CONCURRENTLY，独立文件） |
| `server/migrations/233_issue_template_workspace_index.down.sql` | 回滚 |
| `server/pkg/db/queries/issue_template.sql` | SQLC 查询（List/Get/Create/Update/Delete/Count） |
| `server/pkg/db/generated/issue_template.sql.go` | sqlc 生成的 Go 代码 |
| `server/internal/handler/issue_template.go` | Handler：CRUD + 预置模板播种 |

### 修改文件

| 文件 | 说明 |
|------|------|
| `server/cmd/server/router.go` | 注册 `/api/issue-templates` 路由 |
| `server/internal/handler/workspace.go` | CreateWorkspace 事务中播种 4 个预置模板 |
| `server/pkg/protocol/events.go` | 新增 3 个 WS 事件 |
| `server/pkg/db/generated/models.go` | sqlc 生成：IssueTemplate 结构体 |

---

## API 路由

```
GET    /api/issue-templates           ListIssueTemplates
POST   /api/issue-templates           CreateIssueTemplate
GET    /api/issue-templates/{id}      GetIssueTemplate
PUT    /api/issue-templates/{id}      UpdateIssueTemplate
DELETE /api/issue-templates/{id}      DeleteIssueTemplate
```

所有路由在 `requireWorkspaceMember` 中间件之后，workspace-scoped。

---

## 数据模型

```sql
CREATE TABLE issue_template (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    title_template TEXT NOT NULL DEFAULT '',
    body_template TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'todo' CHECK (...),
    priority TEXT NOT NULL DEFAULT 'none' CHECK (...),
    assignee_type TEXT CHECK (assignee_type IN ('member', 'agent', 'squad')),
    assignee_id UUID,
    project_id UUID,
    stage INT,
    label_ids JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof = 'array'),
    icon TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    is_preset BOOLEAN NOT NULL DEFAULT false,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- 无外键（遵循仓库迁移规范）
- 索引在独立迁移文件中用 `CREATE INDEX CONCURRENTLY` 创建
- `label_ids` 存为 JSONB UUID 字符串数组，与 `CreateIssueRequest.label_ids` 形状一致

---

## 预置模板

4 个预置模板在 `CreateWorkspace` 事务中自动播种：

| 名称 | category | icon | priority | 说明 |
|------|----------|------|----------|------|
| 特性开发 | engineering | sparkles | medium | 新特性规划全流程 |
| Bug 修复 | engineering | bug | high | 缺陷跟踪修复 |
| 需求分析 | planning | lightbulb | medium | 调研拆解新需求 |
| 周报/月报 | planning | list-checks | none | 周期性汇报 |

预置模板 `is_preset=true`，受保护不可删除（返回 409），但可编辑内容。

---

## 校验规则

- `name`：必填，≤64 字符，无控制字符
- `description`：≤500 字符
- `body_template`：≤16000 字符
- `status` / `priority`：枚举校验（与 issue 表 CHECK 约束一致）
- `stage`：≥1
- `assignee_type` + `assignee_id`：复用 `validateAssigneePair`，校验 member/agent/squad 在 workspace 内有效
- `label_ids`：每个 ID 校验属于当前 workspace 的 issue 类型 label，去重，≤50 个
- 所有文本字段经 `sanitizeNullBytes` 清理

---

## WebSocket 事件

| 事件 | 触发时机 |
|------|---------|
| `issue_template:created` | 创建模板后 |
| `issue_template:updated` | 更新模板后 |
| `issue_template:deleted` | 删除模板后 |

---

## 影响范围

- **新建 workspace**：CreateWorkspace 事务中新增 4 行 issue_template 插入，与 workspace 创建原子提交
- **现有 workspace**：不受影响（预置模板仅在新 workspace 创建时播种）
- **现有 API**：无改动，仅新增路由
- **数据库**：新增 1 张表 + 1 个索引，2 对迁移文件

---

## 风险

1. **现有 workspace 无预置模板** — 仅新创建的 workspace 自动播种。已有 workspace 需后续补一个 seeding 脚本/端点（P1，不在本 issue 范围）。
2. **sqlc 版本差异** — 生成的代码含 v1.31.1 版本注释；仓库现有生成文件混用 v1.28.0/v1.31.1。功能无差异，`make sqlc` 会用当前安装版本重新生成。
3. **label_ids 松耦合** — label 删除后模板的 label_ids 可能引用不存在的 label；应用模板时前端应过滤无效 ID（与 CreateIssue 的 label_ids 校验一致，后端会拒绝无效 label）。

---

## 建议测试点

1. CRUD 全流程：创建 → 查询 → 更新 → 删除
2. 预置模板播种：新建 workspace 后 `GET /api/issue-templates` 返回 4 条
3. 预置模板保护：`DELETE` 预置模板返回 409
4. 校验：无效 status/priority/assignee/label_ids 返回 400
5. workspace 隔离：跨 workspace 访问返回 404
6. 迁移：232/233 up/down 可逆

---

## 是否需要配置修改

否。

---

## 是否需要文档更新

是 — API 路由清单需补充 `/api/issue-templates`（由 Documentation 角色 CLO-163 处理）。
