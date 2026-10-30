# 126. Unreachable Rule Bodies in Generated TypeScript

- **Status**: Implemented
- **Date**: 2026-10-31

## Problem and Mechanism

The TypeScript generator redirects a call from a value-free context to the rule's value-free twin, as the Go generator
does, so the original body of a rule that only such contexts reference is never called, and neither are the originals
that only it calls. It printed every rule of the Node and recognizer tables anyway.

The reachability of [record 120](120-unreachable-rule-bodies.md) now comes from one pass on the Core IR that both
generators use (`core.Module.Reach`, `internal/ir/core/reach.go`). The call graph of the IR (`Module.Calls`) holds the
calls of every rule body, so no call is recorded while printing, and the pass takes the two things that differ by
target: the roots and how a call of a rule is entered, through the rule constant (`p.call`, `p.invokePlain`) or through
the plain function of a rule without captures (`r<N>(p)`).

For TypeScript (`internal/gen/tsgen/reach.go`):

- The roots are every start rule of the Node table, which `parseRule` can name at run time and `starts` lists, the
  default start rule, and the recognizer's start rule. `startIn` accepts only names in `starts`, so no other rule can
  be entered from outside.
- The printer prints only the reachable rules. A rule left out has no constant, body, action, plain function or table
  row, and the helpers, literals and expectations that only it would have declared are not declared. A rule entered
  only through its plain function has no `body` assignment; one that also has no value has no constant, because its
  plain function never finishes a value with it. Rule IDs and the numbering of memoized rules are those of the compiled
  program, so the remaining rules keep their values; the expectation table is numbered without the rules left out.
- Generation fails if a printed call reaches a rule left out.

`startIn` told "rule X is not defined" from "rule X is not a start rule" by looking the name up in the rule table. The
generated parser now passes the names of all rules (`ruleNames`) as well, so both messages are unchanged when the
table holds fewer rules.

## Results

Bytes of `parser.ts` (`pego gen -lang ts -types -recognize -runtime import`):

| Grammar | Before | After | Change |
|:--|--:|--:|--:|
| cel | 342,538 | 278,259 | -18.8% |
| csv | 31,372 | 28,713 | -8.5% |
| cue | 1,132,433 | 869,174 | -23.2% |
| duckdb | 6,481,251 | 4,829,804 | -25.5% |
| golang | 1,917,555 | 1,297,792 | -32.3% |
| json | 69,778 | 56,602 | -18.9% |
| python | 1,922,541 | 1,427,308 | -25.8% |
| typescript | 3,294,331 | 2,340,596 | -29.0% |
| xml | 266,720 | 255,756 | -4.1% |
| yaml | 557,945 | 453,264 | -18.8% |
| calculator (calc) | 30,809 | 29,586 | -4.0% |
| calculator (calc_lr) | 41,257 | 39,582 | -4.1% |
| minilang | 143,110 | 130,471 | -8.8% |
| outline | 31,146 | 28,057 | -9.9% |
| Total | 16,262,786 | 12,064,964 | -25.8% |

The removed functions are never called, so parse results and speed do not change; the corpus comparison of the
generated parsers against the engine (330 grammars, 29,252 results) is unchanged.

## Correctness and Tradeoffs

The tests check that a rule referenced only from value-free contexts or by nothing has no constant, that every rule
name stays in `ruleNames`, and the start-rule errors of the generated parser against the interpreter's.

## Limits

A typed Go parser has nothing further to leave out at the level of functions: `trules[N].Body` of a rule entered only
through its wrapper is already not printed (leaving it out of the slot test changes no parser of `parsers/`), and a
reachability scan from `init`, the exported names and the methods finds no unreferenced top-level declaration in
`cel`, `cue`, `duckdb`, `golang`, `python` or `typescript`. What remains per unreached rule is its row in the rule
table (`rulesSpec`, `rulesStrs` and `rulesExtras`), 2.6% (`cel`) to 4.1% (`duckdb`) of `parser.go`. The rows stay: the
typed table is built from them and indexed by rule ID, and the rule names answer the start-rule errors.
