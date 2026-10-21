// Measure the plain-parse throughput of a generated TypeScript parser in one fresh process.
//
//   node --experimental-strip-types bench/ts-parse.mjs --module DIR/parser.ts --input FILE [--name json] [--entry parse]
//
// The inputs come from the bench package: PEGO_TS_INPUT_DIR=DIR go test ./bench -run TestExportTSInputs.
// The script prints one line in the format of go test -bench, so that benchstat can compare runs:
//
//   BenchmarkTSParse/json 12 8123456 ns/op 30.8 MB/s
//
// It parses the input until at least 1.5 s have passed (after a warm-up of 1 s), and reports the mean.
import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {pathToFileURL} from "node:url";
import {register} from "node:module";

// The generated files import each other as ./runtime.js, which a TypeScript build resolves to runtime.ts;
// Node.js running the .ts sources needs this hook to do the same.
register("data:text/javascript," + encodeURIComponent(`export async function resolve(s,c,n){try{return await n(s,c)}catch(e){if(s.startsWith(".")&&s.endsWith(".js"))return n(s.slice(0,-3)+".ts",c);throw e}}`));

const args = Object.create(null);
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i];
  if (!key?.startsWith("--") || process.argv[i + 1] === undefined) throw new Error("Expected --key value arguments");
  args[key.slice(2)] = process.argv[i + 1];
}
if (!args.module || !args.input) throw new Error("--module and --input are required");
const name = args.name ?? "parse";
const entry = args.entry ?? "parse";
const input = readFileSync(resolve(args.input), "utf8");
const mod = await import(pathToFileURL(resolve(args.module)).href);
const fn = mod[entry];
if (typeof fn !== "function") throw new Error(`The module has no function ${entry}`);

function once() {
  const r = fn(input);
  const error = r.error;
  if (error) throw error;
  return r;
}

let keep;
const warmupEnd = performance.now() + 1000;
while (performance.now() < warmupEnd) keep = once();
let n = 0;
const start = performance.now();
let now = start;
while (now - start < 1500) {
  keep = once();
  n++;
  now = performance.now();
}
const ns = ((now - start) * 1e6) / n;
const mbps = (Buffer.byteLength(input) / (ns / 1e9)) / 1e6;
if (keep === undefined) throw new Error("unreachable");
console.log(`BenchmarkTSParse/${name} ${n} ${Math.round(ns)} ns/op ${mbps.toFixed(2)} MB/s`);
