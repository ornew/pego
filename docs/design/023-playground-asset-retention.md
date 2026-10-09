# 023. Playground Asset Retention Across Deploys

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-10

## Context and problem

Pages identify the parser by a content-hashed WASM URL. Replacing a static deployment removes that URL even when
an older page has not loaded its parser yet. Retaining the WASM alone also leaves a Go-version compatibility gap:
the worker's `wasm_exec.js` must match the compiler/runtime that produced the binary.

The site builder clears its output and Netlify builds can start without a previous local output or build cache.
Retention must obtain the published assets independently of that local state, keep storage bounded, and preserve
the new parser selection.

## Goals and non-goals

Keep the current and immediately preceding parser/runtime pairs available for sequential production deployments.
Preserve the bytes identified by each hash, bootstrap existing pre-manifest deployments, and fail a build when an
existing published pair cannot be verified. Keep generated binaries outside Git and retain Go as the build toolchain.

This does not retain entire historical sites or promise indefinite compatibility for already loaded applications.
It does not coordinate overlapping production promotions, rollbacks or manually promoted previews. Such operations
must serialize their publication or use an explicit `-previous-site` pointing to the deployment being replaced.

## Proposal

### Deployment contract

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

### Asset format and worker behavior

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

### Validation and failure behavior

Manifest fields and paths are checked before use; names must match the full SHA-256 digests. Imported files are read
with limits of 64 MiB for WASM and 1 MiB for the runtime, and each HTTP request has a 30-second timeout. The manifest
is limited to 64 KiB. WASM also requires its version-one header; truncated-name collisions or a single WASM mapped
to conflicting runtimes fail rather than overwrite current content.

A missing manifest triggers a one-time bootstrap: read the published playground HTML's hashed WASM reference and
its stable runtime, checking the WASM filename hash. A missing playground too means a first deployment. Other HTTP,
JSON, missing-file, hash and size errors fail before clearing local output. Legacy HTML cannot identify an immutable
deploy or authenticate its runtime pairing; keep publication stable during this bootstrap. Subsequent manifests carry
the immutable source and both digests. A build failure does not promote any cache state or change the published site.

## Alternatives considered

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

## Performance and results

Retention adds one manifest request per hashed worker start, one prior pair download per production build, and at most
one additional WASM/runtime pair in published storage. No parsing work changes. `BenchmarkPublishAssets` measures local
8 MiB artifact hashing/writing with and without a distinct retained binary; it excludes Go compilation, network transfer,
browser compilation and CDN compression. Correctness-related measurements are recorded in the implementation commit;
they are not tuning methods in the optimization table.

## Limitations and open questions

A page older than one retained generation must reload. A page already holding a compiled module still needs a matching
runtime from a retained generation when restarting its worker. Stable application scripts and parser API schemas are not
archived, so a breaking application/protocol change still needs its own compatibility plan.

The imported generation is the one published when the production manifest is read. An overlapping promotion after
that read can make it differ from the deployment immediately preceding publication. Retention does not implement a
distributed deployment lock. Private sites must arrange accessible artifacts or provide a public immutable input;
credentials and query strings are not accepted in site URLs.
