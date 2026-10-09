// Measures unchanged shared-link dispatch in the actual app with a controlled
// DOM/client and immediate decoding, excluding encoding, browser and WASM costs.
import assert from "node:assert/strict";
import { performance } from "node:perf_hooks";
import { playground } from "./playground_harness.mjs";

const state = { g: 'def main = "é" $$', i: "é" };
const h = await playground({ state, app: process.argv[2], decode: async () => state });
const warmup = 10000, iterations = 100000;
for (let i = 0; i < warmup; i++) await h.dispatchHash();
const started = performance.now();
for (let i = 0; i < iterations; i++) await h.dispatchHash();
const elapsed = performance.now() - started;
assert.equal(h.calls.filter((c) => c.method === "parse").length, 1);
console.log(JSON.stringify({ node: process.version, warmup, iterations, nsPerHash: elapsed * 1e6 / iterations }));
