# 116. A Short Alias for the Runtime Import

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

Generated code refers to the runtime package about six thousand times in the duckdb
parser (`rt.TParser`, `rt.TActCtx`, `rt.Shaper`, ...), so the length of the alias counts. The
alias is a choice of the generator, not part of any API: it does not change the exported
names the generated packages declare or the signatures of their functions.

The generator imports the runtime as `rt`. The alias is one constant
(`runtimeAlias`) used by the generator, the types package it writes and the checks that
qualify runtime names; the user-facing signatures of `Parse` and the other entry points
mention the runtime types with the alias. A package-level name `rt` cannot clash:
the generator does not declare one and the names it derives from a grammar are types in
a separate package.

## Results

`parser.go` with `-typed-only`, bytes (lines are unchanged) before and after:

| Parser | Bytes before | Bytes after |
|:--|--:|--:|
| duckdb | 2,014,984 | 1,990,276 |
| typescript | 1,235,730 | 1,221,194 |
| python | 735,945 | 726,993 |
| golang | 645,630 | 638,190 |
| cue | 450,263 | 445,119 |
| cel | 134,240 | 132,592 |

The duckdb test binary is byte-for-byte the same size (19,201,970), since an import
alias exists only at compile time; parse speed is not affected.

## Decision and Limits

Adopted. Tests that compare generated source or write consumers into the generated
file use the alias constant's value.
