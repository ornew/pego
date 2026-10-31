# Operators Rule

An Operators Rule Declaration Statement defines a rule body that parses
expressions by declaring operator parts at binding levels, each with a
precedence and an associativity. Operand and operator shapes are ordinary Rule Expressions.
Their combination follows a parsing algorithm inspired by Pratt parsing.

An Operators Rule is written with the `#operators` pragma, which stands where an expression would and
is valid only as the whole body of a rule: `rule e = #operators { ... }`. Anywhere else it is a syntax
error. The word `operators` has no special meaning and is an ordinary identifier.

```zp
type Num = node[string]
type Ident = node[string]
type Comma = { L Expr; R Expr }
type Assign = { L Expr; R Expr }
type Cond = { Cond Expr; Then Expr; Else Expr }
type Bin = { L Expr; Op node[string]; R Expr }
type Unary = { Op node[string]; X Expr }
type Call = { Fn Expr; Args []Expr }
type Index = { X Expr; Index Expr }
type Member = { X Expr; Name Ident }
type Expr ~ Num | Ident | Comma | Assign | Cond | Bin | Unary | Call | Index | Member

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

rule number Num = ([0-9]+) #atomic
rule ident Ident = ([a-z]+) #atomic
rule ws = [ \t\r\n]*
```

## Syntax

```text
rule <name> [<Type Expression>] = #operators {
    skip <Rule Expression>
    operand <Rule Expression> [=> <action>]
    level [<level name>] {
        prefix <operator part> [=> <action>]
        postfix <operator part> [=> <action>]
        infix (left | right | none) <operator part> [=> <action>]
    }
} [#ignorecase] [=> <action>]
```

- An Operators Rule uses a dedicated
  `rule <name> [<Type Expression>] = operators` form. It cannot
  appear inside another Rule Expression. Its optional type follows the rule
  name; when omitted, the type is inferred from operands and operators.
- `#ignorecase` may follow the closing `}` on the same line and makes the
  string literals of the whole rule case-insensitive (see
  [`#ignorecase`](pragmas.md#ignorecase)). No other pragma may follow it.
- An action after an `operand` or operator declaration uses `=>` and applies to
  that complete operand or operator entry.
- A rule-level Value Action after the closing `}` (and after `#ignorecase`,
  if present) is a [whole-rule action](#whole-rule-action). Omitting it or
  writing the explicit `=> ...` default form keeps the folded value as the
  rule's value.
- Semicolons separate Declaration Statement items when needed; line boundaries may also make the separation unambiguous. Struct fields use semicolons or newlines; constructor fields and arguments use commas.
- An Operators Rule has at most one `skip`, at least one `operand`, and any
  number of `level`s. A `level` contains any number of operators.
- An **operator part** is an arbitrary Rule Expression. It MAY call other
  rules, including the rule that contains the Operators Rule.

## Items

### `skip`

The `skip` expression is implicitly inserted, with its result discarded, before
each operand and before each operator part. It is used for whitespace and
comments. If `skip` does not match, nothing is skipped. Failures inside `skip`
are not included in the expected items of a
[syntax error](../runtime/parsing.md#syntax-errors). `skip` MUST NOT contain captures.

`skip` is not inserted inside an operator part or an operand, so an operator
such as `"?" t:expr ":"` must spell out the whitespace before `":"` (as in
`"?" t:expr ws ":"` in the example above). Whitespace after the last token of
the expression is not consumed either. Without a `skip` item, nothing is
inserted.

### `operand`

An `operand` describes the leading element of an expression that does not start
with a prefix operator. Multiple `operand` items form an ordered choice in
declaration order.

An action (`=>`) applies to the whole `operand` item. To give different
alternatives different actions, write them as separate `operand` items.
An `operand` action can use the captures of its line.

An `operand` MUST NOT call its own rule at its start, since that would be left
recursion. Write constructs that take an expression on their left as `infix` or
`postfix` operators instead.

### `level`

A declaration `level additive { ... }` declares level `additive` on the
Operators Rule target `expr`. The enclosing Operators Rule identifies the target. **Levels declared earlier
bind more loosely**; levels declared later bind more tightly. All operators in
the same level have the same precedence, and a level MAY mix prefix, infix and
postfix operators. Every Operators Rule item, including each level declaration,
uses the same hard-newline or semicolon statement boundary.

The level name is optional. When present, it MUST be unique within the target
rule and may be used by [level-restricted calls](#level-restricted-calls).
Unnamed levels are valid but cannot be selected by `level(rule, name)`; a
level-restricted call must name an existing named level.

### Operators

| Declaration | Form | Associativity |
|:--|:--|:--|
| `prefix P` | `P rhs` | — |
| `postfix P` | `lhs P` | — |
| `infix left P` | `lhs P rhs` | `a-b-c` is `(a-b)-c` |
| `infix right P` | `lhs P rhs` | `a**b**c` is `a**(b**c)` |
| `infix none P` | `lhs P rhs` | `a==b==c` does not chain: the expression ends after `a==b` |

An operator part can consist of several elements. The following declarations
appear inside `level name { ... }` blocks:

```zp
infix right "?" then:expr ":" // conditional    lhs ? then : rhs
postfix "[" i:expr "]" // index          lhs[i]
prefix "if" c:expr "then" t:expr "else" // if expression  if c then t else rhs
infix left _ // application    f x (empty operator part)
```

## Parsing Rules

- **Precedence.** The right operand of an operator includes only operators of
  binding levels that bind more tightly than the operator's own level; for a
  `right` operator, it also includes operators of its own level.
- **Prefix operators.** A prefix operator at the start of an operand position is
  not restricted by binding levels: `2 ** -1` parses even though `**` binds more
  tightly than the prefix `-`.
- **Operator selection.** At each position, the parser selects, among the
  operator parts that match there, **the one with the longest match**; among
  matches of equal length, the operator declared first wins. The operator is
  selected before precedence is considered: if the selected operator cannot be
  applied because of its precedence, the parser does not fall back to a
  shorter candidate. A prefix or postfix operator part that matches the empty
  string is not a candidate (it could otherwise be applied indefinitely);
  infix operator parts may match the empty string, which is how juxtaposition
  is written. An infix operator that is applied without consuming any input
  (an empty operator part and an empty right operand, with no `skip` text)
  ends the expression after it is applied, as an iteration that consumes no
  input ends a repetition; otherwise it would be applied indefinitely.
- **Backtracking.** If an operator part matches but its right operand cannot be
  parsed, the operator is not applied and the expression ends before the
  operator. For a prefix operator, the parser then tries the `operand` items at
  the same position instead. If the operator part passed a
  [cut](parser-expressions.md#cut) (`#cut`), the whole Operators Rule fails
  instead. If no prefix operator part matches and any candidate failed after
  passing a cut, the whole expression also fails instead of trying operands.
  Failed candidates do not veto another successfully matching operator part;
  only the selected operator's own cut controls a later right-operand failure.
  Cuts remain local to their parser-expression choice or called rule. A prefix
  or postfix part rejected for matching empty input contributes a cut failure
  when it passed a cut, just as a part that failed while matching does.

For example, `prefix "-" #cut "z"` with `operand "-x"` rejects `-x`: no prefix
candidate matches, and the attempted prefix passed a cut. Removing `#cut` allows
operand fallback. This rule applies to every engine backend and standalone
Go/TypeScript parsers.

## Level-Restricted Calls

When a rule that has an Operators Rule is called with a level name, as in
`level(expr, assignment)`, its tail applies **only infix and postfix operators of the
named level and of the levels that bind more tightly**. Calling the rule
without a level name (`expr`) allows tail operators of all levels. The `level` keyword and `(` MUST be adjacent, and the named level MUST exist in the called rule.

The [prefix-operator exception](#parsing-rules) still applies: an operand
position, including the start of a level-restricted call, MAY begin with a
prefix operator from any level. Its right operand uses that prefix operator's
own binding level, rather than the caller's minimum. Consequently, operators
looser than the entry level can occur inside that right operand if they bind
more tightly than the prefix. Level restriction is not a filter over every
operator in the resulting tree. Calls written inside operator parts or operands
also use their own level restrictions.

```zp
// Function arguments do not parse the comma operator (only assignment and tighter).
rule args = first:level(expr, assignment) rest:(-ws "," x:level(expr, assignment))*
```

For example, if `~` is a prefix in a level looser than addition and
`multiplication` is a named tighter level, `level(expr, multiplication)` accepts
`~x+y` as `~(x+y)`. Without the prefix, it stops before the `+` in `x+y`.
To exclude a lower-level prefix at entry, the caller must guard that input
explicitly, accounting for any whitespace skip. For a rule without `skip`,
`!"~" level(expr, multiplication)` excludes `~` at entry.

## Value Actions

An operator Value Action can refer to the following names.

| Name | Value | Available in |
|:--|:--|:--|
| `$lhs` | The value of the left operand | `infix`, `postfix` |
| `$rhs` | The value of the right operand | `infix`, `prefix` |
| `$op` | The value of the operator part: a string node for a single literal or class, a tuple node for a sequence | all operators |

Captures inside the operator part (such as `then:expr`) can be referred to as
well. `lhs`, `rhs` and `op` MUST NOT be used as capture names in operator
parts. `value` MUST NOT be used as a capture name in an operand or operator
part either: it names the folded value in the [whole-rule action](#whole-rule-action),
and a capture of that name is a compile-time error, `capture name value is
reserved for the whole-rule action of an Operators Rule`.

A local reads a value that is not a node, such as the string that `text($n)`
returns, as that value, as an ordinary capture does. It is not the node that
carries the value: in `operand "(" n:[0-9] ")" => text($n)`, `$lhs` of `(1)+2`
is `1`, not the text `(1)` of the node that carries it.

The node created by an operator action covers the operator expression: from the
start of the left operand (or of the prefix operator) to the end of the right
operand (or of the postfix operator).

## Whole-Rule Action

A whole-rule action runs once per call of the rule, after the call has applied
its last operator, and its result is the value of the call. It reads the folded
value, the value that the call would have without the action, as the local
`$value`. Parsing the operand of a prefix operator and the right operand of an
infix operator is part of the same call, so the action never runs on an
intermediate result. Every call of the rule runs it, including a
[level-restricted call](#level-restricted-calls) and a call of the rule from
inside one of its own operand or operator parts: in
`operand "(" x:e ")" => $x`, the value of `$x` is the result of the action of
that inner call, so each parenthesized expression is wrapped by the action of
its own call.

```zp
type Expr = { Root _ }
rule e Expr = #operators {
    skip " "*
    operand [0-9]+ #atomic
    operand "(" x:e " "* ")" => $x
    level additive { infix left "+" }
} => Expr{Root: $value}
```

With this rule, `(1 + 2) + 3` produces
`Expr{Root: (Expr{Root: ("1" "+" "2")} "+" "3")}`.

- `$value` is in scope only in the whole-rule action of an Operators Rule.
  The captures of operand and operator lines are local to their lines and are
  not visible to it, and `$lhs`, `$rhs` and `$op` are not defined there.
  Elsewhere `$value` is a compile-time error, `$value is only available in the
  whole-rule action of an Operators Rule`, including in an operand or operator
  action, and in an ordinary rule that does not declare a capture named `value`.
  Such a capture is an ordinary capture in an ordinary rule, so `$value` refers
  to it there; an operand or operator line may not declare it (see
  [Value Actions](#value-actions)).
- A struct node created by the action and returned directly covers the
  complete match of the call, from its start position, including the text
  `skip` consumes before the first operand, to its end, as for the action of an
  ordinary rule (see [Value and Range Details](actions.md#value-and-range-details)).
  The folded value keeps its own range.
- The action does not run when the call fails, so it changes no syntax error,
  expectation or error position. A call that recovers inside its lines runs the
  action on the recovered value. A runtime error in the action fails the parse,
  as for any whole-rule Value Action (see [Types and Errors](actions.md#types-and-errors)).
- A call whose value nothing uses, as in a
  [recognition](../runtime/parsing.md) that no predicate reads, does not run
  the action, as for an ordinary rule, and
  [default values](actions.md#default-values) drop it.

## Types

If the rule is declared with type `T` and has no whole-rule action, the values of all `operand` items and of
all operators MUST be assignable to `T`, and `$lhs` and `$rhs` have type `T`.
An operator without an action produces a tuple node of its operands and its
operator part: `node[(T, O, T)]` for an infix operator and `node[(O, T)]` for a
prefix operator, where `O` is the type of the operator part, with the captures
`lhs`, `op` and `rhs`. When `T` does not accept these nodes, every operator
needs an action; a recursive alias cannot name them, so a join that accepts
them includes `node[_]`.

If the rule has no declared type and no whole-rule action, its type is the
join of the types of all `operand` items and operators.

With a whole-rule action, the declared type `T`, if any, is the type of the
action's result: the result MUST be assignable to `T`, and the rule's type is
`T`, or the type of the action's result when the rule declares none. The
**folded type** is the join of the types of all `operand` items and operators,
where `$lhs` and `$rhs` have the folded type; `$value` has it as well. The
values of the lines are not checked against `T`. The folded type has no name,
so it is inferred as a least fixed point from `_|_`. When it keeps growing,
as the nested tuple nodes of operators without actions do, `$lhs` and `$rhs`
are widened to `node[_]` if every line produces a node, and to a type that is
not known otherwise.

### Named Node Types

If the declared type is exactly a [named node type](types.md#named-node-types)
`N` (also through aliases) and the rule has a whole-rule action, the rule
converts the action's result to `N`, as an ordinary rule converts its value,
and converts no operator. Otherwise, the rule converts the value of every operator it
applies to `N`, as an ordinary rule converts its value (see
[Types and Errors](actions.md#types-and-errors)):

- The value of each `prefix`, `infix` and `postfix` operator, at every nesting
  level of the expression, becomes a node of type `N`. This holds whether the
  operator has an action or not. For an operator without an action, the tuple
  node `(lhs, op, rhs)`, `(lhs, op)` or `(op, rhs)` is converted, and it keeps
  its range, children and captures. For an operator with an action, the node
  that the action returns is converted, and it keeps its range and content;
  a node that already has type `N`, such as `$lhs`, is unchanged. The result
  is a node of type `N` throughout the tree of operators, so `Node.Type()`
  reports `N` and typed output yields `*N` for every operator node.
- The value of an `operand` item is not converted: it is the node the
  operand produces, with the type its own rule or action gives it. It MUST
  be assignable to `N`; otherwise it is a compile-time error, `operand of r
  produces X, which is not assignable to N`, because an operand is never
  converted. Declare the operand rule as `N` so that it converts its own
  value, or declare the Operators Rule as a join of `N` and the operand types,
  which converts nothing. A node held by an operator part, such as the value of
  `$op`, is not an operator node and keeps its type.
- The value of each operator MUST be convertible to `N`: a node whose
  underlying type is assignable to the underlying type of `N`. A tuple node
  of an operator without an action must therefore fit the underlying type, as
  `node[_]` does. Otherwise it is a compile-time error, `operators of r
  without an action produce a tuple node, which cannot be converted to N`
  or `operatorsRule r produces X, which cannot be converted to N`.
  With type checking off, an operator value that does not fit fails the
  parse.
- The converted node has no rule name, as for an ordinary rule.

```zp
type Expr = node[_]
rule expr Expr = #operators {
    operand num
    level { infix left "+" }
    level { prefix "-" }
}
rule num Expr = [0-9]+ #atomic
```

With this rule, every node of `-1+2` that the operators build has type `Expr`,
as do the operands `1` and `2`, which `num` converts itself.

## Concrete Syntax Tree

An operator without an action produces a tuple node (its
[node type name](types.md#node-type-names) is `tuple`) whose range covers the
operator expression. When the rule is declared with a
[named node type](#named-node-types), that node has the named type instead.

| Operator | Elements |
|:--|:--|
| `prefix` | `(op, rhs)` |
| `postfix` | `(lhs, op)` |
| `infix` | `(lhs, op, rhs)` |

The elements are captured as `lhs`, `op` and `rhs`, which source information
reports (see [Nodes and Values](../runtime/nodes.md#source-information)). `op` is the value
of the operator part: a string node for `"+"`, a tuple for `"[" i:e "]"`, whose
elements include the captured `i`, and so on. A node of a nested operand or
operator is not wrapped: the number of levels does not make the tree deeper.

The value of an `operand` item without an action appears unchanged; with source
information, a node that it creates is labeled with the name of the rule.

```zp
rule e = #operators {
    operand [0-9]+ #atomic
    level additive { infix left "+" }
    level multiplicative { infix left "*" }
}
```

With this rule, `1+2*3` produces the following tree (in the S-expression
format of `zp parse -f sexpr -source-info`, where `@e` is the rule name):

```text
(lhs:"1"@e op:"+" rhs:(lhs:"2"@e op:"*" rhs:"3"@e)@e)@e
```

Without source information the same tree is `("1" "+" ("2" "*" "3"))`.
