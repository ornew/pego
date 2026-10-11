# 89. Isolate stream prefixes and batch inactive scratch cleanup

**Status:** Candidate

**Scope:** Closure, recursive VM and iterative VM stream parsing. Generated parsers do not expose streaming APIs.

A top-level `#stream` repetition can keep old result chunks reachable through
its live root and leave references in inactive parser scratch after an element
is committed. This candidate separates storage by ownership and clears dead
references at bounded pruning points.

Before a top-level stream repetition starts, `nodeChunks` begins at
`-nodeChunk/8` (-32). Each cold exact-size prefix allocation advances this
existing sentinel toward zero. Allocations after the 32-slot budget use normal
slabs. At `#stream` start, remaining tails are sealed and the counter resets to
zero. A live root may remain in its sealed prefix chunk or be allocated in a slab
during left-recursion growth. Frame cleanup therefore retains the root-search
protection at stream boundaries and commits. Only completed frame structs are
cleared; published nodes, child slices, fields and function values are never
reused. A nil callback cancels private-prefix mode and collects the ordinary
repetition. The implementation adds no parser field and keeps allocator
helpers inline in normal compiler paths.

After the undo journal is reset, undo cleanup clears its full backing capacity.
After the VM has completed the current element, stack cleanup clears only the
inactive value-stack suffix. Both cleanup operations run with memo pruning when
at least 1,024 input positions have been consumed since the prior prune. Until
that boundary, stale references may remain; slice capacity is retained.

Three build-time constants select the independent reference paths:
`pego_reference_stream_prefix`, `pego_reference_stream_undo`, and
`pego_reference_stream_stack`. The combined
`pego_reference_stream_storage` tag selects all three reference paths. These
controls add no parser fields or runtime flag checks. No public API, bytecode
or generated-parser behavior changes.

## Alternatives and measurements

The earlier full-prefix slab trial wasted about 40 KB on small prefixes; an
unbounded exact-size trial made roughly 35,000 allocations for a 5,000-value
header. The bounded sentinel budget avoids those allocation extremes. Clearing
all inactive VM slots on each element commit increased a mixed VM screening
workload by 29–41%; clearing on each VM pop increased small VM workloads by
7–13%. Those alternatives were not selected.

Five alternating rounds on 2026-10-11 used Go 1.27.1 on Apple M3 Max,
`darwin/arm64`, with `GOMAXPROCS=4`: 42 stream controls at 500 ms and the
batch/CSV controls at one second. Stream controls compare the candidate with
a same-source combined-reference build. Batch/CSV controls also compare the
code baseline `66a1680`. Each round uses fresh processes. Ratios are medians
of paired time ratios;
ranges are the minimum and maximum paired ratios. In the large-first mixed
stream workloads, candidate/reference time ratios were 0.820–0.923 and all
ranges were below parity. Most batch-control ranges and all CSV-stream ranges
included parity. One small 32-row iterative-VM Bytes case was slower (1.007
[1.001–1.032], with 136 additional bytes and 3 allocations). These workloads
do not establish a general speedup.

A 500 ms comparison showed wide recursive-VM CodePoints at 1.037
[1.005–1.049]. A focused five-round, one-second confirmation measured 1.003
[0.979–1.021] for CodePoints and 0.998 [0.974–1.005] for Bytes; each range
includes parity. The initial slowdown did not persist. No general throughput
claim follows from these workloads.

The prefix-size control adds about 8.35–8.51 KB and 40 allocations per
operation for 32- and 5,000-value headers when a callback emits values. With a
nil callback, allocation counts are unchanged and bytes vary by only 1–2 B.
Unicode frame-scratch controls add about 136–159 B and 3–4 allocations per
operation. These are measured tradeoffs of prefix isolation.

Across 12 forced-GC retained-heap cases, the combined candidate retained about
164–442 KB versus 2.04–2.34 MB for the combined reference, an 81–92% reduction.
The single-mechanism controls generally did not release the full retained
result graph; this reduction depends on combining prefix isolation and scratch
cleanup. The retained-heap benchmark includes GC time and is not a throughput
measurement.

## Validation and reproduction

Focused default, combined-reference, and prefix-only/undo-only/stack-only
controls pass for both position units and all three engine backends; the
frame-reference interaction also passes. They cover the private-prefix
allocation budget and slab fallback, memoized prefixes and nil callbacks, LR
leader roots, and zero, finite, wide (on 64-bit) and unbounded repetitions.
Result captures remain immutable; undo and VM cleanup clear inactive references
at their pruning boundary.

Run the focused tests and engine controls with:

```sh
go test ./internal/engine -run '^(TestStream(Storage|Prefix|Frame)|TestVMStreamRepeat|TestFiniteStreamRepetitions)' -count=1
go test -tags=pego_reference_stream_storage ./internal/engine -run '^(TestStream(Storage|Prefix|Frame)|TestVMStreamRepeat|TestFiniteStreamRepetitions)' -count=1
go test ./internal/engine -run '^$' -bench '^(BenchmarkStreamPrefixSize|BenchmarkStreamFrameScratch|BenchmarkStreamRetentionWorkload)$' -benchtime=500ms -benchmem
go test -tags=pego_reference_stream_storage ./internal/engine -run '^$' -bench '^(BenchmarkStreamPrefixSize|BenchmarkStreamFrameScratch|BenchmarkStreamRetentionWorkload)$' -benchtime=500ms -benchmem
```

The prefix-size benchmark compares 32- and 5,000-value prefixes with and
without a callback. Run retained-heap cases separately with
`-bench ^BenchmarkStreamStorageRetainedHeap$ -benchtime=1x -count=1`, rotating
the default, combined-reference and three single-mechanism variants across
five fresh-process rounds. Use these build tags for the single-mechanism
controls:

| Enabled mechanism | Reference build tags |
|:--|:--|
| Prefix only | `pego_reference_stream_undo,pego_reference_stream_stack` |
| Undo only | `pego_reference_stream_prefix,pego_reference_stream_stack` |
| Stack only | `pego_reference_stream_prefix,pego_reference_stream_undo` |

These samples force GC; duration is not throughput.
`BenchmarkStreamFrameScratch` covers 32- and 5,000-row Unicode streams. The CSV
stream and batch controls are in `bench`:

```sh
(cd bench && go test -run '^$' -bench '^BenchmarkStream$/(closure|bytecode|iterative)$' -benchtime=1s -count=1 -benchmem)
(cd bench && go test -run '^$' -bench '^BenchmarkParse$/(JSON|CSV|Minilang)$/(closure|bytecode|iterative)$/codepoints$' -benchtime=1s -count=1 -benchmem)
```

For attribution, compare the single-mechanism builds with the combined
candidate and reference builds. Repeat each command in five
alternating fresh-process rounds. Keep backend, position unit, input shape and
process settings fixed within paired runs. Report allocated bytes, allocations,
retained heap and time separately; retained-heap benchmarks include
garbage-collection cost and do not measure throughput.
These measurements do not establish a universal speedup.
