// Behavior shared by every page: the theme switch, the mobile navigation, search, and code blocks
// (highlighting, copy, and opening PEGO grammars in the playground).

import { escapeHTML, highlight, highlightJSON } from "./highlight.js";
import { encodeState } from "./state.js";
import { siteRoot } from "./pego-client.js";
import { search, snippet } from "./search.js";

const root = siteRoot();

// Old numbered optimization fragments now point to the corresponding catalog entry.
if (location.hash) {
  let id = location.hash.slice(1);
  try { id = decodeURIComponent(id); } catch { /* Keep the literal fragment. */ }
  const target = document.getElementById(id)?.dataset.movedTo;
  if (target) location.replace(target);
}

function storage(key, value) {
  try {
    if (value === undefined) return localStorage.getItem(key);
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // Storage can be unavailable (private windows, blocked site data).
  }
  return null;
}

// Theme: follows the system until the switch is used, then remembers the choice.
document.querySelector(".theme-toggle")?.addEventListener("click", () => {
  const el = document.documentElement;
  const dark = el.dataset.theme ? el.dataset.theme === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
  el.dataset.theme = dark ? "light" : "dark";
  storage("pego-theme", el.dataset.theme);
});

// Mobile navigation.
const menu = document.querySelector(".menu-toggle");
menu?.addEventListener("click", () => {
  const open = document.body.classList.toggle("nav-open");
  menu.setAttribute("aria-expanded", String(open));
});
document.addEventListener("click", (e) => {
  if (!document.body.classList.contains("nav-open")) return;
  if (e.target.closest(".sidebar, .mobile-menu, .menu-toggle")) return;
  document.body.classList.remove("nav-open");
  menu?.setAttribute("aria-expanded", "false");
});
// Scroll the navigation (not the page) to the current page.
const sidebar = document.querySelector(".sidebar");
const current = sidebar?.querySelector('[aria-current="page"]');
if (current) sidebar.scrollTop = current.offsetTop - sidebar.clientHeight / 2;

// Code blocks.
const LANGS = { pego: "pego", go: "go", json: "json", bash: "bash", console: "console", sh: "bash", shell: "bash" };
for (const code of document.querySelectorAll("pre > code")) {
  const pre = code.parentElement;
  const lang = LANGS[(code.className.match(/language-(\w+)/) || [])[1]];
  const text = code.textContent;
  if (lang === "json") code.innerHTML = highlightJSON(text);
  else if (lang) code.innerHTML = highlight(text, lang);
  if (pre.closest(".pg, .live")) continue;
  const actions = document.createElement("div");
  actions.className = "code-actions";
  if (lang === "pego" && /^\s*def\s/m.test(text)) {
    const open = document.createElement("a");
    open.textContent = "Playground";
    open.title = "Open this grammar in the playground";
    open.href = root + "playground/";
    open.addEventListener("click", async (e) => {
      e.preventDefault();
      location.href = root + "playground/#" + (await encodeState({ g: text, i: "" }));
    });
    actions.append(open);
  }
  const copy = document.createElement("button");
  copy.type = "button";
  copy.textContent = "Copy";
  copy.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(text.replace(/\n$/, ""));
      copy.textContent = "Copied";
    } catch {
      copy.textContent = "Failed";
    }
    setTimeout(() => (copy.textContent = "Copy"), 1500);
  });
  actions.append(copy);
  pre.append(actions);
}

// Table of contents: mark the section being read.
const tocLinks = [...document.querySelectorAll(".toc a")];
if (tocLinks.length && "IntersectionObserver" in window) {
  const byID = new Map(tocLinks.map((a) => [decodeURIComponent(a.hash.slice(1)), a]));
  const visible = new Set();
  const observer = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (e.isIntersecting) visible.add(e.target.id);
      else visible.delete(e.target.id);
    }
    const first = [...byID.keys()].find((id) => visible.has(id));
    if (!first) return;
    for (const a of tocLinks) a.classList.toggle("active", a === byID.get(first));
  }, { rootMargin: "-56px 0px -60% 0px" });
  for (const id of byID.keys()) {
    const el = document.getElementById(id);
    if (el) observer.observe(el);
  }
}

// Search over search-index.json, loaded on first use.
const input = document.getElementById("search-input");
const results = document.getElementById("search-results");
let index = null;
let selected = -1;

async function loadIndex() {
  if (!index) {
    index = fetch(root + "search-index.json").then((r) => r.json()).catch((err) => {
      index = null;
      throw err;
    });
  }
  return index;
}

function showResults(hits, query) {
  selected = -1;
  if (!query.trim()) {
    results.hidden = true;
    return;
  }
  if (!hits.length) {
    results.innerHTML = `<div class="r-empty">No results for “${escapeHTML(query)}”</div>`;
  } else {
    results.innerHTML = hits.map(({ p, heading, terms }) => {
      const url = root + p.u + (heading ? "#" + encodeURIComponent(heading[0]) : "");
      return `<a href="${escapeHTML(url)}" role="option"><span class="r-title">${escapeHTML(p.t)}</span><span class="r-section">${escapeHTML(p.s)}</span>` +
        (heading ? `<div class="r-heading">${escapeHTML(heading[1])}</div>` : "") +
        `<div class="r-snippet">${snippet(p.x, terms)}</div></a>`;
    }).join("");
  }
  results.hidden = false;
}

if (input && results) {
  input.addEventListener("focus", () => loadIndex().catch(() => {}));
  input.addEventListener("input", async () => {
    const q = input.value;
    try {
      const pages = await loadIndex();
      if (input.value === q) showResults(search(pages, q), q);
    } catch {
      results.innerHTML = `<div class="r-empty">The search index could not be loaded.</div>`;
      results.hidden = false;
    }
  });
  input.addEventListener("keydown", (e) => {
    const links = [...results.querySelectorAll("a")];
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!links.length) return;
      selected = (selected + (e.key === "ArrowDown" ? 1 : -1) + links.length) % links.length;
      links.forEach((a, i) => a.setAttribute("aria-selected", String(i === selected)));
      links[selected].scrollIntoView({ block: "nearest" });
    } else if (e.key === "Enter" && links.length) {
      e.preventDefault();
      location.href = links[Math.max(selected, 0)].href;
    } else if (e.key === "Escape") {
      results.hidden = true;
      input.blur();
    }
  });
  document.addEventListener("click", (e) => {
    if (!e.target.closest(".search")) results.hidden = true;
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "/" && !e.target.closest("input, textarea, select, [contenteditable]")) {
      e.preventDefault();
      input.focus();
    }
  });
}
