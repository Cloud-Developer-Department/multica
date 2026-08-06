# Workflow / Artifact CLI 设计文档（CLO-175）

> 父需求：CLO-175（扩展 Multica CLI 支持 Workflow 与 Artifact 管理，实现 Agent 研发闭环）
> 文档阶段：阶段七（文档同步，CLO-181）
> 依据代码：`server/cmd/multica/cmd_workflow.go`、`server/cmd/multica/cmd_artifact.go`、
> `server/cmd/multica/main.go`、`server/internal/handler/workflow.go`、
> `server/internal/handler/artifact.go`、`server/cmd/server/router.go`

---

## 1. 概述

本次扩展为 `multica` CLI 新增四个命令，补齐 Agent 软件研发闭环的两项关键能力：

1. **Workflow 管理**：创建研发流程（`workflow create`）、查询流程状态（`workflow get`）。
2. **Artifact 管理**：提交阶段产物（`artifact submit`）、查询产物（`artifact list`）。

四个命令均在 `main.go` 中注册为 **Core** 命令组，与 `issue` / `project` / `agent` /
`squad` 等并列。

| 命令组 | 命令 | 用途 |
|---|---|---|
| `multica workflow` | `create` / `get` | 创建 / 查询研发流程 |
| `multica artifact` | `submit` / `list` | 提交 / 查询阶段产物 |

设计原则：**Agent 可创建流程、提交产物，但不能绕过人工审核**。所有审核 / 推进 / 覆盖
类操作既不提供 CLI 命令，后端路由也以 `RequireHumanActor` 中间件拒绝机器凭证（见 §6）。

## 2. 命令参考

### 2.1 `multica workflow create`

创建 Workflow。内部先用 `--issue-id` 解析来源 Issue，再调用 `POST /api/workflows`。

```bash
multica workflow create \
  --name "学生管理系统开发流程" \
  --issue-id CLO-123 \
  --description "完成学生管理系统需求分析、开发、测试" \
  [--template software_rd] \
  [--output json|table]
```

**参数**

| 参数 | 必填 | 说明 |
|---|---|---|
| `--name` | 是 | Workflow 名称；为空时报错 `--name is required` |
| `--issue-id` | 是 | 来源 Issue，支持编号（`CLO-123`）或完整 UUID；为空时报错 `--issue-id is required` |
| `--description` | 否 | Workflow 描述 |
| `--template` | 否 | Workflow 模板 key；默认 `software_rd`（内置 AI 软件研发流水线，5 个阶段） |
| `--output` | 否 | 输出格式，`json`（默认）或 `table` |

**API 映射**：`POST /api/workflows`

请求体（CLI 发送）：

```json
{
  "name": "学生管理系统开发流程",
  "source_issue_id": "<issue-uuid>",
  "description": "完成学生管理系统需求分析、开发、测试",
  "template_key": "software_rd"
}
```

说明：`--description`、`--template` 为空时不发送对应字段；`--issue-id` 由 CLI 先通过
`GET /api/issues/{key|uuid}` 解析为 UUID 后再填入 `source_issue_id`。

响应（`--output json` 直接透传后端返回值，典型结构）：

```json
{
  "id": "<workflow-uuid>",
  "workspace_id": "<workspace-uuid>",
  "source_issue_id": "<issue-uuid>",
  "name": "学生管理系统开发流程",
  "description": "完成学生管理系统需求分析、开发、测试",
  "status": "todo",
  "current_stage": 1,
  "created_by_type": "agent",
  "created_by_id": "<agent-uuid>",
  "created_at": "...",
  "updated_at": "...",
  "stages": [
    {
      "stage": 1,
      "name": "需求分析",
      "status": "todo",
      "nodes": [
        {
          "id": "<node-uuid>",
          "workflow_id": "<workflow-uuid>",
          "stage": 1,
          "seq": 1,
          "type": "requirements",
          "name": "需求分析",
          "status": "todo",
          "issue_id": "<child-issue-uuid>",
          "assignee_type": null,
          "assignee_id": null,
          "review_required": true,
          "artifact_review_status": "none",
          "created_at": "...",
          "updated_at": "..."
        }
      ]
    }
  ],
  "progress": {"total_nodes": 1, "done_nodes": 0, "blocked_nodes": 0, "in_review_nodes": 0}
}
```

**行为**：创建即实例化 `software_rd` 模板，为每个节点生成子 Issue 并激活阶段一
（`backlog → todo`），触发对应 Agent 执行。

### 2.2 `multica workflow get <workflow-id>`

查询 Workflow 状态。返回当前状态、当前阶段、当前节点 / Agent / 任务以及已提交 Artifact。

```bash
multica workflow get <workflow-id> [--output json|table]
```

**参数**

| 参数 | 必填 | 说明 |
|---|---|---|
| `<workflow-id>` | 是 | Workflow UUID（位置参数） |
| `--output` | 否 | 输出格式，`json`（默认）或 `table` |

**API 映射**：
- `GET /api/workflows/{id}` —— 主查询；
- `GET /api/artifacts?workflow_id=<id>&workspace_id=<ws>` —— 尽力而为地拉取已完成
  Artifact；该调用失败**不**导致命令失败，Artifact 列表降级为空（FR-2）。

响应（`--output json`）：

```json
{
  "id": "<workflow-uuid>",
  "name": "学生管理系统开发流程",
  "status": "in_progress",
  "current_stage": 1,
  "current_node": "需求分析",
  "current_agent": "需求分析智能体",
  "current_task": "<child-issue-uuid>",
  "artifacts": [
    {
      "id": "<artifact-uuid>",
      "title": "requirement.md",
      "type": "requirements",
      "status": "submitted",
      "version": 1,
      "...": "..."
    }
  ],
  "stages": [],
  "progress": {"total_nodes": 1, "done_nodes": 0, "blocked_nodes": 0, "in_review_nodes": 0},
  "source_issue_id": "<issue-uuid>"
}
```

**派生字段**（`deriveWorkflowCurrent`）：`current_node` / `current_agent` / `current_task`
由 `stages` 中当前阶段（`current_stage`）第一个非终态节点推导，跳过 `done` / `cancelled` /
`blocked` / `backlog` 节点；assignee 显示名通过 actor 查找（尽力而为）。

### 2.3 `multica artifact submit`

提交某 Workflow 节点的阶段产物。文本类文件内联进 `content` 字段；二进制或超大类文件
先上传附件，以 `file_attachment_id` 引用。

```bash
multica artifact submit \
  --workflow-id <workflow-uuid> \
  --node-id <node-uuid> \
  --type requirement \
  --name requirement.md \
  --file ./requirement.md \
  [--allow-external-file] \
  [--output json|table]
```

**参数**

| 参数 | 必填 | 说明 |
|---|---|---|
| `--workflow-id` | 是 | 所属 Workflow UUID |
| `--node-id` | 是 | 提交对应的 Workflow 节点 UUID（每个阶段产物挂到具体节点） |
| `--type` | 是 | Artifact 类型，支持别名归一化（见 §3） |
| `--name` | 是 | Artifact 名称 / 标题（如 `requirement.md`），作为 `title` 发送 |
| `--file` | 是 | 待提交文件路径；不接受 URL；默认必须位于当前工作目录内（`MUL-4252`） |
| `--allow-external-file` | 否 | 允许 `--file` 读取工作目录之外的路径 |
| `--output` | 否 | 输出格式，`json`（默认）或 `table` |

**API 映射**：`POST /api/artifacts`（文本内联场景）

```json
{
  "workflow_id": "<workflow-uuid>",
  "node_id": "<node-uuid>",
  "type": "requirements",
  "title": "requirement.md",
  "content_type": "markdown",
  "content": "# 需求分析\n\n实现学生管理系统"
}
```

二进制 / 超过 5 MB 的文本文件改为两步：先 `POST /api/upload-file` 上传附件，再提交
`file_attachment_id` 而非 `content`。

响应（`--output json`，典型结构）：

```json
{
  "id": "<artifact-uuid>",
  "workspace_id": "<workspace-uuid>",
  "workflow_id": "<workflow-uuid>",
  "node_id": "<node-uuid>",
  "issue_id": "<child-issue-uuid>",
  "type": "requirements",
  "title": "requirement.md",
  "content_type": "markdown",
  "version": 1,
  "status": "submitted",
  "author_type": "agent",
  "author_id": "<agent-uuid>",
  "created_at": "...",
  "updated_at": "..."
}
```

**提交侧效应**：Artifact 状态置为 `submitted`，同 (节点, 类型) 旧版本置为
`superseded`；若节点 `review_required=true`，节点与映射子 Issue 一并进入 `in_review`，
并通知审核人（人工审核节点）。

### 2.4 `multica artifact list`

列出某 Workflow 下的 Artifact，可按类型 / 状态过滤。

```bash
multica artifact list --workflow-id <workflow-uuid> \
  [--type requirement] \
  [--status submitted] \
  [--output table|json]
```

**参数**

| 参数 | 必填 | 说明 |
|---|---|---|
| `--workflow-id` | 是 | 所属 Workflow UUID；为空时报错 `--workflow-id is required` |
| `--type` | 否 | 按类型过滤（支持别名归一化，见 §3） |
| `--status` | 否 | 按状态过滤：`draft` / `submitted` / `approved` / `rejected` / `superseded` |
| `--output` | 否 | 输出格式，`table`（默认）或 `json` |

**API 映射**：`GET /api/artifacts?workflow_id=<id>&workspace_id=<ws>[&type=<t>][&status=<s>]`

响应（`--output json` 包装为 `{items, total}`；`--output table` 输出
`TITLE TYPE STATUS VERSION AUTHOR` 列）：

```json
{
  "items": [
    {
      "id": "<artifact-uuid>",
      "title": "requirement.md",
      "type": "requirements",
      "status": "submitted",
      "version": 1,
      "author_type": "agent",
      "author_id": "<agent-uuid>",
      "content_type": "markdown",
      "created_at": "...",
      "updated_at": "..."
    }
  ],
  "total": 1
}
```

## 3. Artifact 类型与别名

后端接受的 Artifact 类型枚举（`node type` 与 `artifact type` 共用）：

`requirements`、`architecture`、`development`、`testing`、`code_review`、`security`、
`documentation`、`deployment`、`other`

CLI 将用户友好拼写归一化为后端枚举（`normalizeArtifactType`），避免触发服务端
CHECK 约束：

| 别名（`--type` 输入） | 归一化结果 |
|---|---|
| `requirement` | `requirements` |
| `requirements` | `requirements` |
| `architecture` | `architecture` |
| `development` / `code` | `development` |
| `testing` / `test` / `test_report` | `testing` |
| `code_review` / `review` | `code_review` |
| `security` | `security` |
| `documentation` / `doc` | `documentation` |
| `deployment` / `deploy` | `deployment` |
| `other` | `other` |

非法类型报错：`invalid artifact type "<input>"; valid values: ...`。

## 4. 内容传输策略

`artifact submit` 依据文件扩展名与大小决定 `content` 是否内联（`artifactContentType`）：

| 扩展名 | content_type | 内联？ |
|---|---|---|
| `.md` / `.markdown` | `markdown` | 是（≤ 5 MB） |
| `.json` | `json` | 是（≤ 5 MB） |
| `.txt .text .yaml .yml .toml .ini .cfg .go .ts .tsx .js .jsx .py .java .c .cpp .h .sql .sh .bat .ps1 .html .css .xml .csv` | `text` | 是（≤ 5 MB） |
| `.pdf .zip .png .jpg .jpeg .gif .webp .svg .doc .docx .xls .xlsx .ppt .pptx` | `file` | 否（走附件上传） |
| 未知扩展名 | 含 NUL 视为 `file`，否则 `text` | 按大小阈值 |

内联阈值 `maxInlineArtifactBytes = 5 MB`；超过则回退到附件上传通道（V-03 安全审计，防
超大内联请求体 DoS）。

## 5. 后端 API 映射总表

| CLI 命令 | HTTP | 路径 | 说明 |
|---|---|---|---|
| `workflow create` | POST | `/api/workflows` | 创建 Workflow（`--issue-id` 先经 `GET /api/issues/{key\|uuid}` 解析） |
| `workflow get` | GET | `/api/workflows/{id}` | 查询 Workflow 详情（节点按阶段分组 + progress） |
| `workflow get`（附带） | GET | `/api/artifacts` | 尽力而为拉取该 Workflow 的 Artifact |
| `artifact submit` | POST | `/api/artifacts` | 提交产物（文本内联 / 附件引用） |
| `artifact submit`（附件） | POST | `/api/upload-file` | 二进制 / 超大文件先上传再引用 |
| `artifact list` | GET | `/api/artifacts` | 按 workflow / type / status 过滤查询 |

后端同期提供的其余只读 / 审核 API（CLI 未暴露，供 UI / Human 使用）：

| HTTP | 路径 | 说明 |
|---|---|---|
| GET | `/api/workflows` | Workflow 列表 |
| PUT | `/api/workflows/{id}` | 更新名称 / 描述 |
| GET | `/api/workflows/{id}/nodes` | 节点列表（可按 `stage` / `status` 过滤） |
| GET | `/api/workflows/{id}/transitions` | 状态迁移审计日志 |
| GET | `/api/artifacts/{id}` | 单个 Artifact（含 `latest_version` / `review_required`） |
| GET | `/api/artifacts/{id}/versions` | 版本列表 |
| GET | `/api/artifacts/{id}/diff?from=&to=` | 版本差异 |
| GET | `/api/artifacts/{id}/reviews` | 该 Artifact 的审核记录 |
| GET | `/api/artifacts/stats` | 审核统计 |
| POST | `/api/workflows/{id}/advance` | **Human only**：推进阶段 |
| POST | `/api/workflows/{id}/nodes/{nodeId}/status` | **Human only**：覆盖节点状态 |
| POST | `/api/artifacts/{id}/review` | **Human only**：审核（approved / rejected） |
| GET | `/api/reviews/queue` | **Human only**：人工审核队列 |

## 6. 权限与安全设计

### 6.1 权限矩阵

| 权限 | Agent | Human |
|---|---|---|
| `workflow:create` / `workflow:view` | ✅ | ✅ |
| `artifact:create` / `artifact:view` | ✅ | ✅ |
| `workflow:advance` / node status override | ❌ | ✅ |
| `artifact:review` / `approve` / `reject` | ❌ | ✅ |

### 6.2 后端门禁（机器凭证不可绕过）

- **Human-only 路由**：`POST /api/workflows/{id}/advance`、`POST /api/workflows/{id}/nodes/{nodeId}/status`、
  `POST /api/artifacts/{id}/review`、`GET /api/reviews/queue` 均挂载 `RequireHumanActor`
  中间件（handler 层另有 `requireHumanActor` 防御性复检）。`mat_` 任务令牌 / `mcn_` 云
  节点 PAT 一律 403。
- **关键节点强制审核（V-01）**：`requirements` / `architecture` / `code_review` / `testing`
  类型节点的 `review_required` 恒为 `true`，即使 owner/admin 自定义模板也不能关闭。
- **节点自定义限定（V-01）**：`POST /api/workflows` 携带 `customizations` 时，机器凭据
  一律拒绝（403），仅 workspace owner/admin 可自定义节点配置。
- **提交归属校验（M-2 / V-02）**：提交者必须是节点（或其映射 Issue）的 assignee；两者均
  无 assignee 时默认拒绝，仅 Workflow 创建者可提交。
- **内容可见性（V-04）**：`content` 字段仅在请求者为 owner/admin、作者、Workflow 创建者、
  节点 assignee、节点 Issue assignee 或审核人（来源 Issue 创建者）时返回。
- **内联大小限制（V-03）**：服务端 `MaxBytesReader` 100 MB、内联 content ≤ 5 MB，超出
  返回 413，要求走附件上传通道。
- **推进 / 覆盖权限（M-1 / H-2）**：`advance` 与节点状态覆盖仅限 Workflow 创建者 /
  来源 Issue 创建者 / workspace owner-admin。

### 6.3 状态集

- Workflow 状态：`todo`、`in_progress`、`in_review`、`blocked`、`done`、`cancelled`。
- Workflow 节点状态：`backlog`、`todo`、`in_progress`、`in_review`、`done`、`blocked`、
  `cancelled`（与 Issue 状态集一致）。
- Artifact 状态：`draft`、`submitted`、`approved`、`rejected`、`superseded`。

## 7. 错误处理

- 缺参校验（`--name` / `--issue-id` / `--workflow-id` / `--node-id` / `--type` / `--name` /
  `--file` 等）在 CLI 本地报错，不发请求。
- `--file` 传 URL、读取工作目录外路径（未开 `--allow-external-file`）时报错。
- 后端 `WorkflowError` 按 code 映射 HTTP 状态：`invalid_request` / `invalid_transition` /
  `reject_reason_required` → 400；`forbidden` → 403；`not_found` → 404；
  `duplicate_version` / `already_reviewed` / `stage_conflict` → 409；
  `not_reviewable` / `invalid_state` → 422；其余 → 500。
- `workflow get` 中 Artifact 拉取失败不导致命令失败（Artifact 列表降级为空）。
