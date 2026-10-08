// Tests of the site search (static/assets/search.js). Run by TestJavaScript.

import assert from "node:assert/strict";
import { test } from "node:test";

import { markTerms, search, snippet } from "../static/assets/search.js";

test("terms are never marked inside escaped characters", () => {
  assert.equal(markTerms('def x = "a" -> $a', ["quot", "gt"]), "def x = &quot;a&quot; -&gt; $a");
  assert.equal(markTerms('say "quote"', ["quot"]), "say &quot;<mark>quot</mark>e&quot;");
  assert.equal(markTerms("a < b & c", ["b"]), "a &lt; <mark>b</mark> &amp; c");
});

test("terms are marked whatever their case, longest first", () => {
  assert.equal(markTerms("Pratt and pratt-level", ["pratt", "pratt-level"]), "<mark>Pratt</mark> and <mark>pratt-level</mark>");
  assert.equal(markTerms("a.b", ["."]), "a<mark>.</mark>b");
});

test("snippets show the text around the first match", () => {
  const text = "x".repeat(100) + ' the "#recover" attribute ' + "y".repeat(200);
  const s = snippet(text, ["recover"]);
  assert.ok(s.startsWith("…"));
  assert.ok(s.endsWith("…"));
  assert.match(s, /the &quot;#<mark>recover<\/mark>&quot; attribute/);
  assert.equal(snippet("short", ["nothing"]), "");
  assert.equal(snippet("a & b", ["b"]), "a &amp; <mark>b</mark>");
});

test("search ranks titles first and requires every term", () => {
  const pages = [
    { t: "Errors", u: "e/", s: "Guides", h: [["recover", "Recovering with #recover"]], x: "about #recover and errors" },
    { t: "Recover", u: "r/", s: "Spec", h: [], x: "the recover attribute" },
    { t: "Other", u: "o/", s: "Spec", h: [], x: "nothing here" },
  ];
  const hits = search(pages, "recover");
  assert.deepEqual(hits.map((h) => h.p.u), ["r/", "e/"]);
  assert.deepEqual(hits[1].heading, ["recover", "Recovering with #recover"]);
  assert.deepEqual(search(pages, "recover nothing").map((h) => h.p.u), []);
  assert.deepEqual(search(pages, "  "), []);
});
