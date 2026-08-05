# 研发效能分析平台 · 后端实现交付物索引

> 关联需求：**CLO-227**（需求基线）→ **CLO-228**（PRD / API 契约 v2.0 / 数据口径 v2.0）→ **CLO-237**（阶段7 补充）→ **CLO-239**（后端实现）
> 归档时间：2026-08-06
> 状态：**已实现并本地验证通过**（待 Review / 联调）
> 存放位置：`deliverables/engineering-analytics-2026-08-05/代码/`

## 文件索引

| 文件 | 内容 | 消费者 |
| --- | --- | --- |
| `README.md` | 本索引：实现清单、路由、验证结论 | Leader / 全员 |
| `接口文档-研发效能分析平台.md` | 19 个接口的字段/类型/空值语义/示例/引导态，与 `设计/API契约-研发效能分析平台.md` v2.0 一致 | 前端 / 测试 |
| `本地验证报告.md` | go build / go vet / 单元测试 / 引导态与边界场景验证结论 | Leader / 测试 |
| `src/` 下源码副本 | 新增后端文件快照（与仓库 HEAD 对齐） | 归档 |

## 实现清单（19 接口，全量四看板）

全部挂在 `server/cmd/server/router.go` 的 `/api/analytics` 路由组下（工作区成员中间件，`/{slug}/analytics` 页面所在路由组）。

| # | 路由 | Handler | 说明 |
| --- | --- | --- | --- |
| A1 | `GET /api/analytics/activity/summary` | `GetAnalyticsActivitySummary` | 活跃度汇总 KPI（成员/活跃天数/DAU/MAU/人均 Issue） |
| A2 | `GET /api/analytics/activity/heatmap` | `GetAnalyticsActivityHeatmap` | 活跃热力图（`metric` 三口径） |
| A3 | `GET /api/analytics/activity/top-members` | `GetAnalyticsActivityTopMembers` | 成员活跃排行 Top N |
| A4 | `GET /api/analytics/adoption/summary` | `GetAnalyticsAdoptionSummary` | Agent 渗透率汇总 |
| A5 | `GET /api/analytics/adoption/trend` | `GetAnalyticsAdoptionTrend` | Agent 渗透率按日趋势 |
| B1 | `GET /api/analytics/agents/funnel` | `GetAnalyticsAgentsFunnel` | 执行漏斗（`merged_count: null`=VCS 未接入） |
| B2 | `GET /api/analytics/agents/performance` | `GetAnalyticsAgentsPerformance` | Agent 效能汇总（成功率/时长/失败类） |
| B3 | `GET /api/analytics/agents/top` | `GetAnalyticsAgentsTop` | Agent 效能排行 |
| B4 | `GET /api/analytics/skills/overview` | `GetAnalyticsSkillsOverview` | 技能图谱（总量/新增/积累/复用） |
| B5 | `GET /api/analytics/collaboration/summary` | `GetAnalyticsCollaborationSummary` | 协作指数 + Blocker 汇总 |
| B6 | `GET /api/analytics/collaboration/blockers` | `GetAnalyticsCollaborationBlockers` | Blocker 明细（响应时长） |
| G1 | `GET /api/analytics/git/eloc` | `GetAnalyticsGitEloc` | ELOC 排名（`group_by=member|agent`）+ `source_status` |
| G2 | `GET /api/analytics/git/quality` | `GetAnalyticsGitQuality` | 提交质量雷达 + `source_status` |
| G3 | `GET /api/analytics/git/repos` | `GetAnalyticsGitRepos` | 仓库活跃/PR 积压/MTTM + `source_status` |
| G4 | `GET /api/analytics/git/prs` | `GetAnalyticsGitPRs` | PR 明细下钻 + `source_status` |
| D1 | `GET /api/analytics/dora/lead-time` | `GetAnalyticsDoraLeadTime` | 交付周期（`metric=deploy|merged`）+ `source_status` |
| D2 | `GET /api/analytics/dora/deployments` | `GetAnalyticsDoraDeployments` | 部署频率/变更失败率/MTTR + `source_status` |
| L1 | `GET /api/analytics/identity/lifecycle` | `GetAnalyticsIdentityLifecycle` | 生命周期/熔断 + `source_status` |
| L2 | `GET /api/analytics/identity/departments` | `GetAnalyticsIdentityDepartments` | 部门清单 + `source_status` |

## 新增/修改文件

**新增**
- `server/internal/handler/analytics_dashboard.go` — 19 个接口处理器、`analyticsScope` 公共解析（窗口/时区/部门/项目过滤）、`source_status` 判定
- `server/internal/handler/analytics_dashboard_test.go` — 覆盖全部 19 接口的授权/正常态/引导态/边界（CLO-239 本次补充）
- `server/pkg/db/queries/analytics.sql` — 43 条聚合 SQL（sqlc 源）
- `server/pkg/db/generated/analytics.sql.go` — sqlc 生成代码
- `server/migrations/254_analytics_dashboard.up.sql` / `.down.sql` — 数据模型（`member.department` / `repo_quality_snapshot` / `deployment_event` / `vcs_author_mapping` / `identity_import`）
- `packages/core/analytics-dashboard/`、`packages/core/api/client.ts` / `schemas.ts`、`packages/views/analytics/` — 前端（CLO-240 交付）

**修改**
- `server/cmd/server/router.go` — `/api/analytics` 路由组注册 19 个路由

## 关键实现口径（速览）

- **窗口**：`?days=`（1–365，默认 30）自然日窗口，`?tz=`（IANA，回退用户时区 → UTC）本地 00:00 为日界。
- **活跃成员** = 窗口内创建 Issue / 发表评论 / 发起 Agent 任务的成员（去重）；热力图按 `(本地日, 本地时)` 分桶。
- **比率**：0–1 float（前端 ×100 展示 %），分母为 0 返回 `null`。
- **时长**：秒（交付周期/合并周期/MTTM/Blocker 响应），P50/P95 线性插值。
- **`source_status`**：G/D/L 接口顶层恒返回；未接入数据源返回 `200 + ready=false + reason`，**不 404 / 不 5xx**。
- **`department_id`**：仅 Tab1/Tab2/L1 支持；未配置部门数据时传入 → `400`（E18）。
- **`project_id`**：Tab1/Tab2 过滤；格式非法或不存在 → `400`。

## 验证结论

- `go build ./...` ✅
- `go vet ./internal/handler/` ✅
- 单元测试：`go test ./internal/handler/ -run "TestAnalytics"` — **14 项全部通过**（授权 / A1–A5 / B1–B6 / G1–G4 引导态 / D1–D2 引导态 / L1–L2 引导态 / department_id 400 / project_id 400 / limit 钳制）
- 全量 handler 测试包中 `TestDashboardFailuresByAgentUsesExactWindow`、`TestParseSkillArchive_RejectsUnsafeSkillMdPath` 为基线分支既有失败（本地共享测试库数据/环境相关，与本次改动无关，已对照确认）
- 详细结论见 `本地验证报告.md`

## 下一步

1. 与前端（CLO-240 已交付）联调：`source_status` 判定、ELOC 人/机分组、雷达反轴归一化参数、秒→天换算已按契约对齐
2. 联调通过后进入 Review（阶段10）审查
3. Review 通过后进入测试阶段（阶段11），最后统一造演示数据（DevOps 阶段12）
