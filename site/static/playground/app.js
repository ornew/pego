// The PEGO playground: edit a grammar and an input, and see the parse result as you type.
// Parsing runs in a Web Worker (worker.js) with pego.wasm; see docs/guide/playground.md.

import { PegoClient, siteRoot } from "../assets/pego-client.js";
import { escapeHTML, highlight, highlightJSON, tokenize } from "../assets/highlight.js";
import { decodeState, encodeState } from "../assets/state.js";
import { describeResult } from "./status.js";

const $ = (id) => document.getElementById(id);
const client = new PegoClient(siteRoot() + "playground/");

// ---------------------------------------------------------------- Editors

// renderText renders text as HTML with syntax tokens and marks ({from, to, cls}; a mark with
// from === to is drawn as a caret-like point).
function renderText(text, tokens, marks) {
  const cuts = new Set([0, text.length]);
  for (const t of tokens) {
    cuts.add(t.from);
    cuts.add(t.to);
  }
  const clamp = (x) => Math.max(0, Math.min(text.length, x));
  const ranges = [];
  const points = [];
  for (const m of marks) {
    const from = clamp(m.from);
    const to = clamp(m.to);
    if (to > from) {
      ranges.push({ from, to, cls: m.cls });
      cuts.add(from);
      cuts.add(to);
    } else {
      points.push({ at: from, cls: m.cls });
    }
  }
  const sorted = [...cuts].sort((a, b) => a - b);
  let out = "";
  let ti = 0;
  for (let k = 0; k + 1 < sorted.length; k++) {
    const a = sorted[k];
    const b = sorted[k + 1];
    for (const p of points) if (p.at === a) out += `<span class="${p.cls} point"></span>`;
    while (ti < tokens.length && tokens[ti].to <= a) ti++;
    const cls = [];
    if (ti < tokens.length && tokens[ti].from <= a && tokens[ti].to >= b) cls.push("tok-" + tokens[ti].cls);
    for (const m of ranges) if (m.from <= a && m.to >= b) cls.push(m.cls);
    const s = escapeHTML(text.slice(a, b));
    out += cls.length ? `<span class="${cls.join(" ")}">${s}</span>` : s;
  }
  for (const p of points) if (p.at >= text.length) out += `<span class="${p.cls} point"></span>`;
  return out;
}

const PLAIN_LIMIT = 200000; // above this many characters, the editor shows plain text without marks

class Editor {
  constructor(root, { language = null, indent = "    " } = {}) {
    this.root = root;
    this.language = language;
    this.indent = indent;
    this.ta = root.querySelector("textarea");
    this.backdrop = root.querySelector(".backdrop");
    this.gutter = root.querySelector(".gutter");
    this.gutterInner = document.createElement("div");
    this.gutterInner.className = "gutter-inner";
    this.gutter.append(this.gutterInner);
    this.marks = [];
    this.errorLines = new Set();
    this.escaped = false;
    this.ta.addEventListener("input", () => this.render());
    this.ta.addEventListener("scroll", () => this.syncScroll());
    this.ta.addEventListener("keydown", (e) => this.onKey(e));
  }

  get value() {
    return this.ta.value;
  }

  set value(v) {
    this.ta.value = v;
    this.ta.scrollTop = 0;
    this.ta.scrollLeft = 0;
    this.render();
  }

  // replaceAll replaces the text as an edit, so that it can be undone where the browser supports it.
  replaceAll(text) {
    const top = this.ta.scrollTop;
    this.ta.focus();
    this.ta.select();
    if (!document.execCommand("insertText", false, text)) {
      this.ta.value = text;
      this.ta.dispatchEvent(new Event("input", { bubbles: true }));
    }
    this.ta.setSelectionRange(0, 0);
    this.ta.scrollTop = top;
  }

  onKey(e) {
    if (e.key === "Escape") {
      this.escaped = true;
      return;
    }
    if (e.key === "Tab" && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey && !this.escaped) {
      e.preventDefault();
      if (!document.execCommand("insertText", false, this.indent)) {
        this.ta.setRangeText(this.indent, this.ta.selectionStart, this.ta.selectionEnd, "end");
        this.ta.dispatchEvent(new Event("input", { bubbles: true }));
      }
    }
    this.escaped = false;
  }

  setMarks(marks, errorLines = this.errorLines) {
    this.marks = marks;
    this.errorLines = errorLines;
    this.render();
  }

  render() {
    const text = this.ta.value;
    const plain = text.length > PLAIN_LIMIT;
    this.root.classList.toggle("plain", plain);
    let lines = 1;
    for (let i = text.indexOf("\n"); i >= 0; i = text.indexOf("\n", i + 1)) lines++;
    const nums = new Array(lines);
    for (let i = 0; i < lines; i++) nums[i] = this.errorLines.has(i + 1) ? `<span class="err-line">${i + 1}</span>` : String(i + 1);
    this.gutterInner.innerHTML = nums.join("\n");
    this.backdrop.innerHTML = plain ? "" : renderText(text, this.language ? tokenize(text, this.language) : [], this.marks) + "\n ";
    this.syncScroll();
  }

  syncScroll() {
    const x = this.ta.scrollLeft;
    const y = this.ta.scrollTop;
    this.backdrop.style.transform = `translate(${-x}px, ${-y}px)`;
    this.gutterInner.style.transform = `translateY(${-y}px)`;
  }

  // reveal scrolls so that the UTF-16 offset is visible, without moving the focus.
  reveal(offset) {
    const before = this.ta.value.slice(0, offset);
    const line = before.split("\n").length - 1;
    const lh = parseFloat(getComputedStyle(this.ta).lineHeight) || 20;
    const top = line * lh;
    if (top < this.ta.scrollTop || top > this.ta.scrollTop + this.ta.clientHeight - 2 * lh) {
      this.ta.scrollTop = Math.max(0, top - this.ta.clientHeight / 3);
    }
  }

  focusAt(from, to = from) {
    this.ta.focus();
    this.ta.setSelectionRange(from, to);
    this.reveal(from);
  }
}

// ---------------------------------------------------------------- Positions

// positionMap converts between positions in the parse's unit (code points or bytes) and UTF-16
// offsets in a JavaScript string.
function positionMap(text, unit) {
  const toU16 = [];
  const fromU16 = new Uint32Array(text.length + 1);
  let p = 0;
  for (let i = 0; i < text.length;) {
    const cp = text.codePointAt(i);
    const w = cp > 0xffff ? 2 : 1;
    const size = unit === "bytes" ? (cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4) : 1;
    for (let k = 0; k < size; k++) toU16.push(i);
    for (let k = 0; k < w; k++) fromU16[i + k] = p;
    p += size;
    i += w;
  }
  toU16.push(text.length);
  fromU16[text.length] = p;
  return {
    toU16: (pos) => toU16[Math.max(0, Math.min(pos, toU16.length - 1))],
    fromU16: (i) => fromU16[Math.max(0, Math.min(i, text.length))],
  };
}

// lineColOffset returns the UTF-16 offset of a 1-based line and column counted in code points.
function lineColOffset(text, line, col) {
  let i = 0;
  for (let l = 1; l < line; l++) {
    const nl = text.indexOf("\n", i);
    if (nl < 0) return text.length;
    i = nl + 1;
  }
  for (let c = 1; c < col && i < text.length && text[i] !== "\n"; c++) {
    i += text.codePointAt(i) > 0xffff ? 2 : 1;
  }
  return i;
}

// errorMark marks the character at offset, or a point where there is no character to mark.
function errorMark(text, offset, cls = "m-error") {
  if (offset < text.length && text[offset] !== "\n") {
    const w = text.codePointAt(offset) > 0xffff ? 2 : 1;
    return { from: offset, to: offset + w, cls };
  }
  return { from: offset, to: offset, cls };
}

function lineOf(text, offset) {
  let line = 1;
  for (let i = text.indexOf("\n"); i >= 0 && i < offset; i = text.indexOf("\n", i + 1)) line++;
  return line;
}

// ---------------------------------------------------------------- Tree view

const isNode = (v) => v !== null && typeof v === "object" && typeof v.type === "string";

function nodeChildren(n) {
  const out = [];
  if (n.fields) for (const [k, v] of Object.entries(n.fields)) out.push({ label: k, value: v });
  if (n.children) n.children.forEach((v, i) => out.push({ label: String(i), index: true, value: v }));
  return out;
}

class TreeView {
  constructor(container, { onHover, onSelect }) {
    this.el = container;
    this.onHover = onHover;
    this.onSelect = onSelect;
    this.elements = new WeakMap(); // node -> row element
    this.root = null;
    this.current = null;
    container.addEventListener("click", (e) => this.onClick(e));
    container.addEventListener("dblclick", (e) => {
      const row = e.target.closest(".row");
      if (row?.node) this.onSelect(row.node, true);
    });
    container.addEventListener("mouseover", (e) => {
      const row = e.target.closest(".row");
      this.onHover(row?.node ?? null);
    });
    container.addEventListener("mouseleave", () => this.onHover(null));
    container.addEventListener("keydown", (e) => this.onKey(e));
  }

  set(root, message = "") {
    this.root = root;
    this.current = null;
    this.el.textContent = "";
    this.elements = new WeakMap();
    if (!isNode(root)) {
      const p = document.createElement("p");
      p.className = "tree-empty";
      p.textContent = message;
      this.el.append(p);
      return;
    }
    const ul = document.createElement("ul");
    ul.setAttribute("role", "tree");
    this.budget = 400; // rows expanded automatically
    ul.append(this.item({ label: null, value: root }, 0));
    this.el.append(ul);
  }

  item(entry, depth) {
    const li = document.createElement("li");
    li.setAttribute("role", "treeitem");
    const row = document.createElement("div");
    row.className = "row";
    row.tabIndex = -1;
    const v = entry.value;
    let html = "";
    const kids = isNode(v) ? nodeChildren(v) : [];
    html += kids.length ? '<span class="twisty" aria-hidden="true"></span>' : '<span class="twisty none" aria-hidden="true"></span>';
    if (entry.label !== null) html += `<span class="n-label${entry.index ? " index" : ""}">${escapeHTML(entry.label)}${entry.index ? "" : ":"}</span> `;
    if (isNode(v)) {
      html += `<span class="n-type n-type-${/^(Match|Seq|List|Operator|Error)$/.test(v.type) ? "builtin" : "user"}${v.type === "Error" ? " n-error" : ""}">${escapeHTML(v.type)}</span>`;
      if (v.rule) html += `<span class="n-rule">@${escapeHTML(v.rule)}</span>`;
      if (v.text !== undefined) {
        const t = v.text.length > 80 ? v.text.slice(0, 80) + "…" : v.text;
        html += ` <span class="n-text">${escapeHTML(JSON.stringify(t))}</span>`;
      }
      html += ` <span class="n-span">${v.start}–${v.end}</span>`;
      row.node = v;
      this.elements.set(v, row);
    } else {
      html += `<span class="n-value">${escapeHTML(v === null ? "nil" : JSON.stringify(v))}</span>`;
    }
    row.innerHTML = html;
    li.append(row);
    if (kids.length) {
      li.kids = kids;
      li.depth = depth;
      if (depth < 3 && this.budget > 0) this.expand(li);
      else li.setAttribute("aria-expanded", "false");
    }
    return li;
  }

  expand(li) {
    if (!li.kids) return;
    if (!li.querySelector(":scope > ul")) {
      const ul = document.createElement("ul");
      ul.setAttribute("role", "group");
      for (const k of li.kids) {
        this.budget--;
        ul.append(this.item(k, li.depth + 1));
      }
      li.append(ul);
    }
    li.setAttribute("aria-expanded", "true");
  }

  collapse(li) {
    if (li.kids) li.setAttribute("aria-expanded", "false");
  }

  expandAll() {
    this.budget = 0;
    let count = 0;
    const walk = (li) => {
      if (count++ > 5000) return; // keep very large trees usable
      if (li.kids) {
        this.expand(li);
        for (const child of li.querySelectorAll(":scope > ul > li")) walk(child);
      }
    };
    for (const li of this.el.querySelectorAll(":scope > ul > li")) walk(li);
  }

  collapseAll() {
    for (const li of this.el.querySelectorAll('li[aria-expanded="true"]')) this.collapse(li);
    const top = this.el.querySelector(":scope > ul > li");
    if (top) this.expand(top);
  }

  onClick(e) {
    const row = e.target.closest(".row");
    if (!row) return;
    const li = row.parentElement;
    if (e.target.closest(".twisty") && li.kids) {
      if (li.getAttribute("aria-expanded") === "true") this.collapse(li);
      else this.expand(li);
      return;
    }
    this.setCurrent(row);
    if (row.node) this.onSelect(row.node, false);
  }

  onKey(e) {
    const rows = [...this.el.querySelectorAll(".row")].filter((r) => r.offsetParent !== null);
    const i = rows.indexOf(document.activeElement);
    if (i < 0) return;
    const li = rows[i].parentElement;
    let next = null;
    switch (e.key) {
      case "ArrowDown": next = rows[i + 1]; break;
      case "ArrowUp": next = rows[i - 1]; break;
      case "ArrowRight": if (li.kids) this.expand(li); break;
      case "ArrowLeft":
        if (li.getAttribute("aria-expanded") === "true") this.collapse(li);
        else next = li.parentElement.closest("li")?.querySelector(":scope > .row");
        break;
      case "Enter": if (rows[i].node) this.onSelect(rows[i].node, true); break;
      default: return;
    }
    e.preventDefault();
    if (next) {
      this.setCurrent(next);
      next.focus();
      if (next.node) this.onHover(next.node);
    }
  }

  setCurrent(row) {
    this.current?.classList.remove("current");
    this.current = row;
    row?.classList.add("current");
    row?.setAttribute("tabindex", "0");
  }

  // reveal expands the path to the deepest node that contains pos and marks it.
  reveal(pos) {
    if (!isNode(this.root)) return;
    const path = [this.root];
    for (;;) {
      const n = path[path.length - 1];
      let next = null;
      for (const k of nodeChildren(n)) {
        const v = k.value;
        if (isNode(v) && v.start <= pos && pos < v.end) next = v;
      }
      if (!next) break;
      path.push(next);
    }
    for (const n of path.slice(0, -1)) {
      const row = this.elements.get(n);
      if (row) this.expand(row.parentElement);
    }
    const row = this.elements.get(path[path.length - 1]);
    if (row) {
      this.setCurrent(row);
      row.scrollIntoView({ block: "nearest" });
    }
  }
}

// ---------------------------------------------------------------- State

const grammarEditor = new Editor($("pg-grammar-editor"), { language: "pego", indent: "    " });
const inputEditor = new Editor($("pg-input-editor"), { indent: "\t" });
const startSelect = $("pg-start");
const unitSelect = $("pg-unit");
const backendSelect = $("pg-backend");
const recognizeBox = $("pg-recognize");
const exampleSelect = $("pg-example");
const statusEl = $("pg-status");
const errorsEl = $("pg-errors");

let last = null; // the last parse result
let lastInput = ""; // the input it was parsed from
let positions = null; // positionMap of lastInput
let selectedNode = null;
let hoverNode = null;
let requestedStart = ""; // the start rule chosen by the user; empty for the default
let examples = [];

const tree = new TreeView($("pg-tree"), {
  onHover(node) {
    hoverNode = node;
    updateInputMarks();
  },
  onSelect(node, focus) {
    selectedNode = node;
    updateInputMarks();
    if (!positions || lastInput !== inputEditor.value) return;
    const from = positions.toU16(node.start);
    const to = positions.toU16(node.end);
    if (focus) inputEditor.focusAt(from, to);
    else inputEditor.reveal(from);
  },
});

function updateInputMarks() {
  const marks = [];
  const text = inputEditor.value;
  const fresh = positions && lastInput === text;
  if (fresh && last) {
    for (const e of last.errors ?? []) marks.push(errorMark(text, positions.toU16(e.pos), last.recovered ? "m-warn" : "m-error"));
    for (const n of [selectedNode, hoverNode]) {
      if (n) marks.push({ from: positions.toU16(n.start), to: positions.toU16(n.end), cls: n === hoverNode ? "m-hover" : "m-select" });
    }
  }
  const lines = new Set();
  if (fresh && last) for (const e of last.errors ?? []) lines.add(e.line);
  inputEditor.setMarks(marks, lines);
}

function setStatus(kind, html) {
  statusEl.className = "pg-status " + kind;
  statusEl.innerHTML = html;
}

function showErrors(items) {
  errorsEl.textContent = "";
  errorsEl.hidden = items.length === 0;
  for (const it of items) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "err-item " + (it.kind || "error");
    b.innerHTML = `<span class="err-where">${escapeHTML(it.where)}</span> <span class="err-msg">${escapeHTML(it.message)}</span>` +
      (it.detail ? `<span class="err-detail">${escapeHTML(it.detail)}</span>` : "");
    if (it.go) b.addEventListener("click", it.go);
    else b.disabled = true;
    errorsEl.append(b);
  }
}


// ---------------------------------------------------------------- Parsing

let seq = 0;
let parseTimer = 0;

function scheduleParse(delay = 150) {
  clearTimeout(parseTimer);
  parseTimer = setTimeout(parse, delay);
}

async function parse() {
  const n = ++seq;
  const grammar = grammarEditor.value;
  const input = inputEditor.value;
  const req = {
    grammar,
    input,
    start: requestedStart,
    unit: unitSelect.value,
    backend: backendSelect.value,
    recognize: recognizeBox.checked,
  };
  const slow = setTimeout(() => n === seq && setStatus("busy", "Parsing…"), 300);
  let res;
  try {
    res = await client.call("parse", req);
  } catch (err) {
    clearTimeout(slow);
    if (n !== seq) return;
    setStatus("error", `✗ ${escapeHTML(err.message || String(err))}`);
    return;
  } finally {
    clearTimeout(slow);
  }
  if (n !== seq) return;
  apply(res, req);
  saveState();
}

function apply(res, req) {
  const c = res.compile;
  if (!c) {
    setStatus("error", `✗ ${escapeHTML(res.error || "unexpected response")}`);
    return;
  }
  updateRules(c);

  // Grammar diagnostics.
  const gtext = grammarEditor.value;
  const gmarks = [];
  const glines = new Set();
  for (const d of c.diagnostics) {
    if (d.line > 0) {
      gmarks.push(errorMark(gtext, lineColOffset(gtext, d.line, d.col)));
      glines.add(d.line);
    }
  }
  grammarEditor.setMarks(gmarks, glines);
  $("pg-grammar-info").textContent = c.ok ? `${c.rules.length} rules, ${c.types.length} types` : "";

  if (!c.ok) {
    $("pg-output-root").classList.add("stale");
    setStatus("error", `✗ The grammar has ${c.diagnostics.length === 1 ? "an error" : c.diagnostics.length + " errors"}`);
    showErrors(c.diagnostics.map((d) => ({
      where: d.line ? `grammar ${d.line}:${d.col}` : "grammar",
      message: d.message,
      go: d.line ? () => grammarEditor.focusAt(lineColOffset(grammarEditor.value, d.line, d.col)) : null,
    })));
    return;
  }
  $("pg-output-root").classList.remove("stale");

  last = res;
  lastInput = req.input;
  positions = positionMap(req.input, req.unit);
  selectedNode = null;
  hoverNode = null;

  const tree0 = res.json ? JSON.parse(res.json) : null;
  const { status, empty } = describeResult(res, req);
  tree.set(tree0, empty);
  $("pg-json").innerHTML = res.json ? (res.json.length < 300000 ? highlightJSON(res.json) : escapeHTML(res.json)) : "";
  $("pg-sexpr").textContent = res.sexpr || "";
  updateInputMarks();
  setStatus(status.kind, escapeHTML(status.text));
  const errs = res.errors ?? [];
  showErrors([
    ...(res.error ? [{ where: "parse", message: res.error }] : []),
    ...errs.map((e) => ({
      kind: res.recovered ? "warn" : "error",
      where: `input ${e.line}:${e.col}${res.recovered ? " (recovered)" : ""}`,
      message: e.message,
      detail: e.messages?.length && e.expected?.length ? "expected " + e.expected.join(", ") : "",
      go: () => {
        if (lastInput === inputEditor.value) {
          const at = positions.toU16(e.pos);
          inputEditor.focusAt(at);
        }
      },
    })),
  ]);
  $("pg-input-info").textContent = `${[...req.input].length} characters`;
  if (activeTab === "go") scheduleGenerate();
}

let rulesKey = "";
function updateRules(c) {
  // A requested start rule that the grammar does not define (after an edit, or from a link) stays
  // selected, marked, and the parse reports it, rather than silently parsing from another rule.
  const missing = requestedStart && !c.rules.some((r) => r.name === requestedStart) ? requestedStart : "";
  const key = c.rules.map((r) => r.name).join(" ") + "|" + c.start + "|" + missing;
  if (key !== rulesKey && (c.rules.length || missing)) {
    rulesKey = key;
    startSelect.textContent = "";
    for (const r of c.rules) {
      const o = document.createElement("option");
      o.value = r.name;
      o.textContent = r.name === c.start ? `${r.name} (default)` : r.name;
      startSelect.append(o);
    }
    if (missing) {
      const o = document.createElement("option");
      o.value = missing;
      o.textContent = `${missing} (not defined)`;
      startSelect.append(o);
    }
  }
  if (c.rules.length || missing) startSelect.value = requestedStart || c.start;

  // Rules and types tab.
  const out = $("pg-outline");
  if (!c.rules.length && !c.types.length) {
    out.innerHTML = '<p class="tree-empty">No rules.</p>';
    return;
  }
  let html = "";
  if (c.package) html += `<p>package <code>${escapeHTML(c.package)}</code></p>`;
  html += '<h3>Rules</h3><table class="outline"><thead><tr><th>Rule</th><th>Type</th><th>Line</th><th></th></tr></thead><tbody>';
  for (const r of c.rules) {
    html += `<tr><td><button type="button" class="link-button mono" data-line="${r.line}" data-col="${r.col}">${escapeHTML(r.name)}</button></td>` +
      `<td><code>${escapeHTML(r.type || "")}</code></td><td>${r.line}</td>` +
      `<td><button type="button" class="link-button" data-start="${escapeHTML(r.name)}">${r.name === startSelect.value ? "start rule" : "parse from here"}</button></td></tr>`;
  }
  html += "</tbody></table>";
  if (c.types.length) {
    html += '<h3>Types</h3><table class="outline"><thead><tr><th>Type</th><th>Kind</th><th>Definition</th></tr></thead><tbody>';
    for (const t of c.types) {
      html += `<tr><td><button type="button" class="link-button mono" data-line="${t.line}" data-col="${t.col}">${escapeHTML(t.name)}</button></td>` +
        `<td>${escapeHTML(t.kind)}</td><td><code>${escapeHTML(t.spec)}</code></td></tr>`;
    }
    html += "</tbody></table>";
  }
  out.innerHTML = html;
}

$("pg-outline").addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (!b) return;
  if (b.dataset.line) grammarEditor.focusAt(lineColOffset(grammarEditor.value, +b.dataset.line, +b.dataset.col));
  if (b.dataset.start) {
    requestedStart = b.dataset.start;
    scheduleParse(0);
  }
});

// ---------------------------------------------------------------- Generated Go

let genTimer = 0;
let genSeq = 0;
function scheduleGenerate() {
  clearTimeout(genTimer);
  genTimer = setTimeout(generate, 250);
}

async function generate() {
  const n = ++genSeq;
  const pkg = $("gen-package").value.trim() || "parser";
  const req = {
    grammar: grammarEditor.value,
    package: pkg,
    start: startSelect.value,
    types: $("gen-types").checked,
    recognize: $("gen-recognize").checked,
  };
  const flags = [`-pkg ${pkg}`];
  if (req.start && req.start !== "main") flags.push(`-s ${req.start}`);
  if (req.types) flags.push("-types");
  if (req.recognize) flags.push("-recognize");
  $("gen-cmd").innerHTML = `<code>pego gen -g grammar.pego ${escapeHTML(flags.join(" "))}</code>`;
  const code = $("pg-go").firstElementChild;
  try {
    const res = await client.call("generate", req);
    if (n !== genSeq) return;
    if (res.diagnostics?.length) {
      code.textContent = res.diagnostics.map((d) => (d.line ? `${d.line}:${d.col}: ` : "") + d.message).join("\n");
    } else {
      code.innerHTML = res.code.length < 400000 ? highlight(res.code, "go") : escapeHTML(res.code);
    }
  } catch (err) {
    if (n === genSeq) code.textContent = String(err.message || err);
  }
}

for (const id of ["gen-package", "gen-types", "gen-recognize"]) $(id).addEventListener("input", scheduleGenerate);

// ---------------------------------------------------------------- Tabs

let activeTab = "tree";
const tabs = ["tree", "json", "sexpr", "grammar", "go"];
function selectTab(name) {
  activeTab = name;
  for (const t of tabs) {
    $("tab-" + t).setAttribute("aria-selected", String(t === name));
    $("tab-" + t).tabIndex = t === name ? 0 : -1;
    $("panel-" + t).hidden = t !== name;
  }
  if (name === "go") generate();
  saveState();
}
for (const t of tabs) $("tab-" + t).addEventListener("click", () => selectTab(t));
document.querySelector(".tabs").addEventListener("keydown", (e) => {
  if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
  const i = tabs.indexOf(activeTab) + (e.key === "ArrowRight" ? 1 : -1);
  const t = tabs[(i + tabs.length) % tabs.length];
  selectTab(t);
  $("tab-" + t).focus();
});

$("tree-expand").addEventListener("click", () => tree.expandAll());
$("tree-collapse").addEventListener("click", () => tree.collapseAll());

$("pg-copy").addEventListener("click", async () => {
  const text = {
    tree: last?.sexpr ?? "",
    json: last?.json ?? "",
    sexpr: last?.sexpr ?? "",
    grammar: $("pg-outline").innerText,
    go: $("pg-go").innerText,
  }[activeTab];
  await copyText(text, $("pg-copy"));
});

async function copyText(text, button) {
  const label = button.textContent;
  try {
    await navigator.clipboard.writeText(text);
    button.textContent = "Copied";
  } catch {
    button.textContent = "Copy failed";
  }
  setTimeout(() => (button.textContent = label), 1500);
}

// ---------------------------------------------------------------- State in the URL

let saveTimer = 0;
function saveState() {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(writeHash, 400);
}

async function writeHash() {
  const s = { g: grammarEditor.value, i: inputEditor.value };
  if (requestedStart) s.s = requestedStart;
  if (unitSelect.value !== "codepoints") s.u = unitSelect.value;
  if (backendSelect.value) s.b = backendSelect.value;
  if (recognizeBox.checked) s.r = 1;
  if (activeTab !== "tree") s.t = activeTab;
  const hash = "#" + (await encodeState(s));
  if (location.hash !== hash) history.replaceState(null, "", hash);
}

function applyState(s) {
  grammarEditor.value = s.g ?? "";
  inputEditor.value = s.i ?? "";
  requestedStart = s.s ?? "";
  unitSelect.value = s.u === "bytes" ? "bytes" : "codepoints";
  backendSelect.value = ["closure", "bytecode", "bytecode-iterative"].includes(s.b) ? s.b : "";
  recognizeBox.checked = !!s.r;
  if (s.u || s.b || s.r) document.querySelector(".pg-options").open = true;
  selectTab(tabs.includes(s.t) ? s.t : "tree");
}

function loadExample(name) {
  const ex = examples.find((e) => e.name === name);
  if (!ex) return false;
  applyState({ g: ex.grammar, i: ex.input });
  exampleSelect.value = name;
  scheduleParse(0);
  return true;
}

// ---------------------------------------------------------------- Wiring

grammarEditor.ta.addEventListener("input", () => {
  exampleSelect.value = "";
  scheduleParse();
  saveState();
});
inputEditor.ta.addEventListener("input", () => {
  updateInputMarks(); // drops marks that no longer match the text
  scheduleParse();
  saveState();
});
for (const el of [unitSelect, backendSelect, recognizeBox]) el.addEventListener("change", () => scheduleParse(0));
startSelect.addEventListener("change", () => {
  requestedStart = startSelect.value;
  scheduleParse(0);
});
exampleSelect.addEventListener("change", () => {
  if (exampleSelect.value) loadExample(exampleSelect.value);
});

// Moving the caret in the input shows the node under it.
let caretTimer = 0;
document.addEventListener("selectionchange", () => {
  if (document.activeElement !== inputEditor.ta || activeTab !== "tree") return;
  clearTimeout(caretTimer);
  caretTimer = setTimeout(() => {
    if (!positions || lastInput !== inputEditor.value) return;
    tree.reveal(positions.fromU16(inputEditor.ta.selectionStart));
  }, 120);
});

$("pg-format").addEventListener("click", async () => {
  try {
    const res = await client.call("format", { grammar: grammarEditor.value });
    if (res.diagnostics?.length) {
      setStatus("error", "✗ Fix the errors in the grammar before formatting it");
      return;
    }
    if (res.formatted !== grammarEditor.value) grammarEditor.replaceAll(res.formatted);
  } catch (err) {
    setStatus("error", `✗ ${escapeHTML(err.message || String(err))}`);
  }
});

$("pg-share").addEventListener("click", async () => {
  clearTimeout(saveTimer);
  await writeHash();
  await copyText(location.href, $("pg-share"));
});

window.addEventListener("hashchange", async () => {
  const s = await decodeState(location.hash);
  if (!s) return;
  if (s.example) loadExample(s.example);
  else if (s.g !== grammarEditor.value || s.i !== inputEditor.value) {
    applyState(s);
    scheduleParse(0);
  }
});

client.onrestart = (reason) => {
  if (reason !== "timeout") console.warn("pego: restarting the parser:", reason);
};

async function init() {
  try {
    examples = await (await fetch("examples.json")).json();
  } catch {
    examples = [];
  }
  for (const ex of examples) {
    const o = document.createElement("option");
    o.value = ex.name;
    o.textContent = `${ex.name} — ${ex.description}`;
    exampleSelect.append(o);
  }
  const s = await decodeState(location.hash);
  if (s?.example && loadExample(s.example)) {
    // loaded
  } else if (s && !s.example) {
    applyState(s);
  } else if (examples.length) {
    loadExample(examples[0].name);
  }
  grammarEditor.render();
  inputEditor.render();
  try {
    const v = await client.start();
    const rev = v.revision && !v.module.includes(v.revision.slice(0, 12)) ? ` · revision <code>${escapeHTML(v.revision.slice(0, 12))}${v.modified ? "+" : ""}</code>` : "";
    $("pg-version").innerHTML = `PEGO ${escapeHTML(v.module)} running as WebAssembly, built with ${escapeHTML(v.go)}${rev}. Everything runs in your browser.`;
  } catch (err) {
    setStatus("error", `✗ The parser could not be loaded: ${escapeHTML(err.message || String(err))}`);
    return;
  }
  scheduleParse(0);
}

init();
