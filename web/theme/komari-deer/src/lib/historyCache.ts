import type { RecordFormat } from "@/utils/RecordHelper";

export const SWR_TTL = 300 * 1000; // 5 分钟；弹窗打开即 force 重拉，后台保持缓存不算太旧即可，过高 TTL 浪费链路带宽

// fetch 无默认超时，链路劣化时可能永久挂起 → 卡死该 key 的 in-flight 去重（promise 永不落定），
// 后续所有打开/刷新/preload 都被 dedup 挡住；15s 中止让其走 catch 清理 promise
const FETCH_TIMEOUT_MS = 15_000;

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
  if (!opts.force && isFresh(existing, now)) return; // 非 force 且 5 分钟内 → 零请求

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
  const p = fetch(`/api/records/load?uuid=${encodeURIComponent(uuid)}&hours=${hours}`, {
    signal: controller.signal,
  })
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
        error: err?.name === "AbortError" ? "timeout" : err?.message || "Error",
        promise: null,
      });
      emit();
      return existing?.data ?? { records: [], gpu_devices: {} };
    })
    .finally(() => clearTimeout(timeout));

  cache.set(k, {
    uuid,
    hours,
    data: existing?.data ?? null,
    fetchedAt: existing?.fetchedAt ?? null,
    error: existing?.error ?? null,
    promise: p,
  });
  emit(); // 通知订阅者「已发起拉取」
  return p.then(() => undefined);
}

export async function preloadAll(uuids: string[], hours = 24): Promise<void> {
  for (const uuid of uuids) {
    if (cache.has(key(uuid, hours))) continue; // 已尝试（成功或失败）→ 不重复
    await loadHistory(uuid, hours);            // 串行 await = 错峰，不洪峰打 hub
  }
}

let refresherTimer: ReturnType<typeof setInterval> | null = null;
let refreshing = false;

export function startHistoryRefresher(intervalMs = 300_000): void {
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
