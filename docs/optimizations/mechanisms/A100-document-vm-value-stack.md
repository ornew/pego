# A100: Document VM Value Stack

VM-backed Document reparses can reuse the value stack's cleared capacity instead
of allocating a new array on each parse that evaluates rules.

## Mechanism and Invariants

Finished evaluations clear stack slots; an unchanged whole-root memo hit skips
redundant clearing. Interrupted parses discard the cache. Before and after
parsing, capacity exceeding the input-relative bound `cap(vals)/2 > max(512,
input positions)` is dropped; division avoids integer overflow. Trimming occurs
at parse boundaries, not immediately on Edit. Returned result storage does not
live in this scratch array.

## Scope and State

Applied by default to recursive and iterative VM Document parsing only. Closure
Documents, ordinary batch parsing, generated Go and TypeScript are unchanged.

## Reference Control

Build tag `pego_reference_document_vm_values` restores fresh-stack allocation.
Both same-source builds retain the Document's additional slice header, so the
comparison does not measure that layout or construction footprint.

## Evidence and Implementation

Record 086 compares the same-source fresh-stack reference with repeated
100,000-line Document reparses. VM time medians are about 0.58–0.61 of
reference, with about 88.5% fewer B/op and 28 fewer allocations; closure
controls overlap parity. Nullable, abort and small-after-large cases preserve
behavior. No peak or retained-heap reduction is measured, and the extra Document
slice header is present in both variants.

Evidence records: [086](../086-reuse-document-vm-values.md).

Source owners: [`Document.finishParse and retained VM
values`](../../../internal/engine/document.go), [`VM value cache
policy`](../../../internal/engine/document_vm_values.go), [`ownership and
trimming checks`](../../../internal/engine/document_vm_values_test.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R100](R100-document-repetition-resumption.md), [A240](A240-stream-vm-stack-cleanup.md).

[Optimization Catalog](../README.md)
