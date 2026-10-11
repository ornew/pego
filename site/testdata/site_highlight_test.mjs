// Exercise the page's actual fenced-code wiring against a small controlled DOM.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import { test } from "node:test";

import { highlight, highlightJSON, tokenize } from "../static/assets/highlight.js";

function codeBlock(className, textContent) {
  const pre = {
    children: [],
    closest() { return null; },
    append(child) { this.children.push(child); },
  };
  return {
    className,
    textContent,
    innerHTML: "unchanged",
    parentElement: pre,
  };
}

async function pageHighlight(codes) {
  const source = await readFile(new URL("../static/assets/site.js", import.meta.url), "utf8");
  const start = source.indexOf("// Code blocks.");
  const end = source.indexOf("// Table of contents:", start);
  assert.ok(start >= 0 && end > start, "site.js has a code-block initialization section");
  const document = {
    querySelectorAll(selector) {
      assert.equal(selector, "pre > code");
      return codes;
    },
    createElement(tag) {
      return {
        tagName: tag,
        addEventListener() {},
        append() {},
        setAttribute() {},
      };
    },
  };
  new vm.Script(source.slice(start, end)).runInNewContext({ document, highlight, highlightJSON });
}

test("TypeScript tokenizer recognizes declarations, types, operators and comments", () => {
  const source = `interface Parser<T> {\n  readonly parse: (input: string) => Result<T>;\n}\n// keep this comment`;
  const tokens = tokenize(source, "typescript");
  const tagged = (cls) => tokens.map((token) => token.cls === cls ? source.slice(token.from, token.to) : null).filter(Boolean);
  assert.deepEqual(tagged("keyword"), ["interface", "readonly"]);
  assert.deepEqual(tagged("type"), ["Parser", "T", "string", "Result", "T"]);
  assert.deepEqual(tagged("punct"), ["<", ">", "{", ":", "(", ":", ")", "=>", "<", ">", ";", "}"]);
  assert.deepEqual(tagged("comment"), ["// keep this comment"]);
});

test("TypeScript highlighting escapes code and both fence aliases use the page highlighter", async () => {
  const source = `const text: string = "<img src=x>"; // safe\n`;
  const ts = codeBlock("language-ts", source);
  const typescript = codeBlock("language-typescript", "type Box<T> = { readonly value: T };");
  const unknown = codeBlock("language-rust", "<script>alert(1)</script>");
  await pageHighlight([ts, typescript, unknown]);

  assert.match(ts.innerHTML, /tok-keyword">const<\/span>/);
  assert.match(ts.innerHTML, /tok-type">string<\/span>/);
  assert.match(ts.innerHTML, /tok-string">&quot;&lt;img src=x&gt;&quot;<\/span>/);
  assert.match(ts.innerHTML, /tok-comment">\/\/ safe<\/span>/);
  assert.doesNotMatch(ts.innerHTML, /<img|<script/);
  assert.match(typescript.innerHTML, /tok-keyword">type<\/span>/);
  assert.match(typescript.innerHTML, /tok-keyword">readonly<\/span>/);
  assert.equal(unknown.innerHTML, "unchanged", "unknown fence languages keep their original contents");
  assert.equal(ts.parentElement.children.length, 1, "copy controls remain attached to highlighted fences");
});

// This is a lexical highlighter, so ordinary identifiers must remain ordinary
// even when punctuation resembles a type annotation or generic expression.
test("TypeScript highlighting preserves ordinary identifiers and source text", () => {
  const source = 'const text = first, second;\nconst value = left < right ? yes : no;\ntype box = string;\n';
  const tokens = tokenize(source, "typescript");
  assert.deepEqual(tokens.filter(token => token.cls === "type").map(token => source.slice(token.from, token.to)), ["box", "string"]);
  const rendered = highlight(source, "typescript");
  const decoded = rendered.replace(/<span class="tok-[a-z]+">|<\/span>/g, "")
    .replace(/&quot;/g, '\"').replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");
  assert.equal(decoded, source);
});
