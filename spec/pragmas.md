# Pragmas

A `#` pragma changes the behavior or structure of the immediately preceding
expression. Pragmas apply from left to right in written order. Parenthesize a
sequence when a pragma should apply to the complete sequence rather than its
final item. Character classes in square brackets are primary Rule Expression forms, not pragmas.
Diagnostic and metadata `@` attributes are defined separately in
[Attributes](attributes.md).

Pragmas have pragma-specific argument syntax. For example, recovery retains
the named `skip` argument in `#recover(skip=expression)`; it does not use the
generic positional and named argument binding of attributes.
The name is adjacent to `#`, and an argument list is adjacent to its name:
`#repeat(2)` is valid; `# repeat(2)` and `#repeat (2)` are syntax errors.

| Pragma | Description |
|:--|:--|
| `#recover(skip=e)` | On failure, skips input matching `e` and continues parsing |
| `#repeat(n)` | Repeats exactly `n` times |
| `#repeat(n,)` | Repeats at least `n` times |
| `#repeat(,m)` | Repeats from zero through `m` times |
| `#repeat(n,m)` | Repeats from `n` through `m` times |
| `#stream` | Hands each element of a repetition to the caller as soon as it matches in a stream parse |
| `#atomic` | Returns one text-only `Match` and suppresses construction of child nodes |

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
recovery also removes its error. A recovered expression's type is the union
of its original type and `Error`, and `Error` is assignable to every node type.
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
effect.

## `#repeat`

`#repeat` takes positional nonnegative integer bounds only. Its comma
distinguishes an exact count from an unbounded maximum: `#repeat(n)` is exactly
`n`, `#repeat(n,)` is at least `n`, `#repeat(,m)` is at most `m`, and
`#repeat(n,m)` is bounded. The pragma applies to the immediately preceding
expression; parenthesize a sequence to repeat it as a unit. Named bounds are
not accepted.
