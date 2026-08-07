# 数据监控看板 API 文档（CLO-170）

> 服务：`GET /api/dashboard/*`，均为工作区级统计接口。
> 认证：与其余工作区接口一致，携带登录态，并需 `X-Workspace-ID` 头（或 `X-Workspace-Slug`）。
> 时区：所有接口接受 `?tz=<IANA 时区>`（默认取用户已存时区，兜底 UTC），趋势/环比按该时区对齐。
> 数据范围：`?days=` 参数（1/7/30 对应 PRD 的 24h / 7d / 30d，默认 7，上限 365）。issue-distribution 为全量快照，不受 days 影响。
> 前端消费方：`packages/core/monitoring/queries.ts`（4 个接口各一个 query），`packages/views/dashboard/monitoring/*` 渲染。

---

## 1. GET /api/dashboard/issue-distribution

当前工作区全部 Issue 的状态分布 + 各项目状态构成（PRD §4.1 / §4.2）。**快照口径，不受时间范围影响。**

Query：`?tz=`（可选）

响应 `200`：

```json
{
  "total": 128,
  "status_counts": { "backlog": 10, "todo": 40, "in_progress": 30,
                     "in_review": 5, "done": 30, "blocked": 3, "cancelled": 10 },
  "projects": [
    { "id": "proj-1", "name": "主站", "total": 80,
      "status_counts": { "done": 40, "in_progress": 20 } }
  ]
}
```

- `status_counts` 为状态 id → 计数的字典，状态 id 用 `status` 字段原值，不含未知状态则不出键。
- `projects` 按各项目聚合；无项目 Issue 归入 `id="" / name=""` 桶，保证 `Σprojects[i].total === total`。
- `projects` 顺序：有项目 id 的按名称升序在前，无项目桶在最后；前端自行按 `total` 取 Top 8 + 其他。

---

## 2. GET /api/dashboard/activity

智能体工作量 + 团队活跃度（PRD §4.3）。工作量=在办负荷（in_progress+in_review 承接数），活跃度=时间窗口内 status_changed + comment + run 条数之和。

Query：`?days=` `?tz=`

响应 `200`：

```json
{
  "agent_workload": [
    { "id": "agent-1", "name": "后端开发智能体", "load": 3, "activity": 27 }
  ],
  "team_activity": [
    { "id": "squad-1", "name": "产品研发AI智能体团队", "load": 5, "activity": 40 }
  ]
}
```

- `agent_workload` 按 `load` 降序（并列按 `activity`）；`team_activity` 按 `activity` 降序。
- load 与 activity 都为 0 的实体不出现在列表中。
- 团队活跃度由其 agent 成员活跃度累加；团队工作量=该团队在办承接数。

---

## 3. GET /api/dashboard/comments

评论热度：窗口内评论总数、今日新增数、新增评论时间序列（PRD §4.4）。

Query：`?days=`（1 → 24h 按小时；7/30 → 按天）`?tz=`

响应 `200`：

```json
{
  "total": 320,
  "today": 12,
  "series": [
    { "time": "2026-08-05T00:00:00Z", "count": 12 },
    { "time": "2026-08-06T00:00:00Z", "count": 18 }
  ]
}
```

- `total`：窗口内（`since` 起）新增评论总数；`today`：查看者本地今天新增。
- `series`：按小时/按天分桶，空桶补 0（保证折线连续），`time` 为桶起点 ISO（UTC 表示）。
- 数据为空时 `series` 为空数组，`total`/`today` 为 0。

---

## 4. GET /api/dashboard/completion

完成率 / 延期率 + 环比 + 逐日趋势（PRD §4.5）。快照口径：对窗口内创建（created_at ≥ since）的 Issue 计算。

Query：`?days=` `?tz=`

响应 `200`：

```json
{
  "completion_rate": 45.2,
  "completion_delta": 3.1,
  "delay_rate": 12.5,
  "delay_delta": -2.0,
  "has_due_date_tasks": true,
  "trend": [
    { "time": "2026-08-05T00:00:00Z", "completion": 40.0, "delay": 10.0 }
  ]
}
```

- `completion_rate` = 窗口内 `done / total * 100`；`completion_delta` = 与前一个等长窗口的百分点差。
- `delay_rate` = 窗口内有 due_date 且未 done、due_date 早于查看者本地今天的 Issue / 有 due_date 的 Issue。**无 due_date 任务时为 `null`**，`delay_delta` 同为 `null`，`has_due_date_tasks=false`。
- `trend` 按天给出窗口内各日 cohort 的完成率 / 延期率；`delay` 无 due_date 数据时为 `null`。
- 全部为空/无数据时：rate 为 0、delta 为 0、`delay_rate`/`delay_delta` 为 `null`、`has_due_date_tasks=false`、`trend=[]`。

---

## 5. 资源与运行时状态列表

复用餐用看板既有接口：`GET /api/runtimes`（工作区内全部运行时，含 `last_seen_at`、`runtime_mode`、`provider`、`status`），前端通过 `deriveRuntimeHealth` 计算在线/离线，表格分页在前端完成（10/页）。CPU/内存占用字段 `AgentRuntime` 尚未提供，前端渲染 `—`（PRD P2 预留，见设计标注与 PRD §13 R1 风险备注）。

---

## 通用约定

- 错误响应：`{"error": "<消息>"}`；鉴权失败 401，权限不足 403，服务端聚合异常 500（各模块独立报错，前端单模块降级，不影响其它模块渲染）。
- 参数校验：`days` 非法/越界回退默认 7；`tz` 非法回退 UTC/用户时区；不做 4xx 拒绝（降级渲染策略）。
- 性能：单次聚合为 2~5 条 SQL（仅 issue-distribution 为 2 条全表分组），满足 60s 轮询高频刷新；时间范围切换走全量刷新（前端 range 变更即重查）。
- 数据安全：仅返回工作区成员可见的聚合数据；不做越权过滤的明细字段一律不出现在响应中。
