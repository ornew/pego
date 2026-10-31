# Website Maintenance

This page is for contributors who build, test or publish ZeroParse's website. For using the browser playground and its WebAssembly API, see the [playground guide](guide/playground.md).

## Building and Previewing the Site

The site uses the pinned Nanosite standard `sitegen.BuildWith` pipeline. Its
configuration in `site/config.json` owns publication, defaults, front matter,
record indexes, navigation, pager ordering, and search. The ZeroParse driver adds
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
| `-previous-site` | Empty locally; `https://zeroparse.dev` in Workers Builds of `main` | Import the published current WASM/runtime pair to retain it for one deploy; requires `-wasm` |
| `-deploy-url` | Empty | Record this immutable deploy root for the next build's asset downloads |
| `-serve` | | After building, serve the output at this address |
| `-github`, `-ref` | `https://github.com/ornew/zeroparse`, `main` | Where links to source files point |

The output is plain static files. Any static server works, as long as it serves the files over HTTP (the playground
cannot run from `file://` URLs, because browsers do not load WebAssembly or workers from them):

```bash
python3 -m http.server -d site/dist 8080
```

The build output (`site/dist`) is not committed.

## What the Site Is Made Of

| URL | Source |
|:--|:--|
| `/` | The landing page, assembled from [`README.md`](../README.md): the tagline and introduction, a live example with the README's grammar, the feature list under "Why ZeroParse" as cards, and the following sections |
| `/docs/tutorial/`, `/docs/guide/`, `/spec/`, `/docs/design/`, `/docs/design/records/`, `/docs/…`, `/examples/` | The Markdown files of the repository, rendered at build time. Links between Markdown files (relative, relative to the repository root like `/spec/language/types.md`, or percent-encoded) become links between pages; links to other files of the repository go to GitHub. The design records get a generated index |
| `/reference/` | The API reference of every public package of the module (`zp`, `grammar`, `runtime`, `gen`, `lint`, `schema`, `query`, `sample` and `editfuzz`), generated from the Go source with `go/doc`, and the reference of the `zp` command, generated from what the command prints: its usage message and `zp <command> -h` for every command it lists. Types that are aliases of internal types (such as `Node`) show the definition and methods of the internal type. A site test fails when an importable package outside `internal/` that exports a name is missing from the API reference |
| `/playground/` | The playground: `app.js`, `worker.js`, content-hashed `wasm/zp-<hash>.wasm` and `runtime/wasm_exec-<hash>.js`, plus `assets.json` pairing current/previous Go and optional TinyGo generations; every page records the current Go WASM in `<html data-wasm>` and dual builds also record `data-tinygo-wasm`. The stable `wasm_exec.js` supports pre-manifest workers |
| `/search-index.json` | The text of every page, for the search box in the header (press `/`) |

Nothing is copied by hand, so the site cannot drift from the repository: rebuilding it picks up every change.

The pages need no external resources (no CDN, fonts or analytics), have light and dark themes (following the system
until the switch in the header is used), and work on phone screens.

## Deploying on Cloudflare Workers

Production builds (Workers Builds of the `main` branch) download the currently published WASM and its matching Go runtime before generating the new site.
The output retains at most the new pair and one previous pair per compiler; builds of other branches do not import production assets
automatically. A page from the preceding deploy can therefore start its old parser after publication. The worker reads
`assets.json` and imports the matching hashed runtime. Generations outside that window must reload.

Dual mode is opt-in: `site/build.sh -wasm-mode=dual` requires a compatible
TinyGo installation and Binaryen's `wasm-opt` executable on `PATH`. The
regular site builds use `go` mode. `site/install-playground-toolchains.sh`
provides the pinned, no-root Linux x86_64 setup for a build environment: TinyGo 0.43.0 and
Binaryen 131 are downloaded from their official releases, SHA-256 verified,
stored as archives under `PLAYGROUND_TOOLCHAIN_CACHE_DIR` (default
`$TMPDIR/zp-toolchain-cache`), and extracted into a temporary directory for
that build. Archive persistence depends on the build environment keeping that
path between builds; otherwise the script downloads them again. It rejects other operating systems and architectures
and fails on any missing tool, unexpected version, or checksum mismatch. In a
dual build, the Playground defaults to Go and offers
TinyGo under Options; the choice is carried by `?compiler=tinygo` in shared links. A TinyGo link
opened on a Go-only build reports that TinyGo is unavailable. Before enabling dual mode for
production, first deploy the updated builder with Go-only mode, then enable the
production build command `sh site/install-playground-toolchains.sh`. This staged
rollout leaves a Go-only deployment whose builder understands dual manifests
available for rollback. The production build command deliberately remains Go-only
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
site root, or from the immutable deploy URL when the manifest records one. Missing or invalid published assets fail the build; a site with no
playground, or one that answers with something other than a JSON manifest (such as the placeholder of a newly
created Worker), initializes a first deployment. Keep production publication sequential. Rollbacks and manually promoted
previews need `-previous-site` set to the deployment being replaced; retention does not coordinate concurrent promotions.
To deliberately reset retention, pass `-previous-site ''`. See [the asset-retention design](design/records/023-playground-asset-retention.md)
for the format, bounds and bootstrap limitations.

The official site, [zeroparse.dev](https://zeroparse.dev/), is a Cloudflare Worker with static assets and no Worker
script. [`wrangler.jsonc`](../wrangler.jsonc) at the root of the repository names the Worker (`zeroparse`) and its
assets directory, `site/dist`, the output of the site build; each build runs the link check, so a broken link fails
the deploy instead of publishing it. Cloudflare serves directory index files with a trailing slash
(`html_handling` is `auto-trailing-slash`); the site emits no `404.html`, so unknown paths get Cloudflare's plain 404
response (`not_found_handling` is `none`).

### Workers Builds Settings

Connect the Worker to the Git repository in the Cloudflare dashboard (Workers & Pages, the `zeroparse` Worker,
Settings, Builds) with these values:

| Setting | Value |
|:--|:--|
| Git repository | `ornew/zeroparse` |
| Production branch | `main` |
| Build command | `sh site/build.sh` |
| Deploy command | `npx wrangler deploy` |
| Build variable | `GO_VERSION` = `1.27.1` (the `go` directive of `go.mod` and `site/go.mod`) |
| Custom domain | `zeroparse.dev` (Settings, Domains & Routes) |

Automatic builds on push are off, because pushes to `main` are frequent and each build counts against the Workers
Builds allowance. A maintainer deploys the head of `main` from the dashboard (or with `npx wrangler deploy` after a
local build) when the site should change.

The build image has Node, so `npx` fetches Wrangler on demand. With preview builds enabled, branches other than
the production branch run the preview command, which defaults to `npx wrangler preview`; it creates or updates a
Preview without changing production. Builds of the
production branch import the published playground assets from `https://zeroparse.dev`, as described above. Once the
staged Go-only builder has been published, the production build command can be
`sh site/install-playground-toolchains.sh`.

### Deploying Manually

Build the site, then deploy the directory that `wrangler.jsonc` names (Node 20 or later; `npx` fetches Wrangler and
`wrangler login` or `CLOUDFLARE_API_TOKEN` authenticates):

```bash
sh site/build.sh
npx wrangler deploy --dry-run   # validate the configuration and the assets without publishing
npx wrangler deploy
```

A local build imports no earlier playground assets (`-previous-site` is empty), so use it for the first deployment,
before `https://zeroparse.dev` serves anything. For a later manual deployment, pass the published site explicitly,
`sh site/build.sh -previous-site https://zeroparse.dev`, so that pages open in browsers keep a working parser.

### Redirects and Headers

The build writes two control files into `site/dist`; Workers reads them and does not serve them.

- `_redirects` is generated by Nanosite from `redirects` in `site/config.json`: permanent `301` redirects from the
  former locations of the specification and the design records. Each line is a static redirect and uses one of the
  codes Workers supports. Workers applies redirects before it looks for an asset, so a redirect rule must name only
  paths where no page exists.
- `_headers` is [`site/static/_headers`](../site/static/_headers). It sets a strict `Content-Security-Policy`: the
  pages load scripts, styles and data only from the site itself, and need no inline scripts or styles;
  `'wasm-unsafe-eval'` allows compiling `zp.wasm`, and styles may be inline only because Mermaid sets them while it
  renders a diagram. The one outside origin is Cloudflare Web Analytics, which Cloudflare injects into the pages: its
  script from `https://static.cloudflareinsights.com` and its reports to `https://cloudflareinsights.com`. It also sets `X-Content-Type-Options` and `Referrer-Policy`. The WebAssembly binaries
  (`application/wasm`) and their hashed runtime scripts are cached for a year (`immutable`); the other files are
  revalidated on each visit, which is Workers' default.

Workers limits `_headers` to 100 rules and 2,000 characters per line, and `_redirects` to 2,000 static and 100 dynamic
rules. The local preview server (`-serve`) applies neither file; `npx wrangler dev` does.

Diagrams are written as `mermaid` fences. Only pages that contain one load `assets/mermaid.js`, which loads the pinned
Mermaid build vendored in `assets/vendor/` (the policy allows no remote scripts), renders each diagram once per color
theme and shows the matching image; the fence text remains where scripts do not run. To upgrade Mermaid, replace the
vendored file and its name, and update the version in `assets/mermaid.js`.

Formulas use the delimiters of GitHub's renderer, so one source reads the same on both: `$x$` is inline math, a
paragraph that begins and ends with `$$` is display math, and a `math` fence is display math too. The
[documentation style](documentation-style.md#examples) shows an example. Nanosite renders them with its built-in math
support, enabled by `"math": true` in the `markdown` object of `site/config.json`. Only pages that contain a formula
load the KaTeX 0.19.1 renderer, which Nanosite publishes under `assets/math/` with its stylesheet, fonts and license; a
formula that KaTeX rejects keeps its source. The delimiter rules and the deviations from GitHub are specified in
Nanosite's Markdown math chapter, `spec/markdown-math.md`. To upgrade KaTeX, update the Nanosite version in `site/go.mod`.


## Tests

| Test | What it checks |
|:--|:--|
| `go test ./cmd/playground` | The API natively (it has no build tags), and `TestWasmSmoke`: it builds `zp.wasm` and the `zp` command, and runs [`cmd/playground/testdata/smoke.mjs`](../cmd/playground/testdata/smoke.mjs) in Node, which compares every example of [`cmd/playground/examples.txt`](../cmd/playground/examples.txt) with the command: JSON, S-expression, errors, `-check`, every backend, byte positions, `zp fmt` and `zp gen -types`. It is skipped when `node` is not installed and in `-short` mode |
| `cd site && go test ./...` | Builds the whole site and checks that every Markdown file of `docs/` and `spec/` became a page, that the generated pages and the search index are complete, that no page needs inline scripts or styles, and that every internal link and anchor resolves. It also enforces the documentation rules of the
[style sheet](documentation-style.md): the directions in which pages may link (`TestDocumentLinkDirections`) and the
capitalization of headings (`TestHeadingTitleCase`), and the tags and prompts of code blocks (`TestCodeFenceLanguages`,
`TestNoCommandPrompts`). Run it with `-count=1`, because the result of a run that read
unchanged Go files is cached even when the Markdown changed. `TestJavaScript` runs the tests of the page scripts in `site/testdata/*_test.mjs` with `node --test` (skipped without Node) |

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

- **The grammar reference** in `spec/language/syntax.md` is generated with `go generate ./spec/language` from `zeroparse.zp` and `spec/language/syntax.md.tmpl`; it publishes the simplified syntax view and links to the canonical full grammar in `zeroparse.zp` on GitHub.
- **A new document** in `docs/guide/`, `docs/tutorial/`, `spec/`, `docs/design/`, `docs/design/records/` or `docs/` appears in its section
  automatically through the include rules in [`site/config.json`](../site/config.json).
  Add a section match there for a new documentation directory; unmatched pages otherwise appear under Other.
  Front matter can control publication, navigation, search, titles and routes without changing Markdown bodies.
- **The order** of the tutorial, the guides and the specification follows the links in `docs/tutorial/README.md`,
  `docs/guide/README.md` and `spec/README.md`; other sections are sorted by file name. The Project section and its ordinary links remain visible.
  The Reference section's nested groups and page order are configured with `children` and `first` in
  [`site/config.json`](../site/config.json): Go API pages sit under the collapsed Go API Reference group, while
  the CLI Reference remains directly in Reference.
  The Design Records index appears as the first Overview link inside a nested Design Records disclosure, followed by
  numbered records in numeric file-name order. The disclosure stays closed by default, including on a record page.
  Record footers link to adjacent records. Keep record Status metadata concise, and keep the Design Records index's
  Status and Date cells on one line; its table scrolls horizontally on narrow screens. Numbered optimization records
  remain available through the Optimizations index and search, not the sidebar.
- **An example** for the playground is a line in [`cmd/playground/examples.txt`](../cmd/playground/examples.txt): a name, a
  grammar, an input and a description. The smoke test then checks it too.
- **Templates, styles and scripts** are in `site/templates/` and `site/static/`. They are plain HTML, CSS and JavaScript
  modules; there is no build step and no npm dependency.
  The client-side code highlighter is in `site/static/assets/highlight.js`; fenced code language aliases are mapped in
  `site/static/assets/site.js` and tested through the controlled-DOM harness in `site/testdata/site_highlight_test.mjs`.
  Code-block actions stay in a non-scrolling frame around the horizontally scrolling `pre` element, so Copy and
  Playground controls remain reachable beside long lines.
  Inline code wraps long tokens only when necessary; table code stays unbroken within a horizontally scrolling table.
  On page load, the sidebar scrolls a current link inside a closed disclosure to its visible summary.
