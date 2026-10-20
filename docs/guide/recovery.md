# Recovering From Errors

This guide shows how to keep parsing after a syntax error, so that one run reports every problem in a file and returns a
tree that is complete except at the broken places. It builds on [Error Messages](error-messages.md) and is a companion
to the normative text on [pragmas](../../spec/pragmas.md) (`#recover` and `#missing`) and
[syntax errors](../../spec/parsing.md#syntax-errors).

Contents:

- [Recovering at the Statement Level](#recovering-at-the-statement-level): `#recover`, error values, attribute order
- [What Happens to the Errors](#what-happens-to-the-errors): where they go, when they are dropped
- [Using the Errors from Go](#using-the-errors-from-go): a program that handles every outcome
- [Do Not Let the Skip Eat the Closer](#do-not-let-the-skip-eat-the-closer)
- [Inserting a Missing Literal](#inserting-a-missing-literal): `#missing`
- [Recipes](#recipes): a missing closing bracket, skipping to the next statement or line, recovering inside a list
- [Troubleshooting](#troubleshooting)

The examples use the `pego` command; build it once with `go build -o pego ./cmd/pego` (or run `go run ./cmd/pego`
instead of `pego`). Each command is followed by its output with standard output and standard error shown together, as
a terminal shows them. `pego` prints errors on standard error with the prefix `pego: ` and exits with status 1 when
the input has a syntax error, even if it recovered from all of them (the tree is then printed first, on standard
output).

A grammar block whose first line is `// name.pego` is a complete file: save it under that name. A block whose first
line is `// name.pego (changed rules)` replaces the rules of the same names in that file, and the examples after it use
the edited file. Every command and its output below were run as shown, in this order.

The grammar is the finished `toy.pego` of [Writing Readable Messages](error-messages.md#writing-readable-messages): a
small statement language with `let x = 1 + 2;`, `print x;` and `{ ... }` blocks whose errors carry `@error` labels. Save
it under that name:

```pego
// toy.pego
package toy

type Let = { Name node[string]; Value Expr }
type Print = { Value Expr }
type Block = { Body []Stmt }
type Sum = { Left Expr; Right Expr }
type Expr ~ node[string] | Sum
type Stmt ~ Let | Print | Block | error
type Program = { Body []Stmt }

rule main Program = body:stmts ws $$ => Program{Body: $body}
rule stmts = ss:(-ws s:stmt)* => map($ss, (x) => x.$s)

rule stmt Stmt = let / print / block
rule let Let = (
    "let" ws n:name @error(message="expected a name") ws "=" @error(message="expected '='")
    ws v:expr ws ";" @error(message="expected ';'")
) => Let{Name: $n, Value: $v}
rule print Print = "print" ws v:expr ws ";" @error(message="expected ';'") => Print{Value: $v}
rule block Block = "{" body:stmts ws "}" @error(message="expected '}'") => Block{Body: $body}

rule expr Expr = first:term rest:(-ws "+" -ws t:term)* => foldl($first, $rest, (acc, i) => Sum{Left: acc, Right: i.$t})
rule term Expr = (number / name) @error(message="expected an expression") / group
rule group Expr = "(" ws e:expr ws ")" @error(message="expected ')'") => $e

rule name = [a-z]+ #atomic
rule number = [0-9]+ #atomic
rule ws = (&[ \t\r\n] .)*
```

## Recovering at the Statement Level

Without recovery, the first error ends the parse. A language server, a linter or a compiler that wants to report all
problems in a file needs the parser to continue. `#recover(skip=e)` turns a failure into a success:

1. The expression is tried. If it matches, nothing happens.
2. If it fails, the syntax error is **recorded** (what would have been the error of the whole parse).
3. The parser goes back to where the expression **started** and matches `e` there. The input matched by `e` is
   skipped.
4. The value of the expression becomes an `error` value that covers the skipped input and holds the recorded message, and parsing continues after it.

If `e` fails, or matches nothing, there is no recovery and the expression fails as usual (so a `#recover` in a
repetition cannot loop forever on empty skips).

Recover at the statement level: when a statement does not parse, skip to the end of its line or to the next
`;`, whichever comes first, but never past a brace.

```pego
// toy.pego (changed rules)
// If a statement does not parse, skip to the next ";" (but never past a brace) and carry on.
rule stmt Stmt = s:(let / print / block) #recover(skip=[^;{}\n]+ ";"?) => $s
```

```bash
pego parse -g toy.pego -f sexpr -i $'let a = 1;\nlet = 2;\nprint (a +);\nprint a\nlet b = a + 1;\n{ print 1 let c = ; print 2; }\nprint b;\n'
```

```text
Program{Body: [Let{Name: "a", Value: "1"} error("2:5: expected a name") error("3:11: expected an expression") error("5:1: expected ';'") Let{Name: "b", Value: Sum{Left: "a", Right: "1"}} Block{Body: [error("6:11: expected ';'") Print{Value: "2"}]} Print{Value: "b"}]}
pego: 2:5: expected a name
3:11: expected an expression
5:1: expected ';'
6:11: expected ';'
```

Four errors are reported from one run, and the tree is complete except at the broken statements, which are `error`
values in the `Body` list. `error` is a built-in type that only recovery produces: the type of `stmt` is
`Stmt | error`, so the union `Stmt` of this grammar lists `error`, which is why `Body []Stmt` can hold it. In JSON, the
default output format, an error value looks like this:

```bash
pego parse -g toy.pego -i $'let = 2;\nprint 1;\n'
```

```text
{
  "type": "Program",
  "fields": {
    "Body": [
      {
        "start": 0,
        "end": 8,
        "error": "1:5: expected a name"
      },
      {
        "type": "Print",
        "fields": {
          "Value": {
            "start": 15,
            "end": 16,
            "value": "1"
          }
        }
      }
    ]
  }
}
pego: 1:5: expected a name
```

`start`/`end` delimit the skipped text, and `error` is the recorded message, with its position, as a string.

Details worth knowing:

- **The skip starts where the expression started, not where it failed.** The error position (here `1:5`, inside
  `let = 2;`) is usually inside the skipped range. `skip` is matched against the whole broken statement from its first
  character, so it must be written as "the rest of this construct", not "the rest after the error".
- **Recovery turns an expression's failure into success, so it applies to every failure inside it, however deep.**
  `s:(let / print / block) #recover(...)` recovers from a failure in any of the three alternatives, including a
  failure far inside a nested expression. The nearest enclosing `#recover` wins: in the example, a broken statement
  inside a block is recovered by the `stmt` rule of the block's own body, not by the outer statement.
- **Order of attributes matters.** Attributes apply from the innermost outward in the order written. Write
  `@error(...) #recover(...)`: the label is applied first, so the recovered error carries the label. With the order
  reversed, `@error` wraps a `#recover` that almost never fails, so the label is never used:

```pego
// order.pego
rule good = stmt @error(message="bad statement") #recover(skip=[^;]* ";")* $$
rule bad = stmt #recover(skip=[^;]* ";") @error(message="bad statement")* $$
rule stmt = "a" ";"
```

```bash
pego parse -g order.pego -s good -f sexpr -i 'a;b;a;'
```

```text
([("a" ";") error("1:3: bad statement") ("a" ";")])
pego: 1:3: bad statement
```

```bash
pego parse -g order.pego -s bad -f sexpr -i 'a;b;a;'
```

```text
([("a" ";") error("1:3: syntax error: expected \"a\"") ("a" ";")])
pego: 1:3: syntax error: expected "a"
```

## What Happens to the Errors

**Where they go.** `Parser.Parse` returns the tree and `SyntaxErrors` (see [Getting the Errors Out](error-messages.md#getting-the-errors-out)); the
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
pego: 2:1: syntax error: expected "let", "print", "{", [^;{}\n], end of input
```

The stray `}` stops every recovery in this grammar, because `skip` refuses to eat braces. That is the right trade-off
for a block-structured language, but if you want to report stray closing braces too, the top-level rule has to handle
them.

## Using the Errors From Go

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

// errorNodes collects the error values of a tree.
func errorNodes(n *pego.Node, out *[]*pego.Node) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case pego.KindError:
		*out = append(*out, n)
	case pego.KindStruct:
		for _, v := range n.Fields() {
			if child, ok := v.(*pego.Node); ok {
				errorNodes(child, out)
			}
		}
	case pego.KindTuple, pego.KindList, pego.KindTupleValue, pego.KindListValue:
		for _, child := range n.Elems() {
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
				fmt.Printf("  error value [%d,%d): %s\n", n.Start(), n.End(), n.Text())
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
ok: Program{Body: [Print{Value: "1"}]}
--- "let = 2;\nprint (a +);\nprint 3;\n"
recovered from 2 error(s); tree: Program{Body: [error("1:5: expected a name") error("2:11: expected an expression") Print{Value: "3"}]}
  line 1, column 5 (offset 4): expected a name
  line 2, column 11 (offset 19): expected an expression
  error value [0,8): 1:5: expected a name
  error value [9,21): 2:11: expected an expression
--- "print 1;\n}\n"
failed: 2:1: syntax error: expected "let", "print", "{", [^;{}\n], end of input (node is nil: true)
  expected=["\"let\"" "\"print\"" "\"{\"" "[^;{}\\n]" "end of input"] messages=[]
```

## Do Not Let the Skip Eat the Closer

A recovery inside a repetition is attempted every time an element fails to parse, including the normal end of the
repetition. When the closing bracket of the enclosing construct can be matched by `skip`, recovery swallows it. This is
what the `toy.pego` statement rule does if `{}` is not excluded from the skip:

```pego
// toy_greedy.pego
package toy

type Let = { Name node[string]; Value Expr }
type Print = { Value Expr }
type Block = { Body []Stmt }
type Expr ~ node[string]
type Stmt ~ Let | Print | Block | error
type Program = { Body []Stmt }

rule main Program = body:stmts ws $$ => Program{Body: $body}
rule stmts = ss:(-ws s:stmt)* => map($ss, (x) => x.$s)

rule stmt Stmt = s:(let / print / block) #recover(skip=[^;\n]+ ";"?) => $s
rule let Let = "let" ws n:name ws "=" ws v:expr ws ";" => Let{Name: $n, Value: $v}
rule print Print = "print" ws v:expr ws ";" => Print{Value: $v}
rule block Block = "{" body:stmts ws "}" @error(message="expected '}'") => Block{Body: $body}

rule expr Expr = number / name
rule name = [a-z]+ #atomic
rule number = [0-9]+ #atomic
rule ws = (&[ \t\r\n] .)*
```

```bash
pego parse -g toy_greedy.pego -f sexpr -i $'{ print 1; }\nprint 2;\n'
```

```text
Program{Body: [error("3:1: expected '}'") error("1:12: syntax error: expected \"let\", \"print\", \"{\"") Print{Value: "2"}]}
pego: 3:1: expected '}'
1:12: syntax error: expected "let", "print", "{"
```

The valid input `{ print 1; }` is reported as erroneous. At the `}`, the `stmt` rule inside the block fails (a `}` is
not a statement), and `skip` consumes the `}` as the "rest of a broken line". The block runs on to the end of the input
without finding its closing brace, and the outer statement recoveries then produce the two errors shown. The fix is the
one `toy.pego` uses: `skip` must not be able to start at, or run over, the token that ends the enclosing repetition. The
same applies to `)`, `]`, `end` and so on. In general, `skip` should describe "the rest of this element" and be unable
to match at the delimiter that closes the list the element is in.

## Inserting a Missing Literal

Use `#missing` on a nonempty literal when an editor needs a tree even though that
token is absent. In a Closure whole-input parse, `pego.WithMissingTokens(n)`
enables up to `n` insertion attempts for that call. Without the option, the
literal remains strict. For example, `rule main = "name" ";" #missing $$`
parses `name` with a zero-width `missing` value at EOF and returns a recovered
`SyntaxErrors` value. The value records the expected token `;` and the
diagnostic, and its `Text()` returns the diagnostic message. A literal with
`#missing` has the type `node[string] | missing`: when no semicolon was read,
the value is a `missing`, a kind of `error`, and not a string node.

The limit counts abandoned attempts too. Exhaustion returns
`*pego.MissingTokenLimitError` with no tree. VM backends, generated parsers,
Document, streams and compiled grammar serialization do not support this
initial feature. The budget includes abandoned attempts and left-recursion seed
replay, so a limit of 16 does not guarantee acceptance of a tree with 16 missing
nodes. The [pragma
specification](../../spec/pragmas.md#missing) defines the full contract.

## Recipes

### Report a Missing Closing Bracket

A missing `)`, `]` or `}` fails at the position where the closer was expected, which is where the parser could not
continue. Label the closer, not the group, so that errors inside the group keep their own messages. This is how
`toy.pego` does it:

```pego
rule group Expr = "(" ws e:expr ws ")" @error(message="expected ')'") => $e
```

(Writing `("(" ws e:expr ws ")") @error(...)` instead would replace the messages of everything inside the group with the
one message.) With the statement-level recovery of the `stmt` rule above, a missing `)` costs one statement and nothing else:

```bash
pego parse -g toy.pego -f sexpr -i $'let x = (1 + 2;\nprint x;\n'
```

```text
Program{Body: [error("1:15: expected ')'") Print{Value: "x"}]}
pego: 1:15: expected ')'
```

The same holds inside a block: the broken statement becomes an `error` value, and the statements after it, up to the
block's own `}`, are parsed normally.

```bash
pego parse -g toy.pego -f sexpr -i $'{ print (1; let y = 2;\n  print 3;\n}\n'
```

```text
Program{Body: [Block{Body: [error("1:11: expected ')'") Let{Name: "y", Value: "2"} Print{Value: "3"}]}]}
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
  in the end" in [What Happens to the Errors](#what-happens-to-the-errors)). `skip` of the statement rule refuses to start at `{`, and an unclosed block leaves nothing
  to skip anyway, so nothing can recover it.

### Skip to the Next Statement or Line and Continue

This is the `stmt` rule from the start of this guide. The pattern is always the same:

```pego
rule stmt = s:(let / print / block) #recover(skip=[^;{}\n]+ ";"?) => $s
```

- `[^;{}\n]+` skips the characters of the broken statement. Excluding `;`, the brace characters and the line end makes
  it stop at the first place where a new statement could start.
- `";"?` consumes the terminator if that is what the skip stopped at, so the next statement starts cleanly.
- It must consume at least one character, which it does as long as the statement starts with a character that is not
  `;`, `{` or `}`. A broken statement that starts with one of them is not recovered: for `{` and `}` this is
  deliberate, and a stray `;` is an error.

For a language where an indented block follows a header line, skip the broken line **and the more deeply indented
lines after it**; otherwise the body of a broken header would be parsed as statements at the wrong level. The `skip`
reads the indentation variable (see the [context-sensitive guide](context-sensitive.md#indentation-blocks)). This
grammar has assignments and `if` blocks:

```pego
// blocks.pego
package blocks

type Assign = { Name node[string]; Value node[string] }
type If = { Test node[string]; Body []Stmt }
type Stmt ~ Assign | If | error

var indent int

rule main []Stmt = {indent := 0} ss:stmts blank* $$ => $ss
rule stmts = v1:(blank* s:stmt)+ => map($v1, (x) => x.$s)

// The indentation is outside the recovery: a line that is not at this block's indentation ends the block.
rule stmt = indentation s:(if_stmt / assign) #recover(skip=skip_line) => $s
rule indentation = s:spaces {len($s) == indent}

rule if_stmt If = "if " t:name ":" eol b:body => If{Test: $t, Body: $b}
rule body = &(blank* s:spaces) {len($s) > indent} {indent := len($s)} ss:stmts => $ss
rule assign Assign = n:name " = " v:num eol => Assign{Name: $n, Value: $v}

// The rest of the broken line and the lines indented deeper than the block.
rule skip_line = [^\n]+ deeper_line*
rule deeper_line = "\n" blank* s:spaces {len($s) > indent} [^\n]+

rule spaces = " "* #atomic
rule name = [a-z]+ #atomic
rule num = [0-9]+ #atomic
rule eol = "\n" / $$
rule blank = [ \t]* "\n"
```

```bash
pego parse -g blocks.pego -f sexpr -i $'if x:\n    y = 1\n    z = = 2\n        w = 3\n    v = 4\nu = 5\n'
```

```text
[If{Body: [Assign{Name: "y", Value: "1"} error("3:9: syntax error: expected [0-9]") Assign{Name: "v", Value: "4"}], Test: "x"} Assign{Name: "u", Value: "5"}]
pego: 3:9: syntax error: expected [0-9]
```

The broken line and its more indented continuation became one `error` value; `v = 4` and `u = 5` were parsed normally,
each in its own block. A broken header takes its body with it:

```bash
pego parse -g blocks.pego -f sexpr -i $'if x\n    y = 1\nu = 5\n'
```

```text
[error("1:5: syntax error: expected \":\", [a-z]") Assign{Name: "u", Value: "5"}]
pego: 1:5: syntax error: expected ":", [a-z]
```

The indentation check is outside the `#recover`: a line that is not at the indentation of the block fails `stmt` before
the recovery starts, which ends the block instead of being skipped.

### Recover Inside a List

Recovery does not have to be at the statement level. In a bracketed, comma-separated list, recover element by element
and let `skip` stop in front of the separator and the closing bracket, so that neither is consumed:

```pego
// list.pego
package nums

type Item ~ node[string] | error
type Numbers = { Items []Item }

rule main Numbers = ws l:list ws $$ => $l

rule list Numbers = "[" ws xs:items? ws "]" @error(message="expected ',' or ']'") => Numbers{Items: concat($xs)}
rule items = first:item rest:(-ws "," -ws i:item)* => concat(list($first), map($rest, (r) => r.$i))

// A bad element is skipped up to (not including) the next "," or "]".
rule item Item = n:number @error(message="expected a number") #recover(skip=[^,\]\n]+) => $n

rule number = [0-9]+ #atomic
rule ws = (&[ \t\n] .)*
```

```bash
pego parse -g list.pego -f sexpr -i '[1, x, 3, y z]'
```

```text
Numbers{Items: ["1" error("1:5: expected a number") "3" error("1:11: expected a number")]}
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

## Troubleshooting

| Symptom | Likely cause | What to do |
|:--|:--|:--|
| `#recover` has no effect | `skip` fails at the start of the expression or matches nothing; the expression is not where the failure occurs; or the parse failed in the end so all recovered errors were dropped | Test `skip` on the broken text alone; check that the error is not outside the `#recover`; check for an unclosed construct |
| Valid input fails after adding `#recover` | `skip` can match the closing token of the enclosing construct | Exclude the closers from `skip` |
| `err != nil`, yet a tree is wanted | The parse recovered; the error is a `pego.SyntaxErrors` | Check for `SyntaxErrors` with `errors.As` before returning |

## Related Topics

- [Error Messages](error-messages.md): `@error` labels, cuts and the shape of `SyntaxError`.
- [Context-Sensitive Parsing](context-sensitive.md): a `skip` that reads variables, such as the indentation.
- [Running Parsers](running-parsers.md): the outcomes of `Parse`, including `SyntaxErrors`.
- [Debugging and Profiling](debugging.md): tracing a recovery that does not do what you expect.
