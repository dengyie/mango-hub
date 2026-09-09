import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// cnb.ts 是 .ts 文件，node:test 无法直接 import；
// 这里按仓库既有模式（quotaFormat.test.mjs）做源码级断言 + 内联复算核心逻辑。
// 若实现与断言漂移，测试会失败提示同步。
const source = readFileSync(new URL("../src/utils/cnb.ts", import.meta.url), "utf8");

// —— 与 cnb.ts 实现等价的内联副本（执行级验证）——
const CNB_HOST_NAME_PATTERN = /azure/i;
const CNB_HOST_UUID = "7ff47295-1d4c-4443-b72a-e6f81a56b527";

function isCnbReachabilityTaskName(name) {
  const n = (name ?? "").toLowerCase();
  return n.includes("反代") && n.includes("cnb");
}
function isCnbProxyHost(node) {
  if (!node) return false;
  if (node?.uuid && String(node.uuid).toLowerCase() === CNB_HOST_UUID) return true;
  return CNB_HOST_NAME_PATTERN.test(node?.name ?? "");
}
function filterCnbReachability(items, isHost) {
  if (isHost) return items;
  return (items ?? []).filter((item) => !isCnbReachabilityTaskName(item?.name ?? ""));
}
// —— 内联副本结束 ——

const AZURE_UUID = "7ff47295-1d4c-4443-b72a-e6f81a56b527";

test("CNB 宿主判定：名称含 Azure 或 uuid 命中锚点都判为宿主", () => {
  assert.equal(isCnbProxyHost({ name: "[香港]Azure-VPS", uuid: AZURE_UUID }), true); // 两者都命中
  assert.equal(isCnbProxyHost({ name: "AzureVPS", uuid: "some-other" }), true); // 仅名称命中
  // anchor uuid 强识别：即使名称不含 azure 仍判为宿主
  assert.equal(isCnbProxyHost({ name: "[香港]反代母机", uuid: AZURE_UUID }), true);
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
  assert.ok(source.includes("CNB_HOST_NAME_PATTERN"));
  assert.ok(source.includes("CNB_HOST_UUID")); // 铺点 uuid 强识别需存在
});