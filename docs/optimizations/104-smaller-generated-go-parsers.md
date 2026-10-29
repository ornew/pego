# 104. Smaller Generated Go Parsers

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

A generated Go function was emitted twice: a specialization for parses without a
work control, and the full body that charges steps and input. The charged copy was
about half of every generated parser, though only parsers that cancel or bound
work run it. Inside the ordinary copy, three forms repeated at every site: the
node literal of a match or sequence, the five-line read of the next character,
and the clauses of the removed control in class tests. Together they made the
`duckdb` parser 35 MB and its test binary 70 MB.

Three changes shrink the output without changing what a parse does.

1. **Work controls on request.** The generation flag `-work-controls`
   (`WorkControls` in `gen.Go`) emits the work-controlled form. Without it the
   ordinary specialization replaces each body, so a function exists once; with
   `-document` the function is the form that serves a Document, with no charge
   and no control read (the typed constructors and the closures of actions
   charge nothing either), which is 7% smaller than the controlled form (a
   fixture of 3 rules: 41,179 bytes with controls, 38,138 without). The
   package then exports none of `WithContext`, `WithWorkLimits` and their types,
   and the runtime grammar records `NoWorkControls`, so a control option passed
   through the runtime returns an error instead of being ignored. Parsers that
   read untrusted input (JSON, YAML, XML, CSV) and the benchmark parsers keep
   the flag.
2. **Runtime helpers for the ordinary forms.** `NewTerminal` and `NewBranch`
   build the nodes of matches and sequences; `NextOrdinary` reads the next
   character with the code-point fast path. The ordinary specialization rewrites
   the open-coded read to `NextOrdinary`. `NewTerminalOrdinary`,
   `NewBranchOrdinary` and `NextOrdinary` inline (compiler costs 58, 56 and 79,
   budget 80; named results keep `NextOrdinary` under the budget). The
   controlled forms `NewTerminal` and `NewBranch` do not inline, as `NewNode` did
   not.
3. **Folded conditions.** The comparisons of the removed control with nil fold,
   and so do the boolean operators around them when the other operand has no
   effects; blank lines left by removed charges are dropped.

Invariant: the ordinary form builds the same nodes in the same order, and a
parser generated with work controls behaves as before.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go Node, recognition and typed output | Applied | Same generator |
| Generated Go with `-work-controls` | Partial | One controlled form; helpers 2 and 3 apply |
| Generated Go with `-document`, without `-work-controls` | Partial | One form that serves a Document, without charges and control reads |
| Closure, bytecode and iterative VMs | Not applicable | No generated source |
| Generated TypeScript | Not applied | Separate generator and runtime |

## Reproduction

Baseline origin/main 1336583b (which contains 5b07c199's generator); candidate
this branch, Apple M3 Max, Go 1.27.1, `-trimpath`. Sizes are `wc -lc` of
`parser.go`. Compile times are a cold `GOCACHE`
`go test -trimpath -c` of `parsers/duckdb`. Benchmarks are test binaries built per
tree and run alternately, six rounds at `-benchtime=200ms` with `-benchmem`, then
`benchstat`: `^Benchmark(Parse|Recognize|Incremental|IncrementalLong|Stream|ControlledParse)$/./generated`
of `./bench` and the parser module benchmarks (`Parse`, `ParseAST`,
`Recognize`, `Statements`, `Policies*`, `ParseModule`, `ParseFile`). Runs were
serialized with the shared lock.

## Results

| Parser | Baseline | Work controls on request | With helpers |
|:--|--:|--:|--:|
| duckdb | 35.40 MB, 1,648,359 lines | 17.10 MB, 737,985 | 13.88 MB, 614,553 |
| typescript | 12.36 MB, 553,302 | 6.12 MB, 255,948 | 5.60 MB, 246,648 |
| python | 7.88 MB, 327,650 | 4.00 MB, 152,836 | 3.68 MB, 148,052 |
| golang | 7.58 MB, 334,381 | 3.75 MB, 154,245 | 3.39 MB, 148,193 |
| cue | 5.32 MB, 221,875 | 2.63 MB, 103,389 | 2.41 MB, 99,365 |
| yaml (keeps controls) | 2.58 MB, 122,017 | 2.53 MB, 116,213 | 2.40 MB, 113,865 |
| cel | 1.49 MB, 69,472 | 0.73 MB, 32,506 | 0.65 MB, 30,378 |
| xml (keeps controls) | 1.03 MB, 48,104 | 1.01 MB, 45,650 | 0.96 MB, 44,782 |
| json (keeps controls) | 0.26 MB, 12,643 | 0.26 MB, 12,081 | 0.24 MB, 11,745 |
| csv (keeps controls) | 0.09 MB, 4,436 | 0.09 MB, 4,246 | 0.08 MB, 4,126 |
| Total of the ten | 74.79 MB | 37.13 MB | 33.29 MB |

| Measurement | Baseline | Candidate |
|:--|--:|--:|
| Cold compile of the duckdb test binary, wall | 42 s | 16 s |
| Same, CPU | 161 s | 75 s |
| duckdb test binary | 69.7 MB | 42.4 MB |

| Benchmark | Change | Uncertainty |
|:--|--:|:--|
| `./bench` generated, geomean, work controls on request only | −0.35% | six rounds, no row above +2.7% |
| `./bench` generated, geomean, with helpers | −1.6% | Recognize XML −9.9%, Parse XML ast −6.7% |
| duckdb Statements and Parse (all rows) | −2% to −8% | p ≤ 0.04, six rounds |
| cue ParseAST | −3.4% to −3.9% | p = 0.04 |
| cel PoliciesRecognize | −4.6% | p = 0.03 |
| json and csv module benchmarks | no change | ten rounds, p ≥ 0.2 |

## Correctness and Tradeoffs

The root suite (the differential tests of every backend against generated code,
the generation checks and the work-control tests), the tests of each parser module
and the up-to-date check of the generated files pass. A new test generates a
parser without work controls, checks that no charge, control read or work type is
left (with and without `-document`), that it is smaller than the controlled
parser, that it compiles and that each entry point (`Parse`, `Recognize`, `ParseAST`,
`ParseRule`) rejects both control options. A second test builds a parser with every
entry point, a Document, a stream, left recursion and recovery both ways and
compares what the entry points return for inputs that include invalid UTF-8, byte
positions, error positions and a depth limit; a parser of a mutated grammar shows
that the comparison can fail. The engine tests can also run with their fixtures
generated without work controls (`PEGO_TEST_ORDINARY=1`); the tests that pass a
control skip.

In functions the compiler considers very large, calls to the inlinable helpers
stay calls (about 45% of the `NextOrdinary` sites of `duckdb`); the benchmarks
above include those parsers and show no loss, since the code that no longer
spells the forms out is smaller to fetch.

The generated work-control tests keep their coverage because the test fixtures
generate with the flag unless `PEGO_TEST_ORDINARY=1` is set.

## Decision and Limits

Adopted. TypeScript output has its own copies of the work-control charge, and
`-work-controls` selects its form independently: the Go output of this entry is
not affected.
