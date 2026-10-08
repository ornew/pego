// Tests of the site's JavaScript modules that do not need a browser. Run by TestJavaScript in
// site_test.go (node --test).

import assert from "node:assert/strict";
import { test } from "node:test";

import { describeResult } from "../static/playground/status.js";

const base = { start: "main", micros: 10 };

test("a start rule without a value matches", () => {
  const { status, empty } = describeResult({ ...base, matched: true }, {});
  assert.equal(status.kind, "ok");
  assert.match(status.text, /Matched without a value/);
  assert.match(empty, /without producing a value/);
});

test("a failed parse without syntax errors shows its error", () => {
  const { status, empty } = describeResult({ ...base, matched: false, error: "action in main: variable depth is not defined" }, {});
  assert.equal(status.kind, "error");
  assert.match(status.text, /variable depth is not defined/);
  assert.match(empty, /does not match/);
  assert.match(describeResult({ ...base, matched: false }, {}).status.text, /does not match/);
});

test("a syntax error", () => {
  const { status } = describeResult({ ...base, matched: false, errors: [{ line: 2, col: 3, message: "x" }] }, {});
  assert.equal(status.text, "✗ Syntax error at 2:3 · start main · 10 µs");
});

test("recognition that recovered from errors does not count nodes", () => {
  const { status, empty } = describeResult({ ...base, matched: true, recovered: true, errors: [{}, {}] }, { recognize: true });
  assert.equal(status.kind, "warn");
  assert.equal(status.text, "⚠ Recovered from 2 errors · start main · 10 µs");
  assert.match(empty, /Recognition mode/);
});

test("a full parse that recovered counts nodes", () => {
  const { status } = describeResult({ ...base, matched: true, recovered: true, errors: [{}], json: "{}", nodes: 5 }, {});
  assert.equal(status.text, "⚠ Recovered from 1 error · 5 nodes · start main · 10 µs");
});

test("a match whose tree cannot be shown", () => {
  const res = { ...base, matched: true, nodes: 9000, error: "the tree is nested 6000 levels deep" };
  const { status, empty } = describeResult(res, {});
  assert.equal(status.kind, "warn");
  assert.match(status.text, /Matched, but the tree is nested 6000 levels deep/);
  assert.match(empty, /nested 6000 levels deep/);
});

test("a successful parse", () => {
  const { status, empty } = describeResult({ ...base, matched: true, json: "{}", nodes: 1, micros: 1500 }, {});
  assert.equal(status.text, "✓ Matched · 1 node · start main · 1.50 ms");
  assert.equal(empty, "");
});
