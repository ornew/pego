# Trees and Actions

A PEGO grammar does two jobs: it decides which input is valid, and it decides what **tree** you get back. This guide is
about the second job. It starts with the tree that every grammar produces without any extra work, shows how to shape it
with captures, atomic (`@`) and discard (`-`), and then shows how to replace it with a typed AST built by
[actions](../../spec/actions.md). It ends with recipes and the authoring guidelines that keep parsing fast.

The guide complements the normative chapters [Parsing Expressions](../../spec/parser-expressions.md),
[Type System](../../spec/types.md) and [Actions](../../spec/actions.md); read them for the exact rules. Here the focus
is on how and why to use each feature. For operator expressions (`foldl` chains versus Pratt expressions) see the
companion guide [Parsing expressions with operators](expressions.md).

Every grammar and output below was produced by running it. To follow along, put each grammar in the file named on its
first line and run the command shown, using either the installed tool or `go run ./cmd/pego` from a checkout:

```bash
go install github.com/ornew/pego/cmd/pego@latest
```

`pego parse -g <grammar> -s <rule> -i '<input>' -f sexpr` parses the input with the start rule `<rule>` (default
`main`), which must match the whole input, and prints the tree.

## 1. Reading a tree

Start with the smallest grammar that has some structure:

```pego
// assign.pego
def main = ^^ assign $$
def assign = name ws "=" ws value ";"
def name = (?a-z)+
def value = number / name
def number = (?0-9)+
def ws = " "*
```

```bash
pego parse -g assign.pego -f sexpr -i 'ab = 12;'
```

```text
(Seq (Seq ["a" "b"]@name [" "]@ws "=" [" "]@ws ["1" "2"]@number ";")@assign)@main
```

The `sexpr` format is a compact notation for nodes:

| Notation | Meaning |
|:--|:--|
| `"ab"` | A `Match` node: a terminal holding the matched text |
| `Number"12"` | A terminal of the user-defined terminal type `Number` |
| `(Seq a b)` | A node with children: a `Seq` here, or any struct type, as in `(Pair Key=... Value=...)` |
| `[a b]` | A `List` node (the value of a repetition) |
| `x@name` | The node `x` was produced by the rule `name` (see below) |
| `k=...` | A field (a capture or a struct field), printed after the children and sorted by name |
| `nil` | An absent value, such as an optional element that did not match |
| `` `text` ``, `5`, `true` | A string, integer or boolean struct field (see [Actions](#5-actions)) |

Reading the output above from the inside out: `name` is the repetition `(?a-z)+`, so it is a `List` of one `Match` per
letter (shown as `["a" "b"]`), labeled `@name` because the rule `name` created it. `ws` is also a list (of spaces), and
`value` is `number`, so the node is the list of digits labeled `@number`. `assign` is a sequence, so a `Seq` labeled
`@assign`; and `main` wraps that in another `Seq`, because `^^` and `$$` have no value and the sequence therefore has a
single child.

The default JSON format carries the same information and adds positions. Here is a rule with an optional element:

```pego
// optional.pego
def main = name ("," name)?
def name = @(?a-z)+
```

```bash
pego parse -g optional.pego -i 'a'
```

```text
{
  "type": "Seq",
  "rule": "main",
  "start": 0,
  "end": 1,
  "children": [
    {
      "type": "Match",
      "rule": "name",
      "start": 0,
      "end": 1,
      "text": "a"
    },
    null
  ]
}
```

| JSON key | Meaning |
|:--|:--|
| `type` | `Match`, `Seq`, `List`, `Operator`, `Error`, or the name of a type you declared |
| `rule` | The rule that produced the node, when it is a node of the default tree (omitted otherwise) |
| `start`, `end` | The range of the node in the input; `end` is exclusive (see [Positions](#7-positions)) |
| `text` | The text of a terminal |
| `children` | The children of `Seq`, `List` and `Operator` nodes; an optional element that did not match is `null` |
| `fields` | Struct fields and captures, with keys sorted; a value is a node, a number, a string, a boolean or `null` |

## 2. The default tree

If a rule has no action, the tree is determined by the shape of the parsing expression alone. This is the **concrete
syntax tree** (CST). The rules are short enough to memorize:

| Expression | Value |
|:--|:--|
| `"x"`, `(?a-z)`, `.`, `@a`, `_` | A `Match` terminal |
| `a b` | A `Seq`; its children are the values of the elements that **have** a value |
| `a / b` | The value of the alternative that matched; the choice adds no node |
| `a*`, `a+`, `a{n,m}` | A `List`, one child per iteration |
| `a?` | The value of `a`, or `nil`; it is not a list (`a{0,1}` is) |
| `label:a` | The value of `a` (and a field, see below) |
| Rule call | The value of the called rule |
| `-a`, `&a`, `!a`, `--`, anchors, predicates | No value: never a child of a `Seq` |

Three consequences are worth knowing before you write any action.

**One value per rule, and rule names.** A rule produces exactly one value. A node that a rule creates *in its own body*
is labeled with the rule's name; a node that comes from another rule keeps that rule's label.

```pego
// cst.pego
def seq = "y" "z"
def alt = "x" / "y" "z"
def one = "x"
def same = one
def rep = "x"*
def opt = "x"?
def cap = k:"x" "y"
```

```bash
pego parse -g cst.pego -s seq -f sexpr -i 'yz'
pego parse -g cst.pego -s alt -f sexpr -i 'x'
pego parse -g cst.pego -s alt -f sexpr -i 'yz'
pego parse -g cst.pego -s same -f sexpr -i 'x'
pego parse -g cst.pego -s rep -f sexpr -i 'xxx'
pego parse -g cst.pego -s opt -f sexpr -i 'x'
pego parse -g cst.pego -s cap -f sexpr -i 'xy'
```

```text
(Seq "y" "z")@seq
"x"@alt
(Seq "y" "z")@alt
"x"@one
["x" "x" "x"]@rep
"x"@opt
(Seq "x" "y" k="x")@cap
```

`same` yields the node labeled `@one`, not `@same`. The choice `alt` yields a `Match` or a `Seq` depending on the
alternative, which is why it is called *transparent* (the reasoning is in
[design record 002](../design/002-transparent-choice.md)). In `cap`, the capture `k` adds a field to the sequence but
the matched `"x"` is still the first child; see [Captures](#32-captures).

**Only valued elements are children.** In `"(" expr ")"` the two parentheses are children of the `Seq`, next to the
expression. To keep them out, discard them with `-`; do the same for whitespace and separators.

**The start rule must consume the whole input.** Remaining input is a syntax error ("expected end of input"), so the
start rule needs no `$$` for that. What it does need is to consume trailing whitespace and comments explicitly.

## 3. Shaping the tree

### 3.1 Atomic and discard

`@a` matches `a` and produces *one* terminal holding the matched text, instead of the tree of `a`. Use it for tokens:
a number is one node, not a list of digits. `-a` matches `a` and produces nothing. Use it for input the syntax needs but
the tree does not:

```pego
// shape.pego
def main = ^^ assign $$
def assign = name -ws -"=" -ws value -";"
def name = @(?a-z)+
def value = number / name
def number = @(?0-9)+
def ws = " "*
```

```bash
pego parse -g shape.pego -f sexpr -i 'ab = 12;'
```

```text
(Seq (Seq "ab"@name "12"@number)@assign)@main
```

Compare this with the tree in section 1: the same input now yields a `Seq` of just two terminals (inside the `Seq` of
`main`). Both operators suppress construction, so they are also cheaper (see
[Performance](#10-performance-guidelines)). Because captures are meaningless inside them, `@(k:"x" "y")` and
`-(k:"x" "y")` are compile errors.

### 3.2 Captures

A capture `label:a` gives the value of `a` a name that actions and predicates can use as `$label`. Without an action,
captures also show up in the tree: they become **fields** of the node. They do not remove anything from the children.

```pego
// capture.pego
def pair = k:name -ws -"=" -ws v:value -";"
def name = @(?a-z)+
def value = number / name
def number = @(?0-9)+
def ws = " "*
def list = name ("," n:name)*
def list2 = name (-"," n:name)*
```

```bash
pego parse -g capture.pego -s pair -f sexpr -i 'ab = 12;'
pego parse -g capture.pego -s list -f sexpr -i 'a,b,c'
pego parse -g capture.pego -s list2 -f sexpr -i 'a,b,c'
```

```text
(Seq "ab"@name "12"@number k="ab"@name v="12"@number)@pair
(Seq "a"@name [(Seq "," "b"@name n="b"@name) (Seq "," "c"@name n="c"@name)])@list
(Seq "a"@name [(Seq "b"@name n="b"@name) (Seq "c"@name n="c"@name)])@list2
```

Note how `k` and `v` appear both as children and as fields (the same node is printed twice). A capture inside a
repetition belongs to the **scope of one iteration**: each element of the list is a `Seq` that carries the field `n`.
If the iteration's value is not a `Seq` the engine wraps it in one, so that the field has somewhere to live. This is
what makes `$rest` in `foldl($first, $rest, (acc, i) => ... $i.n ...)` work (see [recipes](#9-recipes)).

Two details matter in practice:

- A capture that did not match (inside `?` or in only some alternatives of a choice) is `nil`, and its type is
  optional (`*T`).
- A captured value keeps its **whole subtree**. If you capture a repetition such as `rest:(ws "," ws x:ident)*`, every
  whitespace node inside it stays in the tree, because you may inspect it with `.children` or `text` later. Discard the
  parts you do not need *inside* the capture, punctuation included: `rest:(-ws -"," -ws x:ident)*`.

### 3.3 Unwrapping the root

A start rule such as `main = ^^ expr $$` wraps the real result in a `Seq`. When you build an AST, return the value you
want instead:

```pego
def main: Expr = e:expr $$ -> $e
```

This is the idiom of the calculator example.

## 4. Declaring types

### 4.1 Structs, unions and terminal types

A **struct type** is a node type with named fields; an action builds its nodes with `new`. A **union**
(`type T = A | B`) is the type of a value that is one of several types, much like a Go interface. A **terminal type** (`type T terminal`)
is a node type for tokens: a rule declared with it produces a terminal of that type holding the matched text,
whatever its body looks like.

```pego
// assign-ast.pego
type Name terminal
type Number terminal
type Value = Name | Number
type Assign struct {
    Name  Name
    Value Value
}

def main = ^^ assign $$
def assign: Assign = k:name -ws -"=" -ws v:value -";" -> new Assign{Name: $k, Value: $v}
def value: Value = number / name
def name: Name = (?a-z)+
def number: Number = (?0-9)+
def ws = " "*
```

```bash
pego parse -g assign-ast.pego -f sexpr -i 'ab = 12;'
pego parse -g assign-ast.pego -f sexpr -i 'ab = cd;'
```

```text
(Seq (Assign Name=Name"ab"@name Value=Number"12"@number))@main
(Seq (Assign Name=Name"ab"@name Value=Name"cd"@name))@main
```

Terminal-typed rules need no `@`: `def name: Name = (?a-z)+` is a `Name"ab"` terminal, not a list. The `Assign` node has
no `@rule` label because it was built by an action, not by the default tree. Type names and field names start with an
uppercase letter; rule names are in a separate namespace, so `def name: Name` is fine. `Match`, `Seq`, `List`,
`Operator` and `Error` are predefined node types, and `int`, `string`, `bool`, `node` and `terminal` are built in (a
rule's value must be a node, but struct fields and action expressions may also hold `int`, `string` and `bool`).

### 4.2 Optional and list fields

A field type can be `*T` (a `T` or `nil`) or `[]T` (a `List` whose elements are `T`). Both are checked at compile time:
a value that may be `nil` cannot go into a plain `T` field.

```pego
// decls.pego
type Ident terminal
type Num terminal
type Lit = Ident | Num

type Decl struct {
    Name  Ident
    Value *Lit // optional: nil when there is no initializer
}

type Program struct { Decls []Decl }

def main: Program = ds:decls $$ -> new Program{Decls: $ds}
def decls = ds:(-ws d:decl)* -ws -> map($ds, (x) => $x.d)
def decl: Decl = -"let" -ws n:ident v:init? -ws -";" -> new Decl{Name: $n, Value: $v}
def init: Lit = -ws -"=" -ws l:lit -> $l
def lit: Lit = num / ident
def ident: Ident = (?a-z)+
def num: Num = (?0-9)+
def ws = (? \n)*
```

```bash
pego parse -g decls.pego -f sexpr -i 'let x = 1; let y;'
```

```text
(Program Decls=[(Decl Name=Ident"x"@ident Value=Num"1"@num) (Decl Name=Ident"y"@ident Value=nil)])
```

A field that an action does not set is absent rather than `nil`; in JSON output it is omitted, while an explicit `nil`
prints as `null`.

### 4.3 Rule result types and inference

A rule can declare its type: `def name: T = ...`. The checker verifies that the value of the body (or of the action) is
assignable to `T`, and every use of the rule then sees `T`. If you leave the type out it is **inferred** from the body
or the action, iterating until recursive rules stop changing:

| Rule | Inferred type |
|:--|:--|
| `def num = @(?0-9)+` | `Match` |
| `def key = (?a-z)+` | `[]Match` |
| `def pair = k:num "=" v:num` | `Seq` with the fields `k: Match` and `v: Match` |
| `def v = a / b` | The union of the types of `a` and `b` |
| `def o = a?` | `*T`, where `T` is the type of `a` |

Declare types on the rules whose values flow into struct fields, and on recursive rules; leave helpers inferred. The
type shows up in the error messages, for example when a field expects a `Match` and the rule returns a list:

```pego
// field-type.pego
type Pair struct { Key Match, Value Match }
def pair: Pair = k:key "=" v:value -> new Pair{Key: $k, Value: $v}
def key = (?a-z)+
def value = @(?0-9)+
```

```bash
pego parse -g field-type.pego -s pair -i 'a=1'
```

```text
pego: field-type.pego:3:48: cannot use []Match as Match in field Key of Pair
```

The fix is to make `key` a single token: `def key = @(?a-z)+`, or give it a terminal type.

A terminal type is a property of a rule *without* an action: the rule's value is the matched text. If the rule has an
action, the action's value is the rule's value, and it must already be of that type:

```pego
// term-action.pego
type Num terminal
def num: Num = " "* d:(?0-9)+ -> $d
```

```bash
pego parse -g term-action.pego -s num -i ' 12'
```

```text
pego: term-action.pego:3:1: rule num produces []Match, which is not assignable to Num
```

Keep whitespace and other trivia out of token rules: write `def num: Num = (?0-9)+` and put `-ws` in the rules that
use it.

## 5. Actions

An action is an expression after `->` that computes the value of the rule from what the body matched:

```pego
def <rule>: <type> = <parsing expression> -> <action>
```

The action covers the whole right-hand side, and its result must be a node (or `nil`). There is no way to attach an
action to one alternative of a choice or to a group; give each alternative its own rule:

```pego
// inline-action.pego
def bad = "a" ("b" -> $1)
```

```bash
pego parse -g inline-action.pego -s bad -i 'ab'
```

```text
pego: inline-action.pego:2:20: expected ')', found '->'
```

### 5.1 Referring to captures

| Syntax | Refers to |
|:--|:--|
| `$label` | The value captured with `label:` in this rule |
| `$n` | The `n`-th (1-based) element of the rule body that has a value |
| `$0` | A list of the values of all elements that have a value |

Prefer `$label`. `$n` counts only elements that have a value, so adding a discard or a lookahead does not shift the
numbers, but inserting a valued element does, and a rule that uses `$n` must build the full `Seq` of its body (see
[Performance](#10-performance-guidelines)). The rule below is the same as the one in section 4.1 but written with
positions:

```pego
// positional.pego
type Name terminal
type Number terminal
type Value = Name | Number
type Assign struct { Name Name, Value Value }

def assign: Assign = name -ws -"=" -ws value -";" -> new Assign{Name: $1, Value: $2}
def value: Value = number / name
def name: Name = (?a-z)+
def number: Number = (?0-9)+
def ws = " "*
def both = name "," name -> $0
```

```bash
pego parse -g positional.pego -s assign -f sexpr -i 'ab = cd;'
pego parse -g positional.pego -s both -f sexpr -i 'ab,cd'
```

```text
(Assign Name=Name"ab"@name Value=Name"cd"@name)
[Name"ab"@name "," Name"cd"@name]
```

Captures are visible only in the rule that made them, not in the rules it calls or that call it. A reference to an
undefined capture is a compile-time error (`undefined capture $z`).

### 5.2 Building nodes and reading fields

`new T{Field: value, ...}` builds a struct node. Field values are checked against the declared field types, so typos and
mismatches are compile-time errors:

```pego
// field-errors.pego
type Name terminal
type Number terminal
type Assign struct { Name Name, Value Number }

def a: Assign = k:name "=" v:name -> new Assign{Name: $k, Value: $v}
def b: Assign = k:name "=" v:number -> new Assign{Name: $k, Valu: $v}
def name: Name = (?a-z)+
def number: Number = (?0-9)+
```

```bash
pego parse -g field-errors.pego -s a -i 'a=b'
```

```text
pego: field-errors.pego:7:61: Assign has no field Valu
```

`x.Field` reads a field of a node; `x` may be a capture or the result of another access. Every node also has the fields
`startPos`, `endPos` (integers) and `children` (a list). Reading a field of a value that may be `nil` is an error, so
`n:name?` followed by `$n.startPos` does not compile. When `x` has a union type the field is allowed if at least one
member declares it.

Struct fields can hold integers, strings and booleans too, which is how you attach derived data to a node:

```pego
// derived.pego
type Word struct {
    Text   string
    Length int
    Long   bool
    From   int
    To     int
}

def main: Word = ws w:word ws $$ -> $w
def word: Word = n:@(?a-z)+
    -> new Word{Text: text($n), Length: len($n), Long: len($n) > 4, From: $n.startPos, To: $n.endPos}
def ws = " "*
```

```bash
pego parse -g derived.pego -f sexpr -i '  hello '
```

```text
(Word From=2 Length=5 Long=true Text=`hello` To=7)
```

## 6. Built-in functions and lambdas

| Function | Result |
|:--|:--|
| `len(x)` | The length of a string or terminal in positions; the number of children of any other node; `0` for `nil` |
| `text(x)` | The input text that the node covers; `""` for `nil` |
| `list(a, b, ...)` | A list of the arguments (nodes or `nil`) |
| `concat(l1, l2, ...)` | The concatenation of lists; a `nil` argument counts as an empty list |
| `map(list, (x) => ...)` | The list of the results of applying the function to each element |
| `foldl(init, list, (acc, x) => ...)` | Folds the list from the left, starting with `init` |
| `foldr(init, list, (acc, x) => ...)` | Folds the list from the right, starting with `init` |

The function arguments are **lambdas**. They can only be written as arguments of `map`, `foldl` and `foldr`, and inside
them the parameters are referred to with `$`, just like captures: `$acc`, `$x`. A lambda can also read the captures of
the enclosing rule. The function passed to `map` must produce a node (or `nil`).

```pego
// builtins.pego
type Call struct {
    Name  string
    Arity int
    Raw   string
}

def call: Call = n:@(?a-z)+ "(" args:(a:@(?a-z)+ -","?)* ")"
    -> new Call{Name: text($n), Arity: len($args), Raw: text($args)}
```

```bash
pego parse -g builtins.pego -s call -f sexpr -i 'f(a,b,c)'
```

```text
(Call Arity=3 Name=`f` Raw=`a,b,c`)
```

`text($args)` is the input text covered by the node, including the commas that were discarded from the tree: ranges are
properties of the input, not of the children. Here is a lambda that reads a capture of the enclosing rule, `$k`, to
pair the key with every item:

```pego
// outer-capture.pego
type Ident terminal
type Pair struct { A Ident, B Ident }
type Pairs struct { Items []Pair }

def pairs: Pairs = k:ident ":" xs:(x:ident -","?)*
    -> new Pairs{Items: map($xs, (r) => new Pair{A: $k, B: $r.x})}
def ident: Ident = (?a-z)+
```

```bash
pego parse -g outer-capture.pego -s pairs -f sexpr -i 'k:a,b'
```

```text
(Pairs Items=[(Pair A=Ident"k"@ident B=Ident"a"@ident) (Pair A=Ident"k"@ident B=Ident"b"@ident)])
```

Action expressions also support the arithmetic, comparison and boolean operators listed in the
[Actions chapter](../../spec/actions.md#operators). There is no conditional expression, so branch on the grammar side
(separate rules or alternatives) rather than inside actions.

## 7. Positions

Every node has a range `[start, end)`. Nodes of the default tree cover what their expression matched. A node built by
`new` has one of two ranges:

- If it is the **result of the action**, it covers what the rule matched.
- If it is created elsewhere, for example inside a `foldl` lambda, it covers the smallest range that includes the nodes
  in its fields (or the rule's match if it has none).

Lists made by `list`, `map` and `concat` cover their elements. This is what makes the nested nodes of a left fold span
exactly their operands, which the Go program below prints.

Positions are counted in **code points** by default, or in UTF-8 **bytes** with `-unit bytes`
(`pego.WithUnit(pego.Bytes)` in Go). Matching is always by code point; the unit only changes how positions and `len`
are measured. The value `v` below starts at position 3 in code points and at 7 in bytes:

```pego
// unit.pego
type Name terminal
def name: Name = (?^=)+
def kv = k:name "=" v:name
```

```bash
pego parse -g unit.pego -s kv -i 'あい=うえお' | grep -A6 '"v"'
pego parse -g unit.pego -s kv -i 'あい=うえお' -unit bytes | grep -A6 '"v"'
```

```text
    "v": {
      "type": "Name",
      "rule": "name",
      "start": 3,
      "end": 6,
      "text": "うえお"
    }
    "v": {
      "type": "Name",
      "rule": "name",
      "start": 7,
      "end": 16,
      "text": "うえお"
    }
```

## 8. Using the tree from Go

`Parser.Parse` returns a `*pego.Node`. Its exported fields mirror the JSON keys: `Type`, `Rule`, `Start`, `End`,
`Text`, `Children` and `Fields`. `Field(name)` returns the value of a struct field or capture (a `*pego.Node`, `int`,
`string`, `bool` or `nil`), and `IsTerminal` tells terminals apart. The node also marshals to the JSON shown above with
`encoding/json`.

The program below parses a left-associative sum (the grammar of
[Recipe 1](#91-left-associative-trees-with-foldl) without the `main` rule) and prints every node with its range:

```go
// main.go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/ornew/pego"
)

const src = `
type Num terminal
type Op terminal
type Bin struct {
    Left  Expr
    Op    Op
    Right Expr
}
type Expr = Num | Bin

def sum: Expr = first:num rest:(-ws op:op -ws n:num)*
    -> foldl($first, $rest, (acc, i) => new Bin{Left: $acc, Op: $i.op, Right: $i.n})
def num: Num = (?0-9)+
def op: Op = "+" / "-"
def ws = " "*
`

func main() {
	p, err := pego.CompileSource(src, "sum")
	if err != nil {
		log.Fatal(err)
	}
	n, err := p.Parse("1 - 2 + 3")
	if err != nil {
		log.Fatal(err)
	}
	dump(n, 0)

	// Fields are read by name; terminals carry their text.
	right := n.Field("Right").(*pego.Node)
	fmt.Println("right operand:", right.Text, right.Start, right.End)
}

func dump(n *pego.Node, depth int) {
	fmt.Printf("%s%s [%d,%d)", strings.Repeat("  ", depth), n.Type, n.Start, n.End)
	if n.IsTerminal() {
		fmt.Printf(" %q", n.Text)
	}
	fmt.Println()
	for _, f := range n.Fields {
		if c, ok := f.Value.(*pego.Node); ok && c != nil {
			fmt.Printf("%s.%s\n", strings.Repeat("  ", depth+1), f.Name)
			dump(c, depth+2)
		}
	}
	for _, c := range n.Children { // Seq, List and Operator nodes; a nil child is an unmatched optional
		if c != nil {
			dump(c, depth+1)
		}
	}
}
```

```text
Bin [0,9)
  .Left
    Bin [0,5)
      .Left
        Num [0,1) "1"
      .Op
        Op [2,3) "-"
      .Right
        Num [4,5) "2"
  .Op
    Op [6,7) "+"
  .Right
    Num [8,9) "3"
right operand: 3 8 9
```

The inner `Bin` covers `1 - 2` and the outer one the whole input, even though the inner node was created inside the
lambda.

## 9. Recipes

### 9.1 Left-associative trees with `foldl`

A repetition gives a flat list; a fold turns it into the nested tree. Capture the first operand, capture the repeated
`(operator, operand)` pairs, and fold from the left, so that `1 - 2 + 3` becomes `(1 - 2) + 3`:

```pego
// sum.pego
type Num terminal
type Op terminal
type Bin struct {
    Left  Expr
    Op    Op
    Right Expr
}
type Expr = Num | Bin

def main: Expr = e:sum $$ -> $e
def sum: Expr = first:num rest:(-ws op:op -ws n:num)*
    -> foldl($first, $rest, (acc, i) => new Bin{Left: $acc, Op: $i.op, Right: $i.n})
def num: Num = (?0-9)+
def op: Op = "+" / "-"
def ws = " "*
```

```bash
pego parse -g sum.pego -f sexpr -i '1 - 2 + 3'
pego parse -g sum.pego -f sexpr -i '7'
```

```text
(Bin Left=(Bin Left=Num"1"@num Op=Op"-"@op Right=Num"2"@num) Op=Op"+"@op Right=Num"3"@num)
Num"7"@num
```

Notes: the accumulator starts as a `Num` and becomes a `Bin`, so its type is widened to the union of both, and the
declared `Expr = Num | Bin` must contain both. With a single operand the repetition is empty and the fold returns the
initial value unchanged, which is why `7` stays a bare `Num`. For several precedence levels you would write one such
rule per level; for that case also look at [Pratt expressions](expressions.md).

### 9.2 Right-associative trees with `foldr`

`a ^ b ^ c` must be `a ^ (b ^ c)`. Capture the pairs *(operand, operator)* and the last operand separately, and fold
from the right, starting with the last operand:

```pego
// pow.pego
type Num terminal
type Op terminal
type Bin struct {
    Left  Expr
    Op    Op
    Right Expr
}
type Expr = Num | Bin

def pow: Expr = pairs:(l:num -ws op:op -ws)* last:num
    -> foldr($last, $pairs, (acc, i) => new Bin{Left: $i.l, Op: $i.op, Right: $acc})
def num: Num = (?0-9)+
def op: Op = "^"
def ws = " "*
```

```bash
pego parse -g pow.pego -s pow -f sexpr -i '2 ^ 3 ^ 4'
pego parse -g pow.pego -s pow -f sexpr -i '2'
```

```text
(Bin Left=Num"2"@num Op=Op"^"@op Right=(Bin Left=Num"3"@num Op=Op"^"@op Right=Num"4"@num))
Num"2"@num
```

The repetition is greedy and does not backtrack, but it is still correct here: it consumes `2 ^` and `3 ^`, then
`4` fails to be followed by `^`, so the repetition stops and `last` matches `4`.

### 9.3 Member chains with a union accumulator

The same fold builds `a.b.c`. The accumulator is first an `Ident` and then a `Member`, so declare the union and use it
for the rule type and the field:

```pego
// path.pego
type Ident terminal
type Member struct {
    X    Path
    Name Ident
}
type Path = Ident | Member

def path: Path = first:ident rest:(-"." n:ident)*
    -> foldl($first, $rest, (acc, i) => new Member{X: $acc, Name: $i.n})
def ident: Ident = (?a-z)+
```

```bash
pego parse -g path.pego -s path -f sexpr -i 'a.b.c'
```

```text
(Member Name=Ident"c"@ident X=(Member Name=Ident"b"@ident X=Ident"a"@ident))
```

### 9.4 Flattening separated lists

`first (sep item)*` yields a nested `Seq`/`List` structure. To get one flat list of items, capture the repeated part and
rebuild a list: `list($first)` makes a one-element list, `map` extracts the item from every iteration, and `concat`
joins them. A list that is optional (`args?`) works with `concat` too, because a `nil` argument counts as empty:

```pego
// args.pego
type Ident terminal
type Call struct {
    Fn   Ident
    Args []Ident
}

def call: Call = fn:ident -ws "(" -ws args:args? -ws ")"
    -> new Call{Fn: $fn, Args: concat($args)}
def args = first:ident rest:(-ws "," -ws x:ident)*
    -> concat(list($first), map($rest, (r) => $r.x))
def args_cst = ident (-ws "," -ws ident)*
def ident: Ident = (?a-z)+
def ws = " "*
```

```bash
pego parse -g args.pego -s call -f sexpr -i 'f(a, b, c)'
pego parse -g args.pego -s call -f sexpr -i 'f()'
pego parse -g args.pego -s args_cst -f sexpr -i 'a, b, c'
```

```text
(Call Args=[Ident"a"@ident Ident"b"@ident Ident"c"@ident] Fn=Ident"f"@ident)
(Call Args=[] Fn=Ident"f"@ident)
(Seq Ident"a"@ident [(Seq "," Ident"b"@ident) (Seq "," Ident"c"@ident)])@args_cst
```

`args_cst` shows what you would otherwise get: the first item followed by a list of `(Seq "," item)`. Writing
`Args: $args` directly does not compile, because `$args` is optional (`*[]Ident`) and the field is `[]Ident`:

```pego
// args-optional.pego
type Ident terminal
type Call struct { Fn Ident, Args []Ident }
def call: Call = fn:ident "(" args:args? ")" -> new Call{Fn: $fn, Args: $args}
def args = first:ident rest:(-"," x:ident)* -> concat(list($first), map($rest, (r) => $r.x))
def ident: Ident = (?a-z)+
```

```bash
pego parse -g args-optional.pego -s call -i 'f(a)'
```

```text
pego: args-optional.pego:4:67: cannot use *[]Ident as []Ident in field Args of Call
```

### 9.5 Keeping only the AST you need

The parse tree of a realistic language is full of trivia: whitespace, comments, separators and keywords. The AST should
contain the data. Four habits get you there:

1. **Name what you need, discard the rest.** Capture the values the action uses and put `-` in front of everything
   else, including whitespace rules that also skip comments.
2. **Discard inside captured repetitions**, not only around them (see [Captures](#32-captures)).
3. **Pick fields with `map`** when each iteration carries more than the value you want.
4. **Unwrap the start rule** with `e:rule $$ -> $e`.

```pego
// config.pego
type Ident terminal
type Value terminal
type Entry struct {
    Key   Ident
    Value Value
}
type Config struct { Entries []Entry }

def main: Config = es:entries ws $$ -> new Config{Entries: $es}
def entries = es:(-ws e:entry)* -> map($es, (x) => $x.e)
def entry: Entry = k:key -ws -"=" -ws v:value -ws -";" -> new Entry{Key: $k, Value: $v}
def key: Ident = (?a-z_)+
def value: Value = (?a-z0-9.)+
def ws = ((? \t\r\n) / "#" (?^\n)*)*
```

```bash
pego parse -g config.pego -f sexpr -i $'# web server\nhost = example.com;  # comment\nport = 8080;\n'
```

```text
(Config Entries=[(Entry Key=Ident"host"@key Value=Value"example.com"@value) (Entry Key=Ident"port"@key Value=Value"8080"@value)])
```

Comments, whitespace, `=` and `;` are all gone; only the entries remain. If you also need the comments (for a
formatter, say), capture them instead of discarding them.

## 10. Performance guidelines

PEGO builds values only where something can observe them. The guidelines of
[performance.md](../performance.md#grammar-authoring-guidelines-for-performance) boil down to helping the engine know
what is unobserved:

1. **Discard trivia inside captured expressions** with `-x`: `rest:(-ws -"," -ws v:value)*`. The engine cannot drop it
   automatically, because a captured value exposes its full CST.
2. **Use terminal types or `@(...)` for tokens.** Their bodies are matched without building values: an identifier is
   one node instead of one node per character.
3. **Write actions with captures rather than `$n`.** A rule whose action does not use `$n` is matched without building
   the `Seq` of its body.

How much do they matter? Parsing one 140 KB list of 20,000 identifiers separated by `" ,  "` with the default backend
allocated, measured around a single `Parse` call with `runtime.MemStats`:

| Grammar | Allocated per parse |
|:--|--:|
| Whitespace kept in the captured repetition (`rest:(ws "," ws x:ident)*`), `ident` a terminal type | 27.8 MB |
| Whitespace discarded (`rest:(-ws "," -ws x:ident)*`) | 14.0 MB |
| Whitespace and the separator discarded (`rest:(-ws -"," -ws x:ident)*`) | 11.3 MB |
| Whitespace discarded, but `ident` without a type: `def ident = (?a-z)+` (a list of `Match` nodes) | 22.3 MB |
| Same, with `def ident = @(?a-z)+` | 14.0 MB |

The shipped example grammars follow the same rules. Discarding their trivia was part of a change that took the JSON
example from 50.5 MB to 42.8 MB per parse and minilang from 32.8 MB to 23.0 MB, with identical ASTs (change 9 in the
[tuning log](../performance.md#9-value-free-pratt-lines-discarding-trivia-in-example-grammars-936857c)).

If you only need to know whether the input is valid, use `pego parse -check` (`pego.RecognizeOnly()`): it builds no
values and runs no actions.

## 11. Troubleshooting

| Message | Cause and fix |
|:--|:--|
| `cannot use X as Y in field F of T` | The value's type does not fit the field. Inspect the rule's type (declared or inferred) and change the rule, the field or use a union |
| `cannot use *X as X in field F of T` | The value may be `nil` (an optional or a capture in one alternative). Declare the field `*X`, or make the grammar guarantee a match |
| `cannot use *[]X as []X in field F of T` | An optional list; pass it through `concat(...)` to get an empty list instead of `nil` |
| `T has no field F` | A typo, or the struct does not declare the field |
| `cannot access .F of *T` | The value may be `nil` (for example `n:name?`). Read the field in a rule where the value is always present |
| `rule r produces X, which is not assignable to T` | The declared type does not accept the body's value; the CST of a sequence is a `Seq`, so a struct type needs an action |
| `expected ')', found '->'` | An action was written inside a group; actions apply to a whole rule, so split the alternative into its own rule |
| `capture k inside @, -, or ! has no effect` | Captures cannot be made inside atomic, discard or negative lookahead; capture the whole expression or restructure |
| `undefined capture $z` | The capture is not made in this rule; captures are not visible across rules |
| `$n is out of range: the rule has N elements with a value` | `$n` counts only elements with a value |
| `syntax error: expected ..., end of input` after a complete-looking input | The start rule must consume all the input. Consume trailing whitespace and comments explicitly |
