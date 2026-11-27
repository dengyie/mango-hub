import test from "node:test";
import assert from "node:assert/strict";
import {
  parseNodeProjects,
  matchesNodeSearch,
  mapClientRecord,
  buildDashboardCommandItems,
} from "../src/lib/nodeProjects.ts";

test("parseNodeProjects splits comma, Chinese punctuation, semicolon, middle dot, and newlines", () => {
  assert.deepEqual(
    parseNodeProjects("one-mail, OneImage、PanRouter; daily-checkin · social-hub\nlockkey"),
    ["one-mail", "OneImage", "PanRouter", "daily-checkin", "social-hub", "lockkey"]
  );
});

test("parseNodeProjects deduplicates and ignores empty entries", () => {
  assert.deepEqual(parseNodeProjects(" , MangoApi ; CPA ; MangoApi , "), ["MangoApi", "CPA"]);
  assert.deepEqual(parseNodeProjects(""), []);
  assert.deepEqual(parseNodeProjects(null), []);
  assert.deepEqual(parseNodeProjects(undefined), []);
});

test("mapClientRecord preserves public_remark and all dashboard fields", () => {
  const mapped = mapClientRecord(
    {
      uuid: "ab579921-0000-0000-0000-000000000000",
      name: "[中国]上海-腾讯云-轻量服务器",
      cpu_name: "AMD EPYC",
      virtualization: "kvm",
      arch: "amd64",
      cpu_cores: 4,
      os: "Debian 13",
      kernel_version: "6.12",
      gpu_name: "",
      region: "CN",
      mem_total: 4096,
      swap_total: 1024,
      disk_total: 40960,
      group: "prod",
      tags: "allow-ssh-fleet",
      public_remark: "one-mail, OneImage, PanRouter",
      traffic_limit: 500,
      traffic_limit_type: "sum",
    },
    (v) => Number(v) || 0
  );

  assert.equal(mapped.public_remark, "one-mail, OneImage, PanRouter");
  assert.equal(mapped.tags, "allow-ssh-fleet");
  assert.equal(mapped.group, "prod");
  assert.equal(mapped.traffic_limit, 500);

  const missingRemark = mapClientRecord({ uuid: "u2", name: "n2" }, () => 0);
  assert.equal(missingRemark.public_remark, "");
});

test("matchesNodeSearch matches single and multi-token queries across public_remark, tags, group, and status", () => {
  const node = {
    name: "[中国]上海-腾讯云-轻量服务器",
    os: "Debian 13",
    arch: "amd64",
    region: "CN",
    price: 0,
    group: "prod-core",
    tags: "allow-ssh-fleet;ssh_host:10.144.144.1",
    public_remark: "one-mail, OneImage, PanRouter, ai-novel",
  };
  const noRegion = () => false;

  assert.equal(matchesNodeSearch(node, "one-mail", "online", noRegion), true);
  assert.equal(matchesNodeSearch(node, "panrouter", "online", noRegion), true);
  assert.equal(matchesNodeSearch(node, "prod-core", "online", noRegion), true);
  assert.equal(matchesNodeSearch(node, "allow-ssh-fleet", "online", noRegion), true);
  assert.equal(matchesNodeSearch(node, "在线", "online", noRegion), true);
  assert.equal(matchesNodeSearch(node, "上海 one-mail", "online", noRegion), true);
  assert.equal(matchesNodeSearch(node, "在线 panrouter", "online", noRegion), true);
  assert.equal(matchesNodeSearch(node, "离线 panrouter", "online", noRegion), false);
  assert.equal(matchesNodeSearch(node, "non-existent-project", "online", noRegion), false);
});

test("buildDashboardCommandItems groups unique projects, nodes, and quick actions", () => {
  const items = buildDashboardCommandItems(
    [
      { uuid: "u1", name: "tebi", os: "Debian", public_remark: "MangoApi, IP-Pool" },
      { uuid: "u2", name: "huawei", os: "Debian", public_remark: "IP-Pool, slidex" },
    ],
    {
      projects: "Projects",
      nodes: "Nodes",
      actions: "Actions",
      toggleView: "Toggle view",
      showAttention: "Attention",
      showOffline: "Offline",
      showAll: "All",
      resetFilters: "Reset",
    }
  );

  const projPool = items.find((i) => i.id === "proj:IP-Pool");
  assert.ok(projPool);
  assert.equal(projPool.hint, "tebi · huawei");
  assert.equal(projPool.group, "Projects");

  const node1 = items.find((i) => i.id === "node:u1");
  assert.ok(node1);
  assert.equal(node1.label, "tebi");
  assert.equal(node1.hint, "MangoApi, IP-Pool");

  const actionView = items.find((i) => i.id === "action:view");
  assert.ok(actionView);
  assert.equal(actionView.group, "Actions");
});
