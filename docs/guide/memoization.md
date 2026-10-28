# Memoization

PEGO memoizes rule results so that backtracking does not evaluate the same rule again and again at the same position. This guide explains
what is memoized and when, and what that means for memory, streaming and incremental parsing. The same holds on every
[backend](backends.md).

PEGO parses with **packrat memoization**: the result of a rule at an input position is remembered, so the rule is
evaluated at most twice at that position, however much backtracking the grammar does. It is on by default, **automatic, and has no public switch**. The engine decides per rule:

- Rules that call no other rule, and rules referenced only once in the grammar, are not memoized in a normal parse,
  because their entries could never be reused.
- Other rules are memoized at a position from their second call there: most rules are called only once per position,
  and an entry that is never reused only costs memory. A rule whose calls do repeat is memoized from the first call
  for the rest of the parse.
- Rules in a left-recursive cycle are not memoized one by one; only the cycle's leader, the rule that grows the seed, is.
- Rules that read [variables](../../spec/language/predicates.md#interaction-with-memoization) are memoized per combination of
  those variables' values, so memoization never changes the result; it only changes the cost.

What this means for you:

- **Parse time is not guaranteed to be linear.** Memoization bounds the re-evaluation of rule calls. It does not bound
  a repetition that is run again after backtracking, the growth of a left-recursive rule, or a rule whose result
  depends on variables with many distinct values; these can take more than linear time on some inputs.
- **Memory grows with input size.** The memo table, like the tree, is proportional to the input. For very large or
  unbounded input use a `#stream` repetition and `ParseStream`, which discards memo entries and input already
  handed to you (see [streaming.md](streaming.md)).
- **Incremental parsing reuses the memo.** `Document` keeps it between edits, and memoizes the rules that a normal parse
  skips, so that more of a previous parse can be reused.
- **Generated parsers memoize in the same way.** Memoization decisions are made by the same analysis and are saved in
  `.pegoc` files.
- The design is in [003-packrat-parsing.md](../design/records/003-packrat-parsing.md), and the history of how the engine's
  speed was tuned (including the memo table) in [performance.md](../performance.md).

## Related Topics

- [Choosing a Backend](backends.md): the engines that share the memo design.
- [Incremental Parsing](incremental.md): a `Document` keeps the memo between edits.
- [Streaming](streaming.md): discarding memo entries for input of any size.
- [Debugging and Profiling](debugging.md): finding why a parse is slow.
