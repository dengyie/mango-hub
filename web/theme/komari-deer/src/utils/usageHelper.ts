/**
 * CNB token 用量快照的纯逻辑：按天桶聚合、今日/累计求和、格式化、stale 判定。
 * 与后端契约：ai-proxy 仓 deploy/cnb-register.py record_usage 写入 /www/cnb-usage.json，
 * 经 hk nginx GET /ops/usage 暴露（CORS *）。字段变更必须两侧同步。
 * 数据链：反代 /usage 快照（boot 内累计）→ cron usage-sync 每 5 分钟上报 → hk 按 boot_id
 * 增量对账入 UTC+8 按天桶（工作区回收=新 boot 从零，差值语义保证不重复计数）。
 */

export interface UsageDay {
  prompt: number;
  completion: number;
  requests: number;
  errors: number;
}

export interface UsageSnapshot {
  version: number;
  days: Record<string, UsageDay>;
  boots?: Record<string, UsageDay>;
  updated_at: string;
}

const ZERO: UsageDay = { prompt: 0, completion: 0, requests: 0, errors: 0 };

function addDay(a: UsageDay, b: Partial<UsageDay> | undefined): UsageDay {
  return {
    prompt: a.prompt + (Number(b?.prompt) || 0),
    completion: a.completion + (Number(b?.completion) || 0),
    requests: a.requests + (Number(b?.requests) || 0),
    errors: a.errors + (Number(b?.errors) || 0),
  };
}

/** UTC+8 当天 key（YYYY-MM-DD），与 hk 侧 day_key_utc8 同口径 */
export function todayKeyUtc8(now: number = Date.now()): string {
  // +8h 后取 UTC 年月日（与后端 time.gmtime(ts + 8*3600) 一致）
  const shifted = new Date(now + 8 * 3600 * 1000);
  const y = shifted.getUTCFullYear();
  const m = String(shifted.getUTCMonth() + 1).padStart(2, "0");
  const d = String(shifted.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

/** 今日用量（按 UTC+8）；无当天数据返回全零 */
export function todayUsage(
  snap: UsageSnapshot | null | undefined,
  now: number = Date.now()
): UsageDay {
  const days = snap?.days;
  if (!days) return { ...ZERO };
  return addDay({ ...ZERO }, days[todayKeyUtc8(now)]);
}

/** 所有天累计求和 */
export function sumDays(snap: UsageSnapshot | null | undefined): UsageDay {
  const days = snap?.days;
  if (!days) return { ...ZERO };
  return Object.values(days).reduce<UsageDay>((acc, d) => addDay(acc, d), {
    ...ZERO,
  });
}

/** 最近 N 天（按 key 升序，最后 N 个），返回 [dateKey, UsageDay][] */
export function recentDays(
  snap: UsageSnapshot | null | undefined,
  n: number = 7
): [string, UsageDay][] {
  const days = snap?.days;
  if (!days) return [];
  return Object.keys(days)
    .sort()
    .slice(-n)
    .map((k) => [k, addDay({ ...ZERO }, days[k])] as [string, UsageDay]);
}

/**
 * token 计数 → 紧凑字符串：<1000 原样，<1e6 用 K（一位小数），否则 M（两位小数）。
 * 例：842639 → "842.6K"，1250000 → "1.25M"，527 → "527"。
 */
export function formatTokens(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return "-";
  const v = Number(n);
  if (v < 1000) return String(Math.round(v));
  if (v < 1_000_000) return `${(v / 1000).toFixed(1)}K`;
  return `${(v / 1_000_000).toFixed(2)}M`;
}

/** 整数千分位（requests 等小计数用） */
export function formatCount(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return "-";
  return Math.round(Number(n)).toLocaleString();
}

/** 快照更新时间距 now 是否超过 stale 阈值（默认 30 分钟，与 quota 同口径） */
export function isUsageStale(
  updatedAt: string | undefined,
  now: number,
  staleMs = 30 * 60 * 1000
): boolean {
  if (!updatedAt) return false;
  const t = new Date(updatedAt).getTime();
  if (!Number.isFinite(t)) return false;
  return now - t > staleMs;
}
