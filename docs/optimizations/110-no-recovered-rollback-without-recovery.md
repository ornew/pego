# 110. No Recovered-Error Rollback Without Recovery

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

Every backtracking point of a generated parser saved `len(p.Recovered)` next to the
position and truncated the list when it went back. `p.Recovered` is only appended by
recovery (`#recover`); memo entries copy only errors that exist. In a grammar without
`#recover`, the list stays empty, so saving its length and truncating it are no-ops.

The generator now decides once per program whether any rule can recover (a `#recover`
attribute or a recovery expression). If none can, the saved variable, the
`p.Restore(start, rec)` calls and the length stores are not generated; a rollback is
`p.Pos = start`. Grammars with `#recover` generate what they did before.
`TestGeneratedRecoveredRollbackOnlyWithRecovery` checks both forms, and
`TestGeneratedParsersMatchEngine`, `TestGeneratedTypes`,
`TestGeneratedDocumentsMatchBatchOnCorpus`, `TestGeneratedUnsharedParsersMatchEngine`
and `TestGeneratedConstructedFieldChecks` fail when the rollback is dropped under
recovery.

## Results

`parser.go` with `-typed-only`, bytes and lines before and after:

| Parser | Bytes before | Bytes after | Lines before | Lines after |
|:--|--:|--:|--:|--:|
| duckdb | 2,337,553 | 2,207,830 | 116,542 | 114,168 |
| typescript | 1,427,281 | 1,352,046 | 69,180 | 68,061 |
| python | 845,854 | 805,025 | 39,373 | 38,640 |
| golang | 784,767 | 706,034 | 39,207 | 36,536 |
| cue | 518,213 | 490,503 | 24,488 | 23,951 |
| cel | 159,089 | 146,061 | 7,782 | 7,349 |

The duckdb test binary shrinks from 17,656,658 to 17,078,738 bytes (-3.3%); its cold
compile time (best of three, clean build cache) is 7.0 s before and 7.4 s after, which
is the spread of that measurement. Parse speed (30 interleaved rounds, minimum, an
identical copy as the control) is within noise: python -1.25% (control -1.21%), cue
-2.01% (-0.29%), duckdb -1.76% (-0.03%), cel +1.14% (+1.05%); cue and duckdb are 1.7 points
faster than their controls, python and cel within 0.1 point.

## Decision and Limits

Adopted. The optimization applies to the whole program: one `#recover` anywhere keeps
the rollback everywhere.
