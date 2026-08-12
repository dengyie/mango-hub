import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const themeContextSource = readFileSync(
  new URL("../src/contexts/ThemeContext.tsx", import.meta.url),
  "utf8",
);
const providersSource = readFileSync(
  new URL("../src/components/providers.tsx", import.meta.url),
  "utf8",
);

test("theme defaults to dark when no local or managed preference exists", () => {
  assert.equal(themeContextSource.includes('DEFAULT_APPEARANCE: Appearance = "dark"'), true);
  assert.equal(providersSource.includes('defaultTheme="dark"'), true);
});
