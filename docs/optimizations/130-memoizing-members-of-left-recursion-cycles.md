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
(`add`, `mul`) generates the same ordinary code as before.

## Reproduction

Base `f52b6bd9`, Go 1.27.1, Apple silicon, benchlock with a load below 8. Two test binaries of the `bench`
package, one per revision, ran interleaved for 12 rounds with `-benchtime=300ms -benchmem`; benchstat compared
them. Tests: `TestLeftRecursionMemberMemoLinear`, `TestLeftRecursionMemberMemoMatchesNoMemo`,
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

Speed (medians of 12 interleaved samples, all differences within noise unless noted):

| Workload | Change |
|:--|:--|
| Parse, left-recursive calculator, six engine backends, generated Go | within ±0.2% except one engine cell at +0.7% (p = 0.045) |
| Recognize, left-recursive calculator, generated | +0.7% (p < 0.001), engine backends unchanged |
| Parse and Recognize JSON (control) | no difference on any backend |

Allocations are unchanged. Generated Go for the calculator is byte-identical outside Documents; the Document
forms of the calculator parser grow by 26 bytes. Controlled and ordinary generated Go on the left-recursive
calculator: 16.75 ms and 14.24 ms for the same input, the cost of the budget control that is paid with or
without this change.

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

## Decision and Limits

Adopted on every backend. Not measured: grammars with many members called from several places, where
memoization could cost more than it saves, as no example grammar of the repository has them.
