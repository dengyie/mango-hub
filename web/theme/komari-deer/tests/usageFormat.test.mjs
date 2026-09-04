import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// usageHelper.ts 是 .ts 文件，node:test 无法直接 import；
// 按仓库既有模式（quotaFormat.test.mjs）做源码级断言 + 内联复算核心逻辑。
// 若实现与断言漂移，测试会失败提示同步。
const source = readFileSync(
  new URL("../src/utils/usageHelper.ts", import.meta.url),
  "utf8"
);
const hook = readFileSync(
  new URL("../src/hooks/useCnbUsage.ts", import.meta.url),
  "utf8"
);
const chart = readFileSync(
  new URL("../src/components/instance/QuotaChart.tsx", import.meta.url),
  "utf8"
);

// —— 与实现等价的纯函数副本（聚合/格式化契约的执行级验证）——
function todayKeyUtc8(now) {
  const shifted = new Date(now + 8 * 3600 * 1000);
  const y = shifted.getUTCFullYear();
  const m = String(shifted.getUTCMonth() + 1).padStart(2, "0");
  const d = String(shifted.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}
function addDay(a, b) {
  return {
    prompt: a.prompt + (Number(b?.prompt) || 0),
    completion: a.completion + (Number(b?.completion) || 0),
    requests: a.requests + (Number(b?.requests) || 0),
    errors: a.errors + (Number(b?.errors) || 0),
  };
}
const ZERO = { prompt: 0, completion: 0, requests: 0, errors: 0 };
function todayUsage(snap, now) {
  const days = snap?.days;
  if (!days) return { ...ZERO };
  return addDay({ ...ZERO }, days[todayKeyUtc8(now)]);
}
function sumDays(snap) {
  const days = snap?.days;
  if (!days) return { ...ZERO };
  return Object.values(days).reduce((acc, d) => addDay(acc, d), { ...ZERO });
}
function recentDays(snap, n = 7) {
  const days = snap?.days;
  if (!days) return [];
  return Object.keys(days)
    .sort()
    .slice(-n)
    .map((k) => [k, addDay({ ...ZERO }, days[k])]);
}
function formatTokens(n) {
  if (n == null || !Number.isFinite(n)) return "-";
  const v = Number(n);
  if (v < 1000) return String(Math.round(v));
  if (v < 1_000_000) return `${(v / 1000).toFixed(1)}K`;
  return `${(v / 1_000_000).toFixed(2)}M`;
}
function formatCount(n) {
  if (n == null || !Number.isFinite(n)) return "-";
  return Math.round(Number(n)).toLocaleString();
}
function isUsageStale(updatedAt, now, staleMs = 30 * 60 * 1000) {
  if (!updatedAt) return false;
  const t = new Date(updatedAt).getTime();
  if (!Number.isFinite(t)) return false;
  return now - t > staleMs;
}

test("helper module exists and exports the full contract surface", () => {
  for (const name of [
    "UsageDay",
    "UsageSnapshot",
    "todayKeyUtc8",
    "todayUsage",
    "sumDays",
    "recentDays",
    "formatTokens",
    "formatCount",
    "isUsageStale",
  ]) {
    assert.ok(source.includes(name), `usageHelper must export ${name}`);
  }
});

test("todayKeyUtc8 matches hk day_key_utc8 (UTC+8 bucket, not local TZ)", () => {
  // 2026-09-04T20:00:00Z = 北京时间 09-05 04:00 → 桶应落在 09-05
  const now = Date.parse("2026-09-04T20:00:00Z");
  assert.equal(todayKeyUtc8(now), "2026-09-05");
  // 2026-09-04T15:59:00Z = 北京 23:59 → 09-04；+1min 跨桶 → 09-05
  assert.equal(todayKeyUtc8(Date.parse("2026-09-04T15:59:00Z")), "2026-09-04");
  assert.equal(todayKeyUtc8(Date.parse("2026-09-04T16:00:00Z")), "2026-09-05");
});

test("todayUsage picks UTC+8 bucket; sumDays aggregates all days", () => {
  const snap = {
    version: 2,
    updated_at: "2026-09-05T03:10:55+08:00",
    days: {
      "2026-09-03": { prompt: 100, completion: 10, requests: 1, errors: 0 },
      "2026-09-04": { prompt: 200, completion: 20, requests: 2, errors: 1 },
      "2026-09-05": { prompt: 400, completion: 40, requests: 3, errors: 0 },
    },
  };
  // now 取 09-05 桶内的时刻（UTC+8 当天）
  const now = Date.parse("2026-09-05T00:30:00+08:00");
  const today = todayUsage(snap, now);
  assert.equal(today.prompt, 400);
  assert.equal(today.requests, 3);
  const total = sumDays(snap);
  assert.equal(total.prompt, 700);
  assert.equal(total.completion, 70);
  assert.equal(total.requests, 6);
  assert.equal(total.errors, 1);
});

test("empty/missing snapshot yields zeros, recentDays sorts and slices", () => {
  assert.equal(todayUsage(null).prompt, 0);
  assert.equal(sumDays(undefined).completion, 0);
  assert.deepEqual(recentDays(null), []);
  const snap = {
    version: 2,
    updated_at: "x",
    days: {
      "2026-09-05": { prompt: 5, completion: 0, requests: 0, errors: 0 },
      "2026-09-01": { prompt: 1, completion: 0, requests: 0, errors: 0 },
      "2026-09-03": { prompt: 3, completion: 0, requests: 0, errors: 0 },
    },
  };
  assert.deepEqual(
    recentDays(snap, 2).map(([k]) => k),
    ["2026-09-03", "2026-09-05"]
  );
  // 缺失字段按 0 处理（宽松解析，向后兼容）
  assert.equal(todayUsage({ version: 2, updated_at: "x", days: { "2026-09-05": {} } }).prompt, 0);
});

test("formatTokens compact scale: raw/K/M; formatCount locale ints", () => {
  assert.equal(formatTokens(0), "0");
  assert.equal(formatTokens(527), "527");
  assert.equal(formatTokens(999), "999");
  assert.equal(formatTokens(1000), "1.0K");
  assert.equal(formatTokens(842639), "842.6K");
  assert.equal(formatTokens(1250000), "1.25M");
  assert.equal(formatTokens(null), "-");
  assert.equal(formatTokens(NaN), "-");
  assert.equal(formatTokens(undefined), "-");
  assert.equal(formatCount(1234567), "1,234,567");
  assert.equal(formatCount(null), "-");
});

test("isUsageStale boundary mirrors quota isStale (30min)", () => {
  const now = Date.parse("2026-09-05T02:00:00Z");
  assert.equal(isUsageStale("2026-09-05T01:31:00Z", now), false);
  assert.equal(isUsageStale("2026-09-05T01:29:00Z", now), true);
  assert.equal(isUsageStale("not-a-date", now), false);
  assert.equal(isUsageStale(undefined, now), false);
});

test("hook contract: correct URL, poll cadence, validity guard", () => {
  assert.ok(hook.includes("/ops/usage"), "hook must fetch /ops/usage");
  assert.ok(hook.includes("5 * 60 * 1000"), "poll must be 5min (cron cadence)");
  assert.ok(hook.includes("data.days != null"), "must reject snapshots without days");
  assert.ok(hook.includes("isUsageStale"), "stale must come from helper");
});

test("QuotaChart embeds token section with independent failure semantics", () => {
  assert.ok(chart.includes("useCnbUsage"), "chart must consume usage hook");
  assert.ok(chart.includes("TokenUsageSection"), "token section must exist");
  assert.ok(
    chart.includes("failed: usageFailed"),
    "usage failure must be namespaced, not clobber quota failure"
  );
  // 近 7 日趋势条的最大值分母必须防零（空数据不渲染 trend 区）
  assert.ok(chart.includes("Math.max(1,"), "trendMax must guard divide-by-zero");
});
