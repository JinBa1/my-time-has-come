"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const { installCommandForEnv, packageForTarget } = require("./mthc.js");

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

test("installCommandForEnv preserves an existing environment override", () => {
  assert.equal(
    installCommandForEnv({ MTHC_INSTALL_COMMAND: "/custom/mthc" }, ["node", "/ignored/mthc"]),
    "/custom/mthc"
  );
});

test("installCommandForEnv uses argv[1] when no environment override is set", () => {
  assert.equal(installCommandForEnv({}, ["node", "/usr/local/bin/mthc"]), "/usr/local/bin/mthc");
});

test("installCommandForEnv falls back to mthc when argv[1] is missing", () => {
  assert.equal(installCommandForEnv({}, ["node"]), "mthc");
});
