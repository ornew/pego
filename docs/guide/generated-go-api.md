# The Generated Go API

A Go parser written by [`pego gen`](code-generation.md) has a small API: `parser.go` defines the functions that parse, `api.go` re-exports the types and options of the runtime package (the copy in `internal/runtime`, or `github.com/ornew/pego/runtime` with `-runtime import`) under their own names, and, with `-types`, `types/types.go` defines the Go types of the grammar. This guide describes
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

Every generated package has the same small API, spread over `parser.go` and `api.go`:

```go
// Parse parses the whole input with the start rule. Options select position units and the nested rule-call limit.
func Parse(input string, opts ...ParseOption) (*Node, error)

// ParseRule parses the whole input with the start rule name, a rule marked @start; an empty
// name selects the grammar's default start rule.
func ParseRule(name, input string, opts ...ParseOption) (*Node, error)

type ParseOption = runtime.ParseOption // an alias; the options are internal to the runtime
func WithUnit(unit PositionUnit) ParseOption
func WithMaxDepth(n int) ParseOption

type PositionUnit int
const (
	CodePoints PositionUnit = iota // the default
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
  `Parser.WithStart(name).Parse(input)`, and like it accepts only rules marked `@start`: another rule is an error,
  `rule pair is not a start rule; parsing can begin only at a rule marked @start`, and so is an unknown name,
  `rule nope is not defined`. An empty name selects the grammar's default start rule, as for
  `Parser.WithStart`. Like the engine, both require the rule to consume the whole input.
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
  @start
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
  ([("let" " " "a" ";") error("1:11: syntax error: expected [a-z]") ("let" " " "b" ";")])
  1 11 syntax error: expected [a-z]
  ```

- A generated parser is **safe for concurrent use**: each call has its own state, and the tables are read-only after
  start-up. (Checked here with sixteen goroutines under the race detector.)
- Runtime errors in actions (such as division by zero) and exceeding the nesting limit return plain errors, as in the
  engine.

### What Is Inside the Package

| Part | Contents |
|:--|:--|
| Runtime | The parser state, memoization, left recursion, Operators Rule loop, attribute handling and the built-in functions of actions. It is **the same for every grammar**: the package `github.com/ornew/pego/runtime`, which the generated package imports (`-runtime import`) or carries a copy of in `internal/runtime` (`-runtime embed`, the default). |
| `api.go` | The names of the runtime that the users of the parser need (`Node`, `SyntaxError`, the options and so on), as aliases and wrappers. It is the same for grammars generated with the same options. |
| `types/types.go` | With `-types`, the package `<module>/types`: a Go type for each type of the grammar, `TNode`, `Error` and the tuple types. |
| Rule table | For each rule, the results of the engine's static analysis (memoized? left-recursion leader? capture names?). |
| Parser expressions | One method per expression, with literals, character classes and rule indices embedded as constants, and one per rule that is never memoized and has no captures, which calls the rule's body directly. |
| Value Actions and Predicate Actions | Go expressions; lambdas become Go function literals. |

The runtime is not part of `parser.go`, so the size of `parser.go` is that of the per-grammar parts: it grows with the
grammar, and a tiny grammar produces a file of a few kilobytes. Options such as `-types` and `-document` add code to
`parser.go`.

## Recognition (`-recognize`)

With `-recognize` (`Recognize` of `gen.Go`), the generated package also has

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
With `-typed-only` (`TypedOnly` of `gen.Go`, which needs `-types`) the package has only `ParseAST` and `ParseRuleAST`: `Parse`, `ParseRule`, `Recognize` and the code that builds `*Node` values are not generated, and `-recognize`, `-document` and `-stream` are rejected. When a typed entry point converts the Node of its rule (an action reads a field of a struct, or a result type has no Go type of its own), the generated package keeps the unexported Node rule functions that conversion runs, so the saving is smaller for such a grammar.
No options preserves CodePoints and the generated depth default, normally 100,000. A positive `WithMaxDepth(n)`
overrides that default for one invocation, including typed direct parsing and node conversion. Zero resets the
field to the generated default. Options apply in order; only a negative final limit returns a configuration error,
before matching, actions or named-rule lookup. A nil option panics when applied, as engine options do.
Parser pooling and concurrent calls keep their limits independent. Limits count nested rule calls rather than
brackets or AST nodes, and raising the limit does not guarantee host-stack capacity.

## Cancellation and Budget Limits

A Go parser generated with `-budget-controls` also accepts invocation-local
cancellation and budget limits. Use the generated package's options and error
types; a package generated without the flag exports none of them, and a parse
that passes `WithContext` with a context or `WithLimits` with a limit from the
runtime returns an error saying that the parser was generated without budget
controls (a nil context, a zero `Limits` and a `Limits` whose fields are `Unlimited` set no control and are accepted):

```go
node, err := pairsparser.Parse(input,
    pairsparser.WithContext(ctx),
    pairsparser.WithLimits(pairsparser.Limits{
        Budget: 5_000_000,
    }))
```

A nil context has no deadline or cancellation signal. A zero limit selects its default: a zero `Budget` is
`max(2^20, 1024 * n)` for an input of `n` units, so every parse of a controlled parser has a budget. The generated
package exports `Unlimited`, which disables a limit; `Limits{Budget: Unlimited}` with no context runs without
a control. `Limits.MaxInput` bounds the input length in the position unit; its default is `Unlimited`, and a longer input fails
before any parsing with a `*LimitError` of kind `LimitInput` (a generated stream fails when the parser reaches a unit beyond it). Options apply once in
order; the last context and the last complete `Limits` value win. The counter
resets for every call and remain independent during concurrent parsing. The
same options work with named rules, recognition, direct typed parsing, and
Node-to-typed conversion.

`Budget` counts cumulative spend in one counter, one unit each: inspected
input elements, including input preparation, repeated scans and text
extraction, and execution and per-element spend in rule calls, loop iterations,
actions, memo lookup, graph/list construction, capture rollback, diagnostics
and typed conversion. A loop that only scans input, such as a repeated
character class, charges for every element it reads, so `Budget` bounds it.
Counts depend on the generated
artifact and position unit; a limit is not a character count, memory cap, or
elapsed-time limit. Backtracking never refunds spend. Depth remains a separate
limit.

Use `errors.Is(err, context.Canceled)` or `context.DeadlineExceeded` for
cancellation, and `errors.Is(err, pairsparser.ErrLimit)` for an exhausted
budget. `errors.As` can retrieve a `*pairsparser.LimitError` with `Kind`,
`Limit`, `Used` and `Requested`. Interrupted entry points return their zero
result; recovery cannot turn an interruption into a successful partial tree.
Previously returned results stay valid, and a later invocation can retry.
Cancellation is cooperative during engine execution and is checked before input
preparation and before publishing a result.

## Type Names

With `-types`, the types of the grammar are declared in the package `<module>/types`, not in the parser package, so they
share no namespace with `parser.go` and `api.go`: a grammar type may be called `Node`, `ParseOption`, `Walk` or like any
other name of the parser package, and is then used as `types.Node`. The types package declares `TNode`, `Error`,
`PegoHooks` and, where used, `Unit`, `TupleN`, `PegoNodeTypes` itself and the constant `PegoSchema_<digest>` (which ties the parser to this package), and generation fails if a grammar type has one
of those names. Declarations that you add by hand to the generated packages must not reuse the names they declare. The
[generated parser contract](../../spec/runtime/generated-parsers.md) lists the rule.

## Related Topics

- [Generating Parsers](code-generation.md): the command, what is supported and keeping generated code up to date.
- [Reading Trees](reading-trees.md): the `Node` that the untyped entry points return.
- [Incremental Parsing](incremental.md) and [Streaming](streaming.md): the engine's versions of the same features.
