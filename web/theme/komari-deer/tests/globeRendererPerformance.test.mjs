import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const rendererSource = readFileSync(
  new URL("../src/components/earth/GlobeRenderer.tsx", import.meta.url),
  "utf8"
);

test("earth globe renderer does not force preserveDrawingBuffer", () => {
  assert.equal(rendererSource.includes("preserveDrawingBuffer"), false);
});

test("earth globe renderer uses purcarte-plus constructor initialization", () => {
  assert.equal(rendererSource.includes("new (Globe as any)"), true);
  assert.equal(rendererSource.includes("(Globe as any)()(canvas)"), false);
});

test("earth globe renderer uses static country polygons for glow outlines", () => {
  assert.equal(rendererSource.includes("getEarthCountryPolygons"), true);
  assert.equal(rendererSource.includes(".polygonsData(countryPolygons)"), true);
  assert.equal(rendererSource.includes("new THREE.MeshBasicMaterial"), true);
  assert.equal(rendererSource.includes("transparent: true"), true);
  assert.equal(rendererSource.includes(".polygonCapMaterial("), true);
  assert.equal(rendererSource.includes(".polygonCapColor("), false);
  assert.equal(rendererSource.includes(".polygonSideColor("), false);
  assert.equal(rendererSource.includes(".polygonSideMaterial("), false);
  assert.equal(rendererSource.includes("fetch("), false);
});

test("earth globe country outlines use coarse local topology", () => {
  const polygonSource = readFileSync(
    new URL("../src/components/earth/earthCountryPolygons.ts", import.meta.url),
    "utf8",
  );

  assert.equal(polygonSource.includes("world-countries-110m.json"), true);
  assert.equal(polygonSource.includes("world-countries-50m.json"), false);
});
