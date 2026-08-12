# Komari Deer 主题：历史曲线缓存 + 预加载 开发设计文档

- 日期：2026-08-11
- 状态：待实现（本档为 v0.1.2 实现依据）
- 目标版本：`komari-deer` v0.1.2
- 承接：v0.1.1（📈 弹窗资源曲线，分支 `feature/popover-resource-curves`）
- 工作分支：`feature/history-cache-preload`（自当前 HEAD 分出）

---

## 1. 背景与问题

`komari-deer`（dengyie/komari-deer）是独立 Next.js 主题仓（`next.config.ts`: `distDir:'dist'`, `output:'export'`；Next 16.1.0, React 19；无数据获取库，用普通 fetch + 自定义 context）。

### 1.1 数据流现状

| 数据 | 来源 | 频率 | 承载 |
|---|---|---|---|
| 实时状态 | RPC2 `common:getNodesLatestStatus` | 每 2s 轮询 | `LiveDataContext`（全节点在 context，覆盖 cpu/ram/swap/disk/network/connections/gpu/uptime/process） |
| 节点列表 | RPC2 `common:getNodes` | 每 5s refresh | `NodeListContext`（`NodeBasicInfo[]`，每项含 `uuid`） |
| 历史曲线 | `GET /api/records/load?uuid=X&hours=N` | **每次挂载/切视图** | `LoadChart` 自 fetch → `remoteData` state |

### 1.2 问题

`LoadChartFloat`（📈 弹窗）每次 open 都 mount/unmount `LoadChart`，`LoadChart` 的 fetch effect（`src/components/instance/LoadChart.tsx:106-165`）在每次挂载时执行 `GET /api/records/load?uuid=X&hours=24`：

- 每次打开弹窗都重新拉 24h 历史（~282 点 / ~122KB / 节点，hub 内网快但仍是重复请求）；
- 打开先闪 "Loading..."（`<Loading/>`）再渲染；
- 详情页 `LoadChart`（非 embedded）切视图（4h↔1d）每次重拉，进入详情页也重拉；
- 同一节点弹窗 + 详情页同时存在时无去重（各自拉一次）。

无缓存、无预加载、无并发去重。

## 2. 目标 / 非目标

### 目标
- 弹窗打开**零 Loading 闪屏**：缓存命中（含过期）立即渲染。
- **预加载**：dashboard 就绪后批量拉全节点 24h，首开即秒开。
- **全局共享**：弹窗 24h 与详情页 4h/1d 共用一个缓存，切视图/重进详情页展示缓存、不闪 Loading。
- **并发去重**：同一 `(uuid, hours)` 在弹窗与详情页同时加载时只发一次请求。
- **后台温热 + 打开即刷**：后台每 30s 刷新一次历史缓存；每次打开图表/切视图触发一次刷新；**刷新期间始终展示缓存**。

### 非目标
- 不改后端 / hub 镜像（纯前端主题改动）。
- 不做 localStorage / 跨整页刷新持久化（模块级内存即可；整页刷新丢缓存属预期）。
- 不引入 react-query/SWR/axios 等新依赖。
- 不预加载 7d/30d（`record_preserve_time=24` 时这两个视图根本不出现）。
- 不做 LRU 淘汰（实际键极少，见 §7）。

## 3. 已拍板决策

| 项 | 决策 |
|---|---|
| 缓存载体 | **模块级内存 store + `useSyncExternalStore`**（方案 A）：零 Provider、精确订阅、一套机制覆盖弹窗+详情页 |
| 预加载时机 | **dashboard 节点列表就绪后**批量预加载全节点 24h（`preloadAll` 只填缺失项，错峰串行） |
| 新鲜度 | **后台 30s 循环刷新 + 打开图表强制触发一次刷新 + 始终展示缓存**（2026-08-11 用户决策）：`SWR_TTL = 30s`；打开图表立即显示缓存，同时 `force: true` 后台重拉；`Providers` 常驻根每 30s 兜底刷新已有条目 |
| 缓存范围 | **全局共享**：键 `uuid:hours`，弹窗 24h 与详情页 4h/1d 同一缓存 |

## 4. 架构总览

```
                        ┌─────────────────────────────────────────────┐
                        │  src/lib/historyCache.ts（纯 TS，无 React）   │
                        │   Map<"uuid:hours", CacheEntry>              │
                        │   subscribe/getVersion/getEntry              │
                        │   loadHistory(uuid,hours)  ← SWR + 去重       │
                        │   preloadAll(uuids, hours=24) ← 填缺失项      │
                        └──────────────┬──────────────────────────────┘
                                       │ emit()（版本号 +1，通知订阅者）
              useSyncExternalStore      │
   ┌───────────────┐   ┌───────────────┴──────────────────┐
   │ useHistory.ts │ ← │ LoadChart.tsx                    │
   │（React hook） │   │  弹窗(embedded 24h) + 详情页(4h/1d) │
   └───────────────┘   └──────────────────────────────────┘
                                       ▲
                        ┌──────────────┴──────────────┐
                        │ DashboardContent.tsx         │
                        │ nodeList 变化 → preloadAll   │
                        └─────────────────────────────┘
```

- 缓存层是纯 TS（可单测），React 只通过 `useHistory` 消费。
- 模块级 store 跨客户端路由存活（`/` 与 `/instance/<uuid>` 切换不丢缓存）；整页刷新才丢。
- 无需包 Provider 树。

## 5. 文件与接口

### 5.1 新建 `src/lib/historyCache.ts`

```ts
import type { RecordFormat } from "@/utils/RecordHelper";

export const SWR_TTL = 30 * 1000; // 30 秒；后台循环与「打开即刷」的过期阈值（2026-08-11 用户决策）

// /api/records/load 响应 data 的忠实缓存形状（含 gpu_devices，供详情页 GPU 合并）
export type RecordsData = {
  records: RecordFormat[];
  gpu_devices: Record<string, GpuDeviceRecords>; // 与现有 LoadChart 用法一致，宽松定义即可
};

export type CacheEntry = {
  uuid: string;               // 供后台刷新器遍历重建 key
  hours: number;
  data: RecordsData | null;   // null = 拉取失败或从未成功
  fetchedAt: number | null;   // 成功拉取时间戳
  error: string | null;
  promise: Promise<RecordsData> | null; // in-flight 去重；错误条目不保留 promise 以允许重试
};
```

模块级状态与订阅原语：

```ts
const cache = new Map<string, CacheEntry>();
const listeners = new Set<() => void>();
let version = 0;

function key(uuid: string, hours: number) { return `${uuid}:${hours}`; }
function emit() { version++; for (const l of listeners) l(); }

export function subscribe(l: () => void): () => void; // 返回取消订阅
export function getVersion(): number;                 // useSyncExternalStore 快照（整数，随 emit 递增）
export function getEntry(uuid: string, hours: number): CacheEntry | undefined;
export function isFresh(entry: CacheEntry | undefined, now: number): boolean;
//   entry?.data && entry.fetchedAt !== null && now - entry.fetchedAt < SWR_TTL
```

核心加载（SWR + 并发去重）：

```ts
export async function loadHistory(
  uuid: string,
  hours?: number,
  opts: { force?: boolean } = {}
): Promise<void> {
  if (!uuid || !hours) return;
  const k = key(uuid, hours);
  const now = Date.now();
  const existing = cache.get(k);

  if (existing?.promise) return existing.promise;      // in-flight 去重（始终生效）
  if (!opts.force && isFresh(existing, now)) return;   // 非 force 且 30s 内 → 零请求

  const p = fetch(`/api/records/load?uuid=${encodeURIComponent(uuid)}&hours=${hours}`)
    .then((res) => {
      if (!res.ok) throw new Error(res.statusText);
      return res.json();
    })
    .then((resp) => {
      const data: RecordsData = {
        records: resp.data?.records ?? [],
        gpu_devices: resp.data?.gpu_devices ?? {},
      };
      cache.set(k, { uuid, hours, data, fetchedAt: Date.now(), error: null, promise: null });
      emit();
      return data;
    })
    .catch((err) => {
      // 保留旧数据（若曾成功），错误条目不设 promise → 下次打开自动重试
      cache.set(k, {
        uuid,
        hours,
        data: existing?.data ?? null,
        fetchedAt: existing?.fetchedAt ?? null,
        error: err?.message || "Error",
        promise: null,
      });
      emit();
      return existing?.data ?? [];
    });

  cache.set(k, {
    uuid,
    hours,
    data: existing?.data ?? null,
    fetchedAt: existing?.fetchedAt ?? null,
    error: existing?.error ?? null,
    promise: p,
  });
  emit(); // 通知订阅者「已发起拉取」
  return p;
}
```

> `loadHistory` 内部 catch，永不 reject（`preloadAll` / 刷新器无需再包 try/catch）。
> 先 `cache.set(...promise)` 再 `emit()`，保证订阅者首次看到的就是「含 in-flight」的条目。
> `force` 语义：**打开图表/切视图时传 `force: true`（展示缓存 + 无条件后台重拉）**；后台循环与 `preloadAll` 用默认（SWR 过期判断）。

预加载（只填缺失项，串行天然错峰）：

```ts
export async function preloadAll(uuids: string[], hours = 24): Promise<void> {
  for (const uuid of uuids) {
    if (cache.has(key(uuid, hours))) continue; // 已尝试（成功或失败）→ 不重复
    await loadHistory(uuid, hours);            // 串行 await = 错峰，不洪峰打 hub
  }
}
```

> 只填缺失：新节点上线 → nodeList 变化 → 再次 `preloadAll` → 新 uuid 缺失 → 被预加载。常驻节点每次迭代只做 `cache.has` 命中返回，成本可忽略。
> 已有条目的新鲜度维护由后台刷新器（§5.1.1）与「打开即刷（force）」负责。

### 5.1.1 后台刷新器（30s 循环）

```ts
let refresherTimer: ReturnType<typeof setInterval> | null = null;
let refreshing = false;

export function startHistoryRefresher(intervalMs = 30_000): void {
  if (refresherTimer) return;                 // 幂等，防重复启动
  refresherTimer = setInterval(() => {
    if (refreshing) return;                   // 上一轮未完成则跳过（防重叠）
    refreshing = true;
    (async () => {
      try {
        for (const entry of cache.values()) {
          await loadHistory(entry.uuid, entry.hours); // SWR：fresh 跳过 / 过期重拉
        }
      } finally {
        refreshing = false;
      }
    })();
  }, intervalMs);
}

export function stopHistoryRefresher(): void {
  if (refresherTimer) {
    clearInterval(refresherTimer);
    refresherTimer = null;
  }
}
```

> 只刷新**已有条目**（填充缺失是 `preloadAll` 职责）。每条目约每 30s 拉一次；`loadHistory` 内部 fresh 判断 + in-flight 去重保证不与「打开即刷」重复发请求。
> `for...of cache.values()` 迭代中 `cache.set` 同键仅原地更新、不新增键，迭代安全。

开发辅助：

```ts
export function clearHistoryCache(): void; // cache.clear() + emit()（仅 dev/测试）
```

### 5.2 新建 `src/lib/useHistory.ts`

```ts
"use client";
import { useEffect, useSyncExternalStore } from "react";
import { subscribe, getVersion, getEntry, loadHistory, SWR_TTL } from "./historyCache";
import type { RecordsData } from "./historyCache";

export type HistoryStatus = "idle" | "loading" | "ready" | "stale" | "error";

export function useHistory(uuid: string, hours?: number): {
  data: RecordsData | null;
  status: HistoryStatus;
  error: string | null;
} {
  useSyncExternalStore(subscribe, getVersion); // 缓存变化 → 重渲染（预加载/TTL 重校验完成后自动更新）

  useEffect(() => {
    void loadHistory(uuid, hours, { force: true }); // 打开图表/切视图 → 展示缓存 + 无条件后台重拉
  }, [uuid, hours]);

  if (!hours) return { data: null, status: "idle", error: null }; // 实时视图：不拉历史

  const entry = getEntry(uuid, hours);
  const hasData = entry?.data != null;
  const hasError = !!entry?.error;
  const inFlight = !!entry?.promise;
  const stale =
    hasData && entry.fetchedAt !== null && Date.now() - entry.fetchedAt >= SWR_TTL;

  let status: HistoryStatus = "loading";
  if (hasData) status = stale ? "stale" : "ready";
  else if (hasError && !inFlight) status = "error"; // 有错且无重试在飞 → 错误态；重试中算 loading
  else status = "loading";

  return { data: entry?.data ?? null, status, error: hasError ? entry.error : null };
}
```

> `useSyncExternalStore` 的 `getSnapshot` 用 `getVersion()`（素数），满足快照「引用稳定、变化才变」约束。
> 弹窗/详情页复用同一 hook，天然共享缓存；`Date.now()` 不要求精确到 TTL 边界——每次弹窗 remount 都会重新算 staleness。
> 展示语义：只要 `entry.data` 存在（fresh 或 stale）就返回它（UI 立即渲染缓存）；`force` 重拉在后台完成后再 `emit()` 更新。

### 5.3 修改 `src/components/instance/LoadChart.tsx`

改动点（**渲染代码零改动**，仅状态来源换成本 hook）：

1. **删除** 状态与自 fetch effect：
   - `remoteData / loading / error` state（第 42-44 行）；
   - fetch effect 整段（第 106-165 行）。
   - `useEffect/useState` import 仅剩 `useState`（`hoursView` 仍需要）；新增 `useMemo` import。
2. **接入 hook + 搬运合并逻辑**：

   ```ts
   const selected = avaliableView.find((v) => v.label === activeHoursView);
   const { data: cacheData, status: historyStatus, error: historyError } = useHistory(
     uuid,
     selected?.hours
   );

   const remoteData = useMemo(() => {
     if (!cacheData) return null;
     const records = cacheData.records;
     const gpuDevices = cacheData.gpu_devices;
     // ===== 原 effect 体内的 gpu_detailed 合并 + 排序代码原样搬到这里 =====
     // （对 cacheData.records / cacheData.gpu_devices 操作，输出 mergedRecords）
     return mergedRecords; // RecordFormat[]
   }, [cacheData]);

   const loading = historyStatus === "loading";
   const error = historyStatus === "error" ? historyError : null;
   ```

3. 保留不动：`activeHoursView` 派生（94-98 行）、非 embedded 的 reset effect（100-104 行，仅 `!embedded` 跑）、`chartData` 派生（238-255 行）、以及全部渲染 JSX（`loading`/`error` 变量名不变，第 275-284 行渲染零改动）。

> 行为不变点：实时视图（`selected.hours` 为 undefined）→ hook 返回 idle → `remoteData=null`、`loading=false`，与旧 effect 的「无 hours 即清空」分支等价。

### 5.4 修改 `src/components/DashboardContent.tsx`

在 `useNodeList()` 后加预加载 effect：

```ts
const { nodeList, isLoading, error, refresh } = useNodeList();

useEffect(() => {
  if (!nodeList?.length) return;
  void preloadAll(nodeList.map((n) => n.uuid), 24);
}, [nodeList]);
```

> `nodeList` 每 5s refresh 产生新数组 → effect 每 5s 触发一次 `preloadAll`，但内部 `cache.has` 命中即返回，常驻节点零请求；新节点（新 uuid）缺失 → 被预加载。
> 用原始 `nodeList`（而非 `useStableValueWhile` 冻结的 `displayedNodeList`），不依赖地球视图开关。

### 5.5 修改 `src/components/providers.tsx`（常驻根，后台刷新器挂载点）

在 `Providers` 组件内启动 30s 后台刷新器（覆盖 dashboard 与详情页，整会话运行）：

```tsx
export function Providers({ children }: { children: React.ReactNode }) {
  React.useEffect(() => {
    startHistoryRefresher(); // 后台每 30s 刷新历史缓存
    return () => stopHistoryRefresher();
  }, []);
  // ...现有 JSX 不变
}
```

> 挂常驻根（layout 的 `Providers`）而非 DashboardContent：切到详情页时后台刷新不中断。
> `startHistoryRefresher` 幂等，重复挂载不双开定时器。

`LoadChartFloat.tsx`、`Node.tsx` 不改。

## 6. 数据流

### 6.1 打开弹窗（命中缓存）
1. 点击 📈 → `LoadChartFloat` Popover open → mount `<LoadChart uuid embedded data={[]}>`。
2. embedded → `activeHoursView` 派生为 24h → `useHistory(uuid, 24)`。
3. 有缓存（fresh 或 stale）→ **立即渲染 4 卡曲线，无 `<Loading/>`**（展示缓存）。
4. 同时 effect 以 `force: true` 触发后台重拉 → 完成后 `emit()` → `useSyncExternalStore` 重渲染 → 正在打开的弹窗自动刷新为最新曲线。

### 6.2 首次打开（预加载漏网 / 拉取失败）
- 缓存缺失 → 首次渲染 `loading` → effect 现场拉 → 完成后 `emit()` → 渲染曲线。
- 失败 → `status='error'` → 显示错误态；下次打开 remount → effect 再调 `loadHistory` → 错误条目无 promise → 自动重试。

### 6.3 详情页切视图
- `hoursView` 切换（4h↔1d）→ `selected.hours` 变 → `useHistory(uuid, newHours)` → 有缓存立即展示 + `force` 后台重拉；无缓存现场拉。切回旧视图同样「展示缓存 + 重拉」。

### 6.4 并发去重
- 弹窗 + 详情页同节点同 hours 同时 in-flight → 共享同一个 `entry.promise` → 只发一次请求，两个订阅者都收到数据。

### 6.5 后台 30s 循环
- `Providers` 挂载 → `startHistoryRefresher()` → 每 30s 顺序 `loadHistory` 所有已有条目（fresh 跳过 / 过期重拉）。
- 完成 → `emit()` → 打开中的弹窗/详情页曲线自动更新，无感。
- 浏览器后台标签节流 `setInterval`（Chrome ≥1min）→ 循环变慢，但「打开即刷（force）」兜底保证打开时刷新。

## 7. 新鲜度 / 错误语义

| 场景 | 行为 |
|---|---|
| 打开图表 | **立即展示缓存**（有则显示，绝不等网络），同时 `force: true` 后台重拉一次；完成后原地更新 |
| 30s 后台循环 | 每 30s 顺序刷新已有条目（`loadHistory` 默认 SWR：fresh 跳过 / 过期重拉），缓存恒 ≤30s 旧 |
| 并发去重 | 同一 `(uuid,hours)` 无论谁触发（后台/打开/预加载）共享 in-flight promise，只发一次请求 |
| 拉取失败且无旧数据 | `status='error'`，显示错误态；下次打开/后台循环自动重试 |
| 拉取失败但有过期旧数据 | 保留旧曲线继续显示（SWR 语义），不弹错误 |
| 预加载期间某节点失败 | `preloadAll` 继续下一个节点（串行 + 内部 catch），不阻塞其余节点 |
| 内存 | 实际键仅 `(uuid,4)` / `(uuid,24)`，5 节点 ≈ 10 条目，无需淘汰；`clearHistoryCache` 供 dev 用 |

## 8. 边界与陷阱

- **`useSyncExternalStore` 快照必须是稳定引用**：`getVersion()` 返回素数，满足约束；禁止返回 `{...}` 字面量新对象。
- **`emit()` 不能在渲染期调用**：`loadHistory` 在 effect（而非 render）中触发；其同步段 `cache.set` + `emit()` 执行于 effect 中，安全。
- **gpu_devices 不能丢**：缓存存完整 `RecordsData`（records + gpu_devices），详情页 GPU 合并逻辑保留在 LoadChart 的 `useMemo` 内，字节等价。
- **错误条目不保留 promise**：否则下一次打开会命中 in-flight 死引用、永不重试。
- **`preloadAll` 只填缺失**：填充职责与「30s 后台刷新（保活）」分离，不重复发请求。
- **后台循环与「打开即刷」不打架**：`loadHistory` 的 in-flight 去重保证同键同时最多一次请求。
- **后台标签节流**：`setInterval` 在不可见标签被节流（Chrome ≥1min），后台刷新变慢；打开图表时 force 刷新兜底。
- **`intervalSec` prop** 当前未被使用（解构里都没有），不触碰。

## 9. 验证（不截图）

### 9.1 本地构建（铁律）
```
npm ci && npm run build   # next build static export → dist/，必须通过
```

### 9.2 浏览器验证（本地 Node 代理：服务 dist + 转发 /api/* 到 hub、剥离 Origin/Referer）
Playwright a11y 文本快照 + `browser_network_requests` 断言：

1. **预加载**：dashboard 加载后，恰好每节点 1 次 `records/load?hours=24`，且请求错峰（非同时）。
2. **秒开 + 打开即刷**：点击 📈 弹窗 → 4 卡曲线**立即渲染**（文本快照无 Loading 占位），同时触发 1 次 `records/load` 后台重拉（force）；图表全程不闪 Loading。
3. **30s 后台循环**：不开任何图表，代理/devtools 观察每节点约每 30s 出现一次 `records/load?hours=24`（有缓存条目后）。
4. **并发去重**：连续快速开关同一弹窗 → 同一时刻只有 1 个 in-flight 请求。
5. **详情页共享**：进 `/instance/<uuid>` → 切 4h→1d→4h → 每视图每节点 1 次请求（force）；切回视图展示缓存 + 重拉；GPU 卡在 `has_gpu_data` 时正常显示。
6. **回归**：实时视图正常（走 `data` prop）；embedded 隐藏视图切换器；`Loading`/错误态正常；关闭再开弹窗曲线仍正常渲染。

### 9.3 版本与打包
- `komari-theme.json`：`version` 0.1.1 → **0.1.2**，`description` 更新为 "history cache + preload"。
- 打包 zip = `preview.png` + `komari-theme.json` + `dist/`。

## 10. 部署与回滚（同 v0.1.1 流程）

1. **先本地 build 通过**（§9.1），再动线上。
2. hub 备份现有主题：`/home/mango/komari/data/theme/komari-deer/` → `theme-backup/komari-deer-v0.1.1.tar.gz`。
3. login（session_token）→ `PUT /api/admin/theme/upload`（raw zip，同 short `komari-deer`）；若拒重复 short → delete + upload + `theme/set`。
4. `GET /api/admin/theme/set?theme=komari-deer`；校验部署 bundle 与本地 **sha256 一致**、theme list 显示 v0.1.2。
5. 回滚：`theme/set?theme=mango-dashboard` 秒回，或解包恢复备份。

## 11. 后续（非本次范围）

- 若未来节点数大增（>20）：给 `preloadAll` 加并发上限（如 2-3 并发）或对 24h 预加载加优先级队列。
- 可选：把纯 TS store 配 vitest 单测（当前仓库无测试框架，不新增）。
- 若需跨整页刷新持久化：升级为 localStorage + 失效时间，本轮不做。
