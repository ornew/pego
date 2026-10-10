import assert from "node:assert/strict";
import { test } from "node:test";
import { installLegacyOptimizationRedirects } from "../static/assets/legacy-redirect.js";

function harness(hash = "") {
  const targets = new Map([
    ["77-inline-node-expressions", "../optimizations/077-inline-node-expressions/#77-inline-node-expressions"],
  ]);
  const replaced = [];
  const listeners = new Map();
  const win = {
    location: {
      hash,
      replace(target) { replaced.push(target); },
    },
    document: {
      getElementById(id) {
        const target = targets.get(id);
        return target ? { dataset: { movedTo: target } } : null;
      },
    },
    addEventListener(type, listener) { listeners.set(type, listener); },
  };
  installLegacyOptimizationRedirects(win);
  return { win, replaced, listeners };
}

test("redirects a legacy fragment during page startup", () => {
  const h = harness("#77-inline-node-expressions");
  assert.deepEqual(h.replaced, ["../optimizations/077-inline-node-expressions/#77-inline-node-expressions"]);
  assert.equal(h.listeners.has("hashchange"), true);
});

test("redirects legacy fragments after later navigation", () => {
  const h = harness();
  h.win.location.hash = "#77-inline-node-expressions";
  h.listeners.get("hashchange")();
  assert.deepEqual(h.replaced, ["../optimizations/077-inline-node-expressions/#77-inline-node-expressions"]);
});

test("leaves unknown and malformed fragments alone", () => {
  const h = harness("#unknown");
  h.listeners.get("hashchange")();
  h.win.location.hash = "#%E0%A4%A";
  h.listeners.get("hashchange")();
  assert.deepEqual(h.replaced, []);
});
