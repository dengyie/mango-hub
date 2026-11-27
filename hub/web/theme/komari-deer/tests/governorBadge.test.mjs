import test from "node:test";
import assert from "node:assert/strict";
import { governorBadge, miningStatusPill, GOVERNOR_TONE_CLASS } from "../src/utils/miningHelper.ts";
import { normalizeLiveData } from "../src/lib/liveData.ts";

test("governorBadge maps the three power_governor tiers to tones", () => {
  assert.deepEqual(governorBadge({ tier: "full" }), { tier: "full", tone: "ok", i18nKey: "mining.govFull" });
  assert.deepEqual(governorBadge({ tier: "eco" }), { tier: "eco", tone: "warn", i18nKey: "mining.govEco" });
  assert.deepEqual(governorBadge({ tier: "pause" }), { tier: "pause", tone: "alert", i18nKey: "mining.govPause" });
});

test("governorBadge is null without a tier and degrades unknown tiers", () => {
  assert.equal(governorBadge(null), null);
  assert.equal(governorBadge(undefined), null);
  assert.equal(governorBadge({}), null);
  assert.equal(governorBadge({ tier: "   " }), null);
  assert.deepEqual(governorBadge({ tier: "FULL" }), { tier: "full", tone: "ok", i18nKey: "mining.govFull" });
  assert.deepEqual(governorBadge({ tier: "weird" }), { tier: "weird", tone: "muted", i18nKey: "mining.govUnknown" });
});

test("GOVERNOR_TONE_CLASS covers every tone", () => {
  for (const tone of ["ok", "warn", "alert", "muted"]) {
    assert.equal(typeof GOVERNOR_TONE_CLASS[tone], "string");
    assert.ok(GOVERNOR_TONE_CLASS[tone].length > 0);
  }
});

test("miningStatusPill carries the governor badge without touching legacy fields", () => {
  const pill = miningStatusPill({ algorithm: "pearlhash", hashrate_1min: 62.4e12, governor: { tier: "eco" } });
  assert.ok(pill);
  assert.equal(pill.governor?.tier, "eco");
  assert.equal(pill.governor?.tone, "warn");
  assert.equal(pill.metric, "62.4 TH/s");

  const legacy = miningStatusPill({ algorithm: "xelishashv3", hashrate_1min: 1094.16 });
  assert.equal(legacy?.governor, undefined);
  assert.equal(legacy?.metric, "1.09 kH/s");
});

test("miningStatusPill never fabricates 0 H/s from a missing hashrate", () => {
  // Number(null) === 0 会把「无算力字段」误判成「矿工在跑但 0 H/s」——必须按类型判定。
  assert.equal(miningStatusPill({ governor: { tier: "pause" } }), null);
  assert.equal(miningStatusPill({ hashrate_1min: null, governor: { tier: "pause" } }), null);
  assert.equal(miningStatusPill({ hashrate_1min: undefined, algorithm: "" }), null);
  assert.equal(miningStatusPill(null), null);
  // 真实上报 0(矿工在跑但没算力)仍要显示,别把有效状态也吞掉。
  const stalled = miningStatusPill({ algorithm: "pearlhash", hashrate_1min: 0 });
  assert.equal(stalled?.metric, "0 H/s");
});

const liveBase = {
  online: true,
  cpu: 5, ram: 10, swap: 0, disk: 1,
  load: 0, load5: 0, load15: 0,
  net_out: 1, net_in: 2, net_total_out: 3, net_total_in: 4,
  connections: 1, connections_udp: 0,
  uptime: 100, process: 5,
  time: "2026-11-27T01:30:15+08:00",
};

test("normalizeLiveData keeps the governor block and coerces numbers", () => {
  const res = normalizeLiveData({
    "home-win": {
      ...liveBase,
      mining: {
        algorithm: "pearlhash",
        hashrate_1min: 62.4e12,
        governor: { tier: "eco", idle_sec: 7, gpu_pct: 5.25 },
      },
    },
  });
  const mining = res.data.data["home-win"].mining;
  assert.ok(mining);
  assert.deepEqual(mining.governor, { tier: "eco", idle_sec: 7, gpu_pct: 5.25 });
});

test("normalizeLiveData drops governor when tier is absent or not a string", () => {
  for (const governor of [undefined, {}, { idle_sec: 3 }, { tier: 42 }]) {
    const res = normalizeLiveData({
      n2: { ...liveBase, mining: { algorithm: "pearlhash", hashrate_1min: 1e12, governor } },
    });
    assert.equal(res.data.data.n2.mining?.governor, undefined);
  }
});

test("normalizeLiveData keeps mining usable when the governor block is malformed", () => {
  const res = normalizeLiveData({
    n3: { ...liveBase, mining: { algorithm: "pearlhash", hashrate_1min: 1e12, governor: { tier: "eco", idle_sec: "x", gpu_pct: -1 } } },
  });
  const mining = res.data.data.n3.mining;
  assert.equal(mining?.governor?.tier, "eco");
  // 非法数值按 0 处理, 不产生 NaN 污染图表/徽标
  assert.equal(mining?.governor?.idle_sec, 0);
  assert.equal(mining?.governor?.gpu_pct, 0);
});

// 回归:pause 档会停掉矿工服务,后端只发 {mining:{governor:{tier:"pause"}}}。
// 归一化不能把它当「没有 mining」丢掉(否则徽标消失),也不能缺字段就渲染假 pill。
test("governor-only mining block survives normalization without a fake pill", () => {
  const res = normalizeLiveData({
    paused: { ...liveBase, mining: { governor: { tier: "pause" } } },
  });
  const mining = res.data.data.paused.mining;
  assert.ok(mining, "mining block must survive so the governor badge can render");
  assert.equal(mining.algorithm, undefined);
  assert.equal(mining.hashrate_1min, undefined);
  assert.equal(mining.governor?.tier, "pause");
  assert.equal(miningStatusPill(mining), null, "pause must not fabricate a 'Mining · 0 H/s' pill");
  assert.equal(governorBadge(mining.governor)?.tone, "alert");
});
