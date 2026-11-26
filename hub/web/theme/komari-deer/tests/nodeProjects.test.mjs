import test from "node:test";
import assert from "node:assert/strict";
import { parseNodeProjects, matchesNodeSearch } from "../src/lib/nodeProjects.ts";

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

test("matchesNodeSearch matches deployed projects in public_remark, tags, and group", () => {
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
  assert.equal(matchesNodeSearch(node, "non-existent-project", "online", noRegion), false);
});
