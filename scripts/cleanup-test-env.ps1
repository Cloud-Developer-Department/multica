# cleanup-test-env.ps1 - 清理本机测试环境残留（供 delivery-devops 执行）
#
# CLO-650 / CLO-651 交付：一次性清理 2026-08-19 凌晨破坏用户 multica 配置的
# 测试环境残留。
#
# 执行说明（交给 delivery-devops / 用户确认后手动执行，本子任务只交付脚本）:
#   powershell -ExecutionPolicy Bypass -File scripts/cleanup-test-env.ps1
#
# 处理对象:
#   1. 计划任务 multica-e2e-env（supervisor.ps1 无限循环拉起 PG/server/web）
#   2. 计划任务 multica-web-clo503（start-web-clo503.cmd）
#   3. 目录 C:\Users\xuyi\multica_e2e_env（已重命名为 .disabled-20260819，防复发）
#   4. 目录 C:\Users\xuyi\multica_workspaces\...\40a90ef6 等测试 workdir 下的
#      .multica\daemon_task_context.json 残留 marker（可选）
#
# 注意: 本脚本默认 DRY-RUN 只打印将执行的命令；加 -Apply 才真正执行。

param(
    [switch]$Apply
)

$E = "C:\Users\xuyi\multica_e2e_env"
$EDisabled = "C:\Users\xuyi\multica_e2e_env.disabled-20260819"

function Invoke-Step {
    param([string]$ScriptBlock, [string]$Desc)
    Write-Host "`n==> $Desc" -ForegroundColor Cyan
    if ($Apply) {
        Invoke-Expression $ScriptBlock
    } else {
        Write-Host "[DRY-RUN] $ScriptBlock" -ForegroundColor DarkGray
    }
}

# 1) 计划任务
foreach ($task in @("multica-e2e-env", "multica-web-clo503")) {
    $exists = schtasks /query /tn $task 2>$null
    if ($LASTEXITCODE -eq 0) {
        Invoke-Step "schtasks /end /tn $task; schtasks /delete /tn $task /f" "删除计划任务 $task"
    } else {
        Write-Host "计划任务 $task 不存在（已清理），跳过" -ForegroundColor DarkYellow
    }
}

# 2) e2e 环境目录
if (Test-Path -LiteralPath $E) {
    Invoke-Step "Remove-Item -LiteralPath '$E' -Recurse -Force" "删除测试环境目录 $E"
} elseif (Test-Path -LiteralPath $EDisabled) {
    Invoke-Step "Remove-Item -LiteralPath '$EDisabled' -Recurse -Force" "删除已禁用的测试环境目录 $EDisabled"
} else {
    Write-Host "e2e 环境目录不存在，跳过" -ForegroundColor DarkYellow
}

# 3) 检查是否还有测试服务进程占用端口（不应残留）
$ports = @(5432, 18080, 3000, 61149, 58987, 55213)
foreach ($p in $ports) {
    $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
    if ($conn) {
        $procId = ($conn | Select-Object -First 1).OwningProcess
        $procName = (Get-Process -Id $procId -ErrorAction SilentlyContinue).ProcessName
        Write-Host "端口 $p 被 $procName (PID $procId) 占用" -ForegroundColor DarkYellow
        Invoke-Step "Stop-Process -Id $procId -Force" "结束占用端口 $p 的进程 $procName"
    } else {
        Write-Host "端口 $p 空闲" -ForegroundColor DarkGray
    }
}

Write-Host "`n完成。"
if (-not $Apply) {
    Write-Host "DRY-RUN 结束：确认无误后加 -Apply 真正执行。" -ForegroundColor Yellow
}
