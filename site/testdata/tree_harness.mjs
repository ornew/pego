// A small DOM for tree row-count/interaction checks, not a browser layout model.
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import { escapeHTML } from "../static/assets/highlight.js";
import { TreeView } from "../static/playground/tree.js";

class Element {
  constructor(tag, doc) {
    this.tagName = tag;
    this.ownerDocument = doc;
    this.parentElement = null;
    this.children = [];
    this.attributes = {};
    this.listeners = {};
    this.className = "";
    this.classList = {
      add: (name) => { if (!this.matches('.' + name)) this.className += ' ' + name; },
      remove: (name) => { this.className = this.className.split(/\s+/).filter((x) => x !== name).join(' '); },
    };
  }
  set className(value) {
    this._className = value;
    if (!this.countedRow && /(?:^|\s)(row|tree-page)(?:\s|$)/.test(value)) {
      this.countedRow = true;
      this.ownerDocument.createdRows++;
    }
  }
  get className() { return this._className; }
  set textContent(value) { this._text = value; for (const c of this.children) c.parentElement = null; this.children = []; }
  get textContent() { return (this._text || "") + this.children.map((c) => c.textContent).join(""); }
  append(...children) { for (const c of children) { c.remove(); c.parentElement = this; this.children.push(c); } }
  remove() {
    if (this.parentElement) this.parentElement.children = this.parentElement.children.filter((c) => c !== this);
    this.parentElement = null;
  }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  getAttribute(name) { return this.attributes[name] ?? null; }
  addEventListener(name, listener) { (this.listeners[name] ??= []).push(listener); }
  click() { if (!this.disabled) for (const listener of this.listeners.click || []) listener({ target: this }); }
  focus() { this.ownerDocument.activeElement = this; }
  scrollIntoView() { this.scrolled = true; }
  contains(other) { for (let p = other; p; p = p.parentElement) if (p === this) return true; return false; }
  matches(selector) {
    if (selector[0] === ".") return this.className.split(/\s+/).includes(selector.slice(1));
    const m = selector.match(/^([\w-]+)(?:\[([\w-]+)="([^"]+)"\])?$/);
    return !!m && this.tagName === m[1] && (!m[2] || this.getAttribute(m[2]) === m[3]);
  }
  closest(selector) { for (let p = this; p; p = p.parentElement) if (p.matches(selector)) return p; return null; }
  querySelectorAll(selector) {
    if (selector.startsWith(":scope > ")) {
      let found = [this];
      for (const part of selector.slice(9).split(" > ")) found = found.flatMap((el) => el.children.filter((c) => c.matches(part)));
      return found;
    }
    const found = [];
    for (const c of this.children) {
      if (c.matches(selector)) found.push(c);
      found.push(...c.querySelectorAll(selector));
    }
    return found;
  }
  querySelector(selector) { return this.querySelectorAll(selector)[0] ?? null; }
  get offsetParent() {
    for (let p = this.parentElement; p; p = p.parentElement) {
      if (p.tagName === "ul" && p.parentElement?.getAttribute("aria-expanded") === "false") return null;
    }
    return this.parentElement;
  }
}

export async function treeView({ app, tree } = {}) {
  const doc = { activeElement: null, createdRows: 0,
    createElement(tag) { return new Element(tag, this); } };
  const container = doc.createElement("div");
  let Type = TreeView;
  if (app || tree) {
    let source = await readFile(app || tree, "utf8");
    if (app) {
      const start = source.indexOf("const isNode =");
      const end = source.indexOf("// ---------------------------------------------------------------- State", start);
      if (start < 0 || end < 0) throw new Error("Parent app has no tree section");
      source = source.slice(start, end);
    } else {
      source = source.replace(/^import .*;\n/gm, "").replace("export class TreeView", "class TreeView");
    }
    Type = new vm.Script(source + "\nTreeView;").runInNewContext({ document: doc, escapeHTML });
  }
  const selected = [], hovered = [];
  const view = new Type(container, { onHover: (n) => hovered.push(n), onSelect: (n, focus) => selected.push({ n, focus }) });
  return { view, container, doc, selected, hovered,
    rows: () => container.querySelectorAll(".row").length + container.querySelectorAll(".tree-page").length,
    nodeRows: () => container.querySelectorAll(".row"),
    key(key) { let prevented = false; view.onKey({ key, preventDefault() { prevented = true; } }); return prevented; },
  };
}

export function wide(count = 10000) {
  return { type: "List", start: 0, end: count,
    children: Array.from({ length: count }, (_, i) => ({ type: "Match", start: i, end: i + 1, text: "x" })) };
}
