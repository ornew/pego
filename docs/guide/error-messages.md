# Error Messages

This guide explains how ZeroParse reports syntax errors and how to make the messages useful: what an error contains, how to
get it out on the command line and in Go, and how to label the places where a user can go wrong. It is a companion to the
normative text on [attributes](../../spec/language/attributes.md) (`@error`) and [syntax
errors](../../spec/runtime/parsing.md#syntax-errors). [Recovering from Errors](recovery.md) continues with keeping the parse
going after an error.

Contents:

- [What a Syntax Error Is](#what-a-syntax-error-is): the farthest failure, the expected items, positions
- [Getting the Errors Out](#getting-the-errors-out): the command line and the Go API
- [Writing Readable Messages](#writing-readable-messages): quiet whitespace, `@error`, cuts and their pitfalls
- [Troubleshooting](#troubleshooting)

The examples use the `zp` command; build it once with `go build -o zp ./cmd/zp` (or run `go run ./cmd/zp`
instead of `zp`). Each command is followed by its output with standard output and standard error shown together, as
a terminal shows them. `zp` prints errors on standard error with the prefix `zp: ` and exits with status 1 when
the input has a syntax error, even if it recovered from all of them (the tree is then printed first, on standard
output).

A grammar block whose first line is `// name.zp` is a complete file: save it under that name. A block whose first
line is `// name.zp (changed rules)` replaces the rules of the same names in that file, and the examples after it use
the edited file. Every command and its output below were run as shown, in this order.

## What a Syntax Error Is

Listing what was expected costs time and code at every failed alternative, so a parse lists it only when it is asked to
([Recording Expectations](#recording-expectations)). The `zp` commands and the playground ask for
it, which is why the examples below show the lists.

A PEG parser backtracks: when `a / b` fails in `a`, it simply tries `b`, so "where the parse failed" is not a single
event. ZeroParse reports the **farthest failure**: the largest input position at which any expression failed to match,
together with everything that was expected there. If the whole parse fails, that is the error.

```zp
// calc.zp
@start
rule main = expr
rule expr = term (("+" / "-") term)*
rule term = factor (("*" / "/") factor)*
rule factor = number / "(" expr ")"
rule number = [0-9]+ #atomic
```

```bash
zp parse -g calc.zp -i '1+2*(3-' -f sexpr
```

```text
zp: 1:8: syntax error: expected "(", [0-9]
```

The input ends after `-`, so position 8 (line 1, column 8, one past the last character) is the farthest point, and a
`term` needs a digit or a `(` there. Everything that was tried at that position is listed without duplicates. The
items are descriptions of the primitive expressions that failed:

| Expression | Shown as |
|:--|:--|
| `"abc"` | `"abc"` (quoted, with escapes) |
| `[0-9]`, `[^;]` | the class as written |
| `.` | `any character` |
| `^^`, `$$`, `^`, `$` | `beginning of input`, `end of input`, `beginning of line`, `end of line` |

Names such as `end of input` come first, then literals, then classes, each group in byte order. A message lists at most
ten items: a longer list is cut after the tenth and ends with `, ... and N more`. `SyntaxError.Expected` keeps every
item, and [`zp explain`](debugging.md) shows the whole list.

Only the farthest position counts. A failure at an earlier position, however many alternatives were tried there, is
not reported:

```bash
zp parse -g calc.zp -i '1+*'
```

```text
zp: 1:3: syntax error: expected "(", [0-9]
```

### The End of the Input

The start rule must match the whole input. When it matches only a prefix, `end of input` is expected at the first
unconsumed position, next to whatever could have continued the match there:

```bash
zp parse -g calc.zp -i '1+2)'
```

```text
zp: 1:4: syntax error: expected end of input, "*", "+", "-", "/", [0-9]
```

An empty input fails at 1:1 like any other expression that needs something there:

```bash
zp parse -g calc.zp -i ''
```

```text
zp: 1:1: syntax error: expected "(", [0-9]
```

### Lines, Columns and Units

Lines and columns are 1-based. A tab counts as one column, and only a line feed starts a new line (in CRLF text, the
carriage return is the last column of its line). Columns are counted in the
[position unit](../../spec/runtime/parsing.md#positions): code points by default, bytes with `-unit bytes`
(`zp.WithUnit(zp.Bytes)` in Go). The two differ only after non-ASCII text:

```zp
// unit.zp
@start
rule main = "é" "x"
```

```bash
zp parse -g unit.zp -i 'éy'
```

```text
zp: 1:2: syntax error: expected "x"
```

```bash
zp parse -g unit.zp -unit bytes -i 'éy'
```

```text
zp: 1:3: syntax error: expected "x"
```

### What Is Not in the List

Failures inside a lookahead (`&a`, `!a`), Predicate Actions (`{ ... }`) and bottom (`_|_`) add nothing to the list, and
failures inside the `skip` of a [Operators Rule](../../spec/language/operators-rule.md#skip) are ignored. A lookahead that fails
usually means "this alternative is not allowed here", not "something else was expected". The consequence is that an
error caused only by such an expression is reported without any expected item, at the farthest position that did
record something, often the start of the input:

```zp
// keyword.zp
@start
rule main = !keyword ident $$
rule keyword = ("if" / "else") ![a-z]
rule ident = [a-z]+ #atomic
```

```bash
zp parse -g keyword.zp -i 'if'
```

```text
zp: 1:1: syntax error
```

A message like this is a sign that an `@error` label is missing (see [Writing Readable Messages](#writing-readable-messages)).
Whitespace rules suffer in a different way, which is the first thing to fix in a real grammar; it is shown there too.

## Getting the Errors Out

### On the Command Line

`zp parse` prints the tree on standard output and errors on standard error, as above. `-check` (recognition only)
reports the same errors without building a tree. Grammar errors (a rule that does not type-check, an unknown
attribute) are a different kind of error: they are reported with the grammar's file name and position, before any
input is read.

### In Go

`Parser.Parse` returns a node and an error. There are four outcomes, distinguished by the type of the error:

| Outcome | `node` | `err` |
|:--|:--|:--|
| The input matches | the tree | `nil` |
| The input does not match | `nil` | a `*zp.SyntaxError` |
| The parse recovered with `#recover` or an enabled `#missing` | the tree, with `error` values | `zp.SyntaxErrors`, a list of `*zp.SyntaxError` |
| An action failed at run time (for example, reading a field the node does not have) | `nil` | an ordinary error, not a syntax error |

The last row is not subject to backtracking or recovery: it aborts the parse. A `*SyntaxError` has these fields:

| Field | Meaning |
|:--|:--|
| `Pos` | Offset of the error in the position unit, counted from 0 |
| `Line`, `Col` | 1-based line and column |
| `Expected` | All the expected items, in the order of the messages (`"\"x\""` for the literal `"x"`); empty unless [expectations are recorded](#recording-expectations) |
| `Messages` | The messages of [`@error`](#error-labels), if any; empty unless expectations are recorded |

`Error()` returns `line:col: message`, and `Message()` returns the message without the position.

### Recording Expectations

A parse that is not asked to record expectations still reports where the input stopped matching: `Pos`, `Line` and `Col`
are the same. `Expected` and `Messages` are empty and `Message()` returns `syntax error`. Trees, recovered errors and
their positions do not change. A grammar whose authors wrote `@error` messages gets them only when expectations are
recorded.

| Parse | How to record |
|:--|:--|
| `Parser.Parse`, `ParseStream`, `ParsePrefix`, `Document` | Pass `zp.WithExpectations()`. A `Document` fixes it when it is created |
| Generated Go or TypeScript parser | Generate with `-expectations` (`Expectations` in `gen.Go` and `gen.TypeScript`), then pass `WithExpectations()` (Go) or `{ expectations: true }` (TypeScript) to the parse. Generating with the flag alone does not record |

```go
node, err := p.Parse(input, zp.WithExpectations())
// err: 1:8: syntax error: expected "(", [0-9]
```

A complete program that handles all of these outcomes is in [Using the Errors from Go](recovery.md#using-the-errors-from-go).

Things to keep in mind:

- With recovery, `err != nil` does **not** mean that there is no tree. A caller that does `if err != nil { return err }`
  throws the tree away; check for `SyntaxErrors` first if you want the tree.
- `SyntaxErrors` is a slice type, so `errors.As` needs a variable of that type, not a pointer type.
- The message of an `error` value (`Node.Text()`) is the formatted string `line:col: text`. The structured data (`Line`, `Col`,
  `Expected`) is only in the `SyntaxErrors` that `Parse` returns.
- `Parser.ParseStream` and `Document.Parse` report consuming recovery errors in the same way; `#missing` is not supported
  by those entry points. With the `RecognizeOnly` option there is no
  tree, so a recovering parse returns a nil node together with the `SyntaxErrors`.

## Writing Readable Messages

This section builds a small statement language, `toy.zp`, with `let x = 1 + 2;`, `print x;` and `{ ... }` blocks,
and improves its errors step by step. The first version has the usual whitespace rule:

```zp
// toy.zp
package toy

type Name = node[string]
type Num = node[string]
type Let = { Name Name; Value Expr }
type Print = { Value Expr }
type Block = { Body []Stmt }
type Sum = { Left Expr; Right Expr }
type Expr ~ Name | Num | Sum
type Stmt ~ Let | Print | Block
type Program = { Body []Stmt }

@start
rule main Program = body:stmts ws $$ => Program{Body: $body}
rule stmts = ss:(-ws s:stmt)* => map($ss, (x) => x.$s)

rule stmt Stmt = let / print / block
rule let Let = "let" ws n:name ws "=" ws v:expr ws ";" => Let{Name: $n, Value: $v}
rule print Print = "print" ws v:expr ws ";" => Print{Value: $v}
rule block Block = "{" body:stmts ws "}" => Block{Body: $body}

rule expr Expr = first:term rest:(-ws "+" -ws t:term)* => foldl($first, $rest, (acc, i) => Sum{Left: acc, Right: i.$t})
rule term Expr = number / name / group
rule group Expr = "(" ws e:expr ws ")" => $e

rule name Name = ([a-z]+) #atomic
rule number Num = ([0-9]+) #atomic
rule ws = [ \t\r\n]*
```

```bash
zp parse -g toy.zp -f sexpr -i 'let x = 1 + 2;'
```

```text
Program{Body: [Let{Name: Name("x"), Value: Sum{Left: Num("1"), Right: Num("2")}}]}
```

```bash
zp parse -g toy.zp -i 'let x = 1 +;'
```

```text
zp: 1:12: syntax error: expected "(", [ \t\r\n], [0-9], [a-z]
```

The list contains `[ \t\r\n]`: after `+`, the whitespace rule tried one more whitespace character at the failing
position, and that attempt is part of the farthest failure. Whitespace is never what the user got wrong. Because a
failure inside a lookahead is not reported, the whitespace rule can be written so that it consumes the same input
without ever recording a failure:

```zp
// toy.zp (changed rules)
// Whitespace stays out of the "expected" lists: a failure inside a lookahead is not reported.
rule ws = (&[ \t\r\n] .)*
```

```bash
zp parse -g toy.zp -i 'let x = 1 +;'
```

```text
zp: 1:12: syntax error: expected "(", [0-9], [a-z]
```

`[0-9]` and `[a-z]` are the first characters of a `number` and a `name`. The list is accurate, but for a human,
"expected an expression" says more. That is what `@error` is for.

### `@error` Labels

`@error(message="...")` is attached to an expression like a postfix operator. If the expression fails, every expected
item recorded **inside** it is replaced by the message, which is reported at the farthest position reached inside the
expression. If the expression succeeds, nothing changes. A message is part of the recorded expectations: a parse that
does not [record them](#recording-expectations) reports `syntax error` instead.

Here are the labels for `toy.zp`. The statement rules are rewritten so that each place a user can go wrong has a
label, and `term` is split so that its label covers only the "no expression here" case:

```zp
// toy.zp (changed rules)
rule let Let = (
    "let" ws n:name @error(message="expected a name") ws "=" @error(message="expected '='")
    ws v:expr ws ";" @error(message="expected ';'")
) => Let{Name: $n, Value: $v}
rule print Print = "print" ws v:expr ws ";" @error(message="expected ';'") => Print{Value: $v}
rule block Block = "{" body:stmts ws "}" @error(message="expected '}'") => Block{Body: $body}

rule term Expr = (number / name) @error(message="expected an expression") / group
rule group Expr = "(" ws e:expr ws ")" @error(message="expected ')'") => $e
```

```bash
zp parse -g toy.zp -i 'let x = 1 +;'
```

```text
zp: 1:12: expected an expression
```

```bash
zp parse -g toy.zp -i 'let x = (1 + 2;'
```

```text
zp: 1:15: expected ')'
```

```bash
zp parse -g toy.zp -i 'let = 1;'
```

```text
zp: 1:5: expected a name
```

```bash
zp parse -g toy.zp -i 'let x 1;'
```

```text
zp: 1:7: expected '='
```

```bash
zp parse -g toy.zp -i '{ print 1; let y = 2;'
```

```text
zp: 1:22: expected '}'
```

How to place a label:

- **Attach it to the smallest expression that fails.** An attribute applies to the expression immediately before it:
  `"=" @error(...)` labels the literal, while `("let" ws name ws "=") @error(...)` labels the whole group.
- **The message replaces everything at its position, including the expectations of sibling alternatives.** In
  `term`, a label on `name` alone would say "expected a name" where a number or a `(` is just as valid. That is why the
  label covers `(number / name)` and the parenthesized `group` stays outside it: if the label sat on the whole choice,
  it would replace the more specific `expected ')'`, because an outer `@error` also swallows the messages of inner
  ones.
- **The position is the farthest point inside the expression**, not its start: for `let x = 1 +;` the message points at
  the `;`.
- **Labels compete with each other by position.** Only what is recorded at the overall farthest position survives. A
  label on an alternative that fails early is dropped as soon as another alternative gets further. If two labels
  are recorded at the same position, both are shown, joined with `; `.

The last two points are easiest to see in a tiny grammar:

```zp
// labels.zp
@start(default)
rule joined = "a" @error(message="m1") / "b" @error(message="m2")
@start
rule dropped = "a" "b" @error(message="after a") / "a" "c" "d"
```

```bash
zp parse -g labels.zp -s joined -i 'x'
```

```text
zp: 1:1: m1; m2
```

```bash
zp parse -g labels.zp -s dropped -i 'ax'
```

```text
zp: 1:2: after a
```

```bash
zp parse -g labels.zp -s dropped -i 'acx'
```

```text
zp: 1:3: syntax error: expected "d"
```

For `ax` the first alternative fails at the `b` (column 2) and its message is the only thing recorded there. For `acx`
the second alternative gets to column 3, and the label of the first one is gone.

### Labels on Predicate Actions and on `_|_`

A [predicate](../../spec/language/predicates.md) that fails records nothing, so a failed check shows up as a bare
`syntax error`. Attach `@error` to it. The same goes for a rule that exists only to report a message: `_|_` always
fails and can carry the label:

```zp
// xmlish.zp
@start
rule pair = "<" a:name ">" "</" b:name {text($a) == text($b)} @error(message="mismatched end tag") ">"
rule name = [a-z]+ #atomic
rule unknown = "x" / _|_ @error(message="expected 'x'")
```

```bash
zp parse -g xmlish.zp -s pair -i '<a></b>'
```

```text
zp: 1:7: mismatched end tag
```

(Without the label, this input fails with `expected [a-z]`: the farthest recorded failure is the name rule looking
for one more letter.)

### Where the Error Is Reported Relative to Whitespace

Whitespace rules read past line ends. A missing `;` at the end of a line is reported on the next line, at the first
token that did not fit, because the whitespace before the `;` (and before the optional `+` of `expr`) consumed the
newline while looking for more input:

```bash
zp parse -g toy.zp -i $'print x\nprint y;\n'
```

```text
zp: 2:1: expected ';'
```

This is the position of the unexpected token, which is what most parsers report. Pointing at the end of the previous
line would require every place that can continue the statement to skip only spaces and tabs, which is rarely worth it.

### Cuts and Error Messages

A cut (`#cut`) does not change how the farthest failure is chosen. What it does is stop the enclosing choice from trying
its other alternatives, so their failures are never recorded. When a keyword makes the rest of the alternative
unambiguous, a cut also removes the expectations of the generic alternatives that would otherwise have failed at the
same position:

```zp
// cut.zp
@start(default)
rule plain = stmt $$
rule stmt = let / call
rule let = "let" ws name ws "=" ws num ";"
rule call = name ws "(" ")" ";"

// The cut and the choice it commits are in the same rule.
@start
rule committed = stmt_cut $$
rule stmt_cut = "let" #cut ws name ws "=" ws num ";" / call

// The cut is in another rule than the choice, so it commits nothing.
@start
rule elsewhere = stmt_far $$
rule stmt_far = let_cut / call
rule let_cut = "let" #cut ws name ws "=" ws num ";"

rule name = [a-z]+
rule num = [0-9]+
rule ws = (&[ \t\n] .)*
```

```bash
zp parse -g cut.zp -s plain -i 'let 5;'
```

```text
zp: 1:5: syntax error: expected "(", [a-z]
```

```bash
zp parse -g cut.zp -s committed -i 'let 5;'
```

```text
zp: 1:5: syntax error: expected [a-z]
```

```bash
zp parse -g cut.zp -s elsewhere -i 'let 5;'
```

```text
zp: 1:5: syntax error: expected "(", [a-z]
```

Without the cut, `call` also read `let` as a name and expected a `(` after it. With the cut in the same rule as the
choice, the parser knows it is in a `let` statement and reports only what a `let` needs. The third rule shows the
common mistake: a cut commits only the choice of the rule it is written in. Two more cautions:

- The cut is a promise that the other alternatives cannot match. Here `let();`, a call of a function named `let`, is
  valid for `plain` and an error for `committed`. If `let` must remain usable as a name, do not use a cut.
- A cut can hide a better error as well as a worse one: after it, an alternative that would have got further is never
  tried. If a diagnosis looks worse after adding a cut, that is why.

## Troubleshooting

| Symptom | Likely cause | What to do |
|:--|:--|:--|
| The expected list contains `[ \t\r\n]` | The whitespace rule recorded a failure at the farthest position | Write `ws` as `(&[ \t\r\n] .)*`, or label the place |
| `1:1: syntax error` with no expected items | The failure came only from a lookahead, a predicate or `_|_`, which record nothing | Add `@error` to the predicate or to a rule built on `_|_` |
| A label does not appear | The expression it is on failed at an earlier position than another alternative, or an outer `@error` replaced it | Attach labels to the smallest expression; check which alternative reached furthest |
| A label hides valid alternatives | The message replaces every item recorded at its position, including siblings | Label the whole choice, not one alternative |
| The reported error is on the line after the mistake | Whitespace is skipped before the next token, over a newline | See [Where the error is reported relative to whitespace](#where-the-error-is-reported-relative-to-whitespace) |

## Related Topics

- [Recovering from Errors](recovery.md): `#recover`, `#missing` and several errors in one run.
- [Rule Expressions](rule-expressions.md): the cuts and lookaheads that decide where a parse fails.
- [Debugging and Profiling](debugging.md): `zp explain` shows where each expected item comes from.
- [Testing Grammars](testing-grammars.md): checking the errors a grammar reports.
