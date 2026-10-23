#!/usr/bin/env node
// Starts the prebuilt wecut binary for this platform.
//
// There is no install script. npm installs exactly one of the optional
// @wecut/cli-<os>-<arch> packages (chosen by their "os" and "cpu" fields),
// and this file runs the binary inside it with the same arguments and
// standard streams, so `npx wecut mcp` works as an MCP stdio server too.
'use strict';

const { spawnSync } = require('child_process');
const fs = require('fs');

const SCOPE = '@wecut';
const SUPPORTED = ['linux-x64', 'linux-arm64', 'darwin-x64', 'darwin-arm64', 'win32-x64', 'win32-arm64'];
const args = process.argv.slice(2);

function fail(message, hint) {
  const out = JSON.stringify({ ok: false, errors: [{ code: 'E_INSTALL', message, hint }] }) + '\n';
  // Like every wecut command the answer is JSON on stdout, except in MCP
  // mode, where stdout belongs to the protocol.
  (args[0] === 'mcp' ? process.stderr : process.stdout).write(out);
  process.exit(3);
}

function binaryPath() {
  if (process.env.WECUT_BINARY) return process.env.WECUT_BINARY;
  const key = `${process.platform}-${process.arch}`;
  if (!SUPPORTED.includes(key)) {
    fail(`no prebuilt wecut for ${key}`, 'Build it from source (go build ./cmd/wecut) and set WECUT_BINARY to the result.');
  }
  const pkg = `${SCOPE}/cli-${key}`;
  const exe = process.platform === 'win32' ? 'wecut.exe' : 'wecut';
  try {
    return require.resolve(`${pkg}/bin/${exe}`);
  } catch (e) {
    fail(`${pkg} is not installed`, 'Reinstall without --omit=optional or --no-optional (for example `npm i wecut`), or set WECUT_BINARY.');
  }
}

const bin = binaryPath();
const run = () => spawnSync(bin, args, { stdio: 'inherit', windowsHide: true });
let result = run();
if (result.error && result.error.code === 'EACCES') {
  // Some package managers drop the executable bit when unpacking.
  try {
    fs.chmodSync(bin, 0o755);
    result = run();
  } catch (e) {
    // fall through to the error below
  }
}
if (result.error) {
  fail(`cannot start ${bin}: ${result.error.message}`, 'Check that the file exists and is executable, or set WECUT_BINARY.');
}
if (result.signal) {
  process.kill(process.pid, result.signal);
}
process.exit(result.status === null ? 1 : result.status);
