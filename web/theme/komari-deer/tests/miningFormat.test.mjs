import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// miningHelper.ts 是 .ts 文件，node:test 无法直接 import；
// 按仓库既有模式（quotaFormat.test.mjs）做源码级断言 + 内联复算核心逻辑。
// 若实现与断言漂移，测试会失败提示同步。
const source = readFileSync(
  new URL("../src/utils/miningHelper.ts", import.meta.url),
  "utf8"
);

// 内联复算 formatHashrate（与 src/utils/miningHelper.ts 一一对应）
function formatHashrate(hs) {
  if (hs == null || !isFinite(hs) || hs <= 0) return "0 H/s";
  const units = ["H/s", "kH/s", "MH/s", "GH/s", "TH/s", "PH/s"];
  let value = hs;
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit++;
  }
  const digits = value >= 100 ? 0 : value >= 10 ? 1 : 2;
  return `${value.toFixed(digits)} ${units[unit]}`;
}

test("source keeps unit ladder and guards in sync with inlined copy", () => {
  assert.ok(source.includes('["H/s", "kH/s", "MH/s", "GH/s", "TH/s", "PH/s"]'), "unit ladder changed");
  assert.ok(source.includes("hs <= 0"), "non-positive guard changed");
  assert.ok(source.includes("!isFinite(hs)"), "NaN/Inf guard changed");
  assert.ok(source.includes("unit < units.length - 1"), "PH ceiling guard changed");
});

test("formatHashrate adapts units across magnitudes", () => {
  // xinyun XEL CPU 矿工实测 ~1094 H/s
  assert.equal(formatHashrate(1094.16), "1.09 kH/s");
  // home-win pearlhash GPU 矿工实测 ~62.4 TH/s
  assert.equal(formatHashrate(62403052604616.36), "62.4 TH/s");
  assert.equal(formatHashrate(62400000000000000), "62.4 PH/s");
});

test("formatHashrate handles zero and invalid input", () => {
  assert.equal(formatHashrate(0), "0 H/s");
  assert.equal(formatHashrate(null), "0 H/s");
  assert.equal(formatHashrate(-5), "0 H/s");
  assert.equal(formatHashrate(NaN), "0 H/s");
});

test("formatHashrate picks digit precision by magnitude", () => {
  assert.equal(formatHashrate(42.7), "42.7 H/s"); // >=10 → 1 位小数
  assert.equal(formatHashrate(999), "999 H/s"); // >=100 → 0 位小数
  assert.equal(formatHashrate(1049), "1.05 kH/s"); // 1.05 → 2 位小数
  assert.equal(formatHashrate(5.25), "5.25 H/s"); // <10 → 2 位小数
});
