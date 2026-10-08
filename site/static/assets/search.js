// Search over search-index.json: ranking and result snippets. It has no dependencies, so that it can
// be tested in Node.

import { escapeHTML } from "./highlight.js";

// search returns the best pages for the query: every term must appear in the title, a heading or the
// text, and matches in titles and headings count more.
export function search(pages, query) {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!terms.length) return [];
  const hits = [];
  for (const p of pages) {
    const title = p.t.toLowerCase();
    const text = (p.lx ??= p.x.toLowerCase());
    let score = 0;
    let heading = null;
    let ok = true;
    for (const t of terms) {
      let s = 0;
      if (title.includes(t)) s += title.startsWith(t) ? 30 : 20;
      for (const h of p.h) {
        if (h[1].toLowerCase().includes(t)) {
          s += 8;
          heading ??= h;
        }
      }
      let count = 0;
      for (let i = text.indexOf(t); i >= 0 && count < 10; i = text.indexOf(t, i + t.length)) count++;
      s += count;
      if (s === 0) {
        ok = false;
        break;
      }
      score += s;
    }
    if (ok) hits.push({ p, score, heading });
  }
  hits.sort((a, b) => b.score - a.score);
  return hits.slice(0, 12).map((h) => ({ ...h, terms }));
}

const quote = (t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

// markTerms returns text as HTML with each occurrence of a term in <mark>. Terms are found in the
// text itself and the text around them is escaped separately, so that a term never matches inside
// an escaped character ("quot" in &quot;).
export function markTerms(text, terms) {
  const ts = terms.filter(Boolean);
  if (!ts.length) return escapeHTML(text);
  const re = new RegExp(ts.map(quote).sort((a, b) => b.length - a.length).join("|"), "gi");
  let out = "";
  let i = 0;
  for (const m of text.matchAll(re)) {
    out += escapeHTML(text.slice(i, m.index)) + "<mark>" + escapeHTML(m[0]) + "</mark>";
    i = m.index + m[0].length;
  }
  return out + escapeHTML(text.slice(i));
}

// snippet returns, as HTML, the text around the first occurrence of a term, with the terms marked.
export function snippet(text, terms) {
  const ts = terms.filter(Boolean);
  if (!ts.length) return "";
  const m = new RegExp(ts.map(quote).join("|"), "i").exec(text);
  if (!m) return "";
  const start = Math.max(0, m.index - 50);
  const end = Math.min(text.length, m.index + 110);
  return (start > 0 ? "…" : "") + markTerms(text.slice(start, end), ts) + (end < text.length ? "…" : "");
}
