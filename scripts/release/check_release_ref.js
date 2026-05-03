#!/usr/bin/env node
'use strict';

const { spawnSync } = require('node:child_process');

const tag = process.argv[2];
const mainRef = process.argv[3] || 'origin/main';
const tagPattern = /^v\d+\.\d+\.\d+$/;

function fail(message) {
  console.error(`error: ${message}`);
  process.exit(1);
}

function git(args) {
  const result = spawnSync('git', args, {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  if (result.status !== 0) {
    fail(`git ${args.join(' ')} failed: ${result.stderr.trim() || result.stdout.trim()}`);
  }

  return result.stdout.trim();
}

if (!tag || !tagPattern.test(tag)) {
  fail('tag must look like v0.2.0');
}

const tagCommit = git(['rev-list', '-n', '1', tag]);
const mainCommit = git(['rev-parse', '--verify', `${mainRef}^{commit}`]);
const result = spawnSync('git', ['merge-base', '--is-ancestor', tagCommit, mainCommit], {
  encoding: 'utf8',
  stdio: ['ignore', 'pipe', 'pipe'],
});

if (result.status === 0) {
  console.log(`${tag} is contained in ${mainRef}`);
  process.exit(0);
}

if (result.status === 1) {
  fail(`${tag} is not contained in ${mainRef}`);
}

fail(`git merge-base --is-ancestor failed: ${result.stderr.trim() || result.stdout.trim()}`);
