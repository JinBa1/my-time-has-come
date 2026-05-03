"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const { packageForTarget } = require("./mthc.js");

test("packageForTarget maps supported platform and architecture pairs", () => {
  assert.equal(packageForTarget("linux", "x64"), "@jinba1/mthc-linux-x64");
  assert.equal(packageForTarget("linux", "arm64"), "@jinba1/mthc-linux-arm64");
  assert.equal(packageForTarget("darwin", "x64"), "@jinba1/mthc-darwin-x64");
  assert.equal(packageForTarget("darwin", "arm64"), "@jinba1/mthc-darwin-arm64");
});

test("packageForTarget rejects unsupported platform and architecture pairs", () => {
  assert.throws(() => packageForTarget("win32", "x64"), /Unsupported platform/);
  assert.throws(() => packageForTarget("linux", "arm"), /Unsupported platform/);
});
