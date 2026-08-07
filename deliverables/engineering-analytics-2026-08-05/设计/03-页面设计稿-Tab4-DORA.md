# 页面设计稿：Tab4 DORA 效能结果（DORA）—— v2.0 全量页面

> 数据接口：D1 `dora/lead-time`、D2 `dora/deployments`
> 全局：页面壳（PageHeader + Tabs variant=line）+ 时间维度切换 + 时区 + 可选 project 筛选（**无部门筛选**，PRD §4.6）
> 空值语义：`source_status.ready=false → 引导态`、`null → '-'`、`[] → 空态文案`、`0 → 数值零`（API §0.5/§0.7）
> 上游：PRD §4.4 / §5.9 / E16–E17；组件规范见 `01-组件规范与Token对齐-阶段8增量.md`

---

## 1. 页面布局（正常态）

```
┌──────────────────────────────────────────────────────────────────────┐
│ PageHeader（同 Tab1，无部门筛选器）                                    │
├──────────────────────────────────────────────────────────────────────┤
│ Tabs(line): [活跃度与渗透] [Agent效能] [Git贡献] [DORA*]               │
├──────────────────────────────────────────────────────────────────────┤
│ ① KPI 卡组（4 张）                                                   │
│  部署频率(周) │ 变更失败率 │ MTTR │ 平均交付周期(P50)                   │
├──────────────────────────────────────────────┬───────────────────────┤
│ ② 交付周期趋势（半宽）                        │ ③ 部署频率趋势（半宽）   │
│  [Segmented: Issue→部署* / Issue→Merged]      │  柱状图：按周部署次数    │
│  P50/P95 双折线 + 「替代口径」角标             │                        │
├──────────────────────────────────────────────┴───────────────────────┤
│ ④ 变更失败率与 MTTR（全宽）                                          │
│  双指标折线（按周：失败率 / MTTR）+ 最近失败明细表                     │
│  明细：部署ID│应用│失败时间│恢复时间│MTTR│原因│状态                    │
└──────────────────────────────────────────────────────────────────────┘
```

- 内容容器：`mx-auto max-w-6xl space-y-5 p-6`；半宽两卡 `grid grid-cols-1 lg:grid-cols-2 gap-5`。

---

## 2. 模块① KPI 卡组

### 2.1 正常态

容器：`grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4`

| 卡 | label | value（KpiCard） | hint |
| --- | --- | --- | --- |
| 1 | 部署频率(周) | `4.5`（`deploy_frequency_weekly`，1 位小数） | 窗口部署 18 次 |
| 2 | 变更失败率 | `11.1%`（`change_failure_rate`×100） | 失败 2 / 总 18 |
| 3 | MTTR | `1时`（`mttr_seconds` → `formatDuration`） | P50 已恢复样本 |
| 4 | 平均交付周期(P50) | `1.5天`（D1 最近周 `p50_seconds`） | 口径见②角标 |

- 第 2 卡 value `accent="destructive"`（失败率）；第 1 卡 `accent="brand"`；第 3/4 卡 `DurationNumberFlow`。
- `null → '-'`（E16 部署未接入时四卡全部 `-` + 页面顶部说明条）。

### 2.2 引导态（E16：部署流水线未接入）

- 逐卡 `-`；页面顶部 `Alert variant="secondary"` 说明条：「部署流水线数据未接入：可通过 Webhook / 文件导入 / 手动录入接入。当前以「Issue→Merged」替代口径展示交付周期。」+ 「前往配置」`Button variant="outline" size="sm"`。
- ② 仍可展示 merged 口径（若 VCS 接入）；③④ 显示引导态（见下）。

### 2.3 加载态 / 错误态

- 加载：卡组 `Spinner` 居中。
- 错误：`Alert destructive` + 重试。

---

## 3. 模块② 交付周期趋势（D1）

### 3.1 正常态（deploy 口径已接入）

- 容器：`Card`，`CardHeader`：`CardTitle`「交付周期趋势」+ `CardAction` 放口径切换 `Segmented`「Issue→部署 / Issue→Merged」（默认 `deploy`）。
- 图表：`DoraTrendChart`（`01` §2.4）：
  - X 轴：周起始日（周一）；Y 轴：交付周期（秒 → `formatDuration` 天/时）；
  - P50 线 `--color-chart-1`、P95 线 `--color-chart-4`（`DualLineChart` 变体）；
  - `connectNulls={false}`；Tooltip：周 + P50/P95 + `· n 样本`（`sample_count`）。
- 无角标（deploy 已接入，非替代口径）。

### 3.2 自动降级（E16：deploy 未接入，merged 可算）

- 加载后 D1（`metric=deploy`）返回 `source_status.ready=false` → 前端自动重拉 `metric=merged`，Segmented 切至「Issue→Merged」，卡片右上角 `Badge variant="warning"`「替代口径：Merged」。
- 角标旁 `Tooltip`：「部署流水线未接入，暂以 Issue→PR 合并作为交付终点代理；接入部署流水线后自动恢复 Issue→部署口径。」（口径 §4.1）。
- 用户可手动切回 `deploy`：若仍 `ready=false` → 自动回落 merged + 角标（不报错）。

### 3.3 引导态（E16：deploy 与 VCS 均未接入）

- `ready=false` 且无 merged 数据 → `SourceGuideState`（`Timer` icon + 「部署流水线数据未接入」+ 描述「部署流水线数据未接入：可通过 Webhook / 文件导入 / 手动录入接入。当前以「Issue→Merged」替代口径展示交付周期。」+ 「前往配置」按钮）。

### 3.4 空态

- `points:[]` → `Empty`：
  - `EmptyMedia`：`Timer`（`size-8`）
  - `EmptyTitle`：「窗口内暂无交付周期样本」
  - `EmptyDescription`：「当前时间窗口内没有完成交付的记录，切换时间范围或等待交付后自动累积。」

### 3.5 加载态 / 错误态

- 加载：内容区 `Spinner`；口径切换保留旧图 + 原位 spinner。
- 错误：`Alert destructive` + 重试（仅重发该模块请求）。

### 3.6 边界（E17）

- 部署事件未关联到 Issue → 不计入「Issue→部署」样本（仅在部署频率/失败率计数）；前端无需特殊处理（服务端已过滤），图表正常。

---

## 4. 模块③ 部署频率趋势（D2）

### 4.1 正常态

- 容器：`Card`，`CardHeader`：`CardTitle`「部署频率趋势」+ `CardDescription`「按周部署次数」。
- 图表：`DeployFrequencyChart`（`01` §2.5，Recharts `BarChart`）：
  - X=周，Y=部署次数；柱 `--color-chart-2`，`radius={4}`；
  - Tooltip：周 + 部署次数 + 失败数（`trend[].failed`）。

### 4.2 引导态（E16）

- `D2.source_status.ready=false` → `SourceGuideState`（`Rocket` icon + 「部署流水线数据未接入」+ 描述同上 + 「前往配置」按钮）。

### 4.3 空态

- `trend:[]` → `Empty`：
  - `EmptyMedia`：`Rocket`（`size-8`）
  - `EmptyTitle`：「窗口内暂无部署记录」
  - `EmptyDescription`：「当前时间窗口内没有部署记录，切换时间范围或部署上线后自动累积。」

### 4.4 加载态 / 错误态

- 加载：内容区 `Spinner`。
- 错误：`Alert destructive` + 重试。

---

## 5. 模块④ 变更失败率与 MTTR（D2）

### 5.1 正常态

- 容器：`Card`，`CardHeader`：`CardTitle`「变更失败率与 MTTR」+ `CardAction` 放状态筛选 `Segmented`「全部 / 已恢复 / 进行中」（客户端过滤 `failures[].status`）。
- **双指标折线**（`DualLineChart` 复用，`--color-chart-1` 失败率 / `--color-chart-2` MTTR）：
  - X=周；Y 左轴=变更失败率（`failure_rate`×100 %）；Y 右轴=MTTR（秒→时）；
  - `connectNulls={false}`；Tooltip：周 + 失败率 + MTTR。
- **失败明细表**（`FailureDetailTable`，`01` §2.6）：

| 列 | 字段 | 渲染 |
| --- | --- | --- |
| 部署ID | `deployment_id` | `--font-mono text-xs` |
| 应用 | `app` | `text-xs`；`null → '-'` |
| 失败时间 | `failed_at` | `text-xs muted` |
| 恢复时间 | `recovered_at` | `text-xs muted`；`null → '进行中'` |
| MTTR | `mttr_seconds` | `text-xs tabular-nums`，`formatDuration`；`null → '-'` |
| 原因 | `reason` | `text-xs`（`truncate max-w-[16rem]`，hover 全量 `Tooltip`）；`null → '-'` |
| 状态 | `status` | `Badge`：已恢复=`success` / 进行中=`warning` |

- MTTR 列头可排序（默认降序）；行 `hover:bg-muted`。

### 5.2 引导态（E16）

- `D2.source_status.ready=false` → 整卡 `SourceGuideState`（`Rocket` icon + 描述同上 + 「前往配置」按钮）。

### 5.3 空态

- `trend:[]` 且 `failures:[]` → `Empty`：「窗口内暂无部署记录」。
- `total_deployments>0` 但 `failed_deployments=0`：折线正常（失败率 0），明细表 `Empty`：
  - `EmptyMedia`：`TriangleAlert`（`size-8`）
  - `EmptyTitle`：「暂无失败样本，变更失败率与 MTTR 待累积」
  - `EmptyDescription`：「窗口内有部署但无失败记录，本模块无需展示；如有失败部署，明细将自动累积。」

### 5.4 加载态 / 错误态

- 加载：内容区 `Spinner`。
- 错误：`Alert destructive` + 重试。

---

## 6. 全模块状态矩阵（Tab4）

| 模块 | 正常 | 引导态（ready=false） | 空态（ready=true 无数据） | 加载 | 错误 |
| --- | --- | --- | --- | --- | --- |
| ① KPI 卡组 | 数值 | 逐卡 `-` + 顶部说明条 | 逐卡 `0`/`-` | Spinner 居中 | Alert+重试 |
| ② 交付周期趋势 | P50/P95 双线+口径切换 | `SourceGuideState` / 自动降级 merged+角标 | `Empty` 窗口暂无样本 | Spinner | Alert+重试 |
| ③ 部署频率趋势 | 柱状图 | `SourceGuideState` | `Empty` 窗口暂无部署 | Spinner | Alert+重试 |
| ④ 变更失败率与MTTR | 双折线+明细表 | `SourceGuideState` | `Empty` 暂无失败样本待累积 | Spinner | Alert+重试 |

**异常场景映射**：E16（部署未接入）→ ①逐卡 `-` + ②自动降级 merged + ③④引导态；E17（部署无 Issue 关联）→ ②不计样本，图表正常；E20（快照过期）→ ②③角标（如有快照概念）；E7/E8/E9 同 Tab1。

---

## 7. 与 PRD 的口径对应（前端数据映射提示）

| PRD § | 设计落点 | API 字段 |
| --- | --- | --- |
| §4.4.1 ① | KPI 卡组 | D2 汇总 + D1 最近周 |
| §5.9 ② 交付周期 | 口径切换 + 自动降级 | D1 `metric` / `points[]` |
| §5.9 ③ 部署频率 | 柱状图 | D2 `trend[].deployments` |
| §5.9 ④ 失败率/MTTR | 双折线 + 明细表 | D2 `trend[]` / `failures[]` |

**口径降级判定（前端逻辑，E16）**：D1 `metric=deploy` 请求返回 `source_status.ready=false` → 自动以 `metric=merged` 重拉并显示「替代口径：Merged」角标；Segmented 两态均可手动切换，切至不可用口径时自动回落。
