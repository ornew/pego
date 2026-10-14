# 035. Incremental Cache Controls

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

The API gives a `Document` owner an explicit way to discard incremental reuse history while keeping its current text and parse configuration. Cache accounting is separate from the last parse's evaluation statistics, and resumed repetition elements are reported independently from memo hits. Reset is manual and deterministic; it is not an automatic memory budget.

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

The API is implemented. `ParseStats.Reused` retains its combined meaning: it counts memo-result reuse and elements reused by repetition resumption. `MemoHits` and `ResumedElements` expose those sources separately, so existing callers keep the same `Reused` result.

## Motivation

`Document` incrementally reuses memo results and repetition runs. Memo entries can retire into an internal free list while their slabs remain allocated, and the document retains repetition records and VM scratch between parses. Before this API, callers had no public reset or cache-size view, and `Stats.Reused` combined memo hits with repetition resumption. See [`Document`](../../internal/engine/document.go), [`memoTable`](../../internal/engine/memo.go), [`resume.go`](../../internal/engine/resume.go), and the public [`Document` API](../../pego.go).

The implemented API provides observability and an explicit lifecycle control. It does not impose an automatic memory cap or claim that reset improves parse speed.

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

`ResetHistory` clears the document's memo table, retired memo-entry pool, repetition-run records, edit log used to validate reuse, reusable parser stacks, and the weak snapshot-sharing candidate. It also drops derived offset and line tables so they can be rebuilt lazily. It preserves the current text and the document's construction settings. It does not parse, reset `Stats`, or return an error. The next `Parse` is a fresh parse of the current text; later edits and parses resume incremental reuse from that new baseline.

Calling `ResetHistory` detaches memo-owned trees from future reuse. A tree returned before reset remains readable and is no longer moved by later document parses. It may already reflect edits and node moves that happened before reset; the method does not restore an earlier version. As elsewhere, do not read or edit the document concurrently with `Parse`, `Edit`, `ResetHistory`, or cache inspection.

`CacheStats` is a point-in-time estimate for the current reuse cache. `LiveMemoEntries` counts linked entries; `RetiredMemoEntries` counts reusable retired entries. `RepetitionElements` counts elements currently retained in resumable runs. `EstimatedBytes` estimates cache-owned backing storage (memo slots/slabs/seen bits, repetition records, and reusable parser stacks), excluding the current document text/input representation, allocator metadata, caller-held trees and strings shared with those trees. It is an estimate, not a promise about process RSS or bytes immediately returned to the operating system. After `ResetHistory`, all four fields report zero for reuse history.

`ParseStats.MemoHits` counts memo-result reuse and `ParseStats.ResumedElements` counts elements reused through repetition resumption in the most recent `Parse`. `ParseStats.Reused` remains their combined count, preserving its current behavior. A no-edit parse can report reuse from either or both sources. Resetting history leaves the most recent parse statistics intact until another parse completes.

### Implementation and Invariants

Reset by replacing the memo table and clearing document references to repetition runs, edits, parse-local children and reusable VM values. Do not call the memo table's ordinary `reset` when the purpose is to release capacity: it deliberately keeps slabs and backing arrays. Preserve `input.src`, decoded bytes/runes and input unit; clear only derived indexes that can be rebuilt. No active parse may observe a partial reset.

Cache accounting must traverse linked memo slots and the free list without changing them, and count only run records still owned by the document. Estimate capacities from actual backing arrays/slices; document which fixed headers and shared allocations are omitted. Keep cache stats read-only and avoid allocating a copy of the memo or run tables to produce them.

The edit log can be cleared because the memo and runs it relates to are discarded together. New parse results start at the reset generation. Caller-held old nodes are not registered in the new memo, so the parser has no reason to move them again.

### Compatibility and Backend Support

This is an additive Go API on `Document`, available with Closure, recursive Bytecode and BytecodeIterative wherever the current document can be created. `ResetHistory` preserves the selected unit/backend and trace settings. Generated parsers and TypeScript have no `Document` API and are unaffected. Existing `Stats` fields and meanings remain stable; adding a field is source-compatible for keyed literals but may affect unkeyed `ParseStats` literals, which should be called out in release notes.

### Implementation Summary

`ParseStats` derives memo-hit and resumed-element counts at parse completion while preserving the combined `Reused` count. This avoids adding counter increments to each parse operation. Cache accounting is read-only and estimates owned backing capacities.

## Alternatives Considered

- **`ClearCache` that clears only current memo entries.** It sounds simple but leaves retired slabs, repetition records and edit history resident. `ResetHistory` describes the complete reuse boundary.
- **An automatic byte budget.** It would need eviction timing, accounting semantics, a default threshold and decisions about which caches to retain. Start with explicit reset and measurements; callers can choose when to invoke it.
- **Expose only `Stats`.** Last-parse counters do not describe current retained capacity, and a combined reuse count obscures repetition-resumed work. Keep parse activity and cache state separate.
- **Report exact memory bytes.** Go heap object headers, allocator rounding, and tree/string sharing prevent a portable exact process-memory number. Report an estimate over named cache-owned buffers, not RSS.
- **Clear the document text and derived input arrays.** That changes the document rather than only its history and conflicts with the requirement to preserve current text. Reset may discard rebuildable indexes but keeps the active input representation.

## Performance and Results

The reparse comparison below covers two profiles. A separate benchmark measured cache inspection and reset latency across the listed profiles; retained heap remains unmeasured.

On Go 1.27.1, Apple M3 Max (darwin/arm64), `GOMAXPROCS=16`, `GOGC=100`, with `GOMEMLIMIT` off, `BenchmarkDocumentReparse` edited one character and parsed a 100k-line document. Five alternating candidate/base pairs used 300 ms per sample. In Closure/CodePoints, the paired candidate/base median time ratio was 0.9965 (range 0.9626–1.0156), with B/op unchanged at 844,279 and 21 allocations/op. In Iterative VM/Bytes, the median ratio was 0.9929 (range 0.9736–0.9973), B/op changed from 853,524 to 853,525, and allocations stayed at 32/op. These two profiles show no material timing regression; other paths are unmeasured.

The following separate measurements used Go 1.27.1 on an Apple M3 Max,
`GOMAXPROCS=4`, `GOGC=100`, and `GOMEMLIMIT` off. Each benchmark ran for
500 ms with five samples; the CSV stream contained 50,001 elements. Across
Closure, bytecode VM and iterative VM with both CodePoints and Bytes,
`CacheStats` took 0.355–0.402 ms/op, allocated 0 B/op, and reported an
estimated cache size of about 20.6 MB. The per-profile median
`ResetAndParse`/`FreshDocument` time ratios ranged from 0.975 to 1.008, with no
material difference indicated by this measurement. This compares two methods,
not implementation revisions; retained heap
and peak memory were not measured.

## Limitations and Open Questions

The byte estimate excludes document text and caller-owned trees; it must not be presented as a memory cap. There is no automatic reset threshold. If cache telemetry becomes too costly to compute by traversal, a maintained counter would need separate measurement and documented overflow/reset invariants.

## Implemented Scope

The engine and public API provide `Document.ResetHistory` and `Document.CacheStats`
without changing text, construction settings, document version, or last-parse
statistics. Engine parse statistics now expose memo hits and resumed elements
separately while preserving their combined legacy `Reused` count. Reset replaces
the memo table rather than recycling its slabs and releases the edit log,
repetition records, snapshot-sharing candidate, reusable value/child buffers,
and rebuildable flat-input indexes. Caller-held old trees are detached from future
reuse; previously applied movement is preserved.

The estimate uses backing capacities for memo slots, slab directories/slabs,
dense and sparse seen buffers, seen-call counters, repetition element buffers,
the edit log, and cached child/value stacks. It includes repetition record
headers. It excludes map bucket storage, shared expectation/error/environment
payloads, tree storage, source text, allocator metadata, and the empty memo
header retained after reset. These exclusions avoid treating shared slices as
separately owned allocations. Counts and capacity inspection allocate no copies.

Focused test cases cover reset before parsing, read-only accounting, live versus
retired entries, all engine backends and position units, construction option and
last-stat preservation, fresh-parse equivalence after a large edit and after
repetition resumption, recovered and ordinary syntax errors, prior-tree
ownership, and the public reuse-counter invariant
`Reused == MemoHits + ResumedElements`.
