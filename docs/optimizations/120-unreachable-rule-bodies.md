# 120. Unreachable Rule Bodies in Generated Parsers

- **Status**: Implemented
- **Date**: 2026-10-25

## Problem and Mechanism

A generated Node or recognizer parser printed the Go body of every rule, whether or not
anything could enter it. A rule that returns a tree and is not part of a left-recursion cycle
has a value-free twin, and every reference from a value-free context (a predicate, a
discarded part, a recognizer) calls the twin. The original body of a rule that is referenced
only from such contexts is never called, and neither are the original bodies that only it
calls. The recognizer table has the same shape with its own twins. Document parsers route
every call through the rule slot, so there the twins and originals follow the call graph the
same way. On the 14 grammars of the output manifest, between 34% and 44% of the generated Node
source belonged to such bodies.

The generator now runs twice. The first pass, which stops after the Node and recognizer tables, records, for each rule table (`rules`,
`recRules`), the rule slots and wrapper functions that each printed body calls (`ruleCall`),
dropping the calls of attempts that were rolled back. The roots are the start rules of both
tables, which the generated entry points and `Starts` reach, and the calls generated outside a
rule body, which are those of the stream body. A worklist over the recorded calls gives the
entered (rule, slot or wrapper) pairs. The second pass prints only those:

- a rule with neither its slot nor its wrapper entered has no body, no helper functions and no
  `Body`, `Action` or `OperatorsRule` assignment;
- a plain rule that is called only through its wrapper keeps the wrapper and the helpers it
  uses, and has no `Body` assignment and, when the wrapper is the direct form, no body
  function;
- the rule table rows, the expectation table and the capture layouts are the same as before, so
  every ID in the printed code keeps its value.

The second pass fails generation if a printed call reaches a rule that was left out, and if an
entered slot has no printed `Body`, so a missing root cannot become a nil `Body` at run time.
Typed-only output is generated in one pass as before: it prints few Node functions, and its
typed code shares the literals and shapers that their generation declares.

## Results

Generated `parser.go` bytes over the 14 grammars of the output manifest, per mode:

| Mode | Before | After | Change |
|:--|--:|--:|--:|
| node | 8,901,905 | 5,187,538 | -41.7% |
| node-rec | 14,523,285 | 9,334,025 | -35.7% |
| node-wc | 21,177,793 | 11,938,740 | -43.6% |
| node-rec-wc | 34,715,469 | 22,020,327 | -36.6% |
| node-doc | 16,161,287 | 10,659,438 | -34.0% |
| default-value | 8,928,250 | 5,549,421 | -37.8% |
| source-info | 8,902,339 | 5,187,972 | -41.7% |
| typed (Node and recognizer tables included) | 13,128,988 | 9,414,174 | -28.3% |
| typed-rec-wc | 47,487,147 | 34,792,075 | -26.7% |
| typed-doc | 29,962,651 | 24,460,992 | -18.4% |
| typed-only, typed-only-wc | unchanged | unchanged | 0% |

After the change, an AST call-graph check of every non-typed output finds no rule body that
cannot run and no unreferenced function; all outputs define every generated function they call.

Speed was checked on the generated Node parser and recognizer of `parsers/json` and
`parsers/duckdb` (the json and duckdb benchmark inputs, the latter about 256 KB). Ten rounds
interleaved over the build before, the build after and two copies of the build before (the
noise band), `-benchtime=2s`; medians, as the change against the build before:

| Benchmark | Before | After | Noise copy 1 | Noise copy 2 |
|:--|--:|--:|--:|--:|
| json parse | 5.12 ms | -0.4% | -0.4% | -0.4% |
| json recognize | 1.79 ms | +1.5% | +0.6% | +0.8% |
| duckdb parse | 70.5 ms | -0.1% | -0.7% | -1.1% |
| duckdb recognize | 42.1 ms | -1.2% | 0.0% | -0.5% |

The change is in binary and source size, not run time: the removed functions were never
called. The json recognize median is 1.5% slower, above its noise copies (0.6% and 0.8%), but
the minimum of the ten rounds is +0.2% against -0.4% and +0.1% for the copies. The generated
functions on that path are the same code: the 14 recognizer wrappers and all 16 helper
functions have identical instruction opcode sequences before and after, and the inlining
decisions printed by `-gcflags=-m` differ only by the removed functions. What changes is the
addresses of the functions, because 22 functions around them are gone, so the difference is
attributed to code layout; it was not reproduced as a difference in the code that runs.

Generation time of `pego gen` (five runs each, medians): the first pass stops after the Node and
recognizer tables, so the output is generated once in full. duckdb Node with recognizer
6.39 s before and 3.85 s after, duckdb typed 7.13 s and 5.21 s, golang Node with recognizer
1.77 s and 1.22 s, golang typed 1.59 s and 1.24 s: the smaller output costs less to format.

## Limits

Only the Node and recognizer tables of the Go generator are pruned. The typed tables (`trules`)
and the TypeScript generator, which has the same twin redirect, are not. Typed-only output is
generated in one pass and is unchanged.
