# 页面设计稿：Tab3 Git 贡献（Git Contributions）—— v2.0 全量页面

> 数据接口：G1 `git/eloc`、G2 `git/quality`、G3 `git/repos`、G4 `git/prs`（下钻）
> 全局：页面壳（PageHeader + Tabs variant=line）+ 时间维度切换 + 时区 + 可选 project 筛选（**无部门筛选**，PRD §4.6）
> 空值语义：`source_status.ready=false → 引导态`、`null → '-'`、`[] → 空态文案`、`0 → 数值零`（API §0.5/§0.7）
> 上游：PRD §4.3 / §5.6–5.8 / E11–E15、E20；组件规范见 `01-组件规范与Token对齐-阶段8增量.md`

---

## 1. 页面布局（正常态）

```
┌──────────────────────────────────────────────────────────────────────┐
│ PageHeader（同 Tab1，无部门筛选器）                                    │
├──────────────────────────────────────────────────────────────────────┤
│ Tabs(line): [活跃度与渗透] [Agent效能] [Git贡献*] [DORA]               │
├──────────────────────────────────────────────────────────────────────┤
│ ① KPI 卡组（4 张）                                                   │
│  活跃仓库数 │ PR积压 │ 平均合并周期MTTM(P50) │ ELOC总量(人/机)           │
├──────────────────────────────────────────────┬───────────────────────┤
│ ② ELOC 排名（半宽）                           │ ③ 提交质量雷达（半宽）   │
│  [Segmented: 按人* / 按Agent]                 │  [仓库下拉: 全部仓库*▾] │
│  人/机汇总条 + Top10 双色排行                  │  雷达图三轴：           │
│  （点击行→提交明细悬浮）                       │   覆盖率 / 漏洞 / 重复率 │
├──────────────────────────────────────────────┴───────────────────────┤
│ ④ 代码仓活跃分布（全宽）                                              │
│  DataTable：仓库 │ 活跃度 │ PR积压 │ 合并周期P50 │ 合并周期P95          │
│  列头可排序 · 行点击 → PR 明细下钻 Dialog                             │
└──────────────────────────────────────────────────────────────────────┘
```

- 内容容器：`mx-auto max-w-6xl space-y-5 p-6`；半宽两卡 `grid grid-cols-1 lg:grid-cols-2 gap-5`（对齐 Tab1/Tab2）。

---

## 2. 模块① KPI 卡组

### 2.1 正常态

容器：`grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4`

| 卡 | label | value（KpiCard） | hint |
| --- | --- | --- | --- |
| 1 | 活跃仓库数 | `5`（窗口内 `activity>0` 的仓库去重数） | 共 6 个已接入仓库 |
| 2 | PR积压 | `3`（`items[].open_pr_backlog` 求和） | 窗口期末 open PR |
| 3 | 平均合并周期MTTM(P50) | `1天`（各仓库 `mtm_p50_seconds` 中位数，`formatDuration`） | 窗口内 merged PR |
| 4 | ELOC总量(人/机) | `48.2k`（`total_eloc`，`CompactNumberFlow`） | 人 32.1k / 机 16.1k |

- value 包 `NumberFlow` / `CompactNumberFlow`；第 4 卡 hint 人/机拆分用双色（人=`--color-chart-1`、机=`--color-chart-2` 的小色点 + `text-xs muted`）。
- 第 1/4 卡 value 强调 `accent="brand"`；第 3 卡时长用 `DurationNumberFlow`。

### 2.2 引导态（E11：Git 数据源全部未接入）

- KPI 卡组**逐卡渲染**：数值全部 `-`（`total_deployments` 类比 `activity:0`、`open_pr_backlog:0`、`mtm_p50_seconds:null`、`total_eloc:0`——但 `ready=false` 时前端显示 `-` 更明确，见 `01` §3）；卡组下方/卡片内附 `SourceGuideState` 引导态说明（「Git 数据源未接入」+「前往配置」按钮）。
- **KPI 卡组不整体引导态**——数值逐卡 `-`，引导态说明放 Tab3 页面顶部一个 `Alert variant="secondary"` 或各卡片 `CardDescription`（推荐：页面顶部单条说明条，E8 逐卡片不阻塞）。

### 2.3 加载态 / 错误态

- 加载：卡组整体 `Spinner` 居中；窗口切换保留旧值 + 原位 `size-3.5` spinner。
- 错误：`Alert variant="destructive"`「加载失败」+ `{error}` + 「重试」`Button variant="outline" size="sm"`。

---

## 3. 模块② ELOC 排名（G1）

### 3.1 正常态

- 容器：`Card`，`CardHeader`：`CardTitle`「ELOC 排名」+ `CardAction` 放 `Segmented`「按人 / 按Agent」（默认「按人」，对应 API `group_by=member`；切换即重拉 G1）。
- **顶部汇总条**（`grid grid-cols-3 divide-x border-b px-4 py-2`）：
  - 人：色块（`size-2.5 rounded-full bg-chart-1`）+「人」`text-xs muted` + `human_eloc`（`CompactNumberFlow` `text-sm font-medium tabular-nums`）+ 占比 `text-[10px] muted`；
  - Agent：色块 `bg-chart-2` +「Agent」+ `agent_eloc` + 占比；
  - 总量：`total_eloc`（`text-sm font-medium`）+ 标签「总量」`text-xs muted`。
- **排行列表**（`divide-y`，Top10，默认 `eloc` 降序）：`ElocRankingList`（`01` §2.1）：
  - 行：序号 `text-xs muted tabular-nums` + `ActorAvatar`（人 `size="sm"`；Agent 加 `Bot` 角标 `size-4`）+ 名 `text-sm font-medium`（有效 `entity_id` → `AppLink`）+ `eloc`（`text-xs tabular-nums` 右对齐）+ 占比 `text-xs muted`；
  - 人机双色条形：`Progress` `h-1.5`，人 `bg-chart-1`、机 `bg-chart-2`，`value=ratio×100`；
  - 行 `hover:bg-muted`，点击 → `HoverCard`/`Popover` 提交明细（SHA `--font-mono text-xs`、时间、涉及仓库 `Badge`、仓库路径）。
- 数据来源角标：`CardAction` `Badge variant="secondary"`「AST 代码当量」+ 右侧 `Badge variant="secondary"`「数据截至 {updated_at 日期}」（E20 快照时效）。

### 3.2 引导态（E13：ELOC 工具链未配置）

- `G1.source_status.ready=false` → 图表区替换 `SourceGuideState`（`Code2` icon + 「ELOC 代码当量工具链未配置」+ 「前往配置」按钮）。
- 汇总条与排行隐藏；**不阻塞** ③④ 其他卡片（E8）。

### 3.3 空态（E12：窗口内无提交）

- `ready=true` 且 `items:[]` → `Empty`：
  - `EmptyMedia`：`GitCommitHorizontal`（`size-8`）
  - `EmptyTitle`：「窗口内暂无 Git 提交数据」
  - `EmptyDescription`：「当前时间窗口内没有提交记录，切换时间范围或等待代码提交后再试。」

### 3.4 加载态 / 错误态

- 加载：内容区 `Spinner`；切换「按人/按Agent」时保留旧列表 + 原位 spinner。
- 错误：`Alert destructive` + 重试。

### 3.5 边界（E15）

- 作者无法映射到成员/Agent → 归入「未归属」行（`entity_id="unmapped"`），`opacity-60` + `Badge variant="secondary"`「未归属」，置列表底部，不丢数据不报错。
- `total_eloc=0` → `ratio:null` → 条形不渲染、占比 `-`。

---

## 4. 模块③ 提交质量雷达（G2）

### 4.1 正常态

- 容器：`Card`，`CardHeader`：`CardTitle`「提交质量雷达」+ `CardAction` 放仓库 `Select`（默认「全部仓库」）+ 快照角标 `Badge variant="secondary"`「数据截至 {snapshot_at 日期}」（E20）。
- 图表：`QualityRadar`（`01` §2.2，Recharts `RadarChart`）：
  - 三轴：测试覆盖率（`coverage`×100，正轴）、静态扫描漏洞数（`vulnerabilities`，反轴）、代码重复率（`duplication_rate`×100，反轴）；
  - 反轴轴标签旁 `< 更优` 角标 `text-[10px] text-muted-foreground`；
  - `Radar` fill `--color-chart-3` / 30% 透明度，stroke `--color-chart-3`；
  - Tooltip：`ChartTooltipContent` 显示原始值（覆盖率 `72%`、漏洞 `14`、重复率 `9%`）。
- 仓库下拉切换 → 重拉 G2（`repo=all` 或具体仓库），雷达/角标同步更新。

### 4.2 引导态（E14：扫描工具无快照）

- `G2.source_status.ready=false` → `SourceGuideState`（`ScanLine` icon + 「提交质量扫描未配置」+ 描述「提交质量扫描未配置，暂无覆盖率/漏洞/重复率数据」+ 「前往配置」按钮）。

### 4.3 空态（已配置但目标仓库无快照）

- `ready=true` 但 `snapshot_at:null` → `Empty`：
  - `EmptyMedia`：`ScanLine`（`size-8`）
  - `EmptyTitle`：「该仓库暂无扫描快照」
  - `EmptyDescription`：「该仓库尚未导入扫描结果，覆盖率/漏洞/重复率数据将随扫描报告导入后展示。」

### 4.4 加载态 / 错误态

- 加载：内容区 `Spinner`。
- 错误：`Alert destructive` + 重试。

### 4.5 边界（E20）

- 快照时间戳超出窗口 → 仍按最新快照展示 + 「数据截至 {date}」角标（不做硬 TTL）。
- 数值异常（覆盖率为 0、漏洞数为 0）不改变图形，仅数值展示（PRD §5.7）。

---

## 5. 模块④ 代码仓活跃分布（G3 + G4 下钻）

### 5.1 正常态

- 容器：`Card`，`CardHeader`：`CardTitle`「代码仓活跃分布」+ `CardDescription`「窗口内提交/PR 活动 · 点击行下钻 PR 明细」。
- 表格：`RepoActivityTable`（`01` §2.3，复用 `DataTable`/`Table`）：

| 列 | 字段 | 渲染 |
| --- | --- | --- |
| 仓库 | `repo` | `GitBranch size-3.5 muted` + `--font-mono text-xs` |
| 活跃度 | `activity` | `text-sm font-medium tabular-nums` + 下注 `text-[10px] muted`「提交{active_commits} · PR{active_prs}」 |
| PR积压 | `open_pr_backlog` | `text-xs tabular-nums`（0 → muted） |
| 合并周期P50 | `mtm_p50_seconds` | `text-xs tabular-nums`，`formatDuration`（天/时）；`null → '-'` |
| 合并周期P95 | `mtm_p95_seconds` | 同上 |

- 列头可点击排序（`DataTableColumnHeader`），默认「活跃度」降序。
- 行 `hover:bg-muted`，点击 → `Dialog` 该仓库 PR 明细（G4）：
  - `DialogTitle`「{repo} PR 明细」+ `DialogDescription`「窗口内 PR 记录 · {n} 条」；
  - `Table` 列：PR 号 `#12`（`--font-mono`）│ 标题 `truncate` │ 状态 `Badge`（merged=`success`「已合并」/ open=`warning`「开启中」/ closed=`secondary`「已关闭」/ draft=`secondary`「草稿」）│ 创建/合并时间 `text-xs muted` │ 作者 `text-xs muted`；
  - 行点击 PR → `AppLink` 跳 GitCode PR 页（`html_url`）；
  - 空列表 `Empty`「该仓库暂无 PR 记录」。

### 5.2 引导态（E11）

- `G3.source_status.ready=false` → 表格区替换 `SourceGuideState`（`GitBranch` + 「Git 数据源未接入」+ 描述「请在「仓库接入」中连接 GitCode / 配置本地仓库路径」+ 「前往配置」按钮）。

### 5.3 空态（E12）

- `ready=true` 且 `items:[]` → `Empty`：
  - `EmptyMedia`：`GitBranch`（`size-8`）
  - `EmptyTitle`：「窗口内暂无 Git 活动数据」
  - `EmptyDescription`：「仓库已接入但当前时间窗口内没有提交或 PR 活动，切换时间范围或等待活动后再试。」

### 5.4 加载态 / 错误态

- 加载：内容区 `Spinner`。
- 错误：`Alert destructive` + 重试。

---

## 6. 全模块状态矩阵（Tab3）

| 模块 | 正常 | 引导态（ready=false） | 空态（ready=true 无数据） | 加载 | 错误 |
| --- | --- | --- | --- | --- | --- |
| ① KPI 卡组 | 数值 | 逐卡 `-` + 顶部说明条 | 逐卡 `0`/`-` | Spinner 居中 | Alert+重试 |
| ② ELOC 排名 | 汇总条+双色排行+下钻 | `SourceGuideState` 工具链未配置 | `Empty` 窗口暂无提交 | Spinner | Alert+重试 |
| ③ 提交质量雷达 | 三轴雷达+快照角标 | `SourceGuideState` 扫描未配置 | `Empty` 该仓库暂无快照 | Spinner | Alert+重试 |
| ④ 代码仓活跃分布 | 排序表格+PR下钻 | `SourceGuideState` Git 未接入 | `Empty` 窗口暂无活动 | Spinner | Alert+重试 |

**异常场景映射**：E11（Git 全未接入）→ 四模块引导态；E12（已接入无活动）→ ②③④空态 + ①逐卡；E13（ELOC 未配置）→ 仅②引导态；E14（扫描无快照）→ 仅③引导态；E15（作者未归属）→ ②「未归属」行；E20（快照过期）→ ②③角标；E7/E8/E9 同 Tab1。

---

## 7. 与 PRD 的口径对应（前端数据映射提示）

| PRD § | 设计落点 | API 字段 |
| --- | --- | --- |
| §4.3.1 ① | KPI 卡组 | G3 `items` 聚合 / G1 `total_eloc` |
| §5.6 ELOC 排名 | ② 人/机切换 + 排行 | G1 `group_by` / `items[]` |
| §5.7 质量雷达 | ③ 三轴 + 快照角标 | G2 `coverage/vulnerabilities/duplication_rate/snapshot_at` |
| §5.8 代码仓活跃 | ④ 排序表 + 下钻 | G3 `items[]` / G4 `items[]` |
