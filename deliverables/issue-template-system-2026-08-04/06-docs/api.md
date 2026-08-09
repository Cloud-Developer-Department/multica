# Issue 模板系统 — API 参考

**特性**: Issue 模板系统（CLO-159）
**版本**: 0.5.0（规划）
**日期**: 2026-08-04
**角色**: Documentation（杜兰特）

Base URL：`/api/issue-templates`（所有路由在 `requireWorkspaceMember` 中间件之后，workspace-scoped）。

---

## 1. 端点总览

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/issue-templates` | 列出当前工作区所有模板 |
| POST | `/api/issue-templates` | 创建模板 |
| GET | `/api/issue-templates/{id}` | 获取单个模板 |
| PUT | `/api/issue-templates/{id}` | 更新模板 |
| DELETE | `/api/issue-templates/{id}` | 删除模板（预置模板返回 409） |

---

## 2. 数据模型

### IssueTemplate（响应对象）

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "name": "特性开发",
  "description": "用于规划新特性开发的全流程模板",
  "title_template": "【特性】",
  "body_template": "## 需求描述\n\n\n## 验收标准\n\n- \n\n## 影响范围\n\n",
  "status": "todo",
  "priority": "medium",
  "assignee_type": "member",
  "assignee_id": "uuid",
  "project_id": "uuid",
  "stage": 1,
  "label_ids": ["label-uuid-1", "label-uuid-2"],
  "icon": "sparkles",
  "category": "engineering",
  "is_preset": true,
  "created_by": "uuid",
  "created_at": "2026-08-04T05:00:00Z",
  "updated_at": "2026-08-04T05:00:00Z"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` / `workspace_id` / `created_by` | UUID string | 主键、所属工作区、创建者 |
| `name` | string | 模板名称 |
| `description` | string | 描述 |
| `title_template` | string | 标题前缀/预设 |
| `body_template` | string | Markdown 正文预设 |
| `status` | string | `backlog / todo / in_progress / in_review / done / blocked / cancelled` |
| `priority` | string | `urgent / high / medium / low / none` |
| `assignee_type` | string \| null | `member / agent / squad` |
| `assignee_id` | string \| null | 负责人 ID |
| `project_id` | string \| null | 预选项目 |
| `stage` | int \| null | ≥1 的子 Issue 阶段 |
| `label_ids` | string[] | 预选标签 ID（JSONB UUID 数组） |
| `icon` | string | Lucide 图标名，仅展示 |
| `category` | string | 自由分类，仅展示 |
| `is_preset` | bool | 预置标记，预置不可删除 |
| `created_at` / `updated_at` | RFC3339 | 时间戳 |

---

## 3. GET /api/issue-templates

列出当前工作区全部模板，预置优先、名称升序。

### Response 200

```json
{
  "issue_templates": [
    { "id": "...", "name": "特性开发", "is_preset": true, "..." : "" },
    { "id": "...", "name": "自定义模板", "is_preset": false, "..." : "" }
  ],
  "total": 2
}
```

### Curl Example

```bash
curl -H "Authorization: Bearer $TOKEN" \
  -H "X-Workspace-ID: $WORKSPACE_ID" \
  https://example.com/api/issue-templates
```

---

## 4. POST /api/issue-templates

创建模板。body 字段与 `CreateIssueTemplateRequest` 一致，均可选，默认值：`status=todo`、`priority=none`。

### Request Body

```json
{
  "name": "周报",
  "description": "周期性汇报模板",
  "title_template": "【周报】",
  "body_template": "## 本期进展\n\n- \n\n## 遇到的问题\n\n\n## 下期计划\n\n- \n",
  "status": "todo",
  "priority": "none",
  "label_ids": ["label-uuid-1"],
  "icon": "list-checks",
  "category": "planning"
}
```

### Response 201

返回创建的完整 `IssueTemplate` 对象。

### 错误

| 状态码 | 场景 |
|--------|------|
| 400 | 空 name / name 含控制字符 / name>64 / description>500 / body>16000 / 非法 status·priority / stage<1 / 非法或不存在 label / 非法 assignee / label 非 issue 类型 / 畸形 JSON |
| 401 | 未认证 |
| 500 | 数据库错误 |

### Curl Example

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Workspace-ID: $WORKSPACE_ID" \
  -d '{"name":"Bug 修复","title_template":"【Bug】","priority":"high"}' \
  https://example.com/api/issue-templates
```

---

## 5. GET /api/issue-templates/{id}

### Response 200

返回单个 `IssueTemplate` 对象。

### 错误

| 状态码 | 场景 |
|--------|------|
| 404 | 模板不存在或不属于当前工作区（跨 workspace 隔离） |
| 401 | 未认证 |

---

## 6. PUT /api/issue-templates/{id}

更新模板。COALESCE 语义：**省略即保留原值**；`assignee_type` / `assignee_id` / `project_id` / `stage` 传 `null` 会**清空**该字段（与其余字段的省略保留语义不同，注意区分）。

### Request Body

```json
{
  "name": "Bug 修复 v2",
  "priority": "urgent",
  "assignee_type": null,
  "assignee_id": null
}
```

### Response 200

返回更新后的完整 `IssueTemplate` 对象。

### 错误

| 状态码 | 场景 |
|--------|------|
| 400 | 与创建相同校验失败 |
| 404 | 模板不存在或不属于当前工作区 |
| 401 | 未认证 |

---

## 7. DELETE /api/issue-templates/{id}

### Response 204

删除成功（自定义模板）。

### 错误

| 状态码 | 场景 |
|--------|------|
| 409 | 预置模板不可删除 |
| 404 | 模板不存在或不属于当前工作区 |
| 401 | 未认证 |

### Curl Example

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "X-Workspace-ID: $WORKSPACE_ID" \
  https://example.com/api/issue-templates/$TEMPLATE_ID
```

---

## 8. WebSocket 事件

| 事件 | 触发时机 | Payload |
|------|----------|---------|
| `issue_template:created` | 创建模板后 | `{ "issue_template": IssueTemplate }` |
| `issue_template:updated` | 更新模板后 | `{ "issue_template": IssueTemplate }` |
| `issue_template:deleted` | 删除模板后 | 模板 ID |

前端通过 `use-realtime-sync` 的 `issue_template` 前缀处理器 invalidate 模板查询缓存，实现多端实时同步。

---

## 9. 前端 SDK 用法（packages/core）

```ts
import { api } from "@multica/core/api";

// 列出
const { issue_templates } = await api.listIssueTemplates();

// 创建
const tpl = await api.createIssueTemplate({
  name: "需求分析",
  title_template: "【需求】",
  category: "planning",
});

// 更新
await api.updateIssueTemplate(tpl.id, { priority: "high" });

// 删除
await api.deleteIssueTemplate(tpl.id);
```

React Query hooks：`packages/core/issue-templates/`（`issueTemplateListOptions` + `useCreateIssueTemplate` / `useUpdateIssueTemplate` / `useDeleteIssueTemplate`）。

---

## 10. 数据库

### 迁移

- `232_issue_template.up.sql`：建表 `issue_template`
- `233_issue_template_workspace_index.up.sql`：`CREATE INDEX CONCURRENTLY idx_issue_template_workspace ON issue_template(workspace_id)`

遵循仓库硬约束：无外键、索引 CONCURRENTLY 独立文件、up/down 可逆。

### 预置播种

`CreateWorkspace` 事务内调用 `SeedPresetIssueTemplates`，插入 4 条 `is_preset=true` 模板，与工作区创建原子提交。
