#!/usr/bin/env node
'use strict';

const { spawnSync } = require('node:child_process');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..', '..');
const distDir = path.resolve(repoRoot, process.argv[2] || 'dist');
const manifestPath = path.join(distDir, 'npm-assembly.json');
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

function readManifest() {
  try {
    return JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  } catch (error) {
    fail(`failed to read ${manifestPath}: ${error.message}`);
  }
}

function fileMetadata(filePath) {
  const contents = fs.readFileSync(filePath);

  return {
    sha256: crypto.createHash('sha256').update(contents).digest('hex'),
    size: contents.length,
  };
}

function validateManifest() {
  const manifest = readManifest();
  const rootPackage = readPackage('npm/mthc');
  const targets = new Map((manifest.targets || []).map((target) => [target.packageDir, target]));

  if (manifest.packageVersion !== rootPackage.version) {
    fail(`manifest packageVersion ${manifest.packageVersion} does not match root version ${rootPackage.version}`);
  }

  for (const packageDir of packageDirs.slice(0, -1)) {
    const target = targets.get(packageDir);
    if (!target) {
      fail(`manifest missing target for ${packageDir}`);
    }

    const binaryPath = path.join(repoRoot, packageDir, 'bin', 'mthc');
    let stat;
    try {
      stat = fs.statSync(binaryPath);
      fs.accessSync(binaryPath, fs.constants.X_OK);
    } catch (error) {
      fail(`invalid executable ${binaryPath}: ${error.message}`);
    }

    if (!stat.isFile()) {
      fail(`invalid executable ${binaryPath}: not a regular file`);
    }

    const metadata = fileMetadata(binaryPath);
    if (metadata.sha256 !== target.sha256 || metadata.size !== target.size) {
      fail(`manifest metadata mismatch for ${packageDir}/bin/mthc`);
    }
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

validateManifest();

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
