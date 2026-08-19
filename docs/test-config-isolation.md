# multica 测试配置隔离规范（CLO-650 / CLO-651 防复发）

> 生效日期：2026-08-19。适用于所有在用户本机跑 multica 测试 / 调试 / 环境改动的任务。
> 违反本规范的测试会覆盖用户真实 `~/.multica/config.json`，导致用户 multica 无法连接真实服务器。

## 背景：2026-08-19 事故

用户（DESKTOP-FQNI6Q5 / Windows 11）的 `C:\Users\xuyi\.multica\config.json` 被测试活动覆盖：

| 时间 (2026-08-19) | 事件 |
|---|---|
| 00:57:47 | `config.json.clobbered-by-test.bak` 生成，内容 `server_url: http://127.0.0.1:61149, token: token-default` |
| 02:10:28 | `config.json.pre-restore.bak` 生成，内容 `server_url: http://127.0.0.1:58987, token: token-default` |
| 17:30:31 | 根因验证复现时再次写入 `http://127.0.0.1:55213` |

**根因**：`server/cmd/multica/cmd_daemon_diskusage_status_test.go`（及其它调用
`cli.SaveCLIConfigForProfile(..., "")` / `cli.SaveCLIConfig(...)` 的测试）用
`t.Setenv("HOME", t.TempDir())` 隔离配置目录，但 **Go 的 `os.UserHomeDir()` 在 Windows 上
读取 `%USERPROFILE%` 而非 `HOME`**（`os.UserHomeDir` 的平台实现差异）。于是：
- 测试设置的 `HOME=<临时目录>` 被忽略；
- `multicaConfigRoot()` 返回真实 `%USERPROFILE%`（即 `C:\Users\xuyi`）；
- `SaveCLIConfigForProfile(profile="")` 把真实 `~/.multica/config.json` 覆盖为
  `{server_url: <httptest 随机端口>, token: "token-default"}`。

覆盖后所有 `multica` 命令打到本地 mock 服务器（404）或本地测试地址，且 daemon 被
Application Control policy 拦截无法启动。

## 根因修复（已合入 multica 仓库）

让显式 `HOME` 优先于平台 home 查找，Windows 上与 POSIX 行为一致：

- `server/internal/cli/config.go` — `multicaConfigRoot()`：`HOME` 非空时返回 `HOME`，
  否则回退 `os.UserHomeDir()`。
- `server/internal/daemon/config.go` — `ResolveWorkspacesRoot()`：同理。
- `server/cmd/multica/cmd_daemon.go` — `profilesRootDir()`：同理。
- `server/internal/cli/config_test.go` — 新增 `TestCLIConfig_HomeWinsOverPlatformHome`
  防复发回归锁（旧代码下必失败）。

## 隔离规范（所有测试/调试任务必须遵守）

### 1. 禁止写用户默认配置

测试/调试**不得**覆盖用户真实 `~/.multica/config.json`。隔离方式（任选其一，推荐 1a）：

- **1a. 显式设置 `HOME`（跨平台一致）**：`$env:HOME = <临时目录>`，或在 Go 测试中用
  `t.Setenv("HOME", t.TempDir())` + `t.Setenv("USERPROFILE", t.TempDir())` 双重隔离
  （后者兜底旧平台行为）。这是 CLO-651 修复后的标准做法。
- **1b. 使用 `--profile <name>`**：写 `~/.multica/profiles/<name>/config.json`，不碰默认。
- **1c. 使用 `MULTICA_TASK_CONFIG_ROOT`**：daemon 管理的任务会注入该变量，配置落在
  task-local 目录。
- **1d. 容器 / VM 隔离**：团队约定 7 已要求测试环境优先用本地 VM/容器，不在本机裸跑。

### 2. 写配置前先备份

任何代码路径（含 mock/测试 helper）在写 `config.json` 前，若目标文件已存在且指向真实
服务器，先复制为 `config.json.<描述>.bak`。恢复脚本见 `scripts/restore-multica-config.ps1`。

### 3. 测试结束清理

- mock 服务器（`multica.test.exe` 等）测试结束后必须退出，不得残留占用端口
  （事故中 `127.0.0.1:61149` 被长期占用）。
- 一次性计划任务（如 `multica-e2e-env`、`multica-web-clo503`）不得留存在用户机器。
- 清理脚本见 `scripts/cleanup-test-env.ps1`。

### 4. 收尾验证

动过环境（配置/端口/进程/文件）的任务，交付总结必须包含「环境还原验证」段落：
- 证明 `~/.multica/config.json` 未被测试改动（比对 hash 或 `multica config show` 指向真实服务器）；
- 列出清理掉的进程/端口/计划任务。

## 本机恢复验证步骤（用户机器）

```powershell
# 1. 一键恢复真实配置
powershell -ExecutionPolicy Bypass -File scripts/restore-multica-config.ps1

# 2. 验证指向真实服务器
multica config show

# 3. 固定验证命令：确认测试隔离生效（不写真实配置）
#    在 multica/server 下，HOME 指向临时目录运行 diskusage 测试，
#    并比对 ~/.multica/config.json 的 SHA256 在运行前后不变。
$before = (Get-FileHash "$env:USERPROFILE\.multica\config.json").Hash
$env:HOME = "$env:TEMP\multica-iso-home"
go test ./cmd/multica/ -run "TestRunDaemonDiskUsage|TestResolveDiskUsageRoot" -count=1
$after = (Get-FileHash "$env:USERPROFILE\.multica\config.json").Hash
$before -eq $after   # 必须为 True
```

## 参考

- 团队约定 7「环境安全红线」：team-engineering-governance `standards/team-working-agreements.md`
  （commit `1cf480b4`）。
- 根因 issue：CLO-650 / CLO-651。
