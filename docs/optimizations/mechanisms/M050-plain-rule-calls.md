# M050: Plain Rule Calls

Unmemoized rules without captures do not need the full memo lookup,
capture-frame switch and generic rule dispatch on every call.

## Mechanism and Invariants

`rule.plain` excludes left-recursion leaders and requires no capture slots and
no ordinary memoization. `invokePlain` leaves the caller's frame current while
preserving depth accounting, cuts, environment rollback, recovered errors and
result finalization. Call sites known to be plain bypass the memo decision. The
iterative VM combines the plain call with its body frame; generated Go and
TypeScript emit rule-specific wrappers. Document parsing still memoizes
transient rules and must take the appropriate Document route.

## Scope and State

Default eligible plain-call routes exist in closure, recursive VM, iterative VM,
generated Go Node/recognition, typed Go and TypeScript. Typed direct bodies are
a broader generator specialization described separately.

## Reference Control

There is no universal same-source plain-call reference switch. A memo-disable
option changes memo policy, not only wrapper dispatch. Typed body controls also
remove larger groups of code.

## Evidence and Implementation

Records 019/024/034 measure simpler runtime calls; 038/042/043/046 measure
direct call-site or wrapper refinements. For example, 034 reports iterative JSON
recognition from 18.4 to 14.5 ms, and 046 reports 0.3–6% generated improvements.
These are successive historical comparisons, not independently additive current
effects.

Evidence records:
[019](../019-a-plain-call-path-for-unmemoized-rules-without-captures.md),
[021](../021-changes-18-and-19-in-the-generated-parsers.md),
[024](../024-the-plain-call-path-in-the-iterative-vm.md),
[034](../034-one-frame-per-plain-call-in-the-iterative-vm.md),
[038](../038-calling-plain-rules-directly-from-closure-call-sites.md),
[042](../042-generated-calls-of-plain-rules.md),
[043](../043-direct-calls-of-plain-rules-in-the-vms.md),
[046](../046-direct-calls-of-plain-rules-in-generated-parsers.md),
[060](../060-direct-rules-in-the-typed-runtime.md).

Source owners: [`rule.plain
classification`](../../../internal/engine/compile.go),
[`parser.invokePlain`](../../../internal/engine/runtime.go),
[`plainCallFrame`](../../../internal/engine/ivm.go), [`plain call
emission`](../../../internal/engine/gen.go),
[`tsGen.plainCall`](../../../internal/engine/gen_ts.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R120](R120-transient-rule-memo-policy.md), [A030](A030-parser-frame-reuse.md), [A270](A270-shared-empty-capture-frame.md).

[Optimization Catalog](../README.md)
