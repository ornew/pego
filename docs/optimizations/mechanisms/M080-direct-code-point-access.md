# M080: Direct Code-Point Access

Eligible generated Go bodies can read an already-decoded character directly
instead of calling the general `peek` helper for every test.

## Mechanism and Invariants

Direct classes, dot, scan loops and choice guards use `p.in[p.pos]` within the
decoded input bounds. Otherwise they call `peek`, preserving EOF and Bytes UTF-8
decoding. Bytes parsing explicitly keeps the rune input empty even with pooled
state, preventing accidental use of stale CodePoints data.

## Scope and State

Default eligible generated Go Node and typed direct bodies. The fast read is
CodePoints-only; Bytes retains the fallback. Closure, VMs and TypeScript do not
use this Go direct-emitter access route.

## Reference Control

No independent same-source direct-read control exists. Body-lowering controls
remove a broader route.

## Evidence and Implementation

Record 061 historically measures typed JSON 3.75 to 3.30 ms and XML 4.41 to 3.99
ms. Node support arrives through later direct-body lowering, so those typed
figures do not quantify Node or Bytes benefits.

Evidence records: [061](../061-reading-code-points-directly-in-direct-rules.md).

Source owners: [`Flow peek lowering`](../../../internal/ir/flow/lower/lower.go), [`input
preparation`](../../../runtime/runtime.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G100](G100-generated-node-body-inlining.md), [G060](G060-ordinary-typed-direct-rules.md), [M130](M130-short-literal-comparisons.md).

[Optimization Catalog](../README.md)
