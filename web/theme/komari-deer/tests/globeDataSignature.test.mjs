import test from "node:test";
import assert from "node:assert/strict";

import {
  getEarthStatusSignature,
  getEarthStructuralSignature,
  hasEarthStructuralChange,
} from "../src/components/earth/globeDataSignature.ts";

test("earth globe structural signature ignores live status churn", () => {
  const arcs = [
    { startLat: 1.3521, startLng: 103.8198, endLat: 35.8617, endLng: 104.1954 },
  ];
  const firstPoll = [
    {
      code: "SG",
      lat: 1.3521,
      lng: 103.8198,
      servers: ["Singapore Edge"],
      type: "server",
      size: 0.5,
      online: 1,
      offline: 0,
      status: "online",
    },
  ];
  const secondPoll = [
    {
      ...firstPoll[0],
      online: 0,
      offline: 1,
      status: "offline",
    },
  ];

  assert.equal(
    getEarthStructuralSignature(firstPoll, arcs),
    getEarthStructuralSignature(secondPoll, arcs)
  );
  assert.notEqual(
    getEarthStatusSignature(firstPoll),
    getEarthStatusSignature(secondPoll)
  );
  assert.equal(
    hasEarthStructuralChange(
      getEarthStructuralSignature(firstPoll, arcs),
      secondPoll,
      arcs
    ),
    false
  );
});
