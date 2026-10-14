# G070: Literal Text Predicates

A pure OR of comparisons against one capture's text can compute that text once
and dispatch by literal, rather than repeat general expression evaluation for
every branch.

## Mechanism and Invariants

The complete predicate must contain at least two comparisons of the same direct
capture with string literals; either operand order is accepted. The emitter
collapses duplicate literals and emits a switch. Mixed expressions, aliases,
assignments and different captures remain general, preserving short-circuit
order and side effects. Helper/literal-table sharing is independent of this
shape specialization. Generated Go and TypeScript use the switch on ordinary
nil-control calls; active work-controlled calls keep the general charged
evaluator.

## Scope and State

Default eligible predicates in generated Go Node, typed Go and TypeScript.
Closure and both VMs retain their general action evaluator for this disjunction
shape.

## Reference Control

Private generation option `disableLiteralTextDispatch` emits the general
predicate from the same generator. It adds no runtime selector or public option.

## Evidence and Implementation

Record 091 compares the same generator with dispatch disabled. Large Go
workloads have paired geometric-mean time ratios about 0.952/0.955 in
CodePoints/Bytes, while small-workload means are 1.001/0.990. TypeScript has
separate scoped gains and a slower first-match case. No-op source is
byte-identical. These results concern qualifying pure predicates, not arbitrary
OR expressions or all parsers.

Evidence records: [091](../091-specialize-pure-literal-text-disjunctions.md).

Source owners: [`literal text dispatch
recognition/emission`](../../../internal/engine/gen.go), [`TypeScript literal
switch emission`](../../../internal/engine/gen_ts.go), [`route and unsafe-shape
checks`](../../../internal/engine/gen_literal_text_dispatch_test.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M120](M120-unboxed-text-comparisons.md), [G130](G130-generated-literal-sharing.md).

[Optimization Catalog](../README.md)
