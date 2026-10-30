# 045. Serialized Recognition Programs for Compiled Grammars

- **Status**: Superseded
- **Author**: @ornew
- **Date**: 2026-10-11

> **Note**: Superseded by the saved-program decision of
> [Backend-Independent Optimization IR](062-backend-independent-optimization-ir.md#decisions): a compiled grammar
> holds the module of one mode, and a file saved in recognize mode holds the recognizer's module
> ([Compiled Grammars](../../../spec/runtime/compiled-grammars.md)). The record keeps the original decision.

## Summary

Store a separate recognition module only in compiled files without the grammar AST, so the existing
`Parser.Parse(input, pego.RecognizeOnly())` and `pego parse -check` work for bytecode backends without that AST.
AST-bearing files store no recognition bundle; AST-less files with an available complete recognizer store one.
Both use container format version 4, which also holds the optional schema section.
Keep ordinary parsing and recognition caches separate. Recognition builds only values needed by predicates and
their callees; it does not evaluate unrelated actions merely to discard their trees. No new public recognition
entry point is required.

## Current Behavior

[program_parse.go](../../../internal/engine/program_parse.go) lazily builds a recognizer from `Program.Grammar`; without it the error is
`recognition needs the grammar or a recognition bundle; recompile this compiled grammar from source`. [compile.go](../../../internal/compile/compile.go)
already has a recognition compilation mode and `valueNeededRules`: rules supplying predicate captures and their
transitive callees retain value construction/actions. Other rules suppress values and actions. Recognition can
therefore report predicate-dependent evaluation behavior even though unrelated action errors disappear.

Normal [bytecode](../../../spec/bytecode/README.md#rule-table) contains value-free twins only for eligible discarded CST calls.
Twins are not a complete recognizer: action/terminal rules, Operators Rule declarations, LR cycles, streaming rules and selected
start rules may have no such twin. Matching a full module with CALL.keep=0 or discarding the final tree still
executes actions and builds intermediate values. A recognizer must preserve predicate capture dependencies,
variable reads and diagnostics, not simply jump to an arbitrary duplicate name in the rule table.

A container stores the instruction-set version 26, the main module and optional AST, as
[spec/bytecode/README.md](../../../spec/bytecode/README.md) defines. Loading validates
checksum, tables, operands and recovery regions; it does not establish general termination or recheck all
type/stack behavior. Recognition bundles keep the trusted-module restriction.

## Design

### API, Start Selection and Backend Behavior

Keep `MarshalBinary`, `Marshal`, `WithoutAST`, `LoadParser`, `WithStart` and RecognizeOnly signatures unchanged.
AST-bearing saves have no recognition bundle. When the AST is omitted and a complete recognizer is available,
serialization includes that bundle. Source compilation uses the
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

A loaded AST-bearing file compiles its recognizer lazily. An AST-less file without a recognition bundle has no
recognition capability; the runtime returns a capability error recommending recompilation from source, and does
not fall back to a full parse. A file of any other format version is rejected by the version gate. A low-level
file with no saved start still requires explicit start resolution; the public LoadParser contract continues
rejecting a missing default start.

CLI -check uses the same API and backend/unit resolution, prints `ok` only when error is nil, and returns recovered
diagnostics as an error as it does today. Validate the incompatible -check/-stream combination rather than silently
ignoring -check. Document and ParseStream continue rejecting recognition; generated recognition stays independent
of this file-format feature. Generation/formatting from an AST-less module still requires its absent grammar AST.

### Versioned Layout

After the common prefix (header, ISA, outer string table, start/package and main module), a format version 4
container stores a recognition-present boolean and, when true, a length-delimited recognition bundle; then a
schema-present boolean and, when true, a length-delimited schema section; then the AST-present flag and AST
payload; then the CRC over the complete container. The recognition bundle has its own ISA version, local string
table, module, and ordered name/index entry map. Its indices never refer to the main module's tables. Deterministic
ordering and local string interning make repeated serialization and load/save byte-identical for the same
writer-emitted form. A file with the AST has no recognition bundle, and the loader rejects one that has both.

Store the full semantic recognition program, including predicate-required expression code/type tables. There is
no need for new match opcodes just to execute the second program. Every module declares instruction-set version 26;
validation and runtime ISA support apply independently to the main module and to the bundle. A NoValue flag
in the recognizer denotes execution semantics, while the explicit entry map denotes public start eligibility.

The loader accepts only format version 4 and refuses any other version. It requires bundle absence to be explicit
and never treats it as "use normal twins"; the writer emits a bundle only when a recognizer is present.
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
token/cancellation/budget feature must version its own instructions if needed and be compiled into both
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
| AST-less file with a recognition bundle | Explicit unavailable error | Supported | Supported | No new loading/generation API |
| AST-less file without a recognition bundle | Explicit unavailable error | Explicit capability error | Explicit capability error | Not applicable |

All supported engine cells require CodePoints and Bytes, including invalid UTF-8. Generated TS's string/byte,
integer and Unicode contracts are unchanged; a portable compiled VM is outside this feature. Public node/scalar
typing restrictions are unchanged, including top-level scalar rejection.

The writer stores AST-bearing saves without a bundle and AST-less saves with the complete recognition bundle when
available. The user guide and bytecode format tables describe the same writer behavior.

## Alternatives Considered

- Keep ASTs and rebuild recognition for AST-less distribution: rejected. It needs the AST that an AST-less file
  omits, and it compiles the recognizer on first recognition.
- Return nil after running the normal module: easy, but evaluates unrelated actions and retains tree allocation;
  it violates the recognition contract.
- Derive a complete recognizer from normal bytecode on load: might reduce saved bytes, but requires a validated
  dependency/control-flow transformation for captures, predicates, actions, Operators Rule and LR. Defer until a separate
  equivalence proof and cost comparison; available twins alone are insufficient.
- Store only a recognition module: smaller for recognition-only distribution but removes ordinary Parse capability.
  A separately explicit artifact mode may be added later; do not disguise it as WithoutAST.
- Store the recognition bundle in AST-bearing files: rejected. An AST-bearing file already carries the grammar that
  recognition needs, so a second copy only adds bytes and slows the first save and the load in synthetic measurements.
- Lazily serialize only requested start rules: smaller for one caller, but breaks WithStart's existing contract.
  Choose the complete original-rule entry map.

## Validation and Performance Plan

Differential tests run every corpus start rule through source recognition and AST-bearing/AST-less
save/load recognition, both units and both VM models. Cover EOF/trailing text, invalid UTF-8, variables, nested
predicate captures and transitive action dependencies, skipped action failures versus required action errors,
LR/Operators Rule, labels/cuts, recovery rollback, silent memo reuse, streaming annotations and wide zero/large bounds.
Assert nil Node on every outcome and exact diagnostics, not just acceptance. Compare ordinary Parse before/after,
concurrent WithStart views, depth/cancellation abort/retry and pool cleanup. Test the format version gate, golden determinism of the version 4 layout, missing/duplicate entry maps, checksum-valid malformed bundles, unknown ISA/opcodes and independent section-length/index errors. CLI tests cover saved and overridden
start rules, -check output/status, both backend flags, units and -check/-stream rejection.

No broad performance result is claimed. Measure save/load, first recognition preparation and warm recognition separately
from source compilation and full parsing. Compare AST-bearing recognition with AST-less bundle recognition, and ordinary parsing as a regression control;
do not time a capability error as equivalent useful work. Include action-free and predicate-heavy corpora, LR/Operators Rule, recovery and both units.
Record container/AST/module bytes, executable size where affected, time/allocations, retained prepared-module
memory and transient/live memory during loading. Pair serialized runs with pinned revisions/toolchains/inputs.

### Measurements

The size and load-time measurements of the recognition bundle are kept with the user guide in
[compiled-grammars.md](../../guide/compiled-grammars.md). They describe the selected AST-less bundle on the grammars
of the repository and do not establish costs for application grammars.

Reproduce the AST-less serialization matrix with:

```sh
go test -run '^$' -bench '^BenchmarkCompiledRecognitionSerialization$' -benchmem -benchtime=100ms -count=5 ./internal/engine
```

Run the AST-bearing matrix with:

```sh
go test -run '^$' -bench '^BenchmarkCompiledRecognition(ASTBearing|Serialization)$' -benchmem -benchtime=100ms -count=5 ./internal/engine
```


