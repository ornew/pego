// A small syntax highlighter for the languages of the documentation: zp, Go, TypeScript, JSON, S-expressions and shell.
// tokenize returns non-overlapping tokens {from, to, cls}; highlight returns HTML.

const GO_KEYWORDS = new Set([
  "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for", "func",
  "go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select", "struct",
  "switch", "type", "var", "nil", "true", "false", "iota",
]);
const GO_TYPES = new Set([
  "bool", "byte", "error", "float32", "float64", "int", "int8", "int16", "int32", "int64", "rune",
  "string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "any",
]);
const TS_KEYWORDS = new Set([
  "abstract", "as", "asserts", "async", "await", "break", "case", "catch", "class",
  "const", "constructor", "continue", "debugger", "declare", "default", "delete", "do", "else", "enum",
  "export", "extends", "false", "finally", "for", "from", "function", "get", "if", "implements", "import",
  "in", "infer", "instanceof", "interface", "is", "keyof", "let", "module", "namespace", "new", "null",
  "of", "package", "private", "protected", "public", "readonly", "require",
  "return", "set", "static", "super", "switch", "this", "throw", "true", "try", "type", "typeof",
  "unique", "var", "while", "with", "yield",
]);
const TS_TYPES = new Set([
  "Array", "BigInt", "Boolean", "Date", "Error", "Function", "Map", "Number", "Object", "Promise", "Record",
  "RegExp", "Set", "String", "Symbol", "WeakMap", "WeakSet", "Readonly", "Partial", "Pick", "Omit",
  "Exclude", "Extract", "NonNullable", "ReturnType", "Parameters", "InstanceType", "any", "boolean", "never",
  "number", "object", "string", "symbol", "undefined", "unknown", "void",
]);

// Words of S-expressions: nil and the booleans, and error values, written error(...).
const SEXPR_KEYWORDS = new Set(["nil", "true", "false", "error"]);

const rules = {
  zp: [
    ["comment", /\/\/[^\r\n]*/y],
    ["string", /"(?:[^"\\\n]|\\.)*"?/y],
    ["class", /\[(?!\])(?:[^\]\\\r\n]|\\.)*\]?/y],
    ["attr", /[@#][A-Za-z_][A-Za-z0-9_]*/y],
    ["capture", /\$(?:\$|[A-Za-z_][A-Za-z0-9_]*|[0-9]+)/y],
    ["number", /\d+/y],
    ["ident", /[A-Za-z_][A-Za-z0-9_]*/y],
    ["punct", /:=|\.\.\.|=>|==|!=|<=|>=|&&|\|\||_\|_|\^\^|[=;\/*+?&!@#:\-^|<>%.,()[\]{}]/y],
  ],
  go: [
    ["comment", /\/\/[^\n]*|\/\*[\s\S]*?(?:\*\/|$)/y],
    ["string", /"(?:[^"\\\n]|\\.)*"?|`[^`]*`?|'(?:[^'\\\n]|\\.)*'?/y],
    ["number", /\d[\w.]*/y],
    ["ident", /[\p{L}_][\p{L}\p{N}_]*/uy],
  ],
  typescript: [
    ["comment", /\/\/[^\n]*|\/\*[\s\S]*?(?:\*\/|$)/y],
    ["string", /`(?:[^`\\]|\\.)*`?|"(?:[^"\\\n]|\\.)*"?|'(?:[^'\\\n]|\\.)*'?/y],
    ["number", /(?:\b\d[\w.]*|\b0[xX][\da-fA-F]+)n?/y],
    ["ident", /[\p{L}_$][\p{L}\p{N}_$]*/uy],
    ["punct", /===|!==|=>|\.\.\.|\?\.|\?\?|&&|\|\||==|!=|<=|>=|\+=|-=|\*=|\/=|\*\*|[=<>!~?:;,.+*/%&|^()[\]{}@\-]/y],
  ],
  json: [
    ["string", /"(?:[^"\\\n]|\\.)*"?/y],
    ["number", /-?\d[\d.eE+-]*/y],
    ["keyword", /\b(?:true|false|null)\b/y],
  ],
  // The S-expressions of zp parse -f sexpr: tuples (...), lists [...], structs Type{...} and
  // named node types Type(...) with field labels, quoted strings, numbers, rule suffixes @rule and captures &name:.
  sexpr: [
    ["string", /"(?:[^"\\\n]|\\.)*"?/y],
    ["rule", /@[A-Za-z_][A-Za-z0-9_]*/y],
    ["number", /-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/y],
    ["ident", /[A-Za-z_][A-Za-z0-9_]*/y],
    ["punct", /[()[\]{},&:]/y],
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
    if (lang === "zp" && code.startsWith("/*", i)) {
      const from = i;
      let depth = 1;
      i += 2;
      while (i < n && depth > 0) {
        if (code.startsWith("/*", i)) { depth++; i += 2; }
        else if (code.startsWith("*/", i)) { depth--; i += 2; }
        else i++;
      }
      tokens.push({ from, to: i, cls: "comment" });
      continue;
    }
    let matched = false;
    for (const [cls, re] of rs) {
      re.lastIndex = i;
      const m = re.exec(code);
      if (!m || m[0].length === 0) continue;
      const text = m[0];
      let c = cls;
      if (cls === "ident") {
        c = lang === "zp" ? "ident" : classifyIdent(lang, text, code, i + text.length, prevWord);
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
  return lang === "zp" ? classifyZPTokens(code, tokens) : tokens;
}

// Contextual words are keywords at statement headers, not in rule names,
// references or captures. Track delimiters once without parsing the grammar.
function classifyZPTokens(code, tokens) {
  const significant = tokens.filter((t) => t.cls !== "comment");
  const frames = [{kind: "file", value: false, start: true, header: "", name: "", listNext: false}];
  let previous = null;
  for (let i = 0; i < significant.length; i++) {
    const t = significant[i];
    const text = code.slice(t.from, t.to);
    const next = significant[i + 1];
    const nextText = next ? code.slice(next.from, next.to) : "";
    const prevText = previous ? code.slice(previous.from, previous.to) : "";
    let frame = frames[frames.length - 1];
    if (previous && /[\r\n]/.test(code.slice(previous.to, t.from)) && (frame.kind === "file" || frame.kind === "operators")) {
      frame.start = true;
      frame.value = false;
      frame.header = frame.name = "";
    }
    if (t.cls === "ident") {
      t.cls = /^\p{Lu}/u.test(text) ? "type" : null;
      if (nextText === ":") t.cls = "label";
      else if (frame.name) {
        t.cls = frame.name;
        frame.name = "";
      } else if (frame.kind === "file" && frame.start && ["package", "type", "rule", "var"].includes(text)) {
        t.cls = "keyword";
        frame.header = text;
        frame.name = text === "rule" ? "rule" : text === "type" ? "type" : null; // var: keyword only
      } else if (frame.header === "type" && text === "struct") {
        t.cls = "keyword";
      } else if (frame.kind === "operators" && frame.start && ["skip", "operand", "level", "prefix", "postfix", "infix"].includes(text)) {
        t.cls = "keyword";
        frame.listNext = text === "level";
      } else if (!frame.value && text === "level" && nextText === "(" && next.from === t.to) {
        t.cls = "keyword";
      } else if (["left", "right", "none"].includes(text) && previous?.cls === "keyword" && prevText === "infix") {
        t.cls = "keyword";
      } else if (frame.value && ["true", "false", "nil"].includes(text)) {
        t.cls = "keyword";
      }
      frame.start = false;
    } else if (t.cls === "attr" && text === "#operators" && prevText === "=" && !frame.value && frames.length === 1) {
      // The pragma that starts an Operators Rule is a keyword.
      t.cls = "keyword";
      frame.listNext = true;
      frame.start = false;
    } else if (t.cls === "punct") {
      if (text === "=>") frame.value = true;
      else if (text === "=") { frame.header = ""; frame.start = false; }
      else if (text === ";") { frame.start = true; if (frame.kind === "file" || frame.kind === "operators") frame.value = false; }
      else if (text === "(") frames.push({kind: "expression", value: frame.value || previous?.cls === "attr" && !prevText.startsWith("#recover"), start: false, header: "", name: "", listNext: false});
      else if (text === "{") {
        const list = frame.listNext;
        frame.listNext = false;
        frames.push({kind: list ? "operators" : "value", value: !list, start: true, header: "", name: "", listNext: false});
      } else if (text === ")" || text === "}") { if (frames.length > 1) frames.pop(); }
      else frame.start = false;
    } else frame.start = false;
    previous = t;
  }
  return tokens.filter((t) => t.cls);
}

function classifyIdent(lang, word, code, end, prev) {
  if (lang === "go") {
    if (GO_KEYWORDS.has(word)) return "keyword";
    if (GO_TYPES.has(word)) return "type";
    if (code[end] === "(") return "rule";
    return null;
  }
  if (lang === "sexpr") {
    if (SEXPR_KEYWORDS.has(word)) return "keyword";
    if (code[end] === "{" || code[end] === "(" || code[end] === "[") return "type"; // a struct's or named node type's name
    if (code[end] === ":") return "label"; // a field or capture name
    return null;
  }
  if (lang === "typescript") {
    if (TS_KEYWORDS.has(word)) return "keyword";
    if (TS_TYPES.has(word) || /^\p{Lu}/u.test(word)) return "type";
    if (prev === "interface" || prev === "type" || prev === "class" || prev === "enum") return "type";
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
