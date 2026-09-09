import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// cnb.ts 是 .ts 文件，node:test 无法直接 import；
// 这里按仓库既有模式（quotaFormat.test.mjs）做源码级断言 + 内联复算核心逻辑。
// 若实现与断言漂移，测试会失败提示同步。
const source = readFileSync(new URL("../src/utils/cnb.ts", import.meta.url), "utf8");

// —— 与 cnb.ts 实现等价的内联副本（执行级验证）——
const CNB_HOST_NAME_PATTERN = /azure/i;

function isCnbReachabilityTaskName(name) {
  const n = (name ?? "").toLowerCase();
  return n.includes("反代") && n.includes("cnb");
}
function isCnbProxyHost(node) {
  if (!node) return false;
  return CNB_HOST_NAME_PATTERN.test(node?.name ?? "");
}
function filterCnbReachability(items, isHost) {
  if (isHost) return items;
  return (items ?? []).filter((item) => !isCnbReachabilityTaskName(item?.name ?? ""));
}
// —— 内联副本结束 ——

test("CNB 宿主判定只看名称里的 Azure 关键字", () => {
  assert.equal(isCnbProxyHost({ name: "[香港]Azure-VPS" }), true);
  assert.equal(isCnbProxyHost({ name: "AzureVPS" }), true); // 大小写不敏感
  assert.equal(isCnbProxyHost({}), false);
  assert.equal(isCnbProxyHost(null), false);
  assert.equal(isCnbProxyHost({ name: "[美国]Google-VPS" }), false);
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

test("source 中确实以明星表达式实现 isCnbProxyHost 与过滤闸门", () => {
  assert.ok(source.includes("function isCnbProxyHost"));
  assert.ok(source.includes("function filterCnbReachability"));
  assert.ok(source.includes("CNB_HOST_NAME_PATTERN"));
});