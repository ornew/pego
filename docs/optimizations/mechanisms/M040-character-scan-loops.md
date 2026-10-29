# M040: Character Scan Loops

A value-free repetition of one character need not build an element value or
create a full backtracking mark for every iteration. A scan loop performs the
repeated membership or any-character test directly.

## Mechanism and Invariants

The compiler recognizes eligible single-character class or dot repetitions only
when their result is unused. It preserves minimum/maximum repetition bounds,
input positions, terminal failure expectations and examined input. Captures,
actions or compound expressions that require per-element state remain on the
general repetition path. The bytecode compiler emits `SCAN`; generated emitters
produce an equivalent loop. Stream scans must retain reader loading behavior.

## Scope and State

Applied by default to eligible value-free repetitions in closure, both VMs,
generated Go Node/recognition, typed Go and TypeScript. Value-building
repetitions are outside this specialization.

## Reference Control

No shared same-source scan-disable comparator is maintained. A value-building
parse changes the operation and is not an isolated reference.

## Evidence and Implementation

Record 008 combines scan lowering with unmemoized-call changes and reports
recognition reductions such as CSV 24 to 18 ms. Record 011 carries scan loops
into generated Go. Those historical combined measurements do not assign the
whole improvement to scanning or establish current performance in all six
targets.

Evidence records:
[008](../008-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d.md).

Source owners: [`Walker.scanRepeat`](../../../internal/compile/compile_walk.go),
[`closureBuilder.ScanRepeat`](../../../internal/engine/closures.go),
[`SCAN lowering`](../../../internal/compile/bcompile.go),
[`Scans pass`](../../../internal/ir/core/passes.go),
[`Flow scan lowering`](../../../internal/ir/flow/lower.go), which lowers the
scans of generated Go and TypeScript.

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E010](E010-value-free-rule-bodies.md), [M020](M020-character-class-tests.md).

[Optimization Catalog](../README.md)
