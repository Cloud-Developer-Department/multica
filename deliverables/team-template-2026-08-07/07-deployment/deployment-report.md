# 团队模板能力 — 阶段4 部署 + Health Check + 收口 PR（SGD-20）

**日期**: 2026-08-10
**角色**: devops-agent（交付运维小队）
**父需求**: SGD-17 团队模板能力；前置阶段3（SGD-21 QA）已 APPROVE
**部署版本**: `feature/team-template` @ `9beac55a`（含审核 Artifact `625c7421`，两者 server 代码零差异）

---

## 一、前置确认

| 检查项 | 结果 |
|--------|------|
| 阶段3（SGD-21）交付件齐 | ✅ `deliverables/team-template-2026-08-07/05-review/`（test_report / review_report / approval_status.json=APPROVE）+ `06-docs/`（team-template-api / team-template-format） |
| 版本核对 | ✅ 审核 Artifact `625c7421` 为部署 tip `9beac55a` 祖先，`git diff 625c7421..9beac55a -- server/` 为空 → 源码包版本 == 最新已通过审核 Artifact 版本 |
| DB 迁移 | ✅ 本地 303/303 迁移在册；本特性零新增迁移（复用 agent/squad/skill 表） |

## 二、部署过程

1. **构建**：`CGO_ENABLED=0 go build -o bin/server ./cmd/server`（go 1.25.5）✅
2. **上线**：停旧后端进程（PID 293881，旧二进制备份 `/tmp/.../server.bin.bak-20260810`）→ 启动新后端（:8080，`APP_ENV=development`，`MULTICA_DEV_VERIFICATION_CODE=888888`，同一 DATABASE_URL）
3. **前端**：本特性纯后端（`git diff --stat` 无 apps/、packages/ 改动），:3000 Next.js 代理 `/api/*` → :8080，无需重建
4. **回滚方案**：恢复备份二进制 `server.bin.bak-20260810` 重启即可

## 三、Health Check — 3 个 API（:8080 直连 + :3000 代理）

| # | 端点 | :8080 | :3000 |
|---|------|-------|-------|
| 1 | `GET /api/team-templates` | ✅ 200，返回 `equipment-department-1-3-9` 摘要（1 skill / 10 agent / 3 squad） | ✅ 200 |
| 2 | `GET /api/team-templates/{slug}` | ✅ 200，完整结构（skills 含 content / agents 含完整 instructions / squads 含 instructions 与 members） | ✅ 200 |
| 3 | `POST /api/team-templates/{slug}/apply` | ✅ 201（首次）→ 幂等 reused（后续） | ✅ 201 |

认证：devops.smoke@test.com + `888888` 验证码登录获 JWT；workspace = `team-template-deploy-verify`（新建验证工作区，slug `team-template-deploy-verify`）。

## 四、apply 落库断言

**首次 apply**（HTTP 201）：
- skills `created=1 / reused=0`：`multica-team-workflow`
- agents `created=10 / reused=0`：pmo-agent / ba-agent / pm-agent / ux-agent / architect-agent / dev-agent / senior-dev-agent / qa-reviewer-agent / devops-agent / security-audit-agent
- squads `created=3 / reused=0`：squad-product-design / squad-engineering / squad-delivery-ops

**DB 校验**（workspace `33ddc6c4-...`）：
- agent 10 条，instructions 全部非空（长度 266–683 字符）✅
- squad 3 条，instructions 全部非空 ✅；squad_member 落库（product-design 3 / engineering 4 / delivery-ops 2）✅
- skill 1 条：`multica-team-workflow` ✅
- 经本地 API 复核：agents 10 / squads 3 / skills 1 全部可查 ✅

**重复 apply（幂等）**：skills `created=0/reused=1`、agents `created=0/reused=10`、squads `created=0/reused=3`，DB 计数不变（10/3/1），无重复资源 ✅

## 五、收口 PR

| 项 | 值 |
|----|----|
| PR | https://github.com/patrickstar179/multica/pull/19 |
| base | `Equipment_Department_Exploration`（主分支/本地部署源） |
| head | `feature/team-template`（共享特性分支） |
| 标题 | `SGD-17 feat(teamtmpl): 团队模板能力 — 一键复制 1+3+9 组织（10 agent + 3 squad + 1 skill）` |
| 正文 | 改动说明（teamtmpl 包 + 3 API + 首个模板）+ 影响范围（纯后端零迁移无前端） |
| 合并 | ✅ MERGED（merge commit `386f85d4`，2026-08-10 08:31Z） |
| 分支清理 | ✅ 远端 `feature/team-template` 已删除 |

> 注：PR 与 issue 的自动关联依赖 VCS webhook，本 workspace 未观察到自动回填；关联键已含于标题/正文（SGD-17）。

## 六、结论

- 部署源码包版本 == 最新已通过审核 Artifact 版本 ✅
- :3000/:8080 双端 3 API 全部可用 ✅
- apply 全量落库 10 agent + 3 squad + 1 skill 且 prompt/instructions 完整 ✅
- 重复 apply 幂等全 reused，无重复资源 ✅
- 收口 PR #19 已合入部署源，冗余分支已清理 ✅
