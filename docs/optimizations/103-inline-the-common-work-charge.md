# 103. Inline the Common Work Charge

- **Status**: Implemented
- **Date**: 2026-10-20

## Problem and Mechanism

A generated parser charges its work control once per rule call (`stepWork`) and
once per input unit a matcher reads (`inputWork`). Each charge called
`workControl.charge`, whose body tests both limits and both poll intervals
before adding to four counters; at an inline cost of 322 it never inlined, so a
controlled parse paid a function call per step and per unit read. When the
generated runtime stopped being rewritten per grammar, `inputWork` also kept its
Document branch in grammars without Documents, grew past the inline budget
itself, and controlled parses became about 4% slower.

The work control now keeps, for input and for steps, a *room*: the smallest
request that would reach the next poll boundary or exceed the limit. A charge
smaller than both rooms only subtracts from them; any other charge takes the
slow path, which folds what the rooms consumed into the counters, runs the
former checks unchanged and computes new rooms. `chargeStep` (one step) and
`chargeInput` (n units and one step) are the forms the hot paths use; both
inline, and `stepWork` inlines with `chargeStep` inside it. The reserved totals
are the counters plus what the rooms consumed (`inputUsed`, `stepsUsed`).

Invariant: a fast-path charge is exactly a charge that the former code accepted
without polling, because the room is the interval left before a poll, capped by
the limit left plus one. Cancellation, poll cadence, the reported `Used` and
`Requested` values and the priority of input over step exhaustion are those of
the slow path, which is the former code.

## Applicability

| Execution profile | Status | Reason |
|:--|:--|:--|
| Generated Go Node and recognition | Applied | Shared generated runtime |
| Generated Go typed output | Applied | Shared generated runtime |
| Closure, bytecode and iterative VMs | Not applied | Engine work control is separate |
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

## Correctness and Tradeoffs

The generated work-control tests pass; with rooms forced to unlimited, eight of
them fail, so they exercise the slow path's limits. Generated-code tests read
the reserved totals through `inputUsed()` and `stepsUsed()`. `chargeInput`
requires a non-nil control; `chargeStep` and `charge` accept nil. The fast path
assumes the input room is at least one outside the slow path, which refill and
the fast path both preserve.

## Decision and Limits

Adopted. The closure evaluator, the VMs and the TypeScript runtime keep their
own charge; the same rooms would apply there but are unmeasured.
