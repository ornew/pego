# 014. Tracing and Profiling

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-08

## Summary

A parse can report every rule call to a function (`pego.WithTrace`): the rule, its position and binding level, the
nesting depth, and at the end of the call its outcome, end position, whether the memo answered it, how many times it
evaluated the body, how far it examined the input, and the farthest failure it recorded. A per-rule profile
(`pego.Profile`, `pego.WithProfile`) is built on the same events. The `pego` command gains `trace`, `profile` and
`explain`. Tracing works on the closure backend and both bytecode VMs, in recognition mode, in stream parses and in
`Document`; generated Go parsers do not have it. When tracing is off, an ordinary parse pays one nil check per call
that goes through the general call path, and nothing on the plain call path. The user guide is
[docs/guide/debugging.md](../guide/debugging.md).

## Motivation

Grammar authors could see only the result of a parse: a tree or a syntax error listing what was expected at the
farthest failure. Why that list contains what it does, which alternatives a choice tried, and why a grammar is slow
could only be found by editing the grammar and parsing again, or by reading Go CPU profiles in which every rule is the
same closure or the same VM loop. The engine already counts evaluations and memo hits per parse (`Stats`), but not per
rule, and nothing tells where in the input the work happens.

The requirements were:

1. Report rule calls with their positions, binding levels, outcomes and memo use, and optionally what failed calls
   expected, for people (an indented call tree) and tools (JSON).
2. A per-rule profile with counts, consumed and wasted input and time, sorted by cost, with hints.
3. No measurable cost when tracing is off, so ordinary parses stay as fast: at most one branch per rule call, none per
   expression.
4. Identical results with tracing on and off, on every supported backend.

## Design

### Where calls are observed

Every rule call of the engine goes through one of two paths:

- `parser.call`, used by all backends (the closure backend's rule references, the recursive VM's `CALL`, the start rule,
  streams and documents), which decides between an unmemoized call, a memo lookup, and left-recursion growth; the
  iterative VM performs the same steps in its `bodyFrame`, pushing a `callFrame` or a `plainCallFrame`.
- The **plain call path**: calls of plain rules (never memoized outside `Document`, no captures) bypass `call` and
  run `invokePlain` (or `plainCallFrame`) directly. This path was added for speed (performance.md, changes 19, 38, 42,
  43) and is the hottest in the engine.

Tracing is attached to the first path only. The call sites of the plain path test `!p.noPlain` where they tested
`!p.memoAll`; `noPlain` is set by `Document` (as `memoAll` is) and by tracing. A traced parse therefore sends every call
through `call`, where plain rules take the same steps they take on the plain path (`call` falls back to `invokePlain`
for them), and an untraced parse runs exactly the instructions it ran before.

`call` begins with `if p.tr != nil`. In a traced parse it hands the call to `tracedCall`, which reports the start,
sets `tr.direct` and calls `call` again; `call` sees `direct`, clears it and proceeds as usual, so the call logic is not
duplicated and calls nested in the body are traced in turn. In the iterative VM, `bodyFrame` tests `p.tr` after the
plain-call test and pushes a `traceFrame`, which reports the start, pushes the frame that `bodyFrame` would have pushed
(`callChild`), and reports the end when that frame returns.

Calls are reported before the memo is consulted, so a memo hit is a call like any other, with `Memo` set in its exit
event.

### What an exit event knows

- **Body evaluations** (`Evals`) come from the parse's evaluation counter (`Stats.Evaluated`, incremented once per body
  evaluation by `invokeBegin`, `invokePlain` and `plainCallFrame`): the difference across the call, minus the
  difference across the calls nested in it. A memo hit is a call with no evaluation at all, so `Memo` needs no hook in
  the memo code. A left-recursion leader evaluates its body once per growth step; the recursive calls in its body are
  memo hits that return the seed.
- **Examined input** (`Examined`): the parser already tracks the end of the input examined by the current memoized call
  (`hw`, kept for `Document`), as a maximum that every character test raises. A traced call saves it, sets it to its
  start, reads it at the end and restores the maximum of both, exactly as `callBegin` and `callEnd` do for memoized
  calls. Since only the maximum is ever used, this leaves every memo entry's examined range unchanged.
- **Expectations** (`Failure`): likewise, a traced call records the expectations made during it separately
  (`isolate`) and merges them back (`mergeExpected`) when it ends, as memoized calls already do. The merge replays the
  recorded expectations into the caller's record, which keeps only those at the farthest position, so the final error
  is the same. The event hands the separate record to `Failure`, which formats it like a syntax error only when called.

Because the events expose the parser (`LineCol`, `Text` and `Failure` read it), they are valid for those methods only
during the trace function; the fields can be kept.

### Profile

`Profile.Trace` is an ordinary trace function. It keeps a stack of calls with their start times and the time of their
nested calls, and per rule name (so that a value-free twin counts as its rule) it adds up:

- calls, memo hits, matches, failures and body evaluations;
- **consumed**: the input matched by matching calls;
- **wasted**: for each call that evaluated its body and failed, the input from its position to the end of what it
  examined. This is precise for what it counts: input the rule examined in a call whose result was a failure. It does
  not count successful calls whose result a caller later discarded (a choice that backtracks out of an alternative
  after some of its calls matched), because a call boundary cannot tell whether its caller will keep its result; the
  re-evaluation that such backtracking causes shows instead as repeats;
- **repeats**: calls that evaluated the body at a (position, binding level) where the rule had been evaluated before in
  the same parse, and the most such evaluations at one position. Since memoization is deferred to the second call at a
  position (performance.md, change 18), a memoized rule has at most one repeat per position; more points at a rule that
  is not memoized and whose callers are retried. The positions evaluated are kept in a map, from which those that a
  stream parse has discarded (and so can never return to) are dropped whenever it doubles, so profiling a stream
  keeps its memory bounded;
- **time**: inclusive time, counted only for the outermost call of a rule in progress (so that recursion does not count
  the same time twice), and self time, the inclusive time minus that of the nested calls.

A new parse begins with an enter event at depth 1, which resets the per-parse state (the call stack and the
positions evaluated), so one `Profile` collects any number of parses, including those of a `Document`.

`Hints` reports the rules with the most self time, rules evaluated more than twice at one position, failed calls that
examined at least 16 positions, and rules whose failed calls examined more input in all than the parse did. They are
deliberately few and name what to look at.

### `pego explain`

`explain` parses once without tracing to get the syntax errors, then parses again with a trace that watches only the
errors' positions. When a call ends with a failure at such a position, the expectations it recorded itself are those
of its `Failure` that no nested call recorded at that position (each call passes everything it recorded on to its
caller). Calls with expectations of their own are listed with the stack of calls they were nested in.

A `#recover` that recovers takes the expectations of the expression it recovered from out of the record of the call
that contains it, so they are not in that call's `Failure`. The exit event gives them as `Recovered`, the errors
recovered during the call; the first call to report an error (the innermost, as calls end innermost first) is the one
whose body holds the `#recover`, and its own expectations are again those no nested call recorded. What remains
unattributed is the end of the input that the parser expects after the start rule returns, which the output states
separately.

### Command-line output

`pego trace` prints a call whose nested calls are not shown on one line (`rule line:col -> result`) and other calls as
an enter line and an exit line around their nested calls. It buffers only the last enter event, so the output streams.
`-rule` and `-max-depth` select calls by rule and by depth relative to the top call shown; `-f json` prints one object
per event. `pego profile` prints the table with `text/tabwriter` and the hints; `-f json` prints the same data.

## Cost

What an ordinary parse executes in addition, exactly:

| Place | Before | After |
|:--|:--|:--|
| `parser.call` (closure, recursive VM, start rule, streams, documents) | | one `p.tr != nil` test at entry |
| Plain call sites (closure rule reference, recursive VM `CALL`, iterative VM `bodyFrame`) | test `p.memoAll` | test `p.noPlain` (same cost) |
| Iterative VM `bodyFrame`, non-plain calls | | one `p.tr != nil` test |
| `parser` struct | | one pointer (`tr`) and one bool (`noPlain`) |

Nothing changes in `invoke`, `invokePlain`, `callBegin`, `callEnd`, the memo, or the code of expressions. The
generated parsers' runtimes (`genrt`) are untouched.

With tracing on, every call also goes through `tracedCall`, isolates its expectations and examined range, and calls the
trace function; plain calls lose their shortcut. The trace function itself (formatting, or the profile's map of
positions and clock reads) dominates.

## Testing

- `check`, which nearly every engine test uses, now also parses each input with a trace on every backend, for the
  grammar and its unmemoized variant, and compares the results (trees, positions, errors) with untraced parses. It also
  checks that the events nest, that each exit matches its enter, and that `Memo` and `Evals` agree.
  Each parse is also repeated through a copy of `ParseWith` that exposes the parser (`parseWork`), and the work it did
  must be the same traced and untraced: `Stats` (evaluations and memo reuses), the number of memo entries, the first
  calls recorded and the rules memoized eagerly, that is, the memoization decisions.
- `TestTraceKeepsResults` does the same for the backend corpus (`genCorpus`: the example grammars with their test
  inputs, the typed-runtime cases and others), in both position units and in recognition mode. For each input it also
  parses a `Document`, traced and untraced, before and after three edits, and compares the results and
  `Document.Stats`.
- `TestTraceEvents` checks the exact event sequences of small grammars on every backend: memo hits, left recursion,
  Pratt levels, lookaheads and `#recover`. Stream and `Document` parses are checked against their untraced results,
  and the evaluations and memo hits the events report against `Document.Stats`.
- Profile tests check the counts, repeats, wasted input and hints on grammars where they are known; the CLI tests check
  the commands' output.

Removing the merge of a traced call's expectations into its caller (one line of `traceExit`) makes
`TestTraceKeepsResults` fail on 516 parses, so the equivalence tests do detect a tracer that disturbs the parse.
Likewise, a tracer that memoized every rule (setting `memoAll`) fails the comparison of the work on 1,338 parses, and
one that gave a `Document` parse a fresh memo fails the document comparison 573 times.

## Alternatives considered

- **Hooks in `invoke` and `invokePlain`.** They see every body evaluation, but they are on the plain path, which would
  then pay a branch per call, and they do not see memo hits.
- **An instrumented copy of the program** compiled when tracing is requested. It would make the untraced path free of
  even the nil check, but it doubles compilation work and memory for a debugging feature, and the bytecode programs
  are shared and lazily built, so every backend would need a traced twin.
- **Wrapping rule bodies** (`rule.body`). Bodies belong to the compiled program, which is shared by concurrent parses,
  so a per-parse hook cannot live there; they also do not see memo hits.
- **Tracing expressions.** Reporting every choice alternative and repetition would show backtracking inside a rule,
  but would put a branch on every expression; rule calls are the unit grammar authors reason in.
- **Go CPU profiles with labels per rule.** `pprof` labels are per goroutine and costly to switch, and they cannot
  attribute memo hits, repeats or wasted input.

## Limitations

- Generated Go parsers have no tracing.
- The backends skip different calls by first-character dispatch, so their traces can differ in calls that fail at
  once; results and memoization decisions do not.
- `wasted` counts failed calls only; work thrown away by a backtracking choice in a successful call shows as repeats.
- Profile times include the cost of tracing, which is large relative to small rules: they are meaningful relative to
  each other.
- The steps of a Pratt expression (operands, operators, the binding-power loop) are part of its rule's body; only calls
  of rules from them are reported.
- An aborted parse (a runtime error in an action, an error from a stream's emit function or reader, the nesting limit,
  a panic in the trace function) ends the trace without the exit events of the calls in progress. A panic in the trace
  function is carried through the parse's recovery as an internal value and raised again unchanged, so that a runtime
  error there is not reported as invalid bytecode on the bytecode backends.
- A `Profile` must not be shared by concurrent parses.
- In a stream parse, `LineCol` knows the lines of the input still held only; for other positions it gives 0, 0 (and a
  profile `Location` prints as `position N`). Keeping the line starts of discarded input would make a stream's memory
  grow with its length.
