# 全局组件规范与设计 Token 对齐说明（阶段8 增量）

> 对齐对象：`packages/ui`（shadcn + Base UI）+ `docs/design.md`
> 原则：**零硬编码**。所有样式必须由 token / 组件类驱动；新增组件必须符合 design.md 规范。
> 范围：本文件仅覆盖 **CLO-238 阶段8 新增组件**；v1.0（CLO-236）已交付组件规范（`AnalyticsHeatmap` / `ExecutionFunnel` / `DualLineChart` / `BlockerTable` / `AnalyticsPlaceholder`）原样保留。

---

## 0. 基线沿用（阶段8 继续遵守）

- **Token 基线**：颜色（`surface` / `surface-raised` / `muted` / `foreground` / `muted-foreground` / `border` / `primary` / `brand` / `success` / `warning` / `destructive` / `info` / `chart-1..5`）、字号（`text-xs/sm/base`，图表内 `text-[10px]/[11px]` 例外沿用 charts 惯例）、间距（4px 网格）、圆角（`rounded-sm/md/lg/xl/full`）、动效（150-200ms `ease-out`）——全部同 `01-组件规范与Token对齐.md`（v1.0）§1。
- **字重纪律**：只用 `font-normal` / `font-medium`；KPI 数值沿用现有 `KpiCard` 的 `font-semibold` 既有惯例。
- **空值渲染规则**（API 契约 §0.5）：`null → '-'`、`[] → 空态文案`、缺省字段按类型默认、`0 → 数值零`。
- **颜色使用纪律**：每屏语义色 ≤2-3 种；大面积着色用 10-20% 透明度变体；同屏文字 ≤3 层。

---

## 1. 「数据源未接入」引导态统一组件 `SourceGuideState`（阶段8 核心新增）

> 触发：接口顶层 `source_status.ready === false`（API 契约 §0.7）。`ready=false` 时**渲染引导态而非空态**——引导态表达「功能存在但数据源未接」，空态表达「窗口内无数据」。两者视觉同构但语义不同，文案与操作入口有差异。

### 1.1 结构（基于 `Empty` 组件扩展）

```
SourceGuideState
├─ EmptyHeader
│   ├─ EmptyMedia   （Lucide 图标，size-8，text-muted-foreground）
│   └─ Badge        「数据源未接入」variant="secondary"  （或按模块：Git/DORA/LDAP）
├─ EmptyTitle       「{模块名}数据未接入」
├─ EmptyDescription `source_status.reason`（API 返回的人读原因，如「Git 数据源未接入」）
└─ EmptyFooter（可选）Button variant="outline" size="sm"「前往配置」
```

- 组件路径：`packages/ui/components/ui/source-guide-state.tsx`（或 `packages/views/analytics/source-guide-state.tsx`，前端按现有目录惯例放置）。
- `EmptyFooter` 是 `Empty` 的扩展子区（`empty.tsx` 目前无 footer，前端扩展或并列 `CardContent` 底部按钮位）。
- 「前往配置」按钮跳转：Git → 「仓库接入」设置页；DORA → 部署流水线配置页（待后端路由）；LDAP → 身份/部门映射管理页（`API契约` L2 管理入口）。目标路由未定前，按钮用 `Button variant="outline"` 链接到对应设置区占位路由。

### 1.2 各模块引导态实例（PRD §7 文案）

| 模块 | 图标 | Badge | EmptyTitle | EmptyDescription（=`reason`） | 配置按钮 |
| --- | --- | --- | --- | --- | --- |
| Tab3 全模块 | `GitBranch` | Git | 「Git 数据源未接入」 | 「请在「仓库接入」中连接 GitCode / 配置本地仓库路径」 | 「前往配置」 |
| Tab3 ② ELOC | `Code2` / `Ruler` | Git | 「ELOC 代码当量工具链未配置」 | 「ELOC 代码当量工具链未配置」 | 「前往配置」（ELOC 工具链设置） |
| Tab3 ③ 质量雷达 | `ScanLine` / `ShieldCheck` | Git | 「提交质量扫描未配置」 | 「提交质量扫描未配置，暂无覆盖率/漏洞/重复率数据」 | 「前往配置」（扫描报告导入） |
| Tab4 ② 交付周期 | `Timer` | DORA | 「部署流水线数据未接入」 | 「可通过 Webhook / 文件导入 / 手动录入接入。当前以「Issue→Merged」替代口径展示交付周期。」 | 「前往配置」（部署事件上报设置） |
| Tab4 ③④ | `Rocket` / `TriangleAlert` | DORA | 「部署流水线数据未接入」 | 同 ② | 「前往配置」 |
| Tab1 ⑥⑦⑧ | `UsersRound` / `ShieldCheck` | LDAP | 「用户身份数据源未接入」 | 「接入 LDAP/IDP 或配置部门映射后启用」 | 「前往配置」（身份/部门映射管理） |

### 1.3 视觉规范

| 项 | 值 |
| --- | --- |
| 容器 | 卡片内容区居中，`py-8`；与 `Empty` 全高居中一致 |
| 图标 | Lucide `size-8 text-muted-foreground` |
| 标题 | `EmptyTitle`（`text-sm font-medium`，design.md 调整） |
| 描述 | `EmptyDescription`（`text-xs text-muted-foreground`） |
| 按钮 | `Button variant="outline" size="sm"`，主按钮每模块 1 个（PRD §8：引导态附加说明文案 + 可跳转配置入口的按钮） |
| 时间角标 | 有 `updated_at` 时附加 `text-[10px] text-muted-foreground`「最近同步 {date}」 |

---

## 2. 新增组件规范（阶段8）

以下组件前端实现前需确认，必须符合 design.md token 规范，建议放 `packages/views/analytics/` 或 `packages/views/runtimes/components/charts/`。

### 2.1 `ElocRankingList`（ELOC 排名，Tab3 ②，API G1）

- **结构**：`CardHeader`（`CardTitle`「ELOC 排名」+ `CardAction` 放 `Segmented`「按人 / 按Agent」）+ 顶部汇总条 + 横向排行列表。
- **顶部汇总条**（`grid grid-cols-3 divide-x border-b`）：
  - 人产 ELOC：`human_eloc`，数值 `text-sm font-medium tabular-nums` + 色块（`bg-chart-1`）；标签「人」`text-xs muted`；
  - Agent 产 ELOC：`agent_eloc`，色块（`bg-chart-2`）；标签「Agent」；
  - 总量：`total_eloc`，`ratio` 显示「人/机占比 xx% / xx%」。
- **排行列表**（`divide-y`，Top10，默认按 `eloc` 降序）：
  - 每行 `grid grid-cols-[minmax(0,1.6fr)_auto] gap-3 items-center px-4 py-2`：
    - 列 1：排名序号（`text-xs text-muted-foreground tabular-nums`）+ 头像/标识（人=`ActorAvatar`，Agent=`ActorAvatar`+`Bot` 角标 `size-4`）+ 名 `text-sm font-medium`；
    - 列 2：`eloc` 数值 `text-xs tabular-nums text-right` + 占比 `text-xs text-muted-foreground`。
  - **人/机双色**：行内条形进度条（可选，`Progress` `h-1.5`）——人 `bg-chart-1`、机 `bg-chart-2`，宽度 = `ratio`；用于人机对比可视。
  - 「未归属」行（`entity_id === "unmapped"`）：名显示「未归属」，`opacity-60`，`Badge variant="secondary"`「未归属」置底（E15）。
- **交互**：点击行 → `Popover`/`HoverCard` 悬浮提交明细（提交 SHA `--font-mono text-xs`、时间、涉及仓库 `Badge`、仓库路径）；`entity_id` 有效时点击名 → `AppLink` 跳成员/Agent 页。
- **空态**：`source_status.ready=false` → `SourceGuideState`（「ELOC 代码当量工具链未配置」）；`ready=true` 且 `items:[]` → `Empty`「窗口内暂无 Git 提交数据」。
- **边界**：`total_eloc=0` 时 `ratio:null` → 条形不渲染，占比 `-`。

### 2.2 `QualityRadar`（提交质量雷达，Tab3 ③，API G2）

- **结构**：`CardHeader`（`CardTitle`「提交质量雷达」+ `CardAction` 放仓库下拉 `Select`，默认「全部仓库」）+ 雷达图 + 快照角标。
- **图表**：Recharts `RadarChart`，三轴：
  - **测试覆盖率** `coverage`（0-100%，`×100`）：值越大越好 → 正轴；
  - **静态扫描漏洞数** `vulnerabilities`：越少越好 → **反轴**（前端 `100 - 归一化` 或显式反轴标注）；
  - **代码重复率** `duplication_rate`（0-100%）：越低越好 → **反轴**。
  - 归一化说明：漏洞数与覆盖率/重复率量纲不同，建议 `vulnerabilities` 按 `min(v, 50)/50` 或最大参考值归一（联调阶段确定，标注见 `05`）。
  - `Radar` 填充 `--color-chart-3` 透明度 `fill chart-3/30`，`stroke chart-3`。
- **反轴处理**：轴标签旁加 `< 更优` 角标（`text-[10px] text-muted-foreground`）；tooltip 显示原始值（覆盖率 `%`、漏洞数 `n`、重复率 `%`），不显示归一化值。
- **快照角标**：`snapshot_at` 存在 → 卡片右上角 `Badge variant="secondary"`「数据截至 {date}」（E20）。
- **仓库下拉**：`Select`（`packages/ui/components/ui/select.tsx`），选项 = 仓库列表（来自 G3 或 G2 `repo`），默认「全部仓库」（`repo="all"` 汇总口径）。切换 → 重拉 G2 带 `repo` 参数。
- **交互**：雷达 `hover` 显示各轴值（`ChartTooltipContent`）；数值异常（覆盖率 0 / 漏洞 0）不改变图形，仅数值展示（PRD §5.7）。
- **空态**：`ready=false` → `SourceGuideState`（「提交质量扫描未配置」）；`ready=true` 但 `snapshot_at:null` → `Empty`「该仓库暂无扫描快照」。
- **边界**：快照时间戳超出窗口 → 仍展示最新快照 + 「数据截至 {date}」角标。

### 2.3 `RepoActivityTable`（代码仓活跃分布，Tab3 ④，API G3 + G4 下钻）

- **结构**：`CardHeader`（`CardTitle`「代码仓活跃分布」+ `CardDescription`「窗口内提交/PR 活动 · 按活跃度降序」）+ `DataTable`。
- **列**（`Table`，列头可点击排序，默认活跃度降序）：

| 列 | 字段 | 渲染 |
| --- | --- | --- |
| 仓库 | `repo` | `--font-mono text-xs`（owner/name）+ 行首仓库图标 `GitBranch size-3.5 text-muted-foreground` |
| 活跃度 | `activity` | `text-xs tabular-nums`（`text-sm font-medium`）+ 说明 `text-[10px] muted`「提交{active_commits} · PR{active_prs}」 |
| PR积压 | `open_pr_backlog` | `text-xs tabular-nums`；`0` 显示 muted 灰 |
| 合并周期P50 | `mtm_p50_seconds` | `text-xs tabular-nums`，`formatDuration` 秒→天/时（如 `1天` / `12时`）；`null → '-'` |
| 合并周期P95 | `mtm_p95_seconds` | 同上 |

- **排序**：列头 `DataTableColumnHeader`（复用 `data-table-column-header.tsx`）可点击切换升降序。
- **交互**：行 `hover:bg-muted`，行点击 → `Dialog` 该仓库 PR 明细下钻（G4）：
  - `DialogTitle`「{repo} PR 明细」；
  - 列表/`Table`：PR 号 `#12`（`--font-mono`）│ 标题 `truncate` │ 状态 `Badge`（merged=`success` / open=`warning` / closed=`secondary` / draft=`secondary`）│ 创建/合并时间 `text-xs muted` │ 作者 `text-xs muted`；
  - 空列表 `Empty`「该仓库暂无 PR 记录」；`pr_created_at` 早于窗口但 `merged_at` 在窗口内的 PR 仍计入合并周期（口径 §3.3.3）。
- **空态**：`ready=false` → `SourceGuideState`（「Git 数据源未接入」）；`ready=true` 且 `items:[]` → `Empty`「窗口内暂无 Git 活动数据」。
- **边界**：合并周期仅统计窗口内 `merged_at` 落入窗口的 merged PR；`mtm_p50/p95:null` → `-`。

### 2.4 `DoraTrendChart`（交付周期趋势，Tab4 ②，API D1）

- **结构**：`CardHeader`（`CardTitle`「交付周期趋势」+ `CardAction` 放口径切换 `Segmented`「Issue→部署 / Issue→Merged」）+ 双折线。
- **口径切换与自动降级**（关键逻辑）：
  - 默认口径 `deploy`（Issue→部署）；页面加载时若 D1（`metric=deploy`）返回 `source_status.ready=false` → 前端**自动切换** `metric=merged` 并显示 `Badge variant="warning"`「替代口径：Merged」（E16）；
  - `Segmented` 两个口径均可手动切换；切 `deploy` 时若仍 `ready=false` → 自动回落 `merged` + 角标；
  - 角标语义：`metric=merged` 且 `deploy` 未接入 → 显示「替代口径：Merged」；`deploy` 已接入 → 不显示角标，Segmented 正常两态。
- **图表**：`DualLineChart` 复用（`01` v1.0 §3.3）按周：
  - X 轴：`points[].week`（周起始日，周一）；Y 轴：交付周期（秒，前端 `formatDuration`）；
  - P50 线 `--color-chart-1`，P95 线 `--color-chart-4`（与 v1.0 双折线区分，P95 用渐亮色阶）；
  - `connectNulls={false}`（该周无样本断线）；
  - Tooltip：`ChartTooltipContent` 显示周 + P50/P95 + 样本数 `sample_count`（`· n 样本`）。
- **空态**：`ready=false` 且无 merged 数据 → `SourceGuideState`（「部署流水线数据未接入，且无 VCS 合并数据」）；`points:[]` → `Empty`「窗口内暂无交付周期样本」。
- **边界**：`p50_seconds/p95_seconds:null` → 断线不连点；`sample_count:0` 正常展示（有周无样本）。

### 2.5 `DeployFrequencyChart`（部署频率趋势，Tab4 ③，API D2）

- **结构**：`CardHeader`（`CardTitle`「部署频率趋势」+ `CardDescription`「按周部署次数」）+ 柱状图。
- **图表**：Recharts `BarChart`：X=周，Y=部署次数；柱 `--color-chart-2`；`radius={4}`（`rounded-sm` 视觉）；hover `ChartTooltipContent` 显示周 + 次数 + 失败数。
- **空态**：`ready=false` → `SourceGuideState`（「部署流水线数据未接入」）；`trend:[]` → `Empty`「窗口内暂无部署记录」。

### 2.6 `FailureDetailTable`（变更失败率与 MTTR 明细，Tab4 ④，API D2）

- **结构**：`CardHeader`（`CardTitle`「变更失败率与 MTTR」）+ 双指标折线 + 失败明细表。
- **双指标折线**：`DualLineChart` 复用，X=周，Y 左轴=变更失败率（%，`failure_rate`×100，`--color-chart-1`），Y 右轴=MTTR（秒→时，`mttr_seconds`，`--color-chart-2`）；`connectNulls={false}`。
- **失败明细表**（`Table`，`failure_rate`/`mttr` 有值时展开；顶部 KPI 位显示 变更失败率 / MTTR 汇总）：

| 列 | 字段 | 渲染 |
| --- | --- | --- |
| 部署ID | `deployment_id` | `--font-mono text-xs` |
| 应用 | `app` | `text-xs`；`null → '-'` |
| 失败时间 | `failed_at` | `text-xs muted` |
| 恢复时间 | `recovered_at` | `text-xs muted`；`null → '进行中'` |
| MTTR | `mttr_seconds` | `text-xs tabular-nums`，`formatDuration`；`null → '-'` |
| 原因 | `reason` | `text-xs`（`truncate max-w-[16rem]`，`hover` 全量 `Tooltip`）；`null → '-'` |
| 状态 | `status` | `Badge`：resolved=`success`「已恢复」 / open=`warning`「进行中」 |

- **筛选**：顶部 `Segmented`：全部 / 已恢复 / 进行中（客户端过滤 `failures[].status`）；MTTR 列头排序（默认降序）。
- **空态**：`ready=false` → `SourceGuideState`；`trend:[]` 且 `failures:[]` → `Empty`「窗口内暂无部署记录」；`total_deployments>0` 但 `failed_deployments=0` → 图表正常（失败率 0），明细表 `Empty`「暂无失败样本，变更失败率与 MTTR 待累积」。

### 2.7 `LifecycleCard`（用户生命周期卡，Tab1 ⑥，API L1）

- **结构**：`CardHeader`（`CardTitle`「用户生命周期」+ `CardAction` `Badge variant="secondary"`「LDAP/手动」）+ 两组指标 + 最近事件列表。
- **指标**（`CardContent space-y-4`）：
  - **入职开通率**：`onboarding_rate`×100（`text-2xl font-medium tabular-nums`，`accent="brand"` 或默认）+ 说明「新入职 {new_hires_total} · 已开通 {onboarded_members}」`text-xs muted`；`null → '-'`（无新入职样本，口径 §1.4.1）；
  - **离职权限熔断**：`cutoff_events` 次数 `text-2xl font-medium tabular-nums` + 说明「窗口内离职权限熔断事件数」；`0 → 0`。
- **最近事件列表**（`divide-y`，Top5，`recent_events`）：`event_type` → `Badge`（onboard=`success`「入职开通」/ offboard=`secondary`「离职」/ cutoff=`warning`「权限熔断」）+ 成员名 `text-sm` + 时间 `text-xs muted` + 结果（success=`success`「成功」/ failed=`destructive`「失败」/ null=`muted`「-」）。
- **交互**：点击卡片 → `Dialog` 明细列表（成员、事件类型、时间、结果）。
- **空态**：`ready=false` → `SourceGuideState`（「用户身份数据源未接入」）；`ready=true` 且 `recent_events:[]` → `Empty`「窗口内无入职/离职事件」（PRD §5.10）。

### 2.8 `CutoffSuccessCard`（权限熔断成功率卡，Tab1 ⑦，API L1）

- **结构**：`CardHeader`（`CardTitle`「权限熔断成功率」）+ 单指标。
- **指标**：`cutoff_success_rate`×100（`text-2xl font-medium tabular-nums`，`accent="success"`）+ `Progress`（`value=rate×100`，`bg-success`）+ 说明「熔断成功 {cutoff_success_count} / 应熔断 {cutoff_events}」`text-xs muted`；`null → '-'`（`cutoff_events=0` 无分母）。
- **空态**：同 `LifecycleCard`。

### 2.9 `DepartmentSlicer`（部门维度切片，Tab1 ⑧，API L2 + 各接口 `department_id`）

- **触发条件（条件区块）**：`GET /api/analytics/identity/departments` 返回 `source_status.ready=true` 时才渲染本区块与 PageHeader 部门筛选器（PRD §4.6）；`ready=false` → 区块隐藏（不渲染空态，直接不显示，部门筛选器也隐藏）。
- **结构**：`CardHeader`（`CardTitle`「部门维度切片」+ `CardAction` `Segmented` 或 `Select`：部门列表 / 「全部」）+ 部门对比列表。
- **列表**（`divide-y`，按 `member_count` 或 `active_members` 降序）：部门名 `text-sm font-medium` + 成员数/活跃成员数 `text-xs tabular-nums muted` + 活跃度进度条（`active_members/member_count`，`bg-chart-4`）。
- **交互**：选择部门 → Tab1 全部模块（热力图/渗透率/生命周期/熔断成功率）与 Tab2 执行漏斗按该部门成员子集重新聚合（各接口带 `department_id`）；Tab3/Tab4 不受影响（PRD §4.6）。选中部门在 PageHeader 部门筛选器同步。
- **空态**：`ready=true` 但 `items:[]` → `Empty`「暂无部门数据」；所选部门无成员/无数据 → 相关卡片空态，部门下拉仍可用，返回「全部」恢复全局口径（E19）。

### 2.10 `DepartmentFilter`（PageHeader 部门筛选器，条件显示，API L2）

- **触发**：L2 `source_status.ready=true` 时显示；`ready=false` 时隐藏（PRD §4.6 / E18）。
- **位置**：PageHeader 右侧，时间维度 Segmented 之后、时区 Select 之前。
- **视觉**：`Select`（`native-select.tsx` 或 `select.tsx`），选项「全部」+ 部门列表（L2 `items[].name`），value=`department_id` 或 `all`；选中态 `text-sm font-medium`。
- **交互**：切换 → 仅重查 Tab1/Tab2 接口（带 `department_id`），Tab3/Tab4 缓存不重查；未接入部门数据时传 `department_id` → 400（API §0.2），前端以 `ready=false` 隐藏筛选器规避。

---

## 3. 阶段8 空值渲染规则（延续 API §0.5 + `source_status`）

| 场景 | 渲染 |
| --- | --- |
| `source_status.ready=false` | **引导态**（`SourceGuideState`，含配置入口按钮），非空态 |
| `ready=true` 且业务字段 `null` | `-`（无分母/无样本） |
| `ready=true` 且业务字段 `[]` | 对应模块空态（`Empty`） |
| `ready=true` 且业务字段 `0` | 数值 `0`（非空态信号） |
| `updated_at` 存在 | 数据同步时间，引导态/数据态均可显示「最近同步 {date}」 |

---

## 4. 图标资源（阶段8 增量，Lucide）

| 场景 | 图标 | 尺寸 |
| --- | --- | --- |
| Tab3 Git 数据源引导态 | `GitBranch` | `size-8` |
| ELOC 工具链引导态 | `Code2` / `Ruler` | `size-8` |
| 质量扫描引导态 | `ScanLine` / `ShieldCheck` | `size-8` |
| DORA 部署引导态 | `Rocket` | `size-8` |
| 交付周期引导态 | `Timer` / `Activity` | `size-8` |
| LDAP 身份引导态 | `UsersRound` | `size-8` |
| 熔断成功率卡 | `ShieldCheck` / `ShieldOff` | `size-4`（状态图标） |
| 仓库行 | `GitBranch` | `size-3.5` |
| Agent 标识 | `Bot` | `size-4` |
| 部门切片 | `Building2` / `Users` | `size-8`（空态）/ `size-4` |

禁止混用其他图标库；禁止自制 SVG（Lucide 无合适替代除外）。

---

## 5. 新增资源清单（阶段8）

| 资源 | 类型 | 说明 |
| --- | --- | --- |
| `SourceGuideState` | 组件 | 「数据源未接入」引导态统一组件（§1） |
| `ElocRankingList` | 组件 | ELOC 排名人/机排行（§2.1） |
| `QualityRadar` | 组件 | 提交质量雷达（§2.2），Recharts `RadarChart` |
| `RepoActivityTable` | 组件 | 代码仓活跃分布 + PR 下钻（§2.3） |
| `DoraTrendChart` | 组件 | 交付周期趋势 + 口径切换/降级（§2.4） |
| `DeployFrequencyChart` | 组件 | 部署频率柱状图（§2.5） |
| `FailureDetailTable` | 组件 | 变更失败率/MTTR + 失败明细（§2.6） |
| `LifecycleCard` / `CutoffSuccessCard` | 组件 | 生命周期 / 熔断成功率卡（§2.7/2.8） |
| `DepartmentSlicer` / `DepartmentFilter` | 组件 | 部门切片区块 / PageHeader 筛选器（§2.9/2.10） |
| `AnalyticsPage` 更新 | 页面壳 | PageHeader 增加条件部门筛选器；Tab 结构不变 |
| i18n 文案 | 语言包 | analytics 命名空间新增：Tab3/Tab4/生命周期/部门/引导态/口径角标/明细表列头等 |

**无切图资源需求**：全站图标用 Lucide，无自绘位图/插画（引导态也用 Lucide 大图标，符合 design.md §6）。
