#!/usr/bin/env node
'use strict';

const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..', '..');
const distDir = path.resolve(repoRoot, process.argv[2] || 'dist');

const targets = [
  { os: 'darwin', arch: 'amd64', packageDir: 'npm/platforms/darwin-x64' },
  { os: 'darwin', arch: 'arm64', packageDir: 'npm/platforms/darwin-arm64' },
  { os: 'linux', arch: 'amd64', packageDir: 'npm/platforms/linux-x64' },
  { os: 'linux', arch: 'arm64', packageDir: 'npm/platforms/linux-arm64' },
];

const packageDirs = [
  'npm/mthc',
  ...targets.map((target) => target.packageDir),
];

function fail(message) {
  console.error(`error: ${message}`);
  process.exit(1);
}

function walkFiles(dir) {
  const entries = fs.readdirSync(dir, { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    const entryPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...walkFiles(entryPath));
    } else if (entry.isFile()) {
      files.push(entryPath);
    }
  }

  return files;
}

function targetTokens(target) {
  return [`${target.os}_${target.arch}`, `${target.os}-${target.arch}`];
}

if (!fs.existsSync(distDir) || !fs.statSync(distDir).isDirectory()) {
  fail(`dist directory not found: ${distDir}`);
}

const distFiles = walkFiles(distDir);
const targetMatches = new Map();

for (const target of targets) {
  const tokens = targetTokens(target);
  const matches = distFiles.filter((filePath) => {
    return path.basename(filePath) === 'mthc' && tokens.some((token) => filePath.includes(token));
  });

  if (matches.length !== 1) {
    fail(`expected exactly one mthc binary for ${target.os}/${target.arch}, found ${matches.length}`);
  }

  targetMatches.set(target, matches[0]);
}

for (const packageDir of packageDirs) {
  for (const fileName of ['README.md', 'LICENSE']) {
    fs.copyFileSync(path.join(repoRoot, fileName), path.join(repoRoot, packageDir, fileName));
  }
}

for (const target of targets) {
  const binDir = path.join(repoRoot, target.packageDir, 'bin');
  const destination = path.join(binDir, 'mthc');

  fs.mkdirSync(binDir, { recursive: true });
  fs.copyFileSync(targetMatches.get(target), destination);
  fs.chmodSync(destination, 0o755);
}
