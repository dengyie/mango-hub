# =====================================================================
# power_governor.ps1 v1 — SRBMiner 用户活动自适应功率调节器
# 三档: full(空闲 100%) / eco(人在用 ~70%) / pause(重 GPU 应用临时停挖)
#
# 同一份脚本部署两台 Windows, 按机器名选 Lever:
#   DESKTOP-VST7EIE home-win   : nvidia-smi -pl  (150W 甜点档 / 70% -> 105W)
#   DESKTOP-QAU15K5 laptop-win : nvidia-smi -lgc (1260MHz / 70% -> 880MHz, OEM 锁 -pl)
#
# 只交互两样东西: srbminer 服务 + nvidia-smi。不碰驱动墙配置本身——
# 用户自己的 GPU 项目/游戏要满血(铁律)。矿工没在跑(含用户手动停)时
# 一律把功耗墙还给驱动默认, 顺手修掉历史"150W 残留"老坑。
#
# 部署(两机):
#   D:\program\srbminer\power_governor.ps1   (UTF-8 BOM, PS5.1)
#   D:\program\srbminer\power_governor_hidden.vbs
#   计划任务 SRBMinerPowerGovernor  (onlogon, mango, /it /rl highest, VBS 壳)
#   计划任务 SRBMinerPowerGuard     (每 10 分钟, SYSTEM, -Enforce 兜底)
# 手动熔断: 新建 D:\program\srbminer\governor.off -> 恢复满血并退出
# 测试: -ForceTier eco|full|pause   -Once   -DryRun
# =====================================================================
param(
    [ValidateSet('full','eco','pause')][string]$ForceTier = '',  # 测试: 跳过检测直接应用某档
    [switch]$Once,        # 跑一个周期后退出
    [switch]$Enforce,     # 供 guard: 无用户会话则强制 full; 有会话则确保 daemon 活着
    [switch]$DryRun,      # 只评估不改动
    [double]$HeavyPct = 0,   # 覆盖重载阈值(%), 0=用默认 20 (调优/自测用)
    [int]$IdleBackSec = 0,   # 覆盖回满空闲秒数, 0=用默认 120
    [int]$HoldPolls = 0      # 覆盖重载确认采样次数, 0=用默认 2
)

[Console]::OutputEncoding=[Text.Encoding]::UTF8
$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'

$HostN = $env:COMPUTERNAME
$Svc   = 'srbminer'
$Dir   = 'D:\program\srbminer'
$GovDir    = "$Dir\governor"
$StateFile = "$GovDir\state.json"
$LogFile   = "$GovDir\governor.log"
$KillFile  = "$Dir\governor.off"
$TaskName  = 'SRBMinerPowerGovernor'

$IdleBackFullSec = 120        # 连续无输入多久回 100%
$HeavyGpuPct     = 20.0       # 用户侧 GPU 争用阈值(%), 判重载
$HeavyHoldPolls  = 2          # 连续确认重载的采样次数(防抖)
$PollSec         = 10
$script:heavyRun = 0

# 命令行覆盖(仅测试/调优; daemon 由计划任务裸启动, 全部走默认值)
if ($HeavyPct    -gt 0) { $HeavyGpuPct     = $HeavyPct }
if ($IdleBackSec -gt 0) { $IdleBackFullSec = $IdleBackSec }
if ($HoldPolls   -gt 0) { $HeavyHoldPolls  = $HoldPolls }

# ---- 每机配置(唯一差异点) ---
$Profiles = @{
    'DESKTOP-VST7EIE' = @{ Lever='plimit'; Full=150;  EcoPercent=70; Wall=240 }
    'DESKTOP-QAU15K5' = @{ Lever='cclock'; Full=1260; EcoPercent=70; Wall=0   }
}
if (-not $Profiles.ContainsKey($HostN)) {
    Write-Output "power_governor: unknown host $HostN, refuse to run"
    exit 1
}
$P = $Profiles[$HostN]
if ($P.Lever -eq 'plimit') { $Eco = [int][math]::Max(100, [int][math]::Round($P.Full * $P.EcoPercent/100)) }
else                       { $Eco = [int](([math]::Round($P.Full * $P.EcoPercent/100 /10)) * 10) }

# ============ 基础 ============
function Log([string]$msg) {
    $line = "[{0}] {1}" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $msg
    try { Add-Content -Path $LogFile -Value $line -Encoding UTF8 -ErrorAction Stop } catch {}
    # Write-Host 而非 Write-Output: 日志绝不能进管道, 否则会污染函数返回值
    Write-Host $line
}

function Rotate-Log {
    try {
        if ((Test-Path $LogFile) -and ((Get-Item $LogFile).Length -gt 1MB)) {
            Remove-Item "$LogFile.1" -Force -ErrorAction SilentlyContinue
            Rename-Item $LogFile "$LogFile.1" -Force
            Log "log rotated"
        }
    } catch {}
}

# ============ 状态 ============
function Read-State {
    $d = @{ tier='full'; since=(Get-Date).ToString('o'); idleSec=0; gpuPct=0.0; gpuPid=0; pausedByGov=$false }
    if (Test-Path $StateFile) {
        try {
            $j = Get-Content $StateFile -Raw -Encoding UTF8 | ConvertFrom-Json
            # 容错: 历史上出现过 state.json 被写成数组(函数返回值污染), 取最后一个含 tier 的对象
            if ($j -is [array]) { $j = $j | Where-Object { $_.tier } | Select-Object -Last 1 }
            if ($j -and $j.tier)  { $d.tier  = [string]$j.tier }
            if ($j -and $j.since) { $d.since = [string]$j.since }
            if ($j) { $d.pausedByGov = [bool]$j.pausedByGov }
        } catch {}
    }
    return $d
}

function Write-State($d) {
    try {
        New-Item -ItemType Directory -Path $GovDir -Force | Out-Null
        ($d | ConvertTo-Json) | Set-Content -Path $StateFile -Encoding UTF8
    } catch {}
}

# ============ 检测 ============
Add-Type -Namespace Gov -Name U -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool GetLastInputInfo(ref LASTINPUTINFO plii);
[StructLayout(LayoutKind.Sequential)] public struct LASTINPUTINFO { public uint cbSize; public uint dwTime; }
'@ -ErrorAction SilentlyContinue

function Get-IdleSec {
    try {
        $li = New-Object -TypeName 'Gov.U+LASTINPUTINFO'
        $li.cbSize = [uint32][System.Runtime.InteropServices.Marshal]::SizeOf($li)
        if ([Gov.U]::GetLastInputInfo([ref]$li)) {
            return [math]::Round(([Environment]::TickCount - $li.dwTime)/1000.0)
        }
    } catch {}
    return 99999
}

function Get-Locked { return ((Get-Process -Name LogonUI -ErrorAction SilentlyContinue) -ne $null) }

# 返回 (用户侧 GPU 争用最大值%, 最忙进程 pid); 排除矿工自身
function Get-ForeignGpu {
    $minerPids = @{}
    try {
        $mm = Get-CimInstance Win32_Process -Filter "name='SRBMiner-MULTI.exe'" -ErrorAction SilentlyContinue
        foreach ($m in $mm) { if ($m.ProcessId) { $minerPids[[int]$m.ProcessId] = $true } }
    } catch {}
    try {
        $c = Get-Counter -Counter '\GPU Engine(*)\Utilization Percentage' -SampleInterval 1 -MaxSamples 1 -ErrorAction Stop
        $best = 0.0; $pidBest = 0
        foreach ($s in $c.CounterSamples) {
            $v = [double]$s.CookedValue
            if ($v -le 0) { continue }
            if ($s.InstanceName -match 'pid_([0-9]+)') {
                $p = [int]$Matches[1]
                if ($minerPids.ContainsKey($p)) { continue }
                if ($v -gt $best) { $best = $v; $pidBest = $p }
            }
        }
        return $best, $pidBest
    } catch {
        return -1.0, 0
    }
}

function Get-UserSession {
    try {
        $ex = Get-CimInstance Win32_Process -Filter "name='explorer.exe'" -ErrorAction SilentlyContinue
        foreach ($e in $ex) { if ($e.SessionId -gt 0) { return $true } }
    } catch {}
    return $false
}

function Get-DaemonRunning {
    try {
        $me = $PID
        $ps = Get-CimInstance Win32_Process -Filter "name='powershell.exe'" -ErrorAction SilentlyContinue
        foreach ($p in $ps) {
            if ($p.ProcessId -eq $me) { continue }
            if ($p.CommandLine -and $p.CommandLine -match 'power_governor\.ps1' -and $p.CommandLine -notmatch '\-(Once|Enforce|ForceTier)') { return $true }
        }
    } catch {}
    return $false
}

# ============ 执行器 ============
function Invoke-Nvidia([string[]]$argList) {
    $out = (& nvidia-smi @argList 2>&1 | Out-String)
    Log "nvidia-smi $($argList -join ' ') -> $($out.Trim())"
    return ($LASTEXITCODE -eq 0)
}

function Apply-Power([string]$profile) {
    # profile: full | eco | pause | wall
    if ($P.Lever -eq 'plimit') {
        switch ($profile) {
            'eco'   { return Invoke-Nvidia @('-pl', "$Eco") }
            'full'  { return Invoke-Nvidia @('-pl', "$($P.Full)") }
            default { return Invoke-Nvidia @('-pl', "$($P.Wall)") }   # pause / wall
        }
    } else {
        switch ($profile) {
            'eco'   { return Invoke-Nvidia @('-lgc', "$Eco,$Eco") }
            'full'  { return Invoke-Nvidia @('-lgc', "$($P.Full),$($P.Full)") }
            default { return Invoke-Nvidia @('-rgc') }                 # pause / wall
        }
    }
}

# 把 srbminer 服务与功耗墙调到 target 档所要求的状态
function Apply-Tier([string]$target, $state) {
    if ($DryRun) { Log "[dry] would apply tier=$target (service=$((Get-Service $Svc -ErrorAction SilentlyContinue).Status))"; return $state }

    $svcObj = Get-Service $Svc -ErrorAction SilentlyContinue
    if (-not $svcObj) { Log "service $Svc missing; restore full wall"; $null = Apply-Power 'wall'; return $state }

    $st = $svcObj.Status
    if ($st -eq 'StartPending' -or $st -eq 'StopPending') { Log "service $Svc $st, skip actuator this cycle"; return $state }

    if ($target -eq 'pause') {
        if ($st -eq 'Running') {
            Stop-Service $Svc -Force -ErrorAction SilentlyContinue
            $state.pausedByGov = $true
            Log "tier=pause: stop $Svc; restore full wall so your GPU app has full power"
        } else {
            Log "tier=pause: $Svc already stopped (st=$st); restore full wall"
        }
        $null = Apply-Power 'wall'
        return $state
    }

    # target = eco / full
    if ($st -ne 'Running') {
        if ($state.pausedByGov) {
            Start-Service $Svc -ErrorAction SilentlyContinue
            Start-Sleep -Seconds 3
            $state.pausedByGov = $false
            Log "tier=${target}: resume $Svc (restore our own pause)"
            $null = Apply-Power $target
        } else {
            Log "tier=${target}: $Svc stopped by user, leave it; restore full wall"
            $null = Apply-Power 'wall'
        }
        return $state
    }

    if ($state.pausedByGov) { $state.pausedByGov = $false; Log "cleared pausedByGov (user restarted $Svc manually)" }
    $null = Apply-Power $target
    return $state
}

# ============ 单周期 ============
function Run-Cycle($state) {
    $idle   = Get-IdleSec
    $locked = Get-Locked
    $gpuPct, $gpuPid = Get-ForeignGpu
    if ($gpuPct -lt 0) { $gpuPct = 0 }

    if ($gpuPct -ge $HeavyGpuPct) { $script:heavyRun += 1 } else { $script:heavyRun = 0 }

    if ($script:heavyRun -ge $HeavyHoldPolls) {
        $target = 'pause'
    } elseif ($idle -ge $IdleBackFullSec -or $locked) {
        $target = 'full'
    } else {
        $target = 'eco'
    }

    $state.idleSec = $idle
    $state.gpuPct  = $gpuPct
    $state.gpuPid  = $gpuPid

    if ($target -ne $state.tier) {
        Log ("tier {0} -> {1}  (idle={2}s locked={3} foreignGpu={4}% pid={5} heavyRun={6})" -f $state.tier, $target, $idle, $locked, $gpuPct, $gpuPid, $script:heavyRun)
        $state.tier  = $target
        $state.since = (Get-Date).ToString('o')
        $state = Apply-Tier $target $state
    }
    Write-State $state
    return $state
}

# ============ 入口 ============
New-Item -ItemType Directory -Path $GovDir -Force | Out-Null
Rotate-Log
Log "=== power_governor start host=$HostN lever=$($P.Lever) full=$($P.Full) eco=$Eco idleBack=${IdleBackFullSec}s heavy=$HeavyGpuPct%x$HeavyHoldPolls mode=$(if($ForceTier){"force:$ForceTier"}elseif($Enforce){'enforce'}elseif($Once){'once'}else{'daemon'}) ==="

if (Test-Path $KillFile) {
    Log "kill-switch present ($KillFile); restore full wall and exit"
    $null = Apply-Power 'wall'
    exit 0
}

$state = Read-State

if ($ForceTier) {
    Log "apply forced tier=$ForceTier (was $($state.tier))"
    $state.tier  = $ForceTier
    $state.since = (Get-Date).ToString('o')
    $state = Apply-Tier $ForceTier $state
    Write-State $state
    exit 0
}

if ($Enforce) {
    if (-not (Get-UserSession)) {
        Log "enforce: no interactive user session; force full power"
        if ($state.tier -ne 'full') { $state.tier = 'full'; $state.since = (Get-Date).ToString('o') }
        $null = Apply-Power 'full'
        Write-State $state
    } elseif (-not (Get-DaemonRunning)) {
        Log "enforce: user session present but daemon absent; kick task $TaskName"
        $null = & schtasks.exe /run /tn $TaskName 2>&1
    } else {
        Log "enforce: daemon alive, nothing to do"
    }
    exit 0
}

if ($Once) {
    $null = Run-Cycle $state
    exit 0
}

# ---- daemon(默认) ----
$mtx = New-Object System.Threading.Mutex($false, 'Global\SrbMinerPowerGovernor')
if (-not $mtx.WaitOne(0)) { Log "another daemon instance holds the lock; exit"; exit 0 }
try {
    $state = Run-Cycle $state
    while ($true) {
        Start-Sleep -Seconds $PollSec
        if (Test-Path $KillFile) { Log "kill-switch appeared; exit daemon"; break }
        $state = Run-Cycle $state
    }
} catch {
    Log "FATAL: $($_.Exception.Message)"
} finally {
    try { $mtx.ReleaseMutex() } catch {}
    Log "=== power_governor daemon stopped ==="
}
