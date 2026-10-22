# 108. Prologue and Rollback Helpers

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

Every generated rule body began with the same four lines (count one more rule call,
fail beyond the nesting limit) and every backtracking point and failure exit ended
with the same two statements (return to the saved position, drop the errors recovered
since). Together they were a large part of the generated source.

The runtime now has `Parser.Enter` (the depth count and the nesting check) and
`Parser.Restore` (the position and the recovered errors), and the generator calls
them. Both are inlinable, so the compiled code of a call site is what the statements
were; `runtime/inline_test.go` fails if either stops being inlinable.

## Results

Bytes of `parser.go` with `-typed-only`:

| Parser | Before | After |
|:--|--:|--:|
| duckdb | 2.72 MB | 2.53 MB |
| typescript | 1.83 MB | 1.70 MB |
| python | 0.99 MB | 0.92 MB |
| golang | 0.94 MB | 0.88 MB |
| cue | 0.68 MB | 0.64 MB |
| cel | 0.19 MB | 0.18 MB |

The duckdb test binary grows from 17.76 MB to 18.26 MB (+2.8%): the machine code is
the same (+0.4% text), and each inlined call adds an inline-tree entry to the
function tables the binary keeps. The cold compile time of the duckdb test binary
(best of three, clean build cache) is unchanged at 6.7 to 6.9 s. Parse speed
(30 interleaved rounds, minimum, with an identical copy as the noise control) is
within noise: python -1.1% (control -0.5%), cue -0.1% (control +1.6%), duckdb -0.2%
(control -0.2%), at most 1.7 points from the control.

## Decision and Limits

Adopted. Making `Restore` a call instead (not inlined) shrinks the binary by a
further 0.8% but costs 1.6% to 3% of parse time on the language grammars, so the
helpers stay inlinable.
