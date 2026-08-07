# Task D：首个模板 JSON `equipment-department-1-3-9` 实现说明

**日期**: 2026-08-07
**Issue**: SGD-26
**分支**: `feature/team-template`（本 Task 提交来源分支 `agent/dev-agent/200dcb88`）

## 文件清单

| 文件 | 说明 |
|------|------|
| `server/internal/teamtmpl/templates/equipment-department-1-3-9.json` | 首个正式模板：10 agent + 3 squad + 1 skill（内嵌 content 分支） |
| `server/internal/teamtmpl/templates/__smoke.json` | 已删除（Task A 占位 smoke，被正式模板替换） |

## 模板结构摘要

- **slug**: `equipment-department-1-3-9`（等于文件名 basename，过 loader 规则 1）
- **skills[]**（1）：`multica-team-workflow`，用内嵌 content 分支（`content` = SKILL.md 正文，`source_url` 留空），满足规则 6 恰一非空。
- **agents[]**（10）：`pmo-agent` / `ba-agent` / `pm-agent` / `ux-agent` / `architect-agent` / `dev-agent` / `senior-dev-agent` / `qa-reviewer-agent` / `devops-agent` / `security-audit-agent`，instructions 与现有 roster（`multica agent get <id>`）逐字对齐；每个 `skills` 引用 `multica-team-workflow`；`model`/`visibility`/`max_concurrent_tasks` 按 roster 实际配置（model 有差异：`architect-agent` 为 `huaweicloud/glm-5.2`，其余 `deepseek/deepseek-v4-flash`）。
- **squads[]**（3），leader/member 按现有 squad roster 对齐：
  - `squad-product-design`：leader `pm-agent`，members `ba-agent`/`ux-agent`
  - `squad-engineering`：leader `architect-agent`，members `senior-dev-agent`/`dev-agent`/`qa-reviewer-agent`（qa-reviewer 为工程+交付双队复用）
  - `squad-delivery-ops`：leader `qa-reviewer-agent`，members `devops-agent`

## 内容来源（不凭空捏造）

- 10 agent 的 description/instructions/model/visibility/max_concurrent_tasks：`multica agent get <id>` 逐字对齐现有 roster。
- `multica-team-workflow` skill 全文：取 `.opencode/skills/multica-team-workflow/SKILL.md` 正文（去 frontmatter），与 workspace 现存同名 skill content 一致。
- 3 小队 instructions：`multica squad get <id>` 逐字对齐；`squad-delivery-ops` 的 instructions 由 `squad-delivery-devops` 原文改 squad 名引用。

## 校验

- `go build ./internal/teamtmpl` ✅
- `go vet ./internal/teamtmpl` ✅
- `go test ./internal/teamtmpl -count=1 -v` 全绿（含 `TestLoad_RealTemplates` 走 `//go:embed` 真实加载，10 条规则全过）✅
