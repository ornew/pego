# Streams in Generated Go

Generate with `pego gen -g grammar.pego -module example.com/parser -stream -o parser`, or pass `pego.WithStream()` to
`GenerateGo`. The start rule must have a [`#stream`](../../spec/pragmas.md#stream) repetition; otherwise generation
fails. This adds a Go `ParseStream` beside the ordinary `Parse`, which still treats `#stream` as a plain repetition.

## Parsing a Stream

```go
err := parser.ParseStream(r, func(n *parser.Node) error {
	fmt.Println(n.Kind(), n.Start(), n.End())
	return nil
}, parser.WithUnit(parser.Bytes))
```

`ParseStream` follows the engine's [stream contract](streaming.md): it reads only as much input as matching needs,
hands each element of the start rule's `#stream` repetition to the callback as soon as it matches, and then discards the
element with the input and memo entries before it. Elements are final and own their text. A callback error stops the
parse and is returned unchanged; a non-EOF reader error ends it as well. A nil callback parses the stream without
handing elements over. Recovered diagnostics are returned together as `SyntaxErrors` at the end, and positions and
lines stay absolute. `WithContext`, `WithWorkLimits`, `WithMaxDepth` and, with `-observability`, tracing and profiling
apply to the whole invocation; a cancelled or limited stream checks between reads and elements, and a reader blocked
in `Read` is not interrupted. `-stream` cannot be combined with `-lang ts`. The measurements are in
[design record 044](../design/044-generated-incremental-and-stream-parsers.md#local-measurements-for-generated-go-node-streams).

## Pull Iteration and Diagnostic Controls

The package also has the engine's pull iteration and diagnostic controls, with the same contracts as in
[streaming](streaming.md):

```go
it, err := parser.Iterate(r, parser.WithDiagnosticCallback(func(e *parser.SyntaxError) error {
	log.Print(e) // a committed recovery, just before its element
	return nil
}), parser.WithDiagnosticLimit(100))
if err != nil {
	return err // invalid options; r has not been read
}
defer it.Close()
for {
	n, ok, err := it.Next()
	if !ok {
		return err // nil at a clean end, or the result ParseStream would return
	}
	use(n) // nil for an element without a value
}
```

`Iterate` returns a `*StreamIterator` whose `Next` parses only as far as the next committed element; `Close`
abandons the parse without closing `r`, and `Next` then returns `ErrStreamClosed`. `WithDiagnosticCallback`
receives a copy of each committed recovered error before its element, and its error ends the parse without that
element. `WithDiagnosticLimit(n)` keeps only the latest `n` recovered errors and returns `*TruncatedSyntaxErrors`
when it dropped some, so a long faulty stream holds a bounded number of them. Both options are accepted only by
the stream entry points; `Parse`, `ParseRule`, `ParseAST`, `Recognize`, `NewDocument` and the `Document` methods
reject them.

## Typed Elements

With `-types`, the package also has

```go
type StreamValue = ... // the typed value of one element of the #stream repetition
func ParseStreamAST(r io.Reader, emit func(StreamValue) error, opts ...ParseOption) error
func IterateAST(r io.Reader, opts ...ParseOption) (*StreamASTIterator, error) // Next returns StreamValue
```

`ParseStreamAST` behaves like `ParseStream` but passes each element as the value its expression has in the
[Go type mapping](generated-typed-values.md#mapping-grammar-types-to-go) — a rule's declared type, a join, a list, a `*TNode[T]` or a tuple — and the zero value for an element without a value. `StreamValue` is the type of the repeated element,
not of the start rule:

```go
// rule main = item* #stream
// type Pair = { Key string; Value string }
// rule item Pair = k:[a-z]+ "=" v:[0-9]+ "\n" => Pair{Key: text($k), Value: text($v)}
err := parser.ParseStreamAST(r, func(p *parser.Pair) error {
	fmt.Println(p.Key, p.Value)
	return nil
})
```

Each element is converted on its own once it is committed, so values stay valid after the callback returns and share
nothing with later elements. The conversion counts toward `WithWorkLimits` and observes `WithContext`.

## Related Topics

- [Streaming](streaming.md): the stream contract, shared by the engine and the generated parsers.
- [The Generated Go API](generated-go-api.md): the entry points that `-stream` sits beside.
- [Typed Values in Generated Go](generated-typed-values.md): `-types`, which `ParseStreamAST` needs.
