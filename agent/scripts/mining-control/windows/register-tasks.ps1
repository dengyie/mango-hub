# =====================================================================
# register-tasks.ps1 — 落地 governor 套件 + 注册/刷新三个计划任务
#
# 用法(在目标机, 提权 PowerShell):
#   1) 把本目录全部文件拷到同一临时目录, 例如 C:\Users\<you>\govkit\
#   2) powershell -NoProfile -ExecutionPolicy Bypass -File .\register-tasks.ps1
#
# 幂等: /f 覆盖重建任务; 脚本按字节拷贝, 保留 BOM / CRLF。
# 编码硬约束: .ps1 = UTF-8 BOM + LF; .vbs = CRLF + 纯 ASCII。
# =====================================================================
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$ErrorActionPreference = 'Stop'

$Src = Split-Path -Parent $MyInvocation.MyCommand.Path
$Dst = 'D:\program\srbminer'
$User = $env:USERNAME

if (-not (Test-Path $Dst)) { throw "target dir not found: $Dst (SRBMiner 未安装?)" }
New-Item -ItemType Directory -Force -Path "$Dst\governor" | Out-Null

# ---- 1) 拷脚本(字节级, 不改编码) ----
foreach ($f in 'power_governor.ps1', 'power_governor_hidden.vbs', 'power_guard_hidden.vbs', 'tray_control.ps1', 'tray_start.vbs') {
    $s = Join-Path $Src $f
    if (-not (Test-Path $s)) { throw "missing source file: $s" }
    Copy-Item -LiteralPath $s -Destination (Join-Path $Dst $f) -Force
    $b = [IO.File]::ReadAllBytes((Join-Path $Dst $f))
    $bom = ($b.Length -ge 3 -and $b[0] -eq 0xEF -and $b[1] -eq 0xBB -and $b[2] -eq 0xBF)
    $crlf = ([Text.Encoding]::ASCII.GetString($b) -match "`r`n")
    Write-Output ("{0,-30} len={1,-6} bom={2,-5} crlf={3}" -f $f, $b.Length, $bom, $crlf)
}

# ---- 2) 语法检查(只解析, 不执行) ----
$errs = $null
[void][System.Management.Automation.Language.Parser]::ParseFile("$Dst\power_governor.ps1", [ref]$null, [ref]$errs)
if ($errs) {
    Write-Output 'PARSE ERRORS:'
    foreach ($e in $errs) { Write-Output ("  line {0}: {1}" -f $e.Extent.StartLineNumber, $e.Message) }
    throw 'power_governor.ps1 语法错误, 中止注册'
}
Write-Output 'PARSE OK'

# ---- 3) 注册/刷新任务 ----
# Governor: 登录时以交互用户 + 最高权限启动(改 -pl/-lgc 必须提权)
& schtasks.exe /create /tn SRBMinerPowerGovernor /tr "wscript.exe //B $Dst\power_governor_hidden.vbs" /sc onlogon /ru $User /rl highest /it /f | Out-Null
Write-Output ("SRBMinerPowerGovernor create exit: " + $LASTEXITCODE)

# Guard: 每 10 分钟以 SYSTEM 兜底(注销/崩溃/矿工重启刷回 plimit 的窗口)
& schtasks.exe /create /tn SRBMinerPowerGuard /tr "wscript.exe //B $Dst\power_guard_hidden.vbs" /sc minute /mo 10 /ru SYSTEM /f | Out-Null
Write-Output ("SRBMinerPowerGuard create exit: " + $LASTEXITCODE)

# Tray: 登录时拉起托盘
& schtasks.exe /create /tn SRBMinerTrayStart /tr "wscript.exe /b $Dst\tray_start.vbs" /sc onlogon /ru $User /it /f | Out-Null
Write-Output ("SRBMinerTrayStart create exit: " + $LASTEXITCODE)

# ---- 4) 校验 ----
Write-Output '--- tasks ---'
foreach ($t in 'SRBMinerPowerGovernor', 'SRBMinerPowerGuard', 'SRBMinerTrayStart') {
    $i = Get-ScheduledTaskInfo -TaskName $t
    $s = (Get-ScheduledTask -TaskName $t).State
    Write-Output ("{0,-24} state={1,-8} last={2}" -f $t, $s, $i.LastRunTime)
}
Write-Output '--- apiUrl (托盘; 每机唯一差异行) ---'
(Get-Content -LiteralPath "$Dst\tray_control.ps1" | Select-String '^\$apiUrl').Line
