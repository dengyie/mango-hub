# Komari Deer 历史曲线缓存 + 预加载 实现计划（v0.1.2）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 komari-deer 主题的 📈 弹窗历史曲线加缓存 + 预加载：打开弹窗零 Loading、展示缓存并后台刷新，覆盖弹窗 24h 与详情页 4h/1d。

**Architecture:** 模块级内存 store（`src/lib/historyCache.ts`，纯 TS）+ React hook（`src/lib/useHistory.ts`，`useSyncExternalStore`）。`LoadChart` 不再自 fetch，改由 hook 消费缓存；`DashboardContent` 在节点就绪后 `preloadAll`；`providers.tsx` 常驻根启动 30s 后台刷新器。

**Tech Stack:** Next.js 16.1.0（`distDir:'dist'`, `output:'export'`）、React 19、TypeScript。无数据获取库、无测试框架。

## Global Constraints

- 工作分支：`feature/history-cache-preload`（自 `6ed9d60` 分出，当前已在其上）。
- **不新增任何依赖**（不引入 react-query/SWR/axios）；无测试框架（无 vitest/jest），验证靠 `tsc` + `next build` + 浏览器。
- **推线上前必须 Mac 本地 `npm run build` 通过**（铁律），再动 hub。
- 不截图（浏览器验证用 Playwright a11y 文本快照 + `browser_network_requests`）。
- 不配置/修改 ping 任务。
- 密码 `2625451001` 仅会话内使用，不写入磁盘/memory/日志。
- **`LoadChart.tsx` 渲染 JSX 与 `chartData` 派生零改动**；仅状态来源换成本 hook；gpu_detailed 合并逻辑原样搬运（字节等价）。
- 数据事实（写死在实现里）：历史接口 `GET /api/records/load?uuid={uuid}&hours={hours}`，响应 `{ data: { records: [...], gpu_devices: {...} } }`；`record_preserve_time=24` → 实际可用视图只有 4h / 24h；`SWR_TTL = 30 * 1000`；后台刷新 `intervalMs = 30_000`；预加载 `hours = 24`。

---

## Task 1: 新建 `src/lib/historyCache.ts` + `src/lib/useHistory.ts`

**Files:**
- Create: `src/lib/historyCache.ts`
- Create: `src/lib/useHistory.ts`

**Interfaces:**
- Produces: `SWR_TTL`, `RecordsData`, `CacheEntry`, `subscribe`, `getVersion`, `getEntry`, `isFresh`, `loadHistory(uuid, hours, opts?)`, `preloadAll(uuids, hours?)`, `startHistoryRefresher(intervalMs?)`, `stopHistoryRefresher`, `clearHistoryCache`, and `useHistory(uuid, hours?)` → `{ data, status, error }`.

- [ ] **Step 1: Write `src/lib/historyCache.ts`** (full content)

```ts
import type { RecordFormat } from "@/utils/RecordHelper";

export const SWR_TTL = 30 * 1000; // 30 秒；后台循环与「打开即刷」的过期阈值

type GpuDeviceRecords = {
  records: Array<{
    time: string;
    utilization: number;
    mem_used: number;
    mem_total: number;
    temperature: number;
    device_index: number;
    device_name: string;
  }>;
};

// /api/records/load 响应 data 的忠实缓存形状（含 gpu_devices，供详情页 GPU 合并）
export type RecordsData = {
  records: RecordFormat[];
  gpu_devices: Record<string, GpuDeviceRecords>;
};

export type CacheEntry = {
  uuid: string;               // 供后台刷新器遍历重建 key
  hours: number;
  data: RecordsData | null;   // null = 拉取失败或从未成功
  fetchedAt: number | null;   // 成功拉取时间戳
  error: string | null;
  promise: Promise<RecordsData> | null; // in-flight 去重；错误条目不保留 promise 以允许重试
};

const cache = new Map<string, CacheEntry>();
const listeners = new Set<() => void>();
let version = 0;

function key(uuid: string, hours: number) {
  return `${uuid}:${hours}`;
}

function emit() {
  version++;
  for (const l of listeners) l();
}

export function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function getVersion(): number {
  return version;
}

export function getEntry(uuid: string, hours: number): CacheEntry | undefined {
  return cache.get(key(uuid, hours));
}

export function isFresh(entry: CacheEntry | undefined, now: number): boolean {
  return !!entry?.data && entry.fetchedAt !== null && now - entry.fetchedAt < SWR_TTL;
}

export async function loadHistory(
  uuid: string,
  hours?: number,
  opts: { force?: boolean } = {}
): Promise<void> {
  if (!uuid || !hours) return;
  const k = key(uuid, hours);
  const now = Date.now();
  const existing = cache.get(k);

  if (existing?.promise) return;                    // in-flight 去重（始终生效）
  if (!opts.force && isFresh(existing, now)) return; // 非 force 且 30s 内 → 零请求

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
      return existing?.data ?? { records: [], gpu_devices: {} };
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
}

export async function preloadAll(uuids: string[], hours = 24): Promise<void> {
  for (const uuid of uuids) {
    if (cache.has(key(uuid, hours))) continue; // 已尝试（成功或失败）→ 不重复
    await loadHistory(uuid, hours);            // 串行 await = 错峰，不洪峰打 hub
  }
}

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

export function clearHistoryCache(): void {
  cache.clear();
  emit();
}
```

- [ ] **Step 2: Write `src/lib/useHistory.ts`** (full content)

```ts
"use client";

import { useEffect, useSyncExternalStore } from "react";
import {
  subscribe,
  getVersion,
  getEntry,
  loadHistory,
  SWR_TTL,
} from "./historyCache";
import type { RecordsData } from "./historyCache";

export type HistoryStatus = "idle" | "loading" | "ready" | "stale" | "error";

export function useHistory(uuid: string, hours?: number): {
  data: RecordsData | null;
  status: HistoryStatus;
  error: string | null;
} {
  useSyncExternalStore(subscribe, getVersion, () => 0); // 第三参 = getServerSnapshot，避免 SSR/hydration 快照不一致

  useEffect(() => {
    void loadHistory(uuid, hours, { force: true }); // 打开图表/切视图 → 展示缓存 + 无条件后台重拉
  }, [uuid, hours]);

  if (!uuid || !hours) return { data: null, status: "idle", error: null }; // 实时视图/无 uuid：不拉历史、不显示 loading

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

- [ ] **Step 3: Typecheck** — 两个新文件在 Task 2/3 之前尚未被 import，`next build` 不会编译它们，用独立 tsc 校验：

Run: `npx tsc --noEmit`
Expected: exit 0，无 `historyCache.ts` / `useHistory.ts` 相关错误。`HistoryStatus`/`RecordsData`/`SWR_TTL` 均被引用（导出 + 注解），不会有 unused 告警。

- [ ] **Step 4: Commit**

```bash
git add src/lib/historyCache.ts src/lib/useHistory.ts
git commit -m "feat: add history cache store + useHistory hook"
```

---

## Task 2: `LoadChart` 接入 `useHistory`（渲染零改动）

**Files:**
- Modify: `src/components/instance/LoadChart.tsx`

**Interfaces:**
- Consumes: `useHistory(uuid, hours?)` from `@/lib/useHistory`（Task 1）

- [ ] **Step 1: 改 import**

`src/components/instance/LoadChart.tsx` 第 3 行：
```ts
import { useEffect, useState } from "react";
```
改为：
```ts
import { useEffect, useMemo, useState } from "react";
```

在 `import Loading from "@/components/loading";`（第 26 行）之后新增：
```ts
import { useHistory } from "@/lib/useHistory";
```

- [ ] **Step 2: 删自管理 state（第 41-44 行）**

把：
```ts
  const [hoursView, setHoursView] = useState<string>("real-time");
  const [remoteData, setRemoteData] = useState<RecordFormat[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
```
改为（仅保留 `hoursView`）：
```ts
  const [hoursView, setHoursView] = useState<string>("real-time");
```

- [ ] **Step 3: 删自 fetch effect（第 106-165 行）**

删除整段：
```ts
  useEffect(() => {
    const selected = avaliableView.find((v) => v.label === activeHoursView);
    if (!uuid) return;
    if (!selected || !selected.hours) {
      setRemoteData(null);
      setError(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    fetch(`/api/records/load?uuid=${uuid}&hours=${selected.hours}`)
      .then((res) => {
        if (!res.ok) throw new Error(res.statusText);
        return res.json();
      })
      .then((resp) => {
        const records = resp.data?.records || [];
        const gpuDevices = resp.data?.gpu_devices || {};

        const mergedRecords = records.map((record: RecordFormat) => {
          const gpuDetailed = [];

          for (const deviceIndex in gpuDevices) {
            const device = gpuDevices[deviceIndex];
            const gpuRecord = device.records?.find(
              (gr: any) =>
                new Date(gr.time).getTime() === new Date(record.time).getTime()
            );

            if (gpuRecord) {
              gpuDetailed.push({
                usage: gpuRecord.utilization,
                memory: (gpuRecord.mem_used / gpuRecord.mem_total) * 100,
                temperature: gpuRecord.temperature,
                device_index: gpuRecord.device_index,
                device_name: gpuRecord.device_name,
                mem_total: gpuRecord.mem_total,
                mem_used: gpuRecord.mem_used,});
            }
          }

          return {
            ...record,
            gpu_detailed: gpuDetailed.length > 0 ? gpuDetailed : undefined,
          };
        });

        mergedRecords.sort(
          (a: RecordFormat, b: RecordFormat) =>
            new Date(a.time).getTime() - new Date(b.time).getTime()
        );
        setRemoteData(mergedRecords);
        setLoading(false);
      })
      .catch((err) => {
        setError(err.message || "Error");
        setLoading(false);
      });
  }, [activeHoursView, uuid]);
```

> 注意：删除后 `activeHoursView` 仍在第 94-98 行派生、在 `chartData` 与 `timeFormatter`/`lableFormatter` 中被引用，**不要删**。

- [ ] **Step 4: 插入 hook + 搬运合并逻辑（在 `activeHoursView` 派生之后、reset effect 之前）**

在：
```ts
  const activeHoursView = embedded
    ? (avaliableView.find((v) => v.hours === 24)?.label ??
      avaliableView.find((v) => v.hours)?.label ??
      t("common.real_time"))
    : hoursView;
```
之后插入：
```ts
  const selected = avaliableView.find((v) => v.label === activeHoursView);
  const {
    data: cacheData,
    status: historyStatus,
    error: historyError,
  } = useHistory(uuid, selected?.hours);

  const remoteData = useMemo(() => {
    if (!cacheData) return null;
    const gpuDevices = cacheData.gpu_devices;
    const mergedRecords = cacheData.records.map((record: RecordFormat) => {
      const gpuDetailed = [];

      for (const deviceIndex in gpuDevices) {
        const device = gpuDevices[deviceIndex];
        const gpuRecord = device.records?.find(
          (gr: any) =>
            new Date(gr.time).getTime() === new Date(record.time).getTime()
        );

        if (gpuRecord) {
          gpuDetailed.push({
            usage: gpuRecord.utilization,
            memory: (gpuRecord.mem_used / gpuRecord.mem_total) * 100,
            temperature: gpuRecord.temperature,
            device_index: gpuRecord.device_index,
            device_name: gpuRecord.device_name,
            mem_total: gpuRecord.mem_total,
            mem_used: gpuRecord.mem_used,
          });
        }
      }

      return {
        ...record,
        gpu_detailed: gpuDetailed.length > 0 ? gpuDetailed : undefined,
      };
    });

    mergedRecords.sort(
      (a: RecordFormat, b: RecordFormat) =>
        new Date(a.time).getTime() - new Date(b.time).getTime()
    );
    return mergedRecords;
  }, [cacheData]);

  const loading = historyStatus === "loading";
  const error = historyStatus === "error" ? historyError : null;
```

> 合并逻辑与旧 effect 体内**逐字节等价**（仅数据来源从 fetch 响应换成 `cacheData`）。`loading`/`error` 变量名不变 → 第 275-284 行渲染零改动。

- [ ] **Step 5: Typecheck + 构建**

Run: `npx tsc --noEmit && npm run build`
Expected: exit 0；无 `LoadChart.tsx` 相关类型错误；`dist/` 生成。

- [ ] **Step 6: 行为自查（读改后文件确认）**
- `activeHoursView`、`chartData`（238-255 行）、`timeFormatter`/`lableFormatter`、reset effect（100-104 行）原样保留；
- 实时视图：`selected?.hours` 为 undefined → `useHistory` 返回 `idle` → `remoteData=null`、`loading=false`，与旧行为等价；
- 渲染 JSX 里 `loading`（275 行）与 `error`（280 行）仍引用同名变量。

- [ ] **Step 7: Commit**

```bash
git add src/components/instance/LoadChart.tsx
git commit -m "refactor: LoadChart consumes useHistory (cache-backed history)"
```

---

## Task 3: `DashboardContent` 预加载 + `providers` 后台刷新器

**Files:**
- Modify: `src/components/DashboardContent.tsx`
- Modify: `src/components/providers.tsx`

**Interfaces:**
- Consumes: `preloadAll`, `startHistoryRefresher`, `stopHistoryRefresher`（Task 1）

- [ ] **Step 1: `DashboardContent.tsx` 加预加载**

在 import 区（`import { useNodeList } from "@/contexts/NodeListContext";` 附近）加：
```ts
import { preloadAll } from "@/lib/historyCache";
```

在 `const { nodeList, isLoading, error, refresh } = useNodeList();`（第 145 行）之后加：
```ts
  useEffect(() => {
    if (!nodeList?.length) return;
    void preloadAll(nodeList.map((n) => n.uuid), 24);
  }, [nodeList]);
```

> `nodeList` 每 5s refresh 产生新数组 → effect 每 5s 触发一次 `preloadAll`，内部 `cache.has` 命中即返回，常驻节点零请求；新节点上线自动被预加载。

- [ ] **Step 2: `providers.tsx` 启动后台刷新器**

在 import 区加：
```ts
import { startHistoryRefresher, stopHistoryRefresher } from "@/lib/historyCache";
```

把 `Providers` 函数体开头（`return (` 之前）插入：
```tsx
  React.useEffect(() => {
    startHistoryRefresher(); // 后台每 30s 刷新历史缓存（覆盖 dashboard + 详情页）
    return () => stopHistoryRefresher();
  }, []);
```

- [ ] **Step 3: Typecheck + 构建**

Run: `npx tsc --noEmit && npm run build`
Expected: exit 0。

- [ ] **Step 4: Commit**

```bash
git add src/components/DashboardContent.tsx src/components/providers.tsx
git commit -m "feat: preload history on dashboard ready + 30s background refresher"
```

---

## Task 4: 版本 bump + 本地构建 + 打包

**Files:**
- Modify: `komari-theme.json`

- [ ] **Step 1: `komari-theme.json` 版本 0.1.1 → 0.1.2**

把 `"version": "0.1.1"` 改为 `"version": "0.1.2"`；`"description"` 改为 `"history cache + preload (v0.1.2)"`（保持与现有描述风格一致，其余字段不动）。

- [ ] **Step 2: 完整本地构建（铁律）**

Run: `rm -rf dist && npm run build`
Expected: exit 0，`dist/` 重新生成，`npm run build` 尾部无 error。

- [ ] **Step 3: 打包主题 zip**

```bash
cd /Users/mango/project/komari-deer
rm -f komari-deer-v0.1.2.zip
zip -r komari-deer-v0.1.2.zip preview.png komari-theme.json dist
sha256sum komari-deer-v0.1.2.zip   # 记录哈希，部署后与 hub 落盘比对
```

- [ ] **Step 4: Commit**

```bash
git add komari-theme.json
git commit -m "chore: bump theme version to 0.1.2"
```

---

## Task 5: 浏览器验证（本地代理，不截图）

**Files:** （无代码改动；用 /tmp 下临时脚本 + Playwright）

- [x] **Step 1: 起本地代理**

若 `/tmp/km-proxy/server.mjs` 不存在则新建（内容固定）：
```js
import http from "node:http";
import https from "node:https";
import fs from "node:fs";
import path from "node:path";

const DIST = "/Users/mango/project/komari-deer/dist";
const UPSTREAM = "vps.mangoqwq.com";
const PORT = 8811;

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".svg": "image/svg+xml",
  ".txt": "text/plain; charset=utf-8",
  ".map": "application/json",
};

function proxyApi(req, res) {
  const headers = { ...req.headers, host: UPSTREAM, "accept-encoding": "identity" };
  delete headers.origin;
  delete headers.referer;
  const upstreamReq = https.request(
    { host: UPSTREAM, path: req.url, method: req.method, headers },
    (upRes) => {
      res.writeHead(upRes.statusCode, upRes.headers);
      upRes.pipe(res);
    }
  );
  upstreamReq.on("error", (e) => {
    res.writeHead(502, { "content-type": "text/plain" });
    res.end("proxy error: " + e.message);
  });
  req.pipe(upstreamReq);
}

function serveStatic(req, res) {
  let urlPath = decodeURIComponent(new URL(req.url, "http://x").pathname);
  if (urlPath === "/") urlPath = "/index.html";
  const filePath = path.join(DIST, urlPath);
  if (!filePath.startsWith(DIST)) { res.writeHead(403); res.end("forbidden"); return; }
  if (fs.existsSync(filePath) && fs.statSync(filePath).isDirectory()) {
    const idx = path.join(filePath, "index.html");
    res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    fs.createReadStream(idx).pipe(res);
    return;
  }
  if (fs.existsSync(filePath)) {
    const ext = path.extname(filePath).toLowerCase();
    res.writeHead(200, { "content-type": MIME[ext] || "application/octet-stream" });
    fs.createReadStream(filePath).pipe(res);
  } else {
    res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    fs.createReadStream(path.join(DIST, "index.html")).pipe(res);
  }
}

http.createServer((req, res) => {
  if (req.url.startsWith("/api/")) return proxyApi(req, res);
  return serveStatic(req, res);
}).listen(PORT, () => {
  console.log(`km-proxy listening on http://localhost:${PORT}`);
});
```

Run（后台）: `node /tmp/km-proxy/server.mjs`

- [x] **Step 2: Playwright 断言（a11y 快照 + 网络请求）**

导航 `http://localhost:8811`。按 §9.2 逐条断言：
1. **预加载**：`browser_network_requests` filter `records/load` → 每个节点恰好 1 次 `?hours=24`，且时间戳错峰。
2. **秒开 + 打开即刷**：点击某节点卡 📈（TrendingUp）→ `browser_snapshot` 出现 CPU/Ram/Disk/Network 四卡曲线（x 轴时间跨度 24h），**无 Loading 占位文本**；同时网络出现 1 次 `records/load?hours=24`（force 重拉）。
3. **30s 后台循环**：关闭所有弹窗，等待 ~70s → `browser_network_requests` 出现第二轮每节点 `records/load?hours=24`。
4. **并发去重**：连续快速开/关同一弹窗 3 次 → 网络里同一时刻 `records/load` in-flight 数量 ≤1。
5. **详情页共享**：导航 `/instance/{uuid}` → SegmentedControl 切 4h→1d→4h → 每视图 1 次请求；切回旧视图立即出曲线 + 1 次 force 重拉；GPU 卡在 `has_gpu_data` 节点正常显示。
6. **回归**：实时视图（实时刷新）正常；embedded 无视图切换器；关闭再开弹窗曲线正常。

- [x] **Step 3: 记录结果** — 6 条断言全 PASS（GPU 子断言 N/A，5 节点 gpu=0）：
  1. 预加载 PASS（每节点 1×hours=24 串行错峰）
  2. 秒开+打开即刷 PASS（四卡有数据、无 "No ping data"、开时 1×force）
  3. 30s 后台循环 PASS（~70s 后第二轮刷新）
  4. 并发去重 PASS（in-flight ≤1）
  5. 详情页共享 PASS（每视图 1 请求；切回旧视图缓存秒出+1 force）
  6. 回归 PASS（实时轮询正常；embedded 无切换器仅 4 卡；关开正常）
  - 附注：#418 hydration error（详情 URL 直载）经 v0.1.1 baseline A/B 实测为既有问题，非本 feature 回归；React 自动恢复。

---

## Task 6: 部署 v0.1.2（**先向用户确认再动线上**）

**Files:** 无代码改动；hub admin API 操作。

- [x] **Step 0: 与用户确认** — 本地 build 已过（Task 4）、浏览器验证已过（Task 5）后，明确向用户说明将把 `komari-deer` v0.1.2 上传并激活到 hub（google-vps），**等确认再执行**。回滚预案：`theme/set?theme=mango-dashboard` 秒回 / 恢复备份 tar。✅ 用户 2026-08-11 明确确认「确认，开始部署」。
- [x] **Step 1: 备份现有主题**（hub）: `/home/mango/komari/data/theme/komari-deer/` → `/home/mango/komari/data/theme-backup/komari-deer-v0.1.1.tar.gz`（386 文件，tar 校验 OK）
- [x] **Step 2: 上传** — login（`POST /api/login`，取 `data.set-cookie.session_token`）→ `PUT /api/admin/theme/upload`（multipart `file` 字段，同 short 直接覆盖成功）→ status=success version=0.1.2
- [x] **Step 3: 激活并校验** — `GET /api/admin/theme/set?theme=komari-deer` 成功；theme/list 显示 komari-deer v0.1.2；theme_configurations 激活=komari-deer；hub 落盘 komari-theme.json/preview.png/dist 372/372 与本地 sha256 一致；托管 chunk 与本地一致
- [x] **Step 4: 线上冒烟（不截图）** — 走本地代理打活 hub API：仪表盘 5/5 在线，📈 弹窗四卡真实 24h 数据，console 0 错误（⚠ 当日 Mac→google-vps 直连链路 20% 丢包，浏览器直连被掐，经 curl 验证 CORS/端点正常，判断为链路劣化非部署问题）

---

## Self-Review 备注

- 无占位符：所有代码块为最终实现。
- 类型一致性：`RecordsData`/`CacheEntry`/`HistoryStatus` 在 Task 1 定义、Task 2-3 消费，名称统一；`loadHistory` 第三参 `opts?: { force?: boolean }` 各处一致。
- Spec 覆盖：预加载（Task 3）、后台 30s 刷新（Task 3）、打开即刷 force（Task 1/2）、全局共享（Task 2 详情页同 hook）、并发去重（Task 1 `existing.promise`）、错误语义（Task 1 catch 保留旧数据）、部署回滚（Task 6）——全部落在计划里。
- 已知踩坑已写进实现：`loadHistory` 的 `.catch` 返回类型（`RecordsData` 而非数组，避免 TS 报错）；`useSyncExternalStore` 需 `getServerSnapshot`（`() => 0`）避免 SSR/hydration 快照不一致。
