# 026. Bounded Stream Text Snapshots

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Reuse an immutable snapshot of text that a stream parser has already loaded.
A text value wholly inside that snapshot can share its backing string; cache
misses may build a new bounded snapshot at the requested start. Empty ranges
return empty strings; requests with too little loaded input and oversized
ranges keep the existing copy behavior. The candidate implementation is available on a development branch; the mainline behavior is
unchanged until integration.

## Motivation

Streaming input is held in mutable bytes or runes and consumed prefixes are
compacted. Text returned to syntax nodes and actions must remain stable after
compaction or buffer reuse. Copying every requested range preserves that
contract, but nearby ranges can repeatedly copy already-loaded text.

## Goals

- Preserve returned text in byte and code-point position units.
- Bound the backing source text retained by each cache-backed substring.
- Avoid read-ahead and leave mutable input buffers and discard behavior
  unchanged.
- Return empty ranges as empty strings; keep requests with too little loaded
  input and oversized requests on the existing copy path.
- Demonstrate useful allocation reductions without material regressions in
  streaming or batch controls.

## Non-goals

- Change stream chunking, UTF-8 decoding or input-buffer ownership.
- Return unsafe views of mutable input storage.
- Remove copies for oversized ranges, ranges with too little loaded input, or
  non-streaming ranges.
- Change capture-frame, undo-trail or result-child-storage lifetimes.

## Design

### Public behavior and contracts

There is no public API change. Returned strings remain immutable and valid
independently of later reads, parsing, compaction or parser reuse. An empty
range returns an empty string. The cache may only use positions already loaded;
requesting text must never read from the underlying reader.

### Implementation and invariants

Each parser input owns one immutable snapshot with a start position, end
position and normalized source string. In code-point mode, a parser-owned
offset table maps positions into the snapshot's UTF-8 bytes. The candidate
bounds a snapshot to 1,024 positions and 4,096 bytes.

A request wholly within the current snapshot can share its backing string. On
a cache miss, a request of at most 1,024 positions can create a new snapshot at
its requested start when at least 64 positions are already loaded there. This
also handles a request crossing the previous snapshot's edge. Larger requests
and requests with fewer than 64 loaded positions use the existing detached-copy
path. Cache construction is clamped to available input and does not call the
reader. Position arithmetic must avoid overflow near `MaxInt` by limiting the
loaded distance before adding it to the start.

Byte-mode snapshots preserve raw bytes. Code-point snapshots use the existing
rune-to-string normalization, including replacement of malformed input with
U+FFFD. Offsets are built from the normalized string. The candidate does not
mutate or alias the parser's mutable input buffer. When compaction makes the
cache obsolete, the cache owner is released; already returned strings may keep
at most one bounded snapshot as their backing storage. Undo-trail and old live-
root sibling references are separate retention concerns and are not changed
by this design.

### Compatibility and backend support

The mechanism applies to closure, recursive VM and iterative VM streaming in
the engine. Generated Go, typed Go and TypeScript parsers do not expose this
streaming path. The private `pego_reference_stream_text` build tag selects the
copying reference implementation without adding a public option. Mainline
behavior remains the existing copy path until the candidate is integrated.

### Alternatives considered

- **Copy every range:** simple ownership and the reference behavior; retained
  as fallback for requests with too little loaded input and oversized ranges.
- **Expose mutable input as a string:** unsafe because later compaction or
  reuse could invalidate returned text; rejected.
- **Replace the stream buffer with immutable chunks:** could share more ranges,
  but changes decoding, boundary and discard behavior; deferred as a broader
  design.
- **Cache without size or loaded-input thresholds:** risks retaining much more
  source text for small requests and constructing snapshots unlikely to be
  reused; rejected for this candidate.

## Testing

Compare optimized and reference behavior in both position units and all three
engine runtimes. Cover malformed UTF-8, random ranges, exact and crossing
snapshot boundaries, oversized requests, fewer than 64 loaded positions,
one-byte reads, early EOF and positions near `MaxInt`. Verify text access does
not trigger a read. Retain emitted node and action text through subsequent
stream elements and compactions, and verify results match whole-input parsing.
Test that obsolete cache owners are released while previously returned strings
remain valid. Measure backing retention separately from cumulative allocation.

## Performance and results

Focused measurements on Go 1.27.1, Apple M3 Max (`darwin/arm64`), with
`GOMAXPROCS=4`, used five alternating one-second rounds against both the base
commit and a same-source build tagged `pego_reference_stream_text`.

For 50,001 CSV records, closure, recursive VM and iterative VM allocated
306,046/308,128/308,127 allocations in the same-source reference and
8,159/10,240/10,240 in the candidate, with bytes per operation down 1.1–1.2%.
Candidate/reference median time ratios were 0.984/0.996/1.000; all paired
ranges included parity. The base-commit medians were 0.981/0.973/0.986, with
only recursive VM below parity across all samples. These data show a large
allocation reduction without a general timing claim.

For 32-record Unicode streams, allocations fell 63–75%; CodePoints bytes per
operation rose 1.30–1.42% and Bytes rose 0.05%. For 5,000 records,
allocations fell 94%; CodePoints bytes fell 0.35–0.37% and Bytes rose 0.05%.
All paired time ranges included parity. Nine JSON, CSV and Minilang batch
controls also had ranges including parity; there is no uniform batch-time
gain claim.

A five-round retained-heap check with GC measured first-row retention at about
88–89 KB for 5,000 and 50,000 rows, respectively, with median increases of
1,120 bytes and 160 bytes over the reference. Whole-result retention was
about 5.02 vs 5.04 MB and 49.84 vs 50.08 MB. These measurements include GC
time and are not throughput results. A 56-byte 64-bit / 28-byte 32-bit input
field is present in both candidate and same-source reference builds, so it is
outside that comparison.

Focused and full-suite measurements cover different questions. The retained-heap
measurements do not establish a maximum live heap or the cost of stale
undo-trail references; the full-suite snapshot does not isolate this change.

## Limitations and open questions

- The clean 131-case, three-sample benchmark checkpoint completed on
  `efcd2db` at default GOMAXPROCS=16. It is descriptive and does not isolate
  this change. The candidate remains on a development branch; broader
  undo-trail and old live-root sibling retention are separate follow-ups.
- Retained-text tests cover emitted values across stream commits; stale
  sibling references still require a separate ownership investigation.
- Undo-trail references and older live-root sibling references can retain
  storage independently of the snapshot owner; their cleanup is a separate
  concern.
- Measurements should be repeated on the target environment if cache bounds
  or eligibility thresholds change.
