# komari-deer 弹窗「本月流量」卡 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 📈 弹窗(embedded 模式)里的 Network 卡从实时速率卡改为「本月流量」卡——本月累计大字 + 近 24h 累计上升面积图,纯前端,不动 hub。

**Architecture:** 在 `src/components/instance/LoadChart.tsx` 的 Network 卡上用 `embedded` 门控,embedded 时渲染新的「本月流量」卡(AreaChart 画 `net_total_down`/`net_total_up` 两条累计曲线),非 embedded 走原速率卡(字节不变)。headline 大字用实时 `live_data.network.totalUp/Down`(月翻转计数器,本月至今真实用量);曲线数据用现有 `chartData`(24h/15min 采样,已含 net_total 字段)。新增 i18n key `nodeCard.monthlyTraffic`。

**Tech Stack:** Next.js 16.1.0 static export、React 19、recharts(AreaChart)、react-i18next、TypeScript。

**Plan base:** `feature/history-cache-preload` 分支,HEAD `a3c40a1`(spec commit)。

## Global Constraints

- **只改 embedded 弹窗的 Network 卡**;详情页 Network 卡保持原样(**非 embedded 路径字节不变**)。
- 新增 i18n key `nodeCard.monthlyTraffic`:
  - `zh_CN.json` / `zh_TW.json` = `"本月流量"`
  - `en.json` = `"Monthly Traffic"`
- 图表序列与样式(照抄现有 Network 卡的轴/tooltip/动画属性):
  - ↓ 下载累计 `net_total_down`,色 `colors[0]`(红 `#F38181`,= `primaryColor`)
  - ↑ 上传累计 `net_total_up`,色 `colors[3]`(青 `#95E1D3`)
  - `AreaChart`(非 LineChart),`<YAxis domain={["dataMin", "dataMax"]}>`,`tickFormatter=formatBytes`
- headline 大字:↑/↓ 本月累计,`formatBytes`;`live_data` 缺失时显示 `-`。
- embedded 不再显示实时速率 bytes/s 两行。
- 月初跨月「归零台阶」不做特殊处理(不写代码,最多一行注释)。
- `komari-theme.json` version `0.1.3` → **`0.1.4`**;description 保持 `"Deer themed Komari Monitor dashboard (node card popover resource curves)"` 不动。
- 打包 zip = `preview.png` + `komari-theme.json` + `dist/`,命名 `komari-deer-v0.1.4.zip`。
- **铁律**:推线上前 Mac 本地 `rm -rf dist && npm run build` 必须先过。
- 不截图;hub 密码仅会话内用不写盘;不改 ping 任务。
- 部署 hub **必须用户明确确认**后执行(既有流程:备份 → 上传 → 激活 → 字节校验 → 冒烟)。

---

### Task 1: Network 卡 embedded 模式改为「本月流量」卡 + i18n keys

**Files:**
- Modify: `src/components/instance/LoadChart.tsx:508-584`(Network 卡)
- Modify: `src/i18n/locales/zh_CN.json:402`
- Modify: `src/i18n/locales/en.json:372`
- Modify: `src/i18n/locales/zh_TW.json:372`

**Interfaces:**
- Consumes: 现有 `chartData`(已含 `net_total_up`/`net_total_down` 字段)、`live_data?.network.totalUp/totalDown`(LiveData 类型 `src/types/LiveData.tsx:27-28`)、`Area`/`AreaChart`/`ChartContainer`/`ChartTooltip`/`ChartTooltipContent`/`formatBytes`/`colors`/`chartMargin`/`timeFormatter`/`lableFormatter`(LoadChart.tsx 内均已定义,`Area` 已在文件顶部 import)。
- Produces: `embedded` 时渲染「本月流量」卡;非 embedded 渲染原速率卡(字节不变)。新增 i18n key `nodeCard.monthlyTraffic`。

- [ ] **Step 1: 加 3 个 locale 文件的 i18n key**

`src/i18n/locales/zh_CN.json`(把 `"networkSpeed"` 后插入 `monthlyTraffic`):

old_string:
```json
    "networkSpeed": "网络",
    "networkTraffic": "网络/流量",
```
new_string:
```json
    "networkSpeed": "网络",
    "monthlyTraffic": "本月流量",
    "networkTraffic": "网络/流量",
```

`src/i18n/locales/en.json`:

old_string:
```json
    "networkSpeed": "Net Spd",
    "networkTraffic": "Network/Traffic",
```
new_string:
```json
    "networkSpeed": "Net Spd",
    "monthlyTraffic": "Monthly Traffic",
    "networkTraffic": "Network/Traffic",
```

`src/i18n/locales/zh_TW.json`:

old_string:
```json
    "networkSpeed": "網路",
    "networkTraffic": "網路/流量",
```
new_string:
```json
    "networkSpeed": "網路",
    "monthlyTraffic": "本月流量",
    "networkTraffic": "網路/流量",
```

- [ ] **Step 2: 验证 3 个 key 就位**

Run: `grep -n "monthlyTraffic" src/i18n/locales/{zh_CN,en,zh_TW}.json`
Expected: 3 行命中,值分别为 `本月流量` / `Monthly Traffic` / `本月流量`;JSON 仍合法(`node -e "JSON.parse(require('fs').readFileSync('src/i18n/locales/zh_CN.json'))"` 不报错)。

- [ ] **Step 3: 改 LoadChart.tsx Network 卡**

把当前 Network 卡(`{/* Network */}` 到该卡 `</Card>`,即 508-584 行)的 `CardContent` 内容整体包进 `{embedded ? (...) : (...)}`,**原速率卡 JSX 原封不动放进 `: (...)` 分支**(保证非 embedded 字节不变)。`embedded` 分支 = 本月流量卡,完整代码如下:

```tsx
        {/* Network */}
        <Card className={cn}>
          <CardContent className="p-4">
          {embedded ? (
            <>
            {ChartTitle(
              t("nodeCard.monthlyTraffic"),
              <div className="flex flex-col items-end gap-0 text-sm">
                <span>
                  ↑ {live_data ? formatBytes(live_data.network.totalUp) : "-"}
                </span>
                <span>
                  ↓ {live_data ? formatBytes(live_data.network.totalDown) : "-"}
                </span>
              </div>
            )}
            <ChartContainer
              config={{
                net_total_down: {
                  label: t("chart.network_down"),
                  color: primaryColor,
                },
                net_total_up: {
                  label: t("chart.network_up"),
                  color: colors[3],
                },
              }}
            >
              <AreaChart data={chartData} accessibilityLayer margin={chartMargin}>
                <CartesianGrid vertical={false} />
                <XAxis
                  dataKey="time"
                  tickLine={false}
                  tickFormatter={timeFormatter}
                  interval={0}
                />
                <YAxis
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={(value) =>
                    `${formatBytes(value)}`
                  }
                  orientation="left"
                  type="number"
                  tick={{ dx: -10 }}
                  mirror={true}
                  domain={["dataMin", "dataMax"]}
                />
                <ChartTooltip
                  cursor={false}
                  formatter={formatBytes}
                  content={
                    <ChartTooltipContent
                      labelFormatter={lableFormatter}
                      indicator="dot"
                    />
                  }
                />
                <Area
                  dataKey="net_total_down"
                  animationDuration={0}
                  stroke={primaryColor}
                  fill={primaryColor}
                  opacity={0.8}
                  dot={false}
                />
                <Area
                  dataKey="net_total_up"
                  animationDuration={0}
                  stroke={colors[3]}
                  fill={colors[3]}
                  opacity={0.8}
                  dot={false}
                />
              </AreaChart>
            </ChartContainer>
            </>
          ) : (
            <>
            {ChartTitle(
              t("nodeCard.networkSpeed"),
              <div className="flex flex-col items-end gap-0 text-sm">
                <span>
                  ↑ {formatBytes(live_data?.network.up || 0)}
                  /s
                </span>
                <span>
                  ↓ {formatBytes(live_data?.network.down || 0)}
                  /s
                </span>
              </div>
            )}
            <ChartContainer
              config={{
                net_in: {
                  label: t("chart.network_down"),
                  color: primaryColor,
                },
                net_out: {
                  label: t("chart.network_up"),
                  color: colors[3],
                },
              }}
            >
              <LineChart data={chartData} accessibilityLayer margin={chartMargin}>
                <CartesianGrid vertical={false} />
                <XAxis
                  dataKey="time"
                  tickLine={false}
                  tickFormatter={timeFormatter}
                  interval={0}
                />
                <YAxis
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={(value) =>
                    `${formatBytes(value)}`
                  }
                  orientation="left"
                  type="number"
                  tick={{ dx: -10 }}
                  mirror={true}
                />
                <ChartTooltip
                  cursor={false}
                  formatter={formatBytes}
                  content={
                    <ChartTooltipContent
                      labelFormatter={lableFormatter}
                      indicator="dot"
                    />
                  }
                />
                <Line
                  dataKey="net_in"
                  animationDuration={0}
                  stroke={primaryColor}
                  fill={primaryColor}
                  opacity={0.8}
                  dot={false}
                />
                <Line
                  dataKey="net_out"
                  animationDuration={0}
                  stroke={colors[3]}
                  fill={colors[3]}
                  opacity={0.8}
                  dot={false}
                />
              </LineChart>
            </ChartContainer>
            </>
          )}
          </CardContent>
        </Card>
```

注意:`Area`、`Line`、`AreaChart`、`LineChart` 都已在文件顶部 import(508 行原代码已用 Line/LineChart;CPU/Ram/Disk 卡已用 Area/AreaChart),无需新增 import。

- [ ] **Step 4: 验证原速率卡内容未丢失(非 embedded 路径字节不变)**

Run:
```bash
cd /Users/mango/project/komari-deer
git show HEAD:src/components/instance/LoadChart.tsx | sed -n '508,584p' > /tmp/net-old.txt
awk '{gsub(/^ +| +$/,""); if ($0!="") print}' /tmp/net-old.txt | while read -r line; do grep -Fq "$line" src/components/instance/LoadChart.tsx || echo "MISSING: $line"; done
```
Expected: 无 `MISSING:` 输出(原速率卡每行都还在新文件里)。再肉眼核对 `: (...)` 分支里的 `t("nodeCard.networkSpeed")`、`network.up/down`、`net_in`/`net_out`、`LineChart`、两条 `Line` 与改动前一致。

- [ ] **Step 5: Mac 本地 build(铁律)**

Run: `cd /Users/mango/project/komari-deer && rm -rf dist && npm run build`
Expected: build 成功,无 TS/JSX 错误(尤其核对 `domain={["dataMin", "dataMax"]}` 合法、三元 JSX 括号闭合)。

- [ ] **Step 6: Commit**

```bash
git add src/components/instance/LoadChart.tsx src/i18n/locales/zh_CN.json src/i18n/locales/en.json src/i18n/locales/zh_TW.json
git commit -m "feat: popover Network card -> monthly traffic card (embedded)

Embedded mode shows month-to-date totals from live network.totalUp/Down
plus a 24h cumulative area chart of net_total_*/down. Detail-page Network
speed card unchanged (non-embedded path byte-identical). New i18n key
nodeCard.monthlyTraffic."
```

---

### Task 2: 版本 0.1.4 + 打包 zip + 本地冒烟

**Files:**
- Modify: `komari-theme.json:5`(version)
- Produce: `komari-deer-v0.1.4.zip`(preview.png + komari-theme.json + dist/)

**Interfaces:**
- Consumes: Task 1 的 `dist/`(已含本月流量卡)、`komari-theme.json`。
- Produces: `komari-deer-v0.1.4.zip`(sha256 记录),供 Task 3 部署。

- [ ] **Step 1: 版本号 0.1.3 → 0.1.4**

`komari-theme.json` line 5:

old_string: `  "version": "0.1.3",`
new_string: `  "version": "0.1.4",`

(description 不动;确认 `node -e "console.log(require('./komari-theme.json').version)"` 输出 `0.1.4`)

- [ ] **Step 2: 打包 zip**

Run:
```bash
cd /Users/mango/project/komari-deer
rm -f komari-deer-v0.1.4.zip
zip -r komari-deer-v0.1.4.zip preview.png komari-theme.json dist/
shasum -a 256 komari-deer-v0.1.4.zip
```
Expected: zip 创建成功;顶层含 `preview.png`/`komari-theme.json`/`dist/`(`unzip -l komari-deer-v0.1.4.zip | head` 核对);记录 sha256。

- [ ] **Step 3: 起本地代理 + 浏览器冒烟(不截图,文本快照)**

先起代理(后台):
```bash
DIST=/Users/mango/project/komari-deer/dist PORT=8811 node /tmp/km-proxy/server.mjs
```
用 Playwright 打开 `http://localhost:8811`:
- 首页 5/5 节点在线;点某节点 📈 弹窗 → Network 卡标题为「本月流量」,右上大字显示 ↑/↓ 本月累计(有真实数字,非 "-");
- 该卡图表为**累计上升面积图**(两条面积线,非速率折线);
- 其余 3 卡(CPU/Ram/Disk)正常;无 "No ping data";console 0 错误;
- 回归:详情页 `/instance/{uuid}` Network 卡仍是「Net Spd」实时速率卡(非本月流量),SegmentedControl 正常。

- [ ] **Step 4: Commit**

```bash
git add komari-theme.json
git commit -m "chore: bump theme version to 0.1.4"
```

---

### Task 3: 部署 v0.1.4 到 hub(**必须先经用户明确确认**)

> ⛔ 本任务**不得自动执行**。仅当用户明确说「确认/部署」后才按步骤执行。步骤同 v0.1.3 部署(见 `.superpowers/sdd/2026-08-11-komari-deer-history-cache-preload/progress.md`「v0.1.3 重部署」)。

**Files:** 无代码改动。操作 google-vps(hub)。

- [ ] **Step 1: 备份 hub 现有主题**

`/home/mango/komari/data/theme/komari-deer/` → `/home/mango/komari/data/theme-backup/komari-deer-v0.1.3.tar.gz`

- [ ] **Step 2: 上传激活**

`POST /api/login`(admin 用户名 `mango`,密码会话内取,不写盘)→ `PUT /api/admin/theme/upload`(multipart `file` = `komari-deer-v0.1.4.zip`)→ status=success version=0.1.4 → `GET /api/admin/theme/set?theme=komari-deer`。

- [ ] **Step 3: 字节校验**

hub 落盘 dist 与本地 sha256 全量比对(LC_ALL=C 排序消 locale 差异);theme/list 显示 v0.1.4。

- [ ] **Step 4: 线上冒烟(本地代理 8811)**

同 Task 2 Step 3 断言(针对 hub 托管的 v0.1.4)。确认后更新 Obsidian(演进规划 §0 + AI-DOC-ROUTER Komari 行)+ memory `komari-deer-theme-fork.md` → v0.1.4。
