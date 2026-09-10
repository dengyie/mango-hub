import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// usePingStats.tsx 是 .tsx 文件，node:test 无法直接 import；
// 按仓库既有模式（cnb.test.mjs）做源码级断言 + 内联复算核心逻辑。
// 若实现与断言漂移，测试会失败提示同步。
const source = readFileSync(
  new URL("../src/hooks/usePingStats.tsx", import.meta.url),
  "utf8"
);
const nodeSource = readFileSync(
  new URL("../src/components/Node.tsx", import.meta.url),
  "utf8"
);

// —— 与 usePingStats.tsx httpReachability 计算等价的内联副本 ——
function computeHttpReachability(records, httpTasks) {
  return httpTasks.map((task) => {
    let up = null;
    let newestTs = -Infinity;
    let samples = 0;
    let upSamples = 0;
    for (const record of records) {
      if (record.task_id !== task.id) continue;
      const reachable = record.value >= 0;
      samples += 1;
      if (reachable) upSamples += 1;
      const ts = new Date(record.time).getTime();
      if (Number.isFinite(ts) && ts > newestTs) {
        newestTs = ts;
        up = reachable;
      }
    }
    const availability = samples > 0 ? (upSamples / samples) * 100 : null;
    return { name: task.name, up, availability };
  });
}

test("availability = 窗口内 up 采样占比", () => {
  const task = { id: 7, name: "CNB-AI-反代" };
  const records = [
    { task_id: 7, time: "2026-09-09T22:00:00Z", value: 120 },
    { task_id: 7, time: "2026-09-09T22:01:00Z", value: -1 }, // 不可达
    { task_id: 7, time: "2026-09-09T22:02:00Z", value: 118 },
    { task_id: 7, time: "2026-09-09T22:03:00Z", value: -1 }, // 不可达
  ];
  const [h] = computeHttpReachability(records, [task]);
  assert.equal(h.samples === undefined ? h.availability : h.availability, 50); // 2/4
  assert.equal(h.up, false); // 最新一条不可达
});

test("全部可达 → 100%，up=true；无采样 → availability=null, up=null", () => {
  const all = computeHttpReachability(
    [
      { task_id: 6, time: "2026-09-09T22:00:00Z", value: 20 },
      { task_id: 6, time: "2026-09-09T22:01:00Z", value: 21 },
    ],
    [{ id: 6, name: "本站-可达性" }],
  );
  assert.equal(all[0].availability, 100);
  assert.equal(all[0].up, true);

  const none = computeHttpReachability([], [{ id: 9, name: "任意" }]);
  assert.equal(none[0].availability, null);
  assert.equal(none[0].up, null);
});

test("只统计本任务的采样（task_id 过滤）", () => {
  const [h] = computeHttpReachability(
    [
      { task_id: 6, time: "2026-09-09T22:00:00Z", value: -1 }, // 其它任务的失败，不影响任务 7
      { task_id: 7, time: "2026-09-09T22:00:00Z", value: 90 },
    ],
    [{ id: 7, name: "CNB-AI-反代" }],
  );
  assert.equal(h.availability, 100);
});

test("源码同步：hook 计算 availability，Node tag 渲染百分比并按阈值着色", () => {
  assert.ok(
    source.includes("upSamples / samples") && source.includes("availability"),
    "usePingStats must compute availability from window samples"
  );
  assert.ok(
    nodeSource.includes("h.availability.toFixed(2)"),
    "node tag must render availability percentage"
  );
  assert.ok(
    nodeSource.includes("99.9") && nodeSource.includes("95"),
    "availability color thresholds (green>=99.9, orange>=95, red below) changed"
  );
});
