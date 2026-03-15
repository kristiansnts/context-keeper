#!/usr/bin/env node
"use strict";

const { spawnSync } = require("child_process");
const path = require("path");
const os = require("os");

const platform = os.platform(); // darwin, linux, win32
const arch = os.arch();         // arm64, x64

const archMap = { x64: "amd64", arm64: "arm64" };
const goArch = archMap[arch] || arch;

const binaryName = platform === "win32"
  ? `context-keeper-windows-${goArch}.exe`
  : `context-keeper-${platform}-${goArch}`;

const binaryPath = path.join(__dirname, "bin", binaryName);

const result = spawnSync(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
});

if (result.error) {
  process.stderr.write(
    `[context-keeper] Failed to start binary: ${result.error.message}\n` +
    `[context-keeper] Expected binary at: ${binaryPath}\n`
  );
  process.exit(1);
}

process.exit(result.status || 0);
