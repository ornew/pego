// Client of the playground's Web Worker (playground/worker.js), which runs pego.wasm.
//
//   const pego = new PegoClient(playgroundURL);
//   const res = await pego.call("parse", {grammar, input}, {key: "parse"});
//
// Calls are queued here and sent to the worker one at a time, so that the timeout of a call counts
// only the time the worker spends on it. A call with a key replaces a queued call with the same key
// that has not started (which rejects with an error whose name is "SupersededError"), so that typing
// quickly does not queue a parse per keystroke. A call that takes longer than the timeout terminates
// the worker; the calls queued behind it then run on a new worker.

let modulePromise = null;

function compileModule(url) {
  modulePromise ??= (async () => {
    try {
      return await WebAssembly.compileStreaming(fetch(url));
    } catch {
      // compileStreaming needs the application/wasm content type, which some servers do not send.
      const resp = await fetch(url);
      if (!resp.ok) throw new Error(`cannot load ${url}: ${resp.status} ${resp.statusText}`);
      return WebAssembly.compile(await resp.arrayBuffer());
    }
  })();
  modulePromise.catch(() => {
    modulePromise = null;
  });
  return modulePromise;
}

// fatalError reports whether an error thrown by pego.wasm means that the Go program cannot continue:
// it exited, trapped, or overflowed the JavaScript stack ("Maximum call stack size exceeded" in
// Chromium and Safari, "too much recursion" in Firefox).
export function fatalError(message) {
  return /exited|unreachable|call stack|too much recursion|stack overflow/i.test(message);
}

// friendlyError explains a fatal error.
export function friendlyError(message) {
  if (/call stack|too much recursion|stack overflow/i.test(message)) {
    return "the parse needed more stack than the browser provides (try the bytecode-iterative backend); the parser was restarted";
  }
  return `the parser stopped (${message}) and was restarted`;
}

function superseded() {
  const err = new Error("superseded by a newer call");
  err.name = "SupersededError";
  return err;
}

export class PegoClient {
  // options.Worker and options.compile replace the Worker constructor and the compilation of
  // pego.wasm (for tests).
  // base is the URL of the playground directory (worker.js, wasm_exec.js); wasm is the URL of
  // pego.wasm, by default the one the page names in <html data-wasm>.
  constructor(base, { timeout = 10000, wasm = defaultWasm(), Worker: WorkerClass = globalThis.Worker, compile = compileModule } = {}) {
    this.base = new URL(base, globalThis.location?.href).href;
    this.wasm = new URL(wasm || "pego.wasm", this.base).href;
    this.timeout = timeout;
    this.WorkerClass = WorkerClass;
    this.compile = compile;
    this.queue = []; // calls not yet sent: {method, req, key, resolve, reject}
    this.active = null; // the call the worker is running: {..., id, timer}
    this.seq = 0;
    this.worker = null;
    this.readyPromise = null;
    this.version = null;
    this.onrestart = null;
  }

  // start starts a worker if there is none, and returns the version of pego.wasm once it runs. If
  // starting fails, the next call tries again.
  start() {
    if (!this.readyPromise) {
      const p = this.spawn();
      this.readyPromise = p;
      p.catch(() => {
        if (this.readyPromise === p) this.readyPromise = null;
      });
    }
    return this.readyPromise;
  }

  async spawn() {
    const module = await this.compile(this.wasm);
    const worker = new this.WorkerClass(this.base + "worker.js");
    this.worker = worker;
    const ready = new Promise((resolve, reject) => {
      worker.onmessage = (e) => this.onMessage(worker, e.data, resolve, reject);
      worker.onerror = (e) => reject(new Error(e.message || "the parser worker failed to start"));
    });
    try {
      worker.postMessage({ type: "init", base: this.base, wasm: this.wasm, module });
    } catch {
      worker.postMessage({ type: "init", base: this.base, wasm: this.wasm }); // the module cannot be sent: compile in the worker
    }
    try {
      this.version = await ready;
    } catch (err) {
      if (this.worker === worker) {
        worker.terminate();
        this.worker = null;
      }
      throw err;
    }
    return this.version;
  }

  call(method, req, { key = null } = {}) {
    return new Promise((resolve, reject) => {
      if (key !== null) {
        this.queue = this.queue.filter((c) => {
          if (c.key !== key) return true;
          c.reject(superseded());
          return false;
        });
      }
      this.queue.push({ method, req, key, resolve, reject });
      this.pump();
    });
  }

  // pump sends the next queued call when the worker is idle, starting a worker if needed.
  async pump() {
    if (this.active || this.pumping || !this.queue.length) return;
    this.pumping = true;
    try {
      await this.start();
    } catch (err) {
      // The worker could not start: fail the calls waiting for it; a later call tries again.
      for (const c of this.queue.splice(0)) c.reject(err);
      return;
    } finally {
      this.pumping = false;
    }
    if (this.active || !this.queue.length || !this.worker) {
      if (!this.worker && this.queue.length) this.pump();
      return;
    }
    const c = this.queue.shift();
    c.id = ++this.seq;
    c.timer = setTimeout(() => this.onTimeout(c), this.timeout);
    this.active = c;
    this.worker.postMessage({ id: c.id, method: c.method, req: c.req });
  }

  onTimeout(c) {
    if (this.active !== c) return;
    this.active = null;
    c.reject(new Error(`stopped after ${this.timeout / 1000} seconds; the parser was restarted`));
    this.restart("timeout");
  }

  onMessage(worker, msg, resolve, reject) {
    if (worker !== this.worker) return;
    switch (msg.type) {
      case "ready":
        resolve(msg.version);
        return;
      case "failed":
        reject(new Error(msg.error));
        return;
      case "exit":
        this.restart("the parser stopped unexpectedly");
        return;
    }
    const c = this.active;
    if (!c || c.id !== msg.id) return;
    this.active = null;
    clearTimeout(c.timer);
    if (msg.error && fatalError(msg.error)) {
      c.reject(new Error(friendlyError(msg.error)));
      this.restart(msg.error);
      return;
    }
    if (msg.error) c.reject(new Error(msg.error));
    else c.resolve(JSON.parse(msg.out));
    this.pump();
  }

  // restart replaces the worker. The call it was running fails; the queued calls run on the new one.
  restart(reason) {
    this.worker?.terminate();
    this.worker = null;
    this.readyPromise = null;
    if (this.active) {
      clearTimeout(this.active.timer);
      this.active.reject(new Error(friendlyError(reason)));
      this.active = null;
    }
    this.onrestart?.(reason);
    if (this.queue.length) this.pump();
  }
}

// defaultWasm returns the URL of pego.wasm that the page names in <html data-wasm>, if any.
function defaultWasm() {
  const w = globalThis.document?.documentElement?.dataset?.wasm;
  return w ? new URL(w, globalThis.location?.href).href : null;
}

// siteRoot returns the URL of the site root, which every page records in <html data-root>.
export function siteRoot() {
  return new URL(document.documentElement.dataset.root || "./", location.href).href;
}
