# Task C：team-template handler（List/Get/Apply + 事务）实现说明

**日期**: 2026-08-07
**Issue**: SGD-25
**分支**: `feature/team-template`（本 Task 提交来源分支 `agent/senior-dev-agent/1e0c9572`）

## 文件清单

| 文件 | 说明 |
|------|------|
| `server/internal/handler/team_template.go` | 3 个 handler：`ListTeamTemplates` / `GetTeamTemplate` / `ApplyTeamTemplate` + `init()` 加载 Registry + 预 fetch + 单事务三阶段 find-or-create |
| `server/internal/handler/team_template_test.go` | 12 个单测：apply 幂等 / 两分支 / 422 零残留 / 事务回滚 / model_overrides / 错误码 / 空值语义 |
| `server/internal/teamtmpl/loader.go` | 追加导出构造 `NewRegistry(...TeamTemplate)`（测试注入 catalog 用，纯新增不破坏 loader 契约） |

## 设计要点

- **init() 加载**：仿 `agent_template.go:32-38`，`teamtmpl.Load()` panic on err，包级 `teamTemplates` 供三个 handler 共享。
- **List/Get**：`List` 返回摘要数组（省略 instructions/content/files/members，squad 给 `member_count` = leader 自动入队 + Members 数）；空目录返回 `[]` 非 null。`Get` 返回全文（`TeamTemplateDetailResponse` 数组字段强制非 nil）。
- **Apply 严格按 architecture.md §5 时序**：
  1. 鉴权：`requireUserID` + `resolveWorkspaceID` + `GetAgentRuntimeForWorkspace` + `canUseRuntimeForAgent`（复用 `agent_template.go:210-225`），越权 403。
  2. 校验：`runtime_id` 必填 400；`model_overrides` 键须存在模板 agents 否则 400；slug 不存在 404。
  3. **预 fetch 阶段（事务外）**：对每个 `SourceURL != ""` 的 SkillDef 用 `fetchTemplateSkillsParallel` 并行 fetch（复用 `fetchSkillFromURL` 分发）；任一失败 → 422 `{error, failed_urls}`，不进事务。
  4. **单事务** `h.TxStarter.Begin()` → `qtx = h.Queries.WithTx(tx)` → `defer tx.Rollback()`：
     - ① Skills：`GetSkillByWorkspaceAndName` 命中 reused；未命中 `createSkillWithFilesInTx`（内嵌用 SkillDef.Content/Files，远程用预 fetch 的 importedSkill + origin 溯源）。
     - ② Agents：`GetAgentByWorkspaceAndName` 命中 reused（不重建不改 skills）；未命中 `qtx.CreateAgent`（model = override 或模板默认；permission 复用 `parsePermissionInput` + `replaceInvocationTargetsWithQueries`；max_concurrent_tasks 缺省 6）+ `qtx.AddAgentSkill`（ON CONFLICT DO NOTHING）。
     - ③ Squads：`GetSquadByWorkspaceAndName` 命中 reused；未命中 `qtx.CreateSquad`（leader/creator 按 name→ID 解析）+ `qtx.UpdateSquad` 落 instructions（CreateSquad SQL 无 instructions 列，事务内补写）+ leader 自动入队 role="leader"（UNIQUE 冲突跳过，不重复加）+ 每个 MemberRef `qtx.AddSquadMember`（幂等跳过）。
     - ④ `tx.Commit()`；任一步出错 → return（defer Rollback 全量回滚）。
  5. 返回 201 `ApplyTeamTemplateResponse{template_slug, skills/agents/squads: {created, reused}}`；created/reused 数组永非 null。

## 空值语义（api_design.md §2）

- 所有数组字段（skills/agents/squads/created/reused/members）始终 present，空为 `[]` 非 null（`TeamTemplateResourceOutcome` 显式初始化为空切片；`TeamTemplateDetailResponse` 转换时 nil → `[]`）。
- 可选字符串省略字段（omitempty），model_overrides 缺省 = 用模板默认。

## 自查结果

- `go build ./internal/handler/...` ✅
- `go vet ./internal/handler` ✅
- `gofmt -l` 新文件无输出 ✅
- `go test ./internal/handler -run TeamTemplate -count=1` 全绿（12 个测试）✅

> 说明：本地测试库此前停留在 migration 233（缺 254 `member.department` 等列），已用仓库自带 `go run ./cmd/migrate up` 补齐至最新 schema，Task C 相关测试方可运行。全 handler 套件中 `TestPreviewCommentTriggers_DelegateOfflineLeaderBlocked` 为 LIU-9/LIU-13 既有测试失败（`createHandlerTestAgentOffline` 未写 `agent.runtime_id`，违反 NOT NULL），与本次改动无关。
