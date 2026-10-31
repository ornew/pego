# 129. Expected-Token Sets Are Recorded on Request

- **Status**: Implemented; no measurable change in parse time, smaller generated code
- **Date**: 2026-10-31

## Problem and Mechanism

Every failed match of a literal, class or predicate recorded what the parser expected at the
farthest failure position, in the closure and bytecode engines and in generated Go and TypeScript.
Most callers read only the position of the error, yet each parse paid for the record, for the
descriptor table of the generated parser, and for carrying the record through memo entries,
repetition resume points and Document results.

Recording is now a choice made by the caller. Parsing records nothing by default and reports the
farthest failure position with an empty expected list. The `WithExpectations` parse option (and the
`Expectations` field of `ParseOptions`) turns it on for the interpreters. A generated parser needs
both steps: the `Expectations` generation option of `gen.Go` and `gen.TypeScript` (the
`-expectations` flag of `zp gen`) gives it the descriptor table and a parse option
(`WithExpectations` in Go, `expectations` in TypeScript), and a parse records only when it passes
that option. A Document fixes the choice when it is created. `@error` messages are part of the
record, so they follow the same choice.

When recording is off, a failure still advances the farthest position and keeps a single placeholder
in the record instead of identifiers. The placeholder lets memoized results, resumed repetitions,
Documents and the merge of isolated records carry the farthest position exactly as before, so error
positions, recovered trees and incremental results are the same in both modes; only the text of the
expected list and of `@error` messages disappears. A generated parser without the flag has no
descriptor table.

## Results

Parse time, medians of twelve interleaved rounds against the previous commit (benchstat; no
difference is significant at p < 0.05). The machine load average was 4 to 11 during the run, above
the usual limit of 8 in the first rounds. The inputs have no syntax errors, so they exercise the
cost of the record on the failures inside alternations.

| Workload | Base | Default mode | Recording on |
|:--|--:|--:|--:|
| JSON closure, bytes | 17.46 ms | 17.23 ms (~) | 17.42 ms (~) |
| JSON bytecode, bytes | 21.32 ms | 20.45 ms (~) | 21.23 ms (~) |
| JSON generated, bytes | 7.41 ms | 7.31 ms (~) | 7.48 ms (~) |
| JSON geomean of ten benchmarks | 12.23 ms | 11.93 ms (-2.4%, ~) | 12.21 ms (-0.2%, ~) |
| Go parser (`ParseAST`) | 29.35 ms | 28.45 ms (~) | 29.63 ms (~) |

The Go parser allocates 4.7% fewer bytes per parse (5.766 to 5.495 MiB, p < 0.001) and 1.2% fewer
objects in the default mode. The difference with recording on is within noise of the base.

Generated size (bytes of `parser.go`):

| Parser | Base | Default mode | Recording on |
|:--|--:|--:|--:|
| JSON (benchmark) | 58762 | 58381 | 58795 |
| Go | 524159 | 516868 | 524192 |
| DuckDB | 1588057 | 1586491 | 1586491 |

The saving is the descriptor table; call sites still pass identifiers, which the default-mode
runtime ignores.

## Cost

`(*Parser).ExpectOrdinary` serves the rule bodies of parses without budget controls and without a
Document. It takes the placeholder path of a parse that does not record through the body that
records, which raises the cost of the whole body to 84 against the inlining budget of 80. The
early return for a silent position or one before the farthest failure is therefore an inlinable
guard, and the recording sits in `expectOrdinarySlow`, which is not inlined. The method stays in
the hot-path inlining test.

Measured on the Go parser (`ParseAST` of `parsers/golang`, which is the row that runs these
rule bodies), 12 interleaved runs per variant, at a machine load average that fell from 53 to 24
during the run, so differences of a few percent are noise:

| ParseAST | Base | Without the guard | With the guard |
|:--|--:|--:|--:|
| Default (no recording), ms/op | 28.27 | 27.58 (~) | 27.31 (-3.4%, p=0.03) |
| With `WithExpectations()`, ms/op | 28.27 | 28.79 (~) | 28.73 (~) |

The guard against the version without it shows no difference (p=0.38 default, p=0.93 recording).
B/op falls by 5.2% and allocs by 1.2% in the default mode. The JSON benchmark rows above ran
before the guard was restored; they do not run these rule bodies for their closure and bytecode
rows, and the generated rows show no difference.

The ready-made parsers whose grammars define `@error` messages (Go, Python, TypeScript, XML) are
generated with expectations, because their messages are part of what they report. Their
compatibility entry points that print messages (`ParseFile`, `ParseModule`, `Decode`) pass the parse
option themselves; `ParseAST` records only when the caller passes it. The others are generated
without.
