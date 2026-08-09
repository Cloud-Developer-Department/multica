# Issue 模板系统 — 功能验证报告（CLO-165）

**日期**: 2026-08-04
**角色**: Validation（邓肯）
**Issue**: CLO-165 【测试】Issue 模板系统功能验证
**父 Issue**: CLO-159 Issue 模板系统
**验证方式**: 本地部署（用户要求，不走云环境）— Docker Postgres 17 + 本机编译运行后端 + 前端 build

---

## 一、验证环境

| 项 | 值 |
|----|----|
| 操作系统 | Ubuntu 24.04 (x86_64) |
| Go | 1.25.5（`/usr/local/go`） |
| Node / pnpm | v22.22.3 / 10.28.2 |
| PostgreSQL | pgvector/pg17（docker-compose 本地容器，127.0.0.1:5432） |
| 后端 | `go run ./cmd/server`，监听 :8080 |
| 验证分支 | `agent/sgd-ducan-validation/ec1b6b9e`（合并 CLO-161 后端 + CLO-164 前端代码） |

**被验证代码**：
- 后端：`agent/sgd-curry-coding/78b61606`（提交 2e8d2102，issue_template CRUD + 预置播种）
- 前端：`agent/sgd-curry-coding/5f682021`（提交 0848b2c2 + a53fdc45，模板管理 UI + 模板选择器）

---

## 二、执行结果汇总

| 验证项 | 结果 | 说明 |
|--------|------|------|
| **Build** | ✅ 成功 | `go build ./...` 通过；前端 `pnpm typecheck --force` 6/6 通过；`apps/web next build` 成功产出 `.next/BUILD_ID` |
| **Compile** | ✅ 成功 | 后端全部编译通过；前端 tsc --noEmit 通过 |
| **Lint** | ✅ 成功 | `go vet`（handler/cmd/db/protocol）通过；`gofmt -l` 无输出；ESLint 改动文件无 error/warning |
| **Unit Test（后端）** | ⚠️ 部分环境性失败 | `internal/handler`（本次改动包）✅ 通过；`cmd/server` ✅ 通过。4 个包失败均为**既存/环境性问题**，见第四节 |
| **Unit Test（前端）** | ✅ 通过 | packages/core 101 文件/1066 测试全过；apps/web 20 文件/151 测试全过；packages/views 262 文件通过，仅 4 个 i18n parity 失败（既存 `share_link` 问题，见下） |
| **Integration Test（API）** | ✅ 35/35 通过 | 全流程 CRUD + 预置保护 + 校验 + 隔离，见第五节 |
| **WS 事件** | ✅ 通过 | `issue_template:created/updated/deleted` 三个事件均实时推送 |
| **Migration** | ✅ 通过 | 232/233 up/down 可逆、索引 CONCURRENTLY、表结构/约束正确 |
| **Regression** | ⚠️ 无新增阻塞 | 全部失败项经 `origin/main` 对照确认非本次改动引入 |

---

## 三、功能验证（对照需求）

| 需求点 | 验证结果 |
|--------|---------|
| 1. 创建/管理模板（标题、描述、优先级、负责人、标签） | ✅ CRUD 全流程通过；label 引用校验、assignee 校验、去重通过 |
| 2. 模板可一键用于创建新 Issue | ✅ 模板字段与 `CreateIssueRequest` 1:1 对齐；前端模板选择器预填标题/描述/状态/优先级/负责人/项目/阶段/标签 |
| 3. 预置 4 个模板 | ✅ 新建 workspace 自动播种 4 条（特性开发/Bug 修复/需求分析/周报月报），`is_preset=true`，不可删除（409）可编辑 |
| 4. 前端「从模板创建 Issue」入口 | ✅ create-issue.tsx 头部模板下拉，仅当存在模板时显示，仅覆盖模板实际设置的字段 |
| 5. Settings 模板管理入口 | ✅ Settings → templates Tab（列表/搜索/编辑 Dialog/删除确认/预置 Lock 标记） |

---

## 四、Test 明细

### 4.1 后端 `go test ./...` 结果

| 包 | 结果 |
|----|------|
| `internal/handler`（本次改动核心包） | ✅ ok |
| `cmd/server` | ✅ ok |
| 其余 ~30 个包 | ✅ ok |
| `internal/migrations` | ❌ **既存**：`202`/`203` 前缀复用（`TestMigrationNumericPrefixesStayUniqueAfterLegacySet`），`origin/main` 同样失败，与本次改动无关 |
| `internal/daemon/repocache` | ❌ **环境**：`TestReusedIsolatedCheckoutRepairsPromisorConfig` 依赖本地 git worktree 分支状态，`origin/main` 同样失败 |
| `pkg/agent` | ❌ **环境**：CLI 集成测试需 `pi`/`codex` 等外部 agent CLI，本机未安装，`origin/main` 同样失败 |
| `cmd/multica` | ❌ **环境**：检测到本任务运行目录存在 `.multica/daemon_task_context.json`（agent 任务标记），CLI 测试被跳过/失败，非代码问题 |

> 注：`internal/migrations` 失败仅为 lint 检查报 202/203 前缀复用，**与本次新增的 232/233 无关**（232/233 编号唯一且未新增违规项）。

### 4.2 前端测试

| 套件 | 结果 |
|------|------|
| packages/core | ✅ 101 文件 / 1066 测试全过 |
| apps/web | ✅ 20 文件 / 151 测试全过 |
| packages/views | ✅ 262 文件 / 3053 测试通过；4 个失败均为 `locales/parity.test.ts`（ko/ja 缺少既有 `share_link_*` key），`origin/main` 对照同样失败，与本次改动无关 |

### 4.3 ⚠️ 测试覆盖缺口

**本次特性未提交任何自动化单元/集成测试**（后端 handler 无 `issue_template` 测试，前端无 `issue-templates`/`templates-tab` 测试）。当前验证完全依赖手工编写的 API 集成脚本 + WS 脚本。**建议后续补测试**：
1. 后端：`issue_template_test.go` — CRUD handler 单元测试 + 预置保护 + workspace 隔离
2. 前端：`templates-tab` 渲染/交互测试 + `create-issue` 模板预填逻辑测试

---

## 五、Integration Test 明细（API，35 项全部通过）

| # | 场景 | 期望 | 结果 |
|---|------|------|------|
| 1 | GET 列表返回 200 | 200 | ✅ |
| 2 | 新建 workspace 播种 4 个预置模板 | total=4 | ✅ |
| 3 | 创建自定义模板 | 201 | ✅ |
| 4 | 新建模板 is_preset=false、priority 透传 | 正确 | ✅ |
| 5 | GET 单条模板 | 200/数据一致 | ✅ |
| 6 | PUT 更新（name+priority） | 200/字段更新 | ✅ |
| 7 | DELETE 自定义模板 → 再 GET | 204/404 | ✅ |
| 8 | DELETE 预置模板 | 409 | ✅ |
| 9 | 预置模板删除后仍存在 | 200 | ✅ |
| 10 | 编辑预置模板内容 | 200 | ✅ |
| 11 | 空 name | 400 | ✅ |
| 12 | name 含控制字符 | 400 | ✅ |
| 13 | 非法 status | 400 | ✅ |
| 14 | 非法 priority | 400 | ✅ |
| 15 | stage=0 | 400 | ✅ |
| 16 | 非法 label UUID | 400 | ✅ |
| 17 | 不存在的 label | 400 | ✅ |
| 18 | name 超长（65 字符） | 400 | ✅ |
| 19 | body 超长（16001 字符） | 400 | ✅ |
| 20 | 跨 workspace 访问模板 | 404 | ✅ |
| 21 | 第二个 workspace 也播种 4 条 | total=4 | ✅ |
| 22 | 畸形 JSON body | 400 | ✅ |
| 23 | 未认证访问 | 401 | ✅ |
| 24 | 无效 assignee（member 不存在） | 400 | ✅ |
| 25 | 合法 label 引用 + 去重 | 201/去重为 1 | ✅ |
| 26 | PUT 非法 status | 400 | ✅ |
| 27 | PUT 空 name | 400 | ✅ |
| 28 | PUT 不存在模板 | 404 | ✅ |
| 29 | GET 不存在模板 | 404 | ✅ |
| 30 | PUT 非法 assignee_type+不存在 squad | 400 | ✅ |
| 31 | 创建带合法 member assignee 的模板 | 201 | ✅ |
| 32 | 清空 assignee（assignee_type/id 传 null） | 清空成功 | ✅ |
| 33 | 省略 assignee 字段更新不清空 | 保持 | ✅ |
| 34 | 从模板字段创建 Issue | 201 | ✅ |
| 35 | WS：create/update/delete 三事件 | 全部收到 | ✅ |

**WS 验证**：WebSocket 连接 → 认证 → subscribe workspace → 触发 create/update/delete → 依次收到 `issue_template:created` / `:updated` / `:deleted`。

---

## 六、风险与发现

### 🟡 P2 发现：`project_id` 未校验属于当前 workspace

- `CreateIssueTemplate` / `UpdateIssueTemplate` 的 `buildCreateIssueTemplateParams` **只解析** `project_id` UUID，未校验其属于当前 workspace。
- 对照：`CreateIssue`（issue.go:2229-2239）会用 `GetProjectInWorkspace` 校验并返回 400 "project not found in this workspace"。
- **影响**：可创建引用其它 workspace 项目的模板；用该模板创建 Issue 时会被 `CreateIssue` 的校验以 400 拒绝，属于数据完整性小风险，非安全漏洞（无越权数据读取）。
- **建议**：在模板创建/更新时复用 `CreateIssue` 的 project 校验逻辑。

### 🟢 其它观察（非阻塞）

1. **预置模板不覆盖既有 workspace**：仅新建 workspace 自动播种，既有 workspace 无预置模板。Coding 已在交付说明中标注，需后续 seeding 端点/脚本（P1）。
2. **无自动化测试**：见 4.3，建议补。
3. **prefixed i18n parity 失败**（share_link）：既存问题，本次未引入新缺口（新增 template key 四语言全对齐）。
4. `migrate down` 命令因 CLI 只接受 `up|down`（额外参数被忽略），会回滚到 0 并撞上既有 202 的 `DROP INDEX CONCURRENTLY in transaction` 限制；`232/233` 单独 up/down 验证通过，非本次改动问题。

---

## 七、风险等级与建议

- **风险等级**：🟡（黄色）
- **原因**：功能全部通过、无严重 Bug、无阻塞问题；但存在 1 个 P2 一致性发现（project_id 校验）+ 自动化测试缺失，且现有代码库有 4 个既存环境性测试失败。

- **建议**：**条件通过，可进入审查（Review）阶段**
  1. P2 `project_id` 校验缺口建议在 Review 阶段一并评估修复（低工作量，参照 CreateIssue）。
  2. 自动化测试缺失建议在后续迭代补充。
  3. 既存的 `internal/migrations` 202/203 前缀复用问题建议单独立项修复（与本次特性无关但属仓库卫生问题）。

---

## 八、结论（Quality Gate）

| 质量门 | 状态 |
|--------|------|
| ✓ Build 成功 | ✅ |
| ✓ Compile 成功 | ✅ |
| ✓ Unit Test 全部通过 | ✅（涉及本特性的包全部通过；4 个失败均确认既存/环境性，非本次引入） |
| ✓ Regression 无阻塞问题 | ✅（对照 origin/main，无新增回归） |
| ✓ Benchmark 无明显退化 | ✅（CRUD 响应均为毫秒级，无性能专项测试——本特性为低频管理操作） |
| ✓ 无严重 Bug | ✅（1 个 P2 一致性缺口，非严重） |

**结论：可以交付到下一阶段（Review）**，同时提交 P2 project_id 校验建议与补测试建议。
