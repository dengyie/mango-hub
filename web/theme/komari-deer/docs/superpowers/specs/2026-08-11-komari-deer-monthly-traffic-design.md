# komari-deer 弹窗 Net Spd 卡改为「本月流量」卡 — 设计文档

- 日期:2026-08-11
- 状态:approved(用户 2026-08-11 确认设计)
- 分支:`feature/history-cache-preload`(HEAD `ce67de2`)
- 主题仓:`/Users/mango/project/komari-deer`(dengyie/komari-deer fork,Next.js static export)

## 1. 背景与目标

用户在 📈 弹窗里看到 Network 卡当前画的是**实时速率**(↑/↓ bytes/s,net_in/net_out 速率曲线),要求把它**改为「这个月来累积的流量图」**。

经探查确认的关键数据约束:

- hub(`vps.mangoqwq.com`)**全部 25 个指标 `retention_days=1`**,metrics.db 实测任何系列最远仅 ~24h 历史(1min 分辨率 10.1h、5min 分辨率 23.9h)。**整月逐日累计曲线不可能从历史数据构造。**
- 但**本月累计总量可用**:agent 侧 `AGENT_MONTH_ROTATE=1`,`net.total.up/down` 计数器每月归零 → 实时 `network.totalUp/Down` 当前值即本月至今真实用量(即节点卡「Traffic ↑ 336.9 MB / ↓ 253.8 MB」同源)。
- 近 24h 累计曲线可用:`/api/records/load?hours=24` 返回的每条 record 含 `net_total_up`/`net_total_down`(累计计数器)。

**用户已拍板(2026-08-11)**:采用「近 24h 累计图 + 本月总数」纯前端方案;图表呈现为**累计面积图、去掉实时速率**。零后端改动。

## 2. 范围

- **只改 📈 弹窗(embedded 模式)里的 Network 卡**。
- 详情页的 Network 卡(实时/4h/1d 速率 + SegmentedControl)保持原样。
- 不动 hub、不动缓存/预加载逻辑(`historyCache.ts`/`useHistory.ts` 不改)、不动其它 3 张卡(CPU/Ram/Disk)。

## 3. 设计

### 3.1 卡片内容

| 部位 | 内容 |
|---|---|
| 标题 | 本月流量(新增 i18n key `nodeCard.monthlyTraffic`) |
| 右上大字 | 本月累计 ↑ X / ↓ Y,`formatBytes`;数据缺失显示 "-" |
| 图表 | 近 24h 累计上升面积图(AreaChart,复用现有 chartData 与样式) |

### 3.2 图表序列

- ↓ 下载累计 = `net_total_down`(红 `#F38181` = `colors[0]`,沿用现有下载色)
- ↑ 上传累计 = `net_total_up`(青 `#95E1D3` = `colors[3]`,沿用现有上传色)
- Y 轴 `formatBytes`,**domain `['dataMin', 'dataMax']`**(曲线充满图表,区别于速率卡)
- Tooltip `formatter=formatBytes`,复用现有 `ChartTooltipContent`

### 3.3 数据源(全部现成)

- `chartData` 已是 24h 视图、15min 采样(`fillMissingTimePoints`),每条含 `net_total_up`/`net_total_down`(records/load 返回,`fillMissingTimePoints` 对非 time/updated_at 字段原样保留)。
- headline 大字 = `live_data.network.totalUp`/`totalDown`(LiveData 实时数据,随弹窗 30s 后台刷新 + 打开即 force 同步)。
- 不再渲染实时速率 ↑↓ bytes/s 两行。

## 4. 边界与降级

- `live_data` 未到(加载中)→ 大字显示 "-"(沿用现有卡的占位行为)。
- 无历史数据 → 图表为空(现有 chartData 行为,不新增)。
- 月初计数器归零 + 恰好跨月打开 → 24h 窗口内曲线出现一个「归零台阶」再爬升,语义正确,**不做特殊处理**(注释说明即可)。

## 5. 改动文件

### 5.1 `src/components/instance/LoadChart.tsx` — Network 卡(当前 508-584 行)

- 用 `embedded` 门控:embedded → 本月流量卡;非 embedded → 原速率卡(字节不变)。
- 本月流量卡实现要点:
  - 标题 `t("nodeCard.monthlyTraffic")`
  - 右上大字:`↑ {formatBytes(live_data?.network.totalUp || 0)}` / `↓ {formatBytes(live_data?.network.totalDown || 0)}`(缺失时 "-",沿用现有 `live_data?.x ? ... : "-"` 写法)
  - AreaChart `data={chartData}`,config keys `net_total_down`(label 沿用 `t("chart.network_down")`)与 `net_total_up`(label 沿用 `t("chart.network_up")`)
  - `<YAxis domain={['dataMin', 'dataMax']} tickFormatter={formatBytes} ...>`(其余轴属性照抄现有 Network 卡)
  - Tooltip `formatter={formatBytes}`、`ChartTooltipContent`(照抄)
  - 两条 `<Area dataKey="net_total_down" ...>` / `<Area dataKey="net_total_up" ...>`(`animationDuration={0}`、`dot={false}`、`opacity={0.8}`、stroke/fill 用对应色)

### 5.2 `src/i18n/locales/{zh_CN,en,zh_TW}.json` — 各加一个 key

| 文件 | key | 值 |
|---|---|---|
| `zh_CN.json` | `nodeCard.monthlyTraffic` | `"本月流量"` |
| `en.json` | `nodeCard.monthlyTraffic` | `"Monthly Traffic"` |
| `zh_TW.json` | `nodeCard.monthlyTraffic` | `"本月流量"` |

## 6. 版本与打包

- `komari-theme.json` version `0.1.3` → **`0.1.4`**(description 保持用户向文案)。
- 打包 `komari-deer-v0.1.4.zip` = `preview.png` + `komari-theme.json` + `dist/`。

## 7. 验证(不截图)

1. Mac 本地 `rm -rf dist && npm run build` **必须先过**(铁律,推线上前)。
2. 本地代理 8811(起 `/tmp/km-proxy/server.mjs`)冒烟:
   - 📈 弹窗 Network 卡 = 标题「本月流量」+ 本月累计大字(有真实数字)+ 24h 累计上升面积图(两条面积线);
   - 其余 3 卡(CPU/Ram/Disk)正常;无 "No ping data";console 0 错;
   - 详情页 Network 卡仍是实时速率(回归)。
3. 用户确认后按既有流程部署 hub(备份 → 上传 → 激活 → 字节校验 → 冒烟)。

## 8. 不做(明确排除)

- 不做「整月逐日累计曲线」(需 hub 提 `retention_days`,超出纯主题范围,用户已拍板不选)。
- 不改详情页 Network 卡。
- 不改缓存/预加载/数据层。
- 不做「保留实时速率小字」(用户选了去掉)。
