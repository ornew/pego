# G060: Ordinary Typed Direct Rules

An eligible ordinary typed rule can match and run its action directly in its
generated method, avoiding per-expression dispatch and runtime capture-frame
plumbing.

## Mechanism and Invariants

Direct bodies use Go-local captures and failure labels. Backtracking
saves/restores captures, input, environment and recovered errors; repetition
clears element locals each iteration. Actions read locals directly and
constructors receive rule ranges. Unsupported ordinary layouts stay general. LR,
Operators Rule, cut and recovery extensions use separately selected routes.

## Scope and State

Default eligible generated typed Go routes only. Node generation shares emitter
infrastructure but has different eligibility and ownership. TypeScript and
engine/VM matching do not use these Go direct bodies.

## Reference Control

No universal ordinary-direct-rule disable option is maintained. The
cut/LR/Operators/layout/recovery generation controls select separate extension
groups, not the entire original ordinary route.

## Evidence and Implementation

Historical record 060 reports JSON typed timing 6.44 to 3.86 ms, XML 6.96 to
4.34 ms and lower frame allocation on its measured grammars. That body-lowering
change does not establish the current isolated effect or benefits for
unsupported rules.

Evidence records: [060](../060-direct-rules-in-the-typed-runtime.md).

Source owners: [`generator.flowDirectRule`](../../../internal/engine/gen_flow.go), [`typed framed body
lowering`](../../../internal/engine/gen_direct_node.go), [`GenOptions private
typed controls`](../../../internal/engine/gen.go), [`runtime invocation
ownership`](../../../runtime/typed.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G140](G140-typed-local-cuts.md), [G150](G150-typed-left-recursion-bodies.md), [G160](G160-typed-operators-line-bodies.md), [G200](G200-typed-local-capture-layouts.md).

[Optimization Catalog](../README.md)
