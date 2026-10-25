# Incremental Documents in Generated Go

Generate with `pego gen -g grammar.pego -module example.com/parser -document -o parser`, or pass
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

Both kinds of Document retain memo entries wholly before an edit and discard the suffix, rather than moving suffix
nodes. The Node Document copies the retained prefix entries during `Edit`, so its cost can grow with document
length. A typed Document scans the retained prefix and relinks its entries in place, without copying them. The
generated batch functions remain available. `-document` can be combined with `-types`: eligible
grammars use direct typed matching with a private memo and fresh invocation arenas. A typed result is
read-only and shared, as Node values are: later parses reuse the values of earlier results, so a result can share
values with the Document and with other results. Modifying a result can change what later parses return, including
through a `node[_]` field. Call `CloneAST(value)` to get a copy that shares nothing with the Document or with other
results; the copy can be modified freely, and aliases within it are preserved. Every returned slice has capacity equal
to its length, so appending to one result cannot write into another. Other grammars use the existing Node Document and
convert its final result on each call; generic `Node` members may share the mutable Document graph, so call
`Node.Clone` when a detached tree is needed. The conversion fallback applies to `any` and node results, struct fields
requiring PEGO nodes, or actions and predicates that read struct fields.

## Diagnostics, Limits and Combinations

Both `Document.Parse` and `Document.ParseAST` return owned syntax diagnostics: recovered `SyntaxError` values and
their `Expected` and `Messages` slices are copied; ordinary failures are freshly constructed. Mutating a returned
error cannot affect memoized errors or a later parse. Parsing, diagnostic copying and typed publication share one
work budget; interruption returns the result type's zero value and clears reusable state as needed for a retry.
`-document` cannot be combined with `-lang ts`; engine-only Document controls such as `EditBatch` are separate work. See the [direct typed measurements](../design/records/044-generated-incremental-and-stream-parsers.md#local-measurements-for-direct-typed-document-matching)
and the [conversion fallback measurements](../design/records/044-generated-incremental-and-stream-parsers.md#local-measurements-for-typed-go-document-conversion).

## Related Topics

- [Incremental Parsing](incremental.md): the engine's `Document` and what reuse means.
- [The Generated Go API](generated-go-api.md): the entry points that `-document` sits beside.
- [Typed Values in Generated Go](generated-typed-values.md): `-types`, which `Document.ParseAST` needs.
