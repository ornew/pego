# G050: One Generated Form per Option Set

Resource checks can cost generated parsing even when no budget control is active. A generated Go
parser therefore has one form of its functions, chosen at generation time by the options.

## Mechanism and Invariants

The generator prints each execution function (rule bodies and other functions taking the parser
as their first parameter) once, from Flow IR, in the form the options select:

- Without `-budget-controls` and without `-document` the parser holds the ordinary form only: the
  16 runtime helpers that have a hand-written `Ordinary` form (`NewNode`, `NewTerminal`,
  `NewBranch`, `NewMatch`, `SetCapture`, `Expect`, `Peek`, `MatchLiteral`, `MatchLiteralFold`,
  `MatchAny`, `AtEnd`, `AtLineStart`, `AtEndBatch`, `AtLineStartBatch`, `AtLineEnd`, `SpendRead`),
  no budget charges, no control reads in conditions and class tests, and a compact
  peek. A parse that passes a control option fails.
- With `-budget-controls` the parser holds the controlled form only: budget charges,
  the general runtime helpers and class tests that consult the control. A parse that sets
  no limit and no cancelable context has no control and runs the same code: the inline charges
  (`Spend`, `Control.Take`) accept a nil control and do nothing, the class tests of large classes
  take the compact path, and the helpers test the control themselves. The terms of an
  action carry no charge, and neither do the generated constructors of typed values: only
  repetition is charged, by the runtime's loops over the elements of a list (`map`, `filter`,
  `foldl`, `foldr`), one step per element.
- With `-document` the in-place reads of literals and characters carry the guard
  `p.Document == nil`, so that a Document parse reaches the primitives that record what a rule
  examined. Without `-budget-controls` that form has no charges and no control reads.

There is no second body, no run-time choice between forms, and no splice. Metered paths keep their
checks, cancellation and budgets. Budget accounting is a resource contract, not an optimization in
itself. A user who needs both forms generates two parsers.

## Scope and State

Applied by default in generated Go Node/recognition and typed Go output. Generated TypeScript
chooses its form with the same option but prints its own charges (see [G210](G210-typescript-from-flow-ir.md)).
Closure and VM budget-control implementations do not use this Go mechanism.

## Evidence and Implementation

A parser generated with `-budget-controls` has one controlled form of its functions. The `parser.go` of
the same parser generated without the option is 0.50 to 0.85 of its size (every grammar of `parsers/`,
Node-only, with `-types`, with `-types -recognize` or with `-types -typed-only`; larger grammars save
more, and duckdb with `-typed-only` is the smallest at 0.50). Against a parser generated without `-budget-controls`, a parse of the controlled form that passes no
option takes 1.08 to 1.13 times as long (`Parse`) and 1.08 to 1.17 times (`ParseAST`) on the JSON, XML,
YAML and CSV inputs, and one that passes a limit takes 1.25 to 1.33 times (`Parse`) and 1.26 to 1.45
times (`ParseAST`). The aim of 1.15 times for a parse with a limit is not met; with every charge
made free JSON still takes 1.15 times, so the rest is the shape of the controlled bodies. The
table is in [103](../103-inline-the-common-budget-spend.md). Records
[096](../096-specialize-generated-go-method-helpers.md) and
[121](../121-inactive-budget-control-checks.md) hold earlier measurements of the ordinary body of a
function and of the inlined runtime helpers.

Source owners: [`Flow printer`](../../../internal/gen/gogen/flow.go), [`helper
names`](../../../internal/gen/gogen/helper_names.go), [`metered runtime
helpers`](../../../runtime/budget.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G010](G010-generated-helper-sharing.md), [G060](G060-ordinary-typed-direct-rules.md).

[Optimization Catalog](../README.md)
