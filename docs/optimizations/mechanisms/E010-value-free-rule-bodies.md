# E010: Value-Free Rule Bodies

A rule can consume input without retaining the intermediate tree used to match
its body. This removes Node and child-list construction when an action needs
named captures but no positional body values.

## Mechanism and Invariants

`leanBody` selects value-free lowering for an action that does not read
positional body values (`$n`).
Streaming bodies keep their general route. Captures still build the values their
actions need.

```pego
type B = { B node[string] }
rule ws = [ ]+
rule b = [a-z]
rule a = ws b:b ws => B{B: $b}
```

`a` needs `$b`, but no positional body value. Both `ws` calls still match and
consume whitespace; their unused Node values are omitted. Capture `b` still
builds the value stored in `B.B`. If `ws` has its own action, ordinary parsing
still evaluates it: an unused call result does not authorize skipping the
callee's action. CST twins, Operators Rule line elision and recognition are
separately analyzed mechanisms.

## Scope and State

Applied by default to eligible bodies in closure, both VMs, generated Go Node,
typed Go and TypeScript. It does not mean that every value inside a lean body is
discarded.

## Reference Control

No body-elision-only same-source control is maintained across all backends.
Comparing recognition with a valued parse changes the requested operation and
cannot isolate this mechanism.

## Evidence and Implementation

Record 001 reports historical JSON from 442 to 144 ms and 183 to 76 MB for the
combined body/twin/transient-rule change. That result cannot be attributed
wholly to body elision; current isolated all-backend effects are unmeasured.

Evidence records:
[001](../001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md).

Source owners: [`leanBody`](../../../internal/compile/compile.go),
[`Walker.compileRule`](../../../internal/compile/compile_walk.go), [`body value
lowering`](../../../internal/engine/gen.go), [`direct body build
mode`](../../../internal/engine/gen_flowrule.go), [`TypeScript body build
mode`](../../../internal/gen/tsgen/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E020](E020-value-free-rule-twins.md), [E030](E030-operators-line-value-elision.md), [E040](E040-recognition-lowering.md), [R120](R120-transient-rule-memo-policy.md).

[Optimization Catalog](../README.md)
