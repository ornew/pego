# Getting started with PEGO

This tutorial takes you from an empty directory to a parser that reads a small configuration language and a calculator
that respects operator precedence. Every grammar, command and Go program below is complete and was run to produce the
output shown, so you can type along.

You will need Go 1.27 or later. No other dependencies are required. To try a grammar before installing anything, use
the [playground](https://pego.ornew.net/playground/), which runs PEGO in your browser.

Contents:

1. [Install](#1-install)
2. [Your first grammar](#2-your-first-grammar)
3. [Building blocks](#3-building-blocks)
4. [A configuration language](#4-a-configuration-language)
5. [Typed trees](#5-typed-trees)
6. [Expressions and precedence](#6-expressions-and-precedence)
7. [Syntax errors](#7-syntax-errors)
8. [Next steps](#8-next-steps)

## 1. Install

PEGO has two parts: the Go library, and the `pego` command-line tool that runs, formats and compiles grammars. The
command-line tool is the quickest way to experiment, so install it first.

```bash
go install github.com/ornew/pego/cmd/pego@latest    # command-line tool
```

Running `pego` without arguments lists its commands (`parse`, `fmt`, `convert`, `gen`, `compile`). To use the library
from your own program, add it to your module:

```bash
go get github.com/ornew/pego                        # library
```

Create a directory for the tutorial files:

```bash
mkdir pego-tutorial && cd pego-tutorial
```

## 2. Your first grammar

A PEGO grammar is a set of **rules**. A rule has a name and a body that says what input it matches. Parsing starts at
the **start rule**, which is called `main` by default. Save this as `hello.pego`:

```pego
def main = "hello" " " "world"
```

`main` matches the string literal `hello`, then a space, then `world`. Run it with `pego parse`, giving the grammar with
`-g` and the input with `-i`:

```bash
pego parse -g hello.pego -i 'hello world'
```

```json
{
  "type": "Seq",
  "rule": "main",
  "start": 0,
  "end": 11,
  "children": [
    {
      "type": "Match",
      "start": 0,
      "end": 5,
      "text": "hello"
    },
    {
      "type": "Match",
      "start": 5,
      "end": 6,
      "text": " "
    },
    {
      "type": "Match",
      "start": 6,
      "end": 11,
      "text": "world"
    }
  ]
}
```

The result is a **tree of nodes**. Each node has a `type`, a range in the input (`start` inclusive, `end` exclusive,
counted in Unicode code points), and either `text` (for a terminal) or `children`. Here the sequence of three literals
became a `Seq` node, labeled `"rule": "main"` because the rule `main` created it, with three `Match` terminals under it.

The JSON form is complete but long. `-f sexpr` prints the same tree as a compact S-expression, which is what the rest
of this tutorial uses:

```bash
pego parse -g hello.pego -f sexpr -i 'hello world'
```

```text
(Seq "hello" " " "world")@main
```

How to read it:

| Syntax | Meaning |
|:--|:--|
| `"abc"` | a terminal (a `Match` node) holding the text `abc` |
| `Name"abc"` | a terminal of the user-defined terminal type `Name` |
| `(Seq a b)` | a `Seq` node (the value of a sequence) with children `a` and `b` |
| `[a b]` | a `List` node (the value of a repetition) |
| `(Pair Key=a Value=b)` | a node of a struct type with fields (and captures) `Key` and `Value` |
| `x@rule` | the node was created by the rule `rule` |

The start rule must match the **whole input**. If it matches only a prefix, that is a syntax error, and so is input
that does not match at all. Both are reported with a position and what was expected there:

```bash
pego parse -g hello.pego -f sexpr -i 'hello there'
```

```text
pego: 1:7: syntax error: expected "world"
```

The position is `line:column`, both 1-based. The command exits with status 1.

### From Go

The same grammar from a Go program. Create a module and `main.go`:

```bash
go mod init example.com/hello
go get github.com/ornew/pego
```

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/ornew/pego"
)

const src = `def main = "hello" " " "world"`

func main() {
	p, err := pego.CompileSource(src, "main")
	if err != nil {
		log.Fatal(err) // a mistake in the grammar
	}

	node, err := p.Parse("hello world")
	if err != nil {
		log.Fatal(err) // the input does not match
	}

	fmt.Println(node) // the S-expression form
	fmt.Println(node.Type(), node.Rule(), node.Start, node.End)
	for _, c := range node.Children {
		fmt.Printf("%s %q [%d,%d)\n", c.Type(), c.Text, c.Start, c.End)
	}

	data, _ := json.Marshal(node)
	fmt.Println(string(data))

	_, err = p.Parse("hello there")
	fmt.Println(err)
}
```

```bash
go run .
```

```text
(Seq "hello" " " "world")@main
Seq main 0 11
Match "hello" [0,5)
Match " " [5,6)
Match "world" [6,11)
{"type":"Seq","rule":"main","start":0,"end":11,"children":[{"type":"Match","start":0,"end":5,"text":"hello"},{"type":"Match","start":5,"end":6,"text":" "},{"type":"Match","start":6,"end":11,"text":"world"}]}
1:7: syntax error: expected "world"
```

- `pego.CompileSource(grammarSource, startRule)` parses and type-checks the grammar and returns a `*pego.Parser`. It
  fails with an error that has the line and column in the grammar source if the grammar is wrong. Compile once and reuse
  the parser; it can be used from several goroutines.
- `p.Parse(input)` returns a `*pego.Node`. Nodes expose `Type()`, `Rule()`, `Start`, `End`, `Text`, `Children`
  and `Fields`, and encode to JSON as shown.
- When the input does not match, `Parse` returns a `*pego.SyntaxError` (see [section 7](#7-syntax-errors)).

## 3. Building blocks

The grammar language has a few more pieces than literals. This section introduces them on a tiny language: a lowercase
word, an equals sign, and a number, as in `port=8080`.

### Rules, character classes and repetition

A **character class** `(?...)` matches one character out of a set, written as characters and ranges: `(?a-z)` is a
lowercase ASCII letter and `(?0-9)` is a digit. `+` after an expression repeats it one or more times (`*` is zero or
more, `?` is zero or one). A rule can call another rule by name. Save as `kv.pego`; each step below replaces its
contents with the next version:

```pego
def main = key "=" value
def key = (?a-z)+
def value = (?0-9)+
```

```bash
pego parse -g kv.pego -f sexpr -i 'port=8080'
```

```text
(Seq ["p" "o" "r" "t"]@key "=" ["8" "0" "8" "0"]@value)@main
```

It works, but the tree is noisy: a repetition produces a `List` with one child per iteration, so `key` is a list of
single-letter terminals.

### Atomic: `@`

`@expr` matches `expr` and produces one terminal holding all of the matched text, without building a node for each
part. `@(?a-z)+` means `@((?a-z)+)`, because repetition binds tighter than `@`:

```pego
def main = key "=" value
def key = @(?a-z)+
def value = @(?0-9)+
```

```text
(Seq "port"@key "=" "8080"@value)@main
```

### Discard: `-`

The `=` is needed to parse the input but is of no interest in the tree. `-expr` matches `expr` and throws the result
away:

```pego
def main = key -"=" value
def key = @(?a-z)+
def value = @(?0-9)+
```

```text
(Seq "port"@key "8080"@value)@main
```

### Captures: `name:`

A **capture** `name:expr` gives the value of `expr` a name. Named values are attached to the node as fields, and, as the
next step shows, actions refer to them as `$name`:

```pego
def main = k:key "=" v:value
def key = @(?a-z)+
def value = @(?0-9)+
```

```text
(Seq "port"@key "=" "8080"@value k="port"@key v="8080"@value)@main
```

The label and the colon must be written together (`k:key`, not `k :key`).

### Actions and struct types: `->`

An **action** builds the value of a rule yourself. It is written after `->`, and can create a node of a struct type
declared with `type`. This is the grammar from the README (`kv.pego`, final version):

```pego
type Pair struct { Key Match, Value Match }

def main = k:@(?a-z)+ "=" v:@(?0-9)+ -> new Pair{Key: $k, Value: $v}
```

```bash
pego parse -g kv.pego -f sexpr -i 'abc=12'
```

```text
(Pair Key="abc" Value="12")
```

`new Pair{...}` creates a `Pair` node, and `$k` and `$v` refer to the captures. The literal `=` has no name, so it is
simply left out of the result. The tree now contains exactly what the language is about, and nothing else.

Two things worth knowing at this point:

- An action applies to the **whole** right-hand side of a rule. A rule such as `def r = a / b -> ...` runs the action
  whatever alternative matched; to build different values for different alternatives, give each alternative its own
  rule.
- Types are checked when the grammar is compiled, so a typo in a field name is reported before any input is parsed.
  You will see examples in [section 5](#5-typed-trees).

## 4. A configuration language

Now a more realistic language: a file of `key = value` lines with comments.

```text
# server settings
host = "localhost"
port = 8080
debug = true
```

Create `example.conf` with that content:

```bash
cat > example.conf <<'EOF'
# server settings
host = "localhost"
port = 8080
debug = true
EOF
```

Save the following grammar as `config-cst.pego`. It produces plain syntax-tree nodes; the next section adds types.

```pego
def main = -ws entry* $$

def entry = k:key -ws -"=" -ws v:value -ws

def value = number / string / bool
def key = @(?a-z_)+
def number = @(?0-9)+
def string = @("\"" (?^")* "\"")
def bool = @("true" / "false")

def ws = (blank / comment)*
def blank = (? \t\r\n)+
def comment = "#" (?^\n)*
```

```bash
pego parse -g config-cst.pego -f sexpr < example.conf
```

```text
(Seq [(Seq "host"@key "\"localhost\""@string k="host"@key v="\"localhost\""@string)@entry (Seq "port"@key "8080"@number k="port"@key v="8080"@number)@entry (Seq "debug"@key "true"@bool k="debug"@key v="true"@bool)@entry])@main
```

Without `-i`, `pego parse` reads the input from standard input. The new pieces:

- **Whitespace is explicit.** PEGO does not skip whitespace for you. The rule `ws` matches any run of blanks, newlines
  and comments, and the grammar says where it is allowed. Each `entry` consumes the whitespace after itself, and
  `main` starts with one `-ws` for the whitespace before the first entry. Whitespace is discarded with `-`, so it never
  appears in the tree.
- **`(?^...)` is a negated class.** `(?^")` matches any character except `"`, and `(?^\n)` any character except a line
  feed, so `comment` reads to the end of the line. Escapes such as `\n` and `\"` work in literals and classes.
- **`/` is ordered choice.** `value` tries `number`, then `string`, then `bool`, and commits to the first that
  matches. Order matters: with `("a" / "ab") $$`, the input `ab` fails because `"a"` matches first and is never
  reconsidered.

  ```pego
  def main = ("a" / "ab") $$
  ```

  ```bash
  pego parse -g ab.pego -f sexpr -i 'ab'
  ```

  ```text
  pego: 1:2: syntax error: expected end of input
  ```

  Write `("ab" / "a")` (the longer alternative first) to accept both.
- **`$$` is the end of input.** It matches without consuming anything and has no value. The start rule must consume the
  whole input anyway, so `$$` is optional here, but writing it makes the intent clear. (`^^` is the beginning of the
  input.)
- **`entry*` is a repetition.** The whole file is a `Seq` holding one `List` of entries. An empty file is valid and
  gives `(Seq [])@main`.

## 5. Typed trees

The tree above is a faithful picture of the text, but a program that uses the configuration wants a tree shaped like
the data: a `Config` with a list of `Entry` values, each with a key and a typed value. In PEGO you declare these types
in the grammar and construct them with actions. Save as `config.pego`:

```pego
package config

type Key terminal
type Number terminal
type String terminal
type Bool terminal
type Value = Number | String | Bool

type Entry struct {
    Key   Key
    Value Value
}

type Config struct {
    Entries []Entry
}

def main: Config = -ws es:entry* $$ -> new Config{Entries: $es}

def entry: Entry = k:key -ws -"=" -ws v:value -ws -> new Entry{Key: $k, Value: $v}

def value: Value = number / string / bool
def key: Key = (?a-z_)+
def number: Number = (?0-9)+
def string: String = "\"" (?^")* "\""
def bool: Bool = "true" / "false"

def ws = (blank / comment)*
def blank = (? \t\r\n)+
def comment = "#" (?^\n)*
```

```bash
pego parse -g config.pego -f sexpr < example.conf
```

```text
(Config Entries=[(Entry Key=Key"host"@key Value=String"\"localhost\""@string) (Entry Key=Key"port"@key Value=Number"8080"@number) (Entry Key=Key"debug"@key Value=Bool"true"@bool)])
```

What is new:

- `package config` is optional; it names the grammar's package and is used by the code generator
  ([section 8](#8-next-steps)). It must be the first item in the file.
- `type Key terminal` declares a **terminal type**: a node type for leaves of the tree. A rule declared with a terminal
  type (`def key: Key = (?a-z_)+`) produces a single terminal of that type holding the matched text, so there is no
  need for `@`. The text of a `String` includes its quotes, since it is the text the rule matched.
- `type Value = Number | String | Bool` is a **union type**: a value of any one of the members, much like a Go
  interface. Because `value` is declared as `Value`, the compiler checks that each alternative is one of the members.
- `type Entry struct { ... }` declares a **struct type**; fields are separated by newlines or commas. Field types can be
  lists (`[]Entry`), optional values (`*T`), or unions.
- `def name: Type = ...` states the type of a rule. You can omit it and PEGO infers the type from the body, but
  declaring the types of the rules that build your tree documents them and keeps mistakes close to where they happen.
- `es:entry*` captures the whole list of entries, and `new Config{Entries: $es}` stores it in the `Config` node.

The compiler rejects ill-typed grammars with a position in the grammar file. In a copy of `config.pego`, swapping the
arguments in the `entry` action:

```bash
pego parse -g bad-type.pego -f sexpr -i 'a=1'   # ... new Entry{Key: $v, Value: $k}
```

```text
pego: bad-type.pego:20:64: cannot use Number | String | Bool as Key in field Key of Entry
bad-type.pego:20:73: cannot use Key as Number | String | Bool in field Value of Entry
```

and misspelling a field name:

```bash
pego parse -g bad-field.pego -f sexpr -i 'a=1'   # ... new Entry{Key: $k, Valu: $v}
```

```text
pego: bad-field.pego:20:73: Entry has no field Valu
```

### Using the tree from Go

The typed tree is what your program consumes. Put `config.pego` and an `example.conf` next to this `main.go`, which
embeds the grammar and prints the entries:

```go
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/ornew/pego"
)

//go:embed config.pego
var grammarSource string

func main() {
	p, err := pego.CompileSource(grammarSource, "main")
	if err != nil {
		log.Fatal(err) // a mistake in the grammar
	}

	input, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := p.Parse(string(input))
	if err != nil {
		var se *pego.SyntaxError
		if errors.As(err, &se) {
			fmt.Printf("line %d, column %d: %s\n", se.Line, se.Col, se.Message())
			fmt.Println("expected:", se.Expected)
		}
		log.Fatal(err)
	}

	entries := cfg.Field("Entries").(*pego.Node)
	for _, e := range entries.Children {
		key := e.Field("Key").(*pego.Node)
		value := e.Field("Value").(*pego.Node)
		fmt.Printf("%-6s %-7s %s\n", key.Text, value.Type(), value.Text)
	}
}
```

```bash
go run . example.conf
```

```text
host   String  "localhost"
port   Number  8080
debug  Bool    true
```

`node.Field(name)` returns the value of a struct field or capture, as an `any`: a `*pego.Node` for node values, or an
`int`, `string` or `bool`. A list is a node of type `List` whose `Children` are the elements. `value.Type()` tells you
which member of the `Value` union you got, so a `switch` on it is how you walk a union.

## 6. Expressions and precedence

Arithmetic is where PEG grammars usually get awkward: operator precedence and associativity normally require one rule
per level. PEGO offers two tools, **Pratt expressions** and **left recursion**. Start with a Pratt expression. Save as
`calc.pego`:

```pego
type Number terminal
type Binary struct { Left Expr, Op Match, Right Expr }
type Expr = Number | Binary

def main: Expr = e:expr $$ -> $e

def expr: Expr = pratt {
    operand number
    level { infix left "+" / "-" -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
}

def number: Number = (?0-9)+
```

A `pratt { ... }` body declares an expression by listing its parts:

- `operand` is something that can stand on its own, here a number.
- `level { ... }` declares operators that share a precedence. `infix left "+" / "-"` is a binary operator whose
  alternatives are `+` and `-`, grouping to the left. The action receives `$lhs` and `$rhs`, the operands, and `$op`,
  the operator text.

```bash
pego parse -g calc.pego -f sexpr -i '1+2-3'
```

```text
(Binary Left=(Binary Left=Number"1"@number Op="+" Right=Number"2"@number) Op="-" Right=Number"3"@number)
```

`1+2-3` is `(1+2)-3`, as left associativity demands. But whitespace is not handled yet:

```bash
pego parse -g calc.pego -f sexpr -i '1 + 2'
```

```text
pego: 1:2: syntax error: expected "+", "-", (?0-9), end of input
```

Add a `skip` item, which PEGO inserts before every operand and operator, and fill out the language: more levels,
parentheses, a prefix operator and a right-associative one. **Levels declared earlier bind more loosely**, so the order
of the `level` blocks is the precedence table, from loosest to tightest:

```pego
type Number terminal
type Binary struct { Left Expr, Op Match, Right Expr }
type Unary struct { Op Match, X Expr }
type Expr = Number | Binary | Unary

def main: Expr = e:expr ws $$ -> $e

def expr: Expr = pratt {
    skip    ws
    operand number
    operand "(" e:expr ws ")" -> $e
    level { infix left  "+" / "-"  -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix left  "*" / "/"  -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { prefix      "-"        -> new Unary{Op: $op, X: $rhs} }
    level { infix right "^"        -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
}

def number: Number = (?0-9)+ ("." (?0-9)+)?
def ws = (? \t\r\n)*
```

```bash
pego parse -g calc.pego -f sexpr -i '1+2*3'
```

```text
(Binary Left=Number"1"@number Op="+" Right=(Binary Left=Number"2"@number Op="*" Right=Number"3"@number))
```

Multiplication binds tighter than addition. Some more inputs:

| Input | Result |
|:--|:--|
| `1 - 2 - 3` | `(Binary Left=(Binary Left=Number"1"@number Op="-" Right=Number"2"@number) Op="-" Right=Number"3"@number)` |
| `2^3^2` | `(Binary Left=Number"2"@number Op="^" Right=(Binary Left=Number"3"@number Op="^" Right=Number"2"@number))` |
| `-2^2` | `(Unary Op="-" X=(Binary Left=Number"2"@number Op="^" Right=Number"2"@number))` |
| `(1+2)*3` | `(Binary Left=(Binary Left=Number"1"@number Op="+" Right=Number"2"@number) Op="*" Right=Number"3"@number)` |
| `1.5*2` | `(Binary Left=Number"1.5"@number Op="*" Right=Number"2"@number)` |

`1 - 2 - 3` groups to the left, `2^3^2` is `2^(3^2)` because `^` is `infix right`, and `-2^2` is `-(2^2)` because `^`
is declared after (so binds tighter than) the prefix `-`. A parenthesized operand returns `$e`, the inner expression,
so no node is created for the parentheses.

The `skip` item is a convenience of Pratt expressions only. It is not inserted *inside* an operator or operand, which is
why the parenthesized operand writes `ws` before `")"` itself, and why `main` has a trailing `ws`.

### The same language with left recursion

A rule that starts by calling itself is **left recursive**, and PEGO accepts it. Precedence then comes from having one
rule per level, each defined in terms of the next tighter one:

```pego
type Number terminal
type Binary struct { Left Expr, Op Match, Right Expr }
type Expr = Number | Binary

def main: Expr = ws e:expr ws $$ -> $e

def expr: Expr = sum / product
def sum: Expr = l:expr ws op:@("+" / "-") ws r:product -> new Binary{Left: $l, Op: $op, Right: $r}

def product: Expr = mul / atom
def mul: Expr = l:product ws op:@("*" / "/") ws r:atom -> new Binary{Left: $l, Op: $op, Right: $r}

def atom: Expr = number / group
def group: Expr = "(" ws e:expr ws ")" -> $e
def number: Number = (?0-9)+
def ws = (? \t\r\n)*
```

```bash
pego parse -g calc-lr.pego -f sexpr -i '1 + 2 * 3 - 4'
```

```text
(Binary Left=(Binary Left=Number"1"@number Op="+" Right=(Binary Left=Number"2"@number Op="*" Right=Number"3"@number)) Op="-" Right=Number"4"@number)
```

This produces the same trees as the Pratt version, with one rule per level instead of one line per level. Pratt
expressions scale better as the language grows (the [Go](../../examples/golang/) and
[Python](../../examples/python/) examples use them for their full operator tables), and they support features such as
level-restricted calls and mixed prefix, infix and postfix operators. The guide to
[expressions](../guide/expressions.md) compares the two approaches and covers every option;
[examples/calculator](../../examples/calculator/) contains both versions of a complete calculator with an evaluator.

## 7. Syntax errors

When the input does not match, PEGO reports the **farthest position** it reached and **what it expected there**. You have
already seen the format: `line:column: syntax error: expected a, b, c`, with the expected items sorted. Errors in
`config.pego`:

```bash
printf 'host = "localhost"\nport == 8080\n' | pego parse -g config.pego -f sexpr
```

```text
pego: 2:7: syntax error: expected "#", "\"", "false", "true", (? \t\r\n), (?0-9)
```

Line 2, column 7 is the second `=`: after `port =` the grammar wants a value (or whitespace, or a comment). An
unterminated string is reported at the end of the input:

```bash
pego parse -g config.pego -f sexpr -i 'name = "oops'
```

```text
pego: 1:13: syntax error: expected "\"", (?^")
```

Two things to remember about these messages:

- The position is the farthest point *any* alternative reached, which is usually where the real mistake is, but not
  always: a failed literal is reported at the place where the literal began. For the second line `debug = tru`, the
  error is at 2:9, the `t` of `tru`, not at the end of the line.
- The list is mechanical (it is built from the literals and classes that failed there). To give users something
  friendlier, attach `#error(message="...")` to an expression. Replacing `value` in `config.pego` with

  ```pego
  def value: Value = (number / string / bool) #error(message="expected a number, a string or true/false")
  ```

  turns the same errors into

  ```text
  pego: 2:7: expected a number, a string or true/false
  ```

  (and `pego: 2:9: expected a number, a string or true/false` for `debug = tru`).

A grammar can also **recover** from errors, skipping malformed input and continuing, so that an editor or a compiler can
report every error in one pass and still get a tree (`#recover`). Recovery, error messages and the Go types for
reporting them are covered in the [errors and recovery guide](../guide/errors-and-recovery.md).

Mistakes in the *grammar* are reported before any input is parsed, also with a position. For a `bad.pego` containing
`def main "a"`:

```text
pego: bad.pego:1:10: expected '=', found "a"
```

and for a `bad.pego` containing `def main = item`:

```text
pego: bad.pego:1:12: undefined rule item
```

Asking for a start rule that does not exist with `pego parse -g config.pego -s nosuch -i a` fails the same way:

```text
pego: config.pego: start rule nosuch is not defined
```

## 8. Next steps

### Format your grammars: `pego fmt`

`pego fmt` rewrites a grammar into the canonical layout and keeps comments. `-l` lists the files that would change, `-w`
rewrites them in place. For a grammar written in a hurry:

```pego
type Pair struct{Key Match,Value Match}
// a key=value pair
def main=k:@(?a-z)+  "="   v:@(?0-9)+   ->new Pair{Key:$k,Value:$v}
```

```bash
pego fmt -l messy.pego     # prints the file name if its formatting differs
pego fmt messy.pego        # prints the formatted source
```

```pego
type Pair struct { Key Match, Value Match }
// a key=value pair
def main = k:@(?a-z)+ "=" v:@(?0-9)+ -> new Pair{Key: $k, Value: $v}
```

### Generate a standalone Go parser: `pego gen`

`pego gen` turns a grammar into Go source, so your program does not need the PEGO library or the grammar at run time.
The generated file uses only the standard library. Generate it into a `config` package:

```bash
mkdir config
pego gen -g config.pego -pkg config -o config/parser.go
```

and call it:

```go
package main

import (
	"fmt"
	"log"

	"example.com/hello/config" // your module path
)

func main() {
	node, err := config.Parse("port = 8080\n")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(node)
}
```

```text
(Config Entries=[(Entry Key=Key"port"@key Value=Number"8080"@number)])
```

The generated `Node` type mirrors `pego.Node`, and the generated parser gives the same trees and errors as the
library. `config.ParseRule(name, input)` starts at another rule.

### Backends and compiled grammars

A grammar can run on several backends with identical results: the default closure-compiled engine, a bytecode VM, and
a bytecode VM that uses an explicit stack instead of the Go stack for deeply nested input. Choose one on the command line
or in Go:

```bash
pego parse -g config.pego -f sexpr -backend bytecode < example.conf
pego parse -g config.pego -check -backend bytecode-iterative < example.conf   # validate only, prints ok
```

```go
n, err := p.Parse("abc=12", pego.WithBackend(pego.Bytecode)) // also pego.WithUnit(pego.Bytes), pego.RecognizeOnly()
```

`pego compile -g config.pego -o config.pegoc` saves a compiled grammar that every command accepts in place of the
`.pego` file (`pego parse -g config.pegoc ...`), and `pego.LoadParser` loads it without recompiling.
`pego convert -to json config.pego` converts a grammar to JSON, which is handy for tools that generate grammars.
The [runtime guide](../guide/runtime.md) describes the backends, options and compiled grammars in detail.

### Where to go from here

| To learn about | Read |
|:--|:--|
| Choosing a backend, runtime options, compiled grammars, code generation | [Runtime guide](../guide/runtime.md) |
| Parsing unbounded input as a stream, and reparsing edited documents | [Streaming and incremental parsing](../guide/streaming-and-incremental.md) |
| Shaping trees, struct and union types, `foldl`, `map`, `concat` and the other action functions | [Trees and actions](../guide/trees-and-actions.md) |
| Operator tables, precedence, associativity, level-restricted calls, left recursion | [Expressions](../guide/expressions.md) |
| Error messages, `#error`, `#recover` and the Go error types | [Errors and recovery](../guide/errors-and-recovery.md) |
| Indentation-based languages, matching tags, predicates and variables | [Context-sensitive parsing](../guide/context-sensitive.md) |
| The precise definition of every construct | [Language specification](../../spec/README.md) |
| Complete grammars with tests: CSV, XML, a calculator, an outline format, a small programming language, Go, Python | [examples/](../../examples/README.md) |
| Ready-made parsers for common languages (CSV, Go, JSON, Python, XML, YAML), as Go modules | [parsers/](../../parsers/README.md) |

A good way to continue is to extend the configuration language of section 5: add `[section]` headers, lists of values,
and `#error` messages, then read [parsers/json](../../parsers/json/json.pego) and [examples/csv](../../examples/csv/) to see how
larger grammars are organized.
