import test from "node:test";
import assert from "node:assert/strict";

import {
  buildEarthGlobeData,
  resolveEarthCountryCode,
} from "../src/components/earth/earthData.ts";

test("buildEarthGlobeData groups nodes by country and connects them to the user", () => {
  const nodes = [
    { uuid: "sg-1", name: "Singapore A", region: "🇸🇬" },
    { uuid: "sg-2", name: "Singapore B", region: "SG" },
    { uuid: "us-1", name: "United States A", region: "US" },
    { uuid: "bad-1", name: "Unknown Region", region: "Nowhere" },
  ];

  const result = buildEarthGlobeData({
    nodes,
    liveData: { online: ["sg-1", "us-1"], data: {} },
    userGeo: {
      lat: 1.3521,
      lng: 103.8198,
      city: "Singapore",
    },
  });

  assert.equal(resolveEarthCountryCode("🇸🇬"), "SG");
  assert.equal(result.totalCount, 3);
  assert.equal(result.pointsData.length, 3);
  assert.equal(result.arcsData.length, 2);

  const singapore = result.pointsData.find((point) => point.code === "SG");
  assert.ok(singapore);
  assert.equal(singapore.type, "server");
  assert.deepEqual(singapore.servers, ["Singapore A", "Singapore B"]);
  assert.equal(singapore.online, 1);
  assert.equal(singapore.offline, 1);
  assert.equal(singapore.status, "partial");

  const user = result.pointsData.find((point) => point.type === "user");
  assert.ok(user);
  assert.equal(user.code, "USER");
  assert.deepEqual(user.servers, ["Singapore"]);
});

test("buildEarthGlobeData treats nodes as online when live status is absent", () => {
  const result = buildEarthGlobeData({
    nodes: [
      { uuid: "sg-1", name: "Singapore Edge", region: "SG" },
      { uuid: "sg-2", name: "Singapore Backup", region: "SG" },
    ],
    userGeo: {
      lat: 1.3521,
      lng: 103.8198,
      city: "Singapore",
    },
  });

  const singapore = result.pointsData.find((point) => point.code === "SG");
  assert.ok(singapore);
  assert.equal(singapore.online, 2);
  assert.equal(singapore.offline, 0);
  assert.equal(singapore.status, "online");
});
