[Console]::OutputEncoding=[Text.Encoding]::UTF8
$ErrorActionPreference = 'Continue'

# --- tray app: SRBMiner 矿机控制 (service srbminer) v8 ---
# v8 (2026-09-12 review 修复):
#   - 修复 v7 P1: 自动恢复失败后永久停矿 —— 改为「Start 成功后才清 pauseUntil」,失败保留标记每
#     秒自动重试(限时多秒),且只弹一次失败/成功气泡不刷屏。
#   - 修复 v7 P2: 陈旧 pauseUntil 在服务被手动拉起的场景下"复活"自动恢复 —— 服务 Running 即视为
#     恢复完成并清标记;手动启动/停止统一经 Clear-Pause 清状态。
#   - 修复 v7 P2: StartPending/StopPending 不再误判为「已停止」。
#   - 重构: 暂停状态清理收敛为单一函数,消除三处重复。
# v6/v7: 单实例锁、只读信息区、防误触双击气泡、暂停30m/1h/2h/4h/8h 到点自动恢复。
# v9 (2026-11-27): 显示 power_governor 「自适应功率」档位 —— 读 governor\state.json,
#   菜单首行 + 悬停气泡 + 通知区图标配色(满速绿/节能琥珀/暂停红)三位一体;
#   调节器停写(>45s)、未部署或文件异常时静默退化,不影响原有启停/暂停功能。
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

$svcName = 'srbminer'
$apiUrl  = 'http://127.0.0.1:12001/api/v2/status'
$logFile = 'D:\program\srbminer\logs\srbminer.log'
$govStateFile = 'D:\program\srbminer\governor\state.json'   # power_governor 写的档位状态

# --- 暂停状态（脚本级变量）---
$script:pauseUntil     = $null   # DateTime 或 $null
$script:pauseLabel     = ''      # 档位文案，如「暂停 1 小时」
$script:autoToastShown = $false  # 本次恢复周期是否已弹过「成功恢复」气泡(防刷屏)
$script:failToastShown = $false  # 本次失败是否已弹过「自动恢复失败」气泡(防刷屏)

# --- 自适应档位显示状态 ---
$script:modeIcons     = @{}               # tier -> Icon(启动时一次性绘制,避免每秒重建 GDI 对象)
$script:modeBitmaps   = @{}               # 持有底层 Bitmap,防止 HICON 随 GC 失效


# --- 单实例锁：已有托盘实例在跑则直接退出（防双图标/双菜单）---
$mtx  = New-Object System.Threading.Mutex($false, 'Global\SrbMinerTraySingleInstance')
$gotLock = $false
try { $gotLock = $mtx.WaitOne(0) } catch { $gotLock = $false }
if (-not $gotLock) { exit 0 }

$ctx = New-Object System.Windows.Forms.ContextMenu

# --- 信息区（只读，轮询刷新）---
$mInfo1 = New-Object System.Windows.Forms.MenuItem('算力: 加载中…')
$mInfo1.Enabled = $false
$mInfo2 = New-Object System.Windows.Forms.MenuItem('GPU: …')
$mInfo2.Enabled = $false
$mInfo3 = New-Object System.Windows.Forms.MenuItem('份额: …')
$mInfo3.Enabled = $false
$mInfo4 = New-Object System.Windows.Forms.MenuItem('矿池: …')
$mInfo4.Enabled = $false
$mMode  = New-Object System.Windows.Forms.MenuItem('自适应功率: …')
$mMode.Enabled = $false

# --- 操作区 ---
$mToggle    = New-Object System.Windows.Forms.MenuItem('启动挖矿')
$mPause     = New-Object System.Windows.Forms.MenuItem('暂停挖矿')
$mPause30   = New-Object System.Windows.Forms.MenuItem('暂停 30 分钟')
$mPause1h   = New-Object System.Windows.Forms.MenuItem('暂停 1 小时')
$mPause2h   = New-Object System.Windows.Forms.MenuItem('暂停 2 小时')
$mPause4h   = New-Object System.Windows.Forms.MenuItem('暂停 4 小时')
$mPause8h   = New-Object System.Windows.Forms.MenuItem('暂停 8 小时')
$mPauseStop = New-Object System.Windows.Forms.MenuItem('立即停止（不自动恢复）')
$mLog       = New-Object System.Windows.Forms.MenuItem('打开矿工日志')
$mExit      = New-Object System.Windows.Forms.MenuItem('退出')

[void]$mPause.MenuItems.Add($mPause30)
[void]$mPause.MenuItems.Add($mPause1h)
[void]$mPause.MenuItems.Add($mPause2h)
[void]$mPause.MenuItems.Add($mPause4h)
[void]$mPause.MenuItems.Add($mPause8h)
[void]$mPause.MenuItems.Add('-')
[void]$mPause.MenuItems.Add($mPauseStop)

[void]$ctx.MenuItems.AddRange(@($mMode, $mInfo1, $mInfo2, $mInfo3, $mInfo4))
[void]$ctx.MenuItems.Add('-')
[void]$ctx.MenuItems.AddRange(@($mToggle, $mPause, $mLog))
[void]$ctx.MenuItems.Add('-')
[void]$ctx.MenuItems.Add($mExit)

$ico = New-Object System.Windows.Forms.NotifyIcon
$ico.ContextMenu = $ctx
$ico.Visible = $true
$ico.Text = 'SRBMiner 矿机'

# --- 自适应档位图标:通知区一眼看模式(满速绿/节能琥珀/暂停红)---
function New-DotIcon([string]$html) {
    $bmp   = New-Object System.Drawing.Bitmap -ArgumentList @(32, 32)
    $g     = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.Clear([System.Drawing.Color]::Transparent)
    $brush = New-Object System.Drawing.SolidBrush -ArgumentList @([System.Drawing.ColorTranslator]::FromHtml($html))
    $pen   = New-Object System.Drawing.Pen -ArgumentList @([System.Drawing.Color]::White, [single]2.5)
    $g.FillEllipse($brush, 5, 5, 22, 22)
    $g.DrawEllipse($pen, 5, 5, 22, 22)
    $g.Dispose(); $brush.Dispose(); $pen.Dispose()
    return @{ bmp = $bmp; icon = [System.Drawing.Icon]::FromHandle($bmp.GetHicon()) }
}
try {
    foreach ($kv in @(, @('full', '#22C55E')) + @(, @('eco', '#F59E0B')) + @(, @('pause', '#EF4444'))) {
        $r = New-DotIcon $kv[1]
        $script:modeIcons[$kv[0]]   = $r.icon
        $script:modeBitmaps[$kv[0]] = $r.bmp
    }
} catch {
    # 图标绘制失败(极端显示环境)时退化: 仍用菜单+悬停气泡显示档位, 不影响启停/暂停
    $script:modeIcons = @{}
    $script:modeBitmaps = @{}
}


# ---------- 状态与 UI ----------

# 服务 running 判定考虑中间态
function Get-SvcState {
    $svc = Get-Service $svcName -ErrorAction SilentlyContinue
    if (-not $svc) { return 'nosvc' }
    $s = $svc.Status
    if ($s -eq 'Running') { return 'running' }
    if ($s -eq 'StartPending' -or $s -eq 'StopPending') { return 'trans' }  # 中间态
    return 'stopped'
}

# 状态映射：running 且 API 有响应 = mining;否则按服务状态与暂停标记区分
function Get-State {
    $svstate = Get-SvcState
    if ($svstate -eq 'nosvc') { return @{ state='nosvc' } }
    if ($svstate -eq 'trans') { return @{ state='strans' } }   # 启动中/停止中/API 预热
    if ($svstate -eq 'stopped') {
        if ($script:pauseUntil) { return @{ state='stopped'; paused=$true } }
        return @{ state='stopped'; paused=$false }
    }
    # running 分支: 需 API 有响应才算挖矿中
    try {
        $r = Invoke-RestMethod $apiUrl -TimeoutSec 2   # 最长阻塞 2s, 见 review P2-4 备注
        $g = $r.gpu_devices[0]
        $s = $r.algorithms[0]
        $h = 0.0
        if ($s.hashrate.'1min' -ne $null) { $h = [double]$s.hashrate.'1min' / 1e12 }
        elseif ($r.hashrate.'1min' -ne $null) { $h = [double]$r.hashrate.'1min' / 1e12 }
        $hShow = if ($h -ge 1) { '{0:N1}' -f $h } else { '{0:N2}' -f $h }
        $hwErr = ($s.gpu_compute_errors.PSObject.Properties | ForEach-Object { "$($_.Name)=$($_.Value)" }) -join ' '
        return @{
            state='mining'
            info1="算力: $hShow TH/s（1分钟）"
            info2="GPU: $($g.model -replace '_',' ') $($g.temperature)°C $($g.asic_power)W 风扇$($g.fan_speed_percent)%"
            info3="份额: 接受 $($s.shares.accepted) / 拒绝 $($s.shares.rejected) / 硬件错误 $hwErr"
            info4="矿池: $($s.pool.pool)  $($s.pool.latency)ms"
            tip="SRBMiner 挖矿中 $hShow TH/s`n$($g.model -replace '_',' ') $($g.temperature)°C / $($g.asic_power)W / 风扇 $($g.fan_speed_percent)%`n份额 接受$($s.shares.accepted) 拒绝$($s.shares.rejected)`n$($s.pool.pool) $($s.pool.latency)ms"
        }
    } catch {
        # 服务 Running 但 API 无响应 → 启动中/API 预热
        return @{ state='strans' }
    }
}

# 自适应功率档位:读 power_governor 的 state.json。
# 返回 @{ tier; idleSec; gpuPct; paused } 或 $null(未部署/调节器停写=未知)。
function Get-GovMode {
    try {
        if (-not (Test-Path $govStateFile)) { return $null }
        $f = Get-Item $govStateFile -ErrorAction Stop
        # 调节器 daemon 每 10s 写一次;超过 45s 没更新视为已停止(否则会把最后一个档位当现状一直显示)
        if (((Get-Date) - $f.LastWriteTime).TotalSeconds -gt 45) { return $null }
        $j = Get-Content $govStateFile -Raw -Encoding UTF8 | ConvertFrom-Json
        # 容错: 历史上出现过 state.json 被写成数组, 取最后一个含 tier 的对象
        if ($j -is [array]) { $j = $j | Where-Object { $_.tier } | Select-Object -Last 1 }
        if (-not $j -or -not $j.tier) { return $null }
        return @{
            tier    = [string]$j.tier
            idleSec = [int]$j.idleSec
            gpuPct  = [double]$j.gpuPct
            paused  = [bool]$j.pausedByGov
        }
    } catch { return $null }
}

# 档位 -> 菜单首行文案 + 气泡短名 + 悬停细节
function Get-ModeText($m) {
    if (-not $m) { return @{ row = '自适应功率: 未启用/未知'; short = $null; detail = '' } }
    switch ($m.tier) {
        'full'  { return @{ row = '自适应功率: 满速 100%(空闲)';        short = '满速 100%'; detail = "空闲 $($m.idleSec)s" } }
        'eco'   { return @{ row = '自适应功率: 节能 70%(使用中)';       short = '节能 70%';  detail = "空闲 $($m.idleSec)s · 自动降功率" } }
        'pause' { return @{ row = '自适应功率: 暂停(其他程序占用 GPU)'; short = '已暂停';    detail = "其他程序 GPU $([math]::Round($m.gpuPct, 1))% · 让出显卡" } }
        default { return @{ row = "自适应功率: $($m.tier)";             short = [string]$m.tier; detail = '' } }
    }
}

# 暂停剩余时间 HH:MM:SS
function Get-PauseRemain {
    if (-not $script:pauseUntil) { return '' }
    $rem = $script:pauseUntil - (Get-Date)
    if ($rem.TotalSeconds -lt 0) { return '00:00:00' }
    return ('{0:D2}:{1:D2}:{2:D2}' -f $rem.Hours, $rem.Minutes, $rem.Seconds)
}


# 统一清理暂停状态(同时重置防刷屏标记)
function Clear-Pause {
    $script:pauseUntil     = $null
    $script:pauseLabel     = ''
    $script:autoToastShown = $false
    $script:failToastShown = $false
}

# 弹气泡统一封装
function Show-Balloon($msg, $duration=3000) {
    $ico.BalloonTipTitle = 'SRBMiner 矿机'
    $ico.BalloonTipText  = $msg
    $ico.ShowBalloonTip($duration)
}


function Refresh-UI {
    try {
        $st   = Get-State
        $gov  = Get-GovMode
        $mode = Get-ModeText $gov
        $mMode.Text = $mode.row

        # 档位变化只更新图标/菜单, 不弹任何气泡(后台静默);图标在下面的 switch 里按 tier 选取

        switch ($st.state) {
            'mining' {
                $mInfo1.Text = $st.info1
                $mInfo2.Text = $st.info2
                $mInfo3.Text = $st.info3
                $mInfo4.Text = $st.info4
                $mToggle.Text = '停止挖矿'
                if ($gov -and $script:modeIcons.ContainsKey($gov.tier)) { $ico.Icon = $script:modeIcons[$gov.tier] }
                else { $ico.Icon = [System.Drawing.SystemIcons]::Information }
            }
            'stopped' {
                if ($st.paused) {
                    $rem = Get-PauseRemain
                    $mInfo1.Text = "状态: 已暂停 · $($script:pauseLabel) · 剩余 $rem"
                    $mInfo2.Text = ''; $mInfo3.Text = ''; $mInfo4.Text = ''
                    $mToggle.Text = '立即恢复挖矿'
                    $ico.Icon = [System.Drawing.SystemIcons]::Warning
                } elseif ($gov -and $gov.tier -eq 'pause') {
                    # 服务停着但调节器档位是 pause: 是自动让卡, 不是用户手动停的
                    $mInfo1.Text = '状态: 自适应暂停(其他程序占用 GPU, 退出后自动复挖)'
                    $mInfo2.Text = ''; $mInfo3.Text = ''; $mInfo4.Text = ''
                    $mToggle.Text = '启动挖矿'
                    $ico.Icon = $script:modeIcons['pause']
                } else {
                    $mInfo1.Text = '状态: 已停止'
                    $mInfo2.Text = ''; $mInfo3.Text = ''; $mInfo4.Text = ''
                    $mToggle.Text = '启动挖矿'
                    $ico.Icon = [System.Drawing.SystemIcons]::Warning
                }
            }
            'strans' {
                $mInfo1.Text = '状态: 启动中/API 无响应...'
                $mInfo2.Text = ''; $mInfo3.Text = ''; $mInfo4.Text = ''
                $mToggle.Text = '停止挖矿'
                $ico.Icon = [System.Drawing.SystemIcons]::Warning
            }
            default {
                $mInfo1.Text = '状态: 服务未安装'
                $mInfo2.Text = ''; $mInfo3.Text = ''; $mInfo4.Text = ''
                $mToggle.Text = '启动挖矿'
                $ico.Icon = [System.Drawing.SystemIcons]::Error
            }
        }
        $t = $st.tip
        if (-not $t) {
            $t = if ($st.paused) { "已暂停 · $(Get-PauseRemain)" } else { 'SRBMiner 矿机' }
        }
        # 悬停气泡首段带上档位(通知区直接读得到当前模式)
        if ($gov -and $mode.short) {
            $head = "[$($mode.short)] "
            $room = 63 - $head.Length
            if ($t.Length -gt $room) { $t = $t.Substring(0, $room) }
            $t = $head + $t
        }
        if ($t.Length -gt 63) { $t = $t.Substring(0,63) }
        $ico.Text = $t
    } catch {
        $ico.Text = 'SRBMiner(状态刷新异常)'
    }
}


# --- 设定暂停并停止服务 ---
function Set-Pause($hours, $label) {
    # 先记录目标时刻(即便 Stop 失败, 下一次到达时会自动恢复), 再停服务
    $script:pauseUntil = (Get-Date).AddHours($hours)
    $script:pauseLabel = $label
    $script:failToastShown = $false
    try {
        Stop-Service $svcName -Force
        Show-Balloon "已暂停挖矿。$label 后自动恢复。"
    } catch {
        Show-Balloon "暂停失败: $($_.Exception.Message)"
    }
    Refresh-UI
}


# --- 暂停各档 ---
$mPause30.Add_Click({ Set-Pause 0.5 '30 分钟' })
$mPause1h.Add_Click({ Set-Pause 1   '1 小时' })
$mPause2h.Add_Click({ Set-Pause 2   '2 小时' })
$mPause4h.Add_Click({ Set-Pause 4   '4 小时' })
$mPause8h.Add_Click({ Set-Pause 8   '8 小时' })

# 立即停止（不自动恢复）
$mPauseStop.Add_Click({
    Clear-Pause
    try {
        Stop-Service $svcName -Force
        Show-Balloon '已停止挖矿（仅本次，不自动恢复）'
    } catch {
        Show-Balloon "停止失败: $($_.Exception.Message)"
    }
    Refresh-UI
})

# --- 手动启停（Toggle）---
$mToggle.Add_Click({
    try {
        $cur = Get-State
        if ($cur.state -eq 'mining') {
            Clear-Pause
            Stop-Service $svcName -Force
            Show-Balloon '已停止挖矿（仅本次，如需到点自动恢复请用「暂停」）'
        } else {
            Clear-Pause
            Start-Service $svcName
            Show-Balloon '已请求启动挖矿'
        }
    } catch {
        Show-Balloon "操作失败: $($_.Exception.Message)"
    }
    Refresh-UI
})

$mLog.Add_Click({
    try {
        if (Test-Path $logFile) { Start-Process notepad.exe -ArgumentList "`"$logFile`"" }
        else { [System.Windows.Forms.MessageBox]::Show("日志不存在: $logFile", 'SRBMiner 矿机') }
    } catch {
        Show-Balloon "打开日志失败: $($_.Exception.Message)"
    }
})

$mExit.Add_Click({
    $ico.Visible = $false
    $ico.Dispose()
    foreach ($k in @($script:modeIcons.Keys)) { try { $script:modeIcons[$k].Dispose() } catch { } }
    foreach ($k in @($script:modeBitmaps.Keys)) { try { $script:modeBitmaps[$k].Dispose() } catch { } }
    [System.Windows.Forms.Application]::Exit()
})

# 双击托盘图标 = 弹完整状态气泡（只读，不启停，防误触）
$ico.Add_DoubleClick({
    try {
        $st = Get-State
        $ico.BalloonTipTitle = 'SRBMiner 矿机'
        $ico.BalloonTipText  = if ($st.paused) { "已暂停挖矿 · $(Get-PauseRemain) 后自动恢复" } else { $st.tip }
        $ico.ShowBalloonTip(4000)
    } catch { }
})


# --- 自动恢复 + 轮询 ---
$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 1000   # 每秒：到点恢复 + 倒计时显示
$timer.Add_Tick({
    # 到点自动恢复（成功才清清除;失败保留并下个 tick 重试）
    if ($script:pauseUntil -and (Get-Date) -ge $script:pauseUntil) {
        $sv = Get-Service $svcName -ErrorAction SilentlyContinue
        if ($sv -and $sv.Status -eq 'Running') {
            # 服务已被(用户/其它)拉起 → 视为恢复完成, 清暂停标记(防陈旧复活)
            Clear-Pause
        } else {
            try {
                Start-Service $svcName
                Clear-Pause
                if (-not $script:autoToastShown) {
                    Show-Balloon '休息结束，已自动恢复挖矿' 4000
                    $script:autoToastShown = $true
                }
            } catch {
                # 启动失败: 保留 pauseUntil, 下个 tick 继续自动重试; 只提示一次
                if (-not $script:failToastShown) {
                    Show-Balloon "自动恢复失败，正在重试: $($_.Exception.Message)"
                    $script:failToastShown = $true
                }
            }
        }
    }
    Refresh-UI
})
$timer.Start()

Refresh-UI
try { [System.Windows.Forms.Application]::Run() } catch { }