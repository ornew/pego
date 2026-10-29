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

The generator runs twice. The first pass records, for each rule table (`rules`,
`recRules`, and `trules` of typed output), the rule slots and wrapper functions that each printed body calls (`ruleCall`),
dropping the calls of attempts that were rolled back. The roots are the start rules of the Node and recognizer
tables, which the generated entry points and `Starts` reach, the typed rules whose `Entry` a typed entry point runs,
and the calls generated outside a rule body, which are those of the stream body. A worklist over the recorded calls gives the
entered (rule, slot or wrapper) pairs. The second pass prints only those:

- a rule with neither its slot nor its wrapper entered has no body, no helper functions and no
  `Body`, `Action` or `OperatorsRule` assignment;
- a plain rule that is called only through its wrapper keeps the wrapper and the helpers it
  uses, and has no `Body` assignment and, when the wrapper is the direct form, no body
  function;
- the rule table rows, the expectation table of the Node and recognizer tables and their capture layouts are the
  same as before, so every ID in the printed Node and recognizer code keeps its value;
- a typed rule that nothing enters has no `Body`, `Action`, `Entry`, method, capture layout, expectation, literal, class
  table or term. The typed table has no rows of its own to keep (`NewTRules` builds it from the rule table), so the
  expectations and tables that only such rules would have used are not declared and the IDs of the rest are numbered
  without them. A typed rule has an `Entry` only if a typed entry point runs it;
- the fixed conversion helpers (`astInt`, `astKid`, `astNew`, ...) and the package variables of a rule body (capture
  layouts, literals, shapers, class tables and steps) are kept only if the printed code names them, directly or through
  another kept variable (`internal/gen/gogen/shake.go`; a lexical scan of the identifiers, which can keep an unused one and never drops a
  used one). The scan runs after the passes that edit the text of the functions, which can leave a variable unused.

The second pass fails generation if a printed call reaches a rule that was left out, and if an
entered slot has no printed `Body`, so a missing root cannot become a nil `Body` at run time.
A typed-only parser has no Node or recognizer rule to leave out when its entry points do not convert Nodes. When they
do (an action reads a field of a struct), it prints the Node rules that the entry points reach, under the same checks as
any other output, and the typed rules that nothing enters are left out.

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

Generation time of `pego gen` for the Node and recognizer tables (five runs each, medians), compared with a generator
that prints every rule: duckdb Node with recognizer 6.39 s before and 3.85 s after, duckdb typed 7.13 s and 5.21 s, golang Node with recognizer
1.77 s and 1.22 s, golang typed 1.59 s and 1.24 s: the smaller output costs less to format.

### Typed tables

The typed rules are part of the two passes, and the fixed helpers and one-line package variables are kept only if
named. The output equals that of a generator that prints all typed rules and then removes, from the printed source, what
only an `Entry` of a rule other than a start rule could reach (a parse and format of the whole file), except that the
expectations, literals, shapers, class tables and capture layouts of left-out rules are not numbered; that removal finds
nothing in the output of any of the ten checked-in parsers. Bytes of `parser.go` (all rules printed then removed, as
printed now): cel 117,685 and 117,673; csv 48,274 and 48,273; cue 409,807 and 409,786; duckdb 1,651,194 and 1,651,168;
golang 540,810 and 540,772; json 116,593 and 116,592; python 626,651 and 626,610; typescript 984,343 and 984,251; xml
636,623 and 636,622; yaml 1,171,445 and 1,171,435. The tables change little because the removal already took the code;
what changes is the numbering and one pass over the source, which is gone. The Node outputs of the manifest shrink where
the scan finds an unused one-line variable (cue node: 16 unused work-class range tables, 409,246 to 388,932 bytes).

Generation time of `pego gen` (five runs each, medians, same machine and session), compared with that generator:
duckdb typed-only 2.71 s before, 1.90 s after; yaml with types, recognizer and work controls 1.09 s before,
0.61 s after.

Speed was checked on the generated yaml and duckdb parsers and on the benchmarks of `bench` that do not use
them (a control), eleven alternating rounds, `-benchtime=2s`, medians, as the change against the build before
(the spread of the rounds is 3 to 6 %, 12 % for the CSV standard-library control):

| Benchmark | Before | Change |
|:--|--:|--:|
| yaml Parse | 18.43 ms | +0.3% |
| yaml Recognize | 10.35 ms | -0.8% |
| yaml ParseAST | 11.92 ms | -0.7% |
| duckdb ParseAST | 61.03 ms | -0.5% |
| Incremental document, closure | 146.8 us | +1.9% |
| Stream, closure | 85.9 ms | +2.1% |

The other Incremental and Stream cases differ by -0.2% to +1.4%; `encoding/csv`, which no change touches,
differs by -1.9%.

## Limits

The Go generator prunes its Node, recognizer and typed tables; the TypeScript generator, which has the same
twin redirect, does not. The expectation table that the Node and recognizer tables fix is kept whole, also in
typed-only output, where the Node functions are not printed or only those that the entry points reach are.
