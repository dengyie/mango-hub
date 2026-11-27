# Windows 节点 · 自适应功率调节器 + 托盘

两台 Windows 矿机（home-win / laptop-win）共用一套脚本，唯一差异是**机器名 → 档位杠杆**
和托盘的 `$apiUrl` 端口。脚本源码入库于此，部署目标目录为 `D:\program\srbminer\`。

| 机器 | hostname | GPU | 满速杠杆 | 70% 杠杆 | 驱动墙 |
|---|---|---|---|---|---|
| home-win | `DESKTOP-VST7EIE` | RTX 3070 桌面 | `nvidia-smi -pl 150` | `-pl 105` | 240W |
| laptop-win | `DESKTOP-QAU15K5` | RTX 3070 Laptop | `nvidia-smi -lgc 1260,1260` | `-lgc 880,880` | `-rgc`（OEM 锁死 `-pl`） |

托盘 `$apiUrl`：home `http://127.0.0.1:12001/api/v2/status`，laptop `...12002...`（仅此一行不同）。

## 三档策略（`power_governor.ps1`）

| 档 | 判据 | 动作 |
|---|---|---|
| `full` 100% | 连续空闲 ≥120s（`GetLastInputInfo`；`LogonUI.exe` 存在=锁屏直接算空闲） | 满速杠杆 |
| `eco` 70% | 有输入，且用户侧 GPU 争用 < 20% | 降功率/降频 |
| `pause` 暂停 | 非矿工进程 GPU 占用 ≥20%，连续 2 次采样（防抖） | 停 `srbminer` 服务 + **驱动墙还原 240W / `-rgc`**，把显卡整个还给游戏/渲染 |

- 进 `eco` 立即生效；回 `full` 需连续空闲 120s；进 `pause` 需连续 2 次采样。
- **只恢复自己停的矿**：`state.json` 的 `pausedByGov` 记录是否由调节器停的服务。
  用户手动停的矿不会被拉起（`pausedByGov=false` 时保持不动），用户手动起的矿会清标记并只做降功率。
- 矿工没在跑时把驱动墙还原（home 240W / laptop `-rgc`），修掉历史「150W 残留」老坑。
- **铁律：驱动墙默认保持 240W**，调节器只在运行期临时改 `-pl/-lgc`，不写死驱动墙配置。

## 文件清单

| 文件 | 部署路径 | 编码 | 说明 |
|---|---|---|---|
| `power_governor.ps1` | `D:\program\srbminer\power_governor.ps1` | **UTF-8 BOM, LF** | 调节器主体（含中文日志，PS 5.1 必须 BOM） |
| `power_governor_hidden.vbs` | `D:\program\srbminer\power_governor_hidden.vbs` | **CRLF, ASCII** | 计划任务入口，隐藏窗口启动 daemon |
| `power_guard_hidden.vbs` | `D:\program\srbminer\power_guard_hidden.vbs` | **CRLF, ASCII** | 每 10 分钟 `-Enforce` 兜底 |
| `tray_control.ps1` | `D:\program\srbminer\tray_control.ps1` | **UTF-8 BOM, LF** | 托盘菜单：状态/启停/暂停/自适应档位 |
| `tray_start.vbs` | `D:\program\srbminer\tray_start.vbs` | **CRLF, ASCII** | 托盘入口，隐藏窗口 |
| `tasks/*.xml` | — | UTF-8 | 计划任务参考定义（`__USER_SID__` / `__HOSTNAME__` 占位） |
| `register-tasks.ps1` | 临时目录执行 | UTF-8 BOM | 一键落地脚本 + 注册/刷新三个计划任务 |

> 编码是硬约束：`.ps1` 用 UTF-8 **BOM** + LF；`.vbs` 用 **CRLF + 纯 ASCII**。
> 本目录 `.gitattributes` 已设 `*.ps1/*.vbs/*.xml -text`，禁止 Git 改写行尾/BOM。

## 计划任务

| 任务 | 触发 | 身份 | 动作 |
|---|---|---|---|
| `SRBMinerPowerGovernor` | AtLogOn | 交互用户 + **HighestAvailable** | `wscript.exe //B power_governor_hidden.vbs` |
| `SRBMinerPowerGuard` | 每 10 分钟 | SYSTEM | `wscript.exe //B power_guard_hidden.vbs`（`-Enforce`） |
| `SRBMinerTrayStart` | AtLogOn | 交互用户 | `wscript.exe /b tray_start.vbs` |

`-Enforce` 语义：无交互会话 → 强制 `full` 并还原驱动墙；有会话但 daemon 不在 → `schtasks /run SRBMinerPowerGovernor`。
**daemon 只允许由交互式计划任务启动**，不要手工常驻（会绕过会话判定）。

## 部署 / 升级步骤

```powershell
# 1) 落脚本（编码见上表；可用 scp 传文件再 Copy-Item，避免 SSH 内联 PowerShell 被转义破坏）
# 2) 校验语法
[void][System.Management.Automation.Language.Parser]::ParseFile(
  'D:\program\srbminer\power_governor.ps1', [ref]$null, [ref]$errs)
# 3) 注册/刷新任务
.\register-tasks.ps1
```

托盘升级后需重启托盘进程（先备份原文件）：

```powershell
Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" |
  Where-Object { $_.CommandLine -like '*tray_control.ps1*' } |
  ForEach-Object { Stop-Process -Id $_.ProcessId -Force }
schtasks /run /tn SRBMinerTrayStart
```

## CLI 与运维

```powershell
power_governor.ps1 -Once -DryRun              # 只评估，不改动
power_governor.ps1 -ForceTier eco -Once       # 强制某档一次（测试用，不持互斥锁）
power_governor.ps1 -ForceTier pause -Once     # 立刻暂停并让出显卡
power_governor.ps1 -Enforce                   # guard 用：无会话强制 full / 有会话确保 daemon
New-Item D:\program\srbminer\governor.off     # 手动熔断：恢复满血并退出
```

- 状态：`D:\program\srbminer\governor\state.json`（camelCase：`tier/since/idleSec/gpuPct/gpuPid/pausedByGov`）
- 日志：`governor\governor.log`（1MB 轮转 `.1`）
- 单实例互斥：`Global\SrbMinerPowerGovernor`

## 回滚

```powershell
schtasks /end /tn SRBMinerPowerGuard;   schtasks /delete /tn SRBMinerPowerGuard /f
schtasks /end /tn SRBMinerPowerGovernor; schtasks /delete /tn SRBMinerPowerGovernor /f
schtasks /end /tn SRBMinerTrayStart;    schtasks /delete /tn SRBMinerTrayStart /f
# 删脚本 + 恢复驱动墙：home `nvidia-smi -pl 240`；laptop `nvidia-smi -rgc`
```

## 关键约束（改动前必读）

1. **驱动墙保持 240W**：用户自己的 GPU 项目要满血，调节器不得写死更低墙。
2. **矿工 API 端口 12000–12101 保持入站阻断**，绝不给矿工二进制加放行规则。
3. **不要自动拉起用户手动停掉的矿**：靠 `pausedByGov` 区分。
4. 改托盘须**两机同步**（仅 `$apiUrl` 不同）；改完验证 `pearl`/`TH/s` 等文案只出现一次。
5. `nvidia-smi -pl` 需要提权 → 计划任务必须 `HighestAvailable` + VBS 壳（静默无 UAC）。

## 上游联动（面板徽标）

调节器写 `state.json` → agent 采集 → `report.Governor`（**顶层**，与 `report.Mining` 解耦）
→ hub `v2.Report.Governor` → 瘦身热路径 `miningLiveStatus.Governor` → 主题 `governorBadge`。
`pause` 档矿工停时 `Mining` 为空但 `Governor` 仍在，面板显示「暂停(让卡)」而**不造假 0 H/s 的 pill**。
`idle_sec` / `gpu_pct` 仅管理员可见（未认证调用被粗化）。
