# 045. Serialized Recognition Programs for Compiled Grammars

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Store a separate recognition module only in compiled files without the grammar AST, so the existing
`Parser.Parse(input, pego.RecognizeOnly())` and `pego parse -check` work for bytecode backends without that AST.
AST-bearing files remain container version 2; AST-less files with an available complete recognizer use version 3.
Keep ordinary parsing and recognition caches separate. Recognition builds only values needed by predicates and
their callees; it does not evaluate unrelated actions merely to discard their trees. No new public recognition
entry point is required.

## Current Behavior

[parse.go](../../internal/engine/parse.go) lazily builds a recognizer from `Program.Grammar`; without it the error is
`recognition needs the grammar, which the compiled grammar omits`. [compile.go](../../internal/engine/compile.go)
already has a recognition compilation mode and `valueNeededRules`: rules supplying predicate captures and their
transitive callees retain value construction/actions. Other rules suppress values and actions. Recognition can
therefore report predicate-dependent evaluation behavior even though unrelated action errors disappear.

Normal [bytecode](../../spec/bytecode.md#rule-table) contains value-free twins only for eligible discarded CST calls.
Twins are not a complete recognizer: action/terminal rules, Operators Rule declarations, LR cycles, streaming rules and selected
start rules may have no such twin. Matching a full module with CALL.keep=0 or discarding the final tree still
executes actions and builds intermediate values. A recognizer must preserve predicate capture dependencies,
variable reads and diagnostics, not simply jump to an arbitrary duplicate name in the rule table.

Version-2 containers store an ISA version, the main module and optional AST. The current spec uses ISA 3 for
ordinary modules, 4 for wide bounds, and higher versions for later instruction additions. Loading validates
checksum, tables, operands and recovery regions; it does not establish general termination or recheck all
type/stack behavior. The version-3 extension preserves the trusted-module restriction.

## Design

### API, Start Selection and Backend Behavior

Keep `MarshalBinary`, `Marshal`, `WithoutAST`, `LoadParser`, `WithStart` and RecognizeOnly signatures unchanged.
AST-bearing saves use container version 2 without a recognition bundle. When the AST is omitted and a complete
recognizer is available, serialization uses version 3 and includes that bundle. Source compilation uses the
existing recognition transformation and analysis; do not introduce a weaker bytecode recognizer. Compile and
serialize it once on demand through a thread-safe initialization path, not on each parse.

The recognition bundle includes an explicit ordered entry map from every original grammar rule name to its
recognition rule index. This map must cover all possible WithStart selections, not just the saved default. A
value-free original can have NoValue=true: that flag does not make it an unnamed twin. Validate unique map names,
index/name agreement, and equality of the original start-name sets between bundles. Additional twins remain
internal. Internal Program.Rules, the public Parser.Start and WithStart APIs, and the saved default start retain the main
module's original-rule eligibility contract.

On a new AST-less load, default recognition uses the recursive bytecode backend, matching current default backend
resolution; explicit iterative selection uses the same recognition code with the iterative VM. Explicit Closure
still fails because an AST is unavailable. With AST, preserve the usual Closure default and explicit VM choices,
but all recognition modes must match the freshly compiled reference. Ordinary parsing is unchanged. Recognition
returns a nil public Node on success, syntax failure, recovery and fatal errors; recovered errors remain returned.
It consumes the entire selected start input, reporting the existing end-of-input expectation for trailing text.

Loaded version-1 or version-2 files containing ASTs retain lazy recognition compilation. AST-less old files cannot
promise recognition equivalence from their partial twins; return a capability error recommending recompilation.
Do not silently run a full parse as a fallback. Newly saving an old AST-less module also cannot create a complete
recognizer: preserve the legacy version-2 main module and absence of capability rather than fabricate a bundle.
Document that recompilation from source is necessary to upgrade it. A low-level file with no saved start still
requires explicit start resolution; the public LoadParser contract continues rejecting a missing default start.

CLI -check uses the same API and backend/unit resolution, prints `ok` only when error is nil, and returns recovered
diagnostics as an error as it does today. Validate the incompatible -check/-stream combination rather than silently
ignoring -check. Document and ParseStream continue rejecting recognition; generated recognition stays independent
of this file-format feature. Generation/formatting from an AST-less module still requires its absent grammar AST.

### Versioned Layout

Container version 3 keeps the version-2 prefix (header, ISA, outer string table, start/package and main module),
then stores a recognition-present boolean and, when true, a length-delimited recognition bundle before the
AST-present flag and AST payload. The recognition bundle has its own ISA version, local string table, module, and
ordered name/index entry map. Its indices never refer to the main module's tables. The existing CRC covers the
complete container. Deterministic ordering and local string interning make repeated serialization and load/save
byte-identical for the same writer-emitted form. AST-bearing files use version 2 without a bundle. Loaders may accept
legacy or experimental version-3 AST-bearing files, but re-saving them canonically writes version 2 and drops the
recognition bundle.

Store the full semantic recognition program, including predicate-required expression code/type tables. There is
no need for new match opcodes just to execute the second program. Each module declares the ISA actually required,
including wide repetition bounds; validation and runtime ISA support apply independently to both. A NoValue flag
in the recognizer denotes execution semantics, while the explicit entry map denotes public start eligibility.
Specify that distinction without rewriting the meaning of twins in legacy normal modules.

Old loaders reject version 3. New loaders accept versions 1, 2 and 3 with their documented capability differences.
The loader requires bundle absence to be explicit and never treats it as "use normal twins"; the writer emits
version 3 only when a recognition bundle is present.
Reject noncanonical booleans, invalid bundle lengths/trailing bytes, invalid ISA, indices, duplicated/missing
start names, and mismatched names before exposing a Parser. Validate every module, even one not selected by the
current default backend. Valid checksum alone is insufficient. Do not add executable user callbacks to the format.

### Runtime and Ownership Invariants

Keep recognition and ordinary prepared VM programs, seen numbering, memo entries and pools distinct. The same
loaded Parser supports concurrent ordinary and recognition calls and independent WithStart views; immutable
module tables can be shared, mutable invocation stacks and caches cannot. Copy configuration once before start
lookup/execution, preserving current validation precedence and depth semantics. Do not retain option callbacks.

Preserve predicate capture/environment evaluation and memo keys, including transitive variable reads and Operators Rule
minimum level. LR leaders grow by strictly increasing input positions, preserve provisional dependencies and use
the same candidate/error rollback. Recognition still records expectation ranges, silent lookahead contexts,
labels, cuts, recovered diagnostics and Operators Rule longest-match ties. Value suppression must not remove a capture that
a predicate reads or its action-produced fields. Runtime errors in skipped actions are absent by contract; errors
in required actions retain the existing evaluation/fatal rules.

Compile a source #stream repetition into the recognition batch behavior (NEXT mode 0 where appropriate), never
stream commitment or callbacks. Keep the empty-iteration stop/count and finite bounds unchanged. Consuming recovery
still requires a nonempty skip, and emits diagnostics without an unnecessary public Error tree. Any future missing
token/cancellation/work-budget feature must version its own instructions if needed and be compiled into both
bundles; omission cannot silently change recognition acceptance.

On fatal depth, action failure or cancellation, release capture/value/environment/expectation scratch and discard
incomplete LR state before pool reuse. No caller-visible values are cleared by releasing private storage. The API
returns no recognition tree, but retained diagnostics own their strings/slices and cannot alias pooled scratch.
Malformed-module runtime faults retain the current error handling; this feature does not promise execution of
untrusted adversarial modules is safe or terminating.

## Backend and Implementation Gates

| Input/program | Closure | Recursive VM | Iterative VM | Generated Go/typed Go/TS |
| --- | --- | --- | --- | --- |
| Source or loaded AST | Existing recognizer, preserved | Existing semantics, parity gate | Existing semantics, parity gate | Existing standalone recognizers |
| New AST-less version-3 recognition bundle | Explicit unavailable error | Supported | Supported | No new loading/generation API |
| Legacy AST-less file | Explicit unavailable error | Explicit capability error | Explicit capability error | Not applicable |

All supported engine cells require CodePoints and Bytes, including invalid UTF-8. Generated TS's string/byte,
integer and Unicode contracts are unchanged; a portable compiled VM is outside this feature. Public node/scalar
typing restrictions are unchanged, including top-level scalar rejection.

The candidate writer/loader follows the selected contract: AST-bearing saves remain version 2, while AST-less
saves include the complete recognition bundle when available. Finish backend parity, legacy-format, corruption,
generated-parser and full-suite validation before integration; keep the user guide and bytecode format tables
synchronized with the final writer behavior.

## Alternatives Considered

- Keep ASTs and rebuild recognition: the current baseline and a compatibility fallback for old AST-bearing files,
  but it defeats AST-less distribution and incurs compilation on first recognition.
- Return nil after running the normal module: easy, but evaluates unrelated actions and retains tree allocation;
  it violates the recognition contract.
- Derive a complete recognizer from normal bytecode on load: might reduce saved bytes, but requires a validated
  dependency/control-flow transformation for captures, predicates, actions, Operators Rule and LR. Defer until a separate
  equivalence proof and cost comparison; available twins alone are insufficient.
- Store only a recognition module: smaller for recognition-only distribution but removes ordinary Parse capability.
  A separately explicit artifact mode may be added later; do not disguise it as WithoutAST.
- Lazily serialize only requested start rules: smaller for one caller, but breaks WithStart's existing contract.
  Choose the complete original-rule entry map.

## Validation and Performance Plan

Differential tests run every corpus start rule through source recognition and AST-bearing/AST-less
save/load recognition, both units and both VM models. Cover EOF/trailing text, invalid UTF-8, variables, nested
predicate captures and transitive action dependencies, skipped action failures versus required action errors,
LR/Operators Rule, labels/cuts, recovery rollback, silent memo reuse, streaming annotations and wide zero/large bounds.
Assert nil Node on every outcome and exact diagnostics, not just acceptance. Compare ordinary Parse before/after,
concurrent WithStart views, depth/cancellation abort/retry and pool cleanup. Test versions 1/2 compatibility,
version-3 golden determinism, AST-less legacy resaving, missing/duplicate entry maps, checksum-valid malformed
bundles, unknown ISA/opcodes and independent section-length/index errors. CLI tests cover saved and overridden
start rules, -check output/status, both backend flags, units and -check/-stream rejection.

No broad performance result is claimed. Measure save/load, first recognition preparation and warm recognition separately
from source compilation and full parsing. Compare exact parent AST-bearing recognition, candidate AST-bearing and
AST-less bundles, and ordinary parsing as a regression control; do not time the parent's expected capability error
as equivalent useful work. Include action-free and predicate-heavy corpora, LR/Operators Rule, recovery and both units.
Record container/AST/module bytes, executable size where affected, time/allocations, retained prepared-module
memory and transient/live memory during loading. Pair serialized runs with pinned revisions/toolchains/inputs.

### Focused AST-less Bundle Measurements

A focused prototype comparison used synthetic grammars with 16 and 128 rules on Go 1.27.1, Apple M3 Max
(`darwin/arm64`, `GOMAXPROCS=16`). Each benchmark used five samples with a 100 ms benchtime. It compared an
AST-less version 2 file without a recognition bundle against an AST-less version 3 file with one:

| Rules | File size, v2 → v3 | Cached save, v2 → v3 | First compile + save, v2 → v3 | Load, v2 → v3 |
| ---: | ---: | ---: | ---: | ---: |
| 16 | 1,208 → 2,415 B | 11,384 → 11,423 ns (+0.3%) | 55,814 → 96,482 ns (+73%) | 18,766 → 24,955 ns (+33%) |
| 128 | 10,189 → 20,619 B | 78,539 → 84,609 ns (+7.7%) | 454,742 → 787,141 ns (+73%) | 131,411 → 200,003 ns (+52%) |

The writer caches the encoded recognition bundle for repeated saves. A separate earlier run, before that cache was
added, measured v3 repeated-save medians of 23,686 ns and 174,388 ns for these grammar sizes. Since those results
came from a different run, they do not isolate the cache's effect. The focused comparison does not measure ordinary
parsing or the full test suite, and synthetic grammar results may not predict application workloads. These
measurements describe the selected AST-less bundle but do not establish costs for application grammars.

### Rejected AST-Bearing Bundle Alternative

A separate synthetic comparison measured the earlier alternative of adding a recognition bundle to AST-bearing
files. The selected design excludes that mode: AST-bearing saves stay version 2. On an Apple M3 Max
(`darwin/arm64`) with Go 1.27.1, GOMAXPROCS=4, GOGC=100 and
GOMEMLIMIT disabled, grouped benchmarks used five 100 ms samples. The version 3 recognition bundle's encoded
cache capacity was 1,280 B for 16 rules and 10,880 B for 128 rules.

| Rules | File size, v2 → v3 | First compile + save, v2 → v3 | Load, v2 → v3 |
| ---: | ---: | ---: | ---: |
| 16 | 1,430 → 2,637 B | 66,735 → 112,304 ns | 30,318 → 41,304 ns |
| 128 | 12,127 → 22,557 B | 520,695 → 938,359 ns | 237,661 → 309,062 ns |

Ordinary `Parse` on the same fixtures had equal allocation counts in Default and bytecode modes, with timing
variation within measurement noise. An independent five-pair alternating 128-rule comparison (250 ms per sample)
measured v2/v3 medians of 557,708/944,768 ns for first compile plus save (+69.4%) and 240,047/300,357 ns for
load (+25.1%); version 3 was slower in each pair. These results are limited to synthetic grammars and do not
establish costs for application grammars. The benchmark source is
`internal/engine/compiled_recognition_bench_test.go`; raw result files remain outside the repository. Because
`MarshalBinary` writes AST-bearing saves as version 2, the benchmark writes the rejected version 3 layout with a
test-only encoder.

Reproduce the AST-less serialization matrix with:

```sh
go test -run '^$' -bench '^BenchmarkCompiledRecognitionSerialization$' -benchmem -benchtime=100ms -count=5 ./internal/engine
```

Run the AST-bearing matrix with:

```sh
go test -run '^$' -bench '^BenchmarkCompiledRecognition(ASTBearing|Serialization)$' -benchmem -benchtime=100ms -count=5 ./internal/engine
```

The separate alternating 128-rule comparison used repeated v2 and v3 benchmark invocations in alternating order;
`-count=5` alone does not reproduce that pairing.
