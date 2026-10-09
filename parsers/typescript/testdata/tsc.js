// Parses TypeScript sources with the TypeScript compiler, for the differential tests (tsc_test.go).
//
// Usage: node tsc.js <path to the typescript package>
//
// Reads one JSON request per line on standard input, {"name": file name, "text": source, "tsx": bool}, and
// writes one JSON
// response per line: {"diags": [{"pos": utf16 offset, "code": n, "msg": text}], "nodes": [...]}, where the
// diagnostics are the source file's parseDiagnostics and the nodes are the source file and its descendants
// in preorder (the children in forEachChild order), each as [kind, start, end, number of children], with
// UTF-16 offsets. The start is getStart() (after trivia), except for the source file and JsxText, which
// start at their pos; a node of width zero is placed after the trivia before it.
"use strict";
const ts = require(process.argv[2]);
const readline = require("readline");

// The name of each SyntaxKind, without the aliases FirstX and LastX.
const kindNames = [];
for (const name of Object.keys(ts.SyntaxKind)) {
  const v = ts.SyntaxKind[name];
  if (typeof v !== "number") continue;
  if (kindNames[v] === undefined || /^(First|Last)/.test(kindNames[v])) kindNames[v] = name;
}

function children(n) {
  const cs = [];
  ts.forEachChild(n, (c) => { cs.push(c); }, (list) => { for (const c of list) cs.push(c); });
  return cs;
}

// nodes lists the tree in preorder without recursion, since some test cases nest deeply.
function nodes(sf) {
  const out = [];
  const stack = [sf];
  while (stack.length > 0) {
    const n = stack.pop();
    let start = n.getStart(sf);
    let end = n.end;
    if (n.kind === ts.SyntaxKind.SourceFile || n.kind === ts.SyntaxKind.JsxText) start = n.pos;
    if (n.pos === n.end) start = end = ts.skipTrivia(sf.text, n.pos); // getStart is pos for a node of width 0
    const cs = children(n);
    out.push([kindNames[n.kind], start, end, cs.length]);
    for (let i = cs.length - 1; i >= 0; i--) stack.push(cs[i]);
  }
  return out;
}

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
rl.on("line", (line) => {
  const req = JSON.parse(line);
  const sf = ts.createSourceFile(req.name, req.text, ts.ScriptTarget.Latest, true,
    req.tsx ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const diags = sf.parseDiagnostics.map((d) => ({
    pos: d.start, code: d.code, msg: ts.flattenDiagnosticMessageText(d.messageText, "\n"),
  }));
  const result = { diags, nodes: nodes(sf) };
  // Cooked-value requests are opt-in; the large tree corpus keeps its protocol.
  if (req.values) {
    result.version = ts.version;
    result.values = sf.statements.map((s) => {
      if (!ts.isExpressionStatement(s) ||
          !(ts.isStringLiteral(s.expression) || ts.isNoSubstitutionTemplateLiteral(s.expression))) {
        throw new Error("a cooked-value request must contain only string or template literal statements");
      }
      return s.expression.text;
    });
  }
  process.stdout.write(JSON.stringify(result) + "\n");
});
