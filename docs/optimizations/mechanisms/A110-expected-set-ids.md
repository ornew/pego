# A110: Expected-Set IDs

Repeated diagnostic strings and copied expectation lists make memo/error
bookkeeping expensive. Compile-time IDs and windows of one stack reduce repeated
storage.

## Mechanism and Invariants

Description tables resolve integer IDs into messages. Each isolated region owns
a window of the expectation stack; sets that must survive in memo or recovery
state are copied into an arena, with reuse of equal consecutive kept sets where
supported. Order, farthest position and message flags remain exact; IDs are
internal representation, not user-visible diagnostics.

## Scope and State

Default expectation-ID representation across closure, both VMs, generated Go
Node/typed Go and TypeScript. Go arena allocation and JavaScript array ownership
differ.

## Reference Control

No universal same-source string-set comparator exists. The absence filter
changes duplicate search, not stored diagnostic identity.

## Evidence and Implementation

Historical record 004 reports Minilang allocation volume 59 to 42 MB and timing
about 128 to 103 ms. Record 011 ports the representation among generated
changes. Current per-backend isolated effects are unmeasured.

Evidence records:
[004](../004-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3.md).

Source owners: [`record, isolate and
keep`](../../../internal/engine/runtime.go),
[`staticProgram.descTable`](../../../internal/engine/compile.go), [`generated
expectation IDs`](../../../runtime/runtime.go), [`TypeScript
description IDs`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M100](M100-absent-expectation-filter.md).

[Optimization Catalog](../README.md)
