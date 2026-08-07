# 个人用量看板 · 后端实现交付物索引

> 关联需求：**CLO-206**（需求基线）→ **CLO-211**（API 契约/数据口径）→ **CLO-212**（后端实现）
> 归档时间：2026-08-05
> 状态：**已实现并本地验证通过**（待 Review / 联调）
> 存放位置：`deliverables/personal-dashboard-2026-08-05/代码/`

## 文件索引

| 文件 | 内容 | 消费者 |
| --- | --- | --- |
| `README.md` | 本索引：实现清单、路由、验证结论 | Leader / 全员 |
| `接口文档-个人用量看板.md` | 8 个接口的字段/类型/空值语义/示例，与 `设计/API契约-个人用量看板.md` 一致 | 前端 / 测试 |
| `本地验证报告.md` | go build / go vet / 单元测试 / 边界场景验证结论 | Leader / 测试 |
| `代码/` 下源码副本 | 新增后端文件快照（与仓库 HEAD 对齐） | 归档 |

## 实现清单（8 接口）

全部挂在 `server/cmd/server/router.go` 的 `/api/dashboard` 路由组下，接口 1–7 从登录会话取当前用户并强制过滤。

| # | 路由 | Handler | 说明 |
| --- | --- | --- | --- |
| 1 | `GET /api/dashboard/personal/summary` | `GetPersonalUsageSummary` | KPI：token 拆分、成本分片、任务数、运行时长、成功率 |
| 2 | `GET /api/dashboard/personal/trend` | `GetPersonalUsageTrend` | token/成本趋势（today 小时桶；week/month 天桶，全窗口零填充） |
| 3 | `GET /api/dashboard/personal/models` | `GetPersonalUsageModels` | 模型分布（token 与成本分片，按 token 降序） |
| 4 | `GET /api/dashboard/personal/duration` | `GetPersonalUsageDuration` | 单任务耗时直方图（恒 5 桶） |
| 5 | `GET /api/dashboard/personal/runtime-trend` | `GetPersonalRuntimeTrend` | 总运行时长趋势 |
| 6 | `GET /api/dashboard/personal/errors` | `GetPersonalUsageErrors` | 失败率趋势 + 错误类型分布（复用 failure_reason） |
| 7 | `GET /api/dashboard/personal/rank` | `GetPersonalUsageRank` | 我的排名 + 团队匿名聚合（不返回他人明细） |
| 8 | `GET /api/dashboard/rates` | `GetDashboardRates` | 实时汇率 USD→CNY（外部接口优先，失败降级默认值，5 分钟短缓存） |

## 新增/修改文件

**新增**
- `server/internal/handler/personal_dashboard.go` — 接口 1–7 处理器与响应结构
- `server/internal/handler/dashboard_rates.go` — 汇率服务（`DashboardRatesService` + 短缓存）
- `server/internal/handler/personal_dashboard_test.go` — 覆盖正常/空态/越权/汇率降级/排行隐私的单元测试
- `server/pkg/db/queries/personal_usage.sql` — 12 条个人聚合 SQL（sqlc 源）
- `server/pkg/db/generated/personal_usage.sql.go` — sqlc 生成代码

**修改**
- `server/internal/handler/handler.go` — `Handler` 增加 `DashboardRates *DashboardRatesService`（`New` 中构建）
- `server/cmd/server/router.go` — `/api/dashboard` 路由组下注册 8 个路由

## 关键实现口径（速览）

- **归属**：`agent_task_queue.initiator_user_id`（含 autopilot，算在配置人头上）；NULL 不归属任何个人。
- **范围**：终态任务 `status IN ('completed','failed')`，按 `completed_at` 落入 `[since, until)` 窗口。
- **token**：`input + output + cache_read + cache_write`，分项同步返回。
- **成本**：服务端回原始拆分 `cost_usd_ticks`（1e-10 USD）+ `uncosted_*_tokens`；USD→CNY 由前端按价目表估算 × `/rates` 汇率。
- **成功率**：`completed/(completed+failed)×100`，无终态任务为 `null`。
- **运行时长**：终态任务 `started_at→completed_at` 之和；缺任一时戳不计时长、不计耗时分布。
- **排行**：竞争式排名（并列共享名次）；`exceed_pct = (#得分<我)/(M-1)`；仅返回我的数字 + 团队人均/中位数。
- **汇率**：外部 `open.er-api.com`（可用 `MULTICA_DASHBOARD_FX_URL` 覆盖）优先，TTL 5 分钟缓存，失败降级 `MULTICA_DASHBOARD_FX_DEFAULT_CNY`（默认 7.2）。

## 验证结论

- `go build ./...` ✅
- `go vet ./internal/handler/` ✅
- 单元测试：个人看板相关测试全部通过（`TestPersonalDashboardSummaryAndSeries` / `TestPersonalUsageRank` / `TestPersonalDashboardEmptyState` / `TestPersonalDashboardUnauthorized` / `TestPersonalSummaryIgnoresClientUserID` / `TestDashboardRatesLiveAndDegraded` / `TestDashboardRatesServiceFallbackCache`）
- 详细结论见 `本地验证报告.md`

## 下一步

1. 与前端（CLO-213）联调：字段级命名与空值语义已对齐 `设计/API契约-个人用量看板.md`
2. 联调通过后进入 Review（CLO-214）审查
