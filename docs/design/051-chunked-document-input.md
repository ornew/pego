# 051. Chunked Input for Document Edits

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-13

## Summary

Use internal chunked storage for `Document` so edits to sufficiently large documents need not shift the entire byte or code-point suffix. Documents below 4,096 logical positions retain flat storage; storage promotes to the piece table when an edit grows the document across that threshold. Keep the public `Document.Edit`, `Document.Parse`, `Document.Text`, and `Node.Text` behavior unchanged. Ordinary batch parsing and stream parsing retain their contiguous input paths.

This design does not add a public rope, input callback, edit-batch API, or immutable snapshot contract. It defines the internal piece-table representation and the correctness and measurement gates for its use by `Document`.

## Motivation

`input.replace` currently uses `slices.Replace` on the full `[]byte` or `[]rune` input. It also splices the source string and, for code-point input, the byte-offset table. A small edit can therefore move work proportional to the document size. This is distinct from the parser's memo invalidation and reparse work.

`Document` owns fully loaded input and memo state. `Parser.Parse` uses a separate whole-input path, and stream parsing reads and discards a contiguous prefix. The earlier input-storage design already identifies chunked Document input as a separate internal stage; immutable snapshots and public storage callbacks remain out of scope here. See [design 019](019-incremental-document-and-tree-tooling.md), [design 032](032-versioned-document-snapshots.md), and the current [input implementation](../../internal/engine/input.go) and [Document implementation](../../internal/engine/document.go).

## Goals

- Make a bounded edit cost depend primarily on the changed text and number of pieces, rather than the unchanged suffix length.
- Preserve matching, diagnostics, positions, memo reuse, recovery, left recursion, and edit validation in both `CodePoints` and `Bytes` modes.
- Keep `Document.Text()` current and preserve the lifetime of every returned
  `Node.Text` string across later edits. Retained node positions and descendant
  fields continue to follow the existing mutable memo-reuse contract.
- Leave contiguous batch and stream parsing unchanged unless a separately measured shared helper has no matching-path cost.
- Measure edit-only work separately from reparse and report live heap without confusing retained old Nodes with storage retained only by the Document.

## Non-Goals

This proposal does not change the public API or `Node` fields, add atomic multi-edit transactions, alter position units, add generated-parser Documents, or promise persistent tree snapshots. It does not change stream prefix discard or require callers to supply a rope. It makes no performance claim before paired measurements.

## Design

### Internal Representation

Use a Document-only piece table over immutable text chunks for documents with
at least 4,096 logical positions. Smaller documents keep the existing flat
representation, avoiding piece-index overhead where edit-copying is cheap.
When a flat document grows across the threshold, promote it to piece storage.
Promotion is one-way: shrinking a promoted document does not switch it back to
flat storage.
Inserted text is stored in chunks, and the logical document is an ordered
sequence of ranges into those chunks. Byte mode retains exact bytes;
code-point mode retains decoded runes and normalized text. Edits split and
replace ranges. The indexed sequence supports lookup by absolute logical
position; adjacent ranges are not assumed to coalesce.

Keep `input`'s contiguous representation for `Parser.Parse` and `ParseStream`. Do not route those paths through piece lookup or introduce an interface call per matched character. `Document` may use an internal source abstraction for `decode`, `byteAt`, `text`, `isNewline`, `lineCol`, and `loaded`; choose the narrowest adaptation that preserves the contiguous fast path. A parser cursor may cache the current piece for sequential reads, but absolute positions remain authoritative for memo entries, edits, diagnostics, and random lookups.

When the piece count exceeds 512, compact the logical text into one piece while retaining piece-based storage; this is not a fallback to the old flat input path. Compaction preserves current text and positions and may discard old chunk references only when no returned Node or other supported owner depends on them. The 4,096-position promotion threshold and 512-piece compaction bound are implementation details, not user-visible semantics.

### Text and Position Semantics

`Document.Text()` returns the same complete logical text as today. It may flatten and cache a contiguous string for the current edit generation; an edit invalidates that cache. Repeated reads in one generation should reuse the cached result where practical.

`Node.Text` remains an immutable string containing the matched source at the
time its node was produced. A span fully contained in one immutable source
range may reuse that string storage. A span crossing pieces must materialize
its text into an owned immutable string; it must never expose a mutable buffer
or depend on a later piece-table mutation. Memo reuse may still shift retained
nodes and node-valued fields in place under the current `Document` contract.

Logical positions remain absolute positions in the configured unit. In
`Bytes` mode, source bytes remain exact, including malformed UTF-8, and edit
endpoints must continue to satisfy the existing byte-boundary rule. UTF-8
decoding must see bytes across adjacent pieces as one logical stream: a
multibyte scalar or malformed prefix may straddle a piece boundary after an
edit. A piece boundary is never an EOF boundary; malformed-prefix decoding
must record the same examined range and EOF dependency as the flat input.
In `CodePoints` mode, initial malformed bytes and each insertion are
normalized independently to U+FFFD in the logical rune sequence. Joining
pieces must not re-decode their raw bytes into a different rune. Boundary
lookup, text extraction, line starts, and `lineCol` must agree with the
current flat-input semantics in each unit.

### Edit and Parser Ownership

`Document.Edit` keeps its current signature and validation order. Validate range and byte boundaries before mutating the piece index; an invalid edit leaves text, offset/line tables, memo state, statistics, and previously returned Nodes unchanged. A successful edit updates storage and then runs the same memo splice/invalidation logic as today. Multiple successive edits before `Parse` must preserve their current ordering and coordinate semantics.

The Document is the sole mutable owner of its piece table, index, and generation-local text/position caches. Parsing remains single-owner and non-concurrent as today. Parser reads use a stable view of the current piece index for the duration of one `Parse`; mutation during parsing is unsupported. Nodes returned by a parse own or reference only immutable text, never mutable index buffers. Releasing or flattening Document storage must not invalidate returned Node text.

Small Documents continue to use flat storage until promoted at the 4,096-position threshold. Piece-table compaction above 512 pieces consolidates the current text into a single piece but keeps the piece-based representation. These transitions must not alter memo dependency ranges or parse outcomes. Do not silently retain every historical chunk or version: after edits, chunks unreachable from current input and supported returned Nodes should be collectable.

## Alternatives Considered

- **Keep the flat slices.** This is the simplest and fastest current baseline for matching. It remains the path for Documents below 4,096 logical positions and for batch/stream parsing, but edit copying scales with the unchanged suffix.
- **Piece table with an indexed piece sequence.** Edits add or split a small number of ranges instead of moving the suffix, and immutable chunks simplify `Node.Text` ownership. The costs are index lookup, fragmentation, position conversion, and text materialization across pieces. This is the recommended first prototype because it directly targets the edit path without requiring a public storage API.
- **Gap buffer.** A gap makes nearby edits cheap and supports contiguous reads, but moving the gap to edits at the opposite end still copies a large range. It also complicates stable text references while moving storage. Include it only if representative locality makes it materially better than the piece table.
- **Caller-owned rope or read callback.** An external source could avoid copying an editor's existing piece tree, but creates lifetime, concurrent mutation, random-access, position, and callback-cost contracts for all parser reads. Keep this as a later public API design; do not add it to the first internal prototype.
- **Flatten after every edit.** This retains the current contiguous matching path but preserves the O(document size) edit cost, so it is only a comparison control.

## Testing

Compare the candidate with a simple contiguous-string oracle after deterministic and randomized edit sequences. Cover single and repeated edits before parsing; insert, delete, and replace at the start, middle, and end; empty input; adjacent edits; invalid ranges; and byte endpoints inside a multibyte character. Verify invalid edits leave all observable state unchanged.

For both position units, include ASCII, multi-byte and astral characters,
combining sequences, invalid UTF-8, and replacements that complete an invalid
prefix. In `Bytes` mode, test multibyte sequences split across pieces. In
`CodePoints` mode, test that separate invalid-byte insertions stay separate
replacement runes. Test literals and character classes crossing piece
boundaries, newline anchors, line/column diagnostics, `Document.Text`, and
`Node.Text` on spans within one piece and across several pieces. Retain old
Nodes across edits and compaction: verify their text lifetime and compare
their shifted spans and fields against the existing mutable Document behavior.

Differentially compare fresh contiguous parsing and edited Document parsing for closure, recursive VM, and iterative VM results, spans, errors, expected items, and recovery. Include left recursion, predicates, memo hits and invalidations, operators rules, position-dependent values, large-to-small edits, and trace panics. Confirm `Parser.Parse` and `ParseStream` remain on their existing paths and retain their current behavior.

## Performance and Results

The measured piece table is adopted for `Document`. Five alternating 500 ms samples compared same-source flat and piece-table variants on JSON and XML batch parses using closure, bytecode and iterative backends in both `CodePoints` and `Bytes` modes. Batch parse times were 1.0076–1.0331× the flat control. Treat this as a small measured cost for those cases, not a general parser slowdown estimate.

Five paired incremental samples in `CodePoints` measured edit-and-reparse time ratios of 0.7786–0.8056; the long CSV workload measured 0.7039–0.7549×. Incremental allocation ratios were 0.463–0.473 B/op, and long CSV ratios were 0.183–0.188 B/op. These gains apply to the measured edit workloads and do not imply the same benefit for other inputs or edit patterns.

The live-heap script measured the retained `Document` after 0, 1, 10 and 100 edits on a 4 MiB input, without parsing or retaining Nodes. Retained heap was similar in `CodePoints`. In `Bytes`, the piece-table variant retained about 4 MiB initially versus 8 MiB for flat storage, and about 12.1 MiB versus 16.1 MiB after 100 edits. These figures are not a total-process memory guarantee.

An isolated edit-only `CodePoints` microbenchmark over 1–4 MiB inputs measured 0.00008–0.00485× the flat edit time. This excludes parsing and text materialization and should not be read as an end-to-end speedup. Small documents use flat storage: in the three-pair edit-only crossover check, 1 KiB edits were 1.45–2.14× slower in `Bytes` and 0.545–1.502× in `CodePoints`; at 4 KiB, every tested position and unit was faster (0.163–0.515× in `CodePoints`, 0.457–0.713× in `Bytes`). The piece table adds index and piece metadata, and ordinary batch parse showed the small cost above. Public input callbacks, persistent snapshots, and generated-parser Documents remain out of scope.

## Limitations and Open Questions

Piece lookup may add cost to every Document input read, and a highly fragmented document may erase edit savings. Text returned across chunks may allocate more often than the current substring path. The 4,096-position promotion threshold and 512-piece compaction bound are current implementation choices and may need reevaluation if representative workloads change. If future work requires a public callback or changes `Node.Text` lifetime, prepare a separate API/ownership design first.
