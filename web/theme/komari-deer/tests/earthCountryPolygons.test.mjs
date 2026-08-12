import test from "node:test";
import assert from "node:assert/strict";

import { getEarthCountryPolygons } from "../src/components/earth/earthCountryPolygons.ts";

function ringArea(ring) {
  let area = 0;

  for (let i = 0, previous = ring.length - 1; i < ring.length; previous = i, i += 1) {
    area += ring[previous][0] * ring[i][1] - ring[i][0] * ring[previous][1];
  }

  return area / 2;
}

function polygonRings(geometry) {
  if (geometry.type === "Polygon") {
    return [geometry.coordinates];
  }

  if (geometry.type === "MultiPolygon") {
    return geometry.coordinates;
  }

  return [];
}

test("earth country polygons are resolved from cached local topology", () => {
  const first = getEarthCountryPolygons(["US", "SG", "HK", "NOPE"]);
  const second = getEarthCountryPolygons(["US", "SG", "HK", "NOPE"]);

  assert.equal(first, second);
  assert.deepEqual(
    first.map((polygon) => polygon.code).sort(),
    ["HK", "SG", "US"],
  );
  assert.ok(first.every((polygon) => polygon.geometry));
  assert.deepEqual(
    first
      .filter((polygon) => polygon.synthetic)
      .map((polygon) => polygon.code)
      .sort(),
    ["HK", "SG"],
  );
});

test("earth country polygon fill rings keep three-globe winding", () => {
  const polygons = getEarthCountryPolygons(["US", "JP", "SG"]);

  polygons.forEach((polygon) => {
    polygonRings(polygon.geometry).forEach((rings) => {
      assert.ok(ringArea(rings[0]) < 0, `${polygon.code} exterior ring should fill inside`);

      rings.slice(1).forEach((ring) => {
        assert.ok(ringArea(ring) > 0, `${polygon.code} interior ring should cut a hole`);
      });
    });
  });
});
