import assert from "node:assert/strict";
import { test } from "node:test";
import { treeView, wide } from "./tree_harness.mjs";

const app = process.env.ZP_TREE_APP;
const setup = () => treeView({ app });

test("initial wide siblings obey the row budget and expose a child page", async () => {
  const h = await setup(), root = wide(); h.view.set(root);
  assert(h.rows() <= 400, `created ${h.rows()} rows`);
  assert.equal(h.nodeRows().length, 101);
  assert.equal(h.container.querySelectorAll(".tree-page").length, 1);
  assert.match(h.container.textContent, /Children 1–100 of 10000/);
  assert.equal(h.view.elements.has(root.value[9999]), false);
});

test("initial rendering does not read every sibling value", async () => {
  const h = await setup(), root = wide(); let reads = 0;
  root.value = new Proxy(root.value, { get(target, key, receiver) {
    if (/^\d+$/.test(String(key))) reads++;
    return Reflect.get(target, key, receiver);
  } });
  h.view.set(root);
  assert(reads <= 100, `read ${reads} siblings`);
});

test("child page navigation replaces rows, clears stale mappings and keeps focus", async () => {
  const h = await setup(), root = wide(); h.view.set(root);
  const first = h.view.elements.get(root.value[0]); h.view.setCurrent(first); first.focus();
  let top = h.container.querySelector(":scope > ul > li");
  const next = top.pager.next;
  const before = h.doc.createdRows;
  next.click();
  assert(h.doc.createdRows - before <= 101);
  assert.equal(h.rows(), 102);
  assert.match(h.container.textContent, /Children 101–200 of 10000/);
  assert.equal(h.view.elements.has(root.value[0]), false);
  assert.equal(h.view.current, top.row);
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
  root.value = root.value.map((n) => ({ ...n, value: wide(100).value }));
  h.view.set(root);
  assert(h.rows() <= 400, `created ${h.rows()} rows`);
  assert.equal(h.view.rows, h.rows());
  assert(h.container.querySelectorAll(".tree-page").length > 0);
});

test("Expand shown bounds live rows and repeated expansion cannot spend more", async () => {
  const h = await setup(), root = wide(100);
  root.value = root.value.map((n) => ({ ...n, value: wide(500).value }));
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
  assert.equal(h.view.current.node, root.value[index]);
  assert.equal(h.view.current.scrolled, true);
  assert(h.rows() <= 400, `created ${h.rows()} rows`);
  assert.equal(h.view.rows, h.rows());
});

test("reveal follows nested deferred siblings and preserves containment semantics", async () => {
  const h = await setup(), root = wide(), nested = wide();
  root.value[9999] = nested;
  nested.value[9999] = { start: 9999, end: 10000, error: "x" };
  h.view.set(root); h.view.reveal(9999.5);
  assert.equal(h.view.current.node, nested.value[9999]);
  assert(h.rows() <= 400);
  assert.equal(h.view.rows, h.rows());
  h.view.reveal(10000);
  assert.equal(h.view.current.node, root); // half-open end/EOF
});

test("fields, primitive values and overlapping node spans retain their ordering", async () => {
  const h = await setup();
  const first = { start: 0, end: 4, value: '<"&>' };
  const last = { start: 0, end: 4, value: "later" };
  const root = { type: "User", fields: { '<name>': first, scalar: 7, nothing: null, last } };
  h.view.set(root); h.view.reveal(1);
  assert.equal(h.view.current.node, last);
  const rows = h.nodeRows();
  assert.match(rows[1].innerHTML, /&lt;name&gt;/);
  assert.match(rows[1].innerHTML, /&lt;/);
  assert.match(rows[2].innerHTML, /n-value.*7/);
  assert.match(rows[3].innerHTML, /nil/);
  assert.equal(h.container.querySelectorAll(".tree-page").length, 0);
});

test("captures label the elements they sit on and captures beside the tuple follow them", async () => {
  const h = await setup();
  const key = { start: 0, end: 1, value: "k" }, side = { start: 1, end: 2, value: "s" };
  const root = { start: 0, end: 3, rule: "pair",
    value: [key, { start: 1, end: 2, error: "expected <x>" }, [1, true], null],
    captures: { k: { path: [0] }, look: { value: side } } };
  h.view.set(root); h.view.expandAll();
  const rows = h.nodeRows();
  assert.match(rows[0].innerHTML, /\(4\)<\/span><span class="n-rule">@pair/);
  assert.match(rows[1].innerHTML, /n-label">k:/);
  assert.match(rows[2].innerHTML, /n-label index">1.*n-error.*expected &lt;x&gt;/);
  assert.match(rows[3].innerHTML, /\[2\]/);
  assert.match(rows[4].innerHTML, /n-value">1/);
  assert.match(rows[6].innerHTML, /nil/);
  assert.match(rows[7].innerHTML, /n-label">&amp;look:/);
  h.view.reveal(1.5);
  assert.equal(h.view.current.node, side); // the last element containing the position
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
  root.value = root.value.map((n) => ({ ...n, value: wide(500).value }));
  h.view.set(root);
  h.container.querySelector(":scope > ul > li").pager.next.click();
  h.view.expandAll();
  assert(h.rows() >= 4999);
  const target = h.view.elements.get(root.value[99]);
  assert.equal(target.parentElement.getAttribute("aria-expanded"), "false");
  target.focus();
  h.view.expand(target.parentElement);
  assert(h.rows() <= 5000);
  assert.equal(h.view.rows, h.rows());
  assert.equal(h.view.elements.get(root.value[99]).parentElement.getAttribute("aria-expanded"), "true");
  assert.equal(h.doc.activeElement.node, root.value[99]);
  assert.equal(h.container.contains(h.doc.activeElement), true);
});

test("reveal at the live cap reaches a deferred target after compacting", async () => {
  const h = await setup(), root = wide(100);
  root.value = root.value.map((n, i) => ({ ...n, start: i * 500, end: (i + 1) * 500,
    value: wide(500).value.map((v) => ({ ...v, start: v.start + i * 500, end: v.end + i * 500 })) }));
  root.end = 50000;
  h.view.set(root);
  h.container.querySelector(":scope > ul > li").pager.next.click();
  h.view.expandAll(); assert(h.rows() >= 4999);
  h.view.reveal(49999.5);
  assert.equal(h.view.current.node, root.value[99].value[499]);
  assert(h.rows() <= 5000);
  assert.equal(h.view.rows, h.rows());
});

test("paths deeper than the row cap reveal a subtree with a return control", async () => {
  const h = await setup();
  const leaf = { start: 0, end: 1, value: "x" };
  let root = leaf;
  for (let i = 0; i < 6000; i++) root = { start: 0, end: 1, value: [root] };
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
  let root = { start: 0, end: 1, value: "x" };
  for (let i = 0; i < 6000; i++) root = { start: 0, end: 1, value: [root] };
  h.view.set(root, "", 1);
  let node = root;
  for (let i = 0; i < 5500; i++) {
    h.view.expand(h.view.elements.get(node).parentElement);
    node = node.value[0];
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
  const target = { start: 0, end: 1,
    value: [{ start: 0, end: 1, value: "x" }] };
  let root = target;
  for (let i = 0; i < 2499; i++) root = { start: 0, end: 2,
    value: [root, { start: 1, end: 2, value: "other" }] };
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
  const first = h.view.elements.get(root.value[0]); first.focus();
  assert.equal(h.key("ArrowDown"), true);
  assert.equal(h.view.current.node, root.value[1]);
  h.key("Enter"); assert.equal(h.selected.at(-1).n, root.value[1]);
  top.pager.next.focus(); assert.equal(h.key("ArrowDown"), false);
});

test("new parse results replace pages and empty results show their message", async () => {
  const h = await setup(), root = wide(); h.view.set(root); h.view.reveal(9999.5);
  h.view.set({ start: 0, end: 1, value: "new" });
  assert.equal(h.rows(), 1); assert.equal(h.view.current, h.nodeRows()[0]);
  assert.equal(h.view.elements.has(root.value[9999]), false);
  h.view.set(null, "No tree");
  assert.equal(h.rows(), 0); assert.equal(h.container.textContent, "No tree");
  assert.doesNotThrow(() => { h.view.expandAll(); h.view.collapseAll(); h.view.reveal(0); });
});


function tabStops(h) {
  return h.nodeRows().filter((row) => row.tabIndex === 0 && row.offsetParent !== null);
}

test("initial tree entry and navigation maintain one roving tab stop", async () => {
  const h = await setup(), root = wide(3); h.view.set(root);
  const rows = h.nodeRows();
  assert.deepEqual(tabStops(h), [rows[0]]);
  assert.equal(h.doc.activeElement, null); // Rendering does not steal editor focus.
  rows[0].focus();
  h.key("ArrowRight");
  assert.equal(h.doc.activeElement, rows[1]);
  assert.deepEqual(tabStops(h), [rows[1]]);
  h.key("ArrowDown"); h.key("ArrowUp");
  assert.equal(h.doc.activeElement, rows[1]);
  h.key("Enter");
  assert.deepEqual(h.selected.at(-1), { n: root.value[0], focus: true });
  h.key("ArrowLeft");
  assert.equal(h.doc.activeElement, rows[0]);
  h.key("ArrowLeft");
  assert.equal(rows[0].parentElement.getAttribute("aria-expanded"), "false");
  assert.deepEqual(tabStops(h), [rows[0]]);
  h.key("ArrowRight");
  assert.equal(h.doc.activeElement, rows[0]); // First Right expands; second enters.
  h.key("ArrowRight");
  assert.equal(h.doc.activeElement, rows[1]);
  assert.deepEqual(tabStops(h), [rows[1]]);
});

test("native row focus, selection and collapsing descendants keep a visible entry", async () => {
  const h = await setup(), root = wide(3); h.view.set(root);
  const top = h.container.querySelector(":scope > ul > li");
  const child = h.view.elements.get(root.value[1]); child.focus();
  assert.equal(h.view.current, child);
  assert.deepEqual(tabStops(h), [child]);
  h.view.collapse(top);
  assert.equal(h.doc.activeElement, top.row);
  assert.deepEqual(tabStops(h), [top.row]);
  h.view.expand(top);
  h.view.setCurrent(child);
  const outside = h.doc.createElement("button"); outside.focus();
  h.view.collapse(top);
  assert.equal(h.doc.activeElement, outside);
  assert.deepEqual(tabStops(h), [top.row]);
  h.view.set(null, "No tree");
  assert.deepEqual(tabStops(h), []);
});
