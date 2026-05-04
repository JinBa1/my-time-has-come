#!/usr/bin/env node
'use strict';

const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const sourceRoot = path.resolve(__dirname, '..', '..');

const packageDirs = [
  'npm/mthc',
  'npm/platforms/darwin-arm64',
  'npm/platforms/darwin-x64',
  'npm/platforms/linux-arm64',
  'npm/platforms/linux-x64',
];

const targetFixtures = [
  ['dist/build-darwin_amd64/mthc', 'darwin amd64 fixture'],
  ['dist/build-darwin-arm64/mthc', 'darwin arm64 fixture'],
  ['dist/build-linux_amd64/mthc', 'linux amd64 fixture'],
  ['dist/build-linux-arm64/mthc', 'linux arm64 fixture'],
];

function git(root, args) {
  const result = spawnSync('git', args, {
    cwd: root,
    encoding: 'utf8',
  });
  assert.equal(result.status, 0, result.stderr);
  return result;
}

function makeGitRepoFixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'mthc-release-ref-test-'));
  t.after(() => fs.rmSync(root, { force: true, recursive: true }));

  git(root, ['init', '-b', 'main']);
  git(root, ['config', 'user.email', 'test@example.com']);
  git(root, ['config', 'user.name', 'Test User']);

  fs.writeFileSync(path.join(root, 'file.txt'), 'main\n');
  git(root, ['add', 'file.txt']);
  git(root, ['commit', '-m', 'main']);
  git(root, ['tag', 'v0.2.0']);

  git(root, ['checkout', '-b', 'feature']);
  fs.writeFileSync(path.join(root, 'file.txt'), 'feature\n');
  git(root, ['commit', '-am', 'feature']);
  git(root, ['tag', 'v0.3.0']);

  return root;
}

function makeRepoFixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'mthc-darwin-arm64-release-test-'));
  t.after(() => fs.rmSync(root, { force: true, recursive: true }));

  fs.mkdirSync(path.join(root, 'scripts', 'release'), { recursive: true });
  for (const scriptName of ['check_npm_versions.js', 'check_release_ref.js', 'assemble_npm.js', 'publish_npm.js']) {
    fs.copyFileSync(
      path.join(sourceRoot, 'scripts', 'release', scriptName),
      path.join(root, 'scripts', 'release', scriptName),
    );
  }

  fs.copyFileSync(path.join(sourceRoot, 'README.md'), path.join(root, 'README.md'));
  fs.copyFileSync(path.join(sourceRoot, 'LICENSE'), path.join(root, 'LICENSE'));

  for (const packageDir of packageDirs) {
    fs.mkdirSync(path.join(root, packageDir), { recursive: true });
    fs.copyFileSync(
      path.join(sourceRoot, packageDir, 'package.json'),
      path.join(root, packageDir, 'package.json'),
    );
  }

  return root;
}

function runNode(root, args, options = {}) {
  return spawnSync(process.execPath, args, {
    cwd: root,
    encoding: 'utf8',
    ...options,
  });
}

function runReleaseRefCheck(root, tag, mainRef) {
  return runNode(root, [
    path.join(sourceRoot, 'scripts/release/check_release_ref.js'),
    tag,
    mainRef,
  ]);
}

function writeTargetFixtures(root) {
  for (const [relativePath, content] of targetFixtures) {
    const filePath = path.join(root, relativePath);
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    fs.writeFileSync(filePath, content);
  }
}

test('check_npm_versions rejects prerelease tags before version checks', () => {
  const result = runNode(sourceRoot, ['scripts/release/check_npm_versions.js', 'v0.2.1-beta.1']);

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /tag must look like v0\.2\.0/);
});

test('check_release_ref requires the release tag commit to be on main', (t) => {
  const root = makeGitRepoFixture(t);

  const accepted = runReleaseRefCheck(root, 'v0.2.0', 'main');
  assert.equal(accepted.status, 0, accepted.stderr);

  const rejected = runReleaseRefCheck(root, 'v0.3.0', 'main');
  assert.notEqual(rejected.status, 0);
  assert.match(rejected.stderr, /v0\.3\.0 is not contained in main/);
});

test('goreleaser injects v-prefixed CLI versions while archive names stay unprefixed', () => {
  const config = fs.readFileSync(path.join(sourceRoot, '.goreleaser.yaml'), 'utf8');

  assert.match(
    config,
    /internal\/version\.Version=v\{\{ \.Version \}\}/,
  );
  assert.match(config, /name_template: "mthc_\{\{ \.Version \}\}_\{\{ \.Os \}\}_\{\{ \.Arch \}\}"/);
});

test('assemble_npm matches binaries by dist-relative paths only', (t) => {
  const root = makeRepoFixture(t);
  writeTargetFixtures(root);
  const stray = path.join(root, 'dist', 'no-target-token', 'mthc');
  fs.mkdirSync(path.dirname(stray), { recursive: true });
  fs.writeFileSync(stray, 'stray fixture');

  const result = runNode(root, ['scripts/release/assemble_npm.js', 'dist']);

  assert.equal(result.status, 0, result.stderr);
  assert.equal(
    fs.readFileSync(path.join(root, 'npm/platforms/darwin-arm64/bin/mthc'), 'utf8'),
    'darwin arm64 fixture',
  );
});

test('assemble_npm cleans stale payloads and writes a manifest', (t) => {
  const root = makeRepoFixture(t);
  writeTargetFixtures(root);

  for (const packageDir of packageDirs) {
    fs.writeFileSync(path.join(root, packageDir, 'README.md'), 'stale readme');
    fs.writeFileSync(path.join(root, packageDir, 'LICENSE'), 'stale license');
  }
  for (const packageDir of packageDirs.slice(1)) {
    const binDir = path.join(root, packageDir, 'bin');
    fs.mkdirSync(binDir, { recursive: true });
    fs.writeFileSync(path.join(binDir, 'mthc'), 'stale binary');
    fs.writeFileSync(path.join(binDir, 'extra'), 'stale extra');
  }

  const result = runNode(root, ['scripts/release/assemble_npm.js', 'dist']);

  assert.equal(result.status, 0, result.stderr);
  for (const packageDir of packageDirs) {
    assert.equal(
      fs.readFileSync(path.join(root, packageDir, 'README.md'), 'utf8'),
      fs.readFileSync(path.join(root, 'README.md'), 'utf8'),
    );
    assert.equal(
      fs.readFileSync(path.join(root, packageDir, 'LICENSE'), 'utf8'),
      fs.readFileSync(path.join(root, 'LICENSE'), 'utf8'),
    );
  }
  for (const packageDir of packageDirs.slice(1)) {
    const binDir = path.join(root, packageDir, 'bin');
    assert.deepEqual(fs.readdirSync(binDir), ['mthc']);
    assert.equal(fs.statSync(path.join(binDir, 'mthc')).mode & 0o777, 0o755);
  }

  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'dist/npm-assembly.json'), 'utf8'));
  assert.equal(manifest.packageVersion, '0.2.0');
  assert.equal(manifest.targets.length, 4);
  assert.deepEqual(
    manifest.targets.map((target) => target.packageDir).sort(),
    [
      'npm/platforms/darwin-arm64',
      'npm/platforms/darwin-x64',
      'npm/platforms/linux-arm64',
      'npm/platforms/linux-x64',
    ],
  );
  for (const target of manifest.targets) {
    assert.match(target.source, /^dist\//);
    assert.match(target.sha256, /^[0-9a-f]{64}$/);
    assert.equal(typeof target.size, 'number');
  }
});

test('publish_npm validates assembly manifest before npm commands', (t) => {
  const root = makeRepoFixture(t);
  const npmBin = path.join(root, 'npm-fake');
  fs.mkdirSync(npmBin);
  fs.writeFileSync(path.join(npmBin, 'npm'), '#!/bin/sh\necho npm should not run >&2\nexit 42\n');
  fs.chmodSync(path.join(npmBin, 'npm'), 0o755);

  const result = runNode(root, ['scripts/release/publish_npm.js', 'dist'], {
    env: { ...process.env, PATH: `${npmBin}${path.delimiter}${process.env.PATH}` },
  });

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /npm-assembly\.json/);
  assert.doesNotMatch(result.stderr, /npm should not run/);
});
