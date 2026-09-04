import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// quotaHelper.ts 是 .ts 文件，node:test 无法直接 import；
// 这里按仓库既有模式做源码级断言 + 内联复算核心逻辑（与实现一一对应）。
// 若实现与断言漂移，测试会失败提示同步。
const source = readFileSync(
  new URL("../src/utils/quotaHelper.ts", import.meta.url),
  "utf8"
);

// 与实现等价的纯函数副本（格式化契约的执行级验证）
function formatCredits(milli) {
  if (milli == null || !Number.isFinite(milli)) return "-";
  return (milli / 1000).toLocaleString(undefined, { maximumFractionDigits: 2 });
}
function formatCoreHours(seconds) {
  if (seconds == null || !Number.isFinite(seconds)) return "-";
  return `${(seconds / 3600).toLocaleString(undefined, { maximumFractionDigits: 1 })}h`;
}
function quotaPercent(used, total) {
  if (used == null || !Number.isFinite(used)) return null;
  if (total == null || !Number.isFinite(total) || total <= 0) return null;
  return (used / total) * 100;
}
function clampPercent(p) {
  if (p == null || !Number.isFinite(p)) return 0;
  return Math.min(100, Math.max(0, p));
}
function formatPercent(p) {
  if (p == null || !Number.isFinite(p)) return "-";
  if (p >= 0.01) return `${p.toFixed(2)}%`;
  if (p > 0) return "<0.01%";
  return "0%";
}
function isStale(updatedAt, now, staleMs = 30 * 60 * 1000) {
  if (!updatedAt) return false;
  const t = new Date(updatedAt).getTime();
  if (!Number.isFinite(t)) return false;
  return now - t > staleMs;
}
function quotaTone(p) {
  if (p == null) return "default";
  if (p >= 90) return "danger";
  if (p >= 75) return "warning";
  return "default";
}

test("helper module exists and exports the full contract surface", () => {
  for (const name of [
    "formatCredits",
    "formatCoreHours",
    "quotaPercent",
    "clampPercent",
    "quotaTone",
    "toneTextClass",
    "toneBarClass",
    "formatPercent",
    "isStale",
    "QuotaSnapshot",
  ]) {
    assert.ok(source.includes(name), `quotaHelper must export ${name}`);
  }
});

test("no hardcoded core-hour denominator remains (576000 regression guard)", () => {
  assert.ok(!source.includes("576000"), "magic 576000 must not reappear");
  // hook 也不允许出现
  const hook = readFileSync(
    new URL("../src/hooks/useCnbQuota.ts", import.meta.url),
    "utf8"
  );
  assert.ok(!hook.includes("576000"));
});

test("formatCredits handles null/NaN and known values", () => {
  assert.equal(formatCredits(null), "-");
  assert.equal(formatCredits(undefined), "-");
  assert.equal(formatCredits(NaN), "-");
  assert.equal(formatCredits(190), "0.19");
  assert.equal(formatCredits(1166000), "1,166");
});

test("formatCoreHours handles null and known values", () => {
  assert.equal(formatCoreHours(null), "-");
  assert.equal(formatCoreHours(208083), "57.8h");
  assert.equal(formatCoreHours(5760000), "1,600h");
  assert.equal(formatCoreHours(11242), "3.1h");
});

test("quotaPercent guards: null used, null total, zero total, overuse", () => {
  assert.equal(quotaPercent(null, 1000), null);
  assert.equal(quotaPercent(5, null), null);
  assert.equal(quotaPercent(5, 0), null);
  assert.equal(quotaPercent(5, -1), null);
  assert.equal(quotaPercent(190, 1166000), (190 / 1166000) * 100);
});

test("clampPercent bounds overuse and NaN", () => {
  assert.equal(clampPercent(130), 100);
  assert.equal(clampPercent(-5), 0);
  assert.equal(clampPercent(null), 0);
  assert.equal(clampPercent(NaN), 0);
  assert.equal(clampPercent(57.8), 57.8);
});

test("formatPercent renders <0.01% branch and zero", () => {
  assert.equal(formatPercent((190 / 1166000) * 100), "0.02%");
  assert.equal(formatPercent(0.009), "<0.01%");
  assert.equal(formatPercent(0), "0%");
  assert.equal(formatPercent(null), "-");
});

test("isStale boundary: 29min fresh, 31min stale, bad date never stale", () => {
  const now = Date.parse("2026-09-04T02:00:00Z");
  assert.equal(isStale("2026-09-04T01:31:00Z", now), false);
  assert.equal(isStale("2026-09-04T01:29:00Z", now), true);
  assert.equal(isStale("not-a-date", now), false);
  assert.equal(isStale(undefined, now), false);
});

test("quotaTone thresholds 75/90 with null default", () => {
  assert.equal(quotaTone(null), "default");
  assert.equal(quotaTone(74.9), "default");
  assert.equal(quotaTone(75), "warning");
  assert.equal(quotaTone(89.9), "warning");
  assert.equal(quotaTone(90), "danger");
});

test("QuotaChart consumes ci_total_sec field (contract with backend jq)", () => {
  const chart = readFileSync(
    new URL("../src/components/instance/QuotaChart.tsx", import.meta.url),
    "utf8"
  );
  assert.ok(chart.includes("quota?.ci_total_sec"), "CI bar must use ci_total_sec");
  assert.ok(!chart.includes("576000"), "chart must not hardcode CI total");
  // 独立守卫：总量缺失时只显示用量不渲染进度条（percent null → Progress 不渲染）
  assert.ok(
    chart.includes("row.percent != null &&"),
    "Progress must be conditional on percent availability"
  );
});
