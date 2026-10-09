// A bounded, paged view of the worker's tree. Parsing limits do not bound DOM work.
import { escapeHTML } from "../assets/highlight.js";

const isNode = (v) => v !== null && typeof v === "object" && typeof v.type === "string";
const PAGE_SIZE = 100, INITIAL_ROWS = 400, MAX_ROWS = 5000;

function nodeChildren(n) {
  const fields = n.fields ? Object.keys(n.fields) : [];
  const values = n.children ?? [];
  return {
    length: fields.length + values.length,
    value(i) { return i < fields.length ? n.fields[fields[i]] : values[i - fields.length]; },
    entry(i) {
      return { label: i < fields.length ? fields[i] : String(i - fields.length),
        index: i >= fields.length, value: this.value(i) };
    },
  };
}

export class TreeView {
  constructor(container, { onHover, onSelect }) {
    this.el = container;
    this.doc = container.ownerDocument;
    this.onHover = onHover;
    this.onSelect = onSelect;
    this.elements = new WeakMap();
    this.root = null;
    this.current = null;
    this.rows = 0;
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

  set(root, message = "", limit = INITIAL_ROWS) {
    this.root = root;
    this.render(root, message, limit);
  }

  render(root, message = "", limit = INITIAL_ROWS) {
    this.displayRoot = root;
    this.top = null;
    this.current = null;
    this.rows = 0;
    this.el.textContent = "";
    this.elements = new WeakMap();
    if (!isNode(root)) {
      const p = this.doc.createElement("p");
      p.className = "tree-empty";
      p.textContent = message;
      this.el.append(p);
      return;
    }
    if (root !== this.root) {
      const header = this.doc.createElement("div");
      header.className = "tree-page";
      const label = this.doc.createElement("span");
      label.textContent = "Showing a subtree";
      const back = this.doc.createElement("button");
      back.type = "button";
      back.className = "link-button";
      back.textContent = "Show complete tree";
      back.addEventListener("click", () => {
        this.render(this.root);
        this.setCurrent(this.top.row);
        this.top.row.focus();
      });
      header.append(label, back);
      this.el.append(header);
      this.rows++;
      limit--;
    }
    const ul = this.doc.createElement("ul");
    ul.setAttribute("role", "tree");
    this.top = this.item({ label: null, value: root }, 0, { left: limit, auto: true });
    ul.append(this.top);
    this.el.append(ul);
  }

  item(entry, depth, work) {
    if (work.left < 1) return null;
    work.left--;
    this.rows++;
    const li = this.doc.createElement("li");
    li.setAttribute("role", "treeitem");
    const row = this.doc.createElement("div");
    row.className = "row";
    row.tabIndex = -1;
    const v = entry.value;
    const kids = isNode(v) ? nodeChildren(v) : null;
    let html = kids?.length ? '<span class="twisty" aria-hidden="true"></span>' : '<span class="twisty none" aria-hidden="true"></span>';
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
    li.row = row;
    li.depth = depth;
    if (kids?.length) {
      li.kids = kids;
      li.setAttribute("aria-expanded", "false");
      if (work.auto && depth < 3) this.expand(li, work);
    }
    return li;
  }

  clearGroup(li) {
    if (!li.group) return;
    for (const row of li.group.querySelectorAll(".row")) {
      if (row.node && this.elements.get(row.node) === row) this.elements.delete(row.node);
      if (this.current === row) this.current = null;
      this.rows--;
    }
    this.rows -= li.group.querySelectorAll(".tree-page").length;
    li.group.remove();
    li.group = null;
    li.items = null;
    li.pager = null;
  }

  // Reserve a pager before recursive expansion can spend the last row.
  populate(li, start, work, size = PAGE_SIZE) {
    if (work.left < 2) return;
    const ul = this.doc.createElement("ul");
    ul.setAttribute("role", "group");
    li.items = new Map();
    work.left--;
    let end = start;
    while (end < Math.min(li.kids.length, start + size) && work.left > 0) {
      const child = this.item(li.kids.entry(end), li.depth + 1, work);
      child.parentItem = li;
      child.entryIndex = end;
      li.items.set(end++, child);
      ul.append(child);
    }
    li.pageStart = start;
    li.pageEnd = end;
    if (start > 0 || end < li.kids.length) {
      this.rows++;
      const holder = this.doc.createElement("li");
      holder.setAttribute("role", "none");
      const pager = this.doc.createElement("div");
      pager.className = "tree-page";
      const button = (text, offset, disabled) => {
        const b = this.doc.createElement("button");
        b.type = "button";
        b.className = "link-button";
        b.textContent = text;
        b.disabled = disabled;
        b.addEventListener("click", () => this.showPage(li, offset, text));
        return b;
      };
      const previous = button("Previous children", Math.max(0, start - PAGE_SIZE), start === 0);
      const next = button("Next children", end, end === li.kids.length);
      const range = this.doc.createElement("span");
      range.textContent = `Children ${start + 1}–${end} of ${li.kids.length}`;
      pager.append(previous, range, next);
      li.pager = { previous, next };
      holder.append(pager);
      ul.append(holder);
    } else {
      work.left++; // All children fit; the reserved pager was not created.
    }
    li.group = ul;
    li.append(ul);
  }

  // Compact other materialized branches when an explicit action reaches the cap.
  // The model and the path to the requested item remain available.
  compact(li) {
    const path = [];
    for (let p = li; p.parentItem; p = p.parentItem) path.unshift(p.entryIndex);
    const focused = this.el.contains(this.doc.activeElement);
    const node = li.row.node;
    // A long ancestor chain cannot coexist with the target under a fixed cap.
    // Keep the original model for subsequent reveals and the return control.
    // Leave room for at least a child and its continuation control.
    const overhead = this.displayRoot === this.root ? 3 : 4;
    if (path.length * 2 + overhead > MAX_ROWS) {
      this.render(node, "", 2);
      if (focused) {
        this.setCurrent(this.top.row);
        this.top.row.focus();
      }
      return this.top;
    }
    this.render(this.displayRoot, "", this.displayRoot === this.root ? 1 : 2);
    const work = { left: MAX_ROWS - this.rows, auto: false };
    let target = this.top;
    for (const index of path) {
      this.populate(target, index, work, 1);
      target.setAttribute("aria-expanded", "true");
      target = target.items.get(index);
    }
    if (focused) {
      this.setCurrent(target.row);
      target.row.focus();
    }
    return target;
  }

  expand(li, work) {
    if (!li.kids) return;
    if (!li.group) {
      if (!work) {
        if (MAX_ROWS - this.rows < 2) li = this.compact(li);
        work = { left: Math.min(PAGE_SIZE + 1, MAX_ROWS - this.rows), auto: false };
      }
      this.populate(li, 0, work);
    }
    if (li.group) li.setAttribute("aria-expanded", "true");
  }

  showPage(li, start, direction) {
    this.clearGroup(li);
    this.populate(li, start, { left: Math.min(PAGE_SIZE + 1, MAX_ROWS - this.rows), auto: false });
    // A replaced page's button must not leave keyboard focus detached.
    const first = direction === "Previous children" ? li.pager?.previous : li.pager?.next;
    const other = direction === "Previous children" ? li.pager?.next : li.pager?.previous;
    (first && !first.disabled ? first : other && !other.disabled ? other : li.row).focus();
  }

  collapse(li) {
    if (li.kids) li.setAttribute("aria-expanded", "false");
  }

  expandAll() {
    const work = { left: MAX_ROWS - this.rows, auto: false };
    const walk = (li) => {
      if (!li.kids) return;
      this.expand(li, work);
      if (li.group) for (const child of li.items.values()) walk(child);
    };
    if (this.top && isNode(this.root)) walk(this.top);
  }

  collapseAll() {
    for (const li of this.el.querySelectorAll('li[aria-expanded="true"]')) this.collapse(li);
    if (this.top && isNode(this.root)) this.expand(this.top);
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
    const i = rows.indexOf(this.doc.activeElement);
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

  // Preserve last-containing-child semantics without rendering preceding siblings.
  reveal(pos) {
    if (!isNode(this.root)) return;
    const path = [];
    let n = this.root;
    for (;;) {
      const kids = nodeChildren(n);
      let index = -1;
      for (let i = 0; i < kids.length; i++) {
        const v = kids.value(i);
        if (isNode(v) && v.start <= pos && pos < v.end) index = i;
      }
      if (index < 0) break;
      path.push(index);
      n = kids.value(index);
    }
    if (path.length * 2 + 1 > MAX_ROWS) {
      this.render(n, "", 2);
      this.setCurrent(this.top.row);
      this.top.row.scrollIntoView({ block: "nearest" });
      return;
    }
    if (this.displayRoot !== this.root || MAX_ROWS - this.rows < path.length * 2) this.render(this.root, "", 1);
    const work = { left: MAX_ROWS - this.rows, auto: false };
    let li = this.top;
    for (const index of path) {
      if (!li.items?.has(index)) {
        this.clearGroup(li);
        this.populate(li, index, work, 1);
      }
      li.setAttribute("aria-expanded", "true");
      li = li.items.get(index);
    }
    this.setCurrent(li.row);
    li.row.scrollIntoView({ block: "nearest" });
  }
}
