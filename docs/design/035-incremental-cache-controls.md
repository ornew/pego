# 035. Incremental Cache Controls

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add an explicit way for a `Document` owner to discard incremental reuse history while keeping its current text and parse configuration. Expose cache accounting separately from the last parse's evaluation statistics, and report resumed repetition elements independently from memo hits. The reset is manual and deterministic; it is not an automatic memory budget.

```go
type CacheStats struct {
    LiveMemoEntries     int
    RetiredMemoEntries  int
    RepetitionElements  int
    EstimatedBytes      uint64
}

func (d *Document) ResetHistory()
func (d *Document) CacheStats() CacheStats
```

The API is proposed. Preserve the existing combined meaning of `ParseStats.Reused`: it counts both memo-result reuse and elements reused by repetition resumption. Add `MemoHits` and `ResumedElements` so callers can see the two sources separately. Existing callers therefore keep their current `Reused` result.

## Motivation

`Document` incrementally reuses memo results and repetition runs. Memo entries can retire into an internal free list while their slabs remain allocated, and the document retains repetition records and VM scratch between parses. There is currently no public reset or cache-size view. The existing public `Stats` reports `Evaluated` and `Reused`; repetition resumption is an internal counter, so memo hits can make the benefit or loss of repetition reuse hard to see. See [`Document`](../../internal/engine/document.go), [`memoTable`](../../internal/engine/memo.go), [`resume.go`](../../internal/engine/resume.go), and the public [`Document` API](../../pego.go).

This record proposes observability and an explicit lifecycle control. It makes no claim that an automatic cap is needed or that reset improves parse speed.

## Goals

- Let a single-owner editor release incremental memo and repetition history after large edits, inactivity, or a caller-selected policy.
- Preserve the current document text, position unit, backend, depth and trace configuration.
- Report memo hits, resumed elements, live/retired entries, retained cache capacities and an explicitly approximate byte estimate without conflating them.
- Keep old parse results valid and make their post-reset mutation behavior explicit.

## Non-Goals

- No automatic eviction threshold, global memory cap, background cleanup or process-wide cache manager.
- No immutable snapshots, tree diffing, input piece table or change notifications; see the relevant feature designs separately.
- No change to ordinary `Parser.Parse`, memoization policy, grammar semantics or serialized modules.

## Design

### Public Behavior and Contracts

`ResetHistory` clears the document's memo table, retired memo-entry pool, repetition-run records, edit log used to validate reuse, and reusable parser stacks. It also drops derived offset and line tables so they can be rebuilt lazily. It preserves the current text and the document's construction settings. It does not parse, reset `Stats`, or return an error. The next `Parse` is a fresh parse of the current text; later edits and parses resume incremental reuse from that new baseline.

Calling `ResetHistory` detaches memo-owned trees from future reuse. A tree returned before reset remains readable and is no longer moved by later document parses. It may already reflect edits and node moves that happened before reset; the method does not restore an earlier version. As elsewhere, do not read or edit the document concurrently with `Parse`, `Edit`, `ResetHistory`, or cache inspection.

`CacheStats` is a point-in-time estimate for the current reuse cache. `LiveMemoEntries` counts linked entries; `RetiredMemoEntries` counts reusable retired entries. `RepetitionElements` counts elements currently retained in resumable runs. `EstimatedBytes` estimates cache-owned backing storage (memo slots/slabs/seen bits, repetition records, and reusable parser stacks), excluding the current document text/input representation, allocator metadata, caller-held trees and strings shared with those trees. It is an estimate, not a promise about process RSS or bytes immediately returned to the operating system. After `ResetHistory`, all four fields report zero for reuse history.

`ParseStats.MemoHits` counts memo-result reuse and `ParseStats.ResumedElements` counts elements reused through repetition resumption in the most recent `Parse`. `ParseStats.Reused` remains their combined count, preserving its current behavior. A no-edit parse can report reuse from either or both sources. Resetting history leaves the most recent parse statistics intact until another parse completes.

### Implementation and Invariants

Reset by replacing the memo table and clearing document references to repetition runs, edits, parse-local children and reusable VM values. Do not call the memo table's ordinary `reset` when the purpose is to release capacity: it deliberately keeps slabs and backing arrays. Preserve `input.src`, decoded bytes/runes and input unit; clear only derived indexes that can be rebuilt. No active parse may observe a partial reset.

Cache accounting must traverse linked memo slots and the free list without changing them, and count only run records still owned by the document. Estimate capacities from actual backing arrays/slices; document which fixed headers and shared allocations are omitted. Keep cache stats read-only and avoid allocating a copy of the memo or run tables to produce them.

The edit log can be cleared because the memo and runs it relates to are discarded together. New parse results start at the reset generation. Caller-held old nodes are not registered in the new memo, so the parser has no reason to move them again.

### Compatibility and Backend Support

This is an additive Go API on `Document`, available with Closure, recursive Bytecode and BytecodeIterative wherever the current document can be created. `ResetHistory` preserves the selected unit/backend and trace settings. Generated parsers and TypeScript have no `Document` API and are unaffected. Existing `Stats` fields and meanings remain stable; adding a field is source-compatible for keyed literals but may affect unkeyed `ParseStats` literals, which should be called out in release notes.

### Implementation Sequence

1. Add separate memo-hit and resumed-element counters while retaining the existing combined `Reused` count; test root memo hits, no-edit parses and resumed repetitions.
2. Add a non-mutating cache accounting walk with capacity estimates and document exclusions.
3. Add `ResetHistory`; verify current text/options survive and the next parse agrees with a fresh parse. Verify old results stop changing after reset and new history repopulates normally.
4. Add examples and a guide section showing an editor-controlled reset policy without prescribing a universal threshold.

## Alternatives Considered

- **`ClearCache` that clears only current memo entries.** It sounds simple but leaves retired slabs, repetition records and edit history resident. `ResetHistory` describes the complete reuse boundary.
- **An automatic byte budget.** It would need eviction timing, accounting semantics, a default threshold and decisions about which caches to retain. Start with explicit reset and measurements; callers can choose when to invoke it.
- **Expose only `Stats`.** Last-parse counters do not describe current retained capacity, and a combined reuse count obscures repetition-resumed work. Keep parse activity and cache state separate.
- **Report exact memory bytes.** Go heap object headers, allocator rounding, and tree/string sharing prevent a portable exact process-memory number. Report an estimate over named cache-owned buffers, not RSS.
- **Clear the document text and derived input arrays.** That changes the document rather than only its history and conflicts with the requirement to preserve current text. Reset may discard rebuildable indexes but keeps the active input representation.

## Testing

Cover all three backends and both units. Check that cache stats distinguish linked and retired memo entries, include repetition state, remain unchanged by inspection, and return zero after reset. Check that `ResetHistory` preserves `Text`, unit/backend/depth/trace configuration, and last `ParseStats`; the next parse matches a fresh parse in tree, spans, recovered errors and ordinary errors. Retain a returned tree across reset and a later parse to verify it stops moving after reset. Exercise reset before first parse, after a large edit, after normal failure, after a resumed repetition, and after an aborted parse; check no stale memo/run/stack references remain.

## Performance and Results

Use identical documents with no edit, one small edit, many edits before a parse, and long repetitions. Measure reset time, cache accounting time, cache bytes before/after, next-parse evaluations, allocations and retained heap. Separate short and multi-megabyte text, all backends and both units. Compare an unreset document with an explicit reset and a fresh document. No performance result is asserted here.

## Limitations and Open Questions

The byte estimate excludes document text and caller-owned trees; it must not be presented as a memory cap. The initial design has no automatic reset threshold. If cache telemetry is too costly to compute by traversal, a separately measured maintained counter may be considered, with overflow and reset invariants documented. No maintainer choice is required before prototyping the proposed explicit reset and estimate.
