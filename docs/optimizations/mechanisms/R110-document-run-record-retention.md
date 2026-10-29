# R110: Document Run Record Retention

An unchanged Document parse that hits the root memo should not discard
long-repetition records needed by the next edit. Retaining the same-generation
map avoids losing resumption work.

## Mechanism and Invariants

Only an edit advances run generation. An unchanged parse keeps its map, and a
rebuilt repetition can replace a record within it. A parse after an edit creates
the next map; two unparsed edits, aborted execution and edit-log rollover retain
their invalidation rules. No records or replacement map are copied on the
unchanged path.

## Scope and State

Default Document bookkeeping for closure and both VMs. Generated parsers and
TypeScript have no Document API.

## Reference Control

No maintained same-source clear-map comparator exists. The focused evidence
compares the historical parent and candidate.

## Evidence and Implementation

Record 070 uses a 100,000-line fixture with a redundant unchanged parse outside
timing, then times Edit plus final Parse. Retention restores 99,999 resumed
elements; closure CodePoints drops from 6.90 to 0.778 ms and 48.83 to 1.15 MB.
Without that redundant parse, timing ranges overlap and allocations stay fixed.
This historical result concerns the unchanged-parse-before-edit sequence, not
unchanged-parse cost or batch speed.

Evidence records:
[070](../070-keep-repetition-records-across-unchanged-document-parses.md).

Source owners: [`Document.runGen and run-map
publication`](../../../internal/engine/document.go), [`run
replacement`](../../../internal/engine/core/resume.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R100](R100-document-repetition-resumption.md).

[Optimization Catalog](../README.md)
