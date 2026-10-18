# The Generated Go API

A Go parser written by [`pego gen`](code-generation.md) has a small API defined in the file itself, and options that add
typed values, incremental documents, streams and recognition. This guide describes each of them. It assumes you have
generated a parser as in the [quick start](code-generation.md#quick-start).

- [The generated API](#the-generated-api)
- [Typed values (`-types`)](#typed-values--types)
- [Incremental documents (`-document`)](#incremental-documents--document)
- [Streams (`-stream`)](#streams--stream)
- [Recognition (`-recognize`)](#recognition--recognize)

## The Generated API

Every generated package has the same small API, defined in the file itself:

```go
// Parse parses the whole input with the start rule. Options select position units and the nested rule-call limit.
func Parse(input string, opts ...ParseOption) (*Node, error)

// ParseRule parses the whole input with the rule name.
func ParseRule(name, input string, opts ...ParseOption) (*Node, error)

type ParseOption func(*parseOptions) // parseOptions is internal to the generated package
func WithUnit(unit Unit) ParseOption
func WithMaxDepth(n int) ParseOption

type Unit int
const (
	CodePoints Unit = iota // the default
	Bytes
)

type Node struct { Start, End int32; Text string; Children []*Node; Fields Fields } // and Type() and Rule()
func (n *Node) Field(name string) any
func (n *Node) IsTerminal() bool
func (n *Node) String() string
type Fields []NodeField         // with Get(name) (any, bool); encodes to JSON with sorted keys
type NodeField struct { Name string; Value any }

type SyntaxError struct { Pos, Line, Col int; Expected, Messages []string }
func (e *SyntaxError) Error() string
func (e *SyntaxError) Message() string
type SyntaxErrors []*SyntaxError
```

Compared with the engine's API:

- **`Parse` and `ParseRule` replace `Parser.Parse`.** Position units and rule-call limits use
  functional options (`Parse(input, pairsparser.WithUnit(pairsparser.Bytes))`), like the engine. `ParseRule(name, input)` plays the role of
  `Parser.WithStart(name).Parse(input)`; an unknown name is an error: `rule nope is not defined`. Like the engine, both
  require the rule to consume the whole input.
- **The types belong to the generated package.** `pairsparser.Node` has the same fields, methods and JSON encoding as
  `pego.Node`, but it is a different Go type: you cannot pass one where the other is expected, and error checks use
  `errors.As` with the generated `*pairsparser.SyntaxError` (and `pairsparser.SyntaxErrors` for errors recovered with
  `#recover`). If other code needs `pego.Node`, convert through JSON, which is identical for both, or write code against
  the fields you need.
- **Fields.** As with the engine, a field value is a `*Node`, `int`, `string`, `bool` or `nil`: assert it to
  `*pairsparser.Node` (as the example does with `Field("Key")`), not to `*pego.Node`.
- **Errors** carry the same positions and messages. Recovered errors work too.
  With this grammar, `rec.pego`:

  ```pego
  rule main =  stmt* $$
  rule stmt =  s:("let" " " name ";") #recover(skip=[^;]* ";") => $s
  rule name =  [a-z]+ #atomic
  ```

  generated into a package `recparser`, the node is returned together with the recovered errors:

  ```go
  node, err := recparser.Parse("let a;let 1;let b;")
  fmt.Println(node)
  var errs recparser.SyntaxErrors
  if errors.As(err, &errs) {
  	for _, e := range errs {
  		fmt.Println(e.Line, e.Col, e.Message())
  	}
  }
  ```

  ```
  (Seq [(Seq "let" " " "a"@name ";") Error"let 1;"{message=`1:11: syntax error: expected [a-z]`} (Seq "let" " " "b"@name ";")])@main
  1 11 syntax error: expected [a-z]
  ```

- A generated parser is **safe for concurrent use**: each call has its own state, and the tables are read-only after
  start-up. (Checked here with sixteen goroutines under the race detector.)
- Runtime errors in actions (such as division by zero) and exceeding the nesting limit return plain errors, as in the
  engine.

### What Is Inside the File

| Part | Contents |
|:--|:--|
| Runtime | The parser state, memoization, left recursion, Operators Rule loop, attribute handling and the built-in functions of actions. About 1,800 lines, **the same in every generated file**, embedded in each generated file. |
| Rule table | For each rule, the results of the engine's static analysis (memoized? left-recursion leader? capture names?). |
| Parser expressions | One method per expression, with literals, character classes and rule indices embedded as constants, and one per rule that is never memoized and has no captures, which calls the rule's body directly. |
| Value Actions and Predicate Actions | Go expressions; lambdas become Go function literals. |

That is why even a tiny grammar produces a file of a few dozen kilobytes. Measured sizes: `pairs.pego` (7 lines)
generates 56,970 bytes; the JSON grammar ([parsers/json](../../parsers/json/json.pego), 1,514 bytes of source) generates
92,537 bytes; the minilang grammar (4,416 bytes) generates 134,245 bytes. The growth is in the per-grammar part.

## Typed Values (`-types`)

With `-types` (`pego.WithTypes()`), the generated package also defines a Go type for each type of the grammar and a
function that returns the result as values of those types:

```go
// ParseAST parses the input like Parse and returns the result as typed values.
func ParseAST(input string, opts ...ParseOption) (T, error) // T: the Go type of the start rule's type
func ParseRuleAST(name, input string, opts ...ParseOption) (any, error)
```

For the JSON grammar ([parsers/json](../../parsers/json/json.pego)), whose start rule has the type `Value`, the
generated types are:

```go
type Span struct{ Start, End int }

type String struct {
	Span
	Text string
}
// ... Number, Bool and Null likewise (terminal types)

type Member struct {
	Span
	Key   *String
	Value Value
}

type Object struct {
	Span
	Members []*Member
}
// ... Array likewise (struct types)

// Value is the union type Value = Object | Array | String | Number | Bool | Null.
type Value interface{ isValue() }

func ParseAST(input string, opts ...ParseOption) (Value, error)
```

and a type switch replaces the field lookups and type assertions of `*Node`:

```go
v, err := jsonparser.ParseAST(`{"name": "pego", "tags": ["peg", "go"], "stars": 42}`)
if err != nil {
	log.Fatal(err)
}
for _, m := range v.(*jsonparser.Object).Members {
	switch x := m.Value.(type) {
	case *jsonparser.Array:
		fmt.Println(m.Key.Text, "array of", len(x.Elements))
	case *jsonparser.Number:
		fmt.Println(m.Key.Text, "number", x.Text, "at", x.Start)
	case *jsonparser.String:
		fmt.Println(m.Key.Text, "string", x.Text)
	}
}
```

```
name string pego
tags array of 2
stars number 42 at 49
```

How grammar types map to Go:

| Grammar type | Go type |
|:--|:--|
| `int`, `string`, `bool` | the same |
| struct type `T` | `*T`: the same fields, plus an embedded `Span` with the value's range |
| terminal type `T` | `*T`: `Span` and `Text` |
| `Match` | `*Match`: `Span` and `Text` |
| `Error` (left by `#recover`) | `*Error`: `Span`, `Text` and `Message` |
| union `U ~ A \| B` of node types | an interface `U`, implemented by `*A`, `*B` and `*Error` (and by `*Node` if a member is a CST type) |
| `[]T` | a slice; an empty list is an empty slice, not `nil` |
| `*T` | `*int`, `*string` or `*bool` for basic types; otherwise the Go type of `T`, `nil` when absent |
| `Seq`, `List`, `Operator` and records | `*Node`, as from `Parse` |
| `node`, `terminal`, `any`, unions with basic members | `any`, holding the converted value |
| alias `Alias ~ T` | a Go alias to the value type of `T`; for example, `Alias ~ Word` becomes `type Alias = *Word` |

Details:

- **Names.** Go types have the names of the grammar types. A name that the generated runtime already uses (`Node`,
  `Match`, `Error`, `Parse`, `Unit`, ...) gets a trailing underscore: a grammar type `Node` becomes `Node_`. `Span`
  is renamed the same way if a type or field is called `Span`.
- **Aliases.** Every declared alias is exported, including chained, scalar, optional, list and CST aliases. Node
  unions with the same normalized representation share one generated interface; additional names are Go aliases
  to it. Aliases preserve assignment compatibility and add no new runtime value kind. Regenerate existing parsers
  to receive previously omitted alias names.
- **Errors.** `ParseAST` returns the errors of `Parse`. After `#recover`, an `Error` in a list of a union type stays
  in the list as `*Error`; where a struct or terminal type is expected it becomes `nil` (it is among the returned
  `SyntaxErrors` anyway).
- **Speed.** When every value `ParseAST` can return has a Go type of its own (no `Seq`, `List`, `Operator`, records,
  `node`, `terminal` or `any`, also in struct fields) and no action reads a field of a struct value, the generated
  parser builds the values directly, without the
  nodes `Parse` makes: on the benchmarks `ParseAST` then takes a half to two thirds of the time of `Parse` and a quarter
  to a third of the memory (JSON 6.7 → 3.1 ms and 10.4 → 2.4 MB on a 262 KB input, less than `Recognize`; XML, the
  left-recursive calculator and the outline grammar alike; the Operators Rule calculator about 11% faster). Otherwise
  `ParseAST` runs `Parse` and converts the tree, about 10% slower than `Parse` (minilang). The
  [design record](../design/012-typed-values.md) explains both. The typed runtime adds about as much code to the
  generated file as the parser itself.
- **Scratch reuse.** The typed runtime clears discarded undo entries after rollback, rule return and captured
  repetition elements. Parser recycling also clears saved-capture backing slices, so overwritten values do not remain
  reachable through pooled scratch after a later failed parse. Returned values remain valid. Regenerate existing typed
  parsers to receive this cleanup; output strings may still retain their own backing storage.
- **Named rules.** `ParseRuleAST("expr", input)` returns the selected rule's typed value as `any`.
  Its generated documentation lists each rule's Go result type; use a type assertion or type switch.
  It consumes the whole input and applies options once before looking up the name. Unknown names and
  invalid options return plain nil with an error. Ordinary parse failure returns the selected type's
  zero value: nil pointers and slices retain their dynamic type in `any`, while a nil interface stays
  nil. A successful rule whose value is nil also returns the selected type's zero value, with no error.
  This includes plain nil for `any`, `node` and union-interface results; legacy `ParseAST` keeps its
  existing conversion mapping, which can box a nil `*Node` in an `any` result.
  Recovery and conversion follow `ParseAST`, including `Error` values in unions and nil where
  a struct or terminal is expected. Direct typed execution uses the same full grammar guard as
  `ParseAST`; unsupported result or struct-field types and field-reading actions use Node conversion.
  Rules still must produce nodes; scalar aliases and scalar fields do not enable scalar rule results.
  `ParseRule` continues to return `*Node`, and `ParseAST` continues to use the configured start rule.
- **Regeneration migration.** `ParseRuleAST` is now reserved in typed generated Go. A grammar type
  named `ParseRuleAST` becomes `ParseRuleAST_`; a type already named `ParseRuleAST_` becomes
  `ParseRuleAST__` because names are assigned in sorted order. Update consumers that name those
  generated types, including function values whose `ParseAST` return type uses a renamed type.
  Untyped Go and TypeScript entry points are unchanged. See [design 029](../design/029-typed-named-rule-parsing.md).

## Incremental Documents (`-document`)

Generate with `pego gen -g grammar.pego -pkg parser -document -o parser/parser.go`, or pass
`pego.WithDocument()` to `GenerateGo`. This adds a mutable Node `Document` alongside the ordinary batch `Parse` and
`ParseRule` functions:

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

Both `Document.Parse` and `Document.ParseAST` return owned syntax diagnostics: recovered `SyntaxError` values and
their `Expected` and `Messages` slices are copied; ordinary failures are freshly constructed. Mutating a returned
error cannot affect memoized errors or a later parse. Parsing, diagnostic copying and typed publication share one
work budget; interruption returns the result type's zero value and clears reusable state as needed for a retry.
`-document` cannot be combined with `-lang ts`; engine-only Document controls such as `EditBatch` are separate work. See the [direct typed measurements](../design/044-generated-incremental-and-stream-parsers.md#local-measurements-for-direct-typed-document-matching)
and the [conversion fallback measurements](../design/044-generated-incremental-and-stream-parsers.md#local-measurements-for-typed-go-document-conversion).

## Streams (`-stream`)

Generate with `pego gen -g grammar.pego -pkg parser -stream -o parser/parser.go`, or pass `pego.WithStream()` to
`GenerateGo`. The start rule must have a [`#stream`](../../spec/pragmas.md#stream) repetition; otherwise generation
fails. This adds a Go `ParseStream` beside the ordinary `Parse`, which still treats `#stream` as a plain repetition:

```go
err := parser.ParseStream(r, func(n *parser.Node) error {
	fmt.Println(n.Kind(), n.Text)
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

With `-types`, the package also has

```go
type StreamValue = ... // the typed value of one element of the #stream repetition
func ParseStreamAST(r io.Reader, emit func(StreamValue) error, opts ...ParseOption) error
func IterateAST(r io.Reader, opts ...ParseOption) (*StreamASTIterator, error) // Next returns StreamValue
```

`ParseStreamAST` behaves like `ParseStream` but passes each element as the value its expression has in the
[Go type mapping](#typed-values--types) — a rule's declared type, a union, a list, `*Match`, or `*Node` for a record of
captures — and the zero value for an element without a value. `StreamValue` is the type of the repeated element,
not of the start rule:

```go
// rule main = item* #stream
// rule item Pair = k:[a-z]+ "=" v:[0-9]+ "\n" => Pair{Key: $k, Value: $v}
err := parser.ParseStreamAST(r, func(p *parser.Pair) error {
	fmt.Println(p.Key.Text, p.Value.Text)
	return nil
})
```

Each element is converted on its own once it is committed, so values stay valid after the callback returns and share
nothing with later elements. The conversion counts toward `WithWorkLimits` and observes `WithContext`.

## Recognition (`-recognize`)

With `-recognize` (`pego.WithRecognize()`), the generated package also has

```go
// Recognize checks that the whole input matches the start rule without building a tree, and returns the
// syntax errors Parse would return (SyntaxErrors for errors recovered with #recover).
func Recognize(input string, opts ...ParseOption) error
```

It is the engine's [`RecognizeOnly`](backends.md#recognition-mode) in generated form: the grammar is compiled once more
with no rule building a value (except those whose values predicates read), into a second rule table in the same file.
Actions whose values are not needed by predicates are skipped. Actions needed by predicates still run and can report runtime errors. On the benchmark workloads it
takes about half the time of `Parse` (JSON 8.8 → 4.9 ms on a 262 KB input, CSV 4.0 → 1.8 ms), and it is the fastest
way PEGO has to validate input. The price is a larger file: the second table adds about as much code as the first.

### Invocation Options and Migration

```go
node, err := pairsparser.Parse(input,
    pairsparser.WithUnit(pairsparser.Bytes), pairsparser.WithMaxDepth(600_000))
```

`Parse`, `ParseRule`, `Recognize` (when generated) and `ParseAST`/`ParseRuleAST` (with `-types`) accept the same functional options.
No options preserves CodePoints and the generated depth default, normally 100,000. A positive `WithMaxDepth(n)`
overrides that default for one invocation, including typed direct parsing and node conversion. Zero resets the
field to the generated default. Options apply in order; only a negative final limit returns a configuration error,
before matching, actions or named-rule lookup. A nil option panics when applied, as engine options do.
Parser pooling and concurrent calls keep their limits independent. Limits count nested rule calls rather than
brackets or AST nodes, and raising the limit does not guarantee host-stack capacity.

### Cancellation and work limits

Generated Go entry points also accept invocation-local cancellation and work
limits. Use the generated package's options and error types:

```go
node, err := pairsparser.Parse(input,
    pairsparser.WithContext(ctx),
    pairsparser.WithWorkLimits(pairsparser.WorkLimits{
        MaxInputWork: 4_000_000,
        MaxSteps:     1_000_000,
    }))
```

A nil context has no deadline or cancellation signal; zero limits are unlimited. Options apply once in
order; the last context and the last complete `WorkLimits` value win. Counters
reset for every call and remain independent during concurrent parsing. The
same options work with named rules, recognition, direct typed parsing, and
Node-to-typed conversion.

`MaxInputWork` counts cumulative inspected input elements, including input
preparation, repeated scans and text extraction. `MaxSteps` counts execution
and per-element work in actions, memo lookup, graph/list construction, capture
rollback, diagnostics and typed conversion. Counts depend on the generated
artifact and position unit; a limit is not a character count, memory cap, or
elapsed-time limit. Backtracking never refunds work. Depth remains a separate
limit.

Use `errors.Is(err, context.Canceled)` or `context.DeadlineExceeded` for
cancellation, and `errors.Is(err, pairsparser.ErrWorkLimit)` for an exhausted
budget. `errors.As` can retrieve a `*pairsparser.WorkLimitError` with `Kind`,
`Limit`, `Used` and `Requested`. Interrupted entry points return their zero
result; recovery cannot turn an interruption into a successful partial tree.
Previously returned results stay valid, and a later invocation can retry.
Cancellation is cooperative during engine work and is checked before input
preparation and before publishing a result.

### Regenerating older callers

Regenerating an older Go parser requires an explicit source migration:

- Change `Parse(input, Bytes)` to `Parse(input, WithUnit(Bytes))`, and likewise for the other entry points.
- Replace `[]Unit` expansion with `[]ParseOption`. To preserve the old first-unit policy, append `WithUnit(units[0])`
  when the old unit slice is nonempty; otherwise use no options. Applying every unit option makes the last one win.
- Update function values from `func(string, ...Unit)` to `func(string, ...ParseOption)`, with the corresponding
  `ParseRule`, `Recognize` and typed `ParseAST` return signatures. Calls with no options keep their syntax.

New exports `ParseOption`, `WithUnit`, `WithMaxDepth`, `WithContext`, `WithWorkLimits`,
`WorkLimits`, `WorkLimitKind`, `WorkLimitError`, `ErrWorkLimit`, `InputWorkLimit` and
`StepWorkLimit` reserve their names in typed generation. Grammar types
colliding with runtime names gain underscores in stable sorted order: `ParseOption` becomes `ParseOption_` and an
existing `ParseOption_` becomes `ParseOption__`. Aliases and typed result signatures follow those names. Update
consumers of renamed types and typed function values. Handwritten declarations in the generated package that use
these new names must be renamed by the consumer; the generator cannot inspect those files.

## Related Topics

- [Generating Parsers](code-generation.md): the command, what is supported and keeping generated code up to date.
- [Reading Trees](reading-trees.md): the `Node` that the untyped entry points return.
- [Incremental Parsing](incremental.md) and [Streaming](streaming.md): the engine's versions of the same features.
