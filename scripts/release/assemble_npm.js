#!/usr/bin/env node
'use strict';

const fs = require('node:fs');
const crypto = require('node:crypto');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..', '..');
const distDir = path.resolve(repoRoot, process.argv[2] || 'dist');
const manifestPath = path.join(distDir, 'npm-assembly.json');

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

function normalizePath(filePath) {
  return filePath.split(path.sep).join('/');
}

function readPackage(relativePath) {
  const packagePath = path.join(repoRoot, relativePath);

  try {
    return JSON.parse(fs.readFileSync(packagePath, 'utf8'));
  } catch (error) {
    fail(`failed to read ${relativePath}: ${error.message}`);
  }
}

function fileMetadata(filePath) {
  const contents = fs.readFileSync(filePath);

  return {
    sha256: crypto.createHash('sha256').update(contents).digest('hex'),
    size: contents.length,
  };
}

function removeIfExists(filePath) {
  fs.rmSync(filePath, { force: true, recursive: true });
}

if (!fs.existsSync(distDir) || !fs.statSync(distDir).isDirectory()) {
  fail(`dist directory not found: ${distDir}`);
}

const distFiles = walkFiles(distDir);
const rootPackage = readPackage('npm/mthc/package.json');
const targetMatches = [];

for (const target of targets) {
  const tokens = targetTokens(target);
  const matches = distFiles.map((filePath) => {
    return {
      absolutePath: filePath,
      distRelativePath: normalizePath(path.relative(distDir, filePath)),
      repoRelativePath: normalizePath(path.relative(repoRoot, filePath)),
    };
  }).filter((file) => {
    return path.basename(file.absolutePath) === 'mthc' && tokens.some((token) => file.distRelativePath.includes(token));
  });

  if (matches.length !== 1) {
    fail(`expected exactly one mthc binary for ${target.os}/${target.arch}, found ${matches.length}`);
  }

  targetMatches.push({
    ...target,
    ...matches[0],
    ...fileMetadata(matches[0].absolutePath),
  });
}

for (const packageDir of packageDirs) {
  removeIfExists(path.join(repoRoot, packageDir, 'README.md'));
  removeIfExists(path.join(repoRoot, packageDir, 'LICENSE'));
}

for (const target of targets) {
  removeIfExists(path.join(repoRoot, target.packageDir, 'bin'));
}

for (const packageDir of packageDirs) {
  for (const fileName of ['README.md', 'LICENSE']) {
    fs.copyFileSync(path.join(repoRoot, fileName), path.join(repoRoot, packageDir, fileName));
  }
}

for (const target of targetMatches) {
  const binDir = path.join(repoRoot, target.packageDir, 'bin');
  const destination = path.join(binDir, 'mthc');

  fs.mkdirSync(binDir, { recursive: true });
  fs.copyFileSync(target.absolutePath, destination);
  fs.chmodSync(destination, 0o755);
}

const manifest = {
  packageVersion: rootPackage.version,
  targets: targetMatches.map((target) => ({
    os: target.os,
    arch: target.arch,
    packageDir: target.packageDir,
    source: target.repoRelativePath,
    sha256: target.sha256,
    size: target.size,
  })),
};

fs.writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
