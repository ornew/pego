# 103. Inline the Common Budget Charge

- **Status**: Implemented
- **Date**: 2026-10-20

## Problem and Mechanism

A generated parser charges its budget control once per rule call, loop iteration
or matcher step (`Parser.Spend`) and once per input unit a matcher reads. A
charge that called `BudgetControl.charge` directly would test the limit and the
poll interval before adding to the counters; at that inline cost it never
inlines, so a controlled parse would pay a function call per step and per unit
read. The charge of an input read depends on the function: a function without a
Document charges with `Control.Take(1)`, which inlines; a function with a
Document calls `Parser.SpendRead`, which also records how far the parse examined
the input and keeps that branch out of the inlined path.

The budget control keeps a *room*: the smallest request that would reach the next
poll boundary or exceed the limit. A charge smaller than the room only
subtracts from it; any other charge takes the slow path, which folds what the
room consumed into the counter, runs the limit and poll checks and computes a
new room. `chargeStep` (one unit), `Charge` (n units) and `Take` (n units, for a
condition: it returns true) are the forms the hot paths use; all inline, and
`Spend` inlines with `chargeStep` inside it. The reserved total is the counter
plus what the room consumed (`budgetUsed`).

Invariant: a fast-path charge is exactly a charge that the slow path would
accept without polling, because the room is the interval left before a poll,
capped by the limit left plus one. Cancellation, poll cadence and the reported
`Used` and `Requested` values are those of the slow path.

## Charges of the Inlined Matchers

The generated matchers charge in line what the runtime paths they replace
charge, unit for unit.

- **Input read.** The inlined read of the next character charges one unit with
  `Control.Take(1)`.
- **Literal compare.** The inlined compare of a literal charges one unit per code
  point with `Control.Take(n)` when the comparison holds. A mismatch falls to
  `MatchLiteral`, which charges up to the mismatch.
- **Take.** `Take(n)` subtracts from the room when `n` is below it. Otherwise
  `takeSlow` charges the units one at a time, so exhaustion and polls fall where
  they do when the matcher charges unit by unit; the `Used` and `Requested` of a
  `LimitError` do not depend on the batch.
- **Large classes.** A class with more than four ranges is a `BudgetClass`: the
  ranges, the negation and a table that holds, for each ASCII character, the units
  a scan of the ranges charges up to the range that holds it, and the result.
  `RejectBudgetClass` takes the table cost for an ASCII character and scans the
  ranges, one unit each, for any other. Smaller classes are tested with the
  compact condition and no charge.
- **Expectations.** `ExpectBudget` records the farthest failure of a function
  without a Document. It charges the scan for a duplicate and leaves out the
  Document's record of the examined input that `Expect` makes.
- **Source pass.** The two passes of `setControlledSource` charge up to four
  units per character. A run of ASCII characters, each with at least four bytes
  left, is charged in one subtraction when every one of its charges would take the
  fast path, which is what charging them one by one does.
- **Control only when needed.** A parse has no control when it sets no limit and
  no cancelable context; the inline charges then test the control for nil and do
  nothing, and the literal and class paths skip their charged forms. A context that
  can be canceled creates a control and is polled, with or without a limit. A
  control without a context never polls and grants the largest room, so a parse
  with only a budget reaches the slow path of a charge only at its limit.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go Node and recognition | Applied | Shared generated runtime |
| Generated Go typed output | Applied | Shared generated runtime |
| Closure, bytecode and iterative VMs | Not applied | Engine budget control is separate |
| Generated TypeScript | Not applied | Separate runtime |

## Reproduction

Baseline f327bdb5; candidate the shared-runtime change with this mechanism, on
an Apple M3 Max with Go 1.27.1. Test binaries of `./bench`, `parsers/json` and
`parsers/csv` were built per revision and run alternately from their own trees,
six rounds at `-benchtime=200ms` with `-benchmem`, for
`^Benchmark(Parse|Recognize|Incremental|IncrementalLong|Stream|ControlledParse)$/./generated`
and the parser module benchmarks, then compared with benchstat. The
intermediate revision without this mechanism was measured the same way, ten
rounds at `-benchtime=300ms`, for `ControlledParse/(JSON|XML)/generated`.

## Results

| Workload | Baseline | Candidate | Change | Uncertainty |
|:--|--:|--:|--:|:--|
| ControlledParse JSON generated, codepoints | 14.96 ms | 8.75 ms | −41.5% | ±5% / ±8%, p=0.002 |
| ControlledParse JSON generated_ast, bytes | 10.83 ms | 6.27 ms | −42.0% | ±2%, p=0.002 |
| ControlledParse XML generated, codepoints | 21.17 ms | 11.75 ms | −44.5% | ±5% / ±13%, p=0.002 |
| ControlledParse XML generated_ast, bytes | 17.57 ms | 8.98 ms | −48.9% | ±5% / ±6%, p=0.002 |
| All gated generated benchmarks, geomean | 6.207 ms | 5.390 ms | −13.2% | n=6 |

Without this mechanism the shared-runtime revision measured +4.3% (geomean of
the eight ControlledParse rows, each p≤0.035) against the same baseline. No
gated benchmark became significantly slower, and no B/op or allocs/op row grew.
Ordinary (uncontrolled) parses do not charge; their changes in the same run come
from the shared-runtime change.

### Controlled parses against the uncontrolled parser

The ratio of the median time of a parse to that of a parser generated without
budget controls, over seven paired rounds on the benchmark inputs (JSON, XML,
YAML and CSV grammars of `parsers/`, Go 1.27.1, Apple M3 Max, `-trimpath`, run
under the benchmark lock). (b) passes no option; (c) passes
`Limits{Budget: 2^64-1}`.

| Entry | Grammar | (b) no options | (c) with a limit |
|:--|:--|--:|--:|
| Parse | JSON | 1.112 | 1.249 |
| Parse | XML | 1.128 | 1.315 |
| Parse | YAML | 1.080 | 1.331 |
| Parse | CSV | 1.092 | 1.247 |
| ParseAST | JSON | 1.131 | 1.321 |
| ParseAST | XML | 1.174 | 1.381 |
| ParseAST | YAML | 1.118 | 1.453 |
| ParseAST | CSV | 1.081 | 1.264 |

The aim of 1.15 times the uncontrolled parser for a parse with a limit is not
met: the ratios of (c) are 1.25 to 1.45. The remaining gap is not the charges.
With every charge made free (`Take`, `Charge` and `chargeStep` do nothing) the
JSON parse with a limit still costs 1.15 times the uncontrolled parse in `Parse`
and 1.17 times in `ParseAST`. The controlled bodies call the general runtime
helpers (`NewMatch`, `Peek`, `Text`) and the source pass of a control, where the
ordinary body calls the `Ordinary` forms. Closing the gap needs a controlled body of
the ordinary shape, not cheaper charges.

## Correctness and Tradeoffs

The generated budget-control tests pass; with rooms forced to unlimited, eight of
them fail, so they exercise the slow path's limits. Generated-code tests read
the reserved totals through `inputUsed()` and `stepsUsed()`. `chargeInput`
requires a non-nil control; `chargeStep`, `Charge` and `Take` accept nil. The fast path
assumes the input room is at least one outside the slow path, which refill and
the fast path both preserve. Tests compare `Take`, the class table and the source
pass with a reference that charges one unit at a time, at limits that fall
inside a batch, and check that a context without a limit creates a control and
is polled.

## Decision and Limits

Adopted. The closure evaluator, the VMs and the TypeScript runtime keep their
own charge; the same rooms would apply there but are unmeasured.
