# A190: Evaluation Context Reuse

Every action and predicate needs an evaluation context, but its scratch identity
is not part of the grammar result. Reusing one per parser avoids allocating a
context at every evaluation.

## Mechanism and Invariants

`useCtx` refreshes rule range, captured values and environment for the current
evaluation. Nested lambda calls use their own preserved context when required.
No context reference may outlive its allowed evaluation scope; values returned
by constructors retain separate ownership.

## Scope and State

Default per-parser context reuse in closure, both VMs, generated Go Node/typed
Go and TypeScript. TypeScript reuses within one invocation, not across parser
calls.

## Reference Control

No maintained same-source fresh-context-only control exists.

## Evidence and Implementation

Record 010 combines context reuse with token substring sharing and reports
reduced allocation counts. That combined result cannot assign a separate context
speedup, and direct typed code may avoid the context entirely.

Evidence records:
[010](../010-zero-copy-token-text-reusable-evaluation-context.md).

Source owners: [`parser.useCtx`](../../../internal/engine/core/alloc.go), [`generated
useCtx`](../../../runtime/runtime.go), [`typed
context`](../../../runtime/typed.go),
[`Parser.useCtx`](../../../internal/gen/tsgen/tsrt/runtime.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A040](A040-action-operand-scratch.md), [A130](A130-closure-lambda-arenas.md).

[Optimization Catalog](../README.md)
