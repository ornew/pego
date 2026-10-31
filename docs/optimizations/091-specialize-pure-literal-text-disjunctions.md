# 91. Specialize Pure Literal-Text Disjunctions

**Status:** Implemented.

When a generated predicate is a complete disjunction of two or more text
comparisons against string literals, the generator can emit a switch over the
captured text. Every branch must compare the same direct capture reference to
one string literal; either operand order is accepted. The capture text is
computed once, duplicate literals collapse to one case, and the switch returns
true for a matching literal and false otherwise.

The specialization applies to generated Go Node and typed outputs and generated
TypeScript. It is selected during generation through the private
`disableLiteralTextDispatch` reference control. Generated parsers contain no
runtime optimization selector or public option. Other predicate shapes retain
general expression evaluation. Mixed expressions, different captures,
bindings, and alias-based comparisons do not qualify, preserving the
original evaluation order and side effects.

## Alternatives

Rewriting arbitrary `||` trees as switches could change short-circuit order or
skip side effects. The generator therefore recognizes only the complete,
side-effect-free capture-to-literal shape and leaves all other expressions on
the general path. Literal-table sharing is a separate mechanism; this change
specializes predicate dispatch.

## Validation and Reproduction

`TestLiteralTextPredicateDispatchGeneration` checks the emitted Go and
TypeScript switches and generation-only reference outputs.
`TestLiteralTextPredicateDispatchRejectsUnsafeShapes` verifies that mixed,
aliased, bound, and different-capture forms remain general.
`TestLiteralTextPredicateDispatchDisabledParity` compares generated Go Node,
typed AST and recognition results against the engine in both input units.
`TestLiteralTextPredicateDispatchTSDisabledParity` checks empty and Unicode
literals, duplicate collapse, both input units and reference parity; it runs
strict `tsc` when available. The generated Go and TypeScript fixtures also
match their generation-only controls byte for byte where the workload does not
exercise the specialization.

Run the focused checks with:

```sh
go test ./internal/gen/... -run '^TestLiteralTextPredicateDispatch' -count=1
```

Focused generated-Go and strict TypeScript checks pass. Measurements used Go
1.27.1 on darwin/arm64 (Apple M3 Max, `GOMAXPROCS=4`) and Node 22.18.0 with
TypeScript 5.9.3. Five alternating candidate/reference rounds measured Go
`Parse`, `Recognize` and typed `ParseAST` in both units, plus TypeScript parsing
of strings and byte arrays after 5,000 warmup and 20,000 timed calls. Each Go
sample used 150 ms. The small and large literal workloads covered first,
middle, last and Unicode matches plus misses.

The paired median-ratio geometric means for Go were 1.001/0.990 (CodePoints /
Bytes) on the small workload and 0.952/0.955 on the large workload. Across
individual cases, median ratios ranged 0.965–1.038/0.966–1.014 (small) and
0.919–1.001/0.915–0.999 (large). TypeScript small-workload geometric means
were 0.975/0.997 and large-workload means were 0.904/0.924; per-case ratios
ranged 0.892–1.027/0.948–1.017 (small) and 0.839–0.991/0.864–1.013 (large).
The TypeScript string/Bytes first-match case was 1.013× slower
(range 1.003–1.019×); no other case was slower in all five pairs. These results
do not establish a blanket speedup. Eligible generated cases reduced allocation counts by 1–2 per parse on
small inputs and 1–8 on large inputs, saving 16–32 and 16–128 bytes respectively
(with one-byte rounding variation in recognition results). The no-op control grammar emitted
byte-identical source; its timing ratios were 0.989/1.001 for Go and
0.990/1.004 for TypeScript. Large-workload source shrank by 796 bytes for both
Go Node and typed output, and 274 bytes for TypeScript; small-workload source
increased by 8 and 14 bytes respectively.

The reproducible fixture exporter is:

```sh
ZP_LITERAL_DISPATCH_DIR=/tmp/zp-literal-dispatch \
  go test ./internal/gen -run '^TestGeneratedLiteralDispatchWorkload$' -count=1
```

The measured revision was based on `7f20034` with the literal-dispatch change.
Full generated plain-Go, typed-Go and TypeScript corpus validation and all
parser-module suites pass. The clean 131-case, three-sample full snapshot at
[f8c2399](https://github.com/ornew/zeroparse/commit/f8c2399e4679af46e98dca39d1bd7ccc9c2a4b49)
is recorded in [the full results](../benchmarks.md), without attributing it to
this optimization.
