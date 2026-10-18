# 101. Skip Positional Items for Action-Free Rules

- **Status**: Implemented
- **Date**: 2026-10-18

## Problem and Mechanism

The type checker types a rule body once, then typed every visible top-level
sequence item a second time to build the positional item list that a rule
action reads through `$1`, `$2` and so on. That list was built for every
sequence rule, including rules without an action, where nothing reads it.
For a long sequence of captures the second pass repeated all expression
checking, capture merging and type construction.

The checker now builds the positional list only for rules with an action.
The first pass over the whole body already reports every diagnostic of the
items and records every checked field and result, and the second pass ran
each item in a fresh capture scope whose captures were discarded. Rules with
an action, Operators Rules and the single-item fallback for non-sequence
bodies are unchanged.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Shared type checking | Applied | Runs before compilation and generation for every backend |
| Parser runtimes and generated code | Not applicable | No matching-path change |

## Reproduction

Baseline `178943c` and the candidate differ only in `internal/engine/check.go`.
Both run on Apple M3 Max with Go 1.27.1. Three alternating
baseline/candidate rounds of two samples each give six samples per case:

```sh
go test ./internal/engine -run '^$' -bench '^BenchmarkCaptureChecking$' -benchmem -count 2
```

Each iteration runs the complete `checkTypes` on an already prepared Program.
The shapes are those of record [072](072-reuse-structurally-equal-duplicate-capture-types.md):
recursive repeats `"😀" x:main` in `main`; record repeats `x:item`; mixed
repeats `x:a x:b?` with two node types; unique uses distinct labels. The
control cases check the calculator, minilang and JSON grammars. Raw results
stay local.

## Results

benchstat reports p=0.002 for every row; time variation is within ±2%.

| Workload | Time before → after | B/op before → after | Allocs/op before → after |
|:--|:--|:--|:--|
| Control calculator | 12.34 → 11.93 µs (−3.3%) | 16.73 → 16.23 KiB | 401 → 383 |
| Control minilang | 193.7 → 189.9 µs (−2.0%) | 268.0 → 263.8 KiB | 3,635 → 3,518 |
| Control JSON | 32.11 → 29.58 µs (−7.9%) | 47.12 → 43.84 KiB | 908 → 800 |
| Recursive 1,024 | 65.14 → 55.54 ms (−14.7%) | 30.68 → 1.67 MiB | 274.57k → 60.85k |
| Recursive 32,768 | 2.080 → 1.779 s (−14.5%) | 1,086 → 44.38 MiB | 8.529M → 1.712M |
| Recursive 131,072 | 8.310 → 7.109 s (−14.5%) | 4,366 → 177.9 MiB | 34.09M → 6.823M |
| Record 32,768 | 18.57 → 9.74 ms (−47.5%) | 35.08 → 5.04 MiB | 131,243 → 103 |
| Mixed 32,768 | 97.80 → 67.94 ms (−30.5%) | 120.6 → 50.07 MiB | 3.146M → 2.425M |
| Unique 32,768 | 20.16 → 11.33 ms (−43.8%) | 57.08 → 27.04 MiB | 132,297 → 1,181 |

The 1,024 and 8,192 sizes of each shape change by the same proportions. The
geometric mean over all sixteen cases is −28.5% in time, −77.4% in bytes and
−90.0% in allocations. Bytes are cumulative allocations over one checker
invocation, not retained heap.

## Correctness and Tradeoffs

Before the change, a differential run typed the skipped items anyway for
every action-free sequence rule and compared the result with the state the
first pass left. It covered 289 grammars from the repository and the test
corpora, then the engine, syntax, grammar, LSP and root test suites, about
2,900 checked rules. No rule gained a diagnostic, a checked field or a
checked result; grammars with type errors in items, predicates and inline
actions were included as positive controls. Rules with an action still type
their items, so positional indexes, recursive widening and generated types
are unchanged.

## Decision and Limits

Adopted as the default. Recursive inference still checks every occurrence in
each inference round, which dominates the remaining large recursive cost.
