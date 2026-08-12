import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const providerSource = readFileSync(
  new URL("../src/contexts/RPC2Context.tsx", import.meta.url),
  "utf8"
);

test("rpc2 provider avoids websocket retry loops during polling mode", () => {
  assert.equal(providerSource.includes("autoConnect: false"), true);
  assert.equal(providerSource.includes("autoReconnect: false"), true);
});
