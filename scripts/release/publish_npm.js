#!/usr/bin/env node
'use strict';

const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..', '..');
const packageDirs = [
  'npm/platforms/darwin-arm64',
  'npm/platforms/darwin-x64',
  'npm/platforms/linux-arm64',
  'npm/platforms/linux-x64',
  'npm/mthc',
];

function fail(message) {
  console.error(`error: ${message}`);
  process.exit(1);
}

function readPackage(packageDir) {
  const packagePath = path.join(repoRoot, packageDir, 'package.json');

  try {
    return JSON.parse(fs.readFileSync(packagePath, 'utf8'));
  } catch (error) {
    fail(`failed to read ${packagePath}: ${error.message}`);
  }
}

function isPublished(name, version) {
  const result = spawnSync('npm', ['view', `${name}@${version}`, 'version'], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: process.env,
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  if (result.status === 0 && result.stdout.trim() === version) {
    return true;
  }

  return false;
}

for (const packageDir of packageDirs) {
  const packageJson = readPackage(packageDir);

  if (isPublished(packageJson.name, packageJson.version)) {
    console.log(`skip published ${packageJson.name}@${packageJson.version}`);
    continue;
  }

  const args = ['publish'];
  if (packageJson.name.startsWith('@')) {
    args.push('--access', 'public');
  }

  const result = spawnSync('npm', args, {
    cwd: path.join(repoRoot, packageDir),
    env: process.env,
    stdio: 'inherit',
  });

  if (result.status !== 0) {
    fail(`npm publish failed for ${packageJson.name}@${packageJson.version}`);
  }
}
