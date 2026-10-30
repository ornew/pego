# E030: Operators Rule Line Value Elision

An Operators Rule action often needs captured operands or `$op` text, not the
line's intermediate CST. Value-free line lowering avoids that unused
intermediate result.

## Mechanism and Invariants

`leanLine` distinguishes operator actions, whose matched operator text is
supplied as `$op`, from operand actions that may read positional body values.
Named captures still build required values. A line's frame, match position and
candidate-selection behavior remain owned by the Operators Rule runtime.
Recognition additionally omits line actions where their results are unused.

## Scope and State

Default eligible Operators Rule lines in closure, both VMs, generated Go Node,
typed Go and TypeScript. This analysis is distinct from ordinary body elision
and from direct line-body inlining.

## Reference Control

No line-elision-only same-source control is maintained. Typed Go output has no
generation option for the `leanLine` value decision; it is compared with the
engine oracle in the `internal/engine` tests.

## Evidence and Implementation

Record 009 combines line elision with trivia discards in example grammars,
reporting JSON 50.5 to 42.8 MB and Minilang 32.8 to 23.0 MB. Those reductions
are a combined historical result, not an isolated line-only estimate.

Evidence records:
[009](../009-value-free-operators-rule-lines-discarding-trivia-in-example-grammars-936857c.md).

Source owners: [`LeanLine` and the Operators Rule
walk](../../../internal/compile/compile_walk.go),
[`bcompiler.line`](../../../internal/compile/bcompile.go), [`Operators Rule line
emission`](../../../internal/gen/gogen/generator.go), [`TypeScript line
emission`](../../../internal/gen/tsgen/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E010](E010-value-free-rule-bodies.md), [G160](G160-typed-operators-line-bodies.md), [A170](A170-operators-attempt-scratch.md).

[Optimization Catalog](../README.md)
