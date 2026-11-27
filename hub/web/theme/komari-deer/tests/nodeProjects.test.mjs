import test from "node:test";
import assert from "node:assert/strict";
import { parseNodeProjects, matchesNodeSearch, mapClientRecord } from "../src/lib/nodeProjects.ts";

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
