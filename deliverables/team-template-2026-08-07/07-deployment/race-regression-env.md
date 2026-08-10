# 容器化 race 回归环境标准（devops-agent）

> 解决：本机无 gcc 时 `make test`（带 `--race`）立即失败
> `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` 的环境根因。

## 标准环境

- **镜像**: `golang:1.25`（自带 gcc / cgo，满足 `-race` 编译）
- **网络**: `--network host`（直连宿主 DB：`localhost:5432/multica`）
- **模块缓存**: 挂载宿主 `~/go/pkg/mod` → 容器 `/go/pkg/mod`（只读）
- **模块代理**: `GOPROXY=off`（离线、可复现、hermetic）
- **GOFLAGS**: `-mod=mod`
- **测试入口**: `cd server && bash scripts/test-go.sh --race`（等价 `make test` 的 Go 部分）
- **数据库**: `DATABASE_URL=postgres://multica:multica@localhost:5432/multica?sslmode=disable`（复用宿主已迁移的 DB）

## 一键脚本 `scripts/test-go-docker.sh`

已入库共享特性分支 `feature/team-template`，用法（仓库根目录）：

```bash
bash scripts/test-go-docker.sh --race   # 全量 race 回归（等价 make test）
bash scripts/test-go-docker.sh          # 非 race 回归
```

可覆盖环境变量：

| 变量 | 默认 | 说明 |
|------|------|------|
| `GOLANG_IMAGE` | `golang:1.25` | 运行镜像 |
| `DATABASE_URL` | `postgres://multica:multica@localhost:5432/multica?sslmode=disable` | 测试库 DSN |
| `GO_MOD_CACHE` | `$HOME/go/pkg/mod` | 宿主模块缓存目录 |

脚本封装了容器启动 + 环境注入 + 调用 `scripts/test-go.sh`，本机无 gcc 也可跑完整 race 回归。

## 典型命令

```bash
# 全量 race 回归（最终回归的标准动作）
bash scripts/test-go-docker.sh --race

# 定位单包/单测失败
docker run --rm --network host \
  -v "$PWD:/workspace" -v "$HOME/go/pkg/mod:/go/pkg/mod" \
  -e GOPROXY=off -e GOMODCACHE=/go/pkg/mod \
  -e DATABASE_URL="postgres://multica:multica@localhost:5432/multica?sslmode=disable" \
  -w /workspace/server golang:1.25 \
  go test -race -run 'TestMigrationNumericPrefixesStayUniqueAfterLegacySet' ./internal/migrations/

# 指定分支（基线/特性）回归
#   git worktree add /tmp/multica-base-test <基线commit>  然后同上跑脚本
```

## 复用范围

- **阶段4 SGD-20 部署验证**：本环境可直接复用（同 DB、同模块缓存、同 race 入口），部署后执行
  `bash scripts/test-go-docker.sh --race` + 冒烟（`GET /api/team-templates` 等）。
- 任何后续 Go 回归 / 发布前验证。
