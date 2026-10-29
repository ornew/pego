# R100: Document Repetition Resumption

A long repetition can spend reparse time looking up every unchanged element.
Saved run records let it reuse a prefix, rematch around one edit and splice in
an unchanged suffix.

## Mechanism and Invariants

Repetitions of at least 16 elements record positions, examined ranges,
expectations and values. After an eligible single edit, resumption restarts near
the affected element and rejoins when new and old boundaries align. Reused Nodes
move under the Document contract; capture/variable and error ownership retain
conservative eligibility. Multiple unparsed edits, aborts and reset/rollover can
invalidate the saved run. Bytecode run sites are discovered from the module.

## Scope and State

Default eligible long repetitions in closure, recursive VM and iterative VM
Documents. Batch parsing, streams and generated Go/TypeScript are outside this
resumption mechanism.

## Reference Control

No maintained universal resume-disable same-source control exists. A fresh parse
is a different operation and cannot isolate all run bookkeeping costs.

## Evidence and Implementation

Historical record 054 measures a 100,000-line closure insert/delete reparse from
11.0 to 5.1 ms and long CSV from 15.1 to 8.4 ms. Saved runs cost about 80 bytes
per element; short-repetition Minilang Documents cost about 2% more in that
sample. Record 059 adds VM resumption. Benefit depends on length and rejoin
opportunities, not every Document operation.

Evidence records: [054](../054-resuming-long-repetitions-in-a-document.md),
[059](../059-resuming-long-repetitions-in-the-vms.md).

Source owners: [`run records and
resumeRepeat`](../../../internal/engine/core/resume.go), [`VM repetition run
sites`](../../../internal/engine/vm.go), [`saved runs and
eligibility`](../../../internal/engine/document.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R110](R110-document-run-record-retention.md), [A100](A100-document-vm-value-stack.md).

[Optimization Catalog](../README.md)
