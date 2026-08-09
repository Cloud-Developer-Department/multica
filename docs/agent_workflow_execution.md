# Agent 能力升级：Workflow 执行与项目经理 Agent 行为（CLO-175）

> 父需求：CLO-175（扩展 Multica CLI 支持 Workflow 与 Artifact 管理，实现 Agent 研发闭环）
> 文档阶段：阶段七（文档同步，CLO-181）—— 基于阶段三（CLO-178）代码开发成果同步完善。
> 配套设计文档：`docs/workflow_artifact_cli_design.md`（CLI 命令设计、参数说明、API 映射表）。

---

## 1. 背景

Multica 平台已具备完整的 Workflow / Artifact / Review 数据模型、API 与页面。本升级为
Agent 补齐两块能力：

1. **CLI 命令**：`multica workflow create / get`、`multica artifact submit / list`。
2. **Agent 执行规范**：项目经理 Agent 必须先创建 Workflow；关键阶段必须产出 Artifact；
   Artifact 必须通过 CLI 提交；关键阶段继续前必须等待人工审核。

实现闭环：**用户需求 Issue → 项目经理 Agent → 创建 Workflow → 拆解研发任务 → Agent
执行 → 提交 Artifact → 人工审核 → 继续 Workflow → 完成研发闭环**。

## 2. 可用命令（Agent 侧）

| 命令 | 说明 | API |
|---|---|---|
| `multica workflow create --name X --issue-id <issue> [--description D] [--template T]` | 创建研发流程；节点自动生成子 Issue 并激活阶段一 | `POST /api/workflows` |
| `multica workflow get <workflow-id>` | 查询状态 / 当前阶段 / 当前节点 / 当前 Agent / 当前任务 / 已提交 Artifact | `GET /api/workflows/{id}` + `GET /api/artifacts` |
| `multica artifact submit --workflow-id W --node-id N --type T --name NAME --file PATH` | 提交阶段产物；需审核节点进入 `in_review` 等待人工审核 | `POST /api/artifacts` |
| `multica artifact list --workflow-id W [--type T] [--status S]` | 查询某 Workflow 的 Artifact | `GET /api/artifacts` |

要点：

- `workflow create` 的 `--issue-id` 支持编号（`CLO-123`）或 UUID；`--template` 默认
  `software_rd`。
- `artifact submit` 的 `--node-id` **必填**，产物挂到 Workflow 的具体节点（用
  `workflow get` 返回的节点 `id`）；`--file` 默认必须在当前工作目录内（外部路径需
  `--allow-external-file`）。
- Artifact 类型（`--type`，含别名归一化）：`requirements`（requirement）、`architecture`、
  `development`（code）、`testing`（test / test_report）、`code_review`（review）、
  `security`、`documentation`（doc）、`deployment`（deploy）、`other`。
- Artifact 状态：`draft` / `submitted` / `approved` / `rejected` / `superseded`。
- Workflow 状态：`todo` / `in_progress` / `in_review` / `blocked` / `done` / `cancelled`。

## 3. 内置模板与人工审核节点（software_rd）

`workflow create` 默认实例化 `software_rd` 模板，共 5 个阶段。**需人工审核的节点**
（`review_required=true`）为：

| 阶段 | 节点类型 | 节点名称 | 人工审核 |
|---|---|---|---|
| 1 需求分析 | `requirements` | 需求分析 | ✅ 必须 |
| 2 架构设计 | `architecture` | 架构设计 | ✅ 必须 |
| 3 开发实现 | `development` | 开发实现 | ❌ |
| 3 开发实现 | `code_review` | 代码审查 | ✅ 必须 |
| 4 测试验证 | `testing` | 测试验证 | ✅ 必须 |
| 5 部署交付 | `security` | 安全审计 | ❌ |
| 5 部署交付 | `deployment` | 部署交付 | ❌ |

安全约束（V-01 安全审计）：`requirements` / `architecture` / `code_review` / `testing`
为**关键审核节点**，其 `review_required` 恒为 `true`，即使 owner/admin 自定义模板也不可
关闭。节点分配后，每个节点对应一个子 Issue，走现有 Issue → agent_task_queue → daemon
触发链执行。

## 4. 权限边界（Human Only，Agent 禁止）

Agent **不得**执行、CLI 也**不提供**以下命令：

- `workflow advance` / `workflow status override`
- `artifact approve` / `artifact reject` / `artifact review`

权限矩阵：

| 权限 | Agent | Human |
|---|---|---|
| `workflow:create` / `workflow:view` | ✅ | ✅ |
| `artifact:create` / `artifact:view` | ✅ | ✅ |
| `workflow:advance` / node status override | ❌ | ✅ |
| `artifact:review` / `approve` / `reject` | ❌ | ✅ |

即使 Agent 用 `curl` / `wget` 绕过 CLI 直连 API，后端
`POST /api/workflows/{id}/advance`、`POST /api/workflows/{id}/nodes/{nodeId}/status`、
`POST /api/artifacts/{id}/review`、`GET /api/reviews/queue` 路由均挂载
`RequireHumanActor` 中间件，`mat_` 任务令牌 / `mcn_` 云节点 PAT 会被拒绝（403）。
人工审核不可能被机器凭证绕过。

## 5. Workflow Execution Rules（写入 Agent Runtime Brief）

对所有带 Issue 上下文的 Agent 任务（assignment / comment）生效：

1. Project Manager Agent MUST create a Workflow first when the request is a software R&D task。
2. Every major development phase MUST generate an Artifact。
3. Artifact MUST be submitted through the CLI: `multica artifact submit ...`。
4. Track workflow status with `multica workflow get <id> --output json`。
5. Human approval is REQUIRED before continuing past critical phases; do not advance /
   override status / approve / reject — those are human-only actions。
6. NEVER use curl / wget or direct API calls; NEVER bypass the human review loop。

## 6. 项目经理 Agent 行为升级

升级后行为链：

```
Issue
  → 判断是否为软件研发任务
  → 是：multica workflow create --name "<名称>" --issue-id <issue-id> --output json
  → multica workflow get <id> 确认阶段一节点与 assignee
  → 通过节点子 Issue 拆解任务、分配执行 Agent（沿用 issue assign / 子任务机制）
  → 每个阶段完成后：multica artifact submit --workflow-id <id> --node-id <node> \
      --type <type> --name <file> --file <path>
  → 跟踪 Artifact：multica artifact list --workflow-id <id>
  → 等待人工审核（节点 in_review 期间不推进）
  → 审核通过后推进下一阶段 / 派发下一节点
  → 全部完成 → 生成最终报告
```

边界：

- 项目经理 Agent **不得**执行 advance / approve / reject（CLI 无命令 + 后端门禁兜底）。
- 「推进阶段」由 Human 在审核通过后通过 UI / API 完成；PM Agent 需要触达人类时使用
  评论 / 提醒，而非状态覆盖。

## 7. Artifact 提交规则

1. **提交者归属**：只能提交自己所属节点（或其映射 Issue）的 Artifact；节点与 Issue 均无
   assignee 时，仅 Workflow 创建者可提交（V-02 安全审计）。提交者不是节点 assignee 会收到
   `forbidden`。
2. **节点状态约束**：仅 `in_progress` 或 `todo` 节点可接收提交；`backlog` / `done` /
   `cancelled` / `in_review` 节点提交被拒绝（`invalid_state`）。
3. **版本机制**：同一 (节点, 类型) 的每次提交自动 +1 版本，旧版本置为 `superseded`。
4. **内容通道**：文本类（≤ 5 MB）内联 `content`；二进制 / 超大文件走附件上传
   （`file_attachment_id`）。内联超过限制返回 413。
5. **审核触发**：提交到 `review_required=true` 的节点后，节点与映射子 Issue 进入
   `in_review`，并通知审核人。

## 8. 人工审核节点（Review 流程）

- **入口**：审核队列 `GET /api/reviews/queue`（Human only）或各 Artifact 页面；审核动作
  通过 `POST /api/artifacts/{id}/review`（Human only，action=`approved` / `rejected`）。
- **审核人**：来源 Issue 创建者，或 workspace owner/admin 覆盖；其他成员不可审核。
- **通过（approved）**：Artifact → `approved`；节点 → `done`；映射子 Issue → `done`。
  当该阶段所有节点终态时，Workflow 自动推进下一阶段（`current_stage+1`），下一阶段节点
  `backlog → todo` 并触发对应 Agent。
- **打回（rejected）**：必须填写原因（`comment`）；Artifact → `rejected`；节点与子 Issue
  → `todo`，并重新触发负责 Agent 产出新版本。
- **不可重复审核**：对已审核的 Artifact 再次审核返回 `already_reviewed`（409）。

## 9. 交付物与验收

- CLI 能力：`workflow create` / `workflow get` / `artifact submit` / `artifact list`。
- Agent 能力：PM Agent 可创建 Workflow；Agent 可提交 Artifact；Artifact 可进入审核队列。
- 安全能力：Agent 不能自动审核；Agent 不能绕过 CLI 调用 API；Human Review 机制保持有效。
- 产品闭环：Issue → Project Manager Agent → Workflow → Agent 协作 → Artifact → Human
  Review → Workflow 继续执行 → 研发完成。
