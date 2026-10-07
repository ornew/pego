# Parsing Expressions with Operators

Expressions with operators (`1 + 2 * 3`, `-x ** 2`, `f(a)[i].name`, `c ? a : b`) are where a grammar needs
precedence and associativity. PEGO gives you three ways to write them:

| Technique | You write | Typical size |
|:--|:--|:--|
| **Pratt expression** (`pratt { ... }`) | A table: operands, then one `level` per precedence level, each with its operators | One rule |
| **Left-recursive rules** | One rule per level, each of the form `level = level OP next / next` | One rule per level |
| **Precedence chain** | One rule per level, each a repetition `next (OP next)*` folded with `foldl` | One rule per level |

This guide builds the same calculator in all three styles, compares the trees and error messages they produce, and
helps you choose. It relies on [Trees and actions](trees-and-actions.md) for captures, types and `foldl`; the normative
text is in [Pratt Expressions](../../spec/pratt.md) and
[Left recursion](../../spec/parser-expressions.md#left-recursion), and the reasoning behind the design in
[design record 005](../design/005-pratt-expressions.md) and [design record 003](../design/003-packrat-parsing.md).

To follow along, put each grammar in the file named on its first line and run the commands shown (`pego` is the tool
from `go install github.com/ornew/pego/cmd/pego@latest`, or `go run ./cmd/pego` in a checkout). The Pratt and the
left-recursive calculator are the files `calc.pego` and `calc_lr.pego` in
[`examples/calculator`](../../examples/calculator) (without their header comments); the chain variant is written out
below.

## 1. A Pratt calculator

```pego
// calc.pego
package calc

type Number terminal

type Binary struct {
    Left  Expr
    Op    Match
    Right Expr
}

type Unary struct {
    Op Match
    X  Expr
}

type Expr = Number | Binary | Unary

def main: Expr = e:expr ws $$ -> $e

def expr: Expr = pratt {
    skip    ws
    operand number
    operand "(" e:expr ws ")" -> $e
    level { infix left  "+" / "-"       -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix left  "*" / "/" / "%" -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { prefix      "-" / "+"       -> new Unary{Op: $op, X: $rhs} }
    level { infix right "^"             -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
}

def number: Number = (?0-9)+ ("." (?0-9)+)?
def ws = (? \t\r\n)*
```

```bash
pego parse -g calc.pego -f sexpr -i '1 + 2 * 3'
pego parse -g calc.pego -f sexpr -i '1 - 2 - 3'
pego parse -g calc.pego -f sexpr -i '2 ^ 3 ^ 2'
pego parse -g calc.pego -f sexpr -i '-2 ^ 2'
pego parse -g calc.pego -f sexpr -i '2 ^ -1'
pego parse -g calc.pego -f sexpr -i '(1 + 2) * 3'
```

```text
(Binary Left=Number"1"@number Op="+" Right=(Binary Left=Number"2"@number Op="*" Right=Number"3"@number))
(Binary Left=(Binary Left=Number"1"@number Op="-" Right=Number"2"@number) Op="-" Right=Number"3"@number)
(Binary Left=Number"2"@number Op="^" Right=(Binary Left=Number"3"@number Op="^" Right=Number"2"@number))
(Unary Op="-" X=(Binary Left=Number"2"@number Op="^" Right=Number"2"@number))
(Binary Left=Number"2"@number Op="^" Right=(Unary Op="-" X=Number"1"@number))
(Binary Left=(Binary Left=Number"1"@number Op="+" Right=Number"2"@number) Op="*" Right=Number"3"@number)
```

Here is how to read the `pratt` block.

- **`skip ws`** is inserted, with its result discarded, before every operand and every operator. It is how a grammar
  without a scanner says "tokens may be separated by whitespace". Without a `skip`, nothing is inserted.
- **`operand`** items describe what an expression can start with when no prefix operator applies: numbers and
  parenthesized expressions here. Several `operand` items are tried in order, like an ordered choice. An action belongs
  to its own item, so alternatives that need different actions are separate items (`operand "(" e:expr ws ")" -> $e`
  unwraps the parentheses). An operand cannot start by calling its own rule: that would be left recursion, and it is a
  compile error. Constructs that take an expression on their left (calls, indexing) are operators.
- **`level { ... }`** declares one binding level. **Levels are listed from the loosest to the tightest**, so inserting a
  level is a one-line change and nothing else has to be renumbered. All operators of a level share a precedence, and a
  level may mix prefix, infix and postfix operators.
- **`infix left | right | none`** gives the associativity: `1 - 2 - 3` is `(1 - 2) - 3`, `2 ^ 3 ^ 2` is `2 ^ (3 ^ 2)`,
  and `none` operators do not chain (section 5).
- **An operator part** (`"+" / "-"`) is an ordinary parsing expression. It may be several tokens, call other rules, or
  contain captures (section 4).
- **Actions** see `$lhs`, `$rhs` (the operands) and `$op` (the text the operator part matched, as a `Match` node). In
  a rule with a declared type, `$lhs` and `$rhs` have that type, which is what lets the `Binary` fields be `Expr`.

The unary minus is declared in a level **tighter than `*` but looser than `^`**: `-2 ^ 2` is `-(2 ^ 2)`, while `2 ^ -1`
still parses, because a prefix operator at the start of an operand position is not restricted by the levels.

If the input ends where an operand is required, the error lists what could have followed, without the whitespace that
`skip` would have consumed (compare the next section):

```bash
pego parse -g calc.pego -f sexpr -i '1 +'
```

```text
pego: 1:4: syntax error: expected "(", "+", "-", (?0-9)
```

## 2. The same language with left recursion

A PEG rule may call itself in leftmost position, directly or through other rules. PEGO detects such cycles and parses
them by growing a seed: it first matches the non-recursive alternatives, then repeatedly tries again with the result so
far as the recursive call, as long as the match gets longer. The result is exactly what the grammar says in the textbook
notation, in linear time and without any rewriting:

```pego
// calc_lr.pego
package calc

type Number terminal

type Binary struct {
    Left  Expr
    Op    Match
    Right Expr
}

type Unary struct {
    Op Match
    X  Expr
}

type Expr = Number | Binary | Unary

def main: Expr = e:expr ws $$ -> $e

def expr: Expr = add / term
def add: Expr = l:expr ws op:@("+" / "-") r:term -> new Binary{Left: $l, Op: $op, Right: $r}

def term: Expr = mul / unary
def mul: Expr = l:term ws op:@("*" / "/" / "%") r:unary -> new Binary{Left: $l, Op: $op, Right: $r}

def unary: Expr = neg / power
def neg: Expr = ws op:@("-" / "+") x:unary -> new Unary{Op: $op, X: $x}

// Exponentiation is right-associative, and its right operand may carry a unary operator (2^-1).
def power: Expr = pow / primary
def pow: Expr = l:primary ws op:"^" r:unary -> new Binary{Left: $l, Op: $op, Right: $r}

def primary: Expr = ws p:(number / group) -> $p
def group: Expr = "(" e:expr ws ")" -> $e

def number: Number = (?0-9)+ ("." (?0-9)+)?
def ws = (? \t\r\n)*
```

```bash
pego parse -g calc_lr.pego -f sexpr -i '1 + 2 * 3'
pego parse -g calc_lr.pego -f sexpr -i '1 - 2 - 3'
pego parse -g calc_lr.pego -f sexpr -i '2 ^ 3 ^ 2'
pego parse -g calc_lr.pego -f sexpr -i '-2 ^ 2'
pego parse -g calc_lr.pego -f sexpr -i '2 ^ -1'
pego parse -g calc_lr.pego -f sexpr -i '(1 + 2) * 3'
```

```text
(Binary Left=Number"1"@number Op="+" Right=(Binary Left=Number"2"@number Op="*" Right=Number"3"@number))
(Binary Left=(Binary Left=Number"1"@number Op="-" Right=Number"2"@number) Op="-" Right=Number"3"@number)
(Binary Left=Number"2"@number Op="^" Right=(Binary Left=Number"3"@number Op="^" Right=Number"2"@number))
(Unary Op="-" X=(Binary Left=Number"2"@number Op="^" Right=Number"2"@number))
(Binary Left=Number"2"@number Op="^" Right=(Unary Op="-" X=Number"1"@number))
(Binary Left=(Binary Left=Number"1"@number Op="+" Right=Number"2"@number) Op="*" Right=Number"3"@number)
```

The trees are identical, because both grammars build the same typed AST through actions. What differs is how the
grammar is organized.

- **One rule per level, linked in a chain.** `expr` is `add / term`, `term` is `mul / unary`, and so on. A new level
  means a new pair of rules and an edit to the rule above it.
- **Left associativity is the shape of the recursion**: `add = expr "+" term` makes the left operand as long as
  possible. **Right associativity** is recursion on the right: in `pow = primary "^" unary` the right operand is a
  `unary`, which can lead to another `pow`. Nothing in the text says "right associative"; it is a consequence of where
  the recursion is.
- **Whitespace is part of the rules**: every rule that starts a token or follows an operand has to say `ws`, and the
  main rule has to consume the trailing `ws`.
- **Every level is a rule you can call.** `term` parses a product without sums, which is useful if other parts of your
  grammar need exactly that (a Pratt expression offers the same with
  [level-restricted calls](#4-level-restricted-calls)).

```bash
pego parse -g calc_lr.pego -f sexpr -i '1 +'
```

```text
pego: 1:4: syntax error: expected "(", "+", "-", (? \t\r\n), (?0-9)
```

Note the extra `(? \t\r\n)` in the expected list: a rule-based grammar reports its whitespace, while `skip` is hidden
from error messages.

### Left recursion pitfalls

**The recursive alternative must come first.** Growing starts from the non-recursive match and then tries the
alternatives *in order* with the recursive call bound to the result so far. If the base case comes first, it matches
again with the same length, nothing grows, and the rest of the input is left over:

```pego
// lr-order.pego
def good = expr $$
def expr = expr "+" term / term
def term = @(?0-9)+

def bad = expr2 $$
def expr2 = term / expr2 "+" term
```

```bash
pego parse -g lr-order.pego -s good -f sexpr -i '1+2+3'
pego parse -g lr-order.pego -s bad -f sexpr -i '1+2+3'
```

```text
(Seq (Seq (Seq "1"@term "+" "2"@term)@expr "+" "3"@term)@expr)@good
pego: 1:2: syntax error: expected (?0-9), end of input
```

The error ("expected a digit or the end of input at column 2") is accurate but gives no hint about the order of
alternatives; when a left-recursive rule stops short, look at the order first.

**Ordered choice picks the first operator that matches, not the longest.** In `op:("<" / "<=")` the `<` wins on `1<=2`
and `=2` is left over; write `"<=" / "<"` (or put `!"="` after `"<"`). This is the usual PEG rule, and you have to
follow it in every operator list. A Pratt expression does not have the problem (section 8).

```pego
// lr-ops.pego
def rel = l:num op:("<" / "<=") r:num
def rel2 = l:num op:("<=" / "<") r:num
def num = @(?0-9)+
```

```bash
pego parse -g lr-ops.pego -s rel -f sexpr -i '1<=2'
pego parse -g lr-ops.pego -s rel2 -f sexpr -i '1<=2'
```

```text
pego: 1:3: syntax error: expected (?0-9)
(Seq "1"@num "<=" "2"@num l="1"@num op="<=" r="2"@num)@rel2
```

## 3. The precedence chain

The oldest way to write an expression grammar needs neither left recursion nor Pratt. Each level matches an operand of
the next level, followed by any number of operator-operand pairs, and `foldl` builds the left-leaning tree
([Recipe 1 of the trees guide](trees-and-actions.md#91-left-associative-trees-with-foldl)):

```pego
// chain.pego
type Number terminal
type Binary struct { Left Expr, Op Match, Right Expr }
type Unary struct { Op Match, X Expr }
type Expr = Number | Binary | Unary

def main: Expr = e:expr ws $$ -> $e

def expr: Expr = l:term rest:(ws op:@("+" / "-") r:term)*
    -> foldl($l, $rest, (acc, i) => new Binary{Left: $acc, Op: $i.op, Right: $i.r})

def term: Expr = l:unary rest:(ws op:@("*" / "/" / "%") r:unary)*
    -> foldl($l, $rest, (acc, i) => new Binary{Left: $acc, Op: $i.op, Right: $i.r})

def unary: Expr = neg / power
def neg: Expr = ws op:@("-" / "+") x:unary -> new Unary{Op: $op, X: $x}

// right-associative: the right operand is parsed by the rule that includes the operator itself
def power: Expr = pow / primary
def pow: Expr = l:primary ws op:"^" r:unary -> new Binary{Left: $l, Op: $op, Right: $r}

def primary: Expr = ws p:(number / group) -> $p
def group: Expr = "(" e:expr ws ")" -> $e
def number: Number = (?0-9)+ ("." (?0-9)+)?
def ws = (? \t\r\n)*
```

```bash
pego parse -g chain.pego -f sexpr -i '1 - 2 - 3'
pego parse -g chain.pego -f sexpr -i '2 ^ 3 ^ 2'
pego parse -g chain.pego -f sexpr -i '-2 ^ 2'
```

```text
(Binary Left=(Binary Left=Number"1"@number Op="-" Right=Number"2"@number) Op="-" Right=Number"3"@number)
(Binary Left=Number"2"@number Op="^" Right=(Binary Left=Number"3"@number Op="^" Right=Number"2"@number))
(Unary Op="-" X=(Binary Left=Number"2"@number Op="^" Right=Number"2"@number))
```

Left-associative levels use the repetition and `foldl`; right-associative ones recurse on the right. It works in any
PEG tool, and the only machinery it uses is ordered choice and repetition. Its costs are the same as for rules: one rule
per level, whitespace handled by hand, and one rule call per level for every operand, even a bare number.

**Non-associative operators.** Replace the repetition with `{0,1}`: it yields a list with at most one element, so
`foldl` handles it unchanged, and a second operator is simply not consumed:

```pego
// chain-cmp.pego
type Num terminal
type Bin struct { L E, Op Match, R E }
type E = Num | Bin

def main: E = e:cmp ws $$ -> $e

// at most one comparison: "1 < 2 < 3" is a syntax error
def cmp: E = l:add rest:(ws op:@("<=" / ">=" / "<" / ">" / "==" / "!=") r:add){0,1}
    -> foldl($l, $rest, (acc, i) => new Bin{L: $acc, Op: $i.op, R: $i.r})

def add: E = l:atom rest:(ws op:@("+" / "-") r:atom)*
    -> foldl($l, $rest, (acc, i) => new Bin{L: $acc, Op: $i.op, R: $i.r})

def atom: E = ws n:num -> $n
def num: Num = (?0-9)+
def ws = " "*
```

```bash
pego parse -g chain-cmp.pego -f sexpr -i '1 + 2 <= 3'
pego parse -g chain-cmp.pego -f sexpr -i '1 < 2 < 3'
```

```text
(Bin L=(Bin L=Num"1"@num Op="+" R=Num"2"@num) Op="<=" R=Num"3"@num)
pego: 1:7: syntax error: expected " ", "+", "-", end of input
```

## 4. Level-restricted calls

Some constructs contain an expression that must not use the loosest operators. The classic example is the argument list
of a call: in `f(a, b)` the comma separates arguments, so the arguments must be parsed *without* the comma operator,
while `(a, b)` in parentheses is a comma expression. A Pratt expression handles this with a **named level** and a call
with that level, `expr(assignment)`: it parses only operators of the named level and tighter ones. A call without a
level (`expr`) parses all levels. The rule name and `(` must be adjacent.

The grammar below is the one from the specification: it has every kind of operator.

```pego
// js.pego
type Num terminal
type Ident terminal
type Comma struct { L Expr, R Expr }
type Assign struct { L Expr, R Expr }
type Cond struct { Cond Expr, Then Expr, Else Expr }
type Bin struct { L Expr, Op Match, R Expr }
type Unary struct { Op Match, X Expr }
type Call struct { Fn Expr, Args []Expr }
type Index struct { X Expr, Index Expr }
type Member struct { X Expr, Name Ident }
type Expr = Num | Ident | Comma | Assign | Cond | Bin | Unary | Call | Index | Member

def expr: Expr = pratt {
    skip    ws
    operand number
    operand ident
    operand "(" e:expr ws ")" -> $e

    level            { infix left  ","                  -> new Comma{L: $lhs, R: $rhs} }
    level assignment { infix right "="                  -> new Assign{L: $lhs, R: $rhs} }
    level            { infix right "?" then:expr ws ":" -> new Cond{Cond: $lhs, Then: $then, Else: $rhs} }
    level            { infix none  "==" / "!="          -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level            { infix left  "+" / "-"            -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level            { infix left  "*" / "/"            -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level            { prefix      "-" / "!"            -> new Unary{Op: $op, X: $rhs} }
    level            { infix right "**"                 -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level {
        postfix "(" xs:args? ws ")"  -> new Call{Fn: $lhs, Args: concat($xs)}
        postfix "[" i:expr ws "]"    -> new Index{X: $lhs, Index: $i}
        postfix "." name:ident       -> new Member{X: $lhs, Name: $name}
    }
}

def args = first:expr(assignment) rest:(-ws "," x:expr(assignment))*
    -> concat(list($first), map($rest, (r) => $r.x))

def number: Num = (?0-9)+
def ident: Ident = (?a-z)+
def ws = (? \t\r\n)*
```

```bash
pego parse -g js.pego -s expr -f sexpr -i 'x, y'
pego parse -g js.pego -s expr -f sexpr -i 'f(a = 1, b)(2)[3].x'
pego parse -g js.pego -s expr -f sexpr -i 'a = b = c'
pego parse -g js.pego -s expr -f sexpr -i 'a ? b : c ? d : e'
pego parse -g js.pego -s expr -f sexpr -i '-a ** b'
pego parse -g js.pego -s expr -f sexpr -i '2 ** -1'
```

```text
(Comma L=Ident"x"@ident R=Ident"y"@ident)
(Member Name=Ident"x"@ident X=(Index Index=Num"3"@number X=(Call Args=[Num"2"@number] Fn=(Call Args=[(Assign L=Ident"a"@ident R=Num"1"@number) Ident"b"@ident] Fn=Ident"f"@ident))))
(Assign L=Ident"a"@ident R=(Assign L=Ident"b"@ident R=Ident"c"@ident))
(Cond Cond=Ident"a"@ident Else=(Cond Cond=Ident"c"@ident Else=Ident"e"@ident Then=Ident"d"@ident) Then=Ident"b"@ident)
(Unary Op="-" X=(Bin L=Ident"a"@ident Op="**" R=Ident"b"@ident))
(Bin L=Num"2"@number Op="**" R=(Unary Op="-" X=Num"1"@number))
```

Things to notice:

- `f(a = 1, b)` has two arguments: `args` calls `expr(assignment)`, which includes the `=` level and everything tighter
  but not `,`. Outside a call, `x, y` is a `Comma` node.
- The postfix operators `(...)`, `[...]` and `.name` share one level, so `f(a)(2)[3].x` chains left to right. Their
  operator parts are arbitrary parsing expressions: `"(" xs:args? ws ")"` parses an argument list with the rest of the
  grammar, and the whole matched part is `$op` (here the actions use the captures instead).
- `"?" then:expr ws ":"` is a mixfix operator: the middle operand is parsed by a full `expr` call, and the right operand
  by the Pratt loop, which makes `a ? b : c ? d : e` nest to the right.
- `-a ** b` is `-(a ** b)` because `**` is in a tighter level than the prefix `-`, and `2 ** -1` parses anyway.
- `skip` is **not** inserted inside an operator part, so `"?" then:expr ws ":"` spells out the whitespace before `":"`.
  See [pitfalls](#8-pitfalls).

## 5. Operator forms and associativity

| Declaration | Form | Notes |
|:--|:--|:--|
| `prefix P` | `P rhs` | The operand is parsed by tighter levels only |
| `postfix P` | `lhs P` | Applied repeatedly: `a!!` is `(a!)!` |
| `infix left P` | `lhs P rhs` | `a - b - c` is `(a - b) - c` |
| `infix right P` | `lhs P rhs` | `a ** b ** c` is `a ** (b ** c)`; the right operand may contain the same level |
| `infix none P` | `lhs P rhs` | `a == b == c` does not chain: the expression stops after `a == b` |
| `infix left _` | `lhs rhs` | An empty operator part is juxtaposition: function application, concatenation |

**Non-associative operators.** After `a == b`, an `==` of the same level is not consumed, so the enclosing rule sees
unexpected input. In the grammar of the previous section:

```bash
pego parse -g js.pego -s expr -f sexpr -i 'a == b == c'
```

```text
pego: 1:8: syntax error: expected "(", "*", "**", "+", ",", "-", ".", "/", "?", "["
```

The reported position is the second `==`, and the list of expected tokens contains neither `==` nor `!=`, which is the
accurate message for a chain: the expression stops after `a == b`, and the start rule then reports the leftover input.

**Juxtaposition.** An infix operator may match the empty string, which is how application is written. It is a normal
level, so you choose how tightly it binds:

```pego
// apply.pego
type Id terminal
type Num terminal
type Bin struct { L E, Op Match, R E }
type App struct { Fn E, Arg E }
type Neg struct { X E }
type E = Id | Num | Bin | App | Neg

def e: E = pratt {
    skip ws
    operand num
    operand id
    operand "(" x:e ws ")" -> $x
    level { infix left "+" / "-" -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix left _ -> new App{Fn: $lhs, Arg: $rhs} }
    level { prefix "-" -> new Neg{X: $rhs} }
}
def num: Num = (?0-9)+
def id: Id = (?a-z)+
def ws = " "*
```

```bash
pego parse -g apply.pego -s e -f sexpr -i 'f x y'
pego parse -g apply.pego -s e -f sexpr -i 'f x + g y'
pego parse -g apply.pego -s e -f sexpr -i 'f -1'
pego parse -g apply.pego -s e -f sexpr -i 'f (-1)'
```

```text
(App Arg=Id"y"@id Fn=(App Arg=Id"x"@id Fn=Id"f"@id))
(Bin L=(App Arg=Id"x"@id Fn=Id"f"@id) Op="+" R=(App Arg=Id"y"@id Fn=Id"g"@id))
(Bin L=Id"f"@id Op="-" R=Num"1"@num)
(App Arg=(Neg X=Num"1"@num) Fn=Id"f"@id)
```

The last two lines show the catch of juxtaposition: `f -1` is a subtraction, because at that position both `-` and
the empty operator match and the longer one wins (see [pitfalls](#8-pitfalls)). The argument has to be parenthesized.

**Prefix and postfix in the same level.** The right operand of a prefix operator contains only *tighter* levels, so a
postfix operator of the same level is applied to the whole prefix expression: `-3!` is `(-3)!`. Put the postfix operator
in a tighter level if you want `-(3!)`.

```pego
// same-level.pego
type Num terminal
type Neg struct { X E }
type Fact struct { X E }
type E = Num | Neg | Fact

def same: E = pratt {
    operand num
    level {
        prefix "-"  -> new Neg{X: $rhs}
        postfix "!" -> new Fact{X: $lhs}
    }
}

def tighter: E = pratt {
    operand num
    level { prefix "-"  -> new Neg{X: $rhs} }
    level { postfix "!" -> new Fact{X: $lhs} }
}
def num: Num = (?0-9)+
```

```bash
pego parse -g same-level.pego -s same -f sexpr -i '-3!'
pego parse -g same-level.pego -s tighter -f sexpr -i '-3!'
```

```text
(Fact X=(Neg X=Num"3"@num))
(Neg X=(Fact X=Num"3"@num))
```

## 6. The tree of a Pratt expression

### Typed rules and the `Operator` node

When an operator has no action, it produces a node of the reserved type `Operator`. Its children are `[lhs, op, rhs]`
for infix, `[op, rhs]` for prefix and `[lhs, op]` for postfix operators, its label is the rule, and its `operator` field
is the index of the operator in declaration order across all levels. Operands without an action appear unchanged:

```pego
// raw.pego
def e = pratt {
    operand @(?0-9)+
    level { infix left "+" }
    level { infix left "*" }
    level { prefix "-" }
    level { postfix "!" }
}
```

```bash
pego parse -g raw.pego -s e -f sexpr -i '-1+2*3!'
pego parse -g raw.pego -s e -f sexpr -i '1+2+3'
```

```text
(Operator (Operator "-" "1"@e operator=2)@e "+" (Operator "2"@e "*" (Operator "3"@e "!" operator=3)@e operator=1)@e operator=0)@e
(Operator (Operator "1"@e "+" "2"@e operator=0)@e "+" "3"@e operator=0)@e
```

Here `+` is operator 0, `*` is 1, `-` is 2 and `!` is 3; the input is `(-1) + (2 * (3!))`. The tree is as deep as the
expression, not as deep as the number of levels. Compare the tree of a rule chain without actions, where even a bare
number passes through every level:

```pego
// chain-cst.pego
def expr = term (("+" / "-") term)*
def term = factor (("*" / "/") factor)*
def factor = @(?0-9)+
```

```bash
pego parse -g chain-cst.pego -s expr -f sexpr -i '7'
pego parse -g raw.pego -s e -f sexpr -i '7'
```

```text
(Seq (Seq "7"@factor [])@term [])@expr
"7"@e
```

The default Pratt tree is a good starting point (nothing to write), but note the consequence for typed rules: if the
rule has a declared type that does not include `Operator`, **every operator needs an action**:

```pego
// raw-typed.pego
type Num terminal
def t: Num = pratt {
    operand num
    level { infix left "+" }
}
def num: Num = (?0-9)+
```

```bash
pego parse -g raw-typed.pego -s t -i '1+2'
```

```text
pego: raw-typed.pego:3:1: operators of t without an action produce Operator, which is not assignable to Num
```

### `$n` and operators

Operator actions cannot use `$1`, `$2`, ...; use the captures in the operator part or `$lhs`, `$rhs`, `$op`. Operand
actions can use `$n`:

```pego
// operand-n.pego
type Num terminal
type Paren struct { X E }
type E = Num | Paren

def e: E = pratt {
    operand num
    operand "(" e ")" -> new Paren{X: $2}
}
def num: Num = (?0-9)+
```

```bash
pego parse -g operand-n.pego -s e -f sexpr -i '((1))'
```

```text
(Paren X=(Paren X=Num"1"@num))
```

## 7. Choosing between the three

| | Pratt expression | Left-recursive rules | Precedence chain |
|:--|:--|:--|:--|
| Adding a level | One `level` line | Two rules and an edit to a neighbour | A rule and an edit to a neighbour |
| Associativity | Declared (`left`, `right`, `none`) | Shape of the recursion | Repetition + `foldl`, or recursion |
| Prefix, postfix, mixfix, ternary, calls | Operators in a level | Extra alternatives per level | Extra alternatives per level |
| Operator selection | **Longest match**, then declaration order | First match in the ordered choice | First match in the ordered choice |
| Whitespace | `skip`, hidden from error messages | By hand in every rule | By hand in every rule |
| Tree depth without actions | Proportional to the expression | One node per level for every operand | One node per level for every operand |
| Rules callable for a sub-level | `expr(level)` | Each level is a rule | Each level is a rule |
| Speed (20,000-term expression) | 41.5 ms | 79.9 ms | not in the benchmarks |
| Needs left recursion support | No | Yes | No |

The speed row is from [benchmarks.md](../benchmarks.md) (closure backend, 133 KB input): the Pratt calculator is about
twice as fast as the left-recursive one, as you would expect from a loop that does not make one rule call per level
for every operand and does not grow seeds.

A rule of thumb:

- **Use a Pratt expression** for any language with more than two or three precedence levels, for prefix, postfix,
  ternary or call/index operators, or when you expect to add operators later. It is the default for expression
  languages, and what the larger examples (`minilang`, `golang`, `python`) use.
- **Use left-recursive rules** when the recursion is not an operator table: member chains (`a.b.c`), a handful of
  levels in an otherwise ordinary grammar, constructs such as `list = list "," item / item`, or a grammar ported from a
  notation that already uses left recursion. They also work with Pratt expressions in the same grammar.
- **Use the chain** when you need plain PEG that other tools can read, or for a one- or two-level expression where a
  fold is the shortest thing to write.

The three can be mixed freely. A Pratt expression's operands and operator parts are normal parsing expressions, so they
can call any rule; and the rules of a statement grammar call the rule that holds the Pratt expression like any other.

## 8. Pitfalls

**The word `operand` (and `level`, `skip`, `prefix`, `postfix`, `infix`, `pratt`) is a keyword.** A rule called
`operand` is a syntax error; pick another name such as `atom`. The complete list is in
[Overview](../../spec/overview.md#identifiers-and-keywords).

**An operand cannot call its own rule at the start.** It would be left recursion inside the loop. The compiler says so:

```pego
// operand-lr.pego
def e = pratt {
    operand e "!"
    operand (?0-9)+
    level { infix left "+" }
}
```

```bash
pego parse -g operand-lr.pego -s e -i '1'
```

```text
pego: operand-lr.pego:2:9: operand of e calls e at its start (left recursion); use infix or postfix instead
```

Write `!` as a postfix operator instead.

**`skip` is not inserted inside an operator part or an operand, and trailing whitespace is not consumed.** In
`infix right "?" t:expr ":"` the `":"` must directly follow the end of `t`; write `ws` before it. Otherwise the operator
part fails, the operator is not applied, and the error is reported at the farthest failure (below, the colon at column 7,
which no operator can continue). After the last token the Pratt expression stops, so the rule that uses it must
consume trailing whitespace (the `main` rules of the calculators say `ws $$`).

```pego
// skip.pego
type Num terminal
type Id terminal
type Cond struct { C E, T E, F E }
type E = Num | Id | Cond

def loose: E = pratt {
    skip ws
    operand num
    operand id
    level { infix right "?" t:loose ":" -> new Cond{C: $lhs, T: $t, F: $rhs} }
}
def tight: E = pratt {
    skip ws
    operand num
    operand id
    level { infix right "?" t:tight ws ":" -> new Cond{C: $lhs, T: $t, F: $rhs} }
}
def num: Num = (?0-9)+
def id: Id = (?a-z)+
def ws = " "*
```

```bash
pego parse -g skip.pego -s loose -f sexpr -i 'a ? b : c'
pego parse -g skip.pego -s loose -f sexpr -i 'a ? b: c'
pego parse -g skip.pego -s tight -f sexpr -i 'a ? b : c'
```

```text
pego: 1:7: syntax error: expected "?"
(Cond C=Id"a"@id F=Id"c"@id T=Id"b"@id)
(Cond C=Id"a"@id F=Id"c"@id T=Id"b"@id)
```

**Operators are chosen by longest match, before precedence is considered.** At each position the parser picks, among
the operator parts that match, the longest one; only then does it check whether the operator can be applied at the
current level. If it cannot, the parser does *not* fall back to a shorter operator. This is the behavior of a
tokenizer's maximal munch, and it has two consequences.

First, the order of the operators does not matter for correctness. `"<"` and `"<="` can be in different levels, in any
order, and `1 <= 2` parses as expected (in rule-based grammars `"<" / "<="` is the bug shown in section 2).

Second, `a ++b` is a postfix `++` followed by an unexpected `b`, and not `a + (+b)`, even though both are
syntactically possible:

```pego
// munch.pego
type Num terminal
type Bin struct { L E, Op Match, R E }
type Un struct { Op Match, X E }
type Post struct { X E, Op Match }
type E = Num | Bin | Un | Post

def main: E = e:e $$ -> $e

def e: E = pratt {
    skip ws
    operand num
    level { infix left "+" -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { prefix "+" -> new Un{Op: $op, X: $rhs} }
    level { postfix "++" -> new Post{X: $lhs, Op: $op} }
}
def num: Num = (?0-9)+
def ws = " "*
```

```bash
pego parse -g munch.pego -f sexpr -i '1 + +2'
pego parse -g munch.pego -f sexpr -i '1 ++2'
pego parse -g munch.pego -f sexpr -i '1 ++'
```

```text
(Bin L=Num"1"@num Op="+" R=(Un Op="+" X=Num"2"@num))
pego: 1:5: syntax error: expected "+", "++", end of input
(Post Op="++" X=Num"1"@num)
```

As in C, write `1 + +2` with a space between the operators.

**Word operators need a word boundary.** `"and"` also matches the start of `android`. Check the boundary in the
operator part with a negative lookahead, and exclude the keywords from identifiers:

```pego
// words.pego
type Id terminal
type Bin struct { L E, Op Match, R E }
type Not struct { X E }
type E = Id | Bin | Not

def e: E = pratt {
    skip ws
    operand id
    level { infix left "or" !idchar -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix left "and" !idchar -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { prefix "not" !idchar -> new Not{X: $rhs} }
}
def id: Id = !kw (?a-z)+
def kw = ("or" / "and" / "not") !idchar
def idchar = (?a-z)
def ws = " "*
```

```bash
pego parse -g words.pego -s e -f sexpr -i 'a or b and not c'
pego parse -g words.pego -s e -f sexpr -i 'notable or a'
```

```text
(Bin L=Id"a"@id Op="or" R=(Bin L=Id"b"@id Op="and" R=(Not X=Id"c"@id)))
(Bin L=Id"notable"@id Op="or" R=Id"a"@id)
```

**A matched operator whose right operand fails is not applied, unless you cut.** The parser backs up to before the
operator and ends the expression there, leaving the operator for the enclosing rule. This is usually right (the outer
rule reports the error at the farthest failure), but if the same text can also continue differently, the expression
quietly ends early. A cut (`--`) in the operator part commits to the operator:

```pego
// cut.pego
type Num terminal
type Bin struct { L E, Op Match, R E }
type E = Num | Bin

def plain: E = pratt {
    skip ws
    operand num
    level { infix left "+" -> new Bin{L: $lhs, Op: $op, R: $rhs} }
}
def committed: E = pratt {
    skip ws
    operand num
    level { infix left "+" -- -> new Bin{L: $lhs, Op: $op, R: $rhs} }
}

def m1 = plain ws "+" ws "x" $$
def m2 = committed ws "+" ws "x" $$
def num: Num = (?0-9)+
def ws = " "*
```

```bash
pego parse -g cut.pego -s m1 -f sexpr -i '1 + x'
pego parse -g cut.pego -s m2 -f sexpr -i '1 + x'
```

```text
(Seq Num"1"@num [" "]@ws "+" [" "]@ws "x")@m1
pego: 1:5: syntax error: expected (?0-9)
```

Without the cut, `1 + x` is the number `1` followed by the literal text `+ x`; with it, the `+` is an operator and the
missing operand is an error.

**Left recursion needs the base case last.** See section 2. If a left-recursive rule matches less than you expect,
check the order of its alternatives first.

## 9. A larger example: minilang

[`examples/minilang`](../../examples/minilang/minilang.pego) is a small language where statements are ordinary PEG rules
and expressions are one Pratt expression, nested in each other. The pieces that matter for this guide:

```pego
def expr: Expr = e:operation #error(message="expected an expression") -> $e

def operation: Expr = pratt {
    skip ws
    operand number
    operand string
    operand bool
    operand ident
    operand "(" e:operation ws ")" -> $e
    operand "[" xs:args? ws "]" -> new ArrayLit{Elems: concat($xs)}
    level { infix right "?" t:operation ws ":" -> new Cond{Cond: $lhs, Then: $t, Else: $rhs} }
    level { infix left "||" -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix left "&&" -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix none "==" / "!=" / "<=" / ">=" / "<" / ">" -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix left "+" / "-" -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix left "*" / "/" / "%" -> new Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level { prefix "-" / "!" -> new Unary{Op: $op, X: $rhs} }
    level {
        postfix "(" xs:args? ws ")" -> new Call{Fn: $lhs, Args: concat($xs)}
        postfix "[" i:operation ws "]" -> new Index{X: $lhs, Index: $i}
        postfix "." ws n:name -> new Member{X: $lhs, Name: $n}
    }
}
def args = first:expr rest:(-ws -"," x:expr)* -> concat(list($first), map($rest, (r) => $r.x))
```

(The grammar is abridged here; the types and the lexical rules are in the file.)

- The Pratt rule is `operation`; the rule that statements call is `expr`, a thin wrapper that attaches an `#error`
  message to the whole expression. Inside the Pratt expression, nested expressions call `operation` directly, so the
  wrapper's message applies only where a whole expression is expected.
- The language has no comma operator, so call arguments are plain `expr` calls and no level name is needed.
- Statements such as `let` and `while` are PEG rules that call `expr`. Together with the `#recover` attribute on `stmt`,
  a bad expression only loses its own statement. See [Errors and recovery](errors-and-recovery.md).

Parsing a few statements with the real grammar:

```bash
pego parse -g examples/minilang/minilang.pego -f sexpr -i 'let x = a + b * c;'
pego parse -g examples/minilang/minilang.pego -f sexpr -i 'f(1)(2)[3].x;'
pego parse -g examples/minilang/minilang.pego -f sexpr -i 'let z = -f(x) * 2;'
```

```text
(Program Body=[(Let Name=Ident"x"@ident Value=(Binary Left=Ident"a"@ident Op="+" Right=(Binary Left=Ident"b"@ident Op="*" Right=Ident"c"@ident)))])
(Program Body=[(ExprStmt X=(Member Name=Ident"x"@ident X=(Index Index=Number"3"@number X=(Call Args=[Number"2"@number] Fn=(Call Args=[Number"1"@number] Fn=Ident"f"@ident)))))])
(Program Body=[(Let Name=Ident"z"@ident Value=(Binary Left=(Unary Op="-" X=(Call Args=[Ident"x"@ident] Fn=Ident"f"@ident)) Op="*" Right=Number"2"@number))])
```
