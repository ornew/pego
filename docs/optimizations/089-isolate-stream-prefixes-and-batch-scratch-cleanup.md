# 89. Isolate stream prefixes and batch scratch cleanup

**Status:** Active

**Scope:** Closure, recursive VM and iterative VM stream parsing. Generated
parsers do not expose streaming APIs.

A top-level `#stream` repetition can keep old result chunks reachable through
its live root and leave references in inactive parser scratch after an element
is committed. The implementation isolates the prefix storage and clears inactive
scratch at bounded cleanup points.

Before a top-level stream repetition starts, `nodeChunks` uses the existing
negative sentinel to budget 32 exact-size private prefix allocations. Each
allocation advances the sentinel toward zero. Once the budget is exhausted,
allocations use normal slabs. At `#stream` start, remaining tails are sealed
and the sentinel resets. A live root can remain in a sealed prefix chunk or be
allocated in a slab during left-recursion growth, so root-search protection
remains at stream boundaries and commits. Only completed frame structs are
cleared; published nodes, child slices, fields and function values are never
reused. A nil callback cancels private-prefix mode and collects the ordinary
repetition. The implementation adds no parser field.

After the undo journal resets, cleanup clears its full backing capacity. After
the VM completes the current element, cleanup clears only the inactive value
stack suffix. Both cleanup operations run with memo pruning once at least 1,024
input positions have been consumed since the prior prune. Until that boundary,
inactive references may remain; scratch capacity itself is retained.

Build-time constants select independent reference paths:
`pego_reference_stream_prefix`, `pego_reference_stream_undo`, and
`pego_reference_stream_stack`. The combined
`pego_reference_stream_storage` tag selects all three reference paths. These
controls add no parser fields or dynamic option checks. The change adds no public
API, bytecode or generated-parser behavior.

Compiler diagnostics from Go 1.27.1 keep `newNode`, `nodes`, `fields` and
`newFunc` inline. `kids` loses inline eligibility (estimated cost 74 before
versus 93 after, with a budget of 80); `newFrame` is not inline in either build
(97 versus 138). These diagnostics describe compiler eligibility only and do
not establish a timing effect.

## Validation and measurements

Focused default, combined-reference and single-mechanism tests pass for all
three engine backends. Coverage includes the prefix budget and slab fallback,
memoized prefixes, nil callbacks, left-recursion roots, and zero, finite, wide
and unbounded repetitions. An atomic capture derived from a left-recursive
prefix survives two stream emissions and is checked by a post-stream predicate.
The full root suite, all ten parser modules, generation freshness, vet and
site tests also pass. Five paired rounds are complete for every case.

Returned values remain immutable across commits; undo and VM cleanup clear
inactive references at their pruning boundary.

Five alternating fresh-process pairs on 2026-10-11 use Go 1.27.1,
Apple M3 Max, `darwin/arm64`, `GOMAXPROCS=4` and 500 ms per throughput case.
Ratios are medians of within-round elapsed-time ratios; ranges are the lowest
and highest paired ratios. The same-source combined reference retains bounded
text snapshots and disables only this record's three storage mechanisms.

In the six large-first mixed stream cases, default/reference median time ratios
are 0.759–0.866, with all five pairs below parity in every case. Uniform small
and uniformly wide rows have paired ranges crossing parity. The prefix-size
controls add 8,347–8,506 B and 40 allocations per parse with a callback, while
nil callbacks keep the allocation count and differ by at most 2 B. Retention
workloads add 123–156 B and 3–4 allocations per parse. These results establish
a benefit for the measured large-first workload and a prefix-allocation
tradeoff; they do not establish a general throughput improvement.

The 12 Unicode frame-scratch cases compare both the exact parent and the
same-source reference. Every exact-parent paired range crosses parity;
median ratios are 0.975–1.044. They add 136–174 B and 3–4 allocations per parse
relative to the parent. The short recursive-VM CodePoints case has a median
ratio of 1.002 [0.972–1.038] relative to the parent, which already includes
the text-snapshot implementation in [record 088](088-share-bounded-stream-text-snapshots.md).

The 24 batch controls (JSON, CSV, Minilang and XML; all three engine backends
and both units) have default/parent median time ratios of 0.961–1.017 and
no case slower in all five pairs. Default/reference medians are 0.969–1.064,
also with no case slower in every pair. Parent-relative allocated-byte medians
range from −2.8% to +1.65%; XML recursive- and iterative-VM CodePoints allocate
1.27% and 1.65% more respectively. The corresponding same-source-reference
byte counts are close to the default, but these samples do not identify the
cause of parent-relative allocation differences. The three CSV stream
controls have parent-relative time medians of 0.982–1.007, all with paired
ranges crossing parity, and add 1–3 allocations per parse.

The forced-GC retained-heap cases keep the parser alive during a callback
after a large first row and subsequent small rows. Input and program storage
are present before the baseline GC; the callback retains only the current
small row. Across 12 cases, default median retained heap is 164,000–441,808 B,
versus 2,041,728–2,340,000 B for the same-source combined reference: an
81.1–92.0% reduction. This measures post-GC live storage at that checkpoint,
not peak heap or throughput. Undo/stack capacity is retained.

Single-mechanism controls show an interaction: prefix isolation alone releases
the graph for closure parsing without a retained prefix capture. With that
capture, and in both VMs, each single mechanism leaves roughly 2 MB reachable;
the combined implementation releases it. These fixture-specific results
support measuring combinations rather than adding isolated savings.

## Alternatives

Allocating every prefix object independently increases allocation counts with
large headers; using slabs immediately lets a small live prefix retain later
result storage. The 32-allocation budget limits exact-size allocation while
preserving slab fallback for large prefixes. Clearing inactive VM capacity at
every pop or element commit repeatedly scans or writes hot scratch storage;
cleanup shares the existing 1,024-position pruning cadence instead.

## Reproduction

Run focused tests and stream controls with:

```sh
go test ./internal/engine -run '^(TestStream(Storage|Prefix|Frame)|TestVMStreamRepeat|TestFiniteStreamRepetitions)' -count=1
go test -tags=pego_reference_stream_storage ./internal/engine -run '^(TestStream(Storage|Prefix|Frame)|TestVMStreamRepeat|TestFiniteStreamRepetitions)' -count=1
go test ./internal/engine -run '^$' -bench '^(BenchmarkStreamPrefixSize|BenchmarkStreamFrameScratch|BenchmarkStreamRetentionWorkload)$' -benchtime=500ms -count=1 -benchmem
go test -tags=pego_reference_stream_storage ./internal/engine -run '^$' -bench '^(BenchmarkStreamPrefixSize|BenchmarkStreamFrameScratch|BenchmarkStreamRetentionWorkload)$' -benchtime=500ms -count=1 -benchmem
```

The prefix-size benchmark covers 32- and 5,000-value prefixes, with and without
a callback. Run retained-heap cases separately with
`-bench '^BenchmarkStreamStorageRetainedHeap$' -benchtime=1x -count=1`, rotating
default, combined-reference, and the three single-mechanism variants across
five fresh-process rounds. Single-mechanism reference tags are:

| Enabled mechanism | Reference build tags |
|:--|:--|
| Prefix only | `pego_reference_stream_undo,pego_reference_stream_stack` |
| Undo only | `pego_reference_stream_prefix,pego_reference_stream_stack` |
| Stack only | `pego_reference_stream_prefix,pego_reference_stream_undo` |

Set `GOMAXPROCS=4` for all variants. These samples force GC; duration is not
throughput. `BenchmarkStreamFrameScratch` covers 32- and 5,000-row Unicode streams. CSV stream and batch controls live in
`bench`:

```sh
(cd bench && go test -run '^$' -bench '^BenchmarkStream$/(closure|bytecode|iterative)$' -benchtime=500ms -count=1 -benchmem)
(cd bench && go test -run '^$' -bench '^BenchmarkParse$/(JSON|CSV|Minilang|XML)$/(closure|bytecode|iterative)$/(codepoints|bytes)$' -benchtime=500ms -count=1 -benchmem)
```

The batch comparison covers JSON, CSV, Minilang and XML across all
three backends and both position units (24 cases), plus three CSV stream cases
at 500 ms. Compare the default with the combined reference and exact parent
`aae5a238528de9da6bd30b3185cb74c14ed7623d`. The new retention/prefix fixtures
are absent from that parent, so use same-source controls for those cases.
Build test binaries before timing, then run five alternating fresh-process
rounds from each revision's package directory. Keep backend, unit, input shape
and process settings fixed within each pair. Report allocated bytes, allocation
count, retained heap and time separately. Retained-heap
benchmarks include GC and do not measure throughput.
