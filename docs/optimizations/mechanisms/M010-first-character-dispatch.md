# M010: First-Character Dispatch

An ordered choice may call several rules that cannot begin with the next input
character. A conservative first-terminal guard rejects those alternatives before
constructing their call and memo state.

## Mechanism and Invariants

`firstTerminal` recognizes a required starting literal or class through safe
expression wrappers and calls. Unknown, left-recursive and Operators Rule starts
retain ordinary matching. A rejected alternative records the expectation its
first terminal would have produced. The next character is examined, so a
Document edit can invalidate the result. Depth-limit behavior also constrains
skipped calls. The VMs encode guards in `GUARD` instructions; generated parsers
emit equivalent tests. Alternatives that pass still execute in their original
order.

A case-insensitive literal (`"x"i`, or a literal under `#ignorecase`) is guarded by
both cases of its first ASCII letter in the VMs and in the generated Go and TypeScript
parsers, and in the typed Go path through the Core first set (a lower-case letter with a
fold flag). Record 119 has the measurement.

## Scope and State

Applied by default to eligible choices in closure, both VMs, generated Go
Node/recognition, typed Go and TypeScript. It is not a reordering of PEG
alternatives or a general lookahead prediction.

## Reference Control

No common same-source guard-disable control is maintained across all backends.
Old bytecode without `GUARD` is compatibility input, not a current-source
reference compiler.

## Evidence and Implementation

Historical records 049–051 report JSON parsing improvements of 13% in generated
Node, 7% in closure, and 13%/15% in the VMs on their recorded runs. Record 050
also measures incremental reparse benefits. The revisions and operations differ;
the percentages cannot be combined or generalized to unknown starts or current
TypeScript workloads.

Evidence records:
[049](../049-first-character-dispatch-in-generated-choices.md),
[050](../050-first-character-dispatch-in-the-closure-backend.md),
[051](../051-first-character-dispatch-in-the-vms-instruction-set-3.md).

Source owners: [`firstTerminal`](../../../internal/engine/compile.go),
[`walker.exprForm and compiler.first`](../../../internal/engine/compile_walk.go),
[`closureBuilder.Choice and guard`](../../../internal/engine/closures.go), [`GUARD
emission`](../../../internal/engine/bcompile.go), [`generated choice
guards`](../../../internal/engine/gen.go), [`TypeScript choice
guards`](../../../internal/engine/gen_ts.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M020](M020-character-class-tests.md), [M080](M080-direct-code-point-access.md).

[Optimization Catalog](../README.md)
