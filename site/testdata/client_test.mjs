// Tests of PegoClient (static/assets/pego-client.js) with a fake worker. Run by TestJavaScript.

import assert from "node:assert/strict";
import { test } from "node:test";

import { PegoClient } from "../static/assets/pego-client.js";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// fakeWorkers returns a Worker class whose instances, like the real worker, handle one message at a
// time: they answer {method: "echo"} after req.ms milliseconds, never finish "hang", and fail fatally
// on "overflow".
function fakeWorkers() {
  const workers = [];
  class FakeWorker {
    constructor(url) {
      this.url = url;
      this.terminated = false;
      this.received = [];
      this.busy = Promise.resolve();
      workers.push(this);
    }
    postMessage(msg) {
      this.received.push(msg);
      this.busy = this.busy.then(() => this.handle(msg));
    }
    async handle(msg) {
      const reply = (data) => !this.terminated && this.onmessage?.({ data });
      await sleep(0);
      if (msg.type === "init") return reply({ type: "ready", version: { go: "fake" } });
      if (msg.method === "echo") {
        await sleep(msg.req.ms ?? 0);
        return reply({ id: msg.id, out: JSON.stringify(msg.req) });
      }
      if (msg.method === "overflow") return reply({ id: msg.id, error: "InternalError: too much recursion" });
      await new Promise(() => {}); // "hang"
    }
    terminate() {
      this.terminated = true;
    }
  }
  return { FakeWorker, workers };
}

const client = (opts) => new PegoClient("https://example.test/playground/", { compile: async () => ({}), ...opts });

test("a timeout keeps the calls queued behind the stuck one", async () => {
  const { FakeWorker, workers } = fakeWorkers();
  const c = client({ Worker: FakeWorker, timeout: 50 });
  const stuck = c.call("hang", {});
  const latest = c.call("echo", { input: "corrected" });
  await assert.rejects(stuck, /stopped after/);
  assert.deepEqual(await latest, { input: "corrected" });
  assert.equal(workers.length, 2);
  assert.ok(workers[0].terminated);
});

test("the timeout counts only the time the worker spends on a call", async () => {
  const { FakeWorker } = fakeWorkers();
  const c = client({ Worker: FakeWorker, timeout: 100 });
  const a = c.call("echo", { ms: 70, n: 1 });
  const b = c.call("echo", { ms: 70, n: 2 }); // waits about 70 ms, then runs about 70 ms
  assert.equal((await a).n, 1);
  assert.equal((await b).n, 2);
});

test("a keyed call replaces a queued one with the same key", async () => {
  const { FakeWorker, workers } = fakeWorkers();
  const c = client({ Worker: FakeWorker });
  const first = c.call("echo", { ms: 30, n: 1 }, { key: "parse" });
  await sleep(5); // first is running
  const second = c.call("echo", { n: 2 }, { key: "parse" });
  const other = c.call("echo", { n: 9 }, { key: "generate" });
  const third = c.call("echo", { n: 3 }, { key: "parse" });
  await assert.rejects(second, { name: "SupersededError" });
  assert.equal((await first).n, 1);
  assert.equal((await third).n, 3);
  assert.equal((await other).n, 9);
  const sent = workers[0].received.filter((m) => m.method).map((m) => m.req.n);
  assert.deepEqual(sent, [1, 9, 3]);
});

test("a worker that fails to start is started again by the next call", async () => {
  const { FakeWorker } = fakeWorkers();
  let attempts = 0;
  const c = client({
    Worker: FakeWorker,
    compile: async () => {
      if (++attempts === 1) throw new Error("network down");
      return {};
    },
  });
  await assert.rejects(c.call("echo", {}), /network down/);
  assert.deepEqual(await c.call("echo", { n: 1 }), { n: 1 });
});

test("the client loads the binary the page names", async () => {
  const { FakeWorker, workers } = fakeWorkers();
  let compiled = null;
  const c = client({ Worker: FakeWorker, wasm: "wasm/pego-0123456789abcdef.wasm", compile: async (url) => ((compiled = url), {}) });
  await c.call("echo", {});
  assert.equal(compiled, "https://example.test/playground/wasm/pego-0123456789abcdef.wasm");
  assert.equal(workers[0].received[0].wasm, compiled);
  assert.equal(new PegoClient("https://example.test/playground/", { wasm: null }).wasm, "https://example.test/playground/pego.wasm");
});

test("a stack overflow restarts the worker", async () => {
  const { FakeWorker, workers } = fakeWorkers();
  const c = client({ Worker: FakeWorker });
  let reason = null;
  c.onrestart = (r) => (reason = r);
  await assert.rejects(c.call("overflow", {}), /more stack than the browser provides/);
  assert.match(reason, /too much recursion/);
  assert.deepEqual(await c.call("echo", { n: 1 }), { n: 1 });
  assert.equal(workers.length, 2);
});
