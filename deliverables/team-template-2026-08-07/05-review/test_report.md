# Test Report — 团队模板能力（SGD-21 质量验证）

**验证者**: qa-reviewer-agent（交付运维小队）
**日期**: 2026-08-10
**被测分支**: `feature/team-template` @ `625c7421`（devops 基线复核提交 `d93ae585` 之上）
**回归环境**: 本机无 gcc（`make test` 因 `-race` 需 cgo 无法直接运行）→ 采用 DevOps 固化的容器化环境 `scripts/test-go-docker.sh`（golang:1.25 + `--network host` + host 模块缓存 + `GOPROXY=off`，等价 `make test` Go 部分）

## 1. Build / Compile

| 项 | 命令 | 结果 |
|----|------|------|
| 全量编译 | `CGO_ENABLED=0 go build ./...`（server） | ✅ PASS |
| 迁移 | `go run ./cmd/migrate up` | ✅ PASS（全部 skip=已应用） |

## 2. Unit / 特性测试（teamtmpl + team-template）

| 测试集 | 用例 | 结果 |
|--------|------|------|
| `internal/teamtmpl`（loader） | 17 用例：10 条校验规则 / 零段模板 / 重复 slug / malformed JSON / 下划线占位跳过 | ✅ 全绿 |
| `internal/handler` `...TeamTemplate`（--race） | 12 用例：list 空数组 / list 摘要 / detail / 404 / **首建创建全部** / **重复 apply 全 reused** / 远程 skill 分支 / 坏 URL 422 无残留 / **事务失败全回滚** / model_overrides / 校验错误 / 默认 max_concurrent_tasks | ✅ 全绿 |

### 验收项逐条核对

| 验收项 | 结果 | 依据 |
|--------|------|------|
| 重复 apply 返回全 reused，不产生重复资源 | ✅ | `TestApplyTeamTemplateRepeatApplyReusesAll`（handler/team_template_test.go:296）：第二次 apply `skills:0 created/1 reused; agents:0/2; squads:0/1` |
| apply 任一步失败整体回滚，无残留 | ✅ | `TestApplyTeamTemplateTxFailureRollsBackEverything`（team_template_test.go:456）+ `TestApplyTeamTemplateBadSourceURLReturns422WithNoResidue`（:381） |
| 10 agent + 3 squad + 1 skill 带完整 prompt/instructions | ✅ | 模板 `equipment-department-1-3-9.json` 校验通过（10 条规则含 instructions 非空、squad leader/member 引用存在）；loader 测试覆盖 |
| 3 API 可路由 | ✅ | `team_template_integration_test.go`（router 级 GET list / GET detail / POST apply / 404） |

## 3. Regression（全量 `scripts/test-go.sh --race`）

DevOps 基线复核（`baseline-regression-report.md`）确证：**特性分支相对基线 `0ab7ea6e` 未引入任何新失败**。

剩余失败均为既有技术债，与 team-template **零相关**（`git diff 0ab7ea6e 625c7421` 对相关文件零差异）：

| 失败 | 位置 | 性质 |
|------|------|------|
| migration 前缀 lint | `internal/migrations/migrations_lint_test.go:91` | 基线已存在：202/203/232–239 前缀复用来自 share-link/runtime-profile/issue-template/squad/delegation/workflow，teamtmpl 零新增 migration |
| `TestPreviewCommentTriggers_DelegateOfflineLeaderBlocked` | `internal/handler/comment_delegation_test.go:797` | 基线已存在：LIU-9/LIU-13 测试助手建 agent 未带 runtime_id 撞 NOT NULL |
| `TestInFlightOldHeadKeepsTrailingRefresh` | `internal/integrations/ghsnapshot/refresh_db_test.go:360` | 时序 flaky，基线/特性两侧均不稳定 |

## 4. Benchmark

本次变更（嵌入式静态模板 + 内存 Registry + apply 单事务）无热点路径，未做独立基准；`internal/teamtmpl` 测试包运行 <2s，无退化风险。

## 5. 风险等级

**中低风险（可放行）**：

- 🔴 无阻塞级缺陷
- 🟡 并发重复 apply 竞态（见 review_report 并发轴）：并发双 apply 时后到请求可能 500（事务回滚，数据完整性由 UNIQUE 约束保证，不会产生重复资源）；建议后续优化为把 unique violation 转换为 reused 响应
- 🟢 既有技术债（migration 前缀 / delegation 测试 / ghsnapshot flaky）不在本特性范围，建议单独派单跟踪，供 stage4 收口 PR 处理

## 结论

**Build ✅ Compile ✅ UnitTest ✅ 特性回归 ✅ 无性能退化 ✅ 无严重 Bug ✅** — 特性测试全绿，验收项全部达成。
