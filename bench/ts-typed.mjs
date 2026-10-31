// Measure generated TypeScript Node and typed entry points in one fresh process.
// Export fixtures with ZP_TS_TYPED_DIR; see design 030 for the sampling protocol.
import {readFileSync} from "node:fs";
import {createHash} from "node:crypto";
import {resolve, join} from "node:path";
import {pathToFileURL, fileURLToPath} from "node:url";


const args = Object.create(null);
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i];
  if (!key?.startsWith("--") || process.argv[i + 1] === undefined) throw new Error("Expected --key value arguments");
  if (Object.hasOwn(args, key.slice(2))) throw new Error(`Repeated argument ${key}`);
  args[key.slice(2)] = process.argv[i + 1];
}
const allowed = new Set(["fixtures", "module", "fixture", "size", "unit", "mode", "variant", "phase", "warmup", "count", "retain", "input-kind", "entry-mode", "max-depth", "revision"]);
for (const key of Object.keys(args)) if (!allowed.has(key)) throw new Error(`Unknown argument --${key}`);
function integer(name, fallback, minimum) {
  const s = args[name] ?? String(fallback);
  if (!/^[0-9]+$/.test(s)) throw new Error(`Invalid ${name}: ${s}`);
  const n = Number(s);
  if (!Number.isSafeInteger(n) || n < minimum) throw new Error(`Invalid ${name}: ${s}`);
  return n;
}
function oneOf(name, values, fallback) {
  const s = args[name] ?? fallback;
  if (!values.includes(s)) throw new Error(`Invalid ${name}: ${s}`);
  return s;
}
function sha256(value) { return createHash("sha256").update(value).digest("hex"); }
const fixture = oneOf("fixture", ["fields", "capture-free", "recovery", "fold", "lr"]);
const size = oneOf("size", ["small", "large"], "small");
const unitName = oneOf("unit", ["codepoints", "bytes"], "codepoints");
const mode = oneOf("mode", ["node", "ast"], "node");
const phase = oneOf("phase", ["verify", "time", "memory"], "verify");
const inputKind = oneOf("input-kind", ["string", "bytes"], "string");
const entryMode = oneOf("entry-mode", ["direct", "options"], "direct");
const warmupCount = integer("warmup", 0, 0);
const count = integer("count", 1, 1);
const retainCount = integer("retain", 1, 1);
if (retainCount > 100) throw new Error("retain exceeds the declared modest bound of 100 results");
if (args["max-depth"] !== undefined && entryMode !== "options") throw new Error("max-depth requires options entry mode");
if (!args.fixtures || !args.module) throw new Error("fixtures and module paths are required");
const fixtureDir = resolve(args.fixtures);
const modulePath = resolve(args.module);
const inputsPath = join(fixtureDir, fixture, "inputs.json");
const sourceMeta = (() => {
  const bytes = readFileSync(modulePath);
  return {module_path: modulePath, source_bytes: bytes.length, source_sha256: sha256(bytes)};
})();
let inputStrings;
const inputMeta = (() => {
  const bytes = readFileSync(inputsPath);
  inputStrings = JSON.parse(bytes.toString("utf8"));
  return {inputs_path: inputsPath, inputs_file_bytes: bytes.length, inputs_file_sha256: sha256(bytes)};
})();
if (!Array.isArray(inputStrings) || inputStrings.length < 2 || inputStrings.some(s => typeof s !== "string")) throw new Error("Fixture inputs must be an array of at least two strings");
const inputIndex = size === "small" ? 0 : inputStrings.length - 1;
let selectedText = inputStrings[inputIndex];
inputStrings = null; // Do not intentionally retain every unused fixture input.
const selectedMeta = (() => {
  const bytes = Buffer.from(selectedText, "utf8");
  return {input_index: inputIndex, input_utf8_bytes: bytes.length, input_codepoints: [...selectedText].length,
    input_utf16_units: selectedText.length, input_utf8_sha256: sha256(bytes), input_utf16le_sha256: sha256(Buffer.from(selectedText, "utf16le"))};
})();
const input = inputKind === "string" ? selectedText : Uint8Array.from(Buffer.from(selectedText, "utf8"));
selectedText = null;
const mod = await import(pathToFileURL(modulePath).href);
const unit = unitName === "bytes" ? mod.Bytes : mod.CodePoints;
if (unit !== 0 && unit !== 1) throw new Error("Generated module lacks the expected position unit exports");
let options;
if (entryMode === "options") {
  options = {unit};
  if (args["max-depth"] !== undefined) options.maxDepth = integer("max-depth", 0, 0);
  Object.freeze(options);
}
let invoke;
if (entryMode === "direct") {
  if (mode === "node") invoke = () => mod.parse(input, unit);
  else invoke = () => mod.parseAST(input, unit);
} else {
  if (mode === "node") invoke = () => mod.parseWithOptions(input, options);
  else invoke = () => mod.parseASTWithOptions(input, options);
}
const expectedFunction = mode === "node" ? (entryMode === "direct" ? "parse" : "parseWithOptions") : (entryMode === "direct" ? "parseAST" : "parseASTWithOptions");
if (typeof mod[expectedFunction] !== "function") throw new Error(`Module does not export ${expectedFunction}`);
function rootOf(result) { return mode === "node" ? result.node : result.value; }
function diagnosticSummary(error) {
  if (error === null) return {name: null, count: 0, sha256: sha256("null")};
  function syntax(e) {
    return {name: e.name, message: e.message, pos: e.pos, line: e.line, col: e.col,
      expected: e.expected ?? null, messages: e.messages ?? null};
  }
  const data = Array.isArray(error.errors) ? error.errors.map(syntax) : [syntax(error)];
  return {name: error.name, count: data.length, sha256: sha256(JSON.stringify(data))};
}
function shapeOf(root) {
  if (root === null || root === undefined) throw new Error("Expected nonnil fixture result");
  if (fixture === "fields" || fixture === "recovery") {
    const items = Array.isArray(root) ? root : root.children;
    if (!Array.isArray(items)) throw new Error("Expected list result");
    return {kind: "list", items: items.length};
  }
  if (fixture === "capture-free") {
    return {kind: "terminal", type: root.type, start: root.start, end: root.end,
      text_utf8_sha256: sha256(Buffer.from(root.text, "utf8"))};
  }
  let value = root, nodes = 1;
  const seen = new Set();
  while (value.type === "Branch") {
    if (seen.has(value)) throw new Error("Unexpected cycle in DAG workload");
    seen.add(value);
    const left = mode === "node" ? value.field("Left") : value.Left;
    const right = mode === "node" ? value.field("Right") : value.Right;
    if (left === null || left !== right) throw new Error("DAG capture identity was lost");
    value = left;
    nodes++;
  }
  if (value?.type !== "Leaf" || nodes !== selectedMeta.input_codepoints) throw new Error("DAG chain does not match input length");
  return {kind: "dag", nodes, leaf_type: value.type, shared_left_right: true};
}
function distinctGraphStats(root) {
  const seen = new Set(), todo = [root];
  let edges = 0, arrays = 0;
  while (todo.length !== 0) {
    const v = todo.pop();
    if (v === null || typeof v !== "object" || seen.has(v)) continue;
    seen.add(v);
    if (seen.size > 10000000) throw new Error("Graph observation exceeded its declared bound");
    if (Array.isArray(v)) arrays++;
    // Enumerate data; never call Node.toJSON or recursively serialize result paths.
    for (const key of Object.keys(v)) {
      const child = v[key];
      edges++;
      if (child !== null && typeof child === "object") todo.push(child);
    }
  }
  return {distinct_objects: seen.size, arrays, enumerable_edges: edges};
}
function observe(result) {
  if (!result || !Object.hasOwn(result, "error")) throw new Error("Bad parse result shape");
  const diagnostics = diagnosticSummary(result.error);
  if (fixture === "recovery") {
    if (diagnostics.name !== "SyntaxErrors" || diagnostics.count === 0) throw new Error("Recovery workload lost its diagnostics");
  } else if (result.error !== null) throw result.error;
  const root = rootOf(result);
  return {diagnostics, shape: shapeOf(root), graph: distinctGraphStats(root)};
}
function equalObserved(a, b) {
  // These objects contain only bounded summaries, never parse result graphs.
  if (JSON.stringify(a.diagnostics) !== JSON.stringify(b.diagnostics) || JSON.stringify(a.shape) !== JSON.stringify(b.shape)) throw new Error("Output shape/diagnostics changed during measurement");
}
function warmup() { for (let i = 0; i < warmupCount; i++) invoke(); }
function collect() {
  if (typeof globalThis.gc !== "function") throw new Error("time/memory phases require node --expose-gc");
  for (let i = 0; i < 3; i++) globalThis.gc();
}
// Result-producing calls and observation arguments stay off the module frame.
function observeFresh() { return observe(invoke()); }
function fillRetained() {
  globalThis.__zpTypedRetainedResults = [];
  for (let i = 0; i < retainCount; i++) globalThis.__zpTypedRetainedResults.push(invoke());
}
function observeRetained() { return observe(globalThis.__zpTypedRetainedResults[0]); }
function releaseRetained() { globalThis.__zpTypedRetainedResults = null; }
const initial = observeFresh(); // Only its bounded summary reaches module scope.
const provenance = {
  protocol: "zp-typed-typescript-process-v1", phase, fixture, size, unit: unitName, mode,
  variant: args.variant ?? mode, revision_label: args.revision ?? "unspecified",
  entry_mode: entryMode, input_kind: inputKind, options: options ?? {unit},
  node_version: process.version, v8_version: process.versions.v8,
  platform: process.platform, arch: process.arch, executable: process.execPath, exec_argv: process.execArgv,
  harness_sha256: sha256(readFileSync(fileURLToPath(import.meta.url))),
  ...sourceMeta, ...inputMeta, ...selectedMeta,
};
let measurement;
if (phase === "verify") {
  measurement = {observed: initial};
} else if (phase === "time") {
  warmup();
  collect();
  let last = null;
  const started = process.hrtime.bigint();
  for (let i = 0; i < count; i++) last = invoke();
  const elapsed = process.hrtime.bigint() - started;
  const observed = observe(last); // Traversal/hashing excluded from elapsed.
  equalObserved(initial, observed);
  last = null;
  measurement = {warmup: warmupCount, count, elapsed_ns: elapsed.toString(), ns_per_parse: Number(elapsed) / count, observed};
} else {
  warmup();
  collect();
  const before = process.memoryUsage();
  fillRetained();
  collect();
  const retained = process.memoryUsage();
  const observed = observeRetained();
  equalObserved(initial, observed);
  // Keep no local alias to the retained array/results when collecting after release.
  releaseRetained();
  collect();
  const released = process.memoryUsage();
  measurement = {warmup: warmupCount, retained_results: retainCount,
    heap_before_parse_gc_bytes: before.heapUsed, heap_after_retaining_results_gc_bytes: retained.heapUsed,
    retained_heap_delta_bytes: retained.heapUsed - before.heapUsed,
    heap_after_releasing_results_gc_bytes: released.heapUsed,
    rss_before_parse_gc_bytes: before.rss, rss_after_retaining_results_gc_bytes: retained.rss,
    observed};
}
const processPeak = process.resourceUsage().maxRSS;
console.log(JSON.stringify({provenance, measurement,
  process_max_rss_kib_at_sample: processPeak,
  rss_scope: "process high-water mark through input/provenance setup, imports, verification, JIT/warmup, phase, observation and GC; not instantaneous per-parse peak",
  heap_scope: phase === "memory" ? "heapUsed after explicit GC with the declared retained result count; not instantaneous peak" : null}));
