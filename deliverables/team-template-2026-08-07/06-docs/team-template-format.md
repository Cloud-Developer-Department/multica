# 团队模板格式说明（如何打包一个新组织模板）

团队模板把一套组织形态（skill + agent + squad）打包为单个静态 JSON，随服务发布在 `server/internal/teamtmpl/templates/<slug>.json`。任意组织形态都可做成模板复用。

## 快速上手

1. 新建 `server/internal/teamtmpl/templates/<slug>.json`（slug 必须等于文件名）。
2. 按下方结构填写三个区块：`skills[]`、`agents[]`、`squads[]`。
3. 本地验证：`go test ./internal/teamtmpl/...`（boot 校验等价，10 条规则全过才可提交）。
4. 走 PR 评审合入（模板随版本发布，无运行时编辑）。

## 顶层结构

```json
{
  "slug": "my-org-template",
  "name": "组织模板名",
  "description": "一行简介",
  "category": "Engineering",
  "icon": "Network",
  "accent": "primary",
  "skills": [ /* SkillDef[] */ ],
  "agents":  [ /* AgentDef[] */ ],
  "squads":  [ /* SquadDef[] */ ]
}
```

- `slug`：小写 kebab-case（`a-z 0-9 -`），必须与文件名一致，是 URL 路由键。
- `category` / `icon` / `accent` 可省略（`accent` 需用设计系统 token 名，如 `primary`，禁用硬编码颜色）。

## skills[]（SkillDef）

每个 skill 是"内嵌正文"或"远程导入"二选一（**互斥，loader 强制**）：

```json
{ "name": "shared-workflow", "description": "…", "content": "# SKILL.md 正文…" }
```

```json
{ "name": "review-helper", "description": "…", "source_url": "https://clawhub.ai/acme/review-helper" }
```

- `name`：模板内唯一；也是 apply 时的 workspace 级幂等键（同 workspace 同名 skill 被复用）。
- `content`：SKILL.md 全文内嵌，apply 时直接建。
- `source_url`：仅支持 clawhub.ai / skills.sh / github.com，apply 时走 ImportSkill fetch 链路，失败整体 422 回滚。
- `files`（可选）：内嵌 skill 的附属文件，`[{ "path": "sub/skill.md", "content": "…" }]`；路径禁止绝对路径与 `..`。

## agents[]（AgentDef）

```json
{
  "name": "pm-agent",
  "description": "一行简介",
  "model": "deepseek/deepseek-v4-flash",
  "instructions": "# 角色\n完整 prompt 正文…",
  "skills": ["shared-workflow"],
  "visibility": "private",
  "max_concurrent_tasks": 6
}
```

- `name`：模板内唯一 + workspace 级幂等键。
- `model`：模板默认 model；请求可 `model_overrides` 覆盖；缺省平台默认。
- `instructions`：**必填非空**（loader 校验），写入 `agent.instructions` 列。
- `skills`：按 name 引用 `skills[]`（引用必须存在）。
- `visibility`：`workspace` / `private`，缺省 private。
- `max_concurrent_tasks`：并行任务上限，缺省 6。

## squads[]（SquadDef）

```json
{
  "name": "squad-product-design",
  "description": "…",
  "instructions": "小队协同指令全文…",
  "leader": "pm-agent",
  "members": [ { "agent_name": "ba-agent", "role": "member" } ]
}
```

- `name`：模板内唯一 + workspace 级幂等键。
- `leader`：`agents[]` 中的 agent name（**必须存在**）；leader 自动加入 squad（role=leader），不要再写进 members。
- `members`：额外成员，`agent_name` 必须存在于 `agents[]`；`role` 自由文本（如 `member`）。
- `instructions`：小队指令（可选），apply 时写入 `squad.instructions`。

## loader 强制校验（boot 失败即 panic）

1. slug 非空、kebab-case、与文件名一致
2. name 非空
3. skills/agents/squads 内部 name 各自唯一
4. 每个 skill 恰好有 content 或 source_url 之一
5. agent 的 skill 引用必须存在于 skills[]
6. agent instructions 非空
7. squad leader 与每个 member 必须存在于 agents[]

## apply 语义回顾（打包者须知）

- 幂等键 = `workspace + 资源类型 + name`：重复 apply 已存在的 skill/agent/squad 一律 `reused`，**不覆盖**（同名不同内容也复用 + warning）。
- 单事务 skill→agent→squad 顺序创建，任一步失败整体回滚零残留；远程 skill 在事务外预 fetch。
- 同名资源被复用意味着内容更新需改模板版本并走 PR，而不是直接覆盖线上资源。
