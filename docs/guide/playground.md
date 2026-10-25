# The Playground

The [PEGO playground](https://pego.ornew.net/playground/) lets you write a grammar, type an input and inspect the tree as you edit. Everything runs in your browser: PEGO is compiled to WebAssembly, so grammar text and input are not sent to a server.

This guide covers the playground's controls, shared links, limits and WebAssembly API.

- [Using the playground](#using-the-playground)
- [Export and compare](#export-and-compare)
- [Nesting limits](#nesting-limits)
- [Shared links](#shared-links)
- [The WebAssembly API](#the-webassembly-api)

## Using the Playground

The page has a grammar editor and an input editor side by side (stacked on a phone), a status line, an error list and
a result panel. Every edit re-parses after a short pause; the grammar is compiled again only when it changes.

| Part | What it does |
|:--|:--|
| **Example** | Loads one of the grammars of [`examples/`](../../examples/README.md) or [`parsers/`](../../parsers/README.md) with a sample input: the calculator (Operators Rule and left-recursive), JSON, CSV, XML (and an XML error), the indentation outline, and minilang (and a minilang input that `#recover` recovers from) |
| **Start** | The start rule (`pego parse -s`), one of the rules marked `@start`. The default is the grammar's default start rule. A selected rule that is not a start rule, as in a shared link or after an edit of the grammar, stays selected and is reported as an error |
| **Options** | The position unit (code points or bytes, `-unit`), the backend (`-backend`) and recognition mode (`pego.RecognizeOnly`, which builds no tree), **Default value** (`-default-value`: drops value actions and declared rule types, so the tree is the default node tree; predicates still run), and **Source info** (`-source-info`: the rule of each node and the names of its captures, which `pego parse` leaves out by default). Like `pego parse -check`, recognition reports the same syntax errors as a full parse; where the command prints `ok` only for an input without errors, the playground also shows an input that `#recover` recovered from as a match, with its errors |
| **WASM compiler** | Go is the default. Sites built in dual mode also offer TinyGo. The selection stays in the shared URL's `compiler` query parameter; a TinyGo link on a Go-only site shows an unavailable message |
| **Format** | Formats the grammar like `pego fmt`, keeping comments. The change can be undone |
| **Share** | Copies a link that holds the grammar, the input and the options (see [shared links](#shared-links)) |
| Status line | Whether the input matched (with or without a value, or after recovering from errors), the number of nodes, the start rule and the parse time |
| Errors | Grammar errors with their positions, the syntax error that stopped the parse, or the errors `#recover` recovered from. Click one to jump to it. The editors also mark the position and the line number |

The result panel has six tabs:

| Tab | Contents |
|:--|:--|
| **Tree** | The tree as a collapsible outline: a string node's text, a tuple or list node with its number of elements, an error value with its message, a struct with its type, fields and span, the span `start–end` of each node in the chosen unit, and, with **Source info**, the rule (`@rule`) of each node and the capture names that label its elements. Hovering a node highlights its span in the input, clicking selects it, and double-clicking (or Enter) selects the text in the input editor. Moving the caret in the input reveals the deepest node under it |
| **JSON** | The tree exactly as `pego parse` prints it |
| **S-expression** | The tree as `pego parse -f sexpr` prints it, highlighted: struct and named node types, field and capture labels, rule suffixes, strings, numbers and keywords such as `nil` and `error` |
| **Rules & types** | The rules with their declared types, and the types of the grammar. Click a name to jump to its definition, or "parse from here" to make the rule the start rule |
| **Generated parser** | Go or TypeScript source for the grammar, with the available generation options |
| **Compare backends** | The results from the closure, bytecode and bytecode-iterative Go engine backends for the same grammar and input |

The Tree tab shows at most 100 direct children per page. **Previous children** and **Next children** replace that
page and show its range and total. Initial automatic expansion creates at most 400 rows, including page controls.
**Expand shown** expands the displayed pages up to a 5,000-row cap; use the page controls to browse other siblings.
At the cap, expanding another branch compacts the display around its path. Caret moves reveal deferred children
without displaying all preceding siblings. If an ancestor path itself cannot fit, a subtree view provides a
**Show complete tree** button. The parsed tree and its JSON/S-expression remain complete.

In the editors, Tab inserts indentation; press Escape first to move the focus with Tab instead. Inputs larger than
200,000 characters are shown without highlighting to keep typing responsive.

Parsing runs in a Web Worker, so a slow parse never freezes the page. A parse that takes more than 10 seconds is
stopped and the worker is restarted; edits made in the meantime are parsed on the new worker.

## Export and Compare

The **Generated parser** tab exports Go or TypeScript source. Go accepts a package name; both languages support
`-types` and `-recognize`. Long source files are paged for display. A Go parser is two files, `parser.go` and
`api.go`, which the tab shows one after another, each under a banner (`// ===== parser.go =====`). **Copy** copies the
complete displayed text, which is not one compilable file. **Download parser.go** and **Download api.go** save the two
files separately, to be put in one directory (a Go package named by the package field); they import
`github.com/ornew/pego/runtime`, so the module needs `go get github.com/ornew/pego`. A TypeScript parser is `parser.ts`, `runtime.ts` and, with `-types`, `types.ts`, offered the same way (saved
separately, into one directory). Shared links retain the selected language and generator options.

The **Compare backends** tab runs the same grammar, input, start rule, recognition mode and position unit with the
closure, bytecode and bytecode-iterative Go engine backends. It compares match and recovery status, values, errors
and positions. Generated Go and TypeScript parsers are exported from the **Generated parser** tab; the comparison
does not execute them. Long reports are paged for display, while **Copy** copies the complete report. Error line and
column values are one-based;
columns and offsets use the selected position unit. The report shows at most 100 differences and summarizes the total.
An equal result means that these three engine backends agreed for this input and option set. The summary shows the
result in the status colours: agreement in green, differences in amber and a failed comparison in red. It also gives
the median time of each backend over five parses of the same input.

### Nesting Limits

In the browser, the Go code runs on the JavaScript engine's stack, which is far smaller than the stack of a native Go
program. The playground therefore limits nesting more than the engine does:

- With the closure and bytecode backends (and the default), rule calls may nest at most **600** deep, instead of
  100,000 (`pego.WithMaxDepth`). Deeper input ends with the engine's error `nesting too deep: more than 600 rule calls`.
  A JSON array nests three rule calls per level, so this allows about 200 nested arrays, where `pego parse` handles
  thousands. The bytecode-iterative backend keeps rule calls on its own stack and has no such limit.
- Trees nested more than **400** levels deep are not shown: the input matches, and the status says how deep the tree
  is. Encoding a tree as JSON or an S-expression takes stack for each level.

The limits leave a margin of more than two over what overflowed in measurements (see
[design record 016](../design/records/016-web-site-and-playground.md#nesting-and-the-browsers-stack)). If a parse still
overflows the stack, the worker is restarted and the status says so.

### Shared Links

The state of the playground is kept in the URL fragment, so reloading the page keeps your work and a link reproduces it.
The fragment is `#z=` followed by the state as JSON, compressed with deflate (`CompressionStream`) and encoded as
base64url; browsers without `CompressionStream` write `#j=` with the JSON uncompressed. The fragment is never sent to
the server. `#example=<name>` opens an example by name, for example `playground/#example=minilang-recover`.
Following another shared link in the same page restores every option, including the start rule and result tab,
even when grammar and input are unchanged. Changes to parsing options trigger a new parse; tab-only navigation
preserves the editors and shows the existing result without reparsing (the Go tab generates code).
If editing, sharing or navigation overlaps compression or link loading, the most recent action wins. Older work
cannot replace its state or URL. Share copies its own completed link; a request superseded by a later action does
not copy. The landing page's live-example link likewise follows the newest input. If encoding or decoding fails,
the existing state/link is retained and the browser console reports the failure.

Code blocks of grammars in this documentation have a **Playground** button that opens them in the playground.

## The WebAssembly API

`pego.wasm` (built from `playground/` with `GOOS=js GOARCH=wasm`) installs a global object `pego` when it starts. Each
method takes a request encoded as JSON and returns the response encoded as JSON, so it can be called from a worker, a
page or Node:

| Method | Request | Response |
|:--|:--|:--|
| `compile` | `{grammar}` | `{ok, package, rules: [{name, type, line, col}], types: [{name, kind, spec, line, col}], start, diagnostics: [{line, col, message}]}` |
| `parse` | `{grammar, input, start, unit, backend, recognize, sourceInfo, defaultValue, maxDepth, runs}` | `{compile, start, matched, json, sexpr, nodes, depth, errors: [{pos, line, col, expected, messages, message}], recovered, error, micros, runs}` |
| `format` | `{grammar}` | `{formatted, diagnostics}` |
| `generate` | `{grammar, language, package, start, types, recognize}` | `{code, diagnostics}` |
| `version` | | `{go, module, revision, modified}` |

`matched` is true when the parse succeeded, including a start rule that matches without producing a value (then
`json` is empty) and a parse that recovered from errors (then `recovered` is true and `errors` lists them); a syntax
error that stops the parse, or another error such as a runtime error in an action, makes it false. `json` and `sexpr` are exactly what `pego parse` prints with `-f json` and `-f sexpr` (with `-source-info` when `sourceInfo` is true, without the final newline), and
`message` is the description of an error as the command prints it after `line:col:`. `maxDepth` overrides the [nesting limit](#nesting-limits) (for any backend); `nodes` and `depth` are the number of
nodes of the tree and how deeply they nest. `runs` (default 1, at most 100) parses the input that many times; `micros`
is then the median time of those parses and `runs` is the number of parses it is measured over. An empty `start` selects the grammar's default start rule; a `start` that is not a start rule is reported as the
error `rule X is not a start rule; parsing can begin only at a rule marked @start`, or `rule X is not defined` if
the grammar does not define it (in `error` for `parse`, in `diagnostics` for `generate`). `unit` is `codepoints` (the
default) or `bytes`; `backend` is empty (the default), `closure`, `bytecode` or `bytecode-iterative`. Grammar diagnostics
count columns in code points. A request that cannot be decoded, or an internal error, returns `{error}`.

In Node:

```js
require("./wasm_exec.js"); // from $(go env GOROOT)/lib/wasm
const go = new Go();
const { instance } = await WebAssembly.instantiate(fs.readFileSync("pego.wasm"), go.importObject);
go.run(instance);
const res = JSON.parse(pego.parse(JSON.stringify({ grammar: '@start\nrule main =  "a"+ ', input: "aaa" })));
```

The binary is about 8.3 MB before compression (2.2 MB with gzip or 1.6 MB with Brotli). The landing page loads it only when its live example scrolls into view.
