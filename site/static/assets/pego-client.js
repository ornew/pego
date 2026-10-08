// Client of the playground's Web Worker (playground/worker.js), which runs pego.wasm.
//
//   const pego = new PegoClient(playgroundURL);
//   const res = await pego.call("parse", {grammar, input});
//
// A call that takes longer than the timeout terminates the worker, which is restarted for the next
// call, so that a runaway parse cannot hang the page.

let modulePromise = null;

function compileModule(base) {
  modulePromise ??= (async () => {
    const url = base + "pego.wasm";
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

export class PegoClient {
  constructor(base, { timeout = 10000 } = {}) {
    this.base = new URL(base, location.href).href;
    this.timeout = timeout;
    this.pending = new Map();
    this.seq = 0;
    this.worker = null;
    this.version = null;
    this.readyPromise = null;
    this.onrestart = null;
  }

  start() {
    this.readyPromise ??= this.spawn();
    return this.readyPromise;
  }

  async spawn() {
    const module = await compileModule(this.base);
    const worker = new Worker(this.base + "worker.js");
    this.worker = worker;
    const ready = new Promise((resolve, reject) => {
      worker.onmessage = (e) => this.onMessage(worker, e.data, resolve, reject);
      worker.onerror = (e) => reject(new Error(e.message || "the parser worker failed to start"));
    });
    try {
      worker.postMessage({ type: "init", base: this.base, module });
    } catch {
      worker.postMessage({ type: "init", base: this.base }); // the module cannot be sent: compile in the worker
    }
    this.version = await ready;
    return this.version;
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
    const p = this.pending.get(msg.id);
    if (!p) return;
    this.pending.delete(msg.id);
    clearTimeout(p.timer);
    if (msg.error) {
      p.reject(new Error(msg.error));
      if (/exited|unreachable|stack/i.test(msg.error)) this.restart(msg.error);
    } else {
      p.resolve(JSON.parse(msg.out));
    }
  }

  async call(method, req) {
    await this.start();
    const id = ++this.seq;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`stopped after ${this.timeout / 1000} seconds; the parser was restarted`));
        this.restart("timeout");
      }, this.timeout);
      this.pending.set(id, { resolve, reject, timer });
      this.worker.postMessage({ id, method, req });
    });
  }

  restart(reason) {
    this.worker?.terminate();
    this.worker = null;
    for (const [, p] of this.pending) {
      clearTimeout(p.timer);
      p.reject(new Error(reason));
    }
    this.pending.clear();
    this.readyPromise = this.spawn();
    this.onrestart?.(reason);
  }
}

// siteRoot returns the URL of the site root, which every page records in <html data-root>.
export function siteRoot() {
  return new URL(document.documentElement.dataset.root || "./", location.href).href;
}
