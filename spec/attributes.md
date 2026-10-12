# Attributes and Pragmas

Attributes and behavioral pragmas annotate parsing expressions. An attribute
uses `@name`; a behavioral pragma uses `#name`. Attributes accept positional
arguments followed by named arguments. A positional argument may be `nil`, so
an omitted slot remains distinct from an absent argument. Duplicate named
arguments are errors. Pragmas have pragma-specific argument syntax rather than
the generic attribute argument rules.
Declaration-prefix annotations are not supported.

Annotations apply from left to right in written order. A postfix annotation
applies to the immediately preceding expression; parenthesize a sequence when
the annotation should wrap the complete sequence rather than its final item.

```pego
rule stmt = (let / expr) @error("expected a statement") #recover(skip=[^;]+ ";")
```

| Annotation | Kind | Description |
|:--|:--|:--|
| `@error("...")` | Attribute | Replaces expected items inside an expression with a custom message |
| `#recover(skip=e)` | Behavioral pragma | On failure, skips input matching `e` and continues parsing |
| `#repeat(n)` | Behavioral pragma | Repeats exactly `n` times |
| `#repeat(n,)` | Behavioral pragma | Repeats at least `n` times |
| `#repeat(,m)` | Behavioral pragma | Repeats from zero through `m` times |
| `#repeat(n,m)` | Behavioral pragma | Repeats from `n` through `m` times |
| `#stream` | Behavioral pragma | Hands each element of a repetition to the caller as soon as it matches in a stream parse |
| `#atomic` | Behavioral pragma | Returns one text-only `Match` and suppresses construction of child nodes |

Unknown annotations, invalid arguments, and invalid targets are grammar errors.

## `@error`

When an expression fails, `@error("...")` replaces its expected items with
the configured message at the farthest position reached inside it. A named
form such as `@error(message="...")` is equivalent. When several messages
apply at one position, they are joined with `; `. If the expression succeeds,
expectations recorded inside it are kept as usual.

```pego
rule let = "let" " " name @error("expected a name") ";"
```

## `#recover`

On failure, `#recover(skip=e)` records the syntax error and starts matching
`e` from the annotated expression's initial position. If `e` matches and
consumes input, the expression produces an `Error` node covering the skipped
input and parsing continues.

```pego
// If a statement fails, skip through its next semicolon.
rule stmt = s:(let / assign / call) #recover(skip=[^;]+ ";") => $s
```

The skip expression cannot contain captures and must consume input. Recovered
errors are returned with the resulting node. Backtracking that undoes a
recovery also removes its error. A recovered expression's type is the union of
its original type and `Error`, and `Error` is assignable to every node type.
`Parser.Parse` returns a recovered node together with a `SyntaxErrors` error
listing the diagnostics. If parsing later fails, only the final syntax error
is returned. A no-value expression, including lookahead or a predicate,
remains no-value after recovery, though its error is recorded.

## `#stream`

`#stream` annotates a repetition at the top level of the start rule used by a
stream parse. Each matched element is handed to the caller as soon as it
matches.

```pego
// Receive a log one record at a time.
rule main = ^^ header records:record* #stream $$
```

Handed elements are not included in the repetition's returned list. Once
handed over, an element is final and the parser does not backtrack before it.
Therefore a rule body contains at most one `#stream`, on the complete body, a
top-level sequence element, or the expression captured by such an element.
`#recover` cannot wrap the same repetition in either order. In an ordinary
parse, `#stream` has no effect. A stream parse with no `#stream` on its start
rule is an error.

A stream parse reads only as much input as needed and may discard each handed
element with preceding input and memoization data. Decoding a complete
character needs no future bytes; an incomplete UTF-8 prefix needs more bytes
or actual EOF, while a non-EOF reader error terminates parsing. The first
zero-width successful repetition iteration is handed over once and stops the
repetition even below its minimum; a minimum failure then returns an error
without retracting the handed value. Ordinary repetitions use the same stop
and count rule.

## `#atomic`

Postfix `expr #atomic` matches `expr` but returns a single text-only `Match`
node and suppresses construction of the expression's child nodes. It does not
commit to the match: ordinary PEG backtracking and choice behavior remain in
effect. `#atomic` is a behavioral pragma, not an attribute.

## `#repeat`

`#repeat` takes positional nonnegative integer bounds only. Its comma distinguishes an
exact count from an unbounded maximum: `#repeat(n)` is exactly `n`,
`#repeat(n,)` is at least `n`, `#repeat(,m)` is at most `m`, and
`#repeat(n,m)` is bounded. The pragma applies to the immediately preceding
expression; parenthesize a sequence to repeat it as a unit. Named bounds are
not accepted.
