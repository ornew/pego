# 093. Chunked Input Storage for Document Edits

- **Status**: Adopted
- **Date**: 2026-10-13

## Problem and Mechanism

`Document.Edit` previously spliced the full input buffer and, in `CodePoints`
mode, its offset table. A small edit could copy the unchanged suffix. A
Document below 4,096 logical positions stays flat; when an edit grows it
across that threshold, storage promotes to immutable text chunks and an
indexed sequence of ranges. Edits split and replace ranges. Above 512 pieces,
the current text is compacted into one piece while remaining in piece-based
storage. Promotion is one-way; later shrinking does not restore flat storage.
Ordinary batch and stream parsing retain their contiguous paths. See
[design 051](../design/records/051-chunked-document-input.md) for text ownership,
UTF-8 boundary behavior and correctness contracts.

## Applicability

| Execution profile | Applied / Partial / Not applied / Not applicable | Reason |
|:--|:--|:--|
| `Document` edits and reparses | Applied | Documents below 4,096 logical positions stay flat; larger Documents use piece storage. Batch and stream input stay contiguous. |
| Ordinary batch parsing | Not applied | This change does not route `Parser.Parse` through the piece index. |
| Stream parsing | Not applied | Stream prefix handling keeps its contiguous input path. |
| Generated parsers | Not applicable | Generated parsers do not expose `Document`. |

## Reproduction

The flat and piece-table variants were built from the same source and compared
in alternating paired runs. The five-pair 500 ms batch comparisons covered
JSON and XML, closure, bytecode and iterative backends, and both `CodePoints`
and `Bytes` input. The five-pair 500 ms incremental and long-CSV comparisons
were in `CodePoints` mode. A same-source flat control was selected with the
`zp_reference_document_pieces` build tag. A separate live-heap script
measured a retained 4 MiB `Document` after 0, 1, 10 and 100 edits without
parsing or retaining Nodes. The reported ratios are scoped to these workloads,
not universal estimates. Raw per-run output is not part of the repository.

## Results

| Workload | Flat / reference | Piece table | Change | Scope |
|:--|--:|--:|--:|:--|
| JSON/XML batch parse | 1.000× | 1.0076–1.0331× time | 0.76–3.31% slower | Five paired runs; closure, bytecode and iterative; both input units. |
| Incremental edit and reparse | 1.000× | 0.7786–0.8056× time | 19.44–22.14% faster | Five paired runs; `CodePoints` only. |
| Long CSV incremental workload | 1.000× | 0.7039–0.7549× time | 24.51–29.61% faster | Long repeated-input workload; not representative of every grammar. |
| Incremental edit and reparse | 1.000× | 0.463–0.473× B/op | 52.7–53.7% fewer bytes | Same `CodePoints` scope as the incremental workload above. |
| Long CSV incremental workload | 1.000× | 0.183–0.188× B/op | 81.2–81.7% fewer bytes | Long repeated-input workload. |
| 4 MiB retained `Document`, `Bytes`, 0 edits | 8.0 MiB | 4.0 MiB | About 50% less | Retained-heap checkpoint. |
| 4 MiB retained `Document`, `Bytes`, 100 edits | 16.1 MiB | 12.1 MiB | About 25% less | Retained-heap checkpoint. |
| 4 MiB retained `Document`, `CodePoints`, 0–100 edits | Similar | Similar | No material change observed | Retained-heap checkpoint. |

The three-pair edit-only crossover check found that 1 KiB edits were 1.45–2.14×
slower in `Bytes` and 0.545–1.502× in `CodePoints`. At 4 KiB, all tested edit
positions were faster: 0.163–0.515× in `CodePoints` and 0.457–0.713× in
`Bytes`. This supports keeping small Documents flat rather than treating the
piece-table results as uniformly beneficial at every size.

An isolated edit-only `CodePoints` microbenchmark over 1–4 MiB inputs measured
0.00008–0.00485× the flat edit time. It excludes parsing and text materialization
and is not an end-to-end speed estimate.

## Correctness and Tradeoffs

The piece table preserves the existing `Document` text, position, edit, memo
reuse and returned-node text contracts in both input units. It keeps malformed
UTF-8 normalization consistent with the contiguous implementation and handles
UTF-8 sequences across piece boundaries in `Bytes` mode. The measured batch
cost is small but visible in these JSON/XML cases; keeping batch and stream
parsing on contiguous storage limits the change to the incremental Document
path. The heap measurements apply to the tested retained Document state and do
not establish a process-wide memory bound.

## Decision and Limits

Use the piece table by default for Documents at or above the measured size
threshold. The decision accepts
the measured 0.76–3.31% batch-control cost in exchange for lower edit-and-parse
time and allocations in the tested incremental workloads, along with lower
retained bytes for `Bytes` Documents. Do not generalize these results to other
inputs, edit patterns, or APIs. Public input callbacks, persistent snapshots,
and generated-parser Documents remain out of scope.
