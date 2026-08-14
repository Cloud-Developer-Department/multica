# 反馈中心 — 项目架构说明

> 对应实现：反馈中心 v1.0（2026-08-13），后端 `19b3adae4` + 前端 `7da924f0c`（基线 `e75b617c7`）。
> 本文面向开发人员，说明反馈中心在 Multica 现有架构中的位置、模块划分、数据流与关键实现。

## 1. 总体方案

在**不引入管理员 / 审核 / 独立 RBAC**、**最大限度复用现有体系**的前提下，将原先隐藏在底部帮助菜单中的「反馈」入口升级为
左侧一级导航「反馈中心」：**Public Read + Authenticated Write** 的 workspace 级公共反馈池。

设计原则：

1. **复用优先，禁止重建第二套**：复用现有 `feedback` 表、`POST /api/feedback` 提交链路（桌面端错误上报依赖）、
   `FeedbackModal`、`HelpLauncher`、左侧导航框架、UI 组件库、Auth / Workspace 中间件、sqlc ORM。
2. **不过度设计**：不做状态流转、公开/私有、Feedback→Issue/Workflow 联动、评论嵌套、通知、AI 分类、全文搜索；数据结构保持可扩展。
3. **权限最简单化**：只区分未登录 / 登录；写操作后端 `requireUserID` 强制校验；`creator_id`/`workspace_id` 服务端解析。

## 2. 分层架构

```
┌────────────────────────────── apps/web ──────────────────────────────┐
│  apps/web/app/[workspaceSlug]/(dashboard)/feedback/page.tsx          │
│  apps/web/app/[workspaceSlug]/(dashboard)/feedback/[id]/page.tsx     │
└────────────────────────────────┬─────────────────────────────────────┘
                                 │ (re-export 页面组件)
┌──────────────────────────── packages/views ──────────────────────────┐
│  layout/app-sidebar.tsx（一级导航）  layout/help-launcher.tsx（底部入口）│
│  layout/collection-page.tsx（列表模板） layout/route-icon-components    │
│  feedback/（列表页/详情页/卡片/提交弹窗/点赞/评论 组件）                 │
│  i18n/useT + locales（feedback namespace）                            │
└────────────────────────────┬──────────────────────────────────────────┘
                             │ (@multica/core)
┌──────────────────────────── packages/core ───────────────────────────┐
│  paths/paths.ts（feedback 路由）  paths/route-icons.ts（导航图标）     │
│  feedback/（types + queries + mutations）                            │
│  api/client.ts + api/schemas.ts（zod）                               │
└────────────────────────────┬──────────────────────────────────────────┘
                             │ HTTP /api/feedbacks*  (X-Workspace-Slug)
┌──────────────────────────── server ──────────────────────────────────┐
│  cmd/server/router.go（chi 路由：Workspace 级分组）                    │
│  internal/handler/feedback.go（旧提交兼容，保留）                     │
│  internal/handler/feedback_center.go（列表/详情/提交/点赞/评论）       │
│  internal/middleware/（Auth、RequireWorkspaceMember）                 │
│  pkg/db/queries/feedback.sql（sqlc）→ pkg/db/generated/               │
│  migrations/285/286/287                                              │
└────────────────────────────┬──────────────────────────────────────────┘
                             ▼ PostgreSQL
```

## 3. 后端模块

### 3.1 Router（`server/cmd/server/router.go:1348`）

反馈中心 API 挂载在 **Workspace 级分组**（`RequireWorkspaceMember`）下，与 `/api/issues`、`/api/projects` 同级：

```go
r.Route("/api/feedbacks", func(r chi.Router) {
    r.Get("/", h.ListFeedbacks)
    r.Post("/", h.CreateFeedbackCenter)
    r.Route("/{id}", func(r chi.Router) {
        r.Get("/", h.GetFeedback)
        r.Post("/vote", h.CreateFeedbackVote)
        r.Delete("/vote", h.DeleteFeedbackVote)
        r.Get("/comments", h.ListFeedbackComments)
        r.Post("/comments", h.CreateFeedbackComment)
    })
})
```

写接口在 handler 内用 `requireUserID` 强制登录；读接口跟随 Workspace 中间件（登录成员即可读）。

### 3.2 Handler（`server/internal/handler/feedback_center.go`）

| Handler | 说明 |
|---|---|
| `ListFeedbacks` | 列表：type/keyword/sort/page/page_size；聚合 vote/comment 计数 + `my_vote`；返回 `{items,total,page,page_size,has_more}` |
| `GetFeedback` | 详情：含聚合计数与 `my_vote`；404 兜底 |
| `CreateFeedbackCenter` | 创建（登录）：type 白名单、title≤200、description≤10000（均按 rune 计数）、复用 10 条/小时/用户限流、写入 metadata 埋点 |
| `CreateFeedbackVote` | 点赞（登录）：`feedbackInWorkspace` 拦截 + `ON CONFLICT DO NOTHING` 幂等 |
| `DeleteFeedbackVote` | 取消点赞（登录）：幂等 |
| `ListFeedbackComments` | 评论列表：时间正序 |
| `CreateFeedbackComment` | 发评论（登录）：content≤5000 |

关键点：

- `feedbackInWorkspace(w, r, feedbackID, wsUUID)`：点赞/评论前校验反馈属于当前 workspace，跨 workspace id 一律 404；
- `viewerUUID(r)`：读接口以当前用户计算 `my_vote`，未登录时用零 UUID（恒为 `false`），符合 Public Read；
- 错误统一 `{"error":"<message>"}`；`slog` 记录详细错误，不随响应外发。

### 3.3 旧接口兼容（`server/internal/handler/feedback.go`）

`POST /api/feedback`（用户级）**保留不动**，桌面端路由错误上报链路依赖。写库时：

- `title` 由 message rune 感知截断派生（≤80 字符，避免非法 UTF-8）；
- `type` 由 `kind` 映射（`bug/feature/improvement` 直通，`general/praise/未知→other`）；
- 响应保持 `{id, created_at}`。

新反馈中心提交走 `POST /api/feedbacks`，双轨并行。

### 3.4 查询层（`server/pkg/db/queries/feedback.sql`）

sqlc 查询（改 `.sql` 后运行 `cd server && sqlc generate`，勿手改 `generated/`）：

- `CreateFeedback`（统一供新旧两条链路使用，handler 层映射字段）
- `ListFeedbacks` / `CountFeedbacks`（列表 + 总数，条件拼接 type/keyword，`CASE` 驱动 sort）
- `GetFeedback`（详情 + 聚合计数 + my_vote，scoped by workspace）
- `GetFeedbackInWorkspace`（存在性/归属校验）
- `CreateFeedbackVote` / `DeleteFeedbackVote` / `CountFeedbackVotes`（幂等）
- `ListFeedbackComments` / `CountFeedbackComments` / `CreateFeedbackComment`

搜索：`title ILIKE '%' || keyword || '%' OR description ILIKE '%' || keyword || '%'`（参数化，无注入面）。
计数：一期聚合查询（`COUNT(DISTINCT ...)` / 子查询），不设冗余列。

### 3.5 指标与埋点

- `multica_feedback_submitted_total{kind,platform}`：`knownFeedbackKinds` allow-list 新增 `improvement`（`labels_pr3.go`）；
- analytics 事件 `feedback_submitted`：新中心提交复用该事件（kind=type），不新增事件。

## 4. 前端模块

### 4.1 路由与入口

| 路由 | 页面文件（web） | 组件（views） |
|---|---|---|
| `/{slug}/feedback` | `apps/web/app/[workspaceSlug]/(dashboard)/feedback/page.tsx` | `FeedbackCenterPage` |
| `/{slug}/feedback/{id}` | `apps/web/app/[workspaceSlug]/(dashboard)/feedback/[id]/page.tsx` | `FeedbackDetailPage` |

- 桌面端 `apps/desktop/src/renderer/src/routes.tsx` 同步注册 `feedback` 与 `feedback/:id`；
- 路径统一由 `workspaceScoped().feedback()` / `feedbackDetail(id)` 构建（`packages/core/paths/paths.ts`，禁止硬编码）；
- 导航图标 `MessageCircle` 注册于 `route-icons.ts`（`WORKSPACE_PAGES.feedback`）与 `route-icon-components.tsx`（`ROUTE_ICON_COMPONENTS`）。

### 4.2 左侧导航与底部入口

- `packages/views/layout/app-sidebar.tsx`：`workspaceNav` 新增 `{key:"feedback", labelKey:"feedback"}`（与 Issues/项目/自动化/智能体/小队同级）；
- `packages/views/layout/help-launcher.tsx`：底部「反馈」由 `useModalStore.open("feedback")` 改为 `navigation.push(workspaceScoped(slug).feedback())`（无 workspace 时兜底旧弹窗，不破坏桌面错误上报）。

### 4.3 页面与组件（`packages/views/feedback/`）

| 组件 | 职责 |
|---|---|
| `feedback-center-page.tsx` | 列表页：类型筛选（按钮组）、搜索框（300ms 防抖）、排序 DropdownMenu、`useInfiniteQuery` + `InfiniteScrollSentinel` 加载更多、Skeleton/Empty/Error 状态 |
| `feedback-card.tsx` | 卡片：点赞数、标题、描述摘要（≤140 字符）、类型 Badge、创建者、时间、评论数，整卡跳详情 |
| `feedback-detail-page.tsx` | 详情：返回链接、点赞按钮、类型/创建者/时间、完整描述、评论；404/错误态 |
| `feedback-vote-button.tsx` | 点赞 toggle：乐观更新 + 失败回滚 + Toast；成功后 invalidate 列表/详情缓存 |
| `feedback-comments.tsx` | 评论列表 + 输入区；未登录不渲染输入区；Ctrl/⌘+Enter 发送 |
| `feedback-submit-dialog.tsx` | 提交 Modal：类型 + 标题 + 描述，前端 `canSubmit` 校验，成功 Toast + invalidate 刷新 |
| `feedback-types.tsx` | 类型标签 Badge 与本地化文案 |

### 4.4 数据层（`packages/core/feedback/`）

- `types.ts`：`FEEDBACK_TYPES`、`FeedbackType`、`FeedbackSummary`、`Feedback = FeedbackSummary`、`FeedbackComment`、`ListFeedbacksResponse` 等；保留旧 `FeedbackKind`/`FeedbackContext`（旧链路）；
- `queries.ts`：`feedbackKeys`（`["feedback", wsId, "list"/"detail"/"comments"]`）+ 三个 queryOptions；
- `mutations.ts`：`useCreateCenterFeedback`、`useAddFeedbackVote`、`useRemoveFeedbackVote`、`useCreateFeedbackComment`；保留 `useCreateFeedback`（旧链路）；
- `api/client.ts` + `schemas.ts`：7 个新方法 + zod schema（`.loose()` + EMPTY fallback）。

## 5. 数据库设计

### 5.1 `feedback`（改造，migration 285）

原有：`id, user_id, workspace_id(nullable), message, metadata JSONB, created_at`

改造后：`id, creator_id, workspace_id(nullable), title, description, type, metadata JSONB, created_at, updated_at`

- `creator_id`：由 `user_id` 重命名，`NOT NULL REFERENCES "user"(id) ON DELETE CASCADE`；
- `description`：由 `message` 重命名；
- `title`：新增，`NOT NULL DEFAULT ''`（存量回填：`LEFT(description,80)` 或 `(no title)`）；
- `type`：新增，`NOT NULL DEFAULT 'other' CHECK (type IN ('bug','feature','improvement','other'))`；
- `updated_at`：新增，`NOT NULL DEFAULT now()`；
- 索引：新增 `idx_feedback_workspace_created ON feedback(workspace_id, created_at DESC)`。

### 5.2 `feedback_vote`（新建，migration 286）

```sql
CREATE TABLE feedback_vote (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    feedback_id UUID NOT NULL REFERENCES feedback(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (feedback_id, user_id)   -- 硬约束：同用户同反馈最多一条
);
CREATE INDEX idx_feedback_vote_feedback_id ON feedback_vote(feedback_id);
```

### 5.3 `feedback_comment`（新建，migration 287）

```sql
CREATE TABLE feedback_comment (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    feedback_id UUID NOT NULL REFERENCES feedback(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_feedback_comment_feedback_id_created ON feedback_comment(feedback_id, created_at);
```

### 5.4 关系

```
workspace 1───* feedback 1───* feedback_vote
                        │
                        └───* feedback_comment *───1 user
```

### 5.5 数据删除语义

- 删除 workspace：`feedback.workspace_id` 被 SET NULL 脱离（保留既有行为）；
- `feedback_vote` / `feedback_comment` 无 `workspace_id` 列，在 workspace 删除清单中按 KEEP 保留（`workspaceDeleteKeep` manifest 已更新）。

## 6. 核心数据流

```
浏览：  GET /api/feedbacks?type&keyword&sort&page&page_size
        → RequireWorkspaceMember 解析 workspace_id → 聚合查询（vote/comment 计数）
        → FeedbackSummary[] + total
详情：  GET /api/feedbacks/{id} → 详情 + vote_count/comment_count/my_vote
提交：  POST /api/feedbacks（登录）→ creator_id=session，workspace_id=context
点赞：  POST / DELETE /api/feedbacks/{id}/vote（登录）→ UNIQUE(feedback_id,user_id)
评论：  GET / POST /api/feedbacks/{id}/comments（POST 需登录）
```

## 7. 权限与安全

- **Public Read + Authenticated Write**：读挂 Workspace 中间件（登录成员可读），写叠加 `requireUserID`；
- **无新增角色**：复用 `RequireWorkspaceMember`，全量 diff 无 admin/role/permission 类新增；
- **防越权**：点赞/评论/详情经 `feedbackInWorkspace` 校验，跨 workspace id 一律 404；
- **防注入**：SQL 全参数化（sqlc），ILIKE 模式参数绑定；
- **防 XSS**：标题/描述/评论/用户名 React 文本节点渲染，无 `dangerouslySetInnerHTML`；
- **敏感信息**：响应不返回 `metadata`（含 stack/context），不暴露 email/token；
- **输入校验**：type/sort 白名单、rune 计数长度校验（与前端 `maxLength` 对齐）、body 64KiB 上限、UUID 解析 400/404。

## 8. 风险与扩展

| 项 | 说明 |
|---|---|
| 大列表性能 | 分页 + `(workspace_id, created_at DESC)` 索引；`hot`/`comments` 计数为聚合查询，二期必要时可改冗余计数列 |
| 低危加固（安全审计建议） | `ListFeedbacks` 的 `page` 参数无上限（极端值 offset 溢出）→ 建议后续加上限；评论接口可加限流 |
| 扩展性 | `feedback` 可预留 `related_issue_id`（Feedback→Issue/Workflow 联动），本期不实现 |
| 兼容性 | 桌面端路由错误上报依赖旧 `POST /api/feedback`，不得破坏；前端 `FeedbackModal` 保留供该链路使用 |

## 9. 测试策略

- **后端**：`server/internal/handler/feedback_center_test.go`（列表分页/筛选/搜索/排序、详情、提交、点赞/取消/幂等/UNIQUE、评论、未登录 401、空标题/空描述/非法 type、CJK 长度边界）+ 旧 `feedback_test.go` 回归（含 rune 截断测试）；
- **前端**：`feedback-center-page.test.tsx`、`feedback-detail-page.test.tsx`、`feedback-submit-vote.test.tsx` 组件测试 + paths/icons/tab 一致性测试；
- **E2E**：真实 HTTP 栈（Auth→Workspace→handler→DB）联调验证前后端契约一致。
