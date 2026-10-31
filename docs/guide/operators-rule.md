# Operators Rule

Expressions with operators (`1 + 2 * 3`, `-x ** 2`, `f(a)[i].name`, `c ? a : b`) are where a grammar needs
precedence and associativity. ZeroParse gives you three ways to write them:

| Technique | You write | Typical size |
|:--|:--|:--|
| **Operators Rule** (`rule expr = #operators { ... }`) | A table: operands, then one `level` per precedence level, each with its operators | One rule |
| **Left-recursive rules** | One rule per level, each of the form `level = level OP next / next` | One rule per level |
| **Precedence chain** | One rule per level, each a repetition `next (OP next)*` folded with `foldl` | One rule per level |

This guide builds the same calculator in all three styles, compares the trees and error messages they produce, and
helps you choose. It relies on [Shaping Trees](shaping-trees.md), [Declaring Types](types.md) and [Actions](actions.md) for captures, types and `foldl`; the normative
text is in [Operators Rule](../../spec/language/operators-rule.md) and
[Left recursion](../../spec/language/parser-expressions.md#left-recursion), and the reasoning behind the design in
[design record 005](../design/records/005-operators-rule.md) and [design record 003](../design/records/003-packrat-parsing.md).

To follow along, put each grammar in the file named on its first line and run the commands shown (`zp` is the tool
from `go install github.com/ornew/zeroparse/cmd/zp@latest`, or `go run ./cmd/zp` in a checkout). The Operators Rule and the
left-recursive calculator are the files `calc.zp` and `calc_lr.zp` in
[`examples/calculator`](../../examples/calculator) (without their header comments); the chain variant is written out
below.

## An Operators Rule Calculator

```zp
// calc.zp
package calc


type Binary = {
    Left  Expr
    Op    node[string]
    Right Expr
}

type Unary = {
    Op node[string]
    X  Expr
}

type Expr ~ node[string] | Binary | Unary

@start
rule main Expr = e:expr ws $$ => $e

rule expr Expr = #operators {
    skip ws
    operand number
    operand "(" e:expr ws ")" => $e
    level additive { infix left "+" / "-" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level multiplicative { infix left "*" / "/" / "%" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level prefix { prefix "-" / "+" => Unary{Op: $op, X: $rhs} }
    level power { infix right "^" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
}

rule number = ([0-9]+ ("." [0-9]+)?) #atomic
rule ws = [ \t\r\n]*
```

```bash
zp parse -g calc.zp -f sexpr -i '1 + 2 * 3'
zp parse -g calc.zp -f sexpr -i '1 - 2 - 3'
zp parse -g calc.zp -f sexpr -i '2 ^ 3 ^ 2'
zp parse -g calc.zp -f sexpr -i '-2 ^ 2'
zp parse -g calc.zp -f sexpr -i '2 ^ -1'
zp parse -g calc.zp -f sexpr -i '(1 + 2) * 3'
```

```text
Binary{Left: "1", Op: "+", Right: Binary{Left: "2", Op: "*", Right: "3"}}
Binary{Left: Binary{Left: "1", Op: "-", Right: "2"}, Op: "-", Right: "3"}
Binary{Left: "2", Op: "^", Right: Binary{Left: "3", Op: "^", Right: "2"}}
Unary{Op: "-", X: Binary{Left: "2", Op: "^", Right: "2"}}
Binary{Left: "2", Op: "^", Right: Unary{Op: "-", X: "1"}}
Binary{Left: Binary{Left: "1", Op: "+", Right: "2"}, Op: "*", Right: "3"}
```

Here is how to read an Operators Rule Declaration Statement.

- **`skip ws`** is inserted, with its result discarded, before every operand and every operator. It is how a grammar
  without a scanner says "tokens may be separated by whitespace". Without a `skip`, nothing is inserted.
- **`operand`** items describe what an expression can start with when no prefix operator applies: numbers and
  parenthesized expressions here. Several `operand` items are tried in order, like an ordered choice. An action belongs
  to its own item, so alternatives that need different actions are separate items (`operand "(" e:expr ws ")" => $e`
  unwraps the parentheses). An operand cannot start by calling its own rule: that would be left recursion, and it is a
  compile error. Constructs that take an expression on their left (calls, indexing) are operators.
- **`level name { ... }`** declares one binding level. **Levels are listed from the loosest to the tightest**, so inserting a
  level is a one-line change and nothing else has to be renumbered. All operators of a level share a precedence, and a
  level may mix prefix, infix and postfix operators.
- **`infix left | right | none`** gives the associativity: `1 - 2 - 3` is `(1 - 2) - 3`, `2 ^ 3 ^ 2` is `2 ^ (3 ^ 2)`,
  and `none` operators do not chain ([Operator Forms and Associativity](#operator-forms-and-associativity)).
- **An operator part** (`"+" / "-"`) is an ordinary Rule Expression. It may be several tokens, call other rules, or
  contain captures ([Level-Restricted Calls](#level-restricted-calls)).
- **Actions** see `$lhs`, `$rhs` (the operands) and `$op` (the value of the operator part: a string node for a plain token, a tuple if the part is a sequence). In
  a rule with a declared type, `$lhs` and `$rhs` have that type, which is what lets the `Binary` fields be `Expr`.

The unary minus is declared in a level **tighter than `*` but looser than `^`**: `-2 ^ 2` is `-(2 ^ 2)`, while `2 ^ -1`
still parses, because a prefix operator at the start of an operand position is not restricted by the levels.

If the input ends where an operand is required, the error lists what could have followed, without the whitespace that
`skip` would have consumed (compare the next section):

```bash
zp parse -g calc.zp -f sexpr -i '1 +'
```

```text
zp: 1:4: syntax error: expected "(", "+", "-", [0-9]
```

## The Same Language With Left Recursion

A PEG rule may call itself in leftmost position, directly or through other rules. ZeroParse detects such cycles and parses
them by growing a seed: it first matches the non-recursive alternatives, then repeatedly tries again with the result so
far as the recursive call, as long as the match gets longer. The result is exactly what the grammar says in the textbook
notation, without any rewriting:

```zp
// calc_lr.zp
package calc


type Binary = {
    Left  Expr
    Op    node[string]
    Right Expr
}

type Unary = {
    Op node[string]
    X  Expr
}

type Expr ~ node[string] | Binary | Unary

@start
rule main Expr = e:expr ws $$ => $e

rule expr Expr = add / term
rule add Expr = l:expr ws op:("+" / "-") #atomic r:term => Binary{Left: $l, Op: $op, Right: $r}

rule term Expr = mul / unary
rule mul Expr = l:term ws op:("*" / "/" / "%") #atomic r:unary => Binary{Left: $l, Op: $op, Right: $r}

rule unary Expr = neg / power
rule neg Expr = ws op:("-" / "+") #atomic x:unary => Unary{Op: $op, X: $x}

// Exponentiation is right-associative, and its right operand may carry a unary operator (2^-1).
rule power Expr = pow / primary
rule pow Expr = l:primary ws op:"^" r:unary => Binary{Left: $l, Op: $op, Right: $r}

rule primary Expr = ws p:(number / group) => $p
rule group Expr = "(" e:expr ws ")" => $e

rule number = ([0-9]+ ("." [0-9]+)?) #atomic
rule ws = [ \t\r\n]*
```

```bash
zp parse -g calc_lr.zp -f sexpr -i '1 + 2 * 3'
zp parse -g calc_lr.zp -f sexpr -i '1 - 2 - 3'
zp parse -g calc_lr.zp -f sexpr -i '2 ^ 3 ^ 2'
zp parse -g calc_lr.zp -f sexpr -i '-2 ^ 2'
zp parse -g calc_lr.zp -f sexpr -i '2 ^ -1'
zp parse -g calc_lr.zp -f sexpr -i '(1 + 2) * 3'
```

```text
Binary{Left: "1", Op: "+", Right: Binary{Left: "2", Op: "*", Right: "3"}}
Binary{Left: Binary{Left: "1", Op: "-", Right: "2"}, Op: "-", Right: "3"}
Binary{Left: "2", Op: "^", Right: Binary{Left: "3", Op: "^", Right: "2"}}
Unary{Op: "-", X: Binary{Left: "2", Op: "^", Right: "2"}}
Binary{Left: "2", Op: "^", Right: Unary{Op: "-", X: "1"}}
Binary{Left: Binary{Left: "1", Op: "+", Right: "2"}, Op: "*", Right: "3"}
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
  grammar need exactly that (an Operators Rule offers the same with
  [level-restricted calls](#level-restricted-calls)).

```bash
zp parse -g calc_lr.zp -f sexpr -i '1 +'
```

```text
zp: 1:4: syntax error: expected "(", "+", "-", [ \t\r\n], [0-9]
```

Note the extra `[ \t\r\n]` in the expected list: a rule-based grammar reports its whitespace, while `skip` is hidden
from error messages.

### Left Recursion Pitfalls

**The recursive alternative must come first.** Growing starts from the non-recursive match and then tries the
alternatives *in order* with the recursive call bound to the result so far. If the base case comes first, it matches
again with the same length, nothing grows, and the rest of the input is left over:

```zp
// lr-order.zp
@start(default)
rule good = expr $$
rule expr = expr "+" term / term
rule term = [0-9]+ #atomic

@start
rule bad = expr2 $$
rule expr2 = term / expr2 "+" term
```

```bash
zp parse -g lr-order.zp -s good -f sexpr -i '1+2+3'
zp parse -g lr-order.zp -s bad -f sexpr -i '1+2+3'
```

```text
((("1" "+" "2") "+" "3"))
zp: 1:2: syntax error: expected end of input, [0-9]
```

The error ("expected a digit or the end of input at column 2") is accurate but gives no hint about the order of
alternatives; when a left-recursive rule stops short, look at the order first.

**Ordered choice picks the first operator that matches, not the longest.** In `op:("<" / "<=")` the `<` wins on `1<=2`
and `=2` is left over; write `"<=" / "<"` (or put `!"="` after `"<"`). This is the usual PEG rule, and you have to
follow it in every operator list. An Operators Rule does not have the problem ([Pitfalls](#pitfalls)).

```zp
// lr-ops.zp
@start(default)
rule rel = l:num op:("<" / "<=") r:num
@start
rule rel2 = l:num op:("<=" / "<") r:num
rule num = [0-9]+ #atomic
```

```bash
zp parse -g lr-ops.zp -s rel -f sexpr -i '1<=2'
zp parse -g lr-ops.zp -s rel2 -f sexpr -i '1<=2'
```

```text
zp: 1:3: syntax error: expected [0-9]
("1" "<=" "2")
```

## The Precedence Chain

The oldest way to write an expression grammar needs neither left recursion nor Operators Rule. Each level matches an operand of
the next level, followed by any number of operator-operand pairs, and `foldl` builds the left-leaning tree
([the `foldl` recipe](actions.md#left-associative-trees-with-foldl)):

```zp
// chain.zp
type Binary = { Left Expr; Op node[string]; Right Expr }
type Unary = { Op node[string]; X Expr }
type Expr ~ node[string] | Binary | Unary

@start
rule main Expr = e:expr ws $$ => $e

rule expr Expr = l:term rest:(ws op:("+" / "-") #atomic r:term)* => foldl($l, $rest, (acc, i) => Binary{Left: acc, Op: i.$op, Right: i.$r})

rule term Expr = l:unary rest:(ws op:("*" / "/" / "%") #atomic r:unary)* => foldl($l, $rest, (acc, i) => Binary{Left: acc, Op: i.$op, Right: i.$r})

rule unary Expr = neg / power
rule neg Expr = ws op:("-" / "+") #atomic x:unary => Unary{Op: $op, X: $x}

// right-associative: the right operand is parsed by the rule that includes the operator itself
rule power Expr = pow / primary
rule pow Expr = l:primary ws op:"^" r:unary => Binary{Left: $l, Op: $op, Right: $r}

rule primary Expr = ws p:(number / group) => $p
rule group Expr = "(" e:expr ws ")" => $e
rule number = ([0-9]+ ("." [0-9]+)?) #atomic
rule ws = [ \t\r\n]*
```

```bash
zp parse -g chain.zp -f sexpr -i '1 - 2 - 3'
zp parse -g chain.zp -f sexpr -i '2 ^ 3 ^ 2'
zp parse -g chain.zp -f sexpr -i '-2 ^ 2'
```

```text
Binary{Left: Binary{Left: "1", Op: "-", Right: "2"}, Op: "-", Right: "3"}
Binary{Left: "2", Op: "^", Right: Binary{Left: "3", Op: "^", Right: "2"}}
Unary{Op: "-", X: Binary{Left: "2", Op: "^", Right: "2"}}
```

Left-associative levels use the repetition and `foldl`; right-associative ones recurse on the right. It works in any
PEG tool, and the only machinery it uses is ordered choice and repetition. Its costs are the same as for rules: one rule
per level, whitespace handled by hand, and one rule call per level for every operand, even a bare number.

**Non-associative operators.** Replace the repetition with `#repeat(0,1)`: it yields a list with at most one element, so
`foldl` handles it unchanged, and a second operator is simply not consumed:

```zp
// chain-cmp.zp
type Bin = { L E; Op node[string]; R E }
type E ~ node[string] | Bin

@start
rule main E = e:cmp ws $$ => $e

// at most one comparison: "1 < 2 < 3" is a syntax error
rule cmp E = l:add rest:(ws op:("<=" / ">=" / "<" / ">" / "==" / "!=") #atomic r:add) #repeat(0, 1) => foldl($l, $rest, (acc, i) => Bin{L: acc, Op: i.$op, R: i.$r})

rule add E = l:atom rest:(ws op:("+" / "-") #atomic r:atom)* => foldl($l, $rest, (acc, i) => Bin{L: acc, Op: i.$op, R: i.$r})

rule atom E = ws n:num => $n
rule num = [0-9]+ #atomic
rule ws = " "*
```

```bash
zp parse -g chain-cmp.zp -f sexpr -i '1 + 2 <= 3'
zp parse -g chain-cmp.zp -f sexpr -i '1 < 2 < 3'
```

```text
Bin{L: Bin{L: "1", Op: "+", R: "2"}, Op: "<=", R: "3"}
zp: 1:7: syntax error: expected end of input, " ", "+", "-"
```

## Level-Restricted Calls

Some constructs contain an expression that must not use the loosest operators. The classic example is the argument list
of a call: in `f(a, b)` the comma separates arguments, so the arguments must be parsed *without* the comma operator,
while `(a, b)` in parentheses is a comma expression. An Operators Rule handles this with a **named level** and a call
with that level, `level(expr, assignment)`: its tail applies only infix and postfix operators of the named level and tighter
ones. A call without a level (`expr`) allows tail operators of all levels. The rule name and `(` must be adjacent.

Prefix operators can still start an operand at any level, including the head of a restricted call. A selected prefix
parses its right operand using its own precedence, so a looser prefix can absorb operators below the caller's entry
level inside that operand. For example, with prefix `~` looser than addition, `level(expr, multiplication)` accepts `~x+y`
as `~(x+y)`, but stops before `+` in bare `x+y`. A named level does not exclude those operators from every subtree.
If a construct forbids a prefix at entry, guard its input explicitly, accounting for the rule's whitespace skip.

The grammar below is the one from the specification: it has every kind of operator.

```zp
// js.zp
type Comma = { L Expr; R Expr }
type Assign = { L Expr; R Expr }
type Cond = { Cond Expr; Then Expr; Else Expr }
type Bin = { L Expr; Op node[string]; R Expr }
type Unary = { Op node[string]; X Expr }
type Call = { Fn Expr; Args []Expr }
type Index = { X Expr; Index Expr }
type Member = { X Expr; Name node[string] }
type Expr ~ node[string] | Comma | Assign | Cond | Bin | Unary | Call | Index | Member

@start
rule expr Expr = #operators {
    skip ws
    operand number
    operand ident
    operand "(" e:expr ws ")" => $e

    level comma { infix left "," => Comma{L: $lhs, R: $rhs} }
    level assignment { infix right "=" => Assign{L: $lhs, R: $rhs} }
    level conditional { infix right "?" then:expr ws ":" => Cond{Cond: $lhs, Then: $then, Else: $rhs} }
    level equality { infix none "==" / "!=" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level additive { infix left "+" / "-" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level multiplicative { infix left "*" / "/" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level prefix { prefix "-" / "!" => Unary{Op: $op, X: $rhs} }
    level power { infix right "**" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level postfix {
        postfix "(" xs:args? ws ")" => Call{Fn: $lhs, Args: concat($xs)}
        postfix "[" i:expr ws "]" => Index{X: $lhs, Index: $i}
        postfix "." name:ident => Member{X: $lhs, Name: $name}
    }
}

rule args = first:level(expr, assignment) rest:(-ws "," x:level(expr, assignment))* => concat(list($first), map($rest, (r) => r.$x))

rule number = [0-9]+ #atomic
rule ident = [a-z]+ #atomic
rule ws = [ \t\r\n]*
```

```bash
zp parse -g js.zp -s expr -f sexpr -i 'x, y'
zp parse -g js.zp -s expr -f sexpr -i 'f(a = 1, b)(2)[3].x'
zp parse -g js.zp -s expr -f sexpr -i 'a = b = c'
zp parse -g js.zp -s expr -f sexpr -i 'a ? b : c ? d : e'
zp parse -g js.zp -s expr -f sexpr -i '-a ** b'
zp parse -g js.zp -s expr -f sexpr -i '2 ** -1'
```

```text
Comma{L: "x", R: "y"}
Member{Name: "x", X: Index{Index: "3", X: Call{Args: ["2"], Fn: Call{Args: [Assign{L: "a", R: "1"} "b"], Fn: "f"}}}}
Assign{L: "a", R: Assign{L: "b", R: "c"}}
Cond{Cond: "a", Else: Cond{Cond: "c", Else: "e", Then: "d"}, Then: "b"}
Unary{Op: "-", X: Bin{L: "a", Op: "**", R: "b"}}
Bin{L: "2", Op: "**", R: Unary{Op: "-", X: "1"}}
```

Things to notice:

- `f(a = 1, b)` has two arguments: `args` calls `level(expr, assignment)`, which includes the `=` level and everything tighter
  but not `,`. Outside a call, `x, y` is a `Comma` node.
- The postfix operators `(...)`, `[...]` and `.name` share one level, so `f(a)(2)[3].x` chains left to right. Their
  operator parts are arbitrary Rule Expressions: `"(" xs:args? ws ")"` parses an argument list with the rest of the
  grammar, and the whole matched part is `$op` (here the actions use the captures instead).
- `"?" then:expr ws ":"` is a mixfix operator: the middle operand is parsed by a full `expr` call, and the right operand
  by the Operators Rule loop, which makes `a ? b : c ? d : e` nest to the right.
- `-a ** b` is `-(a ** b)` because `**` is in a tighter level than the prefix `-`, and `2 ** -1` parses anyway.
- `skip` is **not** inserted inside an operator part, so `"?" then:expr ws ":"` spells out the whitespace before `":"`.
  See [pitfalls](#pitfalls).

## Operator Forms and Associativity

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
zp parse -g js.zp -s expr -f sexpr -i 'a == b == c'
```

```text
zp: 1:8: syntax error: expected "(", "*", "**", "+", ",", "-", ".", "/", "?", "["
```

The reported position is the second `==`, and the list of expected tokens contains neither `==` nor `!=`, which is the
accurate message for a chain: the expression stops after `a == b`, and the start rule then reports the leftover input.

**Juxtaposition.** An infix operator may match the empty string, which is how application is written. It is a normal
level, so you choose how tightly it binds:

```zp
// apply.zp
type Bin = { L E; Op node[string]; R E }
type App = { Fn E; Arg E }
type Neg = { X E }
type E ~ node[string] | Bin | App | Neg

@start
rule e E = #operators {
    skip ws
    operand num
    operand id
    operand "(" x:e ws ")" => $x
    level additive { infix left "+" / "-" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level application { infix left _ => App{Fn: $lhs, Arg: $rhs} }
    level prefix { prefix "-" => Neg{X: $rhs} }
}
rule num = [0-9]+ #atomic
rule id = [a-z]+ #atomic
rule ws = " "*
```

```bash
zp parse -g apply.zp -s e -f sexpr -i 'f x y'
zp parse -g apply.zp -s e -f sexpr -i 'f x + g y'
zp parse -g apply.zp -s e -f sexpr -i 'f -1'
zp parse -g apply.zp -s e -f sexpr -i 'f (-1)'
```

```text
App{Arg: "y", Fn: App{Arg: "x", Fn: "f"}}
Bin{L: App{Arg: "x", Fn: "f"}, Op: "+", R: App{Arg: "y", Fn: "g"}}
Bin{L: "f", Op: "-", R: "1"}
App{Arg: Neg{X: "1"}, Fn: "f"}
```

The last two lines show the catch of juxtaposition: `f -1` is a subtraction, because at that position both `-` and
the empty operator match and the longer one wins (see [pitfalls](#pitfalls)). The argument has to be parenthesized.

**Prefix and postfix in the same level.** The right operand of a prefix operator contains only *tighter* levels, so a
postfix operator of the same level is applied to the whole prefix expression: `-3!` is `(-3)!`. Put the postfix operator
in a tighter level if you want `-(3!)`.

```zp
// same-level.zp
type Neg = { X E }
type Fact = { X E }
type E ~ node[string] | Neg | Fact

@start(default)
rule same E = #operators {
    operand num
    level mixed {
        prefix "-" => Neg{X: $rhs}
        postfix "!" => Fact{X: $lhs}
    }
}

@start
rule tighter E = #operators {
    operand num
    level prefix { prefix "-" => Neg{X: $rhs} }
    level postfix { postfix "!" => Fact{X: $lhs} }
}
rule num = [0-9]+ #atomic
```

```bash
zp parse -g same-level.zp -s same -f sexpr -i '-3!'
zp parse -g same-level.zp -s tighter -f sexpr -i '-3!'
```

```text
Fact{X: Neg{X: "3"}}
Neg{X: Fact{X: "3"}}
```

## The Tree of an Operators Rule

### Operators Without Actions

When an operator has no action, it produces a tuple of its parts: `(lhs, op, rhs)` for infix, `(op, rhs)` for prefix and
`(lhs, op)` for postfix operators. Operands without an action appear unchanged. With source information, the tuple
carries the rule name and the captures `lhs`, `op` and `rhs`:

```zp
// raw.zp
@start
rule e = #operators {
    operand [0-9]+ #atomic
    level additive { infix left "+" }
    level multiplicative { infix left "*" }
    level prefix { prefix "-" }
    level postfix { postfix "!" }
}
```

```bash
zp parse -g raw.zp -s e -f sexpr -i '-1+2*3!'
zp parse -g raw.zp -s e -f sexpr -i '1+2+3'
```

```text
(("-" "1") "+" ("2" "*" ("3" "!")))
(("1" "+" "2") "+" "3")
```

The input is `(-1) + (2 * (3!))`. The tree is as deep as the
expression, not as deep as the number of levels. Compare the tree of a rule chain without actions, where even a bare
number passes through every level:

```zp
// chain-cst.zp
@start
rule expr = term (("+" / "-") term)*
rule term = factor (("*" / "/") factor)*
rule factor = [0-9]+ #atomic
```

```bash
zp parse -g chain-cst.zp -s expr -f sexpr -i '7'
zp parse -g raw.zp -s e -f sexpr -i '7'
```

```text
(("7" []) [])
"7"
```

The default Operators Rule tree is a good starting point (nothing to write), but note the consequence for typed rules: if the
rule has a declared struct type, **every operator needs an action**, because the tuple is not assignable to a struct. A
declared join that includes `node[_]` accepts the tuples, as `type E ~ node[_] | Bin` does:

```zp
// raw-typed.zp
type Bin = { L node[string]; Op node[string]; R node[string] }
@start
rule t Bin = #operators {
    operand num
    level additive { infix left "+" }
}
rule num = [0-9]+ #atomic
```

```bash
zp parse -g raw-typed.zp -s t -i '1+2'
```

```text
zp: raw-typed.zp:4:1: operatorsRule t produces node[string], which is not assignable to Bin
raw-typed.zp:4:1: operators of t without an action produce a tuple node, which is not assignable to Bin
```

A rule declared with exactly a named node type, as `type E = node[_]` and `rule t E = #operators { ... }`, converts every
operator node it builds to `E`, at every nesting level, with or without an action; the tuple above then has type `E`, and
typed output yields `*E` for each operator. Operands are not converted: each operand rule must produce `E` itself (declare
it `rule num E = ...`), or the Operators Rule must be declared as a join that includes the operand types.

### Captures and Operators

Operator actions refer to their operands as `$lhs`, `$rhs` and `$op`, and to the captures in the operator part. Operand
actions use the captures of their line:

```zp
// operand-captures.zp
type Paren = { X E }
type E ~ node[string] | Paren

@start
rule e E = #operators {
    operand num
    operand "(" e:e ")" => Paren{X: $e}
}
rule num = [0-9]+ #atomic
```

```bash
zp parse -g operand-captures.zp -s e -f sexpr -i '((1))'
```

```text
Paren{X: Paren{X: "1"}}
```

### A Whole-Rule Action

An action after the closing `}` computes the rule's value from the complete expression. It runs once per call of the
rule, after the last operator is applied, and reads the folded value as `$value`. Building the right operand of an
operator is part of the same call, so the action never sees an intermediate result:

```zp
// whole-action.zp
type Expr = { Root _ }

@start
rule e Expr = #operators {
    skip " "*
    operand [0-9]+ #atomic
    operand "(" x:e " "* ")" => $x
    level additive { infix left "+" }
} => Expr{Root: $value}
```

```bash
zp parse -g whole-action.zp -s e -f sexpr -i '1 + 2 + 3'
zp parse -g whole-action.zp -s e -f sexpr -i '(1 + 2) + 3'
```

```text
Expr{Root: (("1" "+" "2") "+" "3")}
Expr{Root: (Expr{Root: ("1" "+" "2")} "+" "3")}
```

Every call of the rule runs the action, including a call inside an operand or an operator part and a level-restricted
call. The parenthesized operand above calls `e`, so its value is wrapped too. To wrap an expression only once, put the
action on a separate rule that calls the Operators Rule.

`$value` exists only in the whole-rule action. An operand or operator action that uses it is a compile error, and so is
a capture named `value` in an operand or operator line, because the name is reserved for the folded value. Name such a
capture differently (`v:`, say).

The declared type is the type of the action's result. `$lhs`, `$rhs` and `$value` have the type of the fold, which is
inferred from the operand and operator lines. A struct made by the action covers the rule's whole match, including the
text that `skip` consumed before the first operand. With a named node type `N`, the action's result is converted to `N`
and the operators are not.

## Choosing Between the Three

| | Operators Rule | Left-recursive rules | Precedence chain |
|:--|:--|:--|:--|
| Adding a level | One `level` line | Two rules and an edit to a neighbour | A rule and an edit to a neighbour |
| Associativity | Declared (`left`, `right`, `none`) | Shape of the recursion | Repetition + `foldl`, or recursion |
| Prefix, postfix, mixfix, ternary, calls | Operators in a level | Extra alternatives per level | Extra alternatives per level |
| Operator selection | **Longest match**, then declaration order | First match in the ordered choice | First match in the ordered choice |
| Whitespace | `skip`, hidden from error messages | By hand in every rule | By hand in every rule |
| Tree depth without actions | Proportional to the expression | One node per level for every operand | One node per level for every operand |
| Rules callable for a sub-level | `level(expr, level)` | Each level is a rule | Each level is a rule |
| Speed (20,000-term expression) | 12.3 ms | 28.2 ms | not in the benchmarks |
| Needs left recursion support | No | Yes | No |

The speed row is from [benchmarks.md](../benchmarks.md) (closure backend, 133 KB input): the Operators Rule calculator is about
2.3 times as fast as the left-recursive one, as you would expect from a loop that does not make one rule call per level
for every operand and does not grow seeds.

A rule of thumb:

- **Use an Operators Rule** for any language with more than two or three precedence levels, for prefix, postfix,
  ternary or call/index operators, or when you expect to add operators later. It is the default for expression
  languages, and what the larger grammars (`examples/minilang`, `parsers/golang`, `parsers/python`) use.
- **Use left-recursive rules** when the recursion is not an operator table: member chains (`a.b.c`), a handful of
  levels in an otherwise ordinary grammar, constructs such as `list = list "," item / item`, or a grammar ported from a
  notation that already uses left recursion. They also work with Operators Rule Declaration Statements in the same grammar.
- **Use the chain** when you need plain PEG that other tools can read, or for a one- or two-level expression where a
  fold is the shortest thing to write.

The three can be mixed freely. An Operators Rule's operands and operator parts are normal Rule Expressions, so they
can call any rule; and the rules of a statement grammar call the rule that holds the Operators Rule like any other.

## Pitfalls

**The words `rule`, `operand`, `level`, `skip`, `prefix`, `postfix` and `infix` are contextual words.** A rule called
`operand` can be defined, but every reference to it is a syntax error (`expected an expression, found 'operand'`);
pick another name such as `atom`. The complete list is in
[Lexical Structure](../../spec/language/lexical.md#contextual-words).

**An operand cannot call its own rule at the start.** It would be left recursion inside the loop. The compiler says so:

```zp
// operand-lr.zp
@start
rule e = #operators {
    operand e "!"
    operand [0-9]+
    level additive { infix left "+" }
}
```

```bash
zp parse -g operand-lr.zp -s e -i '1'
```

```text
zp: operand-lr.zp:3:20: operand of e calls e at its start (left recursion); use infix or postfix instead
```

Write `!` as a postfix operator instead.

**A cut can prevent prefix-to-operand fallback.** If no prefix part matches and a candidate failed after `#cut`, the
Operators Rule fails: `prefix "-" #cut "z"` does not fall back to `operand "-x"` on `-x`. A mismatch before a cut still
allows fallback. The [parsing rules](../../spec/language/operators-rule.md#parsing-rules) define the scopes of candidates and
cuts.

**`skip` is not inserted inside an operator part or an operand, and trailing whitespace is not consumed.** In
`infix right "?" t:expr ":"` the `":"` must directly follow the end of `t`; write `ws` before it. Otherwise the operator
part fails, the operator is not applied, and the error is reported at the farthest failure (below, the colon at column 7,
which no operator can continue). After the last token the Operators Rule stops, so the rule that uses it must
consume trailing whitespace (the `main` rules of the calculators say `ws $$`).

```zp
// skip.zp
type Cond = { C E; T E; F E }
type E ~ node[string] | Cond

@start(default)
rule loose E = #operators {
    skip ws
    operand num
    operand id
    level conditional { infix right "?" t:loose ":" => Cond{C: $lhs, T: $t, F: $rhs} }
}
@start
rule tight E = #operators {
    skip ws
    operand num
    operand id
    level conditional { infix right "?" t:tight ws ":" => Cond{C: $lhs, T: $t, F: $rhs} }
}
rule num = [0-9]+ #atomic
rule id = [a-z]+ #atomic
rule ws = " "*
```

```bash
zp parse -g skip.zp -s loose -f sexpr -i 'a ? b : c'
zp parse -g skip.zp -s loose -f sexpr -i 'a ? b: c'
zp parse -g skip.zp -s tight -f sexpr -i 'a ? b : c'
```

```text
zp: 1:7: syntax error: expected "?"
Cond{C: "a", F: "c", T: "b"}
Cond{C: "a", F: "c", T: "b"}
```

**Operators are chosen by longest match, before precedence is considered.** At each position the parser picks, among
the operator parts that match, the longest one; only then does it check whether the operator can be applied at the
current level. If it cannot, the parser does *not* fall back to a shorter operator. This is the behavior of a
tokenizer's maximal munch, and it has two consequences.

First, the order of the operators does not matter for correctness. `"<"` and `"<="` can be in different levels, in any
order, and `1 <= 2` parses as expected (in rule-based grammars `"<" / "<="` is the bug shown in [The Same Language With Left Recursion](#the-same-language-with-left-recursion)).

Second, `a ++b` is a postfix `++` followed by an unexpected `b`, and not `a + (+b)`, even though both are
syntactically possible:

```zp
// munch.zp
type Bin = { L E; Op node[string]; R E }
type Un = { Op node[string]; X E }
type Post = { X E; Op node[string] }
type E ~ node[string] | Bin | Un | Post

@start
rule main E = e:e $$ => $e

rule e E = #operators {
    skip ws
    operand num
    level additive { infix left "+" => Bin{L: $lhs, Op: $op, R: $rhs} }
    level prefix { prefix "+" => Un{Op: $op, X: $rhs} }
    level postfix { postfix "++" => Post{X: $lhs, Op: $op} }
}
rule num = [0-9]+ #atomic
rule ws = " "*
```

```bash
zp parse -g munch.zp -f sexpr -i '1 + +2'
zp parse -g munch.zp -f sexpr -i '1 ++2'
zp parse -g munch.zp -f sexpr -i '1 ++'
```

```text
Bin{L: "1", Op: "+", R: Un{Op: "+", X: "2"}}
zp: 1:5: syntax error: expected end of input, "+", "++"
Post{Op: "++", X: "1"}
```

As in C, write `1 + +2` with a space between the operators.

**Word operators need a word boundary.** `"and"` also matches the start of `android`. Check the boundary in the
operator part with a negative lookahead, and exclude the keywords from identifiers:

```zp
// words.zp
type Bin = { L E; Op node[(node[string],)]; R E }
type Not = { X E }
type E ~ node[string] | Bin | Not

@start
rule e E = #operators {
    skip ws
    operand id
    level logical_or { infix left "or" !idchar => Bin{L: $lhs, Op: $op, R: $rhs} }
    level logical_and { infix left "and" !idchar => Bin{L: $lhs, Op: $op, R: $rhs} }
    level prefix { prefix "not" !idchar => Not{X: $rhs} }
}
rule id = (!kw [a-z]+) #atomic
rule kw = ("or" / "and" / "not") !idchar
rule idchar = [a-z]
rule ws = " "*
```

```bash
zp parse -g words.zp -s e -f sexpr -i 'a or b and not c'
zp parse -g words.zp -s e -f sexpr -i 'notable or a'
```

```text
Bin{L: "a", Op: ("or"), R: Bin{L: "b", Op: ("and"), R: Not{X: "c"}}}
Bin{L: "notable", Op: ("or"), R: "a"}
```

**A matched operator whose right operand fails is not applied, unless you cut.** The parser backs up to before the
operator and ends the expression there, leaving the operator for the enclosing rule. This is usually right (the outer
rule reports the error at the farthest failure), but if the same text can also continue differently, the expression
quietly ends early. A cut (`#cut`) in the operator part commits to the operator:

```zp
// cut.zp
type Bin = { L E; Op node[(node[string],)]; R E }
type E ~ node[string] | Bin

rule plain E = #operators {
    skip ws
    operand num
    level additive { infix left "+" => Bin{L: $lhs, Op: $op, R: $rhs} }
}
rule committed E = #operators {
    skip ws
    operand num
    level additive { infix left "+" #cut => Bin{L: $lhs, Op: $op, R: $rhs} }
}

@start(default)
rule m1 = plain ws "+" ws "x" $$
@start
rule m2 = committed ws "+" ws "x" $$
rule num = [0-9]+ #atomic
rule ws = " "*
```

```bash
zp parse -g cut.zp -s m1 -f sexpr -i '1 + x'
zp parse -g cut.zp -s m2 -f sexpr -i '1 + x'
```

```text
("1" [" "] "+" [" "] "x")
zp: 1:5: syntax error: expected [0-9]
```

Without the cut, `1 + x` is the number `1` followed by the literal text `+ x`; with it, the `+` is an operator and the
missing operand is an error.

**Left recursion needs the base case last.** See [The Same Language With Left Recursion](#the-same-language-with-left-recursion). If a left-recursive rule matches less than you expect,
check the order of its alternatives first.

## A Larger Example: Minilang

[`examples/minilang`](../../examples/minilang/minilang.zp) is a small language where statements are ordinary PEG rules
and expressions are one Operators Rule, nested in each other. The pieces that matter for this guide:

```zp
rule expr Expr = e:operation @error(message="expected an expression") => $e

rule operation Expr = #operators {
    skip ws
    operand number
    operand string
    operand bool
    operand ident
    operand "(" e:operation ws ")" => $e
    operand "[" xs:args? ws "]" => ArrayLit{Elems: concat($xs)}
    level conditional { infix right "?" t:operation ws ":" => Cond{Cond: $lhs, Then: $t, Else: $rhs} }
    level logical_or { infix left "||" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level logical_and { infix left "&&" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level equality { infix none "==" / "!=" / "<=" / ">=" / "<" / ">" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level additive { infix left "+" / "-" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level multiplicative { infix left "*" / "/" / "%" => Binary{Left: $lhs, Op: $op, Right: $rhs} }
    level prefix { prefix "-" / "!" => Unary{Op: $op, X: $rhs} }
    level postfix {
        postfix "(" xs:args? ws ")" => Call{Fn: $lhs, Args: concat($xs)}
        postfix "[" i:operation ws "]" => Index{X: $lhs, Index: $i}
        postfix "." ws n:name => Member{X: $lhs, Name: $n}
    }
}
rule args = first:expr rest:(-ws -"," x:expr)* => concat(list($first), map($rest, (r) => r.$x))
```

(The grammar is abridged here; the types and the lexical rules are in the file.)

- a rule containing an Operators Rule is `operation`; the rule that statements call is `expr`, a thin wrapper that attaches an `@error`
  message to the whole expression. Inside the Operators Rule, nested expressions call `operation` directly, so the
  wrapper's message applies only where a whole expression is expected.
- The language has no comma operator, so call arguments are plain `expr` calls and no level name is needed.
- Statements such as `let` and `while` are PEG rules that call `expr`. Together with the `#recover` pragma on `stmt`,
  a bad expression only loses its own statement. See [Recovering from Errors](recovery.md).

Parsing a few statements with the real grammar:

```bash
zp parse -g examples/minilang/minilang.zp -f sexpr -i 'let x = a + b * c;'
zp parse -g examples/minilang/minilang.zp -f sexpr -i 'f(1)(2)[3].x;'
zp parse -g examples/minilang/minilang.zp -f sexpr -i 'let z = -f(x) * 2;'
```

```text
Program{Body: [Let{Name: Ident("x"), Value: Binary{Left: Ident("a"), Op: "+", Right: Binary{Left: Ident("b"), Op: "*", Right: Ident("c")}}}]}
Program{Body: [ExprStmt{X: Member{Name: Ident("x"), X: Index{Index: Number("3"), X: Call{Args: [Number("2")], Fn: Call{Args: [Number("1")], Fn: Ident("f")}}}}}]}
Program{Body: [Let{Name: Ident("z"), Value: Binary{Left: Unary{Op: "-", X: Call{Args: [Ident("x")], Fn: Ident("f")}}, Op: "*", Right: Number("2")}}]}
```
