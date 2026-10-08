// describeResult turns a parse response of pego.wasm into what the playground shows: the status
// line ({kind, text}: kind is ok, warn or error; text is plain text) and the message of the tree
// panel when there is no tree. It has no dependencies, so that it can be tested in Node.

export function fmtTime(micros) {
  return micros < 1000 ? `${micros} µs` : `${(micros / 1000).toFixed(micros < 10000 ? 2 : 1)} ms`;
}

const plural = (n, word) => (n === 1 ? `1 ${word}` : `${n} ${word}s`);

// depthHint explains the playground's nesting limit, which is lower than the engine's because the
// browser's stack is small (see playground/main_js.go).
function depthHint(res, req) {
  if (!/^nesting too deep/.test(res.error || "") || req.backend === "bytecode-iterative" || req.maxDepth) return "";
  return " (the playground's limit, which fits the browser's stack; the bytecode-iterative backend has none)";
}

export function describeResult(res, req) {
  const errs = res.errors ?? [];
  const info = `start ${res.start || "?"} · ${fmtTime(res.micros ?? 0)}`;
  const nodes = res.json ? ` · ${plural(res.nodes ?? 0, "node")}` : "";
  let status;
  if (!res.matched) {
    if (errs.length) status = { kind: "error", text: `✗ Syntax error at ${errs[0].line}:${errs[0].col} · ${info}` };
    else status = { kind: "error", text: `✗ ${res.error || "The input does not match the grammar"}${depthHint(res, req)}` };
  } else if (res.error) {
    // The input matched, but the result cannot be shown (for example, a tree nested too deep).
    status = { kind: "warn", text: `⚠ Matched, but ${res.error} · ${info}` };
  } else if (errs.length) {
    status = { kind: "warn", text: `⚠ Recovered from ${plural(errs.length, "error")}${nodes} · ${info}` };
  } else if (req.recognize) {
    status = { kind: "ok", text: `✓ The input matches · ${info}` };
  } else if (!res.json) {
    status = { kind: "ok", text: `✓ Matched without a value · ${info}` };
  } else {
    status = { kind: "ok", text: `✓ Matched${nodes} · ${info}` };
  }

  let empty = "";
  if (!res.json) {
    if (!res.matched) empty = "No tree: the input does not match the grammar.";
    else if (res.error) empty = "No tree: " + res.error + ".";
    else if (req.recognize) empty = "Recognition mode builds no tree.";
    else empty = "The start rule matched without producing a value.";
  }
  return { status, empty };
}
