# 124. Coarse Budget Charges in the Engine

- **Status**: Implemented
- **Date**: 2026-10-30

## Problem and Mechanism

The closure backend and both bytecode VMs charged the budget at every expression (closure), every
instruction (bytecode and iterative VM) and every term of an action. A parse under a limit paid a
charge for nearly every step, 1.4 to 1.5 times the time of the same parse without a limit.

The engine now charges as generated Go does: one step on each rule call, one on each iteration of a
repetition (a scan of a class or `.` charges per character, and an Operators Rule charges per
operator loop iteration and per recursion level), and the steps proportional to the size of the
values it builds. Between two charges only the straight-line code of one rule body runs, whose
length is bounded by the size of the grammar, so the charges still bound the work: a repetition
that matches many times, an unbounded scan and recursion without consuming input are all charged.

The compiler no longer wraps every expression in a charging step. The closure builder charges in its
repetition matchers (only in the metered graph, so the unmetered graph is unchanged). The VM
dispatch loop charges at the iteration instruction and in the scan loop, and the iterative VM
charges where the recursive one does: when a rule call starts and at each loop iteration of an
Operators Rule. A skipped alternative of a guarded choice, and the terms of an action, charge
nothing; the built-ins that loop over a list charge per element as before.

## Results

Smallest budget that parses a 256 KiB JSON and XML, a YAML stream of similar size and 5000 CSV
rows:

| Input | Closure | Bytecode | Iterative |
|:--|--:|--:|--:|
| JSON | 2927892 to 2076497 | 3302916 to 2168922 | 3609915 to 2148039 |
| XML | 3190855 to 2501588 | 3651654 to 2597327 | 3911697 to 2590638 |
| YAML | 8423786 to 6064283 | 9465718 to 6423852 | 10090258 to 6339939 |
| CSV | 1538442 to 1294989 | 1647622 to 1289991 | 1697636 to 1289991 |

Parse time with `Limits{Budget: ^uint64(0)}` against the same parse without a limit, medians of
seven interleaved rounds (the parse without a limit moved by at most 2%):

| Input | Closure | Bytecode | Iterative |
|:--|--:|--:|--:|
| JSON | 1.495 to 1.233 | 1.378 to 1.215 | 1.390 to 1.188 |
| XML | 1.504 to 1.252 | 1.348 to 1.224 | 1.364 to 1.197 |
| YAML | 1.544 to 1.247 | 1.406 to 1.244 | 1.400 to 1.218 |
| CSV | 1.493 to 1.331 | 1.359 to 1.227 | 1.358 to 1.233 |
