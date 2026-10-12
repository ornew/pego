# 045. Serialized Recognition Programs for Compiled Grammars

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Propose storing a separate recognition module in newly serialized compiled grammars, so the existing
`Parser.Parse(input, pego.RecognizeOnly())` and `pego parse -check` work without the grammar AST. Keep ordinary
parsing and recognition caches separate. Recognition builds only values needed by predicates and their callees;
it does not evaluate unrelated actions merely to discard their trees. No new public recognition entry point is
required. The file-format change and defaults described below are proposed, not implemented contracts.

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

The authoritative writer in [compiled.go](../../internal/engine/compiled.go) stores format version 2, an ISA version,
the main module and optional AST. The current spec uses ISA 3 for ordinary modules and 4 for wide bounds. Loading
validates checksum, tables, operands and recovery regions; it does not establish general termination or recheck
all type/stack behavior. This proposal preserves the trusted-module restriction.

## Design

### API, Start Selection and Backend Behavior

Keep `MarshalBinary`, `Marshal`, `WithoutAST`, `LoadParser`, `WithStart` and RecognizeOnly signatures unchanged.
Propose that the new default serialization includes the recognition module, including with WithoutAST.
Source compilation uses the existing recognition transformation and analysis; do not introduce a weaker bytecode
recognizer. Compile/serialize it once on demand through a thread-safe initialization path, not on each parse.

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

### Proposed Versioned Layout

Allocate compiled-container version 3 if still free when this lands. Keep the main v2 prefix (header, ISA, outer
string table, start/package and main module); insert a recognition-present boolean and, when true, a length-delimited
recognition bundle before the existing AST-present flag and AST payload. The recognition bundle has its own ISA
version, local string table, module, and ordered name/index entry map. Its indices never refer to the main module's
tables. The existing CRC covers the complete container. Deterministic ordering and local string interning must
make repeated serialization and load/save byte-identical for the same capability set.

Store the full semantic recognition program, including predicate-required expression code/type tables. There is
no need for new match opcodes just to execute the second program. Each module declares the ISA actually required,
including wide repetition bounds; validation and runtime ISA support apply independently to both. A NoValue flag
in the recognizer denotes execution semantics, while the explicit entry map denotes public start eligibility.
Specify that distinction without rewriting the meaning of twins in legacy normal modules.

Old loaders reject version 3. New loaders accept versions 1, 2 and 3 with their documented capability differences.
Version 3 may represent an absent bundle, but absence must be explicit and cannot mean "use normal twins".
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
| New AST-less recognition bundle | Explicit unavailable error | Proposed supported | Proposed supported | No new loading/generation API |
| Legacy AST-less file | Explicit unavailable error | Explicit capability error | Explicit capability error | Not applicable |

All supported engine cells require CodePoints and Bytes, including invalid UTF-8. Generated TS's string/byte,
integer and Unicode contracts are unchanged; a portable compiled VM is outside this feature. Public node/scalar
typing restrictions are unchanged, including top-level scalar rejection.

First expose the existing source recognizer's module with a complete start map and compare execution through both
VMs. Then specify/implement the versioned writer/loader and corruption checks. Finally wire AST-less recognition
and CLI behavior, publish migration docs, and refresh bytecode/specification and format golden fixtures together.
Keep proposed format numbers subject to collision checks against the then-current spec, not magic constants in
multiple independent patches.

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

Proposed differential tests run every corpus start rule through source recognition and AST-bearing/AST-less
save/load recognition, both units and both VM models. Cover EOF/trailing text, invalid UTF-8, variables, nested
predicate captures and transitive action dependencies, skipped action failures versus required action errors,
LR/Operators Rule, labels/cuts, recovery rollback, silent memo reuse, streaming annotations and wide zero/large bounds.
Assert nil Node on every outcome and exact diagnostics, not just acceptance. Compare ordinary Parse before/after,
concurrent WithStart views, depth/cancellation abort/retry and pool cleanup. Test versions 1/2 compatibility,
version-3 golden determinism, AST-less legacy resaving, missing/duplicate entry maps, checksum-valid malformed
bundles, unknown ISA/opcodes and independent section-length/index errors. CLI tests cover saved and overridden
start rules, -check output/status, both backend flags, units and -check/-stream rejection.

No performance result is claimed. Measure save/load, first recognition preparation and warm recognition separately
from source compilation and full parsing. Compare exact parent AST-bearing recognition, candidate AST-bearing and
AST-less bundles, and ordinary parsing as a regression control; do not time the parent's expected capability error
as equivalent useful work. Include action-free and predicate-heavy corpora, LR/Operators Rule, recovery and both units.
Record container/AST/module bytes, executable size where affected, time/allocations, retained prepared-module
memory and transient/live memory during loading. Pair serialized runs with pinned revisions/toolchains/inputs;
default bundle size and startup costs require evidence before adopting the proposed new writer default.
