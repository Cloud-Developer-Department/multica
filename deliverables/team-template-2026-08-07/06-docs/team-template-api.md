# 团队模板 API 文档

团队模板把一整套组织（多个 agent + 多个小队 + 共享 skill）打包成静态 JSON，通过 3 个 REST 端点一键浏览与应用。模板以嵌入式静态文件随服务发布（`server/internal/teamtmpl/templates/<slug>.json`），变更走 PR 评审，无运行时编辑。

## 认证与授权

所有端点要求已登录且属于目标 workspace 的成员（Bearer token + workspace 上下文）。`apply` 额外校验 `runtime_id` 归属：私有 runtime 仅其 owner 或 workspace admin 可用，否则 `403`。

## 端点

### 1. 模板列表

`GET /api/team-templates`

返回所有模板摘要（不含 instructions/content 正文，payload 永为数组，空目录为 `[]`）。

响应 `200`:

```json
[
  {
    "slug": "equipment-department-1-3-9",
    "name": "装备部 1+3+9 组织",
    "description": "1 调度官 + 3 专业小队 + 9 角色 + 1 共享协作 Skill",
    "category": "Engineering",
    "icon": "Network",
    "accent": "primary",
    "skills": [{"name": "multica-team-workflow", "description": "全团队共享协作规范…", "source_url": ""}],
    "agents": [{"name": "pmo-agent", "model": "deepseek/deepseek-v4-flash", "skills": ["multica-team-workflow"]}],
    "squads": [{"name": "squad-product-design", "leader": "pm-agent", "member_count": 3}]
  }
]
```

### 2. 模板详情

`GET /api/team-templates/{slug}`

返回模板完整结构：skills（含 content/source_url/files）、agents（含完整 instructions）、squads（含完整 instructions 与 members）。数组永非 null（空为 `[]`），可选字段缺省时省略。

响应 `200` 为 `TeamTemplate` 全量对象（见下文"模板格式"）。未知 slug → `404 {"error":"template not found"}`。

### 3. 一键应用

`POST /api/team-templates/{slug}/apply`

单事务原子创建全部 skills → agents → squads。**任一步失败整体回滚，零残留**。重复 apply 幂等（按 `workspace + name` find-or-create），已存在资源返回 `reused`，不覆盖、不产生重复资源。

请求体：

```json
{
  "runtime_id": "5b41b070-ba44-40df-bf6d-bb7cab16cca2",
  "model_overrides": { "architect-agent": "huaweicloud/glm-5.2" },
  "visibility": "private",
  "permission_mode": null
}
```

| 字段 | 必填 | 说明 |
|------|------|------|
| `runtime_id` | ✅ | 所有新建 agent 绑定的共享 runtime UUID |
| `model_overrides` | ❌ | agent name → model 映射，只能引用模板内已声明的 agent；缺省用模板默认 model |
| `visibility` | ❌ | 未在模板内声明 visibility 的 agent 的默认可见性（默认 `private`） |
| `permission_mode` | ❌ | 显式时具权威性（同 CreateAgentFromTemplate 语义） |

响应 `201`：

```json
{
  "template_slug": "equipment-department-1-3-9",
  "skills": { "created": [{"name": "multica-team-workflow", "id": "…"}], "reused": [] },
  "agents": {
    "created": [{"name": "pmo-agent", "id": "…"}, {"name": "ba-agent", "id": "…"}],
    "reused": []
  },
  "squads": { "created": [{"name": "squad-product-design", "id": "…"}], "reused": [] }
}
```

## 错误码

| 状态码 | 场景 |
|--------|------|
| `400` | 请求体非法 / 缺 `runtime_id` / `model_overrides` 引用未知 agent |
| `403` | 私有 runtime 非 owner/admin |
| `404` | 模板 slug 不存在 |
| `422` | 任一远程 skill source fetch 失败，返回 `{"error":"one or more skill sources are unavailable","failed_urls":[…]}`，零写入 |
| `500` | 事务内任一步失败 / commit 失败（已回滚，无残留） |

## 安全约束

- 远程 skill `source_url` 仅允许 clawhub.ai / skills.sh / github.com，scheme 仅 http/https。
- skill 附属文件路径拒绝绝对路径与 `..` 穿越。
- 模板内容随版本发布、boot 校验失败即 panic，杜绝运行时模板注入。

## 使用示例（multica CLI / curl）

```bash
# 列表
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/team-templates

# 详情
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/team-templates/equipment-department-1-3-9

# 应用（幂等）
curl -X POST http://localhost:8080/api/team-templates/equipment-department-1-3-9/apply \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"runtime_id":"<uuid>"}'
```
