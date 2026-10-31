# 023. Playground Asset Retention Across Deploys

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-10

## Context and Problem

Pages identify the parser by a content-hashed WASM URL. Replacing a static deployment removes that URL even when
an older page has not loaded its parser yet. Retaining the WASM alone also leaves a Go-version compatibility gap:
the worker's `wasm_exec.js` must match the compiler/runtime that produced the binary.

The site builder clears its output and Workers Builds can start without a previous local output or build cache.
Retention must obtain the published assets independently of that local state, keep storage bounded, and preserve
the new parser selection.

## Goals and Non-Goals

Keep the current and immediately preceding parser/runtime pairs available for sequential production deployments.
Preserve the bytes identified by each hash, bootstrap existing pre-manifest deployments, and fail a build when an
existing published pair cannot be verified. Keep generated binaries outside Git and retain Go as the build toolchain.

This does not retain entire historical sites or promise indefinite compatibility for already loaded applications.
It does not coordinate overlapping production promotions, rollbacks or manually promoted previews. Such operations
must serialize their publication or use an explicit `-previous-site` pointing to the deployment being replaced.

## Proposal

### Deployment Contract

`-previous-site` names a published site root, including a path prefix if present. The builder reads its
`playground/assets.json` before clearing output. Only that manifest's **current** pair is imported; its previous pair
is never inherited. The new manifest contains the newly built current pair and, when different, one previous pair.
Identical pairs are deduplicated. The output has at most two WASM binaries and two content-hashed runtime scripts.

`-deploy-url` records the immutable root URL for the new deployment in the manifest's `source`. The next build reads
the manifest at the production alias, then downloads its current assets from that immutable URL. This prevents an
alias change between those downloads from mixing deploys. A local/manual build without that flag downloads from the
specified site root; the caller must keep it stable during the build.

The CLI defaults `-previous-site` to `https://zeroparse.dev` only in a Workers Builds run of the production branch
(`WORKERS_CI=1` and `WORKERS_CI_BRANCH=main`). Workers Builds knows no immutable URL for a deployment before it is
published, so `-deploy-url` has no default there and the next build reads the manifest and the assets from the
production site root. Local builds and builds of other branches default to neither flag. Explicit
flags override the defaults, including an empty `-previous-site` for a deliberate reset.

### Asset Format and Worker Behavior

`playground/assets.json` has `version: 1`, optional `source`, `current`, and optional `previous`. Each pair records:

- `wasm`: `wasm/pego-<16 hex characters>.wasm`, with the full `wasmSHA256`;
- `runtime`: `runtime/wasm_exec-<16 hex characters>.js`, with the full `runtimeSHA256`.

The worker requests the revalidated manifest when starting a content-hashed parser, finds the exact requested WASM
URL in either pair, and imports its matching runtime. An expired parser or unavailable manifest fails initialization;
an expired-generation message asks the user to reload. The explicit unhashed `pego.wasm` convention continues to use
`wasm_exec.js` for standalone callers.

Pre-manifest workers import the stable `wasm_exec.js` path. When importing a previous pair, the builder serves that
pair's runtime there; the new worker uses hashed runtimes instead. Without a previous pair, the stable path contains
the current runtime. Applications must use the manifest to select a runtime for generated site binaries.

### Validation and Failure Behavior

Manifest fields and paths are checked before use; names must match the full SHA-256 digests. Imported files are read
with limits of 64 MiB for WASM and 1 MiB for the runtime, and each HTTP request has a 30-second timeout. The manifest
is limited to 64 KiB. WASM also requires its version-one header; truncated-name collisions or a single WASM mapped
to conflicting runtimes fail rather than overwrite current content.

A missing manifest triggers a one-time bootstrap: read the published playground HTML's hashed WASM reference and
its stable runtime, checking the WASM filename hash. A missing playground too means a first deployment. Other HTTP,
JSON, missing-file, hash and size errors fail before clearing local output. Legacy HTML cannot identify an immutable
deploy or authenticate its runtime pairing; keep publication stable during this bootstrap. Subsequent manifests carry
the immutable source and both digests. A build failure does not promote any cache state or change the published site.

## Alternatives Considered

- **Keep the previous local output.** It is absent on fresh build machines and cannot identify which build was published.
- **Commit historical binaries.** Reliable input, but repeatedly grows Git history by megabytes; source commits already
  allow rebuilding and generated artifacts belong in deployment storage.
- **Use a Build Plugin cache.** This adds a Node plugin and successful-publication state. The documented examples save
  before deployment; success-hook persistence and concurrent-build ordering would need separate deployment evidence.
- **Redirect all old hashes to the current binary.** Breaks content identity and can mix incompatible runtime/API versions.
- **Archive every historical deploy or bundle.** Provides a longer window but needs a storage/lifetime policy beyond one
  preceding pair. Cloudflare version preview URLs remain usable without copying all generations to the production site.

## Testing

`site/assets_test.go` checks A→B→C retention, byte identity, matching runtimes, current selection, same-pair deduplication,
immutable-source fetches, first deployment, legacy bootstrap, production/preview defaults, path/hash/HTTP/size failures
and preservation of local output on retrieval failure. `worker_assets_test.mjs` executes the actual worker with controlled
fetch/Go dependencies to check current/previous runtime selection and failures. Full site/link tests include the built
WASM; the playground smoke suite checks the parser API separately.

Local deployment simulation starts from a pre-manifest built site, replaces its output at the same origin, and checks
both parser generations and the real browser worker. This proves the builder/worker interaction; it does not substitute
for observing the deployment status in the Cloudflare dashboard.

## Performance and Results

Retention adds one manifest request per hashed worker start, one prior pair download per production build, and at most
one additional WASM/runtime pair in published storage. No parsing work changes. `BenchmarkPublishAssets` measures local
8 MiB artifact hashing/writing with and without a distinct retained binary; it excludes Go compilation, network transfer,
browser compilation and CDN compression. Correctness-related measurements are recorded in the implementation commit;
they are not tuning methods in the optimization table.

The dual-compiler prototype was measured on an Apple M3 Max (darwin/arm64) with
Go 1.27.1, Node 22, TinyGo 0.43.0 and Binaryen 131 (`wasm-opt`), using five
alternating pairs and matched WASM/runtime files. The Go WASM was 9.6 MiB and
the TinyGo WASM 3.5 MiB; the dual site build was 25.4 MB versus 21.7 MB for a
Go-only build. Across calculator, JSON, XML and pairs examples, TinyGo's
Node-measured startup-to-ready ratios were 0.33–0.34× Go, compile-call ratios
0.73–0.91×, and warm parse-only ratios 0.75–0.92×. Full warm parse API calls
were slower for JSON (2.33×) and XML (2.17×), while calculator and pairs were
near parity. The tree-heavy API-call regressions are why Go remains the default.
These are Node measurements, not browser latency; a Chrome run verified worker
startup, compiler switching and shared-link reload behavior.

A separate narrow 64-bit engine comparison used five alternating pairs.
`BenchmarkLoad/compiled` candidate/main had a median ratio of 1.001 (range
0.916–1.087), with bytes and allocations unchanged. Closure/CodePoints
`BenchmarkDocumentReparse` had a median ratio of 0.990 (range 0.976–1.005),
with bytes, allocations and resumed counts unchanged. These two profiles show
no stable regression; they are not a full benchmark comparison.

## Limitations and Open Questions

### Dual-compiler publication and rollback

An opt-in `-wasm-mode=dual` build extends the version-one manifest with optional
`tinygoCurrent` and `tinygoPrevious` pairs. The required `current` and optional
`previous` fields continue to identify Go binaries, so older workers still
resolve Go. Dual builds keep up to four binary/runtime pairs (current and
preceding Go and TinyGo); a Go-only build may retain a preceding TinyGo pair
for one deployment so an older page can restart. TinyGo requires its own
`wasm_exec.js`, and the worker chooses by the requested binary's manifest pair.
The added fields are accepted by the updated builder, but a builder from before
this extension rejects them because it decodes manifests with
`DisallowUnknownFields`. A rollback to that builder therefore needs an explicit
retention reset or a compatible manifest reader. Browser startup and API parity
remain validation gates before enabling dual mode in production.

A page older than one retained generation must reload. A page already holding a compiled module still needs a matching
runtime from a retained generation when restarting its worker. Stable application scripts and parser API schemas are not
archived, so a breaking application/protocol change still needs its own compatibility plan.

Production activation uses two publications. First publish the dual-capable
builder with Go-only mode; this creates a rollback target that can read the
optional TinyGo manifest fields while retaining the ordinary Go pair. Only then
enable the production build to install the pinned Linux x86_64
toolchains and build in dual mode. Preview and branch contexts remain Go-only.
The repository keeps this production context disabled until the owner selects
this staged rollout or the reset alternative.

Rollback to the staged Go-only builder preserves the preceding Go pair and
does not need a retention reset. A rollback farther to a builder that predates
TinyGo manifest support must explicitly build Go-only with
`-previous-site ''`; this avoids strict-decoder failure but drops every
preceding Go and TinyGo pair, so older pages must reload. Never use that older
builder with the dual manifest and normal production `URL` retention.

The imported generation is the one published when the production manifest is read. An overlapping promotion after
that read can make it differ from the deployment immediately preceding publication. Retention does not implement a
distributed deployment lock. Private sites must arrange accessible artifacts or provide a public immutable input;
credentials and query strings are not accepted in site URLs.
