// Measures extension lifecycle dispatch with immediate stand-ins, excluding a
// real VS Code host, server startup, shutdown and IPC. Compare sequential work
// only; overlapping transitions intentionally change the parent's behavior.
'use strict';

const assert = require('node:assert/strict');
const { performance } = require('node:perf_hooks');
const { loadExtension } = require('./extension-harness');

async function main() {
  const h = loadExtension([], { extension: process.argv[2], record: false });
  const iterations = 200000, warmup = 10000;
  await h.activate();
  for (let i = 0; i < warmup; i++) await h.restart();
  const start = performance.now();
  for (let i = 0; i < iterations; i++) await h.restart();
  const elapsed = performance.now() - start;
  assert.equal(h.running.size, 1);
  assert.equal(h.maxRunning, 1);
  assert.equal(h.created, warmup + iterations + 1);
  await h.extension.deactivate();
  assert.equal(h.running.size, 0);
  assert.equal(h.resources.size, 0);
  console.log(JSON.stringify({ node: process.version, warmup, iterations,
    nsPerRestart: elapsed * 1e6 / iterations, created: h.created }));
}

main().catch((err) => { console.error(err); process.exitCode = 1; });
