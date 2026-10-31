# 036. Stream Iteration and Bounded Diagnostics

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Provide pull-based iteration for `#stream` parsers, immediate callbacks for committed recoveries, and an opt-in bounded diagnostic tail. Each `Next` advances parsing only as far as needed to yield one element or reach a terminal result. A successful element may have a nil value; the separate `ok` result distinguishes it from EOF. Without diagnostic options, `ParseStream` retains its legacy complete `SyntaxErrors` result.

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

`StreamIterator`, `WithDiagnosticCallback`, `WithDiagnosticLimit` and
`TruncatedSyntaxErrors` are implemented. A non-nil diagnostic callback is
valid only for streaming entry points and is rejected by batch parse,
`ParsePrefix` and `Document` entry points. The existing
`ParseStream(r, emit, opts...)` accepts it.

## Motivation

`Parser.ParseStream` pushes each `#stream` element to a callback. It returns recovered `SyntaxErrors` only at EOF and accumulates them until then. This is suitable for finite input but can retain unbounded diagnostics for a long faulty stream. The callback also cannot express demand except by returning an error to stop parsing. See [`Parser.ParseStream`](../../../zeroparse.go), [`ParseStreamWith`](../../../internal/engine/program_parse.go), and the stream commitment rules in [007](007-streaming-and-incremental-parsing.md).

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
- No recognition-only stream mode, generated-parser stream support (designed separately in [044](044-generated-incremental-and-stream-parsers.md)) or change to `#recover` semantics.
- No global memory budget for input; only retained diagnostics are bounded when a positive limit is selected.

## Design

### Public Behavior and Contracts

`Iterate` validates the same start rule, backend and `#stream` requirements as `ParseStream`. Existing `ParseOption`s still select unit, backend, trace and depth. `WithDiagnosticLimit(n)` accepts zero or a positive value; a negative value is invalid. The default, with neither diagnostic option set, preserves today's full-list behavior. An explicitly supplied limit, including zero, or a non-nil callback is valid only for stream entry points and is rejected by batch parse, `ParsePrefix`, and `Document` before parsing begins.

`Next` returns `(node, true, nil)` for each committed element, including `(nil, true, nil)` for an element discarded by `-`. At clean EOF it returns `(nil, false, nil)`. A parse error or recovered-error result returns `(nil, false, err)`. The terminal `(ok, err)` result is stable: repeated calls return the same `ok` and error result rather than replacing the original terminal error with a synthetic done error. `Close` is idempotent. Closing before completion abandons parser state; subsequent `Next` calls return `(nil, false, ErrStreamClosed)`. Closing after termination does not change the stored terminal result.

`WithDiagnosticCallback` is called once for each recovered syntax error when that recovery is committed. It runs immediately before the corresponding stream element is delivered; an error from the diagnostic callback aborts parsing, returns unchanged and suppresses that element. Recoveries in a successful suffix after the streaming repetition are reported before the terminal result. It is not called for speculative failures later rolled back by PEG backtracking. Each call receives an owned defensive copy of the `SyntaxError`, including independent `Expected` and `Messages` slices, so callback mutation cannot change parser state or the retained result. The callback runs synchronously from `Next` or `ParseStream` and must not call back into the same iterator. A callback panic propagates as the original panic after parser cleanup; it is not converted into a generic parse error.

A positive diagnostic limit retains a FIFO tail of at most that many committed
errors while the callback receives each committed error in order for as long as
it returns nil. If the total exceeds the limit, return
`*TruncatedSyntaxErrors` with the retained `SyntaxErrors`, total count and
dropped count. Its error text identifies truncation, and `Unwrap` exposes the
retained `SyntaxErrors` to `errors.As`. Total and dropped counters saturate at
the uint64 maximum; if another increment would overflow, `CountsSaturated` is
set. They never wrap or stop an otherwise endless stream. Saturated counts are
lower bounds, while the truncation status and retained tail remain reliable.
Thus bounded results cannot be mistaken for a complete list, including when no
callback was supplied. If the total does not exceed the limit, return ordinary
`SyntaxErrors`. A limit of one retains the latest error. An explicit zero limit
preserves the legacy complete result; without either diagnostic option, the
same legacy result is used.

The iterator and reader remain caller-owned. The iterator is single-owner and is not safe for concurrent `Next` or `Close`; closing concurrently with a blocked `Next` is not supported. `Close` does not close the reader and cannot interrupt a `Read` already blocked in caller-owned code. The caller must use and cancel/close a reader it owns if it needs that behavior. The pull adapter uses `iter.Pull`, whose runtime coroutine has an internal goroutine but no independently scheduled producer or element queue.

`Next` controls parser advancement, not how many bytes are read. PEG lookahead and the reader's buffer may read ahead. Stream positions remain absolute from the beginning after earlier input is discarded. Empty successful iterations are emitted once and then terminate the repetition before its minimum check. Per-element recovery remains supported; recovery around the streaming repetition remains invalid. Delivered values are irrevocable, as documented in [007](007-streaming-and-incremental-parsing.md).

### Implementation and Invariants

Factor the current synchronous stream parser into state that can pause at an element boundary and resume on request. `ParseStream` can drive the same state repeatedly and invoke `emit`; its no-option behavior must retain the full recovered-error list. Keep diagnostic options in the existing functional parse-option path but mark them stream-only, validating that boundary before parsing a non-stream input.

The parser reports diagnostics only at a recovery commit point, after the parser knows the recovery was not rolled back. It copies error slices before invoking user callbacks. One bounded collector serves callback and iterator modes; an explicit dropped count makes incomplete retention visible in the final wrapper. Terminal state records EOF, parse failure, callback failure, input failure or explicit close and returns the same terminal result on repeated calls. `WithContext` and `WithLimits` apply to the invocation, including callback boundaries and bounded-tail copying. If a budget check observes cancellation or a budget limit before completion, that interruption error is returned instead of a completed diagnostic result.

### Compatibility and Backend Support

This is an additive Go API for the stream-capable closure and bytecode backends; recognition remains unsupported. Both byte and rune position units are supported. TypeScript parsers remain unsupported for streams; generated Go stream packages provide the same iterator and options, as described in [design 044](044-generated-incremental-and-stream-parsers.md). `ParseStream` retains its signature, element order, nil callback values and complete recovered-error result unless the caller explicitly adds a diagnostic option. Stream-only diagnostic options are rejected for batch `Parse`, `ParsePrefix` and `Document` rather than silently ignored.

### Implemented Slices

1. `Iterate` pauses at committed element boundaries, distinguishes a nil
   element from EOF, and preserves stable terminal results and early-close
   cleanup.
2. Stream-only diagnostic options report committed recoveries before element
   delivery and optionally retain a bounded FIFO tail with explicit truncation
   metadata.
3. `ParseStream` uses the same commit boundaries while preserving the complete
   legacy result when no positive limit is selected. Reader ownership, read-ahead,
   cancellation and budget limits are documented for both stream entry points.

## Alternatives Considered

- **Callback only with a diagnostic callback.** This is a smaller extension and supports immediate delivery, but consumers cannot request the next item after processing or naturally control demand. Keep the existing callback API and add pull iteration.
- **Channel backed by a goroutine.** A channel requires scheduling, queue/backpressure policy, shutdown and handling a goroutine blocked in `Read`. A synchronous iterator avoids those ownership problems.
- **Bound `ParseStream` diagnostics by default.** This would bound memory but silently remove errors from existing `SyntaxErrors`. Preserve legacy behavior and make truncation opt-in and explicit.
- **Close the supplied reader on iterator close.** The parser did not create or own that reader; closing it could affect unrelated code. Leave cancellation to a caller-owned reader.

## Testing

Public and engine tests cover both position units and all stream-capable backends for element order, nil elements, spans, recovered and terminal errors, EOF, callback and emit failures, backtracking, positive and zero limits, invalid options, repeated `Next`, early `Close`, stable terminal results, defensive callback copies, panic propagation and `errors.As` through truncated results. Long faulty streams verify bounded retention, callback delivery of every committed recovery, dropped counts and saturating counters. Tests also cover trace preview, budget limit interruption, blocking-reader ownership, read-ahead, empty-element termination and Unicode.

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

A separate 1,000-recovery stream (4,000 input bytes) compared complete
retention with `WithDiagnosticLimit(32)`, after diagnostic-tail scratch reuse,
using five 200 ms samples per subbenchmark across Closure, bytecode VM and
iterative VM in both position units. The bounded case reduced cumulative
`B/op` by about 16,705–16,724 and `allocs/op` by one; median time ratios ranged
from 0.997 to 1.053, which does not show a robust speed change. `B/op` is
cumulative allocation per operation, not retained heap. The limit bounds the
retained diagnostic slice structurally; this benchmark did not measure heap
retention.

For the no-option path, three alternating 200 ms pairs compared main with the
candidate on the 50,001-element CSV stream. Per-profile median time ratios were
0.985–1.017 for callback delivery and 0.995–1.011 for iteration. Per-pair
ranges were wider, and allocations differed by at most one. These observations
do not establish a default-path speedup.

## Limitations and Open Questions

A synchronous API cannot interrupt an arbitrary blocked `Read`; it can only stop at parser-controlled boundaries and release parser-owned state. A positive diagnostic limit bounds parser-retained errors, but cannot bound diagnostics retained by caller callbacks. The successful start-rule trace exit exposes the same ordered latest-n tail as the terminal result, including recoveries in a successful suffix. The trace event does not carry truncation counts; use the terminal `TruncatedSyntaxErrors` for those.

## Implemented Stream Iteration and Diagnostic Controls

The implementation exposes `Parser.Iterate`, `StreamIterator`,
`ErrStreamClosed`, `WithDiagnosticCallback`, `WithDiagnosticLimit` and
`TruncatedSyntaxErrors`. An internal `iter.Pull` adapter suspends the existing
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
before the terminal result. With a positive `WithDiagnosticLimit`, committed
recoveries are copied into a bounded FIFO tail and older parser-retained stream
entries are retired. The callback sees each committed recovery in order,
immediately before its element, while it continues returning nil; a callback
error stops parsing and suppresses that element. An explicit limit of zero
preserves the complete legacy error list. Both an explicit zero and a positive
limit are stream-only; negative limits are rejected. When errors are dropped,
the terminal result is `*TruncatedSyntaxErrors`, with the retained tail,
saturating `Total` and `Dropped` counters, and `CountsSaturated` if another increment would overflow a counter. `Unwrap` exposes the retained `SyntaxErrors`.

For a positive limit, the successful start-rule `TraceExit.Recovered()`
returns the same ordered latest-n tail as the terminal result, including
recoveries from a successful suffix. Truncation counts are available only from
the terminal `TruncatedSyntaxErrors`. The
public and engine tests cover both position units and all stream-capable
backends, including ordering, bounded retention, callback errors, truncation,
trace preview and budget limit interruption. The iterator delivery and bounded-tail measurements are reported above; they
show the tested allocation and timing ranges, not retained-heap or broad
workload performance.
