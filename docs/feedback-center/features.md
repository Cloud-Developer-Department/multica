# 反馈中心 — 功能说明

> 关联需求：WS-24「将现有反馈功能升级为全员可见的反馈中心」。
> 文档版本：v1.0 ｜ 日期：2026-08-13 ｜ 对应代码提交：后端 `19b3adae4`、前端 `7da924f0c`（基线 `e75b617c7`）

## 1. 功能定位

反馈中心（Feedback Center）将原先隐藏在底部帮助菜单里的「反馈」入口升级为**左侧一级导航**，
形成一个「全员可见的公共反馈池」：任何登录用户都能浏览、搜索、筛选、查看反馈详情，并参与点赞与评论；
登录用户还可以提交新的反馈。**不引入管理员角色 / 管理员后台 / 审核流程 / 独立 RBAC**，权限模型只有
「未登录 / 已登录」两档，遵循 Public Read + Authenticated Write。

```text
用户 → 查看反馈 → 发现相同问题 → 👍 支持 → 参与评论 → 提交新的反馈
```

## 2. 功能清单

| 编号 | 功能 | 说明 | 未登录 | 登录 |
|---|---|---|---|---|
| FR-1 | 左侧一级导航「反馈」 | 与 Issues / 项目 / 自动化 / 智能体 / 小队同级，MessageCircle 图标 | — | — |
| FR-2 | 底部帮助菜单「反馈」 | 保留菜单项，点击跳转到反馈中心（同一页面），不再直接打开提交弹窗 | — | — |
| FR-3 | 反馈中心列表页 | 标题 + 副标题 +「提交反馈」按钮 + 类型筛选 + 搜索 + 排序 + 卡片列表 + 分页（无限滚动） | ✅ 可看 | ✅ |
| FR-4 | 搜索 | 关键词模糊匹配**标题 + 描述**（数据库 ILIKE），防抖 300ms | ✅ | ✅ |
| FR-5 | 类型筛选 | 全部 / 问题反馈(`bug`) / 功能建议(`feature`) / 体验优化(`improvement`) / 其他(`other`) | ✅ | ✅ |
| FR-6 | 排序 | 最新(`latest`，默认) / 最热门(`hot`，按点赞数) / 评论最多(`comments`，按评论数) | ✅ | ✅ |
| FR-7 | 分页 | `page` + `page_size`（默认 20，上限 100），前端无限滚动加载更多 | ✅ | ✅ |
| FR-8 | 反馈卡片 | 点赞数、标题、描述摘要（≤140 字符）、类型 Badge、创建者、创建时间、评论数 | ✅ | ✅ |
| FR-9 | 反馈详情页 | 返回链接、点赞按钮、类型 Badge、创建者、时间、完整描述、评论列表、发表评论 | ✅ | ✅ |
| FR-10 | 提交反馈 | Modal 表单：反馈类型 + 标题（≤200 字符）+ 详细描述（≤10000 字符） | ❌ | ✅ |
| FR-11 | 点赞 / 取消点赞 | 「👍 支持 / 👍 已支持」切换；同用户同反馈最多一条 Vote（DB UNIQUE 硬约束）；刷新后状态保持（`my_vote`） | ❌ | ✅ |
| FR-12 | 评论反馈 | 头像 + 用户名 + 内容 + 时间；评论内容 ≤5000 字符；Ctrl/⌘+Enter 快捷发送 | ❌ | ✅ |
| FR-13 | 空状态 | 「还没有反馈 / 成为第一个告诉我们如何改进 Multica 的人。」与「没有找到相关反馈 / 尝试更换关键词或筛选条件。」 | — | — |
| FR-14 | 错误状态 | 「加载反馈失败」+ [重新加载]、「反馈提交失败，请稍后重试。」、「评论发送失败，请稍后重试。」，不出现裸 JSON / 堆栈 | — | — |
| FR-15 | Loading 状态 | 列表 / 详情 / 评论均使用 Skeleton 骨架屏 | — | — |
| FR-16 | 响应式 | Desktop / Tablet 正常显示；移动端不溢出、按钮可点、Modal 不超屏 | — | — |

## 3. 权限模型

- **只区分两类用户**：未登录、已登录。不新增任何 Admin / Moderator 角色，不新增管理员后台，不引入审核流程，不新增独立 RBAC。
- **Public Read**：反馈路由整体挂在 `RequireWorkspaceMember` 分组下（与 Issues / 项目同级），
  任何**登录且为该 workspace 成员**的用户均可读列表、详情、评论与点赞数（本期遵循现有 workspace 强制登录机制）。
- **Authenticated Write**：提交反馈、点赞 / 取消点赞、发表评论均要求登录，后端在 handler 内 `requireUserID` 兜底（未登录返回 401）。
- **身份来源**：`creator_id` 取自服务端登录态，`workspace_id` 取自当前 Workspace 上下文中间件注入，
  请求体不接受客户端传入 `creator_id` / `workspace_id`。

## 4. 反馈类型

| 枚举值 | 展示文案（zh-Hans） |
|---|---|
| `bug` | 问题反馈 |
| `feature` | 功能建议 |
| `improvement` | 体验优化 |
| `other` | 其他 |

数据库层面由 `feedback.type` 的 `CHECK (type IN ('bug','feature','improvement','other'))` 兜底。

## 5. 搜索与排序

- **搜索**：`keyword` 参数对 `title` 与 `description` 做 `ILIKE '%keyword%'` 模糊匹配（sqlc 参数化，无注入面），与类型筛选、排序、分页可组合生效。
- **排序**：
  - `latest`：`created_at DESC`（默认）
  - `hot`：`vote_count DESC, created_at DESC`
  - `comments`：`comment_count DESC, created_at DESC`
  - 计数一期采用**聚合查询**（`COUNT(DISTINCT ...)`），不设冗余计数列；`hot` / `comments` 追加 `created_at DESC` 次级排序键保证翻页稳定。

## 6. 数据模型

```text
workspace 1 ─── * feedback 1 ─── * feedback_vote
                          │
                          └─── * feedback_comment *─── 1 user
```

| 表 | 说明 | 关键约束 |
|---|---|---|
| `feedback`（改造，migration 285） | 原有 message-only 反馈表升级为反馈中心模型 | `creator_id`、`description`、`title`、`type`、`updated_at`；`workspace_id` 保留可空（删除 workspace 时 SET NULL 语义不变）；新增 `(workspace_id, created_at DESC)` 索引 |
| `feedback_vote`（新建，migration 286） | 点赞 | **`UNIQUE(feedback_id, user_id)`** 硬约束 + `ON CONFLICT DO NOTHING` 幂等 |
| `feedback_comment`（新建，migration 287） | 评论 | `(feedback_id, created_at)` 索引 |

存量数据迁移：`title` 取 `description` 前 80 字符截断（空则 `(no title)`），`type` 统一回填为 `other`（存量无法可靠回溯分类），up/down migration 成对可回滚。

## 7. 兼容性与复用

- **复用**：现有 `feedback` 表、`POST /api/feedback` 提交链路（桌面端路由错误上报依赖，保留不动）、`FeedbackModal` 提交弹窗、`HelpLauncher` 入口、左侧导航框架、`route-icons` / `paths`、UI 组件库、API Client、i18n、Auth / Workspace 中间件、sqlc ORM。
- **未重建第二套反馈系统**：反馈中心提交走新 `POST /api/feedbacks`（`type + title + description`）；桌面路由错误上报继续走旧 `POST /api/feedback`（`message + kind + context`），双轨并行互不影响。
- **扩展性**：数据与代码结构预留后续 `related_issue_id`（Feedback→Issue/Workflow）扩展位，本期不实现。

## 8. 本期范围外

反馈状态流转、公开/私有切换、Feedback→Issue/Workflow/Release 联动、评论嵌套 / @用户 / 评论点赞 / 评论置顶、
删除/编辑自己的评论、标签系统、AI 自动分类、通知中心、全文搜索引擎（Elasticsearch 等）、移动端专门设计。
