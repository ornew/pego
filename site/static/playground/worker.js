// Web Worker that runs pego.wasm, so that a long parse never freezes the page. The page sends
// {type: "init", base, wasm, module?} once, then {id, method, req}; each call is answered with
// {id, out} (the JSON response) or {id, error}.

let ready = null;

async function init(msg) {
  importScripts(msg.base + "wasm_exec.js");
  const go = new Go();
  let instance;
  if (msg.module) {
    instance = await WebAssembly.instantiate(msg.module, go.importObject);
  } else {
    const resp = await fetch(msg.wasm);
    instance = (await WebAssembly.instantiate(await resp.arrayBuffer(), go.importObject)).instance;
  }
  go.run(instance).then(() => {
    self.postMessage({ type: "exit" });
  });
  if (!self.pego) throw new Error("pego.wasm did not start");
  const v = JSON.parse(self.pego.version("{}"));
  self.postMessage({ type: "ready", version: v });
}

self.onmessage = async (e) => {
  const msg = e.data;
  if (msg.type === "init") {
    ready = init(msg).catch((err) => {
      self.postMessage({ type: "failed", error: String(err && err.message ? err.message : err) });
      throw err;
    });
    return;
  }
  try {
    await ready;
    const out = self.pego[msg.method](JSON.stringify(msg.req ?? {}));
    self.postMessage({ id: msg.id, out });
  } catch (err) {
    self.postMessage({ id: msg.id, error: String(err && err.message ? err.message : err) });
  }
};
