# 121. Inactive Work-Control Checks in the Generated Go Runtime

- **Status**: Implemented
- **Date**: 2026-10-25

## Problem and Mechanism

A generated parser that is used without a work control still runs the runtime helpers that can
charge one. Each such helper tests the control, and the compiler can inline it only when its body
is small. The helpers are therefore split: the test, the call of a `...Controlled` function that
holds the charging body, and the ordinary body stay in the helper; the controlled body is a
separate unexported function. This covers `appendWork`, `copyWork`, `clearWork`, `growWork`, the
typed `memoGet` and `memoPut`, `valueField`, `setValueField`, the typed and node action contexts
(`And`, `Or`, `Unary`, `Fold`, `ListItems`, `asNode`), `stringBinary`, `compareText`,
`equalExpected`, `sortExpected` and `evalErrorf`.

`FirstCall` runs the ordinary body itself after `controlledFirstCallPrepare`, which does the
controlled charges and growth, so a controlled parse executes the same steps. The operator
`apply` of both runtimes replays recovery errors only when there are some (an empty append
changes nothing), through an out-of-line function, so `apply` inlines.

In the runtime build only `appendWork`, `copyWork`, `clearWork` and the two `apply` functions
inline; the other helpers lose a call layer or a large frame but stay calls (the typed `memoPut`
costs 152 and `Parser.memoPut` 81 against a budget of 80 even without the check). Charges,
cancellation points, limits and results of controlled parses are as in the unsplit helpers.

## Results

Reference control: the same source with every work-control check deleted at build time (the ceiling,
not a supported build). Generated parsers of `parsers/json`, `duckdb`, `cel` and `golang` (Node,
recognizer and typed, generated without work controls, embedded runtime), inputs of the benchmark
suite. Ten interleaved rounds over the build before, the build after, the control and a copy of the
build before, `-benchtime=500ms`; medians as the change against the build before (the copy of the
build before stayed within ±1.2%):

| Benchmark | Before | After | Control |
|:--|--:|--:|--:|
| json node | 5.08 ms | -1.8% | -2.6% |
| json recognize | 1.80 ms | -2.4% | +1.4% |
| json typed | 3.44 ms | -2.9% | -5.6% |
| duckdb node | 70.9 ms | -2.2% | -6.5% |
| duckdb recognize | 42.7 ms | -1.8% | -6.0% |
| duckdb typed | 58.9 ms | -3.4% | -9.8% |
| cel node | 0.40 ms | -1.5% | -2.2% |
| cel recognize | 0.14 ms | -0.2% | -0.1% |
| cel typed | 0.35 ms | -1.7% | -4.2% |
| golang node | 34.5 ms | -0.8% | -2.8% |
| golang recognize | 34.1 ms | -1.3% | -3.8% |
| golang typed | 26.0 ms | -1.9% | -6.8% |

Bytes and allocations per operation are identical. The generated `parser.go` files are unchanged
(the change is in the runtime). A test binary linking all eight parsers grows from 28,715,058 to
28,775,698 bytes (+0.2%).

## Limits

Of the gap to the control, between 28% (golang typed) and 69% (json node) is recovered; on most
rows 65-72% remains. The json recognize control is slower than the build before, and cel
recognize has no gap. What remains is the checks in `Expect`, `Text`, `memoEnv`, `bind` and the
other helpers that do more than one thing, the helpers that stay calls (`memoGet`, `memoPut`,
`valueField`), and code layout. Splitting those further needs a second call that costs as much
as the check.
