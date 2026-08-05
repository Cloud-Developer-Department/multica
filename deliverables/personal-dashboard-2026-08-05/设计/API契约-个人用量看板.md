# API 契约：个人用量看板（8 接口）

> 文档编号：API-CLO-211
> 版本：v1.1（2026-08-05）
> 需求基线：CLO-206 §六（接口清单） / §七（边界异常）
> 配套文档：`PRD-个人用量看板.md`、`数据口径说明.md`
> 数据口径：所有指标计算一律遵循《数据口径说明》，本文档仅定义传输契约。

---

## 0. 通用约定

### 0.1 认证与鉴权

- 所有接口携带登录态（JWT cookie 或 Bearer），鉴权中间件注入 `X-User-ID`（服务端不可伪造）。
- 所有接口要求工作区成员身份（路由挂在工作区成员中间件下，`/{slug}/dashboard` 所在路由组）。
- **个人维度接口（接口 1–7）不接受、忽略任何前端传入的 `user_id`**，一律以服务端会话用户为准（CLO-206 §五）。

### 0.2 请求公共参数

| 参数 | 位置 | 类型 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `range` | query | string | 否 | `week` | `today`/`week`/`month`；非法或缺失回退 `week`，不报错 |
| `tz` | query | string | 否 | 用户已存时区 → UTC | IANA 时区，非法回退；趋势分桶按该时区切日/切时 |
| `metric` | query | string | 否 | 视接口 | 见各接口 |

> 时间窗口统一语义：`today`=今日 00:00→现在；`week`=本周一 00:00→现在；`month`=本月 1 日 00:00→现在（均按 `tz` 本地时区）。详情见 PRD §3。

### 0.3 成本传输与计算职责（重要）

服务端在成本相关字段中返回**原始拆分**（沿用现有工作区看板约定，见 `dashboard.go` 的 `DashboardUsageDailyResponse`）：

- `cost_usd_ticks`：供应商上报成本之和（单位 1e-10 USD），未上报行为 0。
- `uncosted_{input,output,cache_read,cache_write}_tokens`：**未上报成本的行**的 token 分量（需按价目表估算）。

**USD 与 CNY 由前端计算**（与现有看板一致，前端价目表 `packages/views/runtimes/utils.ts`）：

```
cost_usd  = cost_usd_ticks / 1e10 + Σ 估算(uncosted tokens × 前端价目表单价)
cost_cny  = cost_usd × /api/dashboard/rates 返回的 rate
```

- 前端从 `/rates` 取得生效汇率与更新时间，用于 ¥ 展示与「汇率更新于 xx:xx」标注。
- `priced`（模型是否匹配价目表）、`cost_pct` 等派生值由前端计算，契约不返回。
- 前后端价目表必须同步（`数据口径说明 §4.3`）。

### 0.4 响应结构

- 成功：`200`，`Content-Type: application/json`，直接返回数据对象（**无包裹层**，与现有看板接口一致）。
- 失败：**错误码 + 统一错误结构**（§11）。
- 字段命名：snake_case。
- **空值语义（总则）**，所有接口统一遵守：
  - **`null`**：该指标在本窗口**无有效分母或不可计算**（如无终态任务时的成功率、无成员时的中位数），前端渲染为 `-` 或空。
  - **`[]`（空数组）**：该维度**无数据**（如趋势点、模型列表、错误类型），前端渲染空态文案。
  - **缺省（字段不存在）**：仅用于向后兼容的**可选扩展字段**；契约内声明的字段**一律返回**（不存在"省略"语义）。
  - **`0`**：有明确语义的数值零（如无消耗时的 token 总数），**不是空态信号**。空态判定由前端按字段组合完成（见各接口"空态判定"）。

### 0.5 数值精度

- token 类：整数（`int64`/`number`，无小数）。
- 时长：整数秒（`int64`/`number`）。
- `cost_usd_ticks`：整数（1e-10 USD）。
- 百分比：`number`，保留 1 位小数（如 `88.1`）。
- 时间：RFC3339 UTC（趋势桶起点为"桶起始时刻"的 UTC 表示，前端按 `tz` 转本地）。

---

## 1. GET /api/dashboard/personal/summary

**用途**：KPI 卡（我的数据）——token、成本拆分、任务数、运行时长、成功率。

**请求**：`?range=&tz=`（§0.2）

**响应 `200`**：

| 字段 | 类型 | 空值/缺省语义 | 说明 |
| --- | --- | --- | --- |
| `range` | string | — | 实际生效的 range |
| `token_input` | number | 0 | 本人窗口内 input_tokens 之和 |
| `token_output` | number | 0 | output_tokens 之和 |
| `token_cache_read` | number | 0 | cache_read_tokens 之和 |
| `token_cache_write` | number | 0 | cache_write_tokens 之和 |
| `token_total` | number | 0 | 四项之和 |
| `cost_usd_ticks` | number | 0 | 供应商上报成本之和（1e-10 USD）；无上报为 0 |
| `uncosted_input_tokens` | number | 0 | 未上报成本行的 input 分量 |
| `uncosted_output_tokens` | number | 0 | 未上报成本行的 output 分量 |
| `uncosted_cache_read_tokens` | number | 0 | 未上报成本行的 cache_read 分量 |
| `uncosted_cache_write_tokens` | number | 0 | 未上报成本行的 cache_write 分量 |
| `task_count` | number | 0 | 终态任务总数（completed+failed） |
| `completed_count` | number | 0 | 成功数 |
| `failed_count` | number | 0 | 失败数 |
| `run_seconds` | number | 0 | 终态任务时长之和（秒）；无 duration 数据为 0 |
| `success_rate` | number\|null | **null** | `completed/(completed+failed)×100`，1 位小数；**无终态任务时为 null** |

**响应示例**：
```json
{
  "range": "week",
  "token_input": 45000000,
  "token_output": 12000000,
  "token_cache_read": 35000000,
  "token_cache_write": 5000000,
  "token_total": 97000000,
  "cost_usd_ticks": 150000000000,
  "uncosted_input_tokens": 12000000,
  "uncosted_output_tokens": 3000000,
  "uncosted_cache_read_tokens": 8000000,
  "uncosted_cache_write_tokens": 1500000,
  "task_count": 42,
  "completed_count": 37,
  "failed_count": 5,
  "run_seconds": 12900,
  "success_rate": 88.1
}
```

> 示例换算参考：`cost_usd = 150000000000/1e10（=$15.0）+ 估算(未上报 token)`；前端再乘 `/rates` 汇率得 ¥。

**空态判定（前端）**：`token_total === 0 && task_count === 0` → 「{今天/本周/本月}暂无消耗」；`success_rate === null` → 成功率显示 `-`。

---

## 2. GET /api/dashboard/personal/trend

**用途**：Token/成本趋势折线（模块③）。切换仅前端行为。

**请求**：`?range=&tz=&metric=`；`metric=tokens|cost`（可选，默认 `tokens`；**仅用于前端初始化，响应恒返回全部序列**）。

**分桶**：`today` → 小时桶（0 点→当前小时）；`week`/`month` → 天桶（周一/1 号→今天）。全窗口零填充。

**响应 `200`**：

| 字段 | 类型 | 空值/缺省语义 | 说明 |
| --- | --- | --- | --- |
| `range` | string | — | 生效 range |
| `points` | array | **`[]`** | 时间序列，见下 |

`points[]`：

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `time` | string | 桶起点 ISO（RFC3339 UTC） |
| `tokens` | number | 该桶 token 总数（含未定价模型） |
| `cost_usd_ticks` | number | 该桶供应商上报成本之和（1e-10 USD） |
| `uncosted_input_tokens` | number | 该桶未上报成本行的 input 分量 |
| `uncosted_output_tokens` | number | 该桶未上报成本行的 output 分量 |
| `uncosted_cache_read_tokens` | number | 该桶未上报成本行的 cache_read 分量 |
| `uncosted_cache_write_tokens` | number | 该桶未上报成本行的 cache_write 分量 |
| `task_count` | number | 该桶任务数 |

**响应示例**（range=week）：
```json
{
  "range": "week",
  "points": [
    { "time": "2026-08-03T00:00:00Z", "tokens": 12000000, "cost_usd_ticks": 15000000000, "uncosted_input_tokens": 3000000, "uncosted_output_tokens": 800000, "uncosted_cache_read_tokens": 2000000, "uncosted_cache_write_tokens": 400000, "task_count": 6 },
    { "time": "2026-08-04T00:00:00Z", "tokens": 25000000, "cost_usd_ticks": 40000000000, "uncosted_input_tokens": 4000000, "uncosted_output_tokens": 1200000, "uncosted_cache_read_tokens": 2500000, "uncosted_cache_write_tokens": 500000, "task_count": 11 },
    { "time": "2026-08-05T00:00:00Z", "tokens": 60000000, "cost_usd_ticks": 95000000000, "uncosted_input_tokens": 5000000, "uncosted_output_tokens": 1000000, "uncosted_cache_read_tokens": 3500000, "uncosted_cache_write_tokens": 600000, "task_count": 25 }
  ]
}
```

**空态判定**：`points` 为空数组 → 「暂无趋势数据」。

---

## 3. GET /api/dashboard/personal/models

**用途**：模型分布（模块④），token/成本占比。

**请求**：`?range=&tz=`

**响应 `200`**：

| 字段 | 类型 | 空值/缺省语义 | 说明 |
| --- | --- | --- | --- |
| `range` | string | — | 生效 range |
| `total_tokens` | number | 0 | 窗口内总 token |
| `items` | array | **`[]`** | 按 token 总量降序，见下 |

`items[]`：

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `provider` | string | 供应商（小写归一） |
| `model` | string | 模型标识（原文） |
| `token_input` | number | 该模型 input 分量 |
| `token_output` | number | 该模型 output 分量 |
| `token_cache_read` | number | 该模型 cache_read 分量 |
| `token_cache_write` | number | 该模型 cache_write 分量 |
| `token_total` | number | 四者之和 |
| `cost_usd_ticks` | number | 该模型供应商上报成本之和（1e-10 USD） |
| `uncosted_input_tokens` | number | 该模型未上报成本行的 input 分量 |
| `uncosted_output_tokens` | number | 该模型未上报成本行的 output 分量 |
| `uncosted_cache_read_tokens` | number | 该模型未上报成本行的 cache_read 分量 |
| `uncosted_cache_write_tokens` | number | 该模型未上报成本行的 cache_write 分量 |
| `task_count` | number | 该模型任务数 |

> 前端据此计算每模型 `cost_usd`、`cost_cny`、`token_pct`、`cost_pct`；价目表未匹配的模型 cost 计 0，前端归入「未定价」分类展示。

**响应示例**：
```json
{
  "range": "week",
  "total_tokens": 97000000,
  "items": [
    { "provider": "deepseek", "model": "deepseek-v4-flash", "token_input": 40000000, "token_output": 8000000, "token_cache_read": 10000000, "token_cache_write": 2000000, "token_total": 60000000, "cost_usd_ticks": 60000000000, "uncosted_input_tokens": 10000000, "uncosted_output_tokens": 2000000, "uncosted_cache_read_tokens": 3000000, "uncosted_cache_write_tokens": 500000, "task_count": 25 },
    { "provider": "openai", "model": "gpt-5.6-luna", "token_input": 4000000, "token_output": 3000000, "token_cache_read": 15000000, "token_cache_write": 3000000, "token_total": 25000000, "cost_usd_ticks": 50000000000, "uncosted_input_tokens": 1500000, "uncosted_output_tokens": 800000, "uncosted_cache_read_tokens": 4000000, "uncosted_cache_write_tokens": 800000, "task_count": 12 },
    { "provider": "xai", "model": "grok-4.3", "token_input": 1000000, "token_output": 1000000, "token_cache_read": 10000000, "token_cache_write": 0, "token_total": 12000000, "cost_usd_ticks": 40000000000, "uncosted_input_tokens": 0, "uncosted_output_tokens": 0, "uncosted_cache_read_tokens": 0, "uncosted_cache_write_tokens": 0, "task_count": 5 }
  ]
}
```

**空态判定**：`items` 为空数组 → 「暂无模型数据」。

---

## 4. GET /api/dashboard/personal/duration

**用途**：单任务耗时分布直方图（模块⑤）。恒返回 5 个分桶。

**请求**：`?range=&tz=`

**分桶定义**（秒，下含上不含）：`<1m`=[0,60)；`1-5m`=[60,300)；`5-15m`=[300,900)；`15-30m`=[900,1800)；`>30m`=[1800,∞)。

**响应 `200`**：

| 字段 | 类型 | 空值/缺省语义 | 说明 |
| --- | --- | --- | --- |
| `range` | string | — | 生效 range |
| `total` | number | 0 | 计入分布的终态任务数（= Σ buckets.count） |
| `buckets` | array | 恒 5 项 | 见下，空桶 count=0 |

`buckets[]`：

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `bucket` | string | `"<1m"` / `"1-5m"` / `"5-15m"` / `"15-30m"` / `">30m"` |
| `label` | string | 展示名（`<1分钟` / `1-5分钟` / `5-15分钟` / `15-30分钟` / `>30分钟`） |
| `min_seconds` | number | 下界（含） |
| `max_seconds` | number | 上界（不含）；末桶为 null |
| `count` | number | 该桶任务数 |

**响应示例**：
```json
{
  "range": "week",
  "total": 42,
  "buckets": [
    { "bucket": "<1m",    "label": "<1分钟",   "min_seconds": 0,    "max_seconds": 60,     "count": 18 },
    { "bucket": "1-5m",   "label": "1-5分钟",  "min_seconds": 60,   "max_seconds": 300,    "count": 14 },
    { "bucket": "5-15m",  "label": "5-15分钟", "min_seconds": 300,  "max_seconds": 900,    "count": 6  },
    { "bucket": "15-30m", "label": "15-30分钟","min_seconds": 900,  "max_seconds": 1800,   "count": 3  },
    { "bucket": ">30m",   "label": ">30分钟",  "min_seconds": 1800, "max_seconds": null,   "count": 1  }
  ]
}
```

**空态判定**：`total === 0` → 「暂无耗时数据」。

---

## 5. GET /api/dashboard/personal/runtime-trend

**用途**：总运行时长趋势（模块⑥）。

**请求**：`?range=&tz=`

**分桶**：与接口 2 相同（today 小时桶；week/month 天桶），全窗口零填充。

**响应 `200`**：

| 字段 | 类型 | 空值/缺省语义 | 说明 |
| --- | --- | --- | --- |
| `range` | string | — | 生效 range |
| `points` | array | **`[]`** | 见下 |

`points[]`：

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `time` | string | 桶起点 ISO（RFC3339 UTC） |
| `run_seconds` | number | 该桶总时长（秒） |
| `task_count` | number | 该桶任务数 |

**响应示例**：
```json
{
  "range": "week",
  "points": [
    { "time": "2026-08-03T00:00:00Z", "run_seconds": 2400, "task_count": 6 },
    { "time": "2026-08-04T00:00:00Z", "run_seconds": 5100, "task_count": 11 },
    { "time": "2026-08-05T00:00:00Z", "run_seconds": 5400, "task_count": 25 }
  ]
}
```

**空态判定**：`points` 为空数组 → 「暂无运行时长数据」。

---

## 6. GET /api/dashboard/personal/errors

**用途**：失败率趋势（模块⑦）+ 错误类型分布（模块⑧），复用 `taskfailure` 分类。

**请求**：`?range=&tz=`

**响应 `200`**：

| 字段 | 类型 | 空值/缺省语义 | 说明 |
| --- | --- | --- | --- |
| `range` | string | — | 生效 range |
| `completed_count` | number | 0 | 成功数 |
| `failed_count` | number | 0 | 失败数 |
| `failure_rate` | number\|null | **null** | `failed/(completed+failed)×100`，1 位小数；无终态任务为 null |
| `trend` | array | **`[]`** | 失败率逐桶趋势，见下 |
| `types` | array | **`[]`** | 错误类型分布，见下 |

`trend[]`：

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `time` | string | 桶起点 ISO（RFC3339 UTC） |
| `completed` | number | 该桶成功数 |
| `failed` | number | 该桶失败数 |
| `failure_rate` | number\|null | 该桶失败率；**该桶无终态任务为 null** |

`types[]`（按 `count` 降序）：

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `reason` | string | `taskfailure` 规范值；`failure_reason` 为空/缺失的失败归入 `"unclassified"` |
| `count` | number | 该类型失败次数 |
| `pct` | number | `count / failed_count × 100`，1 位小数；failed_count=0 时为 0 |

**响应示例**：
```json
{
  "range": "week",
  "completed_count": 37,
  "failed_count": 5,
  "failure_rate": 11.9,
  "trend": [
    { "time": "2026-08-03T00:00:00Z", "completed": 8,  "failed": 1, "failure_rate": 11.1 },
    { "time": "2026-08-04T00:00:00Z", "completed": 10, "failed": 2, "failure_rate": 16.7 },
    { "time": "2026-08-05T00:00:00Z", "completed": 19, "failed": 2, "failure_rate": 9.5 }
  ],
  "types": [
    { "reason": "agent_error.provider_quota_limit", "count": 3, "pct": 60.0 },
    { "reason": "timeout", "count": 2, "pct": 40.0 }
  ]
}
```

**空态判定**：`types` 为空 → 「暂无错误类型数据」；`trend` 为空 → 「暂无失败数据」。

---

## 7. GET /api/dashboard/personal/rank

**用途**：我的排名卡（模块②）。**安全约束：绝不返回任何他人明细**（CLO-206 §五）。

**请求**：`?range=&tz=`

**响应 `200`**：

| 字段 | 类型 | 空值/缺省语义 | 说明 |
| --- | --- | --- | --- |
| `range` | string | — | 生效 range |
| `my_tokens` | number | 0 | 本人窗口内 token 总数 |
| `rank` | number\|null | **null** | 本人名次（token 降序，`RANK()` 竞争式排名，并列共享名次）；**本人无终态任务为 null** |
| `total_members` | number | 0 | 窗口内有终态任务的成员数（含本人） |
| `exceed_pct` | number\|null | **null** | 消耗低于本人的成员占比，见数据口径 §8；本人无任务为 null，total_members≤1 时为 0 |
| `team_avg_tokens` | number\|null | **null** | 团队人均 token；total_members=0 为 null |
| `team_median_tokens` | number\|null | **null** | 团队 token 中位数；total_members=0 为 null |

**响应示例**：
```json
{
  "range": "week",
  "my_tokens": 48000000,
  "rank": 3,
  "total_members": 12,
  "exceed_pct": 81.8,
  "team_avg_tokens": 15420000,
  "team_median_tokens": 9800000
}
```

**边界示例**：
```json
// 本人无任务：rank/exceed 为 null（前端显示「暂无排名」）
{
  "range": "week",
  "my_tokens": 0,
  "rank": null,
  "total_members": 11,
  "exceed_pct": null,
  "team_avg_tokens": 16800000,
  "team_median_tokens": 9900000
}
```

**空态判定**：`rank === null` → 「暂无排名」；`team_avg_tokens/team_median_tokens === null` → 显示 `-`。

---

## 8. GET /api/dashboard/rates

**用途**：实时汇率 USD→CNY（外部接口优先，失败降级配置默认值），返回生效汇率与更新时间（PRD §6.4 / 数据口径 §4.4）。

**请求**：无 query 参数（可带 `?tz=`，无业务影响，忽略）。

**响应 `200`**：

| 字段 | 类型 | 空值/缺省语义 | 说明 |
| --- | --- | --- | --- |
| `rate` | number | — | 生效汇率（USD→CNY，4 位小数），前端成本换算一律使用本值 |
| `source` | string | — | `"live"`（实时接口成功）\| `"default"`（降级默认值） |
| `updated_at` | string\|null | **null** | 实时汇率最近成功拉取时间（RFC3339）；source=default 时为 null |
| `default_rate` | number | — | 配置的默认汇率（如 7.2） |

**响应示例**：
```json
// 实时成功
{ "rate": 7.1800, "source": "live", "updated_at": "2026-08-05T01:30:00Z", "default_rate": 7.2 }

// 降级默认
{ "rate": 7.2000, "source": "default", "updated_at": null, "default_rate": 7.2 }
```

**前端使用**：页面加载请求一次（60s 内复用）；`source=live` 标注「汇率更新于 `updated_at`（本地 HH:mm）」；`source=default` 标注「汇率更新于 xx:xx（默认汇率）」或省略时间（见 PRD 文案清单）。

---

## 9. 数据表来源

| 表 | 用途 | 说明 |
| --- | --- | --- |
| `agent_task_queue` | 任务生命周期、归属、时长、失败原因 | `initiator_user_id`、`status`、`started_at`、`completed_at`、`failure_reason` |
| `task_usage` | token 明细与成本拆分 | `provider`、`model`、`input/output/cache_read/cache_write_tokens`、`cost_usd_ticks` |
| （价目表） | 未定价成本的静态估算 | 前端 `packages/views/runtimes/utils.ts`（成本计算侧）；服务端 `server/internal/metrics/pricing.go`（Prometheus 等）保持同步 |

> 个人统计**不经 `task_usage_hourly` 预聚合**（无用户维度），按 CLO-206 §六说明从 `task_usage` 直查并按 `initiator_user_id` 关联聚合（本地规模足够）。参考后端已起草的 `server/pkg/db/queries/personal_usage.sql`。

## 10. 路由注册建议（后端）

挂在现有 `r.Route("/api/dashboard", …)` 之下（`server/cmd/server/router.go`）：

```
r.Get("/personal/summary",        h.GetPersonalUsageSummary)
r.Get("/personal/trend",          h.GetPersonalUsageTrend)
r.Get("/personal/models",         h.GetPersonalUsageModels)
r.Get("/personal/duration",       h.GetPersonalUsageDuration)
r.Get("/personal/runtime-trend",  h.GetPersonalRuntimeTrend)
r.Get("/personal/errors",         h.GetPersonalUsageErrors)
r.Get("/personal/rank",           h.GetPersonalUsageRank)
r.Get("/rates",                   h.GetDashboardRates)
```

## 11. 错误码与错误响应结构

### 11.1 统一错误响应

```json
{ "error": "<消息>" }
```

与现有看板/工作区接口一致（`server/internal/middleware/workspace.go` 的 `writeError`）。

### 11.2 状态码与错误码表

| HTTP | 错误码语义 | 典型触发 | 前端表现 |
| --- | --- | --- | --- |
| `401` | 未认证 | 未登录/会话过期 | 跳转登录 |
| `403` | 权限不足 | 非工作区成员 | 提示无权限 |
| `404` | 资源/工作区不存在 | 无效 slug/workspace | 提示 |
| `400` | 参数非法 | `range` 之外参数异常（如畸形 UUID 类参数） | 按模块错误态处理 |
| `429` | 请求过频（可选） | 刷新节流防刷 | 提示稍后再试 |
| `500` | 服务端聚合异常 | 数据库/SQL 异常、汇率服务异常且无默认值 | 模块错误态 + 重试 |
| `503` | 服务不可用 | 依赖服务不可用 | 模块错误态 + 重试 |

> 说明：`range`/`tz` 非法按 **0.2 约定回退默认值，不返回 4xx**（降级渲染策略，与现有看板一致）。`user_id` 若被前端传入一律忽略，不报错。

### 11.3 各接口错误覆盖矩阵

| 场景 | 接口 | 响应 |
| --- | --- | --- |
| 空态（无数据） | 1–7 | `200` + 契约内空态字段（`null`/`[]`/`0`），**不是错误** |
| 汇率拉取失败 | 8 | 仍 `200`，`source=default`；**不返回 500**（除非连默认值也未配置） |
| 汇率未配置且拉取失败 | 8 | `500` |
| 未登录 | 1–8 | `401 {"error":"user not authenticated"}` |
| 非成员 | 1–8 | `403` / `404` |

### 11.4 时间序列一致性（跨接口）

同一 `range` 下，接口 2/5/6 的时间桶轴**必须一致**（同一时区切分、同一零填充规则），保证前端图表并排不错位。
