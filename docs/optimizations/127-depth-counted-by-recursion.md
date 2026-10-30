# 127. The Depth Limit Counts Recursion

- **Status**: Implemented
- **Date**: 2026-10-31

## Problem and Mechanism

`MaxDepth` bounds the nesting of a parse so that deep input ends with an error instead of
exhausting the stack. Every rule entry used to increment and check the depth counter, although a rule
that is on no cycle of the rule call graph appears at most once on the call stack and cannot cause
unbounded growth.

A rule now counts only if it lies on a cycle of the call graph or calls itself. The compiler computes
the set from the full rule reference graph (strongly connected components), marks each such rule, and
the mark travels in the compiled program, the bytecode rule table and the generated parser's rule
table. The IR verifier checks that every cyclic rule is marked. The extra stack that the uncounted
rules use is bounded by the number of rules. Levels of an operators rule always count. The first
terminal dispatch guard charges depth only for calls of recursive rules.

Value-free twins share the mark of the rule they copy. The stream start rule is identified by an
explicit identifier rather than by `Depth == 1`, so a start rule outside every cycle needs no count.

The limit is now a limit on nested recursive rule calls. The engine, the bytecode virtual machine, the
iterative virtual machine, generated Go (ordinary, budget-controlled and typed) and generated
TypeScript agree on where it is hit; a differential fixture covers recursion through helper rules.

## Results

Generated parsers shrink because most rule entries no longer carry depth code (`p.Enter()` calls in
the DuckDB parser fall from 1126 to 201):

| Parser | `parser.go` before | after |
|:--|--:|--:|
| DuckDB | 1,588,057 | 1,558,159 |
| Python | 603,000 | 597,468 |
| Go | 524,159 | 522,420 |
| CEL | 112,843 | 112,170 |
| JSON | 58,692 | 57,857 |

Parse time of the generated Go parsers, 12 interleaved runs, benchstat:

| Benchmark | Before | After | Change |
|:--|--:|--:|:--|
| JSON `ParseAST` | 4.687 ms | 4.582 ms | -2.24% (p=0.039) |
| JSON `SmallParseAST` (two sizes) | 2.366 and 2.536 us | 2.335 and 2.549 us | within noise |
| Go `ParseAST` | 28.96 ms | 28.26 ms | -2.43% (p=0.002) |

Allocations are unchanged.
