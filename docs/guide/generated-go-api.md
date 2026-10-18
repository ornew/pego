# The Generated Go API

A Go parser written by [`pego gen`](code-generation.md) has a small API defined in the file itself. This guide describes
it, the call options that all entry points share, and recognition. Options that add typed values, incremental documents
and streams have their own guides:

| Option | Guide |
|:--|:--|
| `-types` | [Typed Values in Generated Go](generated-typed-values.md) |
| `-document` | [Incremental Documents in Generated Go](generated-documents.md) |
| `-stream` | [Streams in Generated Go](generated-streams.md) |
| `-recognize` | [Recognition](#recognition--recognize), below |

It assumes you have generated a parser as in the [quick start](code-generation.md#quick-start).

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

  ```text
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

## Recognition (`-recognize`)

With `-recognize` (`pego.WithRecognize()`), the generated package also has

```go
// Recognize checks that the whole input matches the start rule without building a tree, and returns the
// syntax errors Parse would return (SyntaxErrors for errors recovered with #recover).
func Recognize(input string, opts ...ParseOption) error
```

It is the engine's [`RecognizeOnly`](recognition-mode.md) in generated form: the grammar is compiled once more
with no rule building a value (except those whose values predicates read), into a second rule table in the same file.
Actions whose values are not needed by predicates are skipped. Actions needed by predicates still run and can report runtime errors. On the benchmark workloads it
takes about half the time of `Parse` (JSON 8.8 → 4.9 ms on a 262 KB input, CSV 4.0 → 1.8 ms), and it is the fastest
way PEGO has to validate input. The price is a larger file: the second table adds about as much code as the first.

## Invocation Options

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

## Cancellation and Work Limits

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

## Regenerating Older Callers

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
