# The Web Site and the Playground

PEGO has a static web site: a landing page, this documentation, an API reference and a **playground** where you write a
grammar, type an input and see the tree as you type. Everything in the playground runs in your browser: the PEGO library
is compiled to WebAssembly, so nothing is sent to a server.

This guide explains what the playground shows, how to build and preview the whole site locally, how it is deployed, and
the small JavaScript API of the WebAssembly module. The design is recorded in
[design record 016](../design/016-web-site-and-playground.md).

- [Using the playground](#using-the-playground)
- [Building and previewing the site](#building-and-previewing-the-site)
- [What the site is made of](#what-the-site-is-made-of)
- [Deploying on Netlify](#deploying-on-netlify)
- [The WebAssembly API](#the-webassembly-api)
- [Tests](#tests)
- [Changing the site](#changing-the-site)

## Using the playground

The page has a grammar editor and an input editor side by side (stacked on a phone), a status line, an error list and
a result panel. Every edit re-parses after a short pause; the grammar is compiled again only when it changes.

| Part | What it does |
|:--|:--|
| **Example** | Loads one of the grammars of [`examples/`](../../examples/README.md) with a sample input: the calculator (Pratt and left-recursive), JSON, CSV, XML (and an XML error), the indentation outline, and minilang (and a minilang input that `#recover` recovers from) |
| **Start** | The start rule (`pego parse -s`). The default is `main`, or the first rule if the grammar has no `main` (where `pego parse` without `-s` would report that `main` is not defined). A start rule that the grammar does not define, after an edit or in a shared link, stays selected and is reported as an error |
| **Options** | The position unit (code points or bytes, `-unit`), the backend (`-backend`) and recognition mode (`pego.RecognizeOnly`, which builds no tree). Like `pego parse -check`, recognition reports the same syntax errors as a full parse; where the command prints `ok` only for an input without errors, the playground also shows an input that `#recover` recovered from as a match, with its errors |
| **Format** | Formats the grammar like `pego fmt`, keeping comments. The change can be undone |
| **Share** | Copies a link that holds the grammar, the input and the options (see [shared links](#shared-links)) |
| Status line | Whether the input matched (with or without a value, or after recovering from errors), the number of nodes, the start rule and the parse time |
| Errors | Grammar errors with their positions, the syntax error that stopped the parse, or the errors `#recover` recovered from. Click one to jump to it. The editors also mark the position and the line number |

The result panel has five tabs:

| Tab | Contents |
|:--|:--|
| **Tree** | The tree as a collapsible outline: the node type, the rule (`@rule`) of nodes produced by rules without an action, the text of terminals, the fields of struct nodes, and the span `start–end` in the chosen unit. Hovering a node highlights its span in the input, clicking selects it, and double-clicking (or Enter) selects the text in the input editor. Moving the caret in the input reveals the deepest node under it |
| **JSON** | The tree exactly as `pego parse` prints it |
| **S-expression** | The tree as `pego parse -f sexpr` prints it |
| **Rules & types** | The rules with their declared types, and the types of the grammar. Click a name to jump to its definition, or "parse from here" to make the rule the start rule |
| **Generated Go** | The code `pego gen` generates for the grammar, with the package name, `-types` and `-recognize` options |

In the editors, Tab inserts indentation; press Escape first to move the focus with Tab instead. Inputs larger than
200,000 characters are shown without highlighting to keep typing responsive.

Parsing runs in a Web Worker, so a slow parse never freezes the page. A parse that takes more than 10 seconds is
stopped and the worker is restarted; edits made in the meantime are parsed on the new worker.

### Nesting limits

In the browser, the Go code runs on the JavaScript engine's stack, which is far smaller than the stack of a native Go
program. The playground therefore limits nesting more than the engine does:

- With the closure and bytecode backends (and the default), rule calls may nest at most **600** deep, instead of
  100,000 (`pego.WithMaxDepth`). Deeper input ends with the engine's error `nesting too deep: more than 600 rule calls`.
  A JSON array nests three rule calls per level, so this allows about 200 nested arrays, where `pego parse` handles
  thousands. The bytecode-iterative backend keeps rule calls on its own stack and has no such limit.
- Trees nested more than **400** levels deep are not shown: the input matches, and the status says how deep the tree
  is. Encoding a tree as JSON or an S-expression takes stack for each level.

The limits leave a margin of more than two over what overflowed in measurements (see
[design record 016](../design/016-web-site-and-playground.md#nesting-and-the-browsers-stack)). If a parse still
overflows the stack, the worker is restarted and the status says so.

### Shared links

The state of the playground is kept in the URL fragment, so reloading the page keeps your work and a link reproduces it.
The fragment is `#z=` followed by the state as JSON, compressed with deflate (`CompressionStream`) and encoded as
base64url; browsers without `CompressionStream` write `#j=` with the JSON uncompressed. The fragment is never sent to
the server. `#example=<name>` opens an example by name, for example `playground/#example=minilang-recover`.

Code blocks of grammars in this documentation have a **Playground** button that opens them in the playground.

## Building and previewing the site

The site is built by a Go program in [`site/`](../../site/), so Go is the only requirement:

```bash
site/build.sh                          # build into site/dist
site/build.sh -serve localhost:8080    # build, then serve it at http://localhost:8080/
```

`site/build.sh` runs `go run . -repo .. -out dist` in `site/`. The flags are:

| Flag | Default | Meaning |
|:--|:--|:--|
| `-repo` | `..` | The root of the repository |
| `-out` | `dist` | The output directory. It is replaced by each build, but only if a previous build wrote it |
| `-wasm` | `true` | Build the playground's WebAssembly binary. Without it, the pages are built but the playground cannot run |
| `-check` | `true` | Fail if a page links to a missing page, file or anchor |
| `-serve` | | After building, serve the output at this address |
| `-github`, `-ref` | `https://github.com/ornew/pego`, `main` | Where links to source files point |

The output is plain static files. Any static server works, as long as it serves the files over HTTP (the playground
cannot run from `file://` URLs, because browsers do not load WebAssembly or workers from them):

```bash
python3 -m http.server -d site/dist 8080
```

The build output (`site/dist`) is not committed.

## What the site is made of

| URL | Source |
|:--|:--|
| `/` | The landing page, assembled from [`README.md`](../../README.md): the tagline and introduction, a live example with the README's grammar, the feature list under "Why PEGO" as cards, and the following sections |
| `/docs/tutorial/`, `/docs/guide/`, `/spec/`, `/docs/design/`, `/docs/…`, `/examples/` | The Markdown files of the repository, rendered at build time. Links between Markdown files (relative, relative to the repository root like `/spec/types.md`, or percent-encoded) become links between pages; links to other files of the repository go to GitHub. The design records get a generated index |
| `/reference/` | The API reference of packages `pego` and `grammar`, generated from the Go source with `go/doc`, and the reference of the `pego` command, generated from what the command prints: its usage message and `pego <command> -h` for every command it lists. Types that are aliases of internal types (such as `Node`) show the definition and methods of the internal type |
| `/playground/` | The playground: `app.js`, `worker.js`, `wasm/pego-<hash>.wasm` built from [`playground/`](../../playground/) (named after a hash of its content, which every page records in `<html data-wasm>`), and `wasm_exec.js` from the Go distribution |
| `/search-index.json` | The text of every page, for the search box in the header (press `/`) |

Nothing is copied by hand, so the site cannot drift from the repository: rebuilding it picks up every change.

The pages need no external resources (no CDN, fonts or analytics), have light and dark themes (following the system
until the switch in the header is used), and work on phone screens.

## Deploying on Netlify

[`netlify.toml`](../../netlify.toml) at the root of the repository configures Netlify: the build command is
`sh site/build.sh`, the publish directory is `site/dist`, and `GO_VERSION` selects the Go version of `go.mod`. Connecting
the repository to a Netlify site is enough to deploy it; each build runs the link check, so a broken link fails the
deploy instead of publishing it.

The configuration also sets a strict `Content-Security-Policy`: the pages load scripts, styles and data only from the
site itself, and need no inline scripts or styles; `'wasm-unsafe-eval'` allows compiling `pego.wasm`. The
WebAssembly binary, whose name changes with its content, is cached for a year (`immutable`); the other files are
revalidated on each visit, Netlify's default.

## The WebAssembly API

`pego.wasm` (built from `playground/` with `GOOS=js GOARCH=wasm`) installs a global object `pego` when it starts. Each
method takes a request encoded as JSON and returns the response encoded as JSON, so it can be called from a worker, a
page or Node:

| Method | Request | Response |
|:--|:--|:--|
| `compile` | `{grammar}` | `{ok, package, rules: [{name, type, line, col}], types: [{name, kind, spec, line, col}], start, diagnostics: [{line, col, message}]}` |
| `parse` | `{grammar, input, start, unit, backend, recognize, maxDepth}` | `{compile, start, matched, json, sexpr, nodes, depth, errors: [{pos, line, col, expected, messages, message}], recovered, error, micros}` |
| `format` | `{grammar}` | `{formatted, diagnostics}` |
| `generate` | `{grammar, package, start, types, recognize}` | `{code, diagnostics}` |
| `version` | | `{go, module, revision, modified}` |

`matched` is true when the parse succeeded, including a start rule that matches without producing a value (then
`json` is empty) and a parse that recovered from errors (then `recovered` is true and `errors` lists them); a syntax
error that stops the parse, or another error such as a runtime error in an action, makes it false. `json` and `sexpr` are exactly what `pego parse` prints with `-f json` and `-f sexpr` (without the final newline), and
`message` is the description of an error as the command prints it after `line:col:`. `maxDepth` overrides the [nesting limit](#nesting-limits) (for any backend); `nodes` and `depth` are the number of
nodes of the tree and how deeply they nest. An empty `start` selects the default start rule (`main`, or else the first rule); a `start` that the grammar does not
define is reported as the error `start rule X is not defined` (in `error` for `parse`, in `diagnostics` for
`generate`). `unit` is `codepoints` (the
default) or `bytes`; `backend` is empty (the default), `closure`, `bytecode` or `bytecode-iterative`. Grammar diagnostics
count columns in code points. A request that cannot be decoded, or an internal error, returns `{error}`.

In Node:

```js
require("./wasm_exec.js"); // from $(go env GOROOT)/lib/wasm
const go = new Go();
const { instance } = await WebAssembly.instantiate(fs.readFileSync("pego.wasm"), go.importObject);
go.run(instance);
const res = JSON.parse(pego.parse(JSON.stringify({ grammar: 'def main = "a"+', input: "aaa" })));
```

The binary is about 8.3 MB, 2.2 MB compressed with gzip and 1.6 MB with Brotli (static hosts such as Netlify compress
it). The landing page loads it only when its live example scrolls into view.

## Tests

| Test | What it checks |
|:--|:--|
| `go test ./playground` | The API natively (it has no build tags), and `TestWasmSmoke`: it builds `pego.wasm` and the `pego` command, and runs [`playground/testdata/smoke.mjs`](../../playground/testdata/smoke.mjs) in Node, which compares every example of [`playground/examples.txt`](../../playground/examples.txt) with the command: JSON, S-expression, errors, `-check`, every backend, byte positions, `pego fmt` and `pego gen -types`. It is skipped when `node` is not installed and in `-short` mode |
| `cd site && go test ./...` | Builds the whole site and checks that every Markdown file of `docs/` and `spec/` became a page, that the generated pages and the search index are complete, that no page needs inline scripts or styles, and that every internal link and anchor resolves. `TestJavaScript` runs the tests of the page scripts in `site/testdata/*_test.mjs` with `node --test` (skipped without Node) |

The site is a separate Go module, so `go test ./...` at the root does not run its tests.

## Changing the site

- **A new document** in `docs/guide/`, `docs/tutorial/`, `spec/`, `docs/design/` or `docs/` appears in its section
  automatically. A Markdown file in a new directory under `docs/` or `spec/` makes the build fail until `docSections` in
  [`site/build.go`](../../site/build.go) includes it, so that nothing is left out silently.
- **The order** of the guides and of the specification follows the links in `docs/guide/README.md` and
  `spec/README.md`; other sections are sorted by file name.
- **An example** for the playground is a line in [`playground/examples.txt`](../../playground/examples.txt): a name, a
  grammar, an input and a description. The smoke test then checks it too.
- **Templates, styles and scripts** are in `site/templates/` and `site/static/`. They are plain HTML, CSS and JavaScript
  modules: there is no build step and no npm dependency.
