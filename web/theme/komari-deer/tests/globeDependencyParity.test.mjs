import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const lock = JSON.parse(
  readFileSync(new URL("../package-lock.json", import.meta.url), "utf8")
);

function lockedVersion(packagePath) {
  return lock.packages?.[packagePath]?.version;
}

test("earth globe dependencies match purcarte-plus locked renderer versions", () => {
  assert.equal(lockedVersion("node_modules/globe.gl"), "2.45.0");
  assert.equal(lockedVersion("node_modules/three-globe"), "2.45.0");
  assert.equal(lockedVersion("node_modules/three-render-objects"), "1.40.4");
});
