import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import { test } from "node:test";

const source = readFileSync(new URL("../static/playground/worker.js", import.meta.url), "utf8");
const base = "https://site.test/playground/";
const a = { wasm: "wasm/pego-0123456789abcdef.wasm", runtime: "runtime/wasm_exec-1111111111111111.js" };
const b = { wasm: "wasm/pego-fedcba9876543210.wasm", runtime: "runtime/wasm_exec-2222222222222222.js" };

function worker(manifest, { ok = true } = {}) {
  const imported = [], fetched = [], messages = [];
  const self = { pego: { version: () => '{"go":"test"}' }, postMessage: (m) => messages.push(m) };
  const ctx = vm.createContext({ self, URL, JSON, Error,
    importScripts: (url) => imported.push(url),
    fetch: async (url, opts) => { fetched.push({ url, opts }); return { ok, status: ok ? 200 : 503, json: async () => manifest }; },
    Go: class { importObject = {}; run() { return new Promise(() => {}); } },
    WebAssembly: { instantiate: async () => ({}) },
  });
  vm.runInContext(source, ctx);
  return { imported, fetched, messages, init: (wasm) => vm.runInContext(`init(${JSON.stringify({base, wasm, module:{}})})`, ctx) };
}

for (const [name, asset] of [["current", b], ["previous", a]]) {
  test(`worker selects the ${name} binary's own Go runtime`, async () => {
    const w = worker({ version: 1, current: b, previous: a });
    await w.init(base + asset.wasm);
    assert.deepEqual(w.imported, [base + asset.runtime]);
    assert.equal(w.fetched[0].url, base + "assets.json");
    assert.equal(w.fetched[0].opts.cache, "no-cache");
    assert.equal(w.messages[0].type, "ready");
  });
}
test("unhashed external WASM retains the explicit stable-runtime convention", async () => {
  const w = worker(null);
  await w.init(base + "pego.wasm");
  assert.deepEqual(w.imported, [base + "wasm_exec.js"]);
  assert.equal(w.fetched.length, 0);
});
for (const [name, manifest, opts, error] of [
  ["expired generation", {version:1,current:b}, {}, /reload this page/],
  ["manifest version", {version:2,current:a}, {}, /reload this page/],
  ["runtime traversal", {version:1,current:{...a,runtime:"../private.js"}}, {}, /reload this page/],
  ["unavailable manifest", {}, {ok:false}, /assets: 503/],
]) {
  test(name, async () => {
    const w = worker(manifest,opts);
    await assert.rejects(w.init(base+a.wasm),error);
    assert.equal(w.imported.length,0);
  });
}
