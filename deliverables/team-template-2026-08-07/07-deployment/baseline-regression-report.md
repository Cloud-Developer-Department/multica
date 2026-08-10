# 基线回归报告：3 个失败确证与 team-template 无关（devops-agent）

> **结论先行**：全量 race 回归在**基线分支** `0ab7ea6e`（`origin/Equipment_Department_Exploration` 本地部署源）
> 与**特性分支** `625c7421`（`feature/team-template`，含全部 SGD 收口）上，3 个目标失败全部可复现、
> 失败内容逐字一致；teamtmpl **零新增 migration**、未触碰任何失败文件 → 与团队模板特性**无关**。

## 一、运行环境（标准化容器化 race 回归）

| 项 | 值 |
|----|----|
| 镜像 | `golang:1.25`（含 gcc/cgo） |
| 网络 | `--network host`（直连宿主 `localhost:5432/multica`） |
| 模块缓存 | 挂载宿主 `~/go/pkg/mod`（只读，`GOPROXY=off` 离线可复现） |
| 入口 | `bash scripts/test-go.sh --race`（等价 `make test` 的 Go 部分，经 `scripts/test-go-docker.sh` 封装） |
| DB | `postgres://multica:multica@localhost:5432/multica`（已迁移，`schema_migrations` 303 条） |

## 二、基线复现：3 个目标失败

| # | 失败测试 | 位置 | 基线(0ab7ea6e) | 特性(625c7421) | 判定 |
|---|----------|------|:---:|:---:|------|
| 1 | `TestMigrationNumericPrefixesStayUniqueAfterLegacySet` | `internal/migrations/migrations_lint_test.go:91` | ❌ FAIL | ❌ FAIL | 前缀 202/203/232–239 复用，**两侧逐字一致** |
| 2 | `TestPreviewCommentTriggers_DelegateOfflineLeaderBlocked` | `internal/handler/comment_delegation_test.go:797` | ❌ FAIL | ❌ FAIL | `runtime_id` NOT NULL 违反，**两侧逐字一致** |
| 3 | `TestInFlightOldHeadKeepsTrailingRefresh` | `internal/integrations/ghsnapshot/refresh_db_test.go:360` | ❌ FAIL（`-count=3` 全败） | ❌ FAIL（`-count=5` 中 3 次） | 时序型 flaky，两侧均不稳定 |

**逐字一致证据**：对上述 3 个测试文件执行 `git diff 0ab7ea6e 625c7421` → **零差异**；`server/migrations/` 全目录 diff → **零差异**（teamtmpl 零新增 migration）。

## 三、失败根因归属（均非 teamtmpl）

1. **migration 前缀复用**：`202`/`203` 由 share-link + runtime-profile（上游 `67e58d0f` 同步并入）、
   `232–239` 由 issue-template / squad-hierarchy / delegation / workflow（LIU-9/LIU-13、CLO-161 等并入 feature）造成，
   全部存在于基线 `0ab7ea6e`。teamtmpl（SGD-23/25/27）改动仅 `teamtmpl/`、`handler/team_template*`、
   `router.go`、`queries`、模板 JSON 等 13 个文件，**不含任何 migration**。
2. **`comment_delegation_test.go`**：该文件由 LIU-9/LIU-13（`204fa25c`）引入并已在基线分支存在；
   `TestPreviewCommentTriggers_DelegateOfflineLeaderBlocked` 的测试助手建 agent 未带 `runtime_id`，
   撞 agent 表 NOT NULL 约束 —— 既有测试缺陷，非特性代码。
3. **`refresh_db_test.go`**：`TestInFlightOldHeadKeepsTrailingRefresh` 是 ghsnapshot 同步时序竞态，
   特性分支未触碰该包，属 flaky（两次全量 run 一次过一次挂，专项 `-count` 复现时断时续）。

## 四、附加发现（两侧一致，进一步佐证无回归）

除目标 3 项外，全量 run 在两侧**完全相同的附加失败**（`--- FAIL` 列表逐字一致），与 teamtmpl 同样无关：

| 失败 | 归属 |
|------|------|
| `TestDispatchAutopilotSuppressesRecentDuplicateIssue`（cmd/server） | pgx `field descriptions ≠ destinations (26/27)` —— 共享 DB schema 漂移/状态依赖，两侧一致 |
| `TestValidateLocalPath/rejects_a_symlink_pointing_at_the_user_home`（daemon） | 容器以 root 运行，`/root` 被识别为系统根而非 home 语义，环境 artifact，两侧一致 |
| `TestRunTask_InjectsPrivateTaskTempDir` 等 3 个 daemon workdir 测试 | Claude 拒绝在 root 下 `bypassPermissions`，环境 artifact，两侧一致 |
| `TestBuiltinSkillsConformToTemplate/multica-mentioning` | 基线 FAIL（description 1091>1024 cap）；**特性分支已修复**（SGD-27 `SKILL.md` 2 行改动缩短描述 → 通过） |
| `TestCreateComment_DelegateOfflineLeaderParksBacklog`（handler） | 与 #2 同根因（`runtime_id` NOT NULL），两侧一致 |

> 注：附加失败均为容器 root 身份 / 共享 DB 状态 / 既有测试缺陷，且两侧 run 输出一致，
> 说明特性分支相对基线**未引入任何新失败**。

## 五、特性侧通过项（QA 已确认，DevOps 复核）

- `internal/teamtmpl/...`：`go test -race` 全绿（loader 10 条校验、apply 首建 10 agent+3 squad+1 skill、重复 apply 全 reused、坏 URL 422、事务回滚无残留）
- `go build ./server/...` 编译通过

## 六、结论与建议

1. **3 个失败均在基线可复现、内容一致，与 team-template 无关**，不构成特性回归阻塞。
2. 建议将 migration 前缀复用（232–239）与 delegation 测试缺陷作为**既有技术债**另行派单修复，
   供 stage 4 收口 PR 时处理，避免 `make test` 持续飘红。
3. 标准化容器化 race 回归环境（`scripts/test-go-docker.sh`）已入库，供 SGD-20 部署验证复用。

**基线**: `0ab7ea6e`（Equipment_Department_Exploration） **特性**: `625c7421`（feature/team-template）
**复现命令**: `bash scripts/test-go-docker.sh --race`（详见 `race-regression-env.md`）
