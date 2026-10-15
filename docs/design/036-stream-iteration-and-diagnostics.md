# 036. Stream Iteration and Bounded Diagnostics

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add pull-based iteration for `#stream` parsers and an opt-in bounded diagnostic tail with a callback for committed recoveries. Each `Next` advances parsing only as far as needed to yield one element or reach a terminal result. A successful element may have a nil value; the separate `ok` result distinguishes it from EOF. Existing `ParseStream` behavior remains unchanged when called without diagnostic options.

```go
type StreamIterator struct { /* opaque */ }
func WithDiagnosticLimit(n int) ParseOption
func WithDiagnosticCallback(func(*SyntaxError) error) ParseOption
func (p *Parser) Iterate(r io.Reader, opts ...ParseOption) (*StreamIterator, error)
func (it *StreamIterator) Next() (node *Node, ok bool, err error)
func (it *StreamIterator) Close()

type TruncatedSyntaxErrors struct {
    Errors SyntaxErrors
    Total, Dropped uint64
    CountsSaturated bool
}
func (e *TruncatedSyntaxErrors) Unwrap() error
```

`StreamIterator` and `WithDiagnosticCallback` are implemented; `WithDiagnosticLimit`
and `TruncatedSyntaxErrors` remain proposed. A non-nil diagnostic callback is
valid only for streaming entry points and is rejected by batch parse and
`Document` entry points. The existing `ParseStream(r, emit, opts...)` accepts it.

## Motivation

`Parser.ParseStream` pushes each `#stream` element to a callback. It returns recovered `SyntaxErrors` only at EOF and accumulates them until then. This is suitable for finite input but can retain unbounded diagnostics for a long faulty stream. The callback also cannot express demand except by returning an error to stop parsing. See [`Parser.ParseStream`](../../pego.go), [`ParseStreamWith`](../../internal/engine/parse.go), and the stream commitment rules in [007](007-streaming-and-incremental-parsing.md).

An `io.Reader` may block inside `Read`. Parser state cannot cancel a read in caller-owned code. The API must make that ownership limit explicit instead of creating a hidden producer goroutine or closing a reader it did not create.

## Goals

- Let callers request one stream element at a time without an independently scheduled producer or unbounded element queue.
- Distinguish EOF from a successful nil-valued element.
- Deliver committed recovered diagnostics promptly and optionally bound the retained tail.
- Preserve existing parse options, stream commitment and terminal errors for callers that do not opt into diagnostic controls.
- Specify reader and iterator ownership, early stop and blocking-read limits.

## Non-Goals

- No asynchronous channel, background reader, forced read cancellation or automatic closing of an arbitrary `io.Reader`.
- No rollback of elements already returned or emitted.
- No recognition-only stream mode, generated-parser stream support or change to `#recover` semantics.
- No global memory budget for input; only retained diagnostics are bounded when a positive limit is selected.

## Design

### Public Behavior and Contracts

`Iterate` validates the same start rule, backend and `#stream` requirements as `ParseStream`. Existing `ParseOption`s still select unit, backend, trace and depth. `WithDiagnosticLimit(0)` means retain all errors; a negative value is invalid. The default, with neither diagnostic option set, preserves today's full-list behavior. Supplying either diagnostic option to non-stream `Parse` or `Document` is an invalid configuration and returns an error before parsing begins.

`Next` returns `(node, true, nil)` for each committed element, including `(nil, true, nil)` for an element discarded by `-`. At clean EOF it returns `(nil, false, nil)`. A parse error or recovered-error result returns `(nil, false, err)`. The terminal `(ok, err)` result is stable: repeated calls return the same `ok` and error result rather than replacing the original terminal error with a synthetic done error. `Close` is idempotent. Closing before completion abandons parser state; subsequent `Next` calls return `(nil, false, ErrStreamClosed)`. Closing after termination does not change the stored terminal result.

`WithDiagnosticCallback` is called once for each recovered syntax error when that recovery is committed. It runs immediately before the corresponding stream element is delivered; an error from the diagnostic callback aborts parsing, returns unchanged and suppresses that element. Recoveries in a successful suffix after the streaming repetition are reported before the terminal result. It is not called for speculative failures later rolled back by PEG backtracking. Each call receives an owned defensive copy of the `SyntaxError`, including independent `Expected` and `Messages` slices, so callback mutation cannot change parser state or the retained result. The callback runs synchronously from `Next` or `ParseStream` and must not call back into the same iterator. A callback panic propagates as the original panic after parser cleanup; it is not converted into a generic parse error.

A positive diagnostic limit retains a FIFO tail of at most that many committed errors while the callback still receives all committed errors in order. If the total exceeds the limit, return a proposed `TruncatedSyntaxErrors` wrapper with the retained `SyntaxErrors`, total count and dropped count. Its error text must identify truncation, and Unwrap exposes the retained SyntaxErrors to errors.As. Total and dropped counters saturate at the uint64 maximum and set CountsSaturated; they never wrap or stop an otherwise endless stream. Saturated counts are lower bounds, while the truncation status and retained tail remain reliable. Thus bounded results cannot be mistaken for a complete list, including when no callback was supplied. If the total does not exceed the limit, return ordinary `SyntaxErrors`. A limit of one retains the latest error. No limit option preserves the legacy complete result.

The iterator and reader remain caller-owned. The iterator is single-owner and is not safe for concurrent `Next` or `Close`; closing concurrently with a blocked `Next` is not supported. `Close` does not close the reader and cannot interrupt a `Read` already blocked in caller-owned code. The caller must use and cancel/close a reader it owns if it needs that behavior. The pull adapter uses `iter.Pull`, whose runtime coroutine has an internal goroutine but no independently scheduled producer or element queue.

`Next` controls parser advancement, not how many bytes are read. PEG lookahead and the reader's buffer may read ahead. Stream positions remain absolute from the beginning after earlier input is discarded. Empty successful iterations are emitted once and then terminate the repetition before its minimum check. Per-element recovery remains supported; recovery around the streaming repetition remains invalid. Delivered values are irrevocable, as documented in [007](007-streaming-and-incremental-parsing.md).

### Implementation and Invariants

Factor the current synchronous stream parser into state that can pause at an element boundary and resume on request. `ParseStream` can drive the same state repeatedly and invoke `emit`; its no-option behavior must retain the full recovered-error list. Keep diagnostic options in the existing functional parse-option path but mark them stream-only, validating that boundary before parsing a non-stream input.

Drain diagnostics only at a recovery commit point, after the parser knows the recovery was not rolled back. Copy error slices before invoking user callbacks. Use one bounded collector for callback and iterator modes; preserve an explicit dropped count so the final wrapper accurately describes incomplete retention. Terminal state records EOF, parse failure, callback failure, input failure or explicit close and returns the same terminal result on repeated calls.

### Compatibility and Backend Support

This is an additive Go API for the stream-capable closure and bytecode backends; recognition remains unsupported. Both byte and rune position units are supported. Generated Go, typed Go and TypeScript parsers remain unsupported for streams. `ParseStream` retains its signature, element order, nil callback values and complete recovered-error result unless the caller explicitly adds a diagnostic option. Stream-only diagnostic options are rejected for batch `Parse` and `Document` rather than silently ignored.

### Implementation Sequence

1. Extract resumable stream state and preserve `ParseStream` output and terminal errors.
2. Add `Iterate` with explicit EOF/nil semantics, idempotent terminal results and early close.
3. Add stream-only functional parse options for committed diagnostic callbacks and bounded-tail retention; use them through `ParseStream` and `Iterate`.
4. Document reader ownership, read-ahead, finite-input compatibility and endless-stream use.

## Alternatives Considered

- **Callback only with a diagnostic callback.** This is a smaller extension and supports immediate delivery, but consumers cannot request the next item after processing or naturally control demand. Keep the existing callback API and add pull iteration.
- **Channel backed by a goroutine.** A channel requires scheduling, queue/backpressure policy, shutdown and handling a goroutine blocked in `Read`. A synchronous iterator avoids those ownership problems.
- **Bound `ParseStream` diagnostics by default.** This would bound memory but silently remove errors from existing `SyntaxErrors`. Preserve legacy behavior and make truncation opt-in and explicit.
- **Close the supplied reader on iterator close.** The parser did not create or own that reader; closing it could affect unrelated code. Leave cancellation to a caller-owned reader.

## Testing

For both units and every stream-capable backend, compare `ParseStream` and iterator output for value order, nil elements, spans, recovered errors and terminal errors. Cover clean EOF, malformed final input, callback and emit failures, callback order across backtracking, positive limits, zero-limit compatibility, invalid limits, rejected stream options in batch and `Document`, repeated `Next`, early `Close`, stable terminal errors and stable prior elements. Verify defensive callback copies, unchanged callback errors, original panic propagation and `errors.As` through truncated diagnostics. Test a long faulty stream with many recoveries: retained diagnostics stay bounded, the callback sees all of them and the final wrapper reports the dropped count. Use a blocking reader to document that `Close` alone does not cancel `Read`; test cancellation only through a caller-owned closable reader. Include lookahead read-ahead, empty-element termination, Unicode and byte/rune positions.

## Performance and Results

On Go 1.27.1, Apple M3 Max (darwin/arm64), `GOMAXPROCS=4`, `GOGC=100`,
and `GOMEMLIMIT` off, a 50,001-element CSV stream was measured for five
500 ms samples across Closure, bytecode VM and iterative VM, each with
CodePoints and Bytes. The pull iterator's full-drain median time was
1.033–1.090× the existing callback path, with 8–10 additional allocations
and 328–552 additional bytes per complete run. The existing callback path was
unchanged. A first-element metric ranged from 1.08–1.47×, but its short
per-sample timing was noisy and does not support a firm latency conclusion.
These measurements compare the two delivery methods on the tested stream
workload; they do not measure retained memory or establish behavior for other
workloads.

## Limitations and Open Questions

A synchronous API cannot interrupt an arbitrary blocked `Read`; it can only stop at parser-controlled boundaries and release parser-owned state. A positive diagnostic limit returns an explicitly truncated error result; applications requiring all errors must process the callback or leave the limit at zero. The bounded collector and its interaction with nested recovery and backtracking still need prototype validation before those signatures are finalized.

## Implemented Pull Iteration and Diagnostic Callback

The implemented slice exposes `Parser.Iterate`, `StreamIterator` and
`ErrStreamClosed`. An internal `iter.Pull` adapter suspends the existing
`ParseStreamWith` execution at each committed element, preserving closure and
VM stream behavior. Construction validates the stream entry point without
reading the caller-owned reader. `Next` distinguishes successful nil elements
from EOF, and terminal errors remain stable. Early `Close` unwinds parser
state through the existing callback-error cleanup path without closing the
reader. Calls to `Next` and `Close` must not overlap.

The diagnostic callback is implemented as a stream-only parse option. At each
element boundary, it reports recoveries surviving backtracking before the
element callback runs; its error suppresses that element. It copies the error
and its slices before invoking caller code. Batch parse and Document reject a
non-nil callback. A successful stream suffix reports any remaining recoveries
before the terminal result. The bounded tail and truncation wrapper remain
proposals, so the stream still retains its full recovered-error list until EOF.
The public and engine iterator tests cover all stream-capable
backends and both position units, lazy reader access, EOF and error stability,
nil elements, early close and caller-owned cancellation. A public comparison
benchmark compares stream callback delivery, pull delivery and first-element
latency under the conditions reported above.
