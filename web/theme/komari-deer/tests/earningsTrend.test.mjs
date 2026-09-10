import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// EarningsCard.tsx 是 .tsx 文件，node:test 无法直接 import；
// 按仓库既有模式做源码级断言 + 内联复算 24h 趋势基准选取逻辑。
// 若实现与断言漂移，测试会失败提示同步。
const source = readFileSync(
  new URL("../src/components/EarningsCard.tsx", import.meta.url),
  "utf8"
);

// —— 与 EarningsCard 24h trend 等价的内联副本 ——
function pickBase(hourly) {
  const last = hourly[hourly.length - 1];
  const lastMs = Date.parse(last.hour_utc);
  const cutoff = lastMs - 24 * 60 * 60 * 1000;
  return hourly.find((h) => Date.parse(h.hour_utc) >= cutoff) ?? hourly[0];
}
function trend24h(hourly) {
  if (!hourly || hourly.length <= 1) return 0;
  const last = hourly[hourly.length - 1];
  const base = pickBase(hourly);
  return Math.max(0, last.total_btc - base.total_btc);
}

test("EarningsCard uses timestamp-based 24h trend base (gap-safe)", () => {
  // 实现必须按时间戳选基准，不得按条目数（数组下标）
  assert.match(source, /hour_utc.*cutoff|cutoff.*hour_utc/s,
    "base selection must reference hour_utc timestamps");
  assert.ok(!/length\s*-\s*25/.test(source),
    "index-based base (length-25) must not be used; gaps make the window > 24h");
});

test("trend window: continuous hourly series picks sample >= last-24h", () => {
  const hourly = Array.from({ length: 30 }, (_, i) => ({
    hour_utc: new Date(Date.UTC(2026, 8, 10, 20, 0, 0) - (29 - i) * 3600e3)
      .toISOString(),
    total_btc: 0.00001 + i * 1e-8,
  }));
  const base = pickBase(hourly);
  const lastMs = Date.parse(hourly[29].hour_utc);
  assert.ok(Date.parse(base.hour_utc) >= lastMs - 24 * 3600e3,
    "base must be within last 24h");
  // 连续序列：基准是 25 小时前的下一条（即 24 小时前那条）
  assert.equal(hourly.indexOf(base), 5);
  const got = trend24h(hourly);
  const want = (0.00001 + 29e-8) - (0.00001 + 5e-8);
  assert.ok(Math.abs(got - want) < 1e-12, `trend = ${got}, want ${want}`);
});

test("trend window: gapped series must not stretch beyond 24h", () => {
  // 中间缺 6 小时（轮询失败/重启）：按旧的下标逻辑第 25 条是 31h 前，
  // 新逻辑应取 24h 窗口内最早的一条。
  const stamps = [
    ...Array.from({ length: 12 }, (_, i) =>
      new Date(Date.UTC(2026, 8, 9, 8, 0, 0) + i * 3600e3).toISOString()),
    // 空洞：跳过 8 小时
    ...Array.from({ length: 10 }, (_, i) =>
      new Date(Date.UTC(2026, 8, 10, 4, 0, 0) + i * 3600e3).toISOString()),
  ];
  const hourly = stamps.map((hour_utc, i) => ({
    hour_utc,
    total_btc: 0.00001 + i * 1e-8,
  }));
  const lastMs = Date.parse(hourly[hourly.length - 1].hour_utc);
  const base = pickBase(hourly);
  assert.ok(Date.parse(base.hour_utc) >= lastMs - 24 * 3600e3,
    `base ${base.hour_utc} must be within last 24h (window must not stretch across the gap)`);
  // 增速只累计 24h 窗口内的增长
  const got = trend24h(hourly);
  const baseIdx = hourly.indexOf(base);
  const want = (0.00001 + 21e-8) - (0.00001 + baseIdx * 1e-8);
  assert.ok(Math.abs(got - want) < 1e-12, `trend = ${got}, want ${want}`);
});

test("trend clamps to zero when totals regress", () => {
  const hourly = [
    { hour_utc: "2026-09-10T18:00:00Z", total_btc: 0.00005 },
    { hour_utc: "2026-09-10T19:00:00Z", total_btc: 0.00004 },
  ];
  assert.equal(trend24h(hourly), 0);
});
