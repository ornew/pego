// Actual parent/candidate TreeView sources run in the same VM and controlled DOM.
// Wide rendering intentionally displays less work; it is not an equivalent-work speedup.
import assert from "node:assert/strict";
import { performance } from "node:perf_hooks";
import { treeView, wide } from "./tree_harness.mjs";

const h = await treeView(process.argv[2] ? { app: process.argv[2] } :
  { tree: new URL("../static/playground/tree.js", import.meta.url) });
const results = [];
for (const [name, width, warmup, iterations] of [["small", 50, 100, 1000], ["wide", 10000, 3, 20]]) {
  const root = wide(width);
  for (let i = 0; i < warmup; i++) h.view.set(root);
  const createdBefore = h.doc.createdRows;
  const started = performance.now();
  for (let i = 0; i < iterations; i++) h.view.set(root);
  const usPerSet = (performance.now() - started) * 1000 / iterations;
  const rows = h.rows(), createdPerSet = (h.doc.createdRows - createdBefore) / iterations;
  assert.equal(rows, createdPerSet);
  if (name === "small") assert.equal(rows, 51); // Equivalent displayed rows on both sides.
  results.push({ name, width, warmup, iterations, rows, createdPerSet, usPerSet });
}
console.log(JSON.stringify({ node: process.version, results }));
