# R170: Left-Recursion Member Memoization

The rules of a left-recursion cycle other than its leader are memoized, so a member reached from several
call sites at one position is evaluated once instead of once per call.

## Mechanism and Invariants

A member's result is memoized under three rules. A call that read the seed of a growth already in progress when it
started stores nothing: the seed changes with each growth step. While a leader grows at a position, a member result
there is evaluated again instead of reused, since it may have read an earlier result of the leader. A call that read
an unstable result, the result of a leader that read an enclosing seed and can grow again at its position because it
is reused in one kind of context only (inside or outside lookaheads), stores nothing either. The parser counts the
growing left recursions, stamps each growing result with that depth, and tracks per memoized call the least depth it
read (0 for an unstable result); the innermost growth's position decides the second rule. Every other result
depends only on the rule, the position and the variables. Documents keep their provisional results, which an edit
does not reuse. Leaders keep their own memoization, and the transient policy applies to members.

## Scope and State

All six backends and Documents. A transient member, one called from a single place, is not memoized in
ordinary parses.

## Reference Control

None beyond `DisableMemo`, which disables memoization of members and other rules together.

## Evidence and Implementation

Evidence record: [130](../130-memoizing-members-of-left-recursion-cycles.md).

Source owners: [`CompileProgram`](../../../internal/compile/compile.go) (the memo and member flags),
[`CallBegin`, `CallEnd` and `GrowBegin`](../../../internal/engine/core/runtime.go), [`Parser.readResult`
and `memberStale`](../../../runtime/runtime.go), [the calls and growths of the generated Go
runtime](../../../runtime/call.go), [the call protocol in Exec IR](../../../internal/ir/exec/core/core.go)
with [its growth operations](../../../internal/ir/exec/ops/transitional.go),
[`Parser.readResult` of the TypeScript runtime](../../../internal/gen/tsgen/tsrt/runtime.ts).

Related mechanisms: [R020](R020-deferred-memoization.md), [R120](R120-transient-rule-memo-policy.md).

[Optimization Catalog](../README.md)
