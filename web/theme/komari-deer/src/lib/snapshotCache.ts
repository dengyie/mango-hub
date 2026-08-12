import type { NodeBasicInfo } from "@/contexts/NodeListContext";
import type { LiveDataResponse } from "@/types/LiveData";

// 首帧快速渲染:把上次成功拉到 nodeList + liveData 落 localStorage,
// 挂载后先读快照渲染(跳过骨架等网络),新数据到达后再覆盖。
// 只在客户端 effect 里读写,SSR/静态导出不触碰,避免 hydration mismatch。

const STORAGE_KEY = "komari-deer:dashboard-snapshot:v1";

// liveData 每 4s 轮询一次,不每次都写 localStorage;节流到 60s 一次,
// 既保「下次进站有最近数据」又不让同步写堵主线程。
const LIVE_DATA_WRITE_INTERVAL = 60_000;

// 快照时效:超过该时长的字段视为过期,水合时跳过,不渲染离谱旧数据。
const SNAPSHOT_MAX_AGE = 24 * 60 * 60 * 1000;

export type DashboardSnapshot = {
  nodeList: NodeBasicInfo[] | null;
  liveData: LiveDataResponse | null;
  nodeListAt: number;
  liveDataAt: number;
  savedAt: number;
};

let lastLiveDataWrite = 0;

function safeGetItem(): string | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage.getItem(STORAGE_KEY);
  } catch {
    return null; // 隐私模式/配额异常:不水合,退回到等网络
  }
}

function safeSetItem(value: string): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, value);
  } catch {
    // 配额满/禁用:静默丢弃,不影响实时数据流
  }
}

// 纯解析,不过滤时效:给 write() 当合并基底,避免 TTL 过滤把过期的兄弟字段
// 置 null 后又被写回存储,永久抹掉该字段。
function readRaw(): DashboardSnapshot | null {
  const raw = safeGetItem();
  if (!raw) return null;
  try {
    const snap = JSON.parse(raw) as DashboardSnapshot;
    if (!snap || typeof snap !== "object") return null;
    return snap;
  } catch {
    return null;
  }
}

export function loadSnapshot(): DashboardSnapshot | null {
  const snap = readRaw();
  if (!snap) return null;
  // 过期的字段置空,让水合端退回到等网络(而不是渲染陈旧数据)。
  const now = Date.now();
  // 老 schema 缺时间戳时 ?? 0:now-0 必然超龄,按过期处理而非永不失效
  if (snap.nodeList && now - (snap.nodeListAt ?? 0) > SNAPSHOT_MAX_AGE) snap.nodeList = null;
  if (snap.liveData && now - (snap.liveDataAt ?? 0) > SNAPSHOT_MAX_AGE) snap.liveData = null;
  return snap;
}

function write(partial: Partial<DashboardSnapshot>): void {
  const prev = readRaw();
  safeSetItem(
    JSON.stringify({
      ...(prev ?? {}),
      ...partial,
      savedAt: Date.now(),
    })
  );
}

export function saveNodeListSnapshot(nodeList: NodeBasicInfo[]): void {
  write({ nodeList, nodeListAt: Date.now() });
}

export function saveLiveDataSnapshot(liveData: LiveDataResponse): void {
  const now = Date.now();
  if (now - lastLiveDataWrite < LIVE_DATA_WRITE_INTERVAL) return;
  lastLiveDataWrite = now;
  write({ liveData, liveDataAt: now });
}
