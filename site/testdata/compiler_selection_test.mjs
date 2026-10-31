import assert from "node:assert/strict";
import { test } from "node:test";
import { playground } from "./playground_harness.mjs";

const tinygoWasm = "wasm/zp-1234567890abcdef.wasm";

test("Go remains the default when both binaries are present", async () => {
  const h = await playground({ tinygoWasm });
  assert.equal(h.el("pg-compiler").value, "go");
  assert.equal(h.el("pg-compiler-tinygo").disabled, false);
  assert.equal(Object.keys(h.clients[0].options).length, 0);
  await h.event("pg-compiler", "change");
  assert.equal(h.location.assigned, "https://example.test/playground/");
  h.el("pg-compiler").value = "tinygo";
  await h.event("pg-compiler", "change");
  assert.equal(h.location.assigned, "https://example.test/playground/?compiler=tinygo");
});

test("TinyGo selection loads its binary and stays in shared links", async () => {
  const h = await playground({ compiler: "tinygo", tinygoWasm });
  assert.equal(h.el("pg-compiler").value, "tinygo");
  assert.equal(h.clients[0].options.wasm, "https://example.test/playground/" + tinygoWasm);
  await h.event("pg-share", "click");
  assert.match(h.copies[0], /\?compiler=tinygo#/);
});

test("a TinyGo link on a Go-only site reports unavailable", async () => {
  const h = await playground({ compiler: "tinygo" });
  assert.equal(h.clients.length, 0);
  assert.equal(h.el("pg-compiler-tinygo").disabled, true);
  assert.match(h.el("pg-status").innerHTML, /TinyGo is unavailable/);
});
