# A040: Action Operand Scratch

Actions and predicates need evaluation contexts, argument windows and
constructor field lists. Reusing invocation-local scratch avoids allocating
those containers for every expression.

## Mechanism and Invariants

The parser keeps an evaluation context, expression stack, one-element action
buffer and operator locals. Evaluators push operands above the current stack
height and restore it after nested evaluation. Built-ins and constructors cannot
retain these temporary windows; published values receive separate ownership.
Lambda environments that must survive a nested call keep their own context.
Cleanup on failed predicates must drop any gathered children and construction
tracking.

## Scope and State

Default reusable action plumbing in closure, both VMs and generated Go
Node/typed Go. TypeScript reuses a per-parser action context and evaluation
storage where its runtime permits; it does not pool a parser across separate
calls. Direct typed code can eliminate some context use entirely.

## Reference Control

No common same-source scratch-off control isolates all action buffers.
Whole-input parser pooling and concat fusion are separate mechanisms with
different lifetimes.

## Evidence and Implementation

Records 010/014/015/027/028 measure successive changes. For example, VM JSON
allocation counts fall from 292k to 147k in 014, and closure Operators Rule
action plumbing from about 40.6k to 0.6k in 027. These are historical
backend/workload-specific comparisons, not one combined or current effect
estimate.

Evidence records: [014](../014-one-operand-stack-for-vm-expression-code.md),
[015](../015-allocation-free-action-plumbing-in-all-backends.md),
[027](../027-struct-and-call-operands-on-the-expression-stack-in-the-closure-backend.md),
[028](../028-field-lists-and-list-built-ins-without-allocation-in-the-generated-parsers.md).

Source owners: [`evalCtx and termEvaluator`](../../../internal/engine/eval.go),
[`vmProgram.eval`](../../../internal/engine/vm.go), [`parser.estack and operator
locals`](../../../internal/engine/runtime.go), [`generated
evalCtx`](../../../internal/engine/genrt/runtime.go), [`typed evaluation
scratch`](../../../internal/engine/genrt/typed.go), [`Parser action
context`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A190](A190-evaluation-context-reuse.md), [A130](A130-closure-lambda-arenas.md), [A150](A150-vm-position-and-lambda-chunks.md).

[Optimization Catalog](../README.md)
