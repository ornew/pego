# Error Reporting and Recovery

This guide explains how PEGO reports syntax errors, how to make the messages useful, and how to keep parsing after an
error so that one run reports every problem in a file. It is a companion to the normative text in
[spec/attributes.md](../../spec/attributes.md) (`#error`, `#recover` and
[syntax errors](../../spec/attributes.md#syntax-errors)); the reasoning behind the design is recorded in
[docs/design/006](../design/006-error-reporting-and-recovery.md).

Contents:

1. [What a syntax error is](#1-what-a-syntax-error-is): the farthest failure, the expected items, positions
2. [Getting the errors out](#2-getting-the-errors-out): the command line and the Go API
3. [Writing readable messages](#3-writing-readable-messages): quiet whitespace, `#error`, cuts and their pitfalls
4. [Recovering from errors](#4-recovering-from-errors): `#recover`, `Error` nodes, several errors in one run
5. [Recipes](#5-recipes): a missing closing bracket, skipping to the next statement or line, recovering inside a list
6. [Troubleshooting](#6-troubleshooting)

The examples use the `pego` command; build it once with `go build -o pego ./cmd/pego` (or run `go run ./cmd/pego`
instead of `pego`). Each command is followed by its output with standard output and standard error shown together, as
a terminal shows them. `pego` prints errors on standard error with the prefix `pego: ` and exits with status 1 when
the input has a syntax error, even if it recovered from all of them (the tree is then printed first, on standard
output).

A grammar block whose first line is `// name.pego` is a complete file: save it under that name. A block whose first
line is `// name.pego (changed rules)` replaces the rules of the same names in that file, and the examples after it use
the edited file. Every command and its output below were run as shown, in this order.

## 1. What a syntax error is

A PEG parser backtracks: when `a / b` fails in `a`, it simply tries `b`, so "where the parse failed" is not a single
event. PEGO reports the **farthest failure**: the largest input position at which any expression failed to match,
together with everything that was expected there. If the whole parse fails, that is the error.

```pego
// calc.pego
def main = expr
def expr = term (("+" / "-") term)*
def term = factor (("*" / "/") factor)*
def factor = number / "(" expr ")"
def number = @((?0-9)+)
```

```bash
pego parse -g calc.pego -i '1+2*(3-' -f sexpr
```

```text
pego: 1:8: syntax error: expected "(", (?0-9)
```

The input ends after `-`, so position 8 (line 1, column 8, one past the last character) is the farthest point, and a
`term` needs a digit or a `(` there. Everything that was tried at that position is listed, sorted and without
duplicates. The items are descriptions of the primitive expressions that failed:

| Expression | Shown as |
|:--|:--|
| `"abc"` | `"abc"` (quoted, with escapes) |
| `(?0-9)`, `(?^;)` | the class as written |
| `.` | `any character` |
| `^^`, `$$`, `^`, `$` | `beginning of input`, `end of input`, `beginning of line`, `end of line` |

Only the farthest position counts. A failure at an earlier position, however many alternatives were tried there, is
not reported:

```bash
pego parse -g calc.pego -i '1+*'
```

```text
pego: 1:3: syntax error: expected "(", (?0-9)
```

### The end of the input

The start rule must match the whole input. When it matches only a prefix, `end of input` is expected at the first
unconsumed position, next to whatever could have continued the match there:

```bash
pego parse -g calc.pego -i '1+2)'
```

```text
pego: 1:4: syntax error: expected "*", "+", "-", "/", (?0-9), end of input
```

An empty input fails at 1:1 like any other expression that needs something there:

```bash
pego parse -g calc.pego -i ''
```

```text
pego: 1:1: syntax error: expected "(", (?0-9)
```

### Lines, columns and units

Lines and columns are 1-based. A tab counts as one column, and only a line feed starts a new line (in CRLF text, the
carriage return is the last column of its line). Columns are counted in the
[position unit](../../spec/overview.md#positions): code points by default, bytes with `-unit bytes`
(`pego.WithUnit(pego.Bytes)` in Go). The two differ only after non-ASCII text:

```pego
// unit.pego
def main = "é" "x"
```

```bash
pego parse -g unit.pego -i 'éy'
```

```text
pego: 1:2: syntax error: expected "x"
```

```bash
pego parse -g unit.pego -unit bytes -i 'éy'
```

```text
pego: 1:3: syntax error: expected "x"
```

### What is not in the list

Failures inside a lookahead (`&a`, `!a`), predicates (`[...]`) and bottom (`_|_`) add nothing to the list, and
failures inside the `skip` of a [Pratt expression](../../spec/pratt.md#skip) are ignored. A lookahead that fails
usually means "this alternative is not allowed here", not "something else was expected". The consequence is that an
error caused only by such an expression is reported without any expected item, at the farthest position that did
record something, often the start of the input:

```pego
// keyword.pego
def main = !keyword ident $$
def keyword = ("if" / "else") !(?a-z)
def ident = @((?a-z)+)
```

```bash
pego parse -g keyword.pego -i 'if'
```

```text
pego: 1:1: syntax error
```

A message like this is a sign that an `#error` label is missing (see [section 3](#3-writing-readable-messages)).
Whitespace rules suffer in a different way, which is the first thing to fix in a real grammar; it is shown there too.

## 2. Getting the errors out

### On the command line

`pego parse` prints the tree on standard output and errors on standard error, as above. `-check` (recognition only)
reports the same errors without building a tree. Grammar errors (a rule that does not type-check, an unknown
attribute) are a different kind of error: they are reported with the grammar's file name and position, before any
input is read.

### In Go

`Parser.Parse` returns a node and an error. There are four outcomes, distinguished by the type of the error:

| Outcome | `node` | `err` |
|:--|:--|:--|
| The input matches | the tree | `nil` |
| The input does not match | `nil` | a `*pego.SyntaxError` |
| The parse recovered from errors with `#recover` | the tree, with `Error` nodes | `pego.SyntaxErrors`, a list of `*pego.SyntaxError` |
| An action failed at run time (for example, reading a field the node does not have) | `nil` | an ordinary error, not a syntax error |

The last row is not subject to backtracking or recovery: it aborts the parse. A `*SyntaxError` has these fields:

| Field | Meaning |
|:--|:--|
| `Pos` | Offset of the error in the position unit, counted from 0 |
| `Line`, `Col` | 1-based line and column |
| `Expected` | The expected items, sorted, as shown in the messages (`"\"x\""` for the literal `"x"`) |
| `Messages` | The messages of [`#error`](#error-labels), if any |

`Error()` returns `line:col: message`, and `Message()` returns the message without the position.

A complete program that handles all of these outcomes is at the end of [section 4](#using-the-errors-from-go).

Things to keep in mind:

- With recovery, `err != nil` does **not** mean that there is no tree. A caller that does `if err != nil { return err }`
  throws the tree away; check for `SyntaxErrors` first if you want the tree.
- `SyntaxErrors` is a slice type, so `errors.As` needs a variable of that type, not a pointer type.
- The `message` field of an `Error` node is the formatted string `line:col: text`. The structured data (`Line`, `Col`,
  `Expected`) is only in the `SyntaxErrors` that `Parse` returns.
- `Parser.ParseStream` and `Document.Parse` report errors in the same way. With the `RecognizeOnly` option there is no
  tree, so a recovering parse returns a nil node together with the `SyntaxErrors`.

## 3. Writing readable messages

This section builds a small statement language, `toy.pego`, with `let x = 1 + 2;`, `print x;` and `{ ... }` blocks,
and improves its errors step by step. The first version has the usual whitespace rule:

```pego
// toy.pego
package toy

type Name terminal
type Num terminal
type Let struct { Name Name, Value Expr }
type Print struct { Value Expr }
type Block struct { Body []Stmt }
type Sum struct { Left Expr, Right Expr }
type Expr = Name | Num | Sum
type Stmt = Let | Print | Block
type Program struct { Body []Stmt }

def main: Program = body:stmts ws $$ -> new Program{Body: $body}
def stmts = ss:(-ws s:stmt)* -> map($ss, (x) => $x.s)

def stmt: Stmt = let / print / block
def let: Let = "let" ws n:name ws "=" ws v:expr ws ";" -> new Let{Name: $n, Value: $v}
def print: Print = "print" ws v:expr ws ";" -> new Print{Value: $v}
def block: Block = "{" body:stmts ws "}" -> new Block{Body: $body}

def expr: Expr = first:term rest:(-ws "+" -ws t:term)*
    -> foldl($first, $rest, (acc, i) => new Sum{Left: $acc, Right: $i.t})
def term: Expr = number / name / group
def group: Expr = "(" ws e:expr ws ")" -> $e

def name: Name = (?a-z)+
def number: Num = (?0-9)+
def ws = (? \t\r\n)*
```

```bash
pego parse -g toy.pego -f sexpr -i 'let x = 1 + 2;'
```

```text
(Program Body=[(Let Name=Name"x"@name Value=(Sum Left=Num"1"@number Right=Num"2"@number))])
```

```bash
pego parse -g toy.pego -i 'let x = 1 +;'
```

```text
pego: 1:12: syntax error: expected "(", (? \t\r\n), (?0-9), (?a-z)
```

The list contains `(? \t\r\n)`: after `+`, the whitespace rule tried one more whitespace character at the failing
position, and that attempt is part of the farthest failure. Whitespace is never what the user got wrong. Because a
failure inside a lookahead is not reported, the whitespace rule can be written so that it consumes the same input
without ever recording a failure:

```pego
// toy.pego (changed rules)
// Whitespace stays out of the "expected" lists: a failure inside a lookahead is not reported.
def ws = (&(? \t\r\n) .)*
```

```bash
pego parse -g toy.pego -i 'let x = 1 +;'
```

```text
pego: 1:12: syntax error: expected "(", (?0-9), (?a-z)
```

`(?0-9)` and `(?a-z)` are the first characters of a `number` and a `name`. The list is accurate, but for a human,
"expected an expression" says more. That is what `#error` is for.

### `#error` labels

`#error(message="...")` is attached to an expression like a postfix operator. If the expression fails, every expected
item recorded **inside** it is replaced by the message, which is reported at the farthest position reached inside the
expression. If the expression succeeds, nothing changes.

Here are the labels for `toy.pego`. The statement rules are rewritten so that each place a user can go wrong has a
label, and `term` is split so that its label covers only the "no expression here" case:

```pego
// toy.pego (changed rules)
def let: Let = "let" ws n:name #error(message="expected a name") ws "=" #error(message="expected '='")
    ws v:expr ws ";" #error(message="expected ';'") -> new Let{Name: $n, Value: $v}
def print: Print = "print" ws v:expr ws ";" #error(message="expected ';'") -> new Print{Value: $v}
def block: Block = "{" body:stmts ws "}" #error(message="expected '}'") -> new Block{Body: $body}

def term: Expr = (number / name) #error(message="expected an expression") / group
def group: Expr = "(" ws e:expr ws ")" #error(message="expected ')'") -> $e
```

```bash
pego parse -g toy.pego -i 'let x = 1 +;'
```

```text
pego: 1:12: expected an expression
```

```bash
pego parse -g toy.pego -i 'let x = (1 + 2;'
```

```text
pego: 1:15: expected ')'
```

```bash
pego parse -g toy.pego -i 'let = 1;'
```

```text
pego: 1:5: expected a name
```

```bash
pego parse -g toy.pego -i 'let x 1;'
```

```text
pego: 1:7: expected '='
```

```bash
pego parse -g toy.pego -i '{ print 1; let y = 2;'
```

```text
pego: 1:22: expected '}'
```

How to place a label:

- **Attach it to the smallest expression that fails.** An attribute applies to the expression immediately before it:
  `"=" #error(...)` labels the literal, while `("let" ws name ws "=") #error(...)` labels the whole group.
- **The message replaces everything at its position, including the expectations of sibling alternatives.** In
  `term`, a label on `name` alone would say "expected a name" where a number or a `(` is just as valid. That is why the
  label covers `(number / name)` and the parenthesized `group` stays outside it: if the label sat on the whole choice,
  it would replace the more specific `expected ')'`, because an outer `#error` also swallows the messages of inner
  ones.
- **The position is the farthest point inside the expression**, not its start: for `let x = 1 +;` the message points at
  the `;`.
- **Labels compete with each other by position.** Only what is recorded at the overall farthest position survives. A
  label on an alternative that fails early is dropped as soon as another alternative gets further. If two labels
  are recorded at the same position, both are shown, joined with `; `.

The last two points are easiest to see in a tiny grammar:

```pego
// labels.pego
def joined = "a" #error(message="m1") / "b" #error(message="m2")
def dropped = "a" "b" #error(message="after a") / "a" "c" "d"
```

```bash
pego parse -g labels.pego -s joined -i 'x'
```

```text
pego: 1:1: m1; m2
```

```bash
pego parse -g labels.pego -s dropped -i 'ax'
```

```text
pego: 1:2: after a
```

```bash
pego parse -g labels.pego -s dropped -i 'acx'
```

```text
pego: 1:3: syntax error: expected "d"
```

For `ax` the first alternative fails at the `b` (column 2) and its message is the only thing recorded there. For `acx`
the second alternative gets to column 3, and the label of the first one is gone.

### Labels on predicates and on `_|_`

A [predicate](../../spec/predicates.md) that fails records nothing, so a failed check shows up as a bare
`syntax error`. Attach `#error` to it. The same goes for a rule that exists only to report a message: `_|_` always
fails and can carry the label:

```pego
// xmlish.pego
def pair = "<" a:name ">" "</" b:name [text($a) == text($b)] #error(message="mismatched end tag") ">"
def name = @((?a-z)+)
def unknown = "x" / _|_ #error(message="expected 'x'")
```

```bash
pego parse -g xmlish.pego -s pair -i '<a></b>'
```

```text
pego: 1:7: mismatched end tag
```

(Without the label, this input fails with `expected (?a-z)`: the farthest recorded failure is the name rule looking
for one more letter.)

### Where the error is reported relative to whitespace

Whitespace rules read past line ends. A missing `;` at the end of a line is reported on the next line, at the first
token that did not fit, because the whitespace before the `;` (and before the optional `+` of `expr`) consumed the
newline while looking for more input:

```bash
pego parse -g toy.pego -i $'print x\nprint y;\n'
```

```text
pego: 2:1: expected ';'
```

This is the position of the unexpected token, which is what most parsers report. Pointing at the end of the previous
line would require every place that can continue the statement to skip only spaces and tabs, which is rarely worth it.

### Cuts and error messages

A cut (`--`) does not change how the farthest failure is chosen. What it does is stop the enclosing choice from trying
its other alternatives, so their failures are never recorded. When a keyword makes the rest of the alternative
unambiguous, a cut also removes the expectations of the generic alternatives that would otherwise have failed at the
same position:

```pego
// cut.pego
def plain = stmt $$
def stmt = let / call
def let = "let" ws name ws "=" ws num ";"
def call = name ws "(" ")" ";"

// The cut and the choice it commits are in the same rule.
def committed = stmt_cut $$
def stmt_cut = "let" -- ws name ws "=" ws num ";" / call

// The cut is in another rule than the choice, so it commits nothing.
def elsewhere = stmt_far $$
def stmt_far = let_cut / call
def let_cut = "let" -- ws name ws "=" ws num ";"

def name = (?a-z)+
def num = (?0-9)+
def ws = (&(? \t\n) .)*
```

```bash
pego parse -g cut.pego -s plain -i 'let 5;'
```

```text
pego: 1:5: syntax error: expected "(", (?a-z)
```

```bash
pego parse -g cut.pego -s committed -i 'let 5;'
```

```text
pego: 1:5: syntax error: expected (?a-z)
```

```bash
pego parse -g cut.pego -s elsewhere -i 'let 5;'
```

```text
pego: 1:5: syntax error: expected "(", (?a-z)
```

Without the cut, `call` also read `let` as a name and expected a `(` after it. With the cut in the same rule as the
choice, the parser knows it is in a `let` statement and reports only what a `let` needs. The third rule shows the
common mistake: a cut commits only the choice of the rule it is written in. Two more cautions:

- The cut is a promise that the other alternatives cannot match. Here `let();`, a call of a function named `let`, is
  valid for `plain` and an error for `committed`. If `let` must remain usable as a name, do not use a cut.
- A cut can hide a better error as well as a worse one: after it, an alternative that would have got further is never
  tried. If a diagnosis looks worse after adding a cut, that is why.

## 4. Recovering from errors

Without recovery, the first error ends the parse. A language server, a linter or a compiler that wants to report all
problems in a file needs the parser to continue. `#recover(skip=e)` turns a failure into a success:

1. The expression is tried. If it matches, nothing happens.
2. If it fails, the syntax error is **recorded** (what would have been the error of the whole parse).
3. The parser goes back to where the expression **started** and matches `e` there. The input matched by `e` is
   skipped.
4. The value of the expression becomes an `Error` node that covers the skipped input, and parsing continues after it.

If `e` fails, or matches nothing, there is no recovery and the expression fails as usual (so a `#recover` in a
repetition cannot loop forever on empty skips).

Let us recover at the statement level: when a statement does not parse, skip to the end of its line or to the next
`;`, whichever comes first, but never past a brace.

```pego
// toy.pego (changed rules)
// If a statement does not parse, skip to the next ";" (but never past a brace) and carry on.
def stmt: Stmt = s:(let / print / block) #recover(skip=(?^;{}\n)+ ";"?) -> $s
```

```bash
pego parse -g toy.pego -f sexpr -i $'let a = 1;\nlet = 2;\nprint (a +);\nprint a\nlet b = a + 1;\n{ print 1 let c = ; print 2; }\nprint b;\n'
```

```text
(Program Body=[(Let Name=Name"a"@name Value=Num"1"@number) Error"let = 2;"{message=`2:5: expected a name`} Error"print (a +);"{message=`3:11: expected an expression`} Error"print a"{message=`5:1: expected ';'`} (Let Name=Name"b"@name Value=(Sum Left=Name"a"@name Right=Num"1"@number)) (Block Body=[Error"print 1 let c = ;"{message=`6:11: expected ';'`} (Print Value=Num"2"@number)]) (Print Value=Name"b"@name)])
pego: 2:5: expected a name
3:11: expected an expression
5:1: expected ';'
6:11: expected ';'
```

Four errors are reported from one run, and the tree is complete except at the broken statements, which are `Error`
nodes in the `Body` list. The type of `stmt` is `Stmt` plus `Error`; `Error` is assignable to every node type, which is
why `Body []Stmt` can hold it. In JSON, the default output format, an `Error` node looks like this:

```bash
pego parse -g toy.pego -i $'let = 2;\nprint 1;\n'
```

```text
{
  "type": "Program",
  "start": 0,
  "end": 18,
  "fields": {
    "Body": {
      "type": "List",
      "start": 0,
      "end": 17,
      "children": [
        {
          "type": "Error",
          "start": 0,
          "end": 8,
          "text": "let = 2;",
          "fields": {
            "message": "1:5: expected a name"
          }
        },
        {
          "type": "Print",
          "start": 9,
          "end": 17,
          "fields": {
            "Value": {
              "type": "Num",
              "rule": "number",
              "start": 15,
              "end": 16,
              "text": "1"
            }
          }
        }
      ]
    }
  }
}
pego: 1:5: expected a name
```

`start`/`end` delimit the skipped text, and `message` is the recorded error, with its position, as a string.

Details worth knowing:

- **The skip starts where the expression started, not where it failed.** The error position (here `1:5`, inside
  `let = 2;`) is usually inside the skipped range. `skip` is matched against the whole broken statement from its first
  character, so it must be written as "the rest of this construct", not "the rest after the error".
- **Recovery turns an expression's failure into success, so it applies to every failure inside it, however deep.**
  `s:(let / print / block) #recover(...)` recovers from a failure in any of the three alternatives, including a
  failure far inside a nested expression. The nearest enclosing `#recover` wins: in the example, a broken statement
  inside a block is recovered by the `stmt` rule of the block's own body, not by the outer statement.
- **Order of attributes matters.** Attributes apply from the innermost outward in the order written. Write
  `#error(...) #recover(...)`: the label is applied first, so the recovered error carries the label. With the order
  reversed, `#error` wraps a `#recover` that almost never fails, so the label is never used:

```pego
// order.pego
def good = (stmt #error(message="bad statement") #recover(skip=(?^;)* ";"))* $$
def bad = (stmt #recover(skip=(?^;)* ";") #error(message="bad statement"))* $$
def stmt = "a" ";"
```

```bash
pego parse -g order.pego -s good -f sexpr -i 'a;b;a;'
```

```text
(Seq [(Seq "a" ";")@stmt Error"b;"{message=`1:3: bad statement`} (Seq "a" ";")@stmt])@good
pego: 1:3: bad statement
```

```bash
pego parse -g order.pego -s bad -f sexpr -i 'a;b;a;'
```

```text
(Seq [(Seq "a" ";")@stmt Error"b;"{message=`1:3: syntax error: expected "a"`} (Seq "a" ";")@stmt])@bad
pego: 1:3: syntax error: expected "a"
```

### What happens to the errors

**Where they go.** `Parser.Parse` returns the tree and `SyntaxErrors` (see [section 2](#2-getting-the-errors-out)); the
command line prints the tree, then each error on its own line, and exits with status 1.

**A recovery that is later undone is not reported.** Recovery can happen on a path that the parser then backtracks from
(an alternative that fails after the recovered part). The errors recorded on that path are discarded with the path, and
a memoized result carries its recovered errors with it, so a reused result still reports them.

**If the parse fails in the end, only the last syntax error is returned**, not the errors that were recovered before it.
This matters most for unclosed constructs. The statements recovered inside a block that never closes are on a path that
failed, so a single missing `}` at the end of the file replaces all the earlier reports with one:

```bash
pego parse -g toy.pego -i $'{ let = 1;\nprint 2;\n'
```

```text
pego: 3:1: expected '}'
```

Here `let = 1;` had been recovered inside the block, but the block as a whole failed at the end of the input (see the
[recipe for a missing closing bracket](#report-a-missing-closing-bracket)).

**When recovery is impossible**, because `skip` cannot match at that position, you get the ordinary syntax error. Its
list of expected items also contains the class that `skip` tried to match:

```bash
pego parse -g toy.pego -i $'print 1;\n}\n'
```

```text
pego: 2:1: syntax error: expected "let", "print", "{", (?^;{}\n), end of input
```

The stray `}` stops every recovery in this grammar, because `skip` refuses to eat braces. That is the right trade-off
for a block-structured language, but if you want to report stray closing braces too, the top-level rule has to handle
them.

### Using the errors from Go

The following program parses three inputs with the finished `toy.pego` and handles each outcome. Run it in the directory
that contains `toy.pego` (the module that contains it must depend on `github.com/ornew/pego`).

```go
// main.go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/ornew/pego"
)

// errorNodes collects the Error nodes of a tree.
func errorNodes(n *pego.Node, out *[]*pego.Node) {
	if n == nil {
		return
	}
	if n.Type() == "Error" {
		*out = append(*out, n)
		return
	}
	for _, c := range n.Children {
		errorNodes(c, out)
	}
	for _, f := range n.Fields {
		if child, ok := f.Value.(*pego.Node); ok {
			errorNodes(child, out)
		}
	}
}

func main() {
	src, err := os.ReadFile("toy.pego")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, input := range []string{
		"print 1;\n",
		"let = 2;\nprint (a +);\nprint 3;\n",
		"print 1;\n}\n",
	} {
		fmt.Printf("--- %q\n", input)
		node, err := p.Parse(input)

		var recovered pego.SyntaxErrors
		var failed *pego.SyntaxError
		switch {
		case err == nil:
			fmt.Println("ok:", node)
		case errors.As(err, &recovered):
			// The parse succeeded after recovering: there is a tree and a list of errors.
			fmt.Printf("recovered from %d error(s); tree: %v\n", len(recovered), node)
			for _, e := range recovered {
				fmt.Printf("  line %d, column %d (offset %d): %s\n", e.Line, e.Col, e.Pos, e.Message())
			}
			var nodes []*pego.Node
			errorNodes(node, &nodes)
			for _, n := range nodes {
				msg, _ := n.Fields.Get("message")
				fmt.Printf("  Error node [%d,%d) %q: %v\n", n.Start, n.End, n.Text, msg)
			}
		case errors.As(err, &failed):
			// The parse failed: there is no tree, only the last error.
			fmt.Printf("failed: %v (node is nil: %v)\n", failed, node == nil)
			fmt.Printf("  expected=%q messages=%q\n", failed.Expected, failed.Messages)
		default:
			fmt.Println("other error:", err) // for example, a runtime error in an action
		}
	}
}
```

```text
--- "print 1;\n"
ok: (Program Body=[(Print Value=Num"1"@number)])
--- "let = 2;\nprint (a +);\nprint 3;\n"
recovered from 2 error(s); tree: (Program Body=[Error"let = 2;"{message=`1:5: expected a name`} Error"print (a +);"{message=`2:11: expected an expression`} (Print Value=Num"3"@number)])
  line 1, column 5 (offset 4): expected a name
  line 2, column 11 (offset 19): expected an expression
  Error node [0,8) "let = 2;": 1:5: expected a name
  Error node [9,21) "print (a +);": 2:11: expected an expression
--- "print 1;\n}\n"
failed: 2:1: syntax error: expected "let", "print", "{", (?^;{}\n), end of input (node is nil: true)
  expected=["\"let\"" "\"print\"" "\"{\"" "(?^;{}\\n)" "end of input"] messages=[]
```

### Do not let the skip eat the closer

A recovery inside a repetition is attempted every time an element fails to parse, including the normal end of the
repetition. When the closing bracket of the enclosing construct can be matched by `skip`, recovery swallows it. This is
what the `toy.pego` statement rule does if `{}` is not excluded from the skip:

```pego
// toy_greedy.pego
package toy

type Name terminal
type Num terminal
type Let struct { Name Name, Value Expr }
type Print struct { Value Expr }
type Block struct { Body []Stmt }
type Expr = Name | Num
type Stmt = Let | Print | Block
type Program struct { Body []Stmt }

def main: Program = body:stmts ws $$ -> new Program{Body: $body}
def stmts = ss:(-ws s:stmt)* -> map($ss, (x) => $x.s)

def stmt: Stmt = s:(let / print / block) #recover(skip=(?^;\n)+ ";"?) -> $s
def let: Let = "let" ws n:name ws "=" ws v:expr ws ";" -> new Let{Name: $n, Value: $v}
def print: Print = "print" ws v:expr ws ";" -> new Print{Value: $v}
def block: Block = "{" body:stmts ws "}" #error(message="expected '}'") -> new Block{Body: $body}

def expr: Expr = number / name
def name: Name = (?a-z)+
def number: Num = (?0-9)+
def ws = (&(? \t\r\n) .)*
```

```bash
pego parse -g toy_greedy.pego -f sexpr -i $'{ print 1; }\nprint 2;\n'
```

```text
(Program Body=[Error"{ print 1;"{message=`3:1: expected '}'`} Error"}"{message=`1:12: syntax error: expected "let", "print", "{"`} (Print Value=Num"2"@number)])
pego: 3:1: expected '}'
1:12: syntax error: expected "let", "print", "{"
```

The valid input `{ print 1; }` is reported as erroneous. At the `}`, the `stmt` rule inside the block fails (a `}` is
not a statement), and `skip` consumes the `}` as the "rest of a broken line". The block runs on to the end of the input
without finding its closing brace, and the outer statement recoveries then produce the two errors shown. The fix is the
one `toy.pego` uses: `skip` must not be able to start at, or run over, the token that ends the enclosing repetition. The
same applies to `)`, `]`, `end` and so on. In general, `skip` should describe "the rest of this element" and be unable
to match at the delimiter that closes the list the element is in.

## 5. Recipes

### Report a missing closing bracket

A missing `)`, `]` or `}` fails at the position where the closer was expected, which is where the parser could not
continue. Label the closer, not the group, so that errors inside the group keep their own messages. This is how
`toy.pego` does it:

```pego
def group: Expr = "(" ws e:expr ws ")" #error(message="expected ')'") -> $e
```

(Writing `("(" ws e:expr ws ")") #error(...)` instead would replace the messages of everything inside the group with the
one message.) With the statement-level recovery of section 4, a missing `)` costs one statement and nothing else:

```bash
pego parse -g toy.pego -f sexpr -i $'let x = (1 + 2;\nprint x;\n'
```

```text
(Program Body=[Error"let x = (1 + 2;"{message=`1:15: expected ')'`} (Print Value=Name"x"@name)])
pego: 1:15: expected ')'
```

The same holds inside a block: the broken statement becomes an `Error` node, and the statements after it, up to the
block's own `}`, are parsed normally.

```bash
pego parse -g toy.pego -f sexpr -i $'{ print (1; let y = 2;\n  print 3;\n}\n'
```

```text
(Program Body=[(Block Body=[Error"print (1;"{message=`1:11: expected ')'`} (Let Name=Name"y"@name Value=Num"2"@number) (Print Value=Num"3"@number)])])
pego: 1:11: expected ')'
```

A missing closer for a construct that spans several statements is different, because there is nothing left to skip:

```bash
pego parse -g toy.pego -f sexpr -i $'{ print 1;\nprint 2;\n'
```

```text
pego: 3:1: expected '}'
```

- The error is at the end of the input, where the `}` was expected. It cannot point at the *opening* brace: errors are
  reported at the farthest position, and the opener is earlier than whatever failed after it. Put what the user needs
  into the message text (`expected '}' to close the block`).
- The parse fails as a whole: there is no tree, and the errors recovered earlier are dropped (see "If the parse fails
  in the end" in section 4). `skip` of the statement rule refuses to start at `{`, and an unclosed block leaves nothing
  to skip anyway, so nothing can recover it.

### Skip to the next statement or line and continue

This is the `stmt` rule of section 4. The pattern is always the same:

```pego
def stmt = s:(let / print / block) #recover(skip=(?^;{}\n)+ ";"?) -> $s
```

- `(?^;{}\n)+` skips the characters of the broken statement. Excluding `;`, the brace characters and the line end makes
  it stop at the first place where a new statement could start.
- `";"?` consumes the terminator if that is what the skip stopped at, so the next statement starts cleanly.
- It must consume at least one character, which it does as long as the statement starts with a character that is not
  `;`, `{` or `}`. A broken statement that starts with one of them is not recovered: for `{` and `}` this is
  deliberate, and a stray `;` is an error.

For a language where an indented block follows a header line, skip the broken line **and the more deeply indented
lines after it**; otherwise the body of a broken `if`/`def` would be parsed as statements at the wrong level.
`examples/python` does this with a skip that reads the indentation variable (see the
[context-sensitive guide](context-sensitive.md#indentation-blocks)):

```pego
def simple_stmt: StmtNode = s:(terminated_simple_stmt / compound_probe) #recover(skip=skip_line) -> $s
def skip_line = (?^\n)+ deeper_line*
def deeper_line = newline blank_line* s:indentation [len($s) > indent] (?^\n)+
```

```bash
pego parse -g examples/python/python.pego -f sexpr -i $'def f(x):\n    y = = 1\n        z = 2\n    return y\nprint(1)\n'
```

```text
(Module Body=[(FunctionDef Args=(Arguments Args=[(Arg Annotation=nil Arg=Identifier"x"@identifier Default=nil)] KwArg=nil KwOnlyArgs=[] PosOnlyArgs=[] VarArg=nil) Body=[Error"y = = 1\n        z = 2"{message=`2:9: expected an expression`} (Return Value=Name"y"@name)] DecoratorList=[] Name=Identifier"f"@identifier Returns=nil) (Expr Value=(Call Args=[Constant"1"@number] Func=Name"print"@name Keywords=[]))])
pego: 2:9: expected an expression
```

The broken line and its more indented continuation became one `Error` node; `return y` and `print(1)` were parsed
normally.

### Recover inside a list

Recovery does not have to be at the statement level. In a bracketed, comma-separated list, recover element by element
and let `skip` stop in front of the separator and the closing bracket, so that neither is consumed:

```pego
// list.pego
package nums

type Num terminal
type Numbers struct { Items []Num }

def main: Numbers = ws l:list ws $$ -> $l

def list: Numbers = "[" ws xs:items? ws "]" #error(message="expected ',' or ']'")
    -> new Numbers{Items: concat($xs)}
def items = first:item rest:(-ws "," -ws i:item)* -> concat(list($first), map($rest, (r) => $r.i))

// A bad element is skipped up to (not including) the next "," or "]".
def item: Num = n:number #error(message="expected a number") #recover(skip=(?^,\]\n)+) -> $n

def number: Num = (?0-9)+
def ws = (&(? \t\n) .)*
```

```bash
pego parse -g list.pego -f sexpr -i '[1, x, 3, y z]'
```

```text
(Numbers Items=[Num"1"@number Error"x"{message=`1:5: expected a number`} Num"3"@number Error"y z"{message=`1:11: expected a number`}])
pego: 1:5: expected a number
1:11: expected a number
```

The stop set (`,`, `]` and the line end) keeps `skip` from consuming the separator and the closing bracket: in
`[1, x, 3]` it eats only the `x`, and the `,` and `]` stay for the list rule. Two limits of recovery are visible:

```bash
pego parse -g list.pego -i '[1, , 3]'
```

```text
pego: 1:5: expected a number
```

```bash
pego parse -g list.pego -i '[1, 2'
```

```text
pego: 1:6: expected ',' or ']'
```

- A *missing* element (`[1, , 3]`) cannot be recovered: `skip` cannot match at the `,`, there is nothing to skip, and
  an empty skip is not a recovery. The ordinary error, with its label, is reported.
- A missing closer at the end of the input is reported by the label on `"]"`. The elements before it are no longer
  reported separately, because the whole list failed (see "If the parse fails in the end" above).

## 6. Troubleshooting

| Symptom | Likely cause | What to do |
|:--|:--|:--|
| The expected list contains `(? \t\r\n)` | The whitespace rule recorded a failure at the farthest position | Write `ws` as `(&(? \t\r\n) .)*`, or label the place |
| `1:1: syntax error` with no expected items | The failure came only from a lookahead, a predicate or `_|_`, which record nothing | Add `#error` to the predicate or to a rule built on `_|_` |
| A label does not appear | The expression it is on failed at an earlier position than another alternative, or an outer `#error` replaced it | Attach labels to the smallest expression; check which alternative reached furthest |
| A label hides valid alternatives | The message replaces every item recorded at its position, including siblings | Label the whole choice, not one alternative |
| `#recover` has no effect | `skip` fails at the start of the expression or matches nothing; the expression is not where the failure occurs; or the parse failed in the end so all recovered errors were dropped | Test `skip` on the broken text alone; check that the error is not outside the `#recover`; check for an unclosed construct |
| Valid input fails after adding `#recover` | `skip` can match the closing token of the enclosing construct | Exclude the closers from `skip` |
| `err != nil`, yet a tree is wanted | The parse recovered; the error is a `pego.SyntaxErrors` | Check for `SyntaxErrors` with `errors.As` before returning |
| The reported error is on the line after the mistake | Whitespace is skipped before the next token, over a newline | See [Where the error is reported relative to whitespace](#where-the-error-is-reported-relative-to-whitespace) |
