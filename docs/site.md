# Website Maintenance

This page is for contributors who build, test or publish PEGO's website. For using the browser playground and its WebAssembly API, see the [playground guide](guide/playground.md).

## Building and Previewing the Site

The site uses the pinned Nanosite standard `sitegen.BuildWith` pipeline. Its
configuration in `site/config.json` owns publication, defaults, front matter,
record indexes, navigation, pager ordering, and search. The PEGO driver adds
the landing page, API reference, Playground and theme adapter through extension
hooks; it also owns deployment-specific asset-retention flags. The installed
Nanosite skill documents the standard configuration fields.

From the repository root, build or preview the site (Go 1.27.1 or later):

```bash
site/build.sh                          # build into site/dist
site/build.sh -serve localhost:8080    # build, then serve it at http://localhost:8080/
```

`site/build.sh` runs the pinned project driver in `site/`; standard publication
and validation are handled by Nanosite. The project-specific flags are:

| Flag | Default | Meaning |
|:--|:--|:--|
| `-repo` | `..` | The root of the repository |
| `-out` | `dist` | The output directory. It is replaced by each build, but only if a previous build wrote it |
| `-wasm` | `true` | Build the playground's WebAssembly binary. Without it, the pages are built but the playground cannot run |
| `-wasm-mode` | `go` | `go` builds the standard Playground binary; `dual` builds both Go and TinyGo binaries. Dual mode requires TinyGo and Binaryen's `wasm-opt` on `PATH`; the TinyGo distribution does not include `wasm-opt`, so install Binaryen separately. The build fails if either tool is missing or compilation fails; there is no automatic fallback |
| `-check` | `true` | Fail if a page links to a missing page, file or anchor |
| `-previous-site` | Empty locally; production `URL` on Netlify | Import the published current WASM/runtime pair to retain it for one deploy; requires `-wasm` |
| `-deploy-url` | Empty locally; production `DEPLOY_URL` on Netlify | Record this immutable deploy root for the next build's asset downloads |
| `-serve` | | After building, serve the output at this address |
| `-github`, `-ref` | `https://github.com/ornew/pego`, `main` | Where links to source files point |

The output is plain static files. Any static server works, as long as it serves the files over HTTP (the playground
cannot run from `file://` URLs, because browsers do not load WebAssembly or workers from them):

```bash
python3 -m http.server -d site/dist 8080
```

The build output (`site/dist`) is not committed.

## What the Site Is Made Of

| URL | Source |
|:--|:--|
| `/` | The landing page, assembled from [`README.md`](../README.md): the tagline and introduction, a live example with the README's grammar, the feature list under "Why PEGO" as cards, and the following sections |
| `/docs/tutorial/`, `/docs/guide/`, `/spec/`, `/docs/design/`, `/docs/…`, `/examples/` | The Markdown files of the repository, rendered at build time. Links between Markdown files (relative, relative to the repository root like `/spec/types.md`, or percent-encoded) become links between pages; links to other files of the repository go to GitHub. The design records get a generated index |
| `/reference/` | The API reference of packages `pego` and `grammar`, generated from the Go source with `go/doc`, and the reference of the `pego` command, generated from what the command prints: its usage message and `pego <command> -h` for every command it lists. Types that are aliases of internal types (such as `Node`) show the definition and methods of the internal type |
| `/playground/` | The playground: `app.js`, `worker.js`, content-hashed `wasm/pego-<hash>.wasm` and `runtime/wasm_exec-<hash>.js`, plus `assets.json` pairing current/previous Go and optional TinyGo generations; every page records the current Go WASM in `<html data-wasm>` and dual builds also record `data-tinygo-wasm`. The stable `wasm_exec.js` supports pre-manifest workers |
| `/search-index.json` | The text of every page, for the search box in the header (press `/`) |

Nothing is copied by hand, so the site cannot drift from the repository: rebuilding it picks up every change.

The pages need no external resources (no CDN, fonts or analytics), have light and dark themes (following the system
until the switch in the header is used), and work on phone screens.

## Deploying on Netlify

Production builds download the currently published WASM and its matching Go runtime before generating the new site.
The output retains at most the new pair and one previous pair per compiler; deploy/branch previews do not import production assets
automatically. A page from the preceding deploy can therefore start its old parser after publication. The worker reads
`assets.json` and imports the matching hashed runtime. Generations outside that window must reload.

Dual mode is opt-in: `site/build.sh -wasm-mode=dual` requires a compatible
TinyGo installation and Binaryen's `wasm-opt` executable on `PATH`. The
regular site builds use `go` mode. `site/install-playground-toolchains.sh`
provides the pinned, no-root Linux x86_64 setup for Netlify: TinyGo 0.43.0 and
Binaryen 131 are downloaded from their official releases, SHA-256 verified,
stored as archives under `NETLIFY_CACHE_DIR` when that variable is available,
and extracted into a temporary directory for that build. Archive persistence
depends on the selected Netlify build image caching that path; otherwise the
script downloads them again. It rejects other operating systems and architectures
and fails on any missing tool, unexpected version, or checksum mismatch. In a
dual build, the Playground defaults to Go and offers
TinyGo under Options; the choice is carried by `?compiler=tinygo` in shared links. A TinyGo link
opened on a Go-only build reports that TinyGo is unavailable. Before enabling dual mode for
production, first deploy the updated builder with Go-only mode, then enable the
production build command `sh site/install-playground-toolchains.sh`. This staged
rollout leaves a Go-only deployment whose builder understands dual manifests
available for rollback. The current `netlify.toml` deliberately remains Go-only
until the owner selects this rollout or a retention-reset policy.
The manifest keeps its version-one `current` and `previous` fields for Go, with optional
`tinygoCurrent` and `tinygoPrevious` pairs. Older workers can still select Go. An older site builder
uses strict JSON decoding and rejects the added TinyGo fields when importing a dual build with
`-previous-site`. Roll back to the staged Go-only builder that understands these
fields to preserve normal Go asset retention. Rolling back farther to a builder
that rejects the fields requires a deliberate retention reset: build Go-only
with `-previous-site ''`, which drops all retained previous compiler pairs and
means pages from those generations must reload. Do not point an older builder
at a dual manifest without that reset.

The first upgrade from a pre-manifest site reads its playground HTML and stable runtime. Later builds fetch from the
immutable deploy URL recorded in the manifest. Missing or invalid published assets fail the build; a genuinely missing
playground initializes a first deployment. Keep production publication sequential. Rollbacks and manually promoted
previews need `-previous-site` set to the deployment being replaced; retention does not coordinate concurrent promotions.
To deliberately reset retention, pass `-previous-site ''`. See [the asset-retention design](design/023-playground-asset-retention.md)
for the format, bounds and bootstrap limitations.

[`netlify.toml`](../netlify.toml) at the root of the repository configures Netlify: the default build command is
`sh site/build.sh`, the publish directory is `site/dist`, and `GO_VERSION` selects the Go version of `go.mod`. Once the staged Go-only builder has been published, the production context can use `sh site/install-playground-toolchains.sh`; preview and branch builds remain on the default Go-only command. Connecting the repository to a Netlify site is enough to deploy it; each build runs the link check, so a broken link fails the
deploy instead of publishing it. The official site, [pego.ornew.net](https://pego.ornew.net/), is deployed this way.

HTTPS requests to `pego-go.netlify.app` receive a permanent `301` redirect to
`https://pego.ornew.net`, preserving the path and query parameters. HTTP requests
first follow Netlify's HTTPS upgrade, then the canonical-domain redirect.
The forced, host-specific rules also redirect published files and leave deploy
preview hosts available. These rules take effect when Netlify deploys the
updated configuration; the local preview server does not apply them.

The configuration also sets a strict `Content-Security-Policy`: the pages load scripts, styles and data only from the
site itself, and need no inline scripts or styles; `'wasm-unsafe-eval'` allows compiling `pego.wasm`. The
WebAssembly binaries and their hashed runtime scripts are cached for a year (`immutable`); the other files are
revalidated on each visit, Netlify's default.


## Tests

| Test | What it checks |
|:--|:--|
| `go test ./playground` | The API natively (it has no build tags), and `TestWasmSmoke`: it builds `pego.wasm` and the `pego` command, and runs [`playground/testdata/smoke.mjs`](../playground/testdata/smoke.mjs) in Node, which compares every example of [`playground/examples.txt`](../playground/examples.txt) with the command: JSON, S-expression, errors, `-check`, every backend, byte positions, `pego fmt` and `pego gen -types`. It is skipped when `node` is not installed and in `-short` mode |
| `cd site && go test ./...` | Builds the whole site and checks that every Markdown file of `docs/` and `spec/` became a page, that the generated pages and the search index are complete, that no page needs inline scripts or styles, and that every internal link and anchor resolves. `TestJavaScript` runs the tests of the page scripts in `site/testdata/*_test.mjs` with `node --test` (skipped without Node) |

The site is a separate Go module, so `go test ./...` at the root does not run its tests.
The playground state tests execute the actual application with a controlled DOM, client and timers. They check
incoming option-only links, omitted/invalid option defaults, parse requests, tabs, editor scroll and named examples;
deferred encoding/decoding tests also cover reverse completion, Share, navigation before event delivery, autosave
during link loading, local edits during initial example fetching, and failures in both playground and landing scripts.
They do not replace checks in a browser with the real WebAssembly worker.
Tree tests exercise initial/live row caps, wide pages, lazy sibling access, deferred-child reveal, deep subtree
windows, exact cap boundaries, reference cleanup and page focus with a controlled DOM. The renderer lives in
`site/static/playground/tree.js`. `node site/testdata/bench_tree.mjs [parent-app.js]` compares actual sources in the
same VM: 50 children compare equal displayed work (100 warmups, 1,000 sets), while 10,000 children compare full
versus bounded display (three warmups, 20 sets). These timings exclude browser layout, parsing and WASM.
`node site/testdata/bench_playground_state.mjs [app.js]` measures unchanged-link dispatch in that harness, with
10,000 warmups and 100,000 measured events. The optional application path compares another revision. Immediate
decoding excludes compression/encoding, browser DOM, worker and WebAssembly costs.

## Changing the Site

- **The grammar reference** in `spec/syntax.md` is generated with `go generate ./spec` from `pego.pego` and `spec/syntax.md.tmpl`; it publishes the simplified syntax view and links to the canonical full grammar in `pego.pego` on GitHub.
- **A new document** in `docs/guide/`, `docs/tutorial/`, `spec/`, `docs/design/` or `docs/` appears in its section
  automatically through the include rules in [`site/config.json`](../site/config.json).
  Add a section match there for a new documentation directory; unmatched pages otherwise appear under Other.
  Front matter can control publication, navigation, search, titles and routes without changing Markdown bodies.
- **The order** of the guides and of the specification follows the links in `docs/guide/README.md` and
  `spec/README.md`; other sections are sorted by file name. The Project section and its ordinary links remain visible.
  The Reference section's nested groups and page order are configured with `children` and `first` in
  [`site/config.json`](../site/config.json): Go API pages sit under the collapsed Go API Reference group, while
  the CLI Reference remains directly in Reference.
  The Design Records index appears as the first Overview link inside a nested Design Records disclosure, followed by
  numbered records in numeric file-name order. The disclosure stays closed by default, including on a record page.
  Record footers link to adjacent records. Keep record Status metadata concise, and keep the Design Records index's
  Status and Date cells on one line; its table scrolls horizontally on narrow screens. Numbered optimization records
  remain available through the Optimizations index and search, not the sidebar.
- **An example** for the playground is a line in [`playground/examples.txt`](../playground/examples.txt): a name, a
  grammar, an input and a description. The smoke test then checks it too.
- **Templates, styles and scripts** are in `site/templates/` and `site/static/`. They are plain HTML, CSS and JavaScript
  modules; there is no build step and no npm dependency.
  The client-side code highlighter is in `site/static/assets/highlight.js`; fenced code language aliases are mapped in
  `site/static/assets/site.js` and tested through the controlled-DOM harness in `site/testdata/site_highlight_test.mjs`.
  Code-block actions stay in a non-scrolling frame around the horizontally scrolling `pre` element, so Copy and
  Playground controls remain reachable beside long lines.
  Inline code wraps long tokens only when necessary; table code stays unbroken within a horizontally scrolling table.
  On page load, the sidebar scrolls a current link inside a closed disclosure to its visible summary.
