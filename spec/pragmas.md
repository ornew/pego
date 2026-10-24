# Pragmas

A `#` pragma changes the behavior or structure of the immediately preceding
expression, except `#cut`, which stands where an expression would. Pragmas apply from left to right in written order. Parenthesize a
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
| `#missing` | With opt-in Closure parsing, inserts a nonempty literal at an empty range |
| `#repeat(n)` | Repeats exactly `n` times |
| `#repeat(n,)` | Repeats at least `n` times |
| `#repeat(,m)` | Repeats from zero through `m` times |
| `#repeat(n,m)` | Repeats from `n` through `m` times |
| `#stream` | Hands each element of a repetition to the caller as soon as it matches in a stream parse |
| `#atomic` | Returns one string node and suppresses construction of the values of the elements inside it |
| `#ignorecase` | Makes the string literals written in the annotated expression case-insensitive, for ASCII letters; see [`#ignorecase`](#ignorecase) |
| `#cut` | Stands in place of an expression and commits the enclosing choice; see [Cut](parser-expressions.md#cut). It takes no arguments: `#cut(` is an error |

## `#ignorecase`

`e #ignorecase` makes every string literal written in `e` match the ASCII
letters of its characters in either case. A literal written with the suffix
`i`, such as `"select"i`, is case-insensitive without the pragma (see
[String Literals](lexical.md#string-literals)).

```pego
rule keyword = ("select" / "from") #ignorecase
rule select = "select"i
```

The pragma is lexical. It reaches the literals of `e` at any depth: under a
sequence, a choice, a repetition, a lookahead (`!` and `&`) or a capture, in the
expression arguments of other pragmas, and in the operands, operators and skip
expression of an [Operators Rule](operators-rule.md) written in `e`. It does
not reach a rule that `e` calls, and it does not change a character class, which
is already a set of characters. Folding is ASCII only, as for the suffix:
`"é" #ignorecase` matches `é` and not `É`.

The pragma takes no arguments. Its expectations show each case-insensitive
literal with its suffix, so `("select") #ignorecase` expects `"select"i`. The
`#missing` pragma inserts the spelling written in the literal, without the suffix
and without any change of case. The pragma follows an expression, like the other
behavioral pragmas, and applies from left to right, so `"a" #ignorecase "b"`
folds only `"a"`.

## `#recover`

On failure, `#recover(skip=e)` records the syntax error and starts matching
`e` from the annotated expression's initial position. If `e` matches and
consumes input, the expression produces an error value covering the skipped
input and parsing continues.

```pego
// If a statement fails, skip through its next semicolon.
rule stmt = s:(let / assign / call) #recover(skip=[^;]+ ";") => $s
```

The skip expression cannot contain captures and must consume input. Recovered
errors are returned with the resulting node. Backtracking that undoes a
recovery also removes its error. A recovered expression's type is the join
of its original type and `error`, and an error value is assignable only to a type that admits `error`
(see [Type System](types.md#assignability)).
`Parser.Parse` returns a recovered value together with a `SyntaxErrors` error
listing the diagnostics. If parsing later fails, only the final syntax error
is returned. A no-value expression, including lookahead or a predicate,
remains no-value after recovery, though its error is recorded.

Recovery proceeds in these steps.

1. The annotated expression is tried. If it matches, nothing else happens.
2. If it fails, the syntax error that would have ended the parse is recorded.
3. The parser returns to the position where the annotated expression started and
   matches the skip expression there. The skip expression is matched from that
   starting position, not from the position of the failure, so the error position
   is usually inside the skipped range.
4. If the skip expression matches and consumes input, the value of the annotated
   expression is an error value and parsing continues after the skipped input.
   If it fails or consumes nothing, there is no recovery and the annotated
   expression fails as usual. A `#recover` in a repetition therefore cannot loop on
   empty skips.

The error value has the skipped input as its range and the recorded error with its
position as its message (`1:5: expected a name`); `Node.Text()` returns the
message. Its `Kind` is `KindError`, and it encodes as `{"start", "end", "error"}`
(see [Nodes and Values](nodes.md#json-encoding)).

Recovery applies to every failure inside the annotated expression, however deeply
nested; the nearest enclosing `#recover` handles it. Attributes and pragmas apply
from the innermost outward in the order written, so `@error(...) #recover(...)`
labels the expression before recovery and the recorded error carries the label,
whereas in `#recover(...) @error(...)` the label applies to the recovered
expression, which fails only when recovery is impossible, so the label is used only
in that case.

A recovery on a path that the parser then abandons is discarded with the path; a
memoized result carries its recovered errors, so a reused result still reports
them. If the parse fails in the end, only the final syntax error is returned and the
errors recovered earlier are not. When recovery is impossible, the ordinary syntax
error is returned, and its expected items include the first items of the skip
expression. The Go and command-line results are defined in
[Parsing](parsing.md#syntax-errors).

## `#stream`

`#stream` annotates a repetition at the top level of the start rule used by a
stream parse. Each matched element is handed to the caller as soon as it
matches.

```pego
// Receive a log one record at a time.
@start
rule main = ^^ header records:record* #stream $$
```

Handed elements are not included in the repetition's returned list. Once
handed over, an element is final and the parser does not backtrack before it.
If a stream diagnostic callback is selected, committed recovery errors within
an element are reported immediately before that element is handed over. A
diagnostic callback error stops the parse without handing over that element;
errors from branches later backtracked are never reported.

Therefore a rule body contains at most one `#stream`, on the complete body, a
top-level sequence element, or the expression captured by such an element.
`#recover` cannot wrap the same repetition in either order. In an ordinary
parse, `#stream` has no effect. A stream parse with no `#stream` on its start
rule is an error. The entry points, placement rules, errors and options of stream
parsing are defined in [Streaming](streaming.md).

A stream parse reads only as much input as needed and may discard each handed
element with preceding input and memoization data. Decoding a complete
character needs no future bytes; an incomplete UTF-8 prefix needs more bytes
or actual EOF, while a non-EOF reader error terminates parsing. The first
zero-width successful repetition iteration is handed over once and stops the
repetition even below its minimum; a minimum failure then returns an error
without retracting the handed value. Ordinary repetitions use the same stop
and count rule.

## `#atomic`

Postfix `expr #atomic` matches `expr` but returns a single string node
and suppresses construction of the values of the expressions inside it. It does not
commit to the match: ordinary PEG backtracking and choice behavior remain in
effect.

## `#repeat`

`#repeat` takes positional nonnegative integer bounds only. Its comma
distinguishes an exact count from an unbounded maximum: `#repeat(n)` is exactly
`n`, `#repeat(n,)` is at least `n`, `#repeat(,m)` is at most `m`, and
`#repeat(n,m)` is bounded. The pragma applies to the immediately preceding
expression; parenthesize a sequence to repeat it as a unit. Named bounds are
not accepted.


## `#missing`

`#missing` takes no arguments and may annotate only a nonempty literal. In
Closure whole-input parsing or recognition, a positive `WithMissingTokens(n)`
allows that literal to insert its entire expected string when the ordinary
match fails. Omission or zero disables insertion. A negative effective limit
is a configuration error; parse options resolve once in order, last value wins.

```pego
@start
rule main = name ";" #missing $$
rule name = [a-z]+ #atomic
```

Parsing `name` with insertion enabled produces an error value of type `missing`
at EOF and a recovered `SyntaxErrors` entry. The value's `Start` and `End` are
equal, and the string fields `expected` and `message` carry the expected literal
and diagnostic; it encodes with an `expected` member. Partial literal matches are restored completely before insertion;
no source text is invented. An enclosing `@error` labels the insertion diagnostic;
nested labels use the outermost applicable explicit label. Ordinary successful
consuming recovery keeps its existing label behavior.

Insertion succeeds at most once per literal site and position on the current
candidate path. Backtracking removes inserted values and diagnostics, but does
not refund attempted insertions. Installing events from a growing LR seed also
costs attempts unless already installed on the path. Returning a selected seed
can exhaust the budget. A selected Operators Rule candidate restores its already
charged synthesis without charging again. Budget exhaustion is a fatal
`MissingTokenLimitError` with `Limit` and `Position`; it returns no tree and is
never ordinary syntax failure. The limit bounds work, not the final missing-node
count, so abandoned alternatives can exhaust it.

Lookahead always disables insertion, including rule calls. Insertion consumes no
input and does not change repetition or LR progress: one zero-progress repetition
iteration counts once, then stops; later LR growth must advance input. Ordered
choice still selects its first successful alternative, including one that inserts.
Use lookahead when insertion must be restricted to a particular boundary.

A successful parse with insertions returns its tree and committed diagnostics in
encounter order; recognition returns the same diagnostics with no tree. A failed
whole-input parse returns only its final ordinary syntax error. Each invocation
has fresh insertion state, including concurrent and reentrant invocations.

Only Closure whole-input Node parsing and recognition currently support this
pragma. Prefix parsing, Document/streams, bytecode VMs, generated Go/TypeScript
parsers and binary serialization reject grammars containing `#missing` or
references to `missing`, even when insertion is disabled. Positive insertion
options also fail on unsupported parse entry points, including `ParsePrefix`,
even when the grammar contains no `#missing` annotation. Zero preserves strict
prefix matching for grammars without `#missing`. Grammar AST JSON and formatting
preserve the annotation. `missing` is a built-in type, a kind of `error`: the
annotated literal infers `node[string] | missing`, and `missing` is assignable to
`error`. Declare a join or keep the Node tree when
consumers need the missing-token identity. Concrete generated typed mappings and
additional backends are outside the initial implementation.
