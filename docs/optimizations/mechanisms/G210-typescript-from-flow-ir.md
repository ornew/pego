# G210: Generated TypeScript Printed from Flow IR

Generated TypeScript prints its rules, Operators Rule lines and outlined nodes from the Flow IR that
generated Go uses, so a rewrite of the Core or Flow IR reaches both.

## Mechanism and Invariants

Every rule of the Node and recognizer tables is lowered by `flow.Lower`, `flow.LowerFrame` or
`flow.LowerOutline` and printed by `gen_ts_flow.go`: a block becomes a labeled block, a loop `for (;;)`,
a condition a Flow IR expression. A rule that Flow IR does not print fails generation; there is no second
printer.

The printer keeps the text small and the functions few:

- Printing runs dry to a fixpoint of what is read, so a mark part (position, environment, trail,
  recovered count) is declared only as far as it is read, and an unread assignment is not printed.
- Consecutive step charges merge into one `p.stepWork(n)`, and an expectation unisolate followed by a
  merge of the same variables is one `p.unisolateMerge(mark)` that merges in place.
- Functions whose printed bodies are equal share one method (`tsGen.methodName`). The key of a function
  replaces each constant it names (a literal table, the capture names, a bind function) by the declaration
  of the constant, so functions that read equal tables under different names share one method although the
  literal tables themselves are not shared (`tsSharedKey`).
- Framed nodes that are large, or whose structure occurs more than once, run as functions of their own
  (`flow.CallOutline`), so no function grows with the grammar and equal nodes share a body
  (`gen_ts_outline.go`).
- A recovered error carries the header line only in `stack`; `recoveredError` suspends
  `Error.stackTraceLimit` while it builds the error, because capturing a stack per recovered error
  dominated recovery time. The error of a failed parse keeps its stack.

Work charges follow the work model of the IR: one at rule entry and one per loop iteration, printed only
with `-work-controls`.

## Scope and State

Default for generated TypeScript, Node and recognizer tables, with and without `-types`.

## Reference Control

None. The generated parsers are compared with the engine oracle over the corpus in both forms
(`TestGeneratedTSParsersMatchEngine`), and the Core and Flow IR goldens freeze the decisions.

## Evidence and Implementation

Record 122 measures the printer against the grammar-tree printer it replaced, in six rotated rounds
on Node 24. The old printer always charges work, so its output is the controlled form; the comparison
below sets it against the new default form, without work controls, and the like-for-like comparison with
`-work-controls` is in the record.

- Speed: parse and recognition of JSON, CSV and XML are 17% to 46% faster (parse 0.83x, 0.83x and 0.68x,
  recognition 0.70x, 0.61x and 0.54x).
- Typed workloads: of nine workload and mode pairs, the two recovery ones are 4% (node) and 6% (tree)
  slower with ranges that do not overlap, the lr node workload is 8% faster with ranges that do not
  overlap, and the other six have overlapping ranges, among them fold with a tree, whose median is 12%
  lower but whose old range spans 2.12 to 3.35 ms.
- Size: `parser.ts` is 16% to 23% smaller in bytes for every grammar but DuckDB, which is 14% larger, but
  it is larger after gzip for every grammar, by 6% to 46% (JSON +13%, CSV +15%, XML +6%, CEL +32%,
  Python +19%, DuckDB +46%): the printed text is shorter and less repetitive. Sharing outlined functions
  of equal structure removes 691 of the 1,924 outlined functions of DuckDB and 3.8% of its bytes; the
  rest of its growth is not explained.
- Work controls: the plain form is at most 5.5% from the controlled form in time (CSV recognition is faster
  plain; XML parsing is 4.6% slower plain with overlapping ranges) and 0.9% to 3.1% smaller.

Evidence records: [122](../122-typescript-from-flow-ir.md).

Source owners: [`Flow IR printer`](../../../internal/engine/gen_ts_flow.go), [`outlining`](../../../internal/engine/gen_ts_outline.go), [`Flow IR lowering`](../../../internal/ir/flow/lower.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G050](G050-generated-work-control-specialization.md), [G190](G190-typed-recovery-bodies.md).

[Optimization Catalog](../README.md)
