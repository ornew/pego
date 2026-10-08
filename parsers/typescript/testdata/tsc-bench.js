// Times the TypeScript compiler's parser on source files, for orientation next to the benchmarks of the Go
// package (bench_test.go). Not used by the tests.
//
// Usage: node tsc-bench.js <path to the typescript package> <file>...
//
// For each file it parses the source with ts.createSourceFile (as the language service does, without setting
// parent nodes) repeatedly for about two seconds after a warm-up of 20 parses, and prints the mean time and
// the throughput, with the number of parse diagnostics (a file with diagnostics is still parsed).
"use strict";
const ts = require(process.argv[2]);
const fs = require("fs");
const path = require("path");

for (const file of process.argv.slice(3)) {
  const text = fs.readFileSync(file, "utf8");
  const kind = /\.tsx$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const parse = () => ts.createSourceFile(path.basename(file), text, ts.ScriptTarget.Latest, false, kind);
  for (let i = 0; i < 20; i++) parse();
  let n = 0;
  const start = process.hrtime.bigint();
  let elapsed = 0n;
  do {
    parse();
    n++;
    elapsed = process.hrtime.bigint() - start;
  } while (elapsed < 2_000_000_000n);
  const ms = Number(elapsed) / 1e6 / n;
  const diags = parse().parseDiagnostics.length;
  const mbs = Buffer.byteLength(text) / 1e6 / (ms / 1e3);
  console.log(`${path.basename(file).padEnd(28)} ${ms.toFixed(2).padStart(9)} ms/parse ${mbs.toFixed(2).padStart(7)} MB/s  ${diags} diagnostics`);
}
