// Exercise the page's actual fenced-code wiring against a small controlled DOM.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import { test } from "node:test";

import { highlight, highlightJSON, tokenize } from "../static/assets/highlight.js";

class FakeElement {
  constructor(tagName) {
    this.tagName = tagName;
    this.children = [];
    this.listeners = {};
    this.attributes = {};
  }
  append(...nodes) {
    for (const node of nodes) {
      node.remove();
      node.parentElement = this;
      this.children.push(node);
    }
  }
  insertBefore(node, reference) {
    node.remove();
    const index = this.children.indexOf(reference);
    node.parentElement = this;
    this.children.splice(index, 0, node);
  }
  remove() {
    if (this.parentElement) this.parentElement.children = this.parentElement.children.filter((node) => node !== this);
    this.parentElement = null;
  }
  addEventListener(type, listener) { (this.listeners[type] ??= []).push(listener); }
  async click(event = { preventDefault() {} }) {
    for (const listener of this.listeners.click || []) await listener(event);
  }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  closest() { return this.excluded ? this : null; }
}

function codeBlock(className, textContent) {
  const container = new FakeElement("article");
  const pre = new FakeElement("pre");
  const code = new FakeElement("code");
  code.className = className;
  code.textContent = textContent;
  code.innerHTML = "unchanged";
  pre.append(code);
  container.append(pre);
  return { code, pre, container };
}

async function pageHighlight(codes) {
  const source = await readFile(new URL("../static/assets/site.js", import.meta.url), "utf8");
  const start = source.indexOf("// Code blocks.");
  const end = source.indexOf("// Table of contents:", start);
  assert.ok(start >= 0 && end > start, "site.js has a code-block initialization section");
  const clipboardWrites = [];
  const encodedStates = [];
  const location = {};
  const document = {
    querySelectorAll(selector) {
      assert.equal(selector, "pre > code");
      return codes.map(({ code }) => code);
    },
    createElement(tag) { return new FakeElement(tag); },
  };
  new vm.Script(source.slice(start, end)).runInNewContext({
    document, highlight, highlightJSON, root: "/", location,
    encodeState: async (state) => (encodedStates.push(state), "encoded-state"),
    navigator: { clipboard: { async writeText(value) { clipboardWrites.push(value); } } },
    setTimeout() {},
  });
  return { clipboardWrites, encodedStates, location };
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
  const grammar = 'rule main = "hello"';
  const zp = codeBlock("language-zp", `${grammar}\n`);
  const h = await pageHighlight([ts, typescript, unknown, zp]);

  assert.match(ts.code.innerHTML, /tok-keyword">const<\/span>/);
  assert.match(ts.code.innerHTML, /tok-type">string<\/span>/);
  assert.match(ts.code.innerHTML, /tok-string">&quot;&lt;img src=x&gt;&quot;<\/span>/);
  assert.match(ts.code.innerHTML, /tok-comment">\/\/ safe<\/span>/);
  assert.doesNotMatch(ts.code.innerHTML, /<img|<script/);
  assert.match(typescript.code.innerHTML, /tok-keyword">type<\/span>/);
  assert.match(typescript.code.innerHTML, /tok-keyword">readonly<\/span>/);
  assert.equal(unknown.code.innerHTML, "unchanged", "unknown fence languages keep their original contents");

  for (const block of [ts, typescript, unknown, zp]) {
    const frame = block.pre.parentElement;
    assert.equal(frame.className, "code-frame");
    assert.equal(frame.children[0], block.pre);
    assert.equal(frame.children[1].className, "code-actions");
    assert.equal(block.pre.children[0], block.code, "controls are siblings of the horizontally scrolling pre");
  }
  const tsActions = ts.pre.parentElement.children[1];
  await tsActions.children.find((control) => control.tagName === "button").click();
  assert.deepEqual(h.clipboardWrites, [source.replace(/\n$/, "")], "Copy writes original code text, without highlight markup");

  const playground = zp.pre.parentElement.children[1].children.find((control) => control.tagName === "a");
  assert.ok(playground, "ZeroParse blocks retain their Playground action outside the pre");
  await playground.click();
  assert.equal(h.encodedStates.length, 1);
  assert.equal(h.encodedStates[0].g, `${grammar}\n`);
  assert.equal(h.encodedStates[0].i, "");
  assert.equal(h.location.href, "/playground/#encoded-state");
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


test("ZeroParse highlighting follows expressions, pragmas and nested comments", () => {
  const source = 'rule main Result = "/*" [#] {n := 1} @error(message="x") #atomic => Result{}\n'
    + '/* outer\n/* inner */ tail */\nrule expr = #operators { operand main } // line\n';
  const tokens = tokenize(source, "zp");
  const words = cls => tokens.filter(t => t.cls === cls).map(t => source.slice(t.from, t.to));
  assert.deepEqual(words("comment"), ['/* outer\n/* inner */ tail */', '// line']);
  assert.deepEqual(words("attr"), ['@error', '#atomic']);
  assert.deepEqual(words("rule"), ['main', 'expr']);
  assert.ok(words("keyword").includes('#operators'));
  assert.ok(words("punct").includes(':='));
  assert.ok(words("punct").includes('{'));
  assert.ok(words("type").includes('Result'));
  assert.deepEqual(words("class"), ['[#]']);
  assert.deepEqual(words("string"), ['"/*"', '"x"']);
  const incomplete = '/* outer /* inner */';
  assert.deepEqual(tokenize(incomplete, "zp"), [{from: 0, to: incomplete.length, cls: 'comment'}]);
});

test("ZeroParse highlighting restricts identifiers to ASCII", () => {
  const source = 'rule ab = "名前" @error(message="é") café';
  const tokens = tokenize(source, "zp");
  const names = tokens.filter((t) => ["rule", "ident", "attr"].includes(t.cls))
    .map((t) => source.slice(t.from, t.to));
  assert.ok(names.includes("ab"));
  assert.ok(names.includes("@error"));
  assert.ok(!names.includes("café"));
  assert.ok(tokens.every((t) => !source.slice(t.from, t.to).includes("é") || t.cls === "string"));
});


test("ZeroParse words are contextual and action braces restore PEG context", () => {
  const source = 'rule type = "x"\nrule main = type type:operand {type := 1} => {type; Result{}}\nrule expr = #operators { operand type; level prefix { infix left "+" => Result{} }; level { prefix type } }';
  const tokens = tokenize(source, "zp");
  const words = (cls) => tokens.filter((t) => t.cls === cls).map((t) => source.slice(t.from,t.to));
  assert.ok(words("rule").includes("type"));
  assert.ok(words("label").includes("type"));
  assert.equal(words("keyword").filter((w) => w === "type").length,0);
  assert.equal(words("keyword").filter((w) => w === "prefix").length,1);
  assert.ok(words("keyword").includes("infix"));
  assert.ok(words("keyword").includes("left"));
  assert.ok(!tokens.some((t) => t.cls === "ident"));
  const call = 'rule a = level(expr, additive)';
  const levels = tokenize(call,"zp").filter((t) => call.slice(t.from,t.to) === "level");
  assert.equal(levels[0].cls,"keyword");
});

test("S-expression highlighting marks types, labels, rules, keywords and escapes the text", () => {
  const source = '[Pair{Key: "a<b", Value: "12"@value} (&n:1 nil) error("bad") Id("x") Items["y"]]';
  const tokens = tokenize(source, "sexpr");
  const words = (cls) => tokens.filter((t) => t.cls === cls).map((t) => source.slice(t.from, t.to));
  assert.deepEqual(words("type"), ["Pair", "Id", "Items"]);
  assert.deepEqual(words("label"), ["Key", "Value", "n"]);
  assert.deepEqual(words("rule"), ["@value"]);
  assert.deepEqual(words("number"), ["1"]);
  assert.deepEqual(words("keyword"), ["nil", "error"]);
  assert.deepEqual(words("string"), ['"a<b"', '"12"', '"bad"', '"x"', '"y"']);
  const html = highlight(source, "sexpr");
  assert.ok(html.includes('<span class="tok-type">Pair</span>'));
  assert.ok(html.includes("a&lt;b"));
  assert.ok(!html.includes("a<b"), "input text is escaped");
});
