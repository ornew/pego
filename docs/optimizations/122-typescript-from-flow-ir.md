# 122. Generated TypeScript Printed from Flow IR

- **Status**: Implemented
- **Date**: 2026-10-28

## Problem and Mechanism

The TypeScript generator lowered each parsing expression to TypeScript text on its own path
(`tsGen.expr` and helpers): conditions were Go text with `==` rewritten to `===` and rebalanced by
counting parentheses, every expression that was not a literal, a character or a rule call became a
function, and an optimization written for the Flow IR of generated Go did not reach TypeScript.

Every rule, Operators Rule line and outlined node is now printed from the same Flow IR as generated
Go. A block is a labeled block, a loop is `for (;;)`, a condition is a Flow IR expression. Printing
runs dry to a fixpoint of what is read, so mark parts and variables are declared only as far as they
are read; consecutive step charges and an expectation unisolate followed by a merge are each one
statement; functions with equal printed bodies share a method; framed nodes that are large or
structurally repeated run as functions of their own. Recovery runs inline under the runtime-owned
frame. A recovered error carries only the header line in `stack`: capturing a stack for each
recovered error dominated recovery time, and `recoveredError` suspends `Error.stackTraceLimit`
while it builds one. The error of a failed parse keeps its stack. A rule that Flow IR does not
print fails generation.

## Results

Node 24.13.0 on darwin/arm64, medians of six rounds that rotate three generators: the grammar-tree
printer (before; its output always spends budget, so it is the controlled form), and the printer of
this change in the default form (after, without budget controls) and with `-budget-controls`. Inputs of
`bench/` (`ts-parse.mjs`; recognition by a harness that runs `recognize` in a loop). The comparison of
the before column with the after column sets the old controlled form against the new plain form; the
column with controls compares like with like:

| Grammar | Parse before → after (ms) | after, with controls | Recognize before → after (ms) | after, with controls |
|:--|--:|--:|--:|--:|
| JSON | 26.21 → 21.71 (0.83x) | 21.93 (0.84x) | 12.96 → 9.10 (0.70x) | 9.05 (0.70x) |
| CSV | 10.96 → 9.13 (0.83x) | 9.18 (0.84x) | 4.65 → 2.83 (0.61x) | 3.00 (0.65x) |
| XML | 34.62 → 23.49 (0.68x) | 22.46 (0.65x) | 23.11 → 12.43 (0.54x) | 12.39 (0.54x) |

Typed workloads of `bench/ts-typed.mjs` (the `TestGeneratedTSTypedWorkloads` and
`TestGeneratedTSTypedSharedDAG` fixtures, large size, code points, six rounds), time per parse in
milliseconds:

| Workload, mode | before | after | ratio |
|:--|--:|--:|--:|
| capture-free, node | 0.175 | 0.168 | ranges overlap |
| fields, node | 1.117 | 1.126 | ranges overlap |
| fields, ast | 2.361 | 2.321 | ranges overlap |
| fold, node | 1.533 | 1.465 | ranges overlap |
| fold, ast | 2.628 | 2.324 | ranges overlap (2.12–3.35 and 2.13–2.57) |
| lr, node | 1.757 | 1.622 | 0.92 (ranges do not overlap) |
| lr, ast | 2.657 | 2.615 | ranges overlap |
| recovery, node | 1.382 | 1.439 | 1.04 |
| recovery, ast | 2.053 | 2.168 | 1.06 |

Recovery with stack capture would be 19% slower than before; the stack-free recovered errors and the
in-place unisolate merge leave 4% to 6%, with ranges that do not overlap. The slowdown is spread over `e3`,
`invoke` and `callBody` in the profile. A trace of deoptimizations shows nothing in the parser, outlining
every repeated node (`outlineDuplicate=6`) gives the same time, and removing the loop locals by hand does
not change the fields workload; the cause is not identified.

`parser.ts` size in bytes and gzip (`gzip -9`), before → after (default form):

| Grammar | bytes | gzip |
|:--|--:|--:|
| JSON | 72,841 → 61,249 (-16%) | 6,686 → 7,560 (+13%) |
| CSV | 30,882 → 23,815 (-23%) | 3,887 → 4,461 (+15%) |
| XML | 323,345 → 254,384 (-21%) | 24,206 → 25,706 (+6%) |
| CEL | 418,070 → 330,829 (-21%) | 25,438 → 33,614 (+32%) |
| Python | 2,477,263 → 1,902,241 (-23%) | 164,918 → 196,110 (+19%) |
| DuckDB | 5,654,358 → 6,439,707 (+14%) | 286,719 → 417,919 (+46%) |

The printed text is shorter but less repetitive, so it compresses less: after gzip every grammar is
larger than before, by 6% to 46%. Outlined nodes of equal structure share one function although each
reads its literal tables under a name of its own (the literal tables are not shared; sharing them
slows warm-up); this removes 691 of the 1,924 outlined functions of DuckDB and 3.8% of its bytes
(6,691,955 to 6,439,707). The remaining growth of DuckDB is not explained; a profile that outlines more
(`outlineLarge=24`, `outlineChild=4`) makes DuckDB 0.8% smaller than the default and leaves speed within
noise on JSON, CSV and XML, and is not the default.

Without `-budget-controls` the rules, actions and the typed runtime emit no charge. Against the
controlled form of the same grammar the plain `parser.ts` is 0.9% smaller for DuckDB, 1.2% for CEL,
1.3% for JSON, 1.5% for CSV, 1.8% for XML and 3.1% for Python. Parse and recognition time of the plain
form are within 1% of the controlled form for JSON and for XML recognition; CSV recognition is 5.5%
faster plain, and XML parsing 4.6% slower plain by the median, with overlapping ranges (21.7 to 23.6
and 22.4 to 22.7 ms), so the gap is at most 5.5% and below noise except for CSV recognition.

## Tests

The generated parsers match the engine over the corpus (330 grammars, 29,252 results, both position
units) with values, with source information and with budget controls; a sweep of mutated inputs shows no
difference from the engine. The typed, replay, budget-control, observation and profile tests, the Core and
Flow IR goldens and `parsers/test.sh` pass.
