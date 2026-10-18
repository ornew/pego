# Typed Values in Generated Go

With `-types` (`pego.WithTypes()`), the generated package also defines a Go type for each type of the grammar and a
function that returns the result as values of those types. This guide describes those types and functions; the rest of
the generated package is in [The Generated Go API](generated-go-api.md).

## The Generated Functions

```go
// ParseAST parses the input like Parse and returns the result as typed values.
func ParseAST(input string, opts ...ParseOption) (T, error) // T: the Go type of the start rule's type
func ParseRuleAST(name, input string, opts ...ParseOption) (any, error)
```

## An Example

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

## Mapping Grammar Types to Go

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

## Details

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

## Related Topics

- [The Generated Go API](generated-go-api.md): `Parse`, `ParseRule` and the types of the untyped entry points.
- [Generating Parsers](code-generation.md): the `-types` option and the others.
- [Reading Trees](reading-trees.md): the `Node` access that typed values replace.
- [Types](types.md): the grammar types that these Go types come from.
