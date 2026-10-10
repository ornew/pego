# 72. Reuse structurally equal duplicate capture types

- Every duplicate capture label previously called the general type union,
  which formats, sorts and deduplicates nested record fields. Repeated
  references to the same recursively inferred record allocate these strings
  again for each occurrence in every inference round. The capture merger now
  compares immutable type structures and reuses the previous equal basic,
  named, list or record type. Ordered nested unions and named-node kinds are
  compared conservatively; top-level optionals and unions still use the
  general normalizer. Capture availability remains the logical OR of both
  occurrences. This changes shared type checking before compilation or
  generation, not the parsing runtimes or generated matching code.
- Five alternating parent/candidate pairs with a 200 ms benchtime compare
  baseline `0ab91da` with this change on Apple M3 Max, Go 1.27.1. Grammar parsing and Program
  setup are outside the timer; each iteration runs the complete `checkTypes`,
  including dependency analysis, recursive inference and diagnostic checking.
  The recursive fixture repeats `"😀" x:main` in `main`; the record fixture
  repeats `x:item`, where `item` captures a Match and a list of nested records.

  | Shape | Occurrences | Time before → after | B/op before → after | Allocs/op before → after |
  |:--|--:|:--|:--|:--|
  | Recursive | 1,024 | 423.966 → 61.126 ms (−85.6%) | 1.188 GB → 32.16 MB | 8,204,256 → 274,551 |
  | Recursive | 8,192 | 4,096.058 → 511.795 ms (−87.5%) | 9.528 GB → 274.67 MB | 65,628,207 → 2,139,000 |
  | Record | 1,024 | 1.921 → 0.456 ms (−76.3%) | 2.065 → 1.067 MB | 51,277 → 4,219 |
  | Record | 8,192 | 16.432 → 4.398 ms (−73.2%) | 16.500 → 8.505 MB | 409,698 → 32,912 |
  | Record | 32,768 | 65.064 → 17.327 ms (−73.4%) | 68.765 → 36.784 MB | 1,638,524 → 131,239 |

  All five time ranges are disjoint. Bytes are cumulative allocations over
  one complete checker invocation, not retained heap. Current bounded
  recursive inference scales approximately linearly in capture count in
  these samples; an older quadratic description is not a current complexity
  claim. Structural comparison still traverses nested types and expression
  checking still processes every occurrence in each inference round.
- Separate single-iteration diagnostics at 32,768/131,072 recursive captures
  take 14.229 → 2.025 s and 55.894 → 8.210 s, respectively. At 131,072,
  cumulative allocations fall 152.647 → 4.578 GB and allocation counts
  1,050,020,237 → 34,088,466. These reproduce the original large case and
  are diagnostic observations, not five-sample estimates. The remaining
  inference/AST traversal work is still substantial.
- Mixed `A`/optional `B` duplicates retain general union normalization:
  1,024/8,192/32,768 pairs change +0.1%/+0.3%/−0.6% in time, with overlapping
  ranges and essentially unchanged allocation counts/bytes. Distinct-label
  record controls change −0.3%/−0.2%/−0.8%, also with overlapping ranges.
  Unchanged calculator/minilang/JSON grammar AST controls change
  +0.6%/approximately 0%/−4.2%, all with overlapping ranges and identical
  bytes/allocation counts. These controls do not establish runtime speedups
  or promise savings for growing heterogeneous unions.
- Type regressions retain exact public types, recursive widening, nested
  records/lists, optional and choice availability, repetition-local captures,
  nil actions and capture-reading actions/predicates. Duplicate-label error
  positions remain exact across Unicode sequences at several sizes.
  A pairwise oracle compares the capture merger with the general union for
  all availability flags, including reordered unions, nested optionals,
  never/any/nil and equal names with differing node kinds. Independent source
  review checks normalization, ordering and immutable type reuse.
- Reproduce with
  `go test ./internal/engine -run '^$' -bench '^BenchmarkCaptureChecking$' -benchtime=200ms -benchmem`.
  Build parent/candidate binaries with the same benchmark source, restoring
  `check.go` and `types.go` from `0ab91da` through a Go overlay for the parent.
  Run five alternating pairs for recursive sizes 1,024/8,192 and all
  record/mixed/unique/control cases, without concurrent tests/builds/probes.
  Large recursive cases use separate single-iteration diagnostics. Raw
  results stay local; the full suite checkpoint follows P27 in this core
  analysis optimization group.
