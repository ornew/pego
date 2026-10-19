import assert from "node:assert/strict";
import { test } from "node:test";
import vm from "node:vm";
import { playground, Element } from "./playground_harness.mjs";
import { decodeState } from "../static/assets/state.js";

const base = { g: 'rule main = "é" $$', i: "é" };
const evaluate = (h, source) => vm.runInContext(source, h.context);

test("TypeScript settings travel in requests and complete shared links", async () => {
  const gen = { language: "typescript", package: "custom", types: false, recognize: true };
  const h = await playground({ state: { ...base, t: "go", gen } });
  assert.equal(h.el("gen-language").value, "typescript");
  assert.equal(h.el("gen-package").disabled, true);
  const req = h.calls.filter((c) => c.method === "generate").at(-1).req;
  assert.equal(req.language, "typescript");
  assert.equal(req.types, false);
  assert.equal(req.recognize, true);
  evaluate(h, `var finishExport; PegoClient.prototype.call = () => new Promise((resolve) => { finishExport = resolve; });`);
  const exporting = evaluate(h, "generate()");
  await h.event("pg-share", "click");
  assert.deepEqual((await decodeState(h.location.hash)).gen, gen);
  evaluate(h, 'finishExport({code: "current TypeScript", diagnostics: []})');
  await exporting;
  assert.equal(h.el("gen-download").disabled, false, "sharing preserves a current download");
  await h.hash(base);
  assert.equal(h.el("gen-language").value, "go");
  assert.equal(h.el("gen-types").checked, true);
  assert.equal(h.el("gen-recognize").checked, false);
});

test("paged exports copy and download the full source", async () => {
  const h = await playground({ state: { ...base, t: "go" } });
  evaluate(h, `PegoClient.prototype.call = async () => ({ code: "x".repeat(60000), diagnostics: [] });`);
  h.el("gen-language").value = "typescript";
  await h.event("gen-language", "input");
  await h.flush(250);
  assert.equal(h.el("pg-go").firstElementChild.textContent.length, 24000);
  assert.equal(h.el("pg-go-page").textContent, "Page 1 of 3");
  await h.event("pg-go-next", "click");
  assert.equal(h.el("pg-go-page").textContent, "Page 2 of 3");
  await h.event("pg-copy", "click");
  assert.equal(h.copies.at(-1).length, 60000);
  const downloads = [], blobs = [], revoked = [];
  h.context.Blob = Blob;
  h.context.URL = { createObjectURL(blob) { blobs.push(blob); return "blob:controlled"; }, revokeObjectURL(url) { revoked.push(url); } };
  h.context.document.createElement = () => {
    const link = new Element();
    link.click = () => downloads.push({ href: link.href, filename: link.download });
    return link;
  };
  await h.event("gen-download", "click");
  assert.equal(downloads[0].filename, "parser.ts");
  assert.equal((await blobs[0].text()).length, 60000);
  await h.flush(1000);
  assert.deepEqual(revoked, ["blob:controlled"]);
  h.el("pg-grammar").value += " ";
  await h.event("pg-grammar", "input");
  assert.equal(h.el("gen-download").disabled, true);
  await h.event("gen-download", "click");
  assert.equal(downloads.length, 1);
});

test("comparison is opt-in and preserves the request unit and options", async () => {
  const h = await playground({ state: { ...base, u: "bytes", s: "main", r: 1, t: "compare" } });
  assert.equal(h.calls.filter((c) => c.method === "parse").length, 1);
  await h.event("pg-compare-run", "click");
  const calls = h.calls.filter((c) => c.method === "parse").slice(1);
  assert.deepEqual(calls.map((c) => c.req.backend), ["closure", "bytecode", "bytecode-iterative"]);
  for (const { req } of calls) {
    assert.equal(req.unit, "bytes");
    assert.equal(req.start, "main");
    assert.equal(req.recognize, true);
    assert.equal(req.input, "é");
    assert.equal(req.runs, 5);
  }
  const summary = h.el("pg-compare-summary");
  assert.equal(summary.className, "pg-status ok");
  const [verdict, times, offsets] = summary.children.map((line) => line.textContent);
  assert.equal(verdict, "✓ Values, errors and positions agree");
  assert.match(times, /^Time per backend: closure .* bytecode-iterative .* \(median of \d+ runs? each\)\.$/);
  assert.match(offsets, /^Offsets: bytes; .*columns in bytes\.$/);
});

test("comparison distinguishes values, errors and positions with a bounded result", async () => {
  const h = await playground({ state: base });
  const diff = evaluate(h, `backendDifferences(
    {tree: {type: "Match", text: "a", start: 1}, errors: [{message: "x", pos: 1}]},
    {tree: {type: "Match", text: "b", start: 2}, errors: [{message: "y", pos: 2}]})`);
  assert.equal(diff.total, 4);
  assert.deepEqual(Array.from(diff.rows, (r) => r.kind).sort(), ["error", "position", "position", "value"]);
  const bounded = evaluate(h, `backendDifferences(Array(1000).fill("a"), Array(1000).fill("b"))`);
  assert.equal(bounded.total, 1000);
  assert.equal(bounded.rows.length, 100);
  const long = evaluate(h, `backendDifferences("a".repeat(100000), "b".repeat(100000))`);
  assert.ok(long.rows[0].reference.length <= 501);
});

test("an export finishing after an editor change cannot enable an obsolete download", async () => {
  const h = await playground({ state: { ...base, t: "go" } });
  evaluate(h, `var finishExport; PegoClient.prototype.call = () => new Promise((resolve) => { finishExport = resolve; });`);
  const generating = evaluate(h, "generate()");
  h.el("pg-grammar").value = 'rule main = "b"';
  await h.event("pg-grammar", "input");
  evaluate(h, 'finishExport({code: "old code", diagnostics: []})');
  await generating;
  assert.equal(h.el("gen-download").disabled, true);
});
