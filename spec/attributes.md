# Attributes

An attribute, written `#name` or `#name(key=value, ...)`, is attached to a
parsing expression and changes how that expression is parsed.

An attribute binds as tightly as a postfix operator and applies to the
expression immediately before it. When several attributes follow an expression,
they are applied in the order written, from the innermost outward.

```pego
def hello = "hello" / _|_ #error(message="expect 'hello'")   // #error applies to _|_
def stmt = (let / expr) #error(message="expected a statement") #recover(skip=(?^;)+ ";")
```

Argument values are parsing expressions; a string argument is written as a
literal. The `(` MUST immediately follow the attribute name. Unknown attributes
and unknown arguments are errors.

| Attribute | Description |
|:--|:--|
| `#error(message="...")` | Replaces the expected items of a [syntax error](parsing.md#syntax-errors) inside the expression with a message |
| `#recover(skip=e)` | When the expression fails, skips input matching `e` and continues parsing |
| `#stream` | In a stream parse, hands each element of a repetition to the caller as soon as it matches |

## `#error`

When the expression fails, `#error(message="...")` replaces all the items that
were expected inside the expression with `message`. The message is reported at
the farthest position reached inside the expression.

```pego
def let = "let" " " name #error(message="expected a name") ";"
// "let 1;" → 1:5: expected a name
```

If several messages apply to the same position, they are joined with `; `. When
a position has a message, the expected items at that position are not shown.
If the expression succeeds, the expected items and messages recorded inside it
are kept as usual.

## `#recover`

When the expression fails, `#recover(skip=e)` records the syntax error and then,
starting from the position where the expression began, skips the input that `e`
matches. The value of the expression becomes an `Error` node that covers the
skipped input, and parsing continues after it.

```pego
// If a statement cannot be parsed, skip up to and including the next ";".
def stmt = s:(let / assign / call) #recover(skip=(?^;)+ ";") -> $s
```

| `Error` node | Value |
|:--|:--|
| Range and text | From the start of the expression to the end of the skipped input |
| `message` field | The recorded syntax error, including its position, as a `string` |

- If `e` fails, or matches without consuming input, there is no recovery and the
  expression fails. Therefore `#recover` inside a repetition cannot loop
  forever.
- `e` MUST NOT contain captures.
- Recovered errors are returned together with the resulting node. In the Go API,
  `Parser.Parse` returns the node and a `SyntaxErrors` error that lists them.
- If parsing still fails after recovery, only the final syntax error is
  returned.
- An error recovered inside a match that was later undone by backtracking is
  not reported.
- An expression that has no value (a lookahead or a predicate, for example) has
  none after a recovery either; the error is still recorded.
- The type of an expression with `#recover` is the union of its original type
  and `Error`. `Error` is assignable to every node type (see
  [Assignability](types.md#assignability)).

See [examples/minilang](../examples/minilang/) for a complete example of error
recovery.

## `#stream`

`#stream` is attached to a repetition (`*`, `+` or `{n,m}`) at the top level of
a rule body. When that rule is the start rule of a stream parse (Go API
`Parser.ParseStream`, command line `pego parse -stream`), each element of the
repetition is handed to the caller as soon as it matches.

```pego
// Receive a log with one record per line, record by record.
def main = ^^ header records:record* #stream $$
```

- A stream parse reads only as much input as it needs, and discards each handed
  element together with the input and memoization data before it, so the input
  need not fit in memory.
  In either position unit, decoding a complete character does not require
  future bytes. An incomplete UTF-8 prefix requires more bytes or actual EOF;
  a non-EOF reader error terminates the parse.
- Handed elements are not included in the value (`List`) of the repetition.
- Once an element is handed over it is final: the parser never backtracks to
  a position before it. For this reason, `#stream` MUST appear only at the top
  level of a rule body: on the body itself, on an element of the body's
  sequence, or on the expression captured by such an element. A rule body MUST
  contain at most one `#stream`.
- Starting a stream parse with a rule that has no `#stream` is an error.
- `#stream` takes no arguments.
- In an ordinary parse (`Parser.Parse`), `#stream` has no effect: the
  repetition behaves as usual.
