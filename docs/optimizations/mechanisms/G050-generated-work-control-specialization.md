# G050: Generated Work-Control Specialization

Resource checks can cost ordinary generated parsing even when no work control is
active. Specializing ordinary functions avoids some nil-control checks while
preserving metered invocation behavior.

## Mechanism and Invariants

The generator emits ordinary function bodies (rule bodies are functions taking the
parser as their first parameter) and specializes only selected runtime helpers
(`NewNode`, `NewMatch`, `SetCapture`, `Expect`). Metered paths retain
their checks, cancellation and budgets. The method registry tracks complete
emitted bodies; synchronous emitter callbacks preserve parser/call-local state.
Broader helper cloning and an additional method split were measured and
rejected. Work accounting is a resource contract, not an optimization in itself.

## Scope and State

Applied by default in generated Go Node/recognition and typed Go output, and only to
parsers with work controls (`-work-controls`) or Documents (`-document`): their
function bodies gain a branch for ordinary parses. A parser generated with neither has
only the ordinary form, which the Flow printer prints directly (helper names with the
`Ordinary` suffix, no control reads, no step charges), and a parse that passes a
control option fails.
Closure, VMs and TypeScript work-control implementations do not use this Go
specializer.

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

Source owners: [`ordinary method specialization
selection`](../../../internal/engine/gen.go), [`work-control method
generation`](../../../internal/engine/gen_ordinary_methods.go), [`metered
runtime helpers`](../../../runtime/work.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G010](G010-generated-helper-sharing.md), [G060](G060-ordinary-typed-direct-rules.md).

[Optimization Catalog](../README.md)
