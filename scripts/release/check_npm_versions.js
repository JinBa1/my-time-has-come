#!/usr/bin/env node
'use strict';

const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..', '..');
const tag = process.argv[2];
const tagPattern = /^v\d+\.\d+\.\d+$/;

const platformPackagePaths = [
  'npm/platforms/darwin-arm64/package.json',
  'npm/platforms/darwin-x64/package.json',
  'npm/platforms/linux-arm64/package.json',
  'npm/platforms/linux-x64/package.json',
];

function fail(message) {
  console.error(`error: ${message}`);
  process.exit(1);
}

function readPackage(relativePath) {
  const packagePath = path.join(repoRoot, relativePath);

  try {
    return JSON.parse(fs.readFileSync(packagePath, 'utf8'));
  } catch (error) {
    fail(`failed to read ${relativePath}: ${error.message}`);
  }
}

if (!tag || !tagPattern.test(tag)) {
  fail('tag must look like v0.2.0');
}

const version = tag.slice(1);
const rootPackage = readPackage('npm/mthc/package.json');

if (rootPackage.version !== version) {
  fail(`npm/mthc/package.json version ${rootPackage.version} does not match ${version}`);
}

for (const relativePath of platformPackagePaths) {
  const platformPackage = readPackage(relativePath);

  if (platformPackage.version !== version) {
    fail(`${relativePath} version ${platformPackage.version} does not match ${version}`);
  }

  const optionalVersion = rootPackage.optionalDependencies?.[platformPackage.name];
  if (optionalVersion !== version) {
    fail(`npm/mthc/package.json optionalDependencies[${platformPackage.name}] ${optionalVersion} does not match ${version}`);
  }
}

console.log(`npm versions match ${tag}`);
