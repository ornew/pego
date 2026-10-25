# G085: Skip Positional Items for Action-Free Rules

A rule action reads its sequence's visible items by position. Typing those
items is only needed when an action can read them.

## Mechanism and Invariants

The checker types the whole rule body once, which reports every diagnostic and
records every checked field and result. It types the visible top-level items
again, each in a discarded capture scope, only for a rule with an action.
Action-free rules skip that second pass; their value type comes from the body.

## Scope and State

Default shared type checking before compilation/generation. No parser backend
performs this analysis in its matching loop.

## Reference Control

No maintained same-source control exists.

## Evidence and Implementation

Record 101 measures complete type checking: 32,768 record captures fall from
18.57 to 9.74 ms and 131,243 to 103 allocations, and recursive captures fall
by about 15% in time with 96% fewer allocated bytes. Unchanged example
grammars check 2–8% faster.

Evidence records:
[101](../101-skip-positional-items-for-action-free-rules.md).

Source owners: [`rule type checking`](../../../internal/engine/check.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G080](G080-equal-capture-type-reuse.md).

[Optimization Catalog](../README.md)
