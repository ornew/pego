# 130. Memoizing the Members of Left-Recursion Cycles

- **Status**: Implemented
- **Date**: 2026-10-31

## Problem and Mechanism

The rules of a left-recursion cycle other than its leader used to be evaluated at every call. With

```
main = A $$
A = C "+" / "n"
C = "(" C ")" "y" / "(" C ")" "z" / A
```

and the input `(` n times, `n`, `)z` n times, each call of `C` at a position evaluates `C` at the next
position through two alternatives, so the evaluations double with each level: 2^n on every backend. The same
grammar with `A = "n"` is linear, because `C` is then memoized.

A member of a cycle is now memoized like any other rule, with three rules that keep every reused result equal to
the result of an evaluation:

- **A result that read a growing seed is not stored.** A growing leader holds a provisional result at its position,
  and a call that reads it depends on the step of the growth that produced it. The parser counts the left recursions
  that grow (the growth depth) and stamps each growing result with its depth. Each memoized call tracks the least
  depth of the growing results that it, and the calls it made, read. A call that read the seed of a growth that was
  already in progress when it started stores nothing, and the next step evaluates it again. Seeds of growths that
  began and ended within the call are final, so reading them does not prevent storing.
- **While a leader grows at a position, a member result there is evaluated again instead of reused.** A member
  result stored before the growth started may have read an earlier result of the leader at that position. This
  happens when the leader grew there inside a lookahead, and the call outside the lookahead does not reuse that
  silent result and grows the leader again. Only calls at the position of a growth read its seed, and the starts of
  the active growths do not decrease from the outermost to the innermost, so the parser checks the innermost growth
  only.
- **A result that read an unstable result is not stored.** A leader can grow again at its position in one parse
  when its result is reused only in one kind of context: it was computed inside a lookahead, or it recovered
  errors. If that growth read the seed of an enclosing growth, the new growth can read other results and have
  another result. Such a leader result is marked unstable, and every call that reads it, at any position, counts as
  having read a seed of depth 0, so no enclosing call stores its result.

Every other result is a function of the rule, the position and the variables, and is stored. The leaders keep their
own memoization. The engine, the generated Go runtime (untyped and typed) and the TypeScript runtime implement the
same three rules. The compiled program marks the members (`Member` in the rule table, the `member` flag of
disassembly and the `RuleMember` flag of generated Go), so the check of the second rule is one comparison.
Documents keep their own rule: a result that depends on an unfinished growth is provisional and is not reused
after an edit.

In the example, `C` at the next position is reached from the growth of `A` at the earlier one, but outside the
growth of `A` at the next position. It reads no seed, is stored by its first evaluation, and the second
alternative reads it. Only the member calls at the position of a growth are evaluated again at each step. How many
there are depends only on the grammar.

## Applicability

| Execution profile | Applied / Partial / Not applied / Not applicable | Reason |
|:--|:--|:--|
| Closure, bytecode, iterative bytecode | Applied | One shared call-end rule in the engine |
| Generated Go, ordinary and budget-controlled | Applied | `Call`, `callMemo` and `grow` of the runtime |
| Generated typed Go | Applied | `TParser` call paths and `grow` |
| Generated TypeScript | Applied | `callBody` and `grow` of the runtime |
| Documents | Applied | Members are memoized under the same rules, and provisional results are not reused after an edit |

A member that is called from one place is still transient, so a grammar like the left-recursive calculator
(`add`, `mul`) keeps its call code; its generated parsers only carry the member flag of those rules.

## Reproduction

Base `f52b6bd9`, Go 1.27.1, Apple silicon, benchlock, started with a load below 8. Test binaries of the `bench`
package, one per revision, ran interleaved for 10 rounds with `-benchtime=200ms -benchmem` over `Parse` and
`Recognize` (left-recursive calculator, JSON, Minilang), `Incremental`, `IncrementalLong` and `Stream`; benchstat
compared them. Tests: `TestLeftRecursionMemberMemoLinear`, `TestLeftRecursionMemberMemoMatchesNoMemo`,
`TestLeftRecursionMemberMemoStale`, `TestLeftRecursionMemberMemoRandom`,
`TestGeneratedLeftRecursionMemberMemoLinear`, `TestGeneratedLeftRecursionMemberMemoStale`,
`TestGeneratedTSLeftRecursionMemberMemoLinear`, `TestGeneratedTSLeftRecursionMemberMemoStale`.

## Results

Rule evaluations on the example (`Profile.Evals`, the same on the three engine backends):

| n | 8 | 16 | 32 | 64 |
|:--|--:|--:|--:|--:|
| Two members (`A`, `C`) | 25 | 41 | 73 | 137 |
| Four members, two call sites each | 85 | 149 | 277 | 533 |

Before the change the example runs out of the default budget (2^20 units) at n = 16. After it, generated Go
(ordinary and controlled, untyped and typed) and generated TypeScript parse n = 300.

Speed (medians of 10 interleaved samples against the base; differences within noise unless noted):

| Workload | Change |
|:--|:--|
| Parse, left-recursive calculator | +1.6% to +2.3% (p ≤ 0.043) on recursive and iterative bytecode in code points, iterative in bytes, generated Go in bytes and generated typed `ParseAST`; the other cells within noise |
| Recognize, left-recursive calculator | +1.6% to +2.2% (p ≤ 0.035) on closure, iterative and generated Go; bytecode within noise |
| Parse and Recognize JSON and Minilang | within noise, except iterative JSON parses 1.0% to 1.6% faster |
| Incremental (Minilang), IncrementalLong (CSV), Stream (CSV) | within noise on every backend |

The cost on the calculator comes from the bookkeeping of every memoized call and growth: saving and merging the
least depth read, and the member check on a hit. Allocations are unchanged except +0.3% (7 of 2,291) on one
iterative cell. The memo entry of the generated runtimes stays at 128 bytes, because one field holds both the depth
of a growing result and the unstable mark of a final one. Generated Go for the calculator differs only in the
member flags of `add` and `mul` in the rule tables: 24 to 58 bytes more per generated form.

## Correctness and Tradeoffs

A result that read a growing seed is never stored, which is stricter than invalidating it when the seed grows: it
needs no per-growth state, and a stored result never depends on a seed that is still growing. The price is that a
member call at the position of a growth is evaluated again at every step of the growth, a number of calls that
depends only on the grammar.

The unstable rule is conservative: it also applies to a rule outside the cycle that reads an unstable result, and
to every call that encloses the read. Such results need a leader that grows inside a lookahead, or recovers an
error, at the position of another growth whose seed it reads; a grammar where this happens can lose memoization at
the calls that enclose the read, which
[Conditions for Linear Cost](../../spec/runtime/parsing.md#conditions-for-linear-cost) states as its own condition.

A differential test compares every engine backend (with and without inserted tokens, and in recognition), generated
Go (untyped and typed, ordinary and budget-controlled) and generated TypeScript with the same grammar compiled
without a memo. Its inputs are the cases that a stored member result made stale, grammars found by a random search
over small cycles with lookaheads, optional and repeated items, nested choices and cuts, and an Operators Rule leader
read at two binding levels. Removing any one of the three rules makes the test fail on some backend.

Budget totals of the controlled form change only on the grammars of the corpus that have memoized members. The
budget-controlled generated parser defers memoization to the second call of a rule at a position, and charges the
bitmap of first calls when a parse first defers a call: 64 words, once per parse
(`controlledFirstCallPrepare` in `runtime/budget.go`). A grammar whose only memoized rules are leaders never builds
the bitmap. In the corpus grammars `unreachable LR tail` and `unreachable LR alias tail`, the cycle of `r1 = r2?`
is led by `r1`, and its member `r2` is called from three places, so it is now memoized, and each parse pays the bitmap plus the steps of the memoized call
path: 65 to 82 units more per input (for example 51 to 130 on the empty input). Grammars that already memoize a rule
other than a leader pay the bitmap with or without this change. The results of every case are unchanged.

## Decision and Limits

Adopted on every backend. Not measured: grammars with many members called from several places, where
memoization could cost more than it saves, as no example grammar of the repository has them.
