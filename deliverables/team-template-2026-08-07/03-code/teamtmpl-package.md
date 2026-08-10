# Task A：teamtmpl 包（types + loader + 校验）实现说明

**日期**: 2026-08-07
**Issue**: SGD-23
**分支**: `feature/team-template`（本 Task 提交来源分支 `agent/senior-dev-agent/127e23ec`）

## 文件清单

| 文件 | 说明 |
|------|------|
| `server/internal/teamtmpl/types.go` | `TeamTemplate` / `SkillDef` / `SkillFile` / `AgentDef` / `SquadDef` / `MemberRef` 结构体，json tag 严格按 architecture.md §3 |
| `server/internal/teamtmpl/loader.go` | `//go:embed templates/*.json` + `Registry{bySlug, order}` + `Load()` / `loadFromFS()` / `validate()` / `List()` / `Get(slug)` |
| `server/internal/teamtmpl/templates/__smoke.json` | 最小 smoke 模板（下划线前缀 → 不进入 catalog，仅作形状参考，Task D 替换删除） |
| `server/internal/teamtmpl/loader_test.go` | 校验单测：10 条规则正/反例 + 成功路径 + malformed JSON 路径 |

## 设计要点

- **结构体**：严格按 architecture.md §3，字段名 / json tag 未做任何改动；`Content` 与 `SourceURL` 互斥由 loader 校验（规则 6）。
- **validate() 10 条规则**（architecture.md §4）：
  1. slug 非空 + kebab-case + 等于文件名 basename
  2. name 非空
  3/4/5. skills / agents / squads name 模板内唯一
  6. SkillDef Content 与 SourceURL 恰一非空
  7. agent.skills 引用存在
  8. squad.leader 引用存在
  9. squad.members[].agent_name 引用存在
  10. agent.instructions 非空
- **smoke 模板处理**：`__smoke.json` 以 `_` 前缀命名的文件被 loader 显式跳过（进不了 catalog）。因为规则 1 要求 slug 等于文件名且必须 kebab-case，`__smoke` 无法通过校验 —— 下划线前缀即「占位文件不进目录」的约定，Task D 的正式模板 `equipment-department-1-3-9.json` 不受影响。
- **0-skill / 0-agent / 0-squad 模板合法**（对齐 agenttmpl 0-skill 语义，architecture.md §4 注）。
- 复用 agenttmpl 同构模式：`loadFromFS(fs.FS, dir)` 便于用 `fstest.MapFS` 写单测；`List()` 按文件名排序保证确定性。

## 自查结果

- `go build ./internal/teamtmpl` ✅
- `go vet ./internal/teamtmpl` ✅
- `gofmt -l ./internal/teamtmpl` 无输出 ✅
- `go test ./internal/teamtmpl -count=1 -v` 全绿（17 个子测试）✅
