# 102. Read Text Struct Fields Without a Struct Lookup

- **Status**: Removed. Tokens are named node types (`type Name = node[string]`), which
  predicates compare with `text($e) == text($n)` through the unboxed comparison of
  [M120](mechanisms/M120-unboxed-text-comparisons.md), so no struct field is read.
- **Date**: 2026-10-19

## Problem and Mechanism

Terminal types were replaced by text structs, `type Name = { Text string }`, and the
repository grammars were rewritten to build them: `rule name Name = t:(...) #atomic =>
Name{Text: text($t)}`. A predicate that compared two names with `text($e) == text($n)`
now reads `$e.(Name).Text == $n.Text`. Measured against the last revision before the
migration, every workload stayed within noise except XML, whose end-tag predicate
makes that comparison once per element: recognition became 3–19% slower depending on
the backend.

A text struct already keeps its text unboxed in the node. The field read, however,
first tested whether the receiver was a node, then looked the struct type up by name
(in the program's type table, or in the generated `structFields` map) to reject an
undeclared field, before reaching the text. A text struct has exactly the field
`Text`, so a read of `Text` from a text struct needs none of that: the closure, both
VMs and generated Go Node parsers now return the text first. Under work controls the
read still takes the full route, which charges the field lookup as before, so step
counts are unchanged. Typed generated Go already read string fields directly.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Closure, bytecode and iterative bytecode | Applied | Shared `evalCtx.memberOperand` |
| Generated Go Node parsers and recognizers | Applied | `actx.memberOperand` in the runtime template |
| Generated typed Go | Not applicable | String fields are already read without a lookup |
| TypeScript | Not applied | `member` still looks the struct up in `structFields`; not measured |
| Work-controlled parses | Not applied | Keeps the charged lookup |

## Reproduction

All runs use Apple M3 Max, Go 1.27.1, `GOMAXPROCS=16`. The migration comparison builds
the `bench` test binary at `9bd2d2d` (before the migration, with its own grammars) and
at `d4fd8c0` (after it, without this change), and runs each from its own tree, since
the benchmarks read grammars at run time. Three alternating rounds of two samples each
give six samples per case:

```sh
go test -c -trimpath -o old.test ./bench   # in each tree
cd bench && ../old.test -test.run '^$' -test.benchmem -test.count 2 \
    -test.bench '^BenchmarkParse$/./^(closure|bytecode)$/^codepoints$'
```

with the same pattern for `generated/codepoints` and `generated_ast` of
`BenchmarkParse`, for the `closure`, `bytecode` and `generated` cases of
`BenchmarkRecognize`, and for all of `BenchmarkIncremental`, `BenchmarkStream` and
`BenchmarkPrepare`. The change itself is measured the same way on `BenchmarkParse/XML`
and `BenchmarkRecognize/XML`, `d4fd8c0` against the candidate. Raw results stay local.

## Results

**Migration, before this change.** Median time of `d4fd8c0` relative to `9bd2d2d`.
Bytes and allocations per operation are unchanged within 0.3% on every parse,
recognition, incremental and stream case (XML `generated_ast` bytes +1.7%).

| Workload | Parse closure | Parse bytecode | Parse generated | Recognize closure | Recognize bytecode | Recognize generated |
|:--|--:|--:|--:|--:|--:|--:|
| JSON | 0.983 | 1.000 | 1.029 | 1.019 | 1.005 | 0.989 |
| CSV | 1.016 | 1.023 | 1.017 | 1.001 | 1.008 | 0.999 |
| XML | 1.054 | 1.010 | 1.025 | 1.077 | 1.034 | **1.186** |
| Minilang | 0.981 | 1.024 | 1.023 | 0.995 | 0.985 | 0.988 |
| Recovery | 1.065 | 0.992 | 0.967 | 0.999 | 0.989 | 0.984 |
| Outline | 0.990 | 1.004 | 0.963 | 0.989 | 1.001 | 1.008 |
| Arith (Operators Rule) | 1.004 | 1.022 | 1.006 | 1.001 | 0.994 | 1.006 |
| Arith (left recursion) | 1.007 | 0.989 | 1.006 | 1.002 | 1.003 | 1.006 |

Incremental Document edits and fresh parses are 0.983–1.029, streaming 1.007–1.017.
Preparing from source takes 7–8% longer for JSON and minilang, whose rewritten
grammars have more actions to check and compile; loading `.pegoc` files is
0.94–1.08 with no consistent direction.

**This change, on XML.** Median time, `d4fd8c0` → candidate:

| Case | Before | After | Change |
|:--|--:|--:|--:|
| Recognize generated | 2.992 ms | 2.693 ms | −10.0% |
| Recognize closure | 9.587 ms | 9.241 ms | −3.6% |
| Recognize bytecode | 11.936 ms | 11.631 ms | −2.6% |
| Recognize iterative | 13.353 ms | 13.069 ms | −2.1% |
| Parse generated | 6.703 ms | 6.255 ms | −6.7% |
| Parse bytecode | 15.771 ms | 14.966 ms | −5.1% |
| Parse closure | 13.768 ms | 13.781 ms | +0.1% |

The generated XML recognizer, 2.55 ms before the migration, is within about 6% of it
again; the rest is building the `Name` struct values that the predicate reads, which
the terminal-based grammar did not need.

## Correctness and Tradeoffs

The fast path applies only to a node marked as a text struct and only to the name
`Text`, which such a struct always declares, so it cannot accept a read that the full
route rejects. Results, errors and work-control step counts are unchanged; the
equivalence suites, the generated-parser tests and the work-control accounting tests
pass.

## Decision and Limits

Adopted. Grammars that read a text struct's `Text` in a hot predicate still build the
struct; keeping such predicates on a capture of the text (`t:(...) #atomic` read with
`text($t)`) avoids that cost entirely.
