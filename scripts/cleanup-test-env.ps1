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

# Confirm-TestResidueProcess 校验一个 PID 是否可证明属于本次测试残留。
# 仅当进程的可执行路径或命令行能追溯到测试环境（multica_e2e_env 目录或
# Go 编译的 *.test.exe 测试二进制）时才返回 Proven=$true；任何无法证明
# 归属的进程一律 Proven=$false（fail closed：不生成也不执行 Stop-Process）。
function Confirm-TestResidueProcess {
    param([int]$ProcId)
    if ($ProcId -le 0) { return @{ Proven = $false; Reason = "无有效 PID" } }
    $proc = Get-Process -Id $ProcId -ErrorAction SilentlyContinue
    if (-not $proc) { return @{ Proven = $false; Reason = "PID $ProcId 已不存在" } }
    $name = $proc.ProcessName
    $cim = Get-CimInstance Win32_Process -Filter "ProcessId=$ProcId" -ErrorAction SilentlyContinue
    $exePath = $null
    $cmdLine = $null
    if ($cim) {
        $exePath = $cim.ExecutablePath
        $cmdLine = $cim.CommandLine
    }
    # 1) Go 编译的测试二进制（multica.test.exe 等）—— httptest 随机端口残留的典型归属。
    if ($name -and $name -match '\.test$') {
        return @{ Proven = $true; Reason = "进程名 $name 匹配 Go 测试二进制 (*.test.exe)" }
    }
    if ($exePath -and $exePath -match '\.test\.exe$') {
        return @{ Proven = $true; Reason = "可执行路径 $exePath 匹配 Go 测试二进制 (*.test.exe)" }
    }
    # 2) 可执行路径落在测试环境目录下。
    if ($exePath) {
        foreach ($base in @($E, $EDisabled)) {
            if ($base -and $exePath -like "$base*") {
                return @{ Proven = $true; Reason = "可执行路径 $exePath 位于测试环境目录 $base 下" }
            }
        }
    }
    # 3) 命令行引用测试环境目录（supervisor.ps1 拉起的 server/web 进程）。
    if ($cmdLine -and $cmdLine -like "*multica_e2e_env*") {
        return @{ Proven = $true; Reason = "命令行引用测试环境目录 multica_e2e_env" }
    }
    return @{ Proven = $false; Reason = "无法证明 PID $ProcId ($name) 属于测试残留 (exe=$exePath)" }
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
#    仅终止「可证明属于本次测试残留」的进程；归属未明的只报告不终止
#    （fail closed，CLO-872 整改要求）。5432 见第 4 步永久保护。
$testPorts = @(18080, 3000, 61149, 58987, 55213)
foreach ($p in $testPorts) {
    $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
    if (-not $conn) {
        Write-Host "端口 $p 空闲" -ForegroundColor DarkGray
        continue
    }
    $procId = ($conn | Select-Object -First 1).OwningProcess
    $procName = (Get-Process -Id $procId -ErrorAction SilentlyContinue).ProcessName
    $verdict = Confirm-TestResidueProcess -ProcId $procId
    if ($verdict.Proven) {
        Write-Host "端口 $p 被 $procName (PID $procId) 占用 —— 已证明归属：$($verdict.Reason)" -ForegroundColor DarkYellow
        Invoke-Step "Stop-Process -Id $procId -Force" "结束占用端口 $p 的测试残留进程 $procName (PID $procId)"
    } else {
        Write-Host "端口 $p 被 $procName (PID $procId) 占用 —— 归属未明，仅报告不终止：$($verdict.Reason)" -ForegroundColor Red
    }
}

# 4) 受保护端口（Docker 等共享服务，非测试残留）：只报告，绝不 Stop-Process
$protectedPorts = @(5432)
foreach ($p in $protectedPorts) {
    $conn = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue
    if ($conn) {
        $procId = ($conn | Select-Object -First 1).OwningProcess
        $procName = (Get-Process -Id $procId -ErrorAction SilentlyContinue).ProcessName
        Write-Host "端口 $p 被 $procName (PID $procId) 占用（受保护，不终止）" -ForegroundColor DarkYellow
    } else {
        Write-Host "端口 $p 空闲（受保护）" -ForegroundColor DarkGray
    }
}

Write-Host "`n完成。"
if (-not $Apply) {
    Write-Host "DRY-RUN 结束：确认无误后加 -Apply 真正执行。" -ForegroundColor Yellow
}
