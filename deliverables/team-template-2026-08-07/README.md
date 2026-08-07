# 团队模板能力（SGD-17）交付件

**日期**: 2026-08-07
**特性**: 团队模板机制（一键复制 1+3+9 组织，首个模板 `equipment-department-1-3-9`）

## 交付物索引

| 子目录 | 阶段 | 说明 |
|--------|------|------|
| `01-research/` | Research | ba-agent《业务需求分析报告》+《需求风险清单》（requirement_analysis.md / requirement_risk.md） |
| `02-design/` | PM-Design | **PRD.md**（功能清单 / User Story / Mermaid 逻辑流程 / 异常状态处理规则 / 验收标准） |

## 关键结论

- 3 个 API：`GET /api/team-templates`、`GET /api/team-templates/{slug}`、`POST /api/team-templates/{slug}/apply`
- apply 幂等键 = `workspace_id + resource_type + name`；重复 apply 全 reused；单事务原子，失败整体回滚零残留
- skill fetch 走**事务外**（规避长事务持锁）；source_url fetch 失败 → 422 + `failed_urls`，零写入
- find-or-create 同名不同内容 → **复用 + warning**（不覆盖不报错）
- 首个模板 `equipment-department-1-3-9`：10 agent + 3 squad + `multica-team-workflow` skill（内嵌 content）

## 分支

- 基线：`origin/Equipment_Department_Exploration`（本地部署源）
- 共享特性分支：`feature/team-template`（零 push，PR 唯一出口 = DevOps）
