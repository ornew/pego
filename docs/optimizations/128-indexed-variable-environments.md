# 128. Indexed Variable Environments

- **Status**: Adopted
- **Date**: 2026-10-31

## Problem and Mechanism

Every declared variable is bound from the start of a parse, so the set of names in an environment
never changes. The environment was nevertheless a name-keyed linked list: a read compared names
along the list, and replacing a value behind other names copied the prefix. A grammar with V
variables paid O(V) per read and per changed bind, in both time and allocation, and a controlled
parse charged the same walk to the budget.

The compiler now numbers the variables in declaration order and every backend addresses them by
that slot. The environment is a persistent vector: a flat copy-on-write array for up to 16
variables, and above that a 16-way trie of depth 1 (up to 256 variables) or 2 (up to 4096). A read
walks `levels+1` nodes; a bind copies the same number of nodes and shares the rest, so saved
environments, rollback, memo keys and `Document` reuse behave as before. Bytecode instructions carry
the slot as their operand (`ISAVersion` 27). The closure engine resolves a bind's slot when the
graph is built, generated Go and TypeScript emit slot numbers, and a memo key is still the values of
the variables a rule reads, now fetched by slot.

## Budget Charges

A controlled bind charges 1 unit for the old value, `levels+1` units when the value changes, and the
value comparison. A read charges `levels+1` units. `EnvValues` charges 1 unit per slot plus the
read charge of a declared slot; the generated version also charges 1 unit when it starts. Variable
names are not charged, so a cost no longer depends on name length or on the position of a name in
the environment.

The smallest budget that parses a 280-variable grammar with a shared 28-character name prefix falls
from 9,974,231 to 1,971 (closure), 1,973 (bytecode) and 1,973 (generated Go). Grammars that bind
variables change their recorded budget (`budget.golden`); grammars without variables do not.

## Results

Medians of ten interleaved runs, before to after.

| Benchmark | Closure | Bytecode | Iterative bytecode |
|:--|--:|--:|--:|
| 280 variables, controlled | 2602 to 76 us (-97%) | 2614 to 76 us (-97%) | 2552 to 75 us (-97%) |
| 17 variables | 14.6 to 7.4 us (-49%) | 16.8 to 8.3 us (-50%) | 16.9 to 8.7 us (-49%) |
| 4 variables | 4.64 to 4.49 us (~) | 6.04 to 5.73 us (~) | 6.23 to 6.12 us (~) |

The 280-variable parse allocates 94.6% fewer bytes and 97.9% fewer objects; four variables allocate
41% to 61% fewer objects and 1% to 2% fewer bytes. Environment-binding streams of 16,000 binds run
21% to 56% faster. Distinct bindings run 75% faster at 16 names and 95% faster at 128.

Controls without variables or with few: JSON and Outline parse, recognize, incremental and stream
benchmarks show no slowdown in any backend; Outline (a few `:=` variables) is 6% to 10% faster, and
JSON is unchanged within noise on closure and generated Go (iterative bytecode -7%).

## Leaf and Fan-Out Cutoff

The flat leaf and the trie fan-out share one width. Width 8, 16 and 32 were swept on the closure and
iterative bytecode engines with ten interleaved runs of 150 ms each, on a machine with other load
(the baseline column has a wide spread):

| Benchmark (sec/op, 16 as the base) | Width 8 | Width 32 |
|:--|--:|--:|
| 4, 9, 12, 16 variables | ~ | ~ |
| 17 variables, closure | -5.7% (controlled) | -5.9% to -7.8% |
| 17 variables, bytecode | ~ | ~ |
| 32 variables | ~ | ~ |
| 280 variables | -14% to -16% | ~ |
| JSON and Outline Parse, closure and iterative | ~ | ~ |

No width is significantly better at small V, and the only gain of width 8 (280 variables) comes from a
base run with a spread above 100%. Width 16 stays. The generated runtime was not swept.

## Limits

The TypeScript environment is verified by tests but not benchmarked. Generated Go for the five large
grammars is slightly smaller (about 0.06% of `parser.go`).
