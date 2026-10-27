# G050: One Generated Form per Option Set

Resource checks can cost generated parsing even when no work control is active. A generated Go
parser therefore has one form of its functions, chosen at generation time by the options.

## Mechanism and Invariants

The generator prints each execution function (rule bodies and other functions taking the parser
as their first parameter) once, from Flow IR, in the form the options select:

- Without `-work-controls` and without `-document` the parser holds the ordinary form only: the
  16 runtime helpers that have a hand-written `Ordinary` form (`NewNode`, `NewTerminal`,
  `NewBranch`, `NewMatch`, `SetCapture`, `Expect`, `Peek`, `MatchLiteral`, `MatchLiteralFold`,
  `MatchAny`, `AtEnd`, `AtLineStart`, `AtEndBatch`, `AtLineStartBatch`, `AtLineEnd`, `InputWork`),
  no step or input-work charges, no control reads in conditions and class tests, and a compact
  peek. A parse that passes a control option fails.
- With `-work-controls` the parser holds the controlled form only: step and input-work charges,
  the general runtime helpers and class tests that consult the control. A parse that sets no
  limit runs the same code; the few inline tests of `p.Control` (in literal and large-class
  tests) take the unmetered path, and the helpers test the control themselves.
- With `-document` the in-place reads of literals and characters carry the guard
  `p.Document == nil`, so that a Document parse reaches the primitives that record what a rule
  examined. Without `-work-controls` that form has no charges and no control reads.

There is no second body, no run-time choice between forms, and no splice. Metered paths keep their
checks, cancellation and budgets. Work accounting is a resource contract, not an optimization in
itself. A user who needs both forms generates two parsers.

## Scope and State

Applied by default in generated Go Node/recognition and typed Go output. Closure, VMs and
TypeScript work-control implementations do not use this Go mechanism.

## Evidence and Implementation

A parser generated with `-work-controls` has one controlled form of its functions. The `parser.go` of
the same parser generated without the option is 0.50 to 0.85 of its size (every grammar of `parsers/`,
Node-only, with `-types`, with `-types -recognize` or with `-types -typed-only`; larger grammars save
more, and duckdb with `-typed-only` is the smallest at 0.50). A parse that sets no limit runs
the controlled form without a fast path: on the YAML and CSV benchmark inputs it is slower than a
parse of a parser generated without `-work-controls` by 17% (YAML `Parse`), 27% (YAML `Recognize`),
27% (CSV `Parse`) and 69% (CSV `Recognize`). Records
[096](../096-specialize-generated-go-method-helpers.md) and
[121](../121-inactive-work-control-checks.md) hold earlier measurements of the ordinary body of a
function and of the inlined runtime helpers.

Source owners: [`Flow printer`](../../../internal/engine/gen_flow.go), [`helper
names`](../../../internal/engine/gen_helper_names.go), [`metered runtime
helpers`](../../../runtime/work.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G010](G010-generated-helper-sharing.md), [G060](G060-ordinary-typed-direct-rules.md).

[Optimization Catalog](../README.md)
