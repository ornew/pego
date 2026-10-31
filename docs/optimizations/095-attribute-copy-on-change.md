# 095. Copy-On-Change Attribute Normalization

- **Status**: Implemented
- **Date**: 2026-10-14

## Problem and Mechanism

`normalizeAttributes` builds the executable grammar before backend compilation. Its eager copying path currently allocates replacement nodes for expression trees even when normalization leaves a node and all of its children unchanged. A copy-on-change path can return the original node in that case and copy only nodes whose child, attribute, or lowering changes.

The source AST must remain unchanged. If a nested node changes, copy each affected ancestor before storing the replacement. Keep attribute order, argument binding and validation, diagnostic positions, source metadata, and existing lowering behavior intact. Reuse unchanged nodes; do not describe the normalized result as a detached deep copy. `Parser.Grammar()` may be identical to, or share unchanged nodes with, the source AST.

Preserve the legacy normalized nil-versus-empty slice shapes, including containers for statements, expression children, fields, operands, levels and operators. Compare against the eager path rather than deriving normalized shapes from the input. Treat opaque extension values as opaque; only the supported expression arguments undergo recursive normalization.

## Applicability

| Execution profile | Applied / Partial / Not applied / Not applicable | Reason |
|:--|:--|:--|
| Shared grammar preparation before backend compilation and generation | Applied | Normalization is shared before backend selection; input matching is unchanged. |

## Reproduction

The initial 315-row comparison used baseline commit `c693d51` and a candidate
source snapshot, with an eager same-source mode as a separate counterfactual.
Five alternating samples ran 1-second benchmarks on Go 1.27.1, Apple M3 Max
(darwin/arm64), GOMAXPROCS=16, GOGC=100 and GOMEMLIMIT=off. Inputs were plain,
attribute-heavy, pragma-heavy, Operators Rule, JSON, Minilang and the full source
grammar. Operations were normalization, compilation of a pre-parsed AST, and
source preparation (Parse plus Compile); source reads and setup were outside the
timed interval.

The targeted 30-row recheck used the same pinned source and binaries but selected
only three compile/source-preparation conditions. Five alternating fresh-process
samples ran for 3 seconds each. It was not a repeat of all 21 workload/operation
combinations in the initial run. Ratios below are medians of paired
candidate/baseline ratios and ranges, not ratios of aggregate medians.

| Input and operation | Initial paired time ratio | Targeted paired time ratio | Targeted B/op | Targeted allocs/op |
|:--|--:|--:|--:|--:|
| Attributes — Compile | 1.065× [1.033–1.087] | 1.002× [0.991–1.002] | 8,936 → 8,760 | 106 → 103 |
| Attributes — source preparation | 1.092× [1.051–1.117] | 0.995× [0.972–1.046] | 20,248 → 20,072 | 155 → 152 |
| Full source grammar — Compile | 1.060× [0.987–1.133] | 0.985× [0.898–1.024] | 3,453,140 → 3,352,091 | 28,660 → 27,873 |

The first run used mixed condition ordering with map-order carryover. Its
normalization paired-median ratios across seven inputs ranged from 0.181 to
0.973 for time, 0 to 0.919 for bytes, and 0 to 0.824 for allocations. Source
preparation allocation examples from that run:

| Input | B/op | Allocations/op |
|:--|--:|--:|
| JSON | 317,218 → 306,930 | 2,546 → 2,471 |
| Minilang | 1,013,919 → 988,444 | 7,655 → 7,461 |
| Full source grammar | 6,778,386 → 6,678,373 | 37,363 → 36,576 |

The longer targeted ranges include parity and did not reproduce a persistent
attribute-heavy slowdown. Ordinary host noise is possible; retained peak heap
was not measured. These comparisons do not establish a universal timing effect.

The candidate test suite includes `BenchmarkAttributeNormalization` in
`internal/compile/normalize_attributes_test.go`. From the repository root, build
the candidate test binary with:

```sh
go test -c -o /tmp/zp-copy-candidate.test ./internal/compile
```

Build the baseline binary from a candidate checkout using a Go overlay. The
overlay JSON maps the checkout's `compile.go` and `normalize_attributes.go` to
their original, byte-for-byte files from commit `c693d51`, and maps
`normalize_attributes_test.go` to a benchmark-only file containing its package
declaration, required imports, benchmark globals, and adapted benchmark
function. Keep calls on the original `normalizeAttributes(g)` and
`Compile(g, Options{})` entry points; exclude the candidate's new non-benchmark
tests and methods. The overlay changes which files the compiler reads without
physically overwriting the checkout. Build the baseline binary with:

```sh
go test -overlay=/tmp/zp-copy-baseline-overlay.json -c \
  -o /tmp/zp-copy-baseline.test ./internal/compile
```

After building both binaries, run each filtered invocation from
`internal/compile` so benchmark fixtures resolve relative to the package. Use a
fresh process per invocation, alternate baseline and candidate order across
five rounds, and keep the corpus and environment fixed:

```sh
cd internal/compile
GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off /tmp/zp-copy-baseline.test \
  -test.run='^$' \
  -test.bench='^BenchmarkAttributeNormalization$/^attributes$/^eager$/^compile$' \
  -test.benchtime=3s -test.count=1
GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off /tmp/zp-copy-candidate.test \
  -test.run='^$' \
  -test.bench='^BenchmarkAttributeNormalization$/^attributes$/^copy-on-change$/^compile$' \
  -test.benchtime=3s -test.count=1
```

For the targeted source-preparation recheck, replace the source/mode/operation
filter with `attributes/eager/source-prepare` or
`attributes/copy-on-change/source-prepare`; for the full grammar compile recheck,
use `source-grammar/eager/compile` or
`source-grammar/copy-on-change/compile`. The initial run additionally covers
the other five inputs and normalization operation. A same-source eager versus
copy-on-change probe is not a substitute for the exact-baseline comparison.

Pinned fingerprints (the baseline test binary includes the listed benchmark-only overlay):

| Artifact | Fingerprint |
|:--|:--|
| Baseline Git commit SHA | `c693d51fba337aa34069acca6b50a29ddd1428a9` |
| Candidate test binary SHA-256 | `d7e253fdb9830f0d2aab77fdfe6f85c15d4bdcd6ae313e729ca3da39b437e3cb` |
| Baseline test binary SHA-256 | `c675cd178618c8502a687f147de7f37e6ca4276803f26c9dd3547110ecc898c4` |
| Baseline overlay manifest SHA-256 | `1f4dfcea350fce3bffa94984ae64c6412f8fc261f75fba84f8de818cc0f7c1dc` |
| Baseline overlay test snippet SHA-256 | `c024148d25119aa868e27bb5c400f3a11b251b914f41e1d73821c9aa0b8ab35a` |
| Baseline overlay `normalize_attributes.go` SHA-256 | `a1b588bf0803bb8d642ad63cccc19e5aede65b29cac7720e69a8a996c2fdc122` |
| Baseline overlay `compile.go` SHA-256 | `54ff0bfd44271bb4bb9cf0f4ae0bc558b7ab5cbf9061f20700a2fa27f63e5790` |
| Candidate `normalize_attributes.go` SHA-256 | `68b7f19aaae2bd0f374e78453ee3fcfe45127c856d28b49f9c178f8fdc2640bf` |
| Candidate `normalize_attributes_test.go` SHA-256 | `5e8bcdfec6f319276ad2396c8f9ba129950fd39a0fc7208904c4d4d9d1480b5e` |
| Candidate `compile.go` SHA-256 | `9ca60a5f32554ba6e99ab6c4018bdb4836ed9445bf581f7431598c94a9871242` |
| `zp.zp` grammar SHA-256 | `7567b967bc19ee30b7ebd0b7f09cdef6df0fe5157468b73bad22a543e57e0774` |
| JSON grammar SHA-256 | `a14e28c3dce151bc42aa1d676ad020059344871af20bb8e3282947a76108db4b` |
| Minilang grammar SHA-256 | `bf466e1f677b9b8f00c2a4378c141545f648ecb9c22c2c33246bad4cb968f930` |

## Results

Copy-on-change is implemented as the default attribute-normalization path; the
eager path remains a private benchmark control. The focused checks verify source
AST immutability, unchanged attribute order and validation, legacy nil/empty
shapes, backend behavior, and sharing only for unchanged subtrees. Generated
parser files were byte-identical after regeneration, and parser-module tests and
vet passed.

The initial synthetic attribute-heavy slowdown was not reproduced by the longer
targeted recheck. The allocation reductions shown above remain workload-specific;
no universal compile-time speedup is claimed. A clean full-suite benchmark snapshot for a combined revision is a checkpoint,
not an adoption gate or proof of this mechanism's causal effect.

## Correctness and Tradeoffs

The caller's source AST is never mutated. Callers must continue to avoid concurrent mutation while the AST is being parsed, validated, formatted, serialized or compiled. Sharing does not provide an independent-copy guarantee: use `Grammar.Clone()` for an independent editable AST within its documented value domain.

Focused checks cover eager/copy-on-change output and error parity, normalized nil/empty shapes, source immutability, binding and validation order, backend behavior, unchanged-subtree sharing, and copied changed paths. Cases include no attributes, nested attributes, pragmas, Operators Rules, opaque extension values and normalization failures.

## Decision and Limits

Implemented. This compile-stage mechanism makes no claim about parse throughput.
