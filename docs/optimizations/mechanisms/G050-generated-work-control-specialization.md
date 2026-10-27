# G050: Generated Work-Control Specialization

Resource checks can cost ordinary generated parsing even when no work control is
active. Specializing ordinary functions avoids some nil-control checks while
preserving metered invocation behavior.

## Mechanism and Invariants

The generator prints each execution function (rule bodies and other functions taking the parser
as their first parameter) once, and writes the places where the controlled and the ordinary form
differ as text fragments of that one traversal. A fragment holds the controlled alternative, the
ordinary one and, for a Document, the form that remains without work controls. Both forms therefore
share names, scope variables and the decision which variables are unread. The ordinary alternatives
are:

- the 16 runtime helpers that have a hand-written `Ordinary` form (`NewNode`, `NewTerminal`,
  `NewBranch`, `NewMatch`, `SetCapture`, `Expect`, `Peek`, `MatchLiteral`, `MatchLiteralFold`,
  `MatchAny`, `AtEnd`, `AtLineStart`, `AtEndBatch`, `AtLineStartBatch`, `AtLineEnd`, `InputWork`);
- no step or input-work charges, no control reads in conditions and class tests, and a compact
  peek;
- labels with the suffix `_ordinary`, since the branch sits inside the controlled function.

Before formatting, the text is cut into the controlled form, which is the generated file, and the
ordinary form. A splice moves the bodies of the ordinary form, formatted by themselves, into the
functions of the same name as a branch that runs under `p.Control == nil` (and
`p.Document == nil` in a parser with Documents). In a Document parser without work controls the
branch runs under `p.Document == nil` and the rest form replaces the body that follows it. Metered
paths retain their checks, cancellation and budgets. Broader helper cloning and an additional
method split were measured and rejected. Work accounting is a resource contract, not an
optimization in itself.

## Scope and State

Applied by default in generated Go Node/recognition and typed Go output, and only to
parsers with work controls (`-work-controls`) or Documents (`-document`): their
function bodies gain a branch for ordinary parses. A parser generated with neither has
only the ordinary form, which the Flow printer prints directly (helper names with the
`Ordinary` suffix, no control reads, no step charges), and a parse that passes a
control option fails.
Closure, VMs and TypeScript work-control implementations do not use this Go
mechanism.

## Reference Control

Private generation control `disableOrdinaryWorkSpecialization` produces the
same-source unspecialized route.

## Evidence and Implementation

Record 096 reports same-source ordinary Node/recognition median ratios of
0.760–0.969 and typed ratios 0.815–0.936 against specialization disabled.
Active-control samples are noisy. The feature's total overhead versus
pre-control main remains measurable; specialization does not establish parity
with that historical baseline.

Record 121 moves the controlled bodies of the shared runtime helpers out of line so that the helpers inline in ordinary parses; it is 0.2% to 3.4% faster in run time than before.

Evidence records: [096](../096-specialize-generated-go-method-helpers.md), [121](../121-inactive-work-control-checks.md).

Source owners: [`fragments and form
selection`](../../../internal/engine/gen_form.go), [`ordinary body
splice`](../../../internal/engine/gen_ordinary_splice.go), [`execution function
identity and helper names`](../../../internal/engine/gen_ordinary_names.go), [`metered
runtime helpers`](../../../runtime/work.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G010](G010-generated-helper-sharing.md), [G060](G060-ordinary-typed-direct-rules.md).

[Optimization Catalog](../README.md)
