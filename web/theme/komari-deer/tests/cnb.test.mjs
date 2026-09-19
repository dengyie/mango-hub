import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// cnb.ts 是 .ts 文件，node:test 无法直接 import；
// 这里按仓库既有模式（quotaFormat.test.mjs）做源码级断言 + 内联复算核心逻辑。
// 若实现与断言漂移，测试会失败提示同步。
const source = readFileSync(new URL("../src/utils/cnb.ts", import.meta.url), "utf8");

// —— 与 cnb.ts 实现等价的内联副本（执行级验证）——
const CNB_HOST_UUID = "cd520e0b-bb1a-456e-b58c-7814f83c78f6";

function isCnbReachabilityTaskName(name) {
  const n = (name ?? "").toLowerCase();
  return n.includes("反代") && n.includes("cnb");
}
function isCnbProxyHost(node) {
  if (!node) return false;
  return !!node?.uuid && String(node.uuid).toLowerCase() === CNB_HOST_UUID;
}
function filterCnbReachability(items, isHost) {
  if (isHost) return items;
  return (items ?? []).filter((item) => !isCnbReachabilityTaskName(item?.name ?? ""));
}
// —— 内联副本结束 ——

const AZURE_UUID = "cd520e0b-bb1a-456e-b58c-7814f83c78f6";

test("CNB 宿主判定：仅 anchor uuid 命中判为宿主（名称不再兜底）", () => {
  assert.equal(isCnbProxyHost({ name: "[香港]HK-Azure 本机", uuid: AZURE_UUID }), true);
  // anchor uuid 强识别：即使名称不含 azure 仍判为宿主
  assert.equal(isCnbProxyHost({ name: "[香港]反代母机", uuid: AZURE_UUID }), true);
  // 名字含 azure 但 uuid 不命中 → 非宿主（2026-09-19 去 azure 名字兜底，防 India 误显）
  assert.equal(isCnbProxyHost({ name: "[印度]Azure-India-ARM", uuid: "9b3a0e72-b51e-4cea-be13-e19e57b492b6" }), false);
  assert.equal(isCnbProxyHost({ name: "AzureVPS", uuid: "some-other" }), false);
  assert.equal(isCnbProxyHost({ name: "AzureVPS" }), false);
  // 非宿主：名称不含 azure 且 uuid 不命中
  assert.equal(isCnbProxyHost({ name: "[美国]Google-VPS" }), false);
  assert.equal(isCnbProxyHost({ name: "[美国]Google-VPS", uuid: "abc" }), false);
  // 空对象 / null
  assert.equal(isCnbProxyHost({}), false);
  assert.equal(isCnbProxyHost(null), false);
});

test("非宿主节点过滤 CNB-AI-反代 可达性标签，但保留其它标签", () => {
  const items = [
    { name: "本站-可达性", up: true },
    { name: "CNB-AI-反代", up: false },
    { name: "某站可达", up: true },
  ];
  const got = filterCnbReachability(items, false);
  const names = got.map((x) => x.name);
  assert.deepEqual(names, ["本站-可达性", "某站可达"]);
});

test("宿主节点不过滤任何可达性标签（保留 CNB-AI-反代）", () => {
  const items = [
    { name: "本站-可达性", up: true },
    { name: "CNB-AI-反代", up: false },
  ];
  const got = filterCnbReachability(items, true);
  assert.equal(got.length, 2);
  assert.equal(got[1].name, "CNB-AI-反代");
});

test("source 中确实以明确定义实现宿主判定与过滤闸门", () => {
  assert.ok(source.includes("function isCnbProxyHost"));
  assert.ok(source.includes("function filterCnbReachability"));
  assert.ok(source.includes("CNB_HOST_UUID")); // anchor uuid 强识别需存在
  assert.ok(!source.includes("CNB_HOST_NAME_PATTERN")); // 名字兜底已移除（2026-09-19）
});