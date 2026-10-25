# R070: Document Memo Edits

An edit should not walk and reposition every memo entry. A gap-buffer position
index moves the nearby slot region, and lazy entry advancement defers
unaffected-entry checks until lookup.

## Mechanism and Invariants

`memoTable.splice` updates the edited positions and gap. Entries record the edit
generation they account for; `advanceEntry` replays later edits in order when
looked up, invalidating examined-overlap results or shifting eligible positions.
Positional results, recovered errors and unfinished seed dependencies retain
conservative invalidation. Unused entries need no immediate result-tree walk.

## Scope and State

Default Document memo management in closure and both VMs. Generated
Go/TypeScript have no editable memo API. Ordinary parse/stream lookups share
core machinery but not Document edits.

## Reference Control

No maintained same-source eager-walk reference control exists. Historical source
comparisons are the available evidence.

## Evidence and Implementation

Records 020/056 trace in-place splice and later lazy advancement. Record 056
reports Minilang edit-plus-reparse 0.73 to 0.17 ms in closure and smaller costs
across VMs. Added entry-generation storage and lookup comparisons are part of
the historical change; current chunked input and other improvements must not be
attributed to that ratio.

Evidence records: [020](../020-splicing-the-memo-table-on-document-edits.md),
[056](../056-applying-a-document-edit-to-memo-entries-when-they-are-looked-up.md).

Source owners: [`memoTable.splice`](../../../internal/engine/memo.go),
[`advanceEntry`](../../../internal/engine/document.go), [`callBegin edit
advancement`](../../../internal/engine/runtime.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R090](R090-moving-document-trees.md), [R100](R100-document-repetition-resumption.md).

[Optimization Catalog](../README.md)
