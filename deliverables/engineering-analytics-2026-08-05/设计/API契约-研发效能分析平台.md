# API 契约：研发效能分析平台（阶段7补充版 v2.0）

> 文档编号：API-CLO-228（阶段7补充版 v2.0）
> 版本：v2.0（2026-08-05）——v1.0（一期 10 接口）全部保留，新增 2.3 Git / 2.4 DORA / 2.1 身份与部门接口
> 需求基线：CLO-227 / 本平台 PRD-CLO-228 v2.0
> 配套文档：`PRD-研发效能分析平台.md`（v2.0）、`数据口径说明.md`（v2.0）、`可行性校验.md`（v2.0）
> 数据口径：所有指标一律遵循《数据口径说明》，本文档仅定义传输契约。
> 路由前缀：`/api/analytics`（挂工作区成员中间件，`/{slug}/analytics` 页面所在路由组）。

---

## 0. 通用约定

### 0.1 认证与鉴权

- 所有接口携带登录态（JWT cookie 或 Bearer），要求**工作区成员身份**。
- 服务端会话用户决定权限；接口不接受前端传入的用户标识。

### 0.2 请求公共参数

| 参数 | 位置 | 类型 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `days` | query | int | 否 | `30` | 1–365；非法或缺失回退 30 |
| `tz` | query | string | 否 | 用户已存时区 → UTC | IANA 时区；非法回退 |
| `project_id` | query | uuid | 否 | 整个工作区 | 按项目过滤；格式非法或不存在 → 400 |
| `department_id` | query | string | 否 | 全部 | 按部门切片（**仅 Tab1/Tab2 接口支持**）；未接入部门数据时传该值 → 400（E18 引导态） |

> 窗口语义：自然日窗口，`tz` 本地 00:00 为日界；`days=30` 覆盖「近 30 个自然日」（含今日）。

### 0.3 响应结构

- 成功：`200`，`Content-Type: application/json`，直接返回数据对象（**无包裹层**，与现有看板接口一致）。
- 失败：统一错误结构（§0.4）。
- 字段命名：snake_case。

### 0.4 错误码与错误响应结构

**HTTP 状态码语义：**

| 状态码 | 场景 |
| --- | --- |
| `400` | 请求参数非法（`project_id` 格式错误/不存在、`department_id` 在未接入时传入、`days`/`tz` 严重非法超出可回退范围） |
| `401` | 未认证 |
| `403` | 非工作区成员 / 无权限 |
| `404` | 路由不存在 |
| `500` | 服务端内部错误（查询失败等） |

**错误响应结构（统一）：**

```json
{ "error": "<human-readable message>" }
```

- `days` / `tz` 非法按 §0.2 回退默认值，**不报错**。
- 无匹配数据不报错，返回对应空值（`null` / `[]` / `0`）或 `source_status`（§0.7），语义见 §0.5。

### 0.5 空值语义（总则，逐字段遵守）

- **`null`**：指标在窗口内无有效分母或不可计算（如无 Agent 时渗透率、无 Blocker 样本时响应时长、无部署样本时 MTTR、漏斗 Merged 阶段数据源未接入）。前端渲染 `-` 或禁用态。
- **`[]`**：该维度无数据（如热力图无活跃、排行空、明细空）。前端渲染空态文案。
- **缺省（字段不存在）**：仅用于向后兼容的可选扩展字段；契约声明的字段**一律返回**。
- **`0`**：有明确语义的数值零，**不是空态信号**。空态由前端按字段组合判定。

### 0.6 数值与时间格式

- 数量类：`int64`（JSON `number`）。
- 比率类：`number`，范围 `0–1`（前端 `×100` 展示百分比）或 `null`。
- 时长类：`number`，单位**秒**；`null` 表示无样本。
- 时间：`string`，RFC3339（`2026-08-05T09:30:00Z` 或含偏移 `+08:00`），按服务端习惯输出；日期 `date` 用 `YYYY-MM-DD`。

### 0.7 外部数据源状态（v2.0 新增，G/D/L 接口必含）

G/D/L 系列接口（Git/DORA/身份）依赖外部数据源，响应体顶层一律包含 `source_status` 对象，供前端渲染「数据源未接入」引导态（PRD §7）：

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | `{ "ready": bool, "reason": string\|null, "updated_at": string\|null }` |
| `source_status.ready` | bool | — | 数据源可用 |
| `source_status.reason` | string \| null | `null`=可用 | 不可用时的人读原因（如「Git 数据源未接入」「LDAP 未配置」「部署流水线未接入」） |
| `source_status.updated_at` | string \| null | `null`=从未同步 | 最近一次数据同步/快照时间 |

> 约定：`source_status.ready=false` 时，该接口的其余业务字段返回 `null` / `[]` / `0`（按各字段语义），前端据此显示引导态而非空态。

---

## 1. 活跃度与渗透（Adoption & Activity）—— v1.0 接口 A1–A5 保留

### 接口 A1 `GET /api/analytics/activity/summary`

活跃度汇总 KPI。

**查询参数**：§0.2 公共参数（含 `department_id`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `window` | object | 恒返回 | `{ "start": date, "end": date }` 窗口起止 |
| `total_members` | int | `0` | 当前工作区（或所选部门）成员总数（分母） |
| `active_members` | int | `0` | 窗口内活跃成员去重数（活跃天数 ≥ 1） |
| `active_days_avg` | number \| null | `null`=无活跃成员 | 活跃成员平均活跃天数 |
| `dau` | int | `0` | 今日活跃成员数 |
| `mau` | int | `0` | 近 30 天活跃成员数 |
| `dau_mau_ratio` | number \| null | `null`=`mau=0` | DAU ÷ MAU |
| `active_user_ratio` | number \| null | `null`=`total_members=0` | DAU ÷ 总成员数 |
| `total_issues` | int | `0` | 窗口内 Issue 总数（创建量） |
| `per_capita_issue_volume` | number \| null | `null`=`active_members=0` | 人均 Issue 处理量（创建量） |

**响应示例（200）：**

```json
{
  "window": { "start": "2026-07-07", "end": "2026-08-05" },
  "total_members": 12,
  "active_members": 9,
  "active_days_avg": 8.3,
  "dau": 5,
  "mau": 9,
  "dau_mau_ratio": 0.556,
  "active_user_ratio": 0.417,
  "total_issues": 47,
  "per_capita_issue_volume": 5.2
}
```

---

### 接口 A2 `GET /api/analytics/activity/heatmap`

活跃热力图日×小时矩阵。`?metric=` 切换三种口径。

**查询参数**：§0.2 公共参数（含 `department_id`）+ `metric`（query, string, 默认 `activity_events`）。

| `metric` | 含义 |
| --- | --- |
| `activity_events` | 活跃事件数（默认） |
| `issue_volume` | 人均 Issue 处理量（创建量） |
| `active_days` | 活跃天数 |

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `metric` | string | 恒返回 | 回显请求口径 |
| `days` | array | `[]`=无数据 | `[{ "date": "YYYY-MM-DD", "hours": [{ "hour": 0-23, "value": int \| null }] }]`；无活跃的整日省略 |
| `max_value` | int | `0` | 全窗口最大值（前端色阶分母） |

> `hours[].value`：该口径下该日该小时的数值；`null` = 该日该小时无事件（空态不渲染深色格）。

**响应示例（200）：** 同 v1.0（见 v1.0 附件），此处省略。

---

### 接口 A3 `GET /api/analytics/activity/top-members`

成员活跃排行 Top N。

**查询参数**：§0.2 公共参数（含 `department_id`）+ `limit`（query, int, 默认 `10`, 最大 `50`）。

**响应字段：** 同 v1.0（`items[]`：member_id / name / active_days / issue_count / last_active_at）。

---

### 接口 A4 `GET /api/analytics/adoption/summary`

Agent 渗透率汇总。**查询参数**：§0.2 公共参数（含 `department_id`）。**响应字段**同 v1.0。

---

### 接口 A5 `GET /api/analytics/adoption/trend`

Agent 渗透率按日趋势。**查询参数**：§0.2 公共参数（含 `department_id`）。**响应字段**同 v1.0。

---

## 2. Agent 效能（Agent Performance）—— v1.0 接口 B1–B6 保留

### 接口 B1 `GET /api/analytics/agents/funnel`

执行漏斗（v2.0 更新 Merged 段：二期接入 VCS 后返回数值）。

**查询参数**：§0.2 公共参数（含 `department_id`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `assign_count` | int | `0` | 窗口内 Assign 数（atq 创建行数） |
| `issue_assigned_count` | int | `0` | Issue 级 Assign（`issue.assignee_type='agent'`） |
| `execute_count` | int | `0` | 窗口内 Execute 数（`started_at IS NOT NULL`） |
| `execute_ratio` | number \| null | `null`=`assign_count=0` | Execute/Assign 转化率 |
| `merged_count` | int \| null | `null`=VCS 未接入 | Merged 数（二期接入 `issue_vcs_pull_request` 且关联 PR state='merged' 后返回；未接入返回 `null`） |
| `merged_ratio` | number \| null | `null`=VCS 未接入或分母 0 | Merged/Execute 转化率 |

**响应示例（200，VCS 已接入）：**

```json
{
  "assign_count": 41,
  "issue_assigned_count": 31,
  "execute_count": 34,
  "execute_ratio": 0.829,
  "merged_count": 22,
  "merged_ratio": 0.647
}
```

**响应示例（200，VCS 未接入）：**

```json
{
  "assign_count": 41,
  "issue_assigned_count": 31,
  "execute_count": 34,
  "execute_ratio": 0.829,
  "merged_count": null,
  "merged_ratio": null
}
```

---

### 接口 B2–B6

同 v1.0（performance / top / skills overview / collaboration summary / blockers），此处省略细节，见 v1.0 附件。

---

## 3. 2.3 Git 贡献（Git Contributions）—— v2.0 新增

### 接口 G1 `GET /api/analytics/git/eloc`

ELOC 排名（AST 代码当量，人/机对比）。

**查询参数**：§0.2 公共参数（**不含** `department_id`）+ `group_by`（query, string, `member`\|`agent`, 默认 `member`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | 见 §0.7 |
| `group_by` | string | 恒返回 | 回显 `member`\|`agent` |
| `total_eloc` | int | `0` | 窗口内 ELOC 总量（人+机） |
| `human_eloc` | int | `0` | 人产 ELOC |
| `agent_eloc` | int | `0` | Agent 产 ELOC |
| `items` | array | `[]`=无数据 | `[{ "entity_id": uuid\|string, "name": string, "eloc": int, "ratio": number\|null, "commit_count": int, "repos": [string] }]` |
| `items[].entity_id` | uuid string | — | 成员/Agent ID；未归属作者为 `"unmapped"` |
| `items[].name` | string | — | 成员/Agent 名；未归属为「未归属」 |
| `items[].eloc` | int | `0` | ELOC 当量 |
| `items[].ratio` | number \| null | `null`=`total_eloc=0` | 占总 ELOC 比例 |
| `items[].commit_count` | int | `0` | 贡献提交数 |
| `items[].repos` | array | `[]`=无数据 | 涉及仓库名列表 |

**响应示例（200，按人）：**

```json
{
  "source_status": { "ready": true, "reason": null, "updated_at": "2026-08-05T07:00:00Z" },
  "group_by": "member",
  "total_eloc": 48200,
  "human_eloc": 32100,
  "agent_eloc": 16100,
  "items": [
    {
      "entity_id": "3a4c2b1e-7f80-4a91-b2c3-d4e5f6a7b8c9",
      "name": "张三",
      "eloc": 18400,
      "ratio": 0.382,
      "commit_count": 96,
      "repos": ["clubdeveloperclub", "team-engineering-governance"]
    },
    {
      "entity_id": "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
      "name": "产品经理智能体",
      "eloc": 8400,
      "ratio": 0.174,
      "commit_count": 41,
      "repos": ["wish-wall"]
    }
  ]
}
```

**引导态示例（Git 未接入）：**

```json
{
  "source_status": { "ready": false, "reason": "Git 数据源未接入", "updated_at": null },
  "group_by": "member",
  "total_eloc": 0,
  "human_eloc": 0,
  "agent_eloc": 0,
  "items": []
}
```

---

### 接口 G2 `GET /api/analytics/git/quality`

提交质量雷达（测试覆盖率 / 静态扫描漏洞 / 代码重复率）。

**查询参数**：§0.2 公共参数（**不含** `department_id`）+ `repo`（query, string, 可选；缺省=全部仓库汇总）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | 见 §0.7 |
| `repo` | string | — | 回显请求仓库；`"all"` = 全部 |
| `snapshot_at` | string \| null | `null`=无快照 | 最近一次扫描快照时间 |
| `coverage` | number \| null | `null`=无快照 | 测试覆盖率 0–1 |
| `vulnerabilities` | int \| null | `null`=无快照 | 静态扫描漏洞数（严重+高危+中危） |
| `duplication_rate` | number \| null | `null`=无快照 | 代码重复率 0–1 |

**响应示例（200）：**

```json
{
  "source_status": { "ready": true, "reason": null, "updated_at": "2026-08-05T07:00:00Z" },
  "repo": "all",
  "snapshot_at": "2026-08-05T07:00:00Z",
  "coverage": 0.72,
  "vulnerabilities": 14,
  "duplication_rate": 0.09
}
```

**引导态示例（扫描未配置）：**

```json
{
  "source_status": { "ready": false, "reason": "提交质量扫描未配置", "updated_at": null },
  "repo": "all",
  "snapshot_at": null,
  "coverage": null,
  "vulnerabilities": null,
  "duplication_rate": null
}
```

---

### 接口 G3 `GET /api/analytics/git/repos`

代码仓活跃分布（仓库活跃度 / PR 积压 / 合并周期 MTTM）。

**查询参数**：§0.2 公共参数（**不含** `department_id`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | 见 §0.7 |
| `items` | array | `[]`=无数据 | `[{ "repo": string, "active_commits": int, "active_prs": int, "activity": int, "open_pr_backlog": int, "mtm_p50_seconds": number\|null, "mtm_p95_seconds": number\|null }]` |
| `items[].repo` | string | — | 仓库名（owner/name） |
| `items[].active_commits` | int | `0` | 窗口内提交数 |
| `items[].active_prs` | int | `0` | 窗口内 PR 活动数（创建+合并+关闭） |
| `items[].activity` | int | `0` | 活跃度 = 提交数 + PR 数 |
| `items[].open_pr_backlog` | int | `0` | 窗口期末 open PR 数 |
| `items[].mtm_p50_seconds` | number \| null | `null`=窗口内无 merged PR | 合并周期 P50（秒） |
| `items[].mtm_p95_seconds` | number \| null | 同上 | 合并周期 P95（秒） |

**响应示例（200）：**

```json
{
  "source_status": { "ready": true, "reason": null, "updated_at": "2026-08-05T07:00:00Z" },
  "items": [
    {
      "repo": "kaverjody/clubdeveloperclub",
      "active_commits": 43,
      "active_prs": 7,
      "activity": 50,
      "open_pr_backlog": 3,
      "mtm_p50_seconds": 86400,
      "mtm_p95_seconds": 259200
    }
  ]
}
```

**引导态示例（Git 未接入）：**

```json
{
  "source_status": { "ready": false, "reason": "Git 数据源未接入", "updated_at": null },
  "items": []
}
```

---

### 接口 G4 `GET /api/analytics/git/prs?repo=<repo>`

（可选，二期实现）仓库 PR 明细下钻列表。

**查询参数**：§0.2 公共参数（**不含** `department_id`）+ `repo`（必填）+ `state`（query, string, 可选 `open`\|`closed`\|`merged`\|`draft`）+ `limit`（默认 `20`, 最大 `100`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | 见 §0.7 |
| `items` | array | `[]`=无数据 | `[{ "pr_number": int, "title": string, "state": string, "author_login": string\|null, "pr_created_at": string, "merged_at": string\|null, "closed_at": string\|null, "additions": int, "deletions": int, "html_url": string }]` |

**响应示例（200）：**

```json
{
  "source_status": { "ready": true, "reason": null, "updated_at": "2026-08-05T07:00:00Z" },
  "items": [
    {
      "pr_number": 12,
      "title": "feat: analytics git module",
      "state": "merged",
      "author_login": "kaverjody",
      "pr_created_at": "2026-08-01T02:10:00+08:00",
      "merged_at": "2026-08-03T09:30:00+08:00",
      "closed_at": "2026-08-03T09:30:00+08:00",
      "additions": 340,
      "deletions": 88,
      "html_url": "https://gitcode.com/kaverjody/clubdeveloperclub/pull/12"
    }
  ]
}
```

---

## 4. 2.4 DORA 效能结果 —— v2.0 新增

### 接口 D1 `GET /api/analytics/dora/lead-time`

交付周期趋势。

**查询参数**：§0.2 公共参数（**不含** `department_id`）+ `metric`（query, string, `deploy`\|`merged`, 默认 `deploy`；未接入部署流水线时前端应请求 `merged`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | 见 §0.7（`metric=merged` 时 `ready` 取决于 VCS） |
| `metric` | string | 恒返回 | `deploy`\|`merged` |
| `points` | array | `[]`=无数据 | `[{ "week": "YYYY-MM-DD", "p50_seconds": number\|null, "p95_seconds": number\|null, "sample_count": int }]` |
| `points[].week` | string | — | 周起始日（周一） |
| `points[].p50_seconds` | number \| null | `null`=该周无样本 | P50 |
| `points[].p95_seconds` | number \| null | 同上 | P95 |
| `points[].sample_count` | int | `0` | 该周交付样本数 |

**响应示例（200，merged 口径）：**

```json
{
  "source_status": { "ready": true, "reason": null, "updated_at": "2026-08-05T07:00:00Z" },
  "metric": "merged",
  "points": [
    { "week": "2026-07-27", "p50_seconds": 172800, "p95_seconds": 518400, "sample_count": 5 },
    { "week": "2026-08-03", "p50_seconds": 129600, "p95_seconds": 432000, "sample_count": 4 }
  ]
}
```

**引导态示例（部署未接入且 VCS 未接入）：**

```json
{
  "source_status": { "ready": false, "reason": "部署流水线数据未接入，且无 VCS 合并数据", "updated_at": null },
  "metric": "merged",
  "points": []
}
```

---

### 接口 D2 `GET /api/analytics/dora/deployments`

部署频率 / 变更失败率 / MTTR 汇总 + 趋势。

**查询参数**：§0.2 公共参数（**不含** `department_id`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | 见 §0.7 |
| `total_deployments` | int | `0` | 窗口内部署数 |
| `failed_deployments` | int | `0` | 窗口内失败部署数 |
| `deploy_frequency_weekly` | number \| null | `null`=窗口周数为 0 | 部署频率（次/周） |
| `change_failure_rate` | number \| null | `null`=`total_deployments=0` | 变更失败率 |
| `mttr_seconds` | number \| null | `null`=无失败恢复样本 | 平均恢复时长 MTTR（秒） |
| `trend` | array | `[]`=无数据 | `[{ "week": "YYYY-MM-DD", "deployments": int, "failed": int, "failure_rate": number\|null, "mttr_seconds": number\|null }]` |
| `failures` | array | `[]`=无数据 | `[{ "deployment_id": string, "app": string\|null, "failed_at": string, "recovered_at": string\|null, "mttr_seconds": number\|null, "reason": string\|null, "status": "resolved"\|"open" }]` |

**响应示例（200）：**

```json
{
  "source_status": { "ready": true, "reason": null, "updated_at": "2026-08-05T07:00:00Z" },
  "total_deployments": 18,
  "failed_deployments": 2,
  "deploy_frequency_weekly": 4.5,
  "change_failure_rate": 0.111,
  "mttr_seconds": 3600,
  "trend": [
    { "week": "2026-07-27", "deployments": 5, "failed": 1, "failure_rate": 0.2, "mttr_seconds": 7200 },
    { "week": "2026-08-03", "deployments": 4, "failed": 0, "failure_rate": 0, "mttr_seconds": null }
  ],
  "failures": [
    {
      "deployment_id": "deploy-20260803-001",
      "app": "multica-web",
      "failed_at": "2026-08-03T14:20:00+08:00",
      "recovered_at": "2026-08-03T15:20:00+08:00",
      "mttr_seconds": 3600,
      "reason": "db migration failed",
      "status": "resolved"
    }
  ]
}
```

**引导态示例（部署未接入）：**

```json
{
  "source_status": { "ready": false, "reason": "部署流水线数据未接入", "updated_at": null },
  "total_deployments": 0,
  "failed_deployments": 0,
  "deploy_frequency_weekly": null,
  "change_failure_rate": null,
  "mttr_seconds": null,
  "trend": [],
  "failures": []
}
```

---

## 5. 2.1 身份与部门（LDAP 相关）—— v2.0 新增

### 接口 L1 `GET /api/analytics/identity/lifecycle`

用户生命周期汇总（入职开通率 / 离职权限熔断 / 权限熔断成功率）。

**查询参数**：§0.2 公共参数（含 `department_id`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | 见 §0.7 |
| `onboarding_rate` | number \| null | `null`=窗口无新入职 | 入职开通率（口径见《数据口径》§1.4） |
| `onboarded_members` | int | `0` | 窗口内开通账号的入职人数 |
| `new_hires_total` | int | `0` | 窗口内入职总人数（外部口径） |
| `cutoff_events` | int | `0` | 窗口内离职权限熔断事件数 |
| `cutoff_success_count` | int | `0` | 熔断成功次数 |
| `cutoff_success_rate` | number \| null | `null`=`cutoff_events=0` | 熔断成功率 |
| `recent_events` | array | `[]`=无数据 | `[{ "event_type": "onboard"\|"offboard"\|"cutoff", "member_name": string, "occurred_at": string, "result": "success"\|"failed"\|null }]` |

**响应示例（200）：**

```json
{
  "source_status": { "ready": true, "reason": null, "updated_at": "2026-08-05T07:00:00Z" },
  "onboarding_rate": 1.0,
  "onboarded_members": 3,
  "new_hires_total": 3,
  "cutoff_events": 2,
  "cutoff_success_count": 2,
  "cutoff_success_rate": 1.0,
  "recent_events": [
    { "event_type": "onboard", "member_name": "赵六", "occurred_at": "2026-08-02T09:00:00+08:00", "result": "success" },
    { "event_type": "offboard", "member_name": "孙七", "occurred_at": "2026-08-04T18:00:00+08:00", "result": null },
    { "event_type": "cutoff", "member_name": "孙七", "occurred_at": "2026-08-04T18:05:00+08:00", "result": "success" }
  ]
}
```

**引导态示例（LDAP 未接入且无手动映射）：**

```json
{
  "source_status": { "ready": false, "reason": "用户身份数据源未接入", "updated_at": null },
  "onboarding_rate": null,
  "onboarded_members": 0,
  "new_hires_total": 0,
  "cutoff_events": 0,
  "cutoff_success_count": 0,
  "cutoff_success_rate": null,
  "recent_events": []
}
```

---

### 接口 L2 `GET /api/analytics/identity/departments`

部门清单与成员映射（供部门筛选器与部门维度切片）。

**查询参数**：§0.2 公共参数（**不含** `department_id`）。

**响应字段：**

| 字段 | 类型 | 空值语义 | 说明 |
| --- | --- | --- | --- |
| `source_status` | object | 恒返回 | 见 §0.7 |
| `items` | array | `[]`=无数据 | `[{ "department_id": string, "name": string, "member_count": int, "active_members": int }]` |
| `items[].department_id` | string | — | 外部系统部门 ID；手动映射时为生成的稳定 ID |
| `items[].name` | string | — | 部门名 |
| `items[].member_count` | int | `0` | 部门成员数 |
| `items[].active_members` | int | `0` | 窗口内活跃成员数（口径同 §1.1.2） |

**响应示例（200）：**

```json
{
  "source_status": { "ready": true, "reason": null, "updated_at": "2026-08-05T07:00:00Z" },
  "items": [
    { "department_id": "rd-platform", "name": "研发效能平台组", "member_count": 5, "active_members": 4 },
    { "department_id": "rd-core", "name": "核心研发组", "member_count": 7, "active_members": 5 }
  ]
}
```

---

## 6. 二期/三期接口映射（v2.0 更新）

| 接口 | 阶段标注 | 说明 |
| --- | --- | --- |
| `GET /api/analytics/git/eloc` | 二期（数据源接入后启用） | ELOC 排名 |
| `GET /api/analytics/git/quality` | 二期 | 提交质量雷达 |
| `GET /api/analytics/git/repos` | 二期 | 代码仓活跃/PR 积压/合并周期 |
| `GET /api/analytics/git/prs` | 二期（可选） | PR 明细下钻 |
| `GET /api/analytics/dora/lead-time` | 三期（`merged` 口径二期可先行） | 交付周期 |
| `GET /api/analytics/dora/deployments` | 三期 | 部署频率/失败率/MTTR |
| `GET /api/analytics/identity/lifecycle` | 三期（手动部门映射可先行） | 生命周期/熔断 |
| `GET /api/analytics/identity/departments` | 三期（手动部门映射可先行） | 部门清单 |

> 一期前端对未接入数据源的模块显示引导态（PRD §7），接口返回 `source_status.ready=false` 即可；请求真实数据接口（如未接入时调 G1）也返回 `200 + source_status=false`，不返回 404。

---

## 7. 错误响应示例

**400（未接入部门数据时传 department_id）：**

```json
{ "error": "department data not configured" }
```

**400（project_id 非法）：**

```json
{ "error": "invalid project_id" }
```

**401：**

```json
{ "error": "unauthorized" }
```

**403（非工作区成员）：**

```json
{ "error": "forbidden: workspace member required" }
```

**500：**

```json
{ "error": "failed to list analytics" }
```
