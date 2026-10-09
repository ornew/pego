// Runs the actual playground application with a controlled DOM, worker client
// and timers. It covers application wiring; browser checks cover real DOM/WASM.
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import * as highlighting from "../static/assets/highlight.js";
import { decodeState, encodeState } from "../static/assets/state.js";
import { describeResult } from "../static/playground/status.js";

export class Element {
  value = "";
  checked = false;
  textContent = "";
  innerHTML = "";
  style = {};
  children = [];
  attributes = {};
  listeners = {};
  scrollTop = 0;
  scrollLeft = 0;
  classList = { add() {}, remove() {}, toggle() {} };
  addEventListener(name, fn) { (this.listeners[name] ??= []).push(fn); }
  append(child) { this.children.push(child); }
  setAttribute(name, value) { this.attributes[name] = value; }
  getAttribute(name) { return this.attributes[name] ?? null; }
  querySelector(selector) { return this.parts?.[selector] ?? null; }
  querySelectorAll() { return []; }
}

export async function playground({ state, app, decode = decodeState, encode = encodeState,
  waitForInit = true, fetchExamples } = {}) {
  const elements = new Map();
  const el = (id) => {
    if (!elements.has(id)) elements.set(id, new Element());
    return elements.get(id);
  };
  for (const [root, textarea] of [["pg-grammar-editor", "pg-grammar"], ["pg-input-editor", "pg-input"]]) {
    el(root).parts = { textarea: el(textarea), ".backdrop": new Element(), ".gutter": new Element() };
  }
  el("pg-unit").value = "codepoints";
  el("gen-package").value = "parser";
  el("gen-types").checked = true;
  el("pg-go").firstElementChild = new Element();
  const document = {
    getElementById: el, querySelector: el, createElement: () => new Element(),
    addEventListener() {},
  };
  const listeners = {}, timers = new Map(), calls = [], writes = [], copies = [], warnings = [];
  let timerID = 0;
  const location = { hash: state ? "#" + await encodeState(state) : "" };
  Object.defineProperty(location, "href", { get: () => "https://example.test/playground/" + location.hash });
  el("pg-share").textContent = "Share";
  class PegoClient {
    async start() { return { module: "controlled", go: "controlled" }; }
    async call(method, req) {
      calls.push({ method, req: structuredClone(req) });
      if (method === "generate") return { code: "package parser", diagnostics: [] };
      return {
        compile: { ok: true, diagnostics: [], rules: [{ name: "main" }, { name: "alt" }], types: [], start: "main" },
        matched: true, start: req.start || "main", nodes: 0, json: "", sexpr: "", errors: [],
      };
    }
  }
  const context = vm.createContext({
    ...highlighting, decodeState: decode, encodeState: encode, describeResult,
    PegoClient, siteRoot: () => "/", document, location,
    console: { ...console, warn: (...args) => warnings.push(args) },
    navigator: { clipboard: { writeText: async (text) => copies.push(text) } },
    window: { addEventListener: (name, fn) => { listeners[name] = fn; } },
    history: { replaceState: (_, __, hash) => { location.hash = hash; writes.push(hash); } },
    fetch: fetchExamples || (async () => ({ json: async () => [{ name: "sample", grammar: 'def main = "a"', input: "a" }] })),
    setTimeout: (fn, delay) => { timers.set(++timerID, { fn, delay }); return timerID; },
    clearTimeout: (id) => timers.delete(id),
  });
  const source = await readFile(app || new URL("../static/playground/app.js", import.meta.url), "utf8");
  // Bind the four imported dependencies above; execute all remaining source,
  // including init and the real hashchange listener, without copying functions.
  const script = new vm.Script(source.replace(/^import .*;\n/gm, ""), { filename: "playground/app.js" });
  const initialization = script.runInContext(context);
  const flush = async (delay) => {
    for (const [id, timer] of [...timers]) if (timer.delay === delay && timers.delete(id)) await timer.fn();
  };
  const ready = initialization.then(() => flush(0));
  const h = {
    el, calls, writes, copies, warnings, timers, location, flush, context, ready,
    async event(id, name, event = {}) {
      for (const fn of el(id).listeners[name] || []) await fn(event);
    },
    async hash(state) {
      location.hash = "#" + await encodeState(state);
      await listeners.hashchange();
      await flush(0);
    },
    dispatchHash: () => listeners.hashchange(),
  };
  if (waitForInit) await ready;
  return h;
}
