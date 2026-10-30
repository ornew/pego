# 105. Character Class Tables in Generated Go Parsers

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

Each character class of a generated typed parser spelled out, in place, the read
of the next character, the range test, the expectation record on failure and the
construction of the terminal. A case-insensitive keyword such as `[uU] [nN] [iI]
[oO] [nN]` repeated that for every letter. These lines were the largest repeated
pattern in the large generated parsers.

A parser generated without budget controls and without Documents now declares one
immutable `Class` per distinct class (a 128-bit ASCII membership set, the pairs of
wider ranges, and the negation flag) and uses it in two places:

- a run of consecutive classes whose value is not used (the letters of a keyword)
  is one call of `ClassesSkip` over a table of steps;
- a single class of more than four ranges is tested through `ClassFast`, which the
  compiler inlines (an ASCII bitset test on the code point or byte at the
  position), followed by `ClassSkip` for every other character, which also records
  the expectation of the failing class at the failing position, as the inline form
  did. Values, errors and expected sets are unchanged.

A single class of up to four ranges, the first-character test of a choice and
the repetition of a class keep their inline forms; a run of classes uses one
table call whatever the size of its classes. Measured on cue with 30 interleaved
rounds, a table lookup in each of them cost between 0.5% and 1.5% (a call per
scan alone cost 4%), and it saved almost no bytes in the repetition and the
first-character test. Controlled parsers and Documents keep the inline forms.

## Results

| Parser | Before | Large classes and keywords | Tables for every class |
|:--|--:|--:|--:|
| duckdb | 5.98 MB | 5.12 MB | 4.84 MB |
| typescript | 2.86 MB | 2.56 MB | 2.53 MB |
| python | 1.74 MB | 1.57 MB | 1.55 MB |
| golang | 1.52 MB | 1.43 MB | 1.41 MB |
| cue | 1.15 MB | 1.07 MB | 0.98 MB |
| cel | 0.30 MB | 0.28 MB | 0.27 MB |

Sizes are the bytes of `parser.go` with `-typed-only`. The adopted column is the
middle one. The last column (also tables for single classes of up to four ranges)
was +2% to +3% in the geometric mean of the language-module benchmarks and is not
adopted.

Language-module benchmarks of the adopted form (six alternating rounds,
`-benchtime=200ms`, against the commit before): cue +1.2%, python +1.1%, duckdb
-0.7%, with the unchanged yaml parser at +0.8% as the noise reference. The
`./bench` generated parsers keep controls or are not specialized and show no
change.

## Correctness and Tradeoffs

The module tests of cel, cue, duckdb, golang and python compare each generated
parser with the interpreter on error text and spans. The engine tests, the root
suite and `parsers/test.sh` pass.

## Decision and Limits

Adopted for Go parsers without budget controls and Documents. The helpers are
generated-code support, not a stable API.
