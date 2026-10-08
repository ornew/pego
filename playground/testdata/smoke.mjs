// Smoke test of the playground's WebAssembly API in Node.
//
//   node smoke.mjs --wasm pego.wasm --wasm-exec wasm_exec.js --cli path/to/pego --repo path/to/repository
//
// It loads pego.wasm, and for each example of playground/examples.txt compiles the grammar, parses the
// sample input and checks that the results match what the pego command prints: the JSON tree, the
// S-expression, the errors, recognition (-check), other backends and units, fmt and gen -types.
// It exits with status 1 if any check fails. playground/wasm_test.go runs it as part of go test.

import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { parseArgs } from "node:util";

const { values: args } = parseArgs({
  options: {
    wasm: { type: "string" },
    "wasm-exec": { type: "string" },
    cli: { type: "string" },
    repo: { type: "string" },
  },
});
for (const k of ["wasm", "wasm-exec", "cli", "repo"]) {
  if (!args[k]) {
    console.error(`missing --${k}`);
    process.exit(2);
  }
}

let failures = 0;
let checks = 0;
function check(name, ok, detail = "") {
  checks++;
  if (!ok) {
    failures++;
    console.log(`FAIL ${name}${detail ? "\n" + detail : ""}`);
  }
}
function diff(got, want) {
  const g = String(got).split("\n");
  const w = String(want).split("\n");
  for (let i = 0; i < Math.max(g.length, w.length); i++) {
    if (g[i] !== w[i]) return `  line ${i + 1}:\n    got  ${JSON.stringify(g[i])}\n    want ${JSON.stringify(w[i])}`;
  }
  return "";
}

// Load the Go runtime support and start the program, which installs globalThis.pego.
createRequire(import.meta.url)(path.resolve(args["wasm-exec"]));
const go = new globalThis.Go();
const { instance } = await WebAssembly.instantiate(readFileSync(args.wasm), go.importObject);
const exited = go.run(instance);
if (!globalThis.pego) {
  await Promise.race([exited, new Promise((r) => setTimeout(r, 100))]);
}
if (!globalThis.pego) {
  console.error("pego.wasm did not install globalThis.pego");
  process.exit(1);
}
const call = (method, req) => JSON.parse(globalThis.pego[method](JSON.stringify(req ?? {})));

// cli runs the pego command and returns its standard output, error (without the "pego: " prefix) and
// exit status.
function cli(...argv) {
  const r = spawnSync(args.cli, argv, { cwd: args.repo, encoding: "utf8", maxBuffer: 1 << 28 });
  if (r.error) throw r.error;
  return { out: r.stdout, err: r.stderr.replace(/^pego: /, "").replace(/\n$/, ""), status: r.status };
}

// errorText renders syntax errors as the pego command prints them.
const errorText = (res) => (res.errors ?? []).map((e) => `${e.line}:${e.col}: ${e.message}`).join("\n");

const manifest = readFileSync(path.join(args.repo, "playground", "examples.txt"), "utf8")
  .split("\n")
  .filter((l) => l.trim() && !l.startsWith("#"))
  .map((l) => {
    const [name, grammar, input] = l.trim().split(/\s+/);
    return { name, grammar, input };
  });
check("manifest has examples", manifest.length >= 6, `got ${manifest.length}`);

for (const ex of manifest) {
  const grammar = readFileSync(path.join(args.repo, ex.grammar), "utf8");
  const input = readFileSync(path.join(args.repo, ex.input), "utf8");
  const tag = `${ex.name}:`;

  const info = call("compile", { grammar });
  check(`${tag} compile`, info.ok && info.diagnostics.length === 0, JSON.stringify(info.diagnostics));
  check(`${tag} rules`, info.rules.some((r) => r.name === "main") && info.start === "main");

  const res = call("parse", { grammar, input });
  const want = cli("parse", "-g", ex.grammar, "-i", input);
  check(`${tag} json`, (res.json ?? "") + (res.json ? "\n" : "") === want.out, diff(res.json, want.out.replace(/\n$/, "")));
  check(`${tag} errors`, (res.error || errorText(res)) === want.err, `  got  ${errorText(res)}\n  want ${want.err}`);
  check(`${tag} matched`, res.matched === (want.out !== ""));

  const sexpr = cli("parse", "-g", ex.grammar, "-f", "sexpr", "-i", input);
  check(`${tag} sexpr`, (res.sexpr ? res.sexpr + "\n" : "") === sexpr.out, diff(res.sexpr, sexpr.out.replace(/\n$/, "")));

  const rec = call("parse", { grammar, input, recognize: true });
  const wantRec = cli("parse", "-g", ex.grammar, "-check", "-i", input);
  check(`${tag} recognize`, (rec.matched && rec.errors === undefined) === (wantRec.out === "ok\n") && rec.json === undefined,
    `  got ${JSON.stringify(rec)}\n  want ${JSON.stringify(wantRec)}`);

  for (const backend of ["closure", "bytecode", "bytecode-iterative"]) {
    const b = call("parse", { grammar, input, backend });
    check(`${tag} backend ${backend}`, b.json === res.json && errorText(b) === errorText(res), diff(b.json, res.json));
  }

  const bytes = call("parse", { grammar, input, unit: "bytes" });
  const wantBytes = cli("parse", "-g", ex.grammar, "-unit", "bytes", "-i", input);
  check(`${tag} bytes`, (bytes.json ? bytes.json + "\n" : "") === wantBytes.out && errorText(bytes) === wantBytes.err,
    diff(bytes.json, wantBytes.out.replace(/\n$/, "")));

  const fmt = call("format", { grammar });
  check(`${tag} fmt`, fmt.formatted === cli("fmt", ex.grammar).out, diff(fmt.formatted, cli("fmt", ex.grammar).out));

  const gen = call("generate", { grammar, package: "parser", types: true });
  const wantGen = cli("gen", "-g", ex.grammar, "-pkg", "parser", "-types");
  check(`${tag} gen -types`, gen.code === wantGen.out, diff(gen.code, wantGen.out));
}

// Grammar errors come back as diagnostics with positions.
const bad = call("compile", { grammar: "def main = (\n" });
check("diagnostic position", bad.ok === false && bad.diagnostics[0]?.line === 2 && bad.diagnostics[0]?.col === 1,
  JSON.stringify(bad));
// The program keeps running after an error response.
const invalid = JSON.parse(globalThis.pego.parse("{"));
check("invalid request", invalid.error?.startsWith("invalid request"), JSON.stringify(invalid));
check("version", call("version").go?.startsWith("go"));

console.log(`${checks - failures}/${checks} checks passed`);
process.exit(failures ? 1 : 0);
