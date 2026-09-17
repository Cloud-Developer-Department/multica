# restore-multica-config.ps1 - 一键恢复被测试覆盖的 ~/.multica/config.json
#
# CLO-650 / CLO-651 防复发工具：任何测试/调试若覆盖了默认 profile 的
# ~/.multica/config.json（本地测试地址 + token-default），运行本脚本可恢复。
#
# 用法:
#   powershell -ExecutionPolicy Bypass -File scripts/restore-multica-config.ps1
#
# 行为:
#   1. 若 ~/.multica/config.json 已指向真实服务器(server_url 非 127.0.0.1/localhost)则跳过。
#   2. 备份当前(被污染的)配置为 config.json.pre-restore-<时间戳>.bak。
#   3. 从已知备份恢复（按优先级）:
#      a. config.json.selfhost-clouddeveloper.bak
#      b. config.json.pre-restore.bak
#      c. 用户手工提供路径
#   4. 校验恢复结果 hash 并输出。
#
# 边界: 本脚本不触碰 ~/.multica/bin、不卸载/禁用任何测试残留进程，只恢复配置。

$ErrorActionPreference = "Stop"

$ConfigDir = Join-Path $env:USERPROFILE ".multica"
$ConfigPath = Join-Path $ConfigDir "config.json"

function Read-Json {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return $null }
    try { return Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json } catch { return $null }
}

function Get-Hash {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return "" }
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash
}

if (-not (Test-Path -LiteralPath $ConfigPath)) {
    Write-Host "no config at $ConfigPath, nothing to restore" -ForegroundColor Yellow
    exit 0
}

$current = Read-Json $ConfigPath
$server = $current.server_url

if ($server -and $server -notmatch "127\.0\.0\.1|localhost" -and $server -match "^https?://") {
    Write-Host "config already points at real server: $server - no restore needed" -ForegroundColor Green
    exit 0
}

Write-Host "config points at test server: $server - restoring" -ForegroundColor Yellow

# 候选备份（优先真实 selfhost 备份）
$candidates = @()
$selfhost = Join-Path $ConfigDir "config.json.selfhost-clouddeveloper.bak"
$preRestore = Join-Path $ConfigDir "config.json.pre-restore.bak"
$cliRestore = Join-Path $ConfigDir "config.json.selfhost.bak"
if (Test-Path -LiteralPath $selfhost) { $candidates += $selfhost }
if (Test-Path -LiteralPath $cliRestore) { $candidates += $cliRestore }
if (Test-Path -LiteralPath $preRestore) { $candidates += $preRestore }

if ($candidates.Count -eq 0) {
    Write-Host "no known-good backup found. manually supply one:" -ForegroundColor Red
    Write-Host "  Copy-Item <backup> $ConfigPath"
    exit 1
}

$chosen = $candidates[0]
Write-Host "using backup: $chosen" -ForegroundColor Cyan

$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$polluted = Join-Path $ConfigDir "config.json.pre-restore-$stamp.bak"
Copy-Item -LiteralPath $ConfigPath -Destination $polluted -Force
Write-Host "backed up polluted config to $polluted" -ForegroundColor DarkGray

Copy-Item -LiteralPath $chosen -Destination $ConfigPath -Force

$restored = Read-Json $ConfigPath
Write-Host "restored server_url: $($restored.server_url)" -ForegroundColor Green
Write-Host "restored hash: $(Get-Hash $ConfigPath)" -ForegroundColor Green
Write-Host "verify with: multica config show" -ForegroundColor Cyan
