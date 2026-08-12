import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const providerSource = readFileSync(
  new URL("../src/components/providers.tsx", import.meta.url),
  "utf8",
);

const capsuleSource = readFileSync(
  new URL("../src/components/NetworkCapsuleBar.tsx", import.meta.url),
  "utf8",
);
const globalStyles = readFileSync(
  new URL("../src/global.css", import.meta.url),
  "utf8",
);

test("network capsule is mounted as a lightweight global enhancement", () => {
  assert.equal(providerSource.includes("NetworkCapsuleBar"), true);
  assert.equal(providerSource.includes("<NetworkCapsuleBar />"), true);
});

test("network capsule fetches public IP data with timeout and fallback", () => {
  assert.equal(capsuleSource.includes("NETWORK_APIS"), true);
  assert.equal(capsuleSource.includes("AbortController"), true);
  assert.equal(capsuleSource.includes("REQUEST_TIMEOUT"), true);
  assert.equal(capsuleSource.includes("escapeHTML"), true);
  assert.equal(capsuleSource.includes("networkInfo"), true);
});

test("back to top handles nested scroll surfaces", () => {
  assert.equal(capsuleSource.includes("element.scrollTop > 0"), true);
  assert.equal(capsuleSource.includes("document.querySelectorAll"), true);
  assert.equal(capsuleSource.includes("scrollTo({ top: 0, behavior: \"smooth\" })"), true);
});

test("network capsule and back to top have global fixed-position styles", () => {
  assert.equal(globalStyles.includes(".deer-network-capsule"), true);
  assert.equal(globalStyles.includes("deerNetworkCapsuleCycle"), true);
  assert.equal(globalStyles.includes(".deer-back-top"), true);
  assert.equal(globalStyles.includes("bottom: max(86px"), true);
});
