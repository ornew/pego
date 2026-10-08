// A small syntax highlighter for the languages of the documentation: pego, go, json and shell.
// tokenize returns non-overlapping tokens {from, to, cls}; highlight returns HTML.

const PEGO_KEYWORDS = new Set([
  "def", "type", "struct", "terminal", "package", "pratt", "level", "infix", "prefix", "postfix",
  "left", "right", "none", "operand", "skip", "new", "nil", "true", "false",
]);
const GO_KEYWORDS = new Set([
  "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for", "func",
  "go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select", "struct",
  "switch", "type", "var", "nil", "true", "false", "iota",
]);
const GO_TYPES = new Set([
  "bool", "byte", "error", "float32", "float64", "int", "int8", "int16", "int32", "int64", "rune",
  "string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "any",
]);

const rules = {
  pego: [
    ["comment", /\/\/[^\n]*/y],
    ["string", /"(?:[^"\\\n]|\\.)*"?/y],
    ["class", /\(\?(?:[^)\\\n]|\\.)*\)?/y],
    ["attr", /#[A-Za-z_][\w]*/y],
    ["capture", /\$(?:\$|[A-Za-z_][\w]*|\d+)/y],
    ["number", /\d+/y],
    ["ident", /[\p{L}_][\p{L}\p{N}_]*/uy],
    ["punct", /->|=>|==|!=|<=|>=|&&|\|\||_\|_|\^\^|[=\/*+?&!@:\-^|<>%.,()[\]{}]/y],
  ],
  go: [
    ["comment", /\/\/[^\n]*|\/\*[\s\S]*?(?:\*\/|$)/y],
    ["string", /"(?:[^"\\\n]|\\.)*"?|`[^`]*`?|'(?:[^'\\\n]|\\.)*'?/y],
    ["number", /\d[\w.]*/y],
    ["ident", /[\p{L}_][\p{L}\p{N}_]*/uy],
  ],
  json: [
    ["string", /"(?:[^"\\\n]|\\.)*"?/y],
    ["number", /-?\d[\d.eE+-]*/y],
    ["keyword", /\b(?:true|false|null)\b/y],
  ],
  bash: [
    ["comment", /(?:^|(?<=\s))#[^\n]*/y],
    ["string", /"(?:[^"\\]|\\.)*"?|'[^']*'?/y],
    ["prompt", /^\$ /my],
  ],
};
rules.console = rules.bash;
rules.sh = rules.bash;
rules.shell = rules.bash;

export function tokenize(code, lang) {
  const rs = rules[lang];
  if (!rs) return [];
  const tokens = [];
  let i = 0;
  let prevWord = "";
  const n = code.length;
  while (i < n) {
    let matched = false;
    for (const [cls, re] of rs) {
      re.lastIndex = i;
      const m = re.exec(code);
      if (!m || m[0].length === 0) continue;
      const text = m[0];
      let c = cls;
      if (cls === "ident") {
        c = classifyIdent(lang, text, code, i + text.length, prevWord);
        prevWord = text;
      } else if (cls !== "comment") {
        prevWord = "";
      }
      if (c) tokens.push({ from: i, to: i + text.length, cls: c });
      i += text.length;
      matched = true;
      break;
    }
    if (!matched) i++; // white space and anything the language does not color
  }
  return tokens;
}

function classifyIdent(lang, word, code, end, prev) {
  if (lang === "pego") {
    if (PEGO_KEYWORDS.has(word)) return "keyword";
    if (prev === "def") return "rule";
    // A label: name directly followed by a colon (but not a rule type "def x: T").
    if (code[end] === ":" && code[end + 1] !== "=") return "label";
    if (/^\p{Lu}/u.test(word)) return "type";
    return null;
  }
  if (lang === "go") {
    if (GO_KEYWORDS.has(word)) return "keyword";
    if (GO_TYPES.has(word)) return "type";
    if (code[end] === "(") return "rule";
    return null;
  }
  return null;
}

const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" };
export function escapeHTML(s) {
  return s.replace(/[&<>"]/g, (c) => ESC[c]);
}

// highlight returns the code as HTML with a span for each token.
export function highlight(code, lang) {
  const tokens = tokenize(code, lang);
  let out = "";
  let i = 0;
  for (const t of tokens) {
    if (t.from > i) out += escapeHTML(code.slice(i, t.from));
    out += `<span class="tok-${t.cls}">${escapeHTML(code.slice(t.from, t.to))}</span>`;
    i = t.to;
  }
  return out + escapeHTML(code.slice(i));
}

// highlightJSON marks object keys separately from string values.
export function highlightJSON(code) {
  return highlight(code, "json").replace(/<span class="tok-string">([^<]*)<\/span>(\s*):/g, '<span class="tok-key">$1</span>$2:');
}
