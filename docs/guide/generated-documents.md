# Incremental Documents in Generated Go

Generate with `pego gen -g grammar.pego -pkg parser -document -o parser/parser.go`, or pass
`pego.WithDocument()` to `GenerateGo`. This adds a mutable Node `Document` alongside the ordinary batch `Parse` and
`ParseRule` functions. The engine's version of the same feature is described in
[Incremental Parsing](incremental.md).

## Editing and Reparsing

```go
doc, err := parser.NewDocument("first draft", parser.WithUnit(parser.CodePoints))
if err != nil {
	return err
}
tree, err := doc.Parse()
if err := doc.Edit(6, 11, "revision"); err != nil {
	return err
}
tree, err = doc.Parse()
stats := doc.Stats() // rule evaluations and reused memo results from this Parse
_ = tree
_ = stats
```

`Edit(start, end, replacement)` replaces the half-open range in the unit selected at construction. It rejects
invalid ranges and positions inside a UTF-8 character in `Bytes` mode without changing the text. `Text()` returns
the exact current input. Construction options become defaults for later `Parse` calls; per-call options can change
depth, context and work limits, but cannot change the unit. An interruption during execution discards memo history so a
later call can retry. `Document` is single-goroutine, and results can share nodes with later parses. Call
`tree.Clone()` if an earlier tree must remain detached from the Document. It copies Node graphs, including Node
values in `[]any` and `[]*Node` fields, while preserving cycles and repeated references to the same slice view.
Overlapping slice views with different lengths or capacities are copied independently; arbitrary mutable action
values such as maps remain shared. `ParseStats` has `Evaluated` and `Reused` fields.

## What Is Kept Between Parses

This first generated implementation retains memo entries wholly before an edit and discards the suffix, rather
than moving suffix nodes. `Edit` scans and copies retained prefix entries, so its cost can grow with document
length. The generated batch functions remain available. `-document` can be combined with `-types`: eligible
grammars use direct typed matching with a private memo and fresh invocation arenas. The published typed graph is
cloned so later parses cannot overwrite prior results and caller mutation cannot change private memo values or
later results. Results preserve cycles, aliases and repeated references to the same slice view, but not slice
capacity or arbitrary overlapping views. Other grammars use the existing Node Document and convert its final
result on each call; CST `Node` members may share the mutable Document graph, so call `Node.Clone` when a detached
tree is needed. The conversion fallback applies to `any`/CST results, struct fields requiring PEGO nodes, or actions
and predicates that read struct fields.

## Diagnostics, Limits and Combinations

Both `Document.Parse` and `Document.ParseAST` return owned syntax diagnostics: recovered `SyntaxError` values and
their `Expected` and `Messages` slices are copied; ordinary failures are freshly constructed. Mutating a returned
error cannot affect memoized errors or a later parse. Parsing, diagnostic copying and typed publication share one
work budget; interruption returns the result type's zero value and clears reusable state as needed for a retry.
`-document` cannot be combined with `-lang ts`; engine-only Document controls such as `EditBatch` are separate work. See the [direct typed measurements](../design/044-generated-incremental-and-stream-parsers.md#local-measurements-for-direct-typed-document-matching)
and the [conversion fallback measurements](../design/044-generated-incremental-and-stream-parsers.md#local-measurements-for-typed-go-document-conversion).

## Related Topics

- [Incremental Parsing](incremental.md): the engine's `Document` and what reuse means.
- [The Generated Go API](generated-go-api.md): the entry points that `-document` sits beside.
- [Typed Values in Generated Go](generated-typed-values.md): `-types`, which `Document.ParseAST` needs.
