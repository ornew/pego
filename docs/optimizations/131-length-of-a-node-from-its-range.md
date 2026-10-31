# 131. Length of a Node from Its Range

- **Status**: Implemented
- **Date**: 2026-10-31

## Problem and Mechanism

`len` of a string node counted the characters of its matched text. Under
code-point positions, and under byte positions in TypeScript, whose strings are
UTF-16, that takes time in proportion to the text, and with
budget controls it spends value budget per character. Grammars that take the
length of an indentation or another capture in a predicate, as the YAML and
outline grammars do, paid it at every line.

The matched text of a string node is always the input of its range: a literal
node holds the input it matched, a missing node has an empty text and an empty
range, an error node holds the skipped input, and conversions keep both. The
range is already in the position unit, so `len` of a string node returns the
end of the range minus its start, in constant time and without budget. String
values keep their representation, and `len` of a string value still counts its
characters. Nodes of a Document keep their range lengths across edits, since
an edit shifts both ends of a reused node.

The complexity classifier follows: it counts `len` as a copying operation only
when the type of the operand, from the checker, may be a string value.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Closure, recursive VM, iterative VM | Applied | One shared built-in |
| Generated Go Node and recognition | Applied | Runtime `Length` |
| Generated typed Go | Applied | Typed runtime `Length` |
| Generated TypeScript | Applied | Runtime `length` |
| Bytes position unit | Applied | Already constant time in Go; TypeScript no longer counts UTF-8 bytes |

## Reproduction

Baseline 284e779a, candidate the commit of this record; Go 1.27.1,
darwin/arm64, Apple M3 Max. Test binaries of both revisions were built with
`-trimpath` and run alternately, ten rounds each, one count per round, under
the benchmark lock:

```sh
cd bench
./bench.test -test.run '^$' -test.benchmem -test.count 1 \
  -test.bench 'BenchmarkParse/Outline/(closure|generated)/codepoints'
```

The focused benchmark parses 2,000 lines whose indentation runs from 0 to 63
spaces with `rule line = s:" "* #atomic {len($s) >= 0} [a-z]* "\n"` on the
closure backend in both units. Results were compared with `benchstat`.

## Results

Medians of ten interleaved runs; `±` is the benchstat confidence interval of
the baseline and the candidate, and every listed change has p=0.000. Each round
started with the one-minute load average below 8, and the load stayed between
5.5 and 7.5. The bytes control of the focused benchmark, which the change does
not affect, shows no difference.

| Workload | Baseline | Candidate | Change | Uncertainty |
|:--|--:|--:|--:|:--|
| Outline, closure, code points | 6.839 ms | 6.443 ms | −5.8% | ±4%, ±2% |
| Outline, generated Go, code points | 2.825 ms | 2.662 ms | −5.8% | ±2%, ±1% |
| Focused, closure, code points | 1.810 ms | 1.604 ms | −11.4% | ±2%, ±7% |
| Focused, closure, bytes | 1.466 ms | 1.468 ms | ~ (p=0.971) | ±7%, ±7% |

Allocations and bytes per operation stay within 0.05%. The generated code does not
change either: the length is taken in the runtimes, and regenerating the
parsers and benchmarks gives no difference.

## Correctness and Tradeoffs

A corpus case takes `len` of string, `node[T]`, list, tuple and optional
nodes, of a node's `Value` and of `text`, with BMP, astral and empty inputs, in
both units; every engine, the generated Go parsers, typed and untyped, and the
TypeScript parsers agree with fixed expected values, and Documents after edits
agree with fresh parses. Budget totals drop where grammars take `len` of
nodes, and with them the context-poll counts of some of them, since a parse
polls its context once per fixed amount of budget; no total or count grows.

The classifier needs the types of the grammar, which come from compiling it
without building matchers. Compiling costs about as much as classifying, so
the classifier compiles only when a `len` without types counted an operand of
unbounded size, and keeps the untyped classification when the grammar does
not compile. The classes of the corpus grammars do not change; the Python
grammar loses 18 causes, all `len` of list captures.

## Decision and Limits

Adopted on every backend. `len` of a string value is unchanged: under code
points it remains proportional to the string.
