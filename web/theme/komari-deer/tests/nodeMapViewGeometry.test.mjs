import test from "node:test";
import assert from "node:assert/strict";

import { getProjectedWorldMap } from "../src/components/nodeMapViewGeometry.ts";

test("node map world geometry is computed once and reused", () => {
  const first = getProjectedWorldMap();
  const second = getProjectedWorldMap();

  assert.equal(first, second);
  assert.ok(first.spherePath.length > 0);
  assert.ok(first.graticulePath.length > 0);
  assert.ok(first.countries.length > 100);
  assert.ok(first.countries.every((country) => country.pathData.length > 0));
});
