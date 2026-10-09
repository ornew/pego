import assert from "node:assert/strict";
import { test } from "node:test";
import { treeView, wide } from "./tree_harness.mjs";

const app = process.env.PEGO_TREE_APP;
const setup = () => treeView({ app });

test("initial wide siblings obey the row budget and expose a child page", async () => {
  const h = await setup(), root = wide(); h.view.set(root);
  assert(h.rows() <= 400, `created ${h.rows()} rows`);
  assert.equal(h.nodeRows().length, 101);
  assert.equal(h.container.querySelectorAll(".tree-page").length, 1);
  assert.match(h.container.textContent, /Children 1–100 of 10000/);
  assert.equal(h.view.elements.has(root.children[9999]), false);
});

test("initial rendering does not read every sibling value", async () => {
  const h = await setup(), root = wide(); let reads = 0;
  root.children = new Proxy(root.children, { get(target, key, receiver) {
    if (/^\d+$/.test(String(key))) reads++;
    return Reflect.get(target, key, receiver);
  } });
  h.view.set(root);
  assert(reads <= 100, `read ${reads} siblings`);
});

test("child page navigation replaces rows, clears stale mappings and keeps focus", async () => {
  const h = await setup(), root = wide(); h.view.set(root);
  const first = h.view.elements.get(root.children[0]); h.view.setCurrent(first); first.focus();
  let top = h.container.querySelector(":scope > ul > li");
  const next = top.pager.next;
  const before = h.doc.createdRows;
  next.click();
  assert(h.doc.createdRows - before <= 101);
  assert.equal(h.rows(), 102);
  assert.match(h.container.textContent, /Children 101–200 of 10000/);
  assert.equal(h.view.elements.has(root.children[0]), false);
  assert.equal(h.view.current, null);
  assert.equal(h.container.contains(h.doc.activeElement), true);
  assert.equal(h.doc.activeElement.textContent, "Next children");
  top.pager.previous.click();
  assert.match(h.container.textContent, /Children 1–100 of 10000/);
  assert.equal(top.pager.previous.disabled, true);
  for (let i = 0; i < 99; i++) top.pager.next.click();
  assert.match(h.container.textContent, /Children 9901–10000 of 10000/);
  assert.equal(top.pager.next.disabled, true);
  assert.equal(h.doc.activeElement.textContent, "Previous children");
  assert.equal(h.rows(), 102);
});

test("automatic recursion counts pager rows within the 400-row budget", async () => {
  const h = await setup(), root = wide(100);
  root.children = root.children.map((n) => ({ ...n, type: "List", children: wide(100).children }));
  h.view.set(root);
  assert(h.rows() <= 400, `created ${h.rows()} rows`);
  assert.equal(h.view.rows, h.rows());
  assert(h.container.querySelectorAll(".tree-page").length > 0);
});

test("Expand shown bounds live rows and repeated expansion cannot spend more", async () => {
  const h = await setup(), root = wide(100);
  root.children = root.children.map((n) => ({ ...n, type: "List", children: wide(500).children }));
  h.view.set(root);
  h.container.querySelector(":scope > ul > li").pager.next.click();
  h.view.expandAll();
  assert(h.rows() >= 4999);
  assert(h.rows() <= 5000, `created ${h.rows()} rows`);
  const count = h.rows(); h.view.expandAll();
  assert.equal(h.rows(), count);
  assert.equal(h.view.rows, h.rows());
  assert(h.container.querySelectorAll(".tree-page").length > 0);
});

for (const index of [0, 5050, 9999]) test(`reveal reaches sibling ${index} without rendering preceding siblings`, async () => {
  const h = await setup(), root = wide(); h.view.set(root);
  h.view.reveal(index + .5);
  assert.equal(h.view.current.node, root.children[index]);
  assert.equal(h.view.current.scrolled, true);
  assert(h.rows() <= 400, `created ${h.rows()} rows`);
  assert.equal(h.view.rows, h.rows());
});

test("reveal follows nested deferred siblings and preserves containment semantics", async () => {
  const h = await setup(), root = wide(), nested = wide();
  root.children[9999] = nested;
  nested.children[9999] = { type: "Error", start: 9999, end: 10000, text: "x" };
  h.view.set(root); h.view.reveal(9999.5);
  assert.equal(h.view.current.node, nested.children[9999]);
  assert(h.rows() <= 400);
  assert.equal(h.view.rows, h.rows());
  h.view.reveal(10000);
  assert.equal(h.view.current.node, root); // half-open end/EOF
});

test("fields, primitive values and overlapping node spans retain their ordering", async () => {
  const h = await setup();
  const first = { type: "Match", start: 0, end: 4, text: '<"&>' };
  const last = { type: "Match", start: 0, end: 4, text: "later" };
  const root = { type: "User", start: 0, end: 4,
    fields: { '<name>': first, scalar: 7, nothing: null }, children: [last] };
  h.view.set(root); h.view.reveal(1);
  assert.equal(h.view.current.node, last);
  const rows = h.nodeRows();
  assert.match(rows[1].innerHTML, /&lt;name&gt;/);
  assert.match(rows[1].innerHTML, /&lt;/);
  assert.match(rows[2].innerHTML, /n-value.*7/);
  assert.match(rows[3].innerHTML, /nil/);
  assert.equal(h.container.querySelectorAll(".tree-page").length, 0);
});

test("collapse and re-expand preserve a selected page without creating more rows", async () => {
  const h = await setup(), root = wide(); h.view.set(root);
  let top = h.container.querySelector(":scope > ul > li"); top.pager.next.click();
  const count = h.rows(); h.view.collapse(top);
  assert.equal(top.getAttribute("aria-expanded"), "false");
  h.view.expand(top);
  assert.equal(top.getAttribute("aria-expanded"), "true");
  assert.match(h.container.textContent, /Children 101–200 of 10000/);
  assert.equal(h.rows(), count);
});

test("manual expansion at the live cap compacts branches and retains the target path", async () => {
  const h = await setup(), root = wide(100);
  root.children = root.children.map((n) => ({ ...n, type: "List", children: wide(500).children }));
  h.view.set(root);
  h.container.querySelector(":scope > ul > li").pager.next.click();
  h.view.expandAll();
  assert(h.rows() >= 4999);
  const target = h.view.elements.get(root.children[99]);
  assert.equal(target.parentElement.getAttribute("aria-expanded"), "false");
  target.focus();
  h.view.expand(target.parentElement);
  assert(h.rows() <= 5000);
  assert.equal(h.view.rows, h.rows());
  assert.equal(h.view.elements.get(root.children[99]).parentElement.getAttribute("aria-expanded"), "true");
  assert.equal(h.doc.activeElement.node, root.children[99]);
  assert.equal(h.container.contains(h.doc.activeElement), true);
});

test("reveal at the live cap reaches a deferred target after compacting", async () => {
  const h = await setup(), root = wide(100);
  root.children = root.children.map((n, i) => ({ ...n, type: "List", start: i * 500, end: (i + 1) * 500,
    children: wide(500).children.map((v) => ({ ...v, start: v.start + i * 500, end: v.end + i * 500 })) }));
  root.end = 50000;
  h.view.set(root);
  h.container.querySelector(":scope > ul > li").pager.next.click();
  h.view.expandAll(); assert(h.rows() >= 4999);
  h.view.reveal(49999.5);
  assert.equal(h.view.current.node, root.children[99].children[499]);
  assert(h.rows() <= 5000);
  assert.equal(h.view.rows, h.rows());
});

test("paths deeper than the row cap reveal a subtree with a return control", async () => {
  const h = await setup();
  const leaf = { type: "Match", start: 0, end: 1, text: "x" };
  let root = leaf;
  for (let i = 0; i < 6000; i++) root = { type: "Seq", start: 0, end: 1, children: [root] };
  h.view.set(root); h.view.reveal(.5);
  assert.equal(h.view.current.node, leaf);
  assert.equal(h.view.root, root);
  assert(h.rows() <= 5000);
  assert.equal(h.view.rows, h.rows());
  const back = h.container.querySelector("button");
  assert.equal(back.textContent, "Show complete tree"); back.click();
  assert.equal(h.view.elements.get(root).parentElement, h.container.querySelector(":scope > ul > li"));
  assert.equal(h.doc.activeElement.node, root);
  assert(h.rows() <= 400);
  h.view.reveal(1); assert.equal(h.view.current.node, root);
});

test("deep manual expansion continues through a bounded subtree window", async () => {
  const h = await setup();
  let root = { type: "Match", start: 0, end: 1, text: "x" };
  for (let i = 0; i < 6000; i++) root = { type: "Seq", start: 0, end: 1, children: [root] };
  h.view.set(root, "", 1);
  let node = root;
  for (let i = 0; i < 5500; i++) {
    h.view.expand(h.view.elements.get(node).parentElement);
    node = node.children[0];
    assert(h.view.elements.has(node));
    assert(h.view.rows <= 5000);
  }
  assert.notEqual(h.view.displayRoot, root);
  assert.equal(h.view.root, root);
  assert.equal(h.view.rows, h.rows());
  h.container.querySelector("button").click();
  assert.equal(h.view.displayRoot, root);
  assert(h.rows() <= 400);
});

test("compaction reserves expansion room at the exact ancestor-cap boundary", async () => {
  const h = await setup();
  const target = { type: "Seq", start: 0, end: 1,
    children: [{ type: "Match", start: 0, end: 1, text: "x" }] };
  let root = target;
  for (let i = 0; i < 2499; i++) root = { type: "Seq", start: 0, end: 2,
    children: [root, { type: "Match", start: 1, end: 2, text: "other" }] };
  h.view.set(root); h.view.expandAll();
  assert.equal(h.view.rows, 4999);
  const row = h.view.elements.get(target);
  assert.equal(row.parentElement.getAttribute("aria-expanded"), "false");
  h.view.expand(row.parentElement);
  assert.equal(h.view.elements.get(target).parentElement.getAttribute("aria-expanded"), "true");
  assert.equal(h.view.displayRoot, target);
  assert.equal(h.view.rows, h.rows());
  assert(h.rows() <= 5000);
});

test("tree keys still select nodes while native pager keys are not intercepted", async () => {
  const h = await setup(), root = wide(); h.view.set(root);
  const top = h.container.querySelector(":scope > ul > li");
  const first = h.view.elements.get(root.children[0]); first.focus();
  assert.equal(h.key("ArrowDown"), true);
  assert.equal(h.view.current.node, root.children[1]);
  h.key("Enter"); assert.equal(h.selected.at(-1).n, root.children[1]);
  top.pager.next.focus(); assert.equal(h.key("ArrowDown"), false);
});

test("new parse results replace pages and empty results show their message", async () => {
  const h = await setup(), root = wide(); h.view.set(root); h.view.reveal(9999.5);
  h.view.set({ type: "Match", start: 0, end: 1, text: "new" });
  assert.equal(h.rows(), 1); assert.equal(h.view.current, null);
  assert.equal(h.view.elements.has(root.children[9999]), false);
  h.view.set(null, "No tree");
  assert.equal(h.rows(), 0); assert.equal(h.container.textContent, "No tree");
  assert.doesNotThrow(() => { h.view.expandAll(); h.view.collapseAll(); h.view.reveal(0); });
});
