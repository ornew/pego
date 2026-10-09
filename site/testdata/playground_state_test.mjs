import assert from "node:assert/strict";
import { test } from "node:test";
import { playground } from "./playground_harness.mjs";

const base = { g: 'def main = "é" $$\ndef alt = "x" $$', i: "é" };
const app = process.env.PEGO_PLAYGROUND_APP;
const parseCalls = (h) => h.calls.filter((c) => c.method === "parse");

for (const [name, field, value, request] of [
  ["closure backend", "b", "closure", "backend"],
  ["bytecode backend", "b", "bytecode", "backend"],
  ["iterative backend", "b", "bytecode-iterative", "backend"],
  ["byte positions", "u", "bytes", "unit"],
  ["recognition", "r", 1, "recognize"],
  ["start rule", "s", "alt", "start"],
  ["missing start rule", "s", "missing", "start"],
]) test(`incoming shared link changes only ${name}`, async () => {
  const h = await playground({ state: base, app });
  await h.hash({ ...base, [field]: value });
  assert.equal(parseCalls(h).length, 2, "one new parse for changed options");
  assert.equal(parseCalls(h).at(-1).req[request], request === "recognize" ? true : value);
  const control = { b: "pg-backend", u: "pg-unit", r: "pg-recognize", s: "pg-start" }[field];
  assert.equal(field === "r" ? h.el(control).checked : h.el(control).value, field === "r" ? true : value);
  if (field === "s") assert.match(h.el("pg-status").innerHTML, new RegExp(`start ${value}`));
  if (field === "r") assert.match(h.el("pg-status").innerHTML, /The input matches/);
  // Removing the option from the next link restores the default and reparses.
  await h.hash(base);
  assert.equal(parseCalls(h).length, 3);
  assert.equal(parseCalls(h).at(-1).req[request], { backend: "", unit: "codepoints", recognize: false, start: "" }[request]);
});

for (const tab of ["json", "sexpr", "grammar", "go"]) test(`tab-only link selects ${tab} without a parse`, async () => {
  const h = await playground({ state: base, app });
  h.el("pg-grammar").scrollTop = 31;
  h.el("pg-input").scrollLeft = 17;
  await h.hash({ ...base, t: tab });
  assert.equal(h.el("tab-" + tab).getAttribute("aria-selected"), "true");
  assert.equal(h.el("panel-" + tab).hidden, false);
  assert.equal(h.el("panel-tree").hidden, true);
  assert.equal(parseCalls(h).length, 1);
  assert.equal(h.el("pg-grammar").scrollTop, 31);
  assert.equal(h.el("pg-input").scrollLeft, 17);
  if (tab === "go") assert.equal(h.calls.filter((c) => c.method === "generate").length, 1);
  await h.hash(base);
  assert.equal(h.el("tab-tree").getAttribute("aria-selected"), "true");
  assert.equal(parseCalls(h).length, 1);
});

test("unchanged and normalized default links do not parse again", async () => {
  const h = await playground({ state: base, app });
  await h.hash(base);
  await h.hash({ ...base, s: "", b: "unsupported", u: "unsupported", r: 0, t: "unsupported" });
  assert.equal(parseCalls(h).length, 1);
  assert.equal(h.el("pg-backend").value, "");
  assert.equal(h.el("pg-unit").value, "codepoints");
});

test("grammar/input changes and named examples still parse", async () => {
  const h = await playground({ state: base, app });
  await h.hash({ ...base, g: 'def main = "a"' });
  assert.equal(parseCalls(h).at(-1).req.grammar, 'def main = "a"');
  await h.hash({ ...base, i: "x" });
  assert.equal(parseCalls(h).at(-1).req.input, "x");
  await h.hash({ example: "sample" });
  assert.equal(parseCalls(h).at(-1).req.input, "a");
  assert.equal(h.el("pg-example").value, "sample");
  assert.equal(parseCalls(h).length, 4);
});
