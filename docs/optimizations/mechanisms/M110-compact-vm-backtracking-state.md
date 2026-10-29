# M110: Compact VM Backtracking State

VM choice and repetition entries are pushed frequently. Keeping rare
label/recovery data off the common entry reduces copied stack state.

## Mechanism and Invariants

The VM moves label, recovery and skip-only fields to a separate label stack,
indexed by the main entry. Saved heights use compact integers. Entry rollback
must restore both stacks together so expectations, recovered errors and skip
behavior stay unchanged.

## Scope and State

Default recursive and iterative VM entry representation. Closure and generated
Go/TypeScript have separate state layouts.

## Reference Control

No maintained same-source large-entry layout comparator exists.

## Evidence and Implementation

Record 033 reports historical entry size from 112 to 56 bytes and sampled VM
improvements of 1–5% for full parsing and 2–6% recognition. Later
recovery/filter/work-control state changes mean those sizes are historical, not
a current layout assertion.

Evidence records: [033](../033-smaller-vm-entries.md).

Source owners: [`entry and label stack`](../../../internal/engine/vm/vm.go),
[`iterative VM entry execution`](../../../internal/engine/vm/ivm.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A030](A030-parser-frame-reuse.md), [M100](M100-absent-expectation-filter.md).

[Optimization Catalog](../README.md)
