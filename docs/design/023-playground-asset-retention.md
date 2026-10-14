# 023. Playground Asset Retention Across Deploys

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-10

## Context and Problem

Pages identify the parser by a content-hashed WASM URL. Replacing a static deployment removes that URL even when
an older page has not loaded its parser yet. Retaining the WASM alone also leaves a Go-version compatibility gap:
the worker's `wasm_exec.js` must match the compiler/runtime that produced the binary.

The site builder clears its output and Netlify builds can start without a previous local output or build cache.
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

The CLI defaults these flags from [Netlify's build environment variables](https://docs.netlify.com/build/configure-builds/environment-variables/)
only when `NETLIFY=true`, `CONTEXT=production` and this is not a Preview Server: `URL` for the previous site and
`DEPLOY_URL` for the new immutable source. Local builds and deploy/branch previews default to neither flag. Explicit
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

## Publication-Time Prototype

The existing build remains the sequential-deployment implementation above. The
publication coordinator in `site/publication.go` is a local prototype with an
injected host interface; it does not implement a Netlify adapter or alter the
normal build command. Its input is a completed, frozen site directory, including
its current pair. A failed or incomplete candidate cannot enter publication.

For each attempt, the coordinator reads the host's authoritative published
deployment ID and immutable URL, downloads that deployment's verified current
pair, and creates a private staged copy of the candidate. It removes the copied
`wasm/` and `runtime/` generations and publishes only the candidate current pair
and the actual predecessor. It reads the candidate's hashed runtime rather than
its stable `wasm_exec.js`, which may already contain a build-time predecessor's
runtime. Content hashes, pairing, same-pair deduplication and the two-pair storage
bound retain the existing format.

The host must upload the prepared site without promoting it, set the manifest's
`source` to the new immutable staging URL, and atomically compare the expected
published deployment ID while promoting. A conflicting promotion discards staging
and restarts predecessor selection, up to an explicit attempt limit. The same
operation applies to an old candidate selected for rollback and to a manually
selected preview. Directly restoring an unchanged historical deploy bypasses this
process and cannot acquire the current predecessor's pair. Candidates are copied;
existing immutable deployments are never edited.

This interface requires an atomic host primitive or an exclusive publisher that
controls **every** production transition for the whole read/stage/promote interval.
A second current-deploy read followed by an ordinary promotion remains racy.
Build completion order, build concurrency limits and a build plugin's success hook
do not provide that publication authority.

### Hosting Policy Options

[Netlify's deployment guide](https://docs.netlify.com/deploy/manage-deploys/manage-deploys-overview/)
documents a lock that disables automatic publishing while still allowing a
completed deployment to be published manually. The
[API guide](https://docs.netlify.com/api-and-cli-guides/api-guides/get-started-with-api/)
describes draft uploads and restoration of an existing deploy. The
[published API schema](https://github.com/netlify/open-api/blob/master/swagger.yml)
exposes the current `published_deploy`; its restore operation lists the site and
deploy IDs and does not document an expected-current-deploy precondition. These
interfaces establish no demonstrated compare-and-swap guarantee for this prototype.

The recommended next policy is a locked site with one serialized publisher. It
must queue ordinary, manually selected and rollback candidates alike, rebuild
retention under the publication lock, upload privately, then promote. Human direct
restore actions and other production credentials must follow that authority; a
Netlify auto-publish lock alone is insufficient. Adapter failure/timeout handling
must distinguish an upload failure from an uncertain promotion result: reread the
authoritative state and verify the promoted immutable pair before retrying.

An alternative is immutable content-addressed asset storage independent of site
promotions. This supports arbitrary host restore operations, but needs an explicit
expiry/capacity policy, matching runtime storage, and revised client loading/CSP
contracts. Another alternative is pinning each page and worker to an immutable
deployment URL. This delegates lifetime to host deploy retention and needs browser,
CORS and worker-origin validation. Neither alternative is implemented here.

The fake host tests interleave A/B/C publication and check that C retains B after
an A-based attempt conflicts, then exercise manual selection, rollback, failures,
first deployment and identical pairs. HTTP requests through the production alias
model an already-open page obtaining the exact retained WASM/runtime bytes. These
tests prove the coordinator contract when the host primitive is atomic; they do
not establish a live hosting guarantee.

## Optional Compiler-Pair Experiment

`site/build-paired-wasm.sh` invokes the isolated `site/cmd/paired-wasm` experiment.
It requires an explicitly selected TinyGo version and a new output directory:

```sh
sh site/build-paired-wasm.sh -out /tmp/pego-compiler-pairs -tinygo-version 0.43.0
```

The command requires Go 1.27.1, checks the requested TinyGo version, builds the
same `./playground` source with both compilers, and obtains each runtime from that
compiler installation. [TinyGo's WASM guide](https://tinygo.org/docs/guides/webassembly/wasm/)
requires its own matching `targets/wasm_exec.js`; the Go build uses
`GOROOT/lib/wasm/wasm_exec.js`. A compiler or pairing failure leaves the requested
output absent. No generated binary is committed and the normal site build remains
Go-only.

The comparison output has `variants.json` with `version: 1`, `default: "go"`, and
a `variants` array of `{id, base, compiler, wasm}` entries. Separate `go/` and
`tinygo/` directories each contain a version-one `assets.json` with their current
pair, hashed WASM/runtime files, stable runtime and a copy of the same worker.
This keeps the existing manifest format and runtime lookup intact. A comparison
caller can select an entry, resolve its `base` against the catalog URL and create
`PegoClient(base, {wasm: entry.wasm})`. Selecting a compiler requires a fresh client
and worker, since their JavaScript runtimes are incompatible. The catalog is not
wired into the production UI, publication coordinator or site output.

Building successfully does not establish compatibility. Before publishing a
selectable compiler, run the shared smoke corpus against both compiler pairs and
compare grammar compilation, results, errors, every backend, recovery, Unicode,
byte positions, formatting, generation and options. Test callback readiness,
restarts, traps and cancellation in actual browsers, including memory pressure.
Record the exact toolchains, raw/gzip/Brotli sizes, startup, parse latency and
memory under identical inputs and browser conditions. TinyGo must support the
project's Go version and standard-library dependencies without silently dropping
API methods or switching semantics. Go remains the default and fallback until
those gates have evidence; no performance or compatibility result is claimed by
the artifact builder.

## Alternatives Considered

- **Keep the previous local output.** It is absent on fresh build machines and cannot identify which build was published.
- **Commit historical binaries.** Reliable input, but repeatedly grows Git history by megabytes; source commits already
  allow rebuilding and generated artifacts belong in deployment storage.
- **Use a Build Plugin cache.** This adds a Node plugin and successful-publication state. The documented examples save
  before deployment; success-hook persistence and concurrent-build ordering would need separate deployment evidence.
- **Redirect all old hashes to the current binary.** Breaks content identity and can mix incompatible runtime/API versions.
- **Archive every historical deploy or bundle.** Provides a longer window but needs a storage/lifetime policy beyond one
  preceding pair. Netlify deploy permalinks remain usable without copying all generations to the production alias.

## Testing

`site/assets_test.go` checks A→B→C retention, byte identity, matching runtimes, current selection, same-pair deduplication,
immutable-source fetches, first deployment, legacy bootstrap, production/preview defaults, path/hash/HTTP/size failures
and preservation of local output on retrieval failure. `worker_assets_test.mjs` executes the actual worker with controlled
fetch/Go dependencies to check current/previous runtime selection and failures. Full site/link tests include the built
WASM; the playground smoke suite checks the parser API separately.

Local deployment simulation starts from a pre-manifest built site, replaces its output at the same origin, and checks
both parser generations and the real browser worker. This proves the builder/worker interaction; it does not substitute
for observing Netlify's deployment status.

## Performance and Results

Retention adds one manifest request per hashed worker start, one prior pair download per production build, and at most
one additional WASM/runtime pair in published storage. No parsing work changes. `BenchmarkPublishAssets` measures local
8 MiB artifact hashing/writing with and without a distinct retained binary; it excludes Go compilation, network transfer,
browser compilation and CDN compression. Correctness-related measurements are recorded in the implementation commit;
they are not tuning methods in the optimization table.

## Limitations and Open Questions

A page older than one retained generation must reload. A page already holding a compiled module still needs a matching
runtime from a retained generation when restarting its worker. Stable application scripts and parser API schemas are not
archived, so a breaking application/protocol change still needs its own compatibility plan.

The imported generation is the one published when the production manifest is read. An overlapping promotion after
that read can make it differ from the deployment immediately preceding publication. Retention does not implement a
distributed deployment lock. Private sites must arrange accessible artifacts or provide a public immutable input;
credentials and query strings are not accepted in site URLs.
