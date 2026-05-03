#!/usr/bin/env node
"use strict";

const { spawnSync } = require("node:child_process");
const path = require("node:path");

const PACKAGES = {
  "darwin:arm64": "@jinba1/mthc-darwin-arm64",
  "darwin:x64": "@jinba1/mthc-darwin-x64",
  "linux:arm64": "@jinba1/mthc-linux-arm64",
  "linux:x64": "@jinba1/mthc-linux-x64"
};

function packageForTarget(platform, arch) {
  const packageName = PACKAGES[`${platform}:${arch}`];
  if (!packageName) {
    throw new Error(`Unsupported platform: ${platform}/${arch}`);
  }
  return packageName;
}

function binaryPathForPackage(packageName) {
  const packageJsonPath = require.resolve(`${packageName}/package.json`);
  return path.join(path.dirname(packageJsonPath), "bin", "mthc");
}

function main() {
  let packageName;
  let binaryPath;

  try {
    packageName = packageForTarget(process.platform, process.arch);
  } catch (error) {
    console.error(`mthc: ${error.message}`);
    console.error("mthc: this npm package supports linux and macOS on x64 and arm64.");
    process.exit(1);
  }

  try {
    binaryPath = binaryPathForPackage(packageName);
  } catch (error) {
    console.error(`mthc: unable to find native package ${packageName}.`);
    console.error("mthc: reinstall with optional dependencies enabled, then run `mthc` again.");
    process.exit(1);
  }

  const result = spawnSync(binaryPath, process.argv.slice(2), {
    stdio: "inherit",
    env: {
      ...process.env,
      MTHC_INSTALL_COMMAND: process.env.MTHC_INSTALL_COMMAND || "mthc"
    }
  });

  if (result.error) {
    console.error(`mthc: failed to run native binary: ${result.error.message}`);
    process.exit(1);
  }

  if (result.signal) {
    process.kill(process.pid, result.signal);
    process.exit(1);
  }

  process.exit(result.status === null ? 1 : result.status);
}

if (require.main === module) {
  main();
}

module.exports = {
  packageForTarget,
  binaryPathForPackage
};
