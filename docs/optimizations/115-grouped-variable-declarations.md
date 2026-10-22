# 115. Grouped Variable Declarations

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem and Mechanism

Declaring the variables of a direct rule method one per line in a `var (...)` block
(`x2 int`, `x3 int`, `v4 any`, ...) costs padding: gofmt aligns the types of a block, so
every line carries padding for the longest name, and a large rule has dozens of such
lines.

The generator declares the variables of one type in a single statement,
`var x2, x3, x8 int`, one statement per type in the order of the first variable of the
type, and writes no block. Separate statements need no alignment. The same
helper writes the declarations of the lean Node bodies and the framed bodies. The
declared variables and their types are unchanged, so the compiled code is the same.
`TestVarLines` pins the grouping and `TestGeneratedMethodsGroupVariables` that a
method declares a type once.

## Results

`parser.go` with `-typed-only`, bytes and lines before and after:

| Parser | Bytes before | Bytes after | Lines before | Lines after |
|:--|--:|--:|--:|--:|
| duckdb | 2,100,950 | 2,014,984 | 111,520 | 98,807 |
| typescript | 1,286,152 | 1,235,730 | 66,535 | 59,470 |
| python | 767,020 | 735,945 | 37,675 | 33,412 |
| golang | 671,638 | 645,630 | 35,579 | 31,670 |
| cue | 467,239 | 450,263 | 23,367 | 20,896 |
| cel | 139,200 | 134,240 | 7,195 | 6,453 |

The duckdb test binary shrinks from 19,234,994 to 19,201,970 bytes (-0.2%, line
tables); the sizes of its text and data are unchanged, so parse speed is not affected.

## Decision and Limits

Adopted. Variables of one type are grouped in the order of their first appearance, so
the position of a variable in the method differs from the order of its first use.
