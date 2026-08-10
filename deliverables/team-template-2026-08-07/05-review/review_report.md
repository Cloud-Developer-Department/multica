# Review Report — 团队模板能力（SGD-21 九轴审查 + 安全审计）

**审查者**: qa-reviewer-agent
**日期**: 2026-08-10
**被测分支**: `feature/team-template` @ `625c7421`
**Severity**: 🟢 通过 / 🟡 建议 / 🔴 阻塞（安全漏洞必须 Blocker）

---

## 一、九轴审查

### 1. 需求满足 🟢
- 3 API 齐备：`GET /api/team-templates`、`GET /api/team-templates/{slug}`、`POST /api/team-templates/{slug}/apply`（`server/cmd/server/router.go:1305-1309`）。
- 首个模板 `equipment-department-1-3-9.json`：10 agent + 3 squad + 1 skill（`multica-team-workflow` 内嵌 content 全文），全部带完整 instructions/描述。
- apply 幂等（workspace+name find-or-create）、单事务原子、失败整体回滚、model_overrides、skill 内嵌/URL 双分支 —— 全部落地且测试覆盖。
- 明确不做项未越界：无前端 UI、无模板 DB 化/运行时编辑、squad 仅 leader/member 两级。

### 2. 架构合理 🟢
- 新增 `server/internal/teamtmpl` 包仿 `agenttmpl`：`go:embed templates/*.json` + boot 校验失败即 panic（`loader.go:13-32`），启动期 fail-fast 优于运行期暴露。
- `Registry` 构建后只读（`loader.go:18-24`），List/Get 无锁并发安全。
- **零新增表/零新增 migration**，仅追加 3 个 sqlc find-by-name 查询（`agent.sql:1497`、`squad.sql:233`、`skill.sql:26`），复用现有 UNIQUE 约束。
- apply 复用 `CreateAgentFromTemplate` 事务骨架、`fetchSkillFromURL`、`createSkillWithFilesInTx`、`CreateSquad`/`AddSquadMember`（走 qtx），无重复造轮子。

### 3. 代码质量 🟡
- `team_template.go`（647 行）单函数 `ApplyTeamTemplate` 偏长，但三阶段（skill→agent→squad）结构清晰、注释到位；可拆分为阶段子函数降低认知负荷（建议非阻塞）。
- 错误路径统一 `writeError` + slog 带请求上下文，无裸 panic、无吞错。
- 空值语义合规：响应数组 `make(...,0,...)` 保证永非 null（`team_template.go:306-309`），可选字段 `omitempty`。

### 4. 安全（审计）🟢 — 无阻塞级问题
| 检查项 | 结果 | 依据 |
|--------|------|------|
| apply 接口权限 | ✅ 需登录 + workspace 成员校验（`team_template.go:227,275`）；`runtime_id` 归属/权限校验，私有 runtime 非 owner/admin → 403（:267-282，与 CreateAgentFromTemplate 同步） | `team_template.go:264-282` |
| skill 远程导入 URL 校验 | ✅ fetch 仅允许 clawhub.ai / skills.sh / github.com（`skill.go:830`），scheme 仅 http/https（`file.go:1105-1109`）；非法源 → 422 `failed_urls` 零写入 | `team_template.go:287-294`、`skill.go:800-830` |
| 技能文件路径穿越 | ✅ `validateFilePath` 拒绝绝对路径与 `..`（`skill.go:248-260`），embed 文件同样过检（`team_template.go:348-359`） | `skill.go:248-260` |
| 事务回滚边界 | ✅ 单事务覆盖 skill→agent→squad，`defer tx.Rollback` + 失败即 return，成功才 Commit（`team_template.go:296-585`）；预 fetch 在事务外（不持锁） | `team_template.go:296,580-585` |
| 幂等防重复 | ✅ 顺序重复 apply 全 reused（测试验证）；并发竞态仅致 500（见并发轴），UNIQUE 约束兜底不产生脏数据 | `team_template.go:314-378,386-393,486-493` |
| 注入面 | ✅ `model_overrides` 仅允许模板内 agent（:603-614）；`createSkillWithFilesInTx`/`CreateAgent` 全参数化；无凭据/密钥进入模板 | — |
| 敏感信息 | ✅ 模板仅 prompt/instructions/描述，无凭据；日志不打印正文 | — |

### 5. 并发 🟡
- 远程 skill 预 fetch 并行（`fetchTeamTemplateRemoteSkills`，事务外并行 + 30s 超时），规避串行拖慢。
- 🟡 **并发重复 apply 竞态**：find-or-create 非原子（先 SELECT 后 INSERT），并发双 apply 时后到请求 INSERT 撞 UNIQUE → 500（整体回滚，数据完整）。建议将 `is_unique_violation` 分支转为 "reused" 响应（现仅记日志，`team_template.go:373,452,521`）。**不阻塞放行**（数据一致性安全，非业务功能缺陷）。

### 6. 性能 🟢
- 模板目录 boot 时加载一次入内存，List/Get O(1) 查询；apply 无 N+1；远程 fetch 并行且带超时。无性能退化。

### 7. 兼容 🟢
- 响应数组非 null（空为 `[]`）、可选字段缺省省略，前端契约稳定；无 schema 变更、无既有行为破坏；模板 model 默认 + 请求级 `model_overrides` 覆盖，运行时不强校验（沿用 agenttmpl 模式）。

### 8. 异常处理 🟢
- 404（未知 slug）、400（非法 body / 缺 runtime_id / 未知 model_override）、403（私有 runtime）、422（fetch 失败 + `failed_urls`）、500（tx 失败）全覆盖，且任一失败回滚零残留；boot 校验失败 panic 快速暴露部署缺陷。

### 9. 测试是否齐 🟢
- loader 10 条校验规则 + 边界（slug 格式/唯一性/引用存在性）单测 17 用例 ✅
- apply 幂等 / 远程分支 / 坏 URL 422 无残留 / tx 回滚 / model_overrides / router 集成共 12 用例 ✅
- 全量回归剩余 3 失败均为基线既有技术债，与特性零相关（见 test_report / devops baseline-regression-report）✅

---

## 二、结论与建议

### 结论
**APPROVE（团队模板特性）**。特性自身 Build/单测/回归全绿，验收项全部达成，安全审计无 Blocker，无严重 Bug。

### 需后续跟进的既有技术债（非本特性引入，建议单独派单）
1. migration 前缀复用 lint（`migrations_lint_test.go:91`）— 来自其他并入 feature 的既有迁移。
2. delegation 测试助手 runtime_id 缺失（`comment_delegation_test.go:797`）。
3. ghsnapshot 时序 flaky（`refresh_db_test.go:360`）。

以上技术债可在 stage4 收口 PR（SGD-20）一并处理或另立技术债 Issue，不影响本特性放行。
