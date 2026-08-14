# 反馈中心 — API 文档

> 对应实现：`server/internal/handler/feedback_center.go`、`server/internal/handler/feedback.go`、`server/cmd/server/router.go`、`server/pkg/db/queries/feedback.sql`。
> 文档版本：v1.0 ｜ 日期：2026-08-13

## 1. 总览

反馈中心 API 为 **workspace 级**接口，整体挂载在 `RequireWorkspaceMember` 分组下（与 `/api/issues`、`/api/projects` 同级）。
权限模型为 **Public Read + Authenticated Write**：

- 读接口（列表 / 详情 / 评论）：任何**登录且为该 workspace 成员**的用户可访问；
- 写接口（创建 / 点赞 / 取消点赞 / 发评论）：在 handler 内 `requireUserID` 强制登录，未登录返回 401；
- `creator_id` / `workspace_id` 一律由服务端从登录态与 Workspace 上下文解析，**不接受**请求体传入；
- 不引入任何 Admin / RBAC / 审核角色。

| 方法 | 路径 | 说明 | 需登录 |
|---|---|---|---|
| GET | `/api/feedbacks` | 反馈列表（type/keyword/sort/page/page_size） | 读 |
| GET | `/api/feedbacks/{id}` | 反馈详情（含 vote_count/comment_count/my_vote） | 读 |
| POST | `/api/feedbacks` | 创建反馈（type/title/description） | ✅ |
| POST | `/api/feedbacks/{id}/vote` | 点赞（幂等） | ✅ |
| DELETE | `/api/feedbacks/{id}/vote` | 取消点赞（幂等） | ✅ |
| GET | `/api/feedbacks/{id}/comments` | 评论列表 | 读 |
| POST | `/api/feedbacks/{id}/comments` | 发表评论（content） | ✅ |
| POST | `/api/feedback`（旧，保留） | 旧 message-only 提交（桌面端错误上报链路） | ✅ |

### 认证与鉴权头

- 复用现有 `ApiClient`：自动携带 Authorization（JWT/PAT）、`X-Workspace-Slug`、CSRF、客户端标识。
- Workspace 解析：中间件按 `X-Workspace-Slug`（首选）/ `X-Workspace-ID` / `?workspace_id` 解析，并校验成员身份。
- 错误返回统一为 `{"error":"<message>"}`（`writeError`）。

## 2. 对象定义

### FeedbackSummary / Feedback（列表项与详情共用同一结构）

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "creator_id": "uuid",
  "creator_name": "cy",
  "creator_avatar_url": "https://.../avatar.png",
  "title": "Agent 批量创建",
  "description": "希望 Agent 创建能够支持批量创建……",
  "type": "feature",
  "vote_count": 32,
  "comment_count": 8,
  "my_vote": false,
  "created_at": "2026-08-13T08:00:00Z",
  "updated_at": "2026-08-13T08:00:00Z"
}
```

- `type`：`bug` / `feature` / `improvement` / `other`
- `my_vote`：当前登录用户是否已点赞；未登录恒为 `false`。
- 响应**不返回** `metadata` JSONB（其中可能含客户端环境/堆栈上下文，仅入库用于埋点）。

### FeedbackComment

```json
{
  "id": "uuid",
  "feedback_id": "uuid",
  "user_id": "uuid",
  "user_name": "cy",
  "user_avatar_url": "https://...",
  "content": "这个功能确实比较需要。",
  "created_at": "2026-08-13T08:00:00Z",
  "updated_at": "2026-08-13T08:00:00Z"
}
```

## 3. GET `/api/feedbacks` — 反馈列表

### 请求参数

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `type` | string | 空 = 全部 | `bug` / `feature` / `improvement` / `other`；非法值 → 400 |
| `keyword` | string | 空 | 对 `title` + `description` 做 `ILIKE '%keyword%'` 模糊匹配 |
| `sort` | string | `latest` | `latest` / `hot` / `comments`；非法值 → 400 |
| `page` | int | 1 | 从 1 起 |
| `page_size` | int | 20 | 上限 100，超限自动截断为 100 |

### 响应 200

```json
{
  "items": [ /* FeedbackSummary[] */ ],
  "total": 123,
  "page": 1,
  "page_size": 20,
  "has_more": true
}
```

### 错误

| 状态码 | 场景 |
|---|---|
| 400 | 非法 `type` / `sort` |
| 401 | 未登录（workspace 成员校验前置） |

## 4. GET `/api/feedbacks/{id}` — 反馈详情

### 响应

- `200`：`FeedbackSummary`（含 `vote_count`、`comment_count`、`my_vote`）。
- `400`：`{id}` 非法 UUID → `{"error":"invalid feedback id"}`。
- `404`：反馈不存在，或不属于当前 workspace（`feedback not found`）。
- `401`：未登录。

## 5. POST `/api/feedbacks` — 创建反馈

### 请求体

```json
{
  "type": "feature",
  "title": "Agent 批量创建",
  "description": "希望 Agent 创建能够支持批量创建……"
}
```

> `creator_id` / `workspace_id` 不在此请求体中——分别由服务端从登录态与 workspace 上下文解析。

### 校验规则（后端强制）

| 规则 | 错误响应 |
|---|---|
| `type` 不在 `bug/feature/improvement/other` | 400 `{"error":"invalid feedback type"}` |
| `title` 为空 | 400 `{"error":"title is required"}` |
| `title` > 200 字符（按 rune 计数） | 400 `{"error":"title too long"}` |
| `description` 为空 | 400 `{"error":"description is required"}` |
| `description` > 10000 字符 | 400 `{"error":"description too long"}` |
| 请求体 > 64 KiB | 400 `{"error":"invalid request body"}` |
| 未登录 | 401 `{"error":"user not authenticated"}` |
| 提交频率超限（10 条/小时/用户） | 429 `{"error":"too many feedback submissions, please try again later"}` |

### 响应

- `201`：`FeedbackSummary`（新反馈，`vote_count=0`、`comment_count=0`、`my_vote=false`）。

## 6. POST `/api/feedbacks/{id}/vote` — 点赞

- **登录必须**（未登录 → 401）。
- **幂等**：同一用户对同一反馈重复点赞为 no-op（`UNIQUE(feedback_id,user_id)` + `ON CONFLICT DO NOTHING`），不产生第二条 Vote。
- `{id}` 非法 → 400；反馈不存在或跨 workspace → 404。
- 响应 `200`：

```json
{ "voted": true, "vote_count": 32 }
```

## 7. DELETE `/api/feedbacks/{id}/vote` — 取消点赞

- **登录必须**（未登录 → 401）。
- **幂等**：取消一个不存在的 Vote 为 no-op，不报错。
- `{id}` 非法 → 400；反馈不存在或跨 workspace → 404。
- 响应 `200`：

```json
{ "voted": false, "vote_count": 31 }
```

## 8. GET `/api/feedbacks/{id}/comments` — 评论列表

- 按 `created_at ASC, id ASC` 时间正序返回。
- `{id}` 非法 → 400；反馈不存在或跨 workspace → 404。
- 响应 `200`：

```json
{
  "items": [ /* FeedbackComment[] */ ],
  "total": 8
}
```

## 9. POST `/api/feedbacks/{id}/comments` — 发表评论

### 请求体

```json
{ "content": "这个功能确实比较需要。" }
```

### 校验规则

| 规则 | 错误响应 |
|---|---|
| 未登录 | 401 `{"error":"user not authenticated"}` |
| `content` 为空 | 400 `{"error":"comment content is required"}` |
| `content` > 5000 字符 | 400 `{"error":"comment too long"}` |
| `{id}` 非法 | 400 |
| 反馈不存在 / 跨 workspace | 404 |

### 响应

- `201`：`FeedbackComment`（含作者 `user_name` / `user_avatar_url`）。

## 10. 旧接口 `POST /api/feedback`（保留，兼容桌面端）

用户级接口（不在 workspace 分组），桌面端路由错误上报链路依赖，**保持不变**。

### 请求体

```json
{
  "message": "……",
  "url": "https://...",
  "kind": "bug",
  "workspace_id": "uuid(可选)",
  "context": { "kind": "desktop_route_error", "trigger": "…", "error": { "name": "…", "message": "…", "stack": "…" } }
}
```

- 写库时 `title` 由 message 截断派生（rune 感知，≤80 字符），`type` 由 `kind` 映射
  （`bug→bug`、`feature→feature`、`improvement→improvement`、`general/praise/未知→other`）。
- 响应保持 `201 {"id":"uuid","created_at":"…"}`。
- 同样受登录校验与 10 条/小时/用户限流约束。

## 11. 前端数据层（API Client）

前端 `ApiClient` 已提供对应方法（`packages/core/api/client.ts`）：

| 方法 | 对应接口 |
|---|---|
| `listFeedbacks(params)` | GET `/api/feedbacks` |
| `getFeedback(id)` | GET `/api/feedbacks/{id}` |
| `createFeedbackCenter({type,title,description})` | POST `/api/feedbacks` |
| `addFeedbackVote(feedbackId)` | POST `/api/feedbacks/{id}/vote` |
| `removeFeedbackVote(feedbackId)` | DELETE `/api/feedbacks/{id}/vote` |
| `listFeedbackComments(feedbackId)` | GET `/api/feedbacks/{id}/comments` |
| `createFeedbackComment(feedbackId, content)` | POST `/api/feedbacks/{id}/comments` |

所有响应经 zod schema（`.loose()` + EMPTY fallback）校验，前后端契约对齐。
