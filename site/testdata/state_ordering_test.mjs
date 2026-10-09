import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import { test } from "node:test";
import { decodeState } from "../static/assets/state.js";
import { Element, playground } from "./playground_harness.mjs";

const base = { g: 'def main = "a"\ndef alt = "b"', i: "a" };
const app = process.env.PEGO_PLAYGROUND_APP;
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function encoder() {
  const pending = [];
  return { pending, encode: (state) => {
    const gate = deferred();
    pending.push({ ...gate, state: structuredClone(state) });
    return gate.promise;
  } };
}
async function edit(h, text) {
  h.el("pg-input").value = text;
  await h.event("pg-input", "input");
}

test("reverse autosave completion preserves the newer input", async () => {
  const e = encoder(), h = await playground({ state: base, app, encode: e.encode });
  await edit(h, "older"); const a = h.flush(400);
  await edit(h, "newer"); const b = h.flush(400);
  e.pending[1].resolve("newer"); await b;
  e.pending[0].resolve("older"); await a;
  assert.deepEqual(h.writes, ["#newer"]);
  assert.equal(h.location.hash, "#newer");
});

for (const [id, event, value] of [
  ["pg-grammar", "input", 'def main = "b"'], ["pg-input", "input", "newer"],
  ["pg-unit", "change", "bytes"], ["pg-backend", "change", "bytecode"],
  ["pg-start", "change", "alt"], ["pg-recognize", "change", true],
  ["tab-json", "click", null], ["pg-example", "change", "sample"],
]) test(`${id} invalidates encoding before the next autosave begins`, async () => {
  const e = encoder(), h = await playground({ state: base, app, encode: e.encode });
  const old = h.flush(400);
  if (id === "pg-recognize") h.el(id).checked = value;
  else if (value !== null) h.el(id).value = value;
  await h.event(id, event);
  e.pending[0].resolve("older"); await old;
  assert.deepEqual(h.writes, []);
});

test("Share supersedes encoding and copies only its own latest link", async () => {
  const e = encoder(), h = await playground({ state: base, app, encode: e.encode });
  await edit(h, "older"); const old = h.flush(400);
  await edit(h, "newer"); const shared = h.event("pg-share", "click");
  e.pending[1].resolve("newer"); await shared;
  e.pending[0].resolve("older"); await old;
  assert.deepEqual(h.writes, ["#newer"]);
  assert.deepEqual(h.copies, ["https://example.test/playground/#newer"]);
});

test("a superseded Share does not copy after a newer Share", async () => {
  const e = encoder(), h = await playground({ state: base, app, encode: e.encode });
  const old = h.event("pg-share", "click");
  await edit(h, "newer"); const newer = h.event("pg-share", "click");
  e.pending[1].resolve("newer"); await newer;
  e.pending[0].resolve("older"); await old;
  assert.deepEqual(h.copies, ["https://example.test/playground/#newer"]);
  assert.deepEqual(h.writes, ["#newer"]);
});

test("parse completion does not cancel Share for the same state", async () => {
  const e = encoder(), h = await playground({ state: base, app, encode: e.encode });
  await edit(h, "newer"); const shared = h.event("pg-share", "click");
  await h.flush(150);
  e.pending[0].resolve("newer"); await shared;
  assert.deepEqual(h.copies, ["https://example.test/playground/#newer"]);
});

function decoder() {
  const pending = [];
  return { pending, decode: (hash) => {
    if (hash.startsWith("#z=") || hash.startsWith("#j=")) return decodeState(hash);
    const gate = deferred(); pending.push({ ...gate, hash }); return gate.promise;
  } };
}
function navigate(h, hash) { h.location.hash = hash; return h.dispatchHash(); }

test("incoming navigation invalidates encoding before decode finishes", async () => {
  const e = encoder(), d = decoder();
  const h = await playground({ state: base, app, encode: e.encode, decode: d.decode });
  const old = h.flush(400), incoming = navigate(h, "#incoming");
  e.pending[0].resolve("older"); await old;
  assert.deepEqual(h.writes, []);
  d.pending[0].resolve({ ...base, i: "incoming" }); await incoming; await h.flush(0);
  assert.equal(h.el("pg-input").value, "incoming");
  assert.equal(h.location.hash, "#incoming");
});

test("a changed hash invalidates encoding before its event is delivered", async () => {
  const e = encoder(), h = await playground({ state: base, app, encode: e.encode });
  const old = h.flush(400);
  h.location.hash = "#navigation-not-dispatched";
  e.pending[0].resolve("older"); await old;
  assert.equal(h.location.hash, "#navigation-not-dispatched");
  assert.deepEqual(h.writes, []);
});

test("navigation before an old autosave starts cannot encode the old UI", async () => {
  const e = encoder(), d = decoder();
  const h = await playground({ state: base, app, encode: e.encode, decode: d.decode });
  h.location.hash = "#incoming";
  const old = h.flush(400);
  // Allow the faulty parent to finish encoding so its stale publication fails.
  if (e.pending.length) e.pending[0].resolve("older");
  await old;
  assert.equal(h.location.hash, "#incoming");
  assert.deepEqual(h.writes, []);
  assert.equal(e.pending.length, 0);
  const incoming = h.dispatchHash();
  d.pending[0].resolve({ ...base, i: "incoming" }); await incoming; await h.flush(0);
  const saved = h.flush(400);
  e.pending[0].resolve("incoming-saved"); await saved;
  assert.deepEqual(h.writes, ["#incoming-saved"]);
});

test("an older parse completion cannot autosave while a link is loading", async () => {
  const e = encoder(), d = decoder();
  const h = await playground({ state: base, app, encode: e.encode, decode: d.decode });
  h.el("pg-unit").value = "bytes"; await h.event("pg-unit", "change");
  const incoming = navigate(h, "#incoming");
  await h.flush(0);
  const old = h.flush(400);
  if (e.pending.length) e.pending[0].resolve("older");
  await old;
  assert.deepEqual(h.writes, []);
  assert.equal(e.pending.length, 0);
  d.pending[0].resolve({ ...base, i: "incoming" }); await incoming;
});

test("reverse decode completion preserves the newer state and options", async () => {
  const d = decoder(), h = await playground({ state: base, app, decode: d.decode });
  const a = navigate(h, "#older"), b = navigate(h, "#newer");
  d.pending[1].resolve({ ...base, i: "newer", b: "bytecode", u: "bytes", s: "alt", t: "json", r: 1 });
  await b; await h.flush(0);
  d.pending[0].resolve({ ...base, i: "older" }); await a; await h.flush(0);
  assert.equal(h.el("pg-input").value, "newer");
  assert.equal(h.el("pg-backend").value, "bytecode");
  assert.equal(h.el("tab-json").getAttribute("aria-selected"), "true");
  assert.equal(h.calls.filter((c) => c.method === "parse").length, 2);
  const req = h.calls.at(-1).req;
  assert.equal(req.unit, "bytes"); assert.equal(req.start, "alt"); assert.equal(req.recognize, true);
});

test("local editing supersedes a pending incoming decode", async () => {
  const e = encoder(), d = decoder();
  const h = await playground({ state: base, app, encode: e.encode, decode: d.decode });
  const incoming = navigate(h, "#incoming");
  await edit(h, "local");
  d.pending[0].resolve({ ...base, i: "incoming" }); await incoming;
  assert.equal(h.el("pg-input").value, "local");
  await h.flush(150);
  assert.equal(h.calls.at(-1).req.input, "local");
  const saved = h.flush(400); e.pending[0].resolve("local"); await saved;
  assert.deepEqual(h.writes, ["#local"]);
});

test("Share supersedes a pending incoming decode with the visible state", async () => {
  const e = encoder(), d = decoder();
  const h = await playground({ state: base, app, encode: e.encode, decode: d.decode });
  const incoming = navigate(h, "#incoming");
  const shared = h.event("pg-share", "click");
  e.pending[0].resolve("visible"); await shared;
  d.pending[0].resolve({ ...base, i: "incoming" }); await incoming;
  assert.equal(h.el("pg-input").value, base.i);
  assert.deepEqual(h.copies, ["https://example.test/playground/#visible"]);
});

test("invalid navigation supersedes an older valid pending decode", async () => {
  const d = decoder(), h = await playground({ state: base, app, decode: d.decode });
  const old = navigate(h, "#older"), invalid = navigate(h, "#invalid");
  d.pending[1].resolve(null); await invalid;
  d.pending[0].resolve({ ...base, i: "older" }); await old;
  assert.equal(h.el("pg-input").value, base.i);
  assert.equal(h.location.hash, "#invalid");
});

test("a later navigation supersedes initial decoding and fallback", async () => {
  const first = deferred(), later = deferred(), reading = deferred(); let reads = 0;
  const h = await playground({ state: base, app, waitForInit: false,
    decode: () => { if (++reads === 1) { reading.resolve(); return first.promise; } return later.promise; } });
  // Ensure initialization has begun decoding, without relying on elapsed time.
  await reading.promise;
  assert.equal(reads, 1);
  const incoming = navigate(h, "#newer");
  later.resolve({ ...base, i: "newer" }); await incoming;
  first.resolve(null); await h.ready;
  assert.equal(h.el("pg-input").value, "newer");
  assert(h.calls.every((c) => c.method !== "parse" || c.req.input === "newer"));
});

test("editing during initial example fetch prevents default overwrite", async () => {
  const fetched = deferred();
  const h = await playground({ app, waitForInit: false, fetchExamples: () => fetched.promise });
  await edit(h, "local");
  fetched.resolve({ json: async () => [{ name: "sample", grammar: 'def main = "a"', input: "a" }] });
  await h.ready;
  assert.equal(h.el("pg-input").value, "local");
});

test("encoding and decoding failures settle without stale writes", async () => {
  const e = encoder(), d = decoder();
  const h = await playground({ state: base, app, encode: e.encode, decode: d.decode });
  const write = h.flush(400); e.pending[0].reject(new Error("encode failed"));
  await assert.doesNotReject(write);
  const read = navigate(h, "#failed"); d.pending[0].reject(new Error("decode failed"));
  await assert.doesNotReject(read);
  assert.deepEqual(h.writes, []);
  assert.equal(h.el("pg-input").value, base.i);
});

async function landing(encode) {
  const elements = new Map(["live", "live-input", "live-output", "live-status", "live-open"].map((id) => [id, new Element()]));
  elements.get("live").dataset = { grammar: base.g };
  elements.get("live-input").value = "initial";
  elements.get("live-open").href = "/playground/";
  const warnings = [];
  const context = vm.createContext({
    document: { getElementById: (id) => elements.get(id) }, window: {},
    PegoClient: class { async call() { return { sexpr: "", nodes: 0, micros: 0 }; } },
    siteRoot: () => "/", encodeState: encode,
    console: { ...console, warn: (...args) => warnings.push(args) },
    setTimeout: () => 1, clearTimeout() {},
  });
  const source = await readFile(process.env.PEGO_LANDING_APP || new URL("../static/assets/landing.js", import.meta.url), "utf8");
  new vm.Script(source.replace(/^import .*;\n/gm, "")).runInContext(context);
  const input = elements.get("live-input"), open = elements.get("live-open");
  return { input, open, warnings, edit(value) { input.value = value; return input.listeners.input[0](); } };
}

test("landing link keeps the newest input after reverse encoding", async () => {
  const e = encoder(), h = await landing(e.encode);
  h.edit("newer");
  e.pending[1].resolve("newer"); await Promise.resolve(); await Promise.resolve();
  e.pending[0].resolve("older"); await Promise.resolve(); await Promise.resolve();
  assert.equal(h.open.href, "/playground/#newer");
});

test("landing encoding failure preserves the existing link", async () => {
  const e = encoder(), h = await landing(e.encode);
  e.pending[0].reject(new Error("encode failed"));
  await Promise.resolve(); await Promise.resolve();
  assert.equal(h.open.href, "/playground/");
  assert.equal(h.warnings.length, 1);
});
