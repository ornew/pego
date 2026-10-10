# Pratt Expressions

A Pratt expression is a rule body that parses expressions with operators by
declaring the operators in a table of binding levels, each with a precedence and
an associativity. The shapes of operands and operators are written as
[parsing expressions](parser-expressions.md); how they combine is determined by
the Pratt parsing algorithm.

The background of the design and the alternatives that were considered are
recorded in [docs/design/005](../docs/design/005-pratt-expressions.md).

```pego
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

## Syntax

```
def <rule name>[: <type>] = pratt {
    skip    <parsing expression>
    operand <parsing expression> [-> <action>]
    level [<level name>] {
        prefix  <operator part> [-> <action>]
        postfix <operator part> [-> <action>]
        infix (left | right | none) <operator part> [-> <action>]
    }
}
```

- `pratt { ... }` MUST be the entire right-hand side of a rule definition. It
  cannot appear inside another parsing expression, and the rule cannot have a
  rule-level action.
- Items need no separator; by convention, each item is written on its own line.
- A Pratt expression has at most one `skip`, at least one `operand`, and any
  number of `level`s. A `level` contains any number of operators.
- An **operator part** is an arbitrary parsing expression. It MAY call other
  rules, including the rule that contains the Pratt expression.

## Items

### `skip`

The `skip` expression is implicitly inserted, with its result discarded, before
each operand and before each operator part. It is used for whitespace and
comments. If `skip` does not match, nothing is skipped. Failures inside `skip`
are not included in the expected items of a
[syntax error](parsing.md#syntax-errors). `skip` MUST NOT contain captures.

`skip` is not inserted inside an operator part or an operand, so an operator
such as `"?" t:expr ":"` must spell out the whitespace before `":"` (as in
`"?" t:expr ws ":"` in the example above). Whitespace after the last token of
the expression is not consumed either. Without a `skip` item, nothing is
inserted.

### `operand`

An `operand` describes the leading element of an expression that does not start
with a prefix operator. Multiple `operand` items form an ordered choice in
declaration order.

An action (`->`) applies to the whole `operand` item. To give different
alternatives different actions, write them as separate `operand` items.
An `operand` action can use captures and positional references (`$n`).

An `operand` MUST NOT call its own rule at its start, since that would be left
recursion. Write constructs that take an expression on their left as `infix` or
`postfix` operators instead.

### `level`

A `level` declares a binding level. **Levels declared earlier bind more
loosely**; levels declared later bind more tightly. All operators in the same
level have the same precedence, and a level MAY mix prefix, infix and postfix
operators.

A level MAY have a name, which MUST be unique within the rule. Named levels are
used by [level-restricted calls](#level-restricted-calls).

### Operators

| Declaration | Form | Associativity |
|:--|:--|:--|
| `prefix P` | `P rhs` | — |
| `postfix P` | `lhs P` | — |
| `infix left P` | `lhs P rhs` | `a-b-c` is `(a-b)-c` |
| `infix right P` | `lhs P rhs` | `a**b**c` is `a**(b**c)` |
| `infix none P` | `lhs P rhs` | `a==b==c` does not chain: the expression ends after `a==b` |

An operator part can consist of several elements. The following declarations
appear inside `level { ... }` blocks:

```pego
infix right "?" then:expr ":"           // conditional    lhs ? then : rhs
postfix "[" i:expr "]"                  // index          lhs[i]
prefix "if" c:expr "then" t:expr "else" // if expression  if c then t else rhs
infix left _                            // application    f x (empty operator part)
```

## Parsing rules

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
  [cut](parser-expressions.md#cut) (`--`), the whole Pratt expression fails
  instead.

## Level-restricted calls

When a rule that has a Pratt expression is called with a level name, as in
`expr(assignment)`, its tail applies **only infix and postfix operators of the
named level and of the levels that bind more tightly**. Calling the rule
without a level name (`expr`) allows tail operators of all levels. The rule
name and `(` MUST be adjacent, and the named level MUST exist in the called rule.

The [prefix-operator exception](#parsing-rules) still applies: an operand
position, including the start of a level-restricted call, MAY begin with a
prefix operator from any level. Its right operand uses that prefix operator's
own binding level, rather than the caller's minimum. Consequently, operators
looser than the entry level can occur inside that right operand if they bind
more tightly than the prefix. Level restriction is not a filter over every
operator in the resulting tree. Calls written inside operator parts or operands
also use their own level restrictions.

```pego
// Function arguments do not parse the comma operator (only assignment and tighter).
def args = first:expr(assignment) rest:(-ws "," x:expr(assignment))*
```

For example, if `~` is a prefix in a level looser than addition and
`multiplication` is a named tighter level, `expr(multiplication)` accepts
`~x+y` as `~(x+y)`. Without the prefix, it stops before the `+` in `x+y`.
To exclude a lower-level prefix at entry, the caller must guard that input
explicitly, accounting for any whitespace skip. For a rule without `skip`,
`!"~" expr(multiplication)` excludes `~` at entry.

## Actions

An operator action can refer to the following names.

| Name | Value | Available in |
|:--|:--|:--|
| `$lhs` | The value of the left operand | `infix`, `postfix` |
| `$rhs` | The value of the right operand | `infix`, `prefix` |
| `$op` | The text that the operator part matched, as a `Match` node | all operators |

Captures inside the operator part (such as `then:expr`) can be referred to as
well. `lhs`, `rhs` and `op` MUST NOT be used as capture names in operator
parts. Positional references (`$n`) MUST NOT be used in operator actions.

The node created by an operator action covers the operator expression: from the
start of the left operand (or of the prefix operator) to the end of the right
operand (or of the postfix operator).

## Types

If the rule is declared with type `T`, the values of all `operand` items and of
all operators MUST be assignable to `T`, and `$lhs` and `$rhs` have type `T`.
An operator without an action produces an `Operator` node, so when `T` does not
accept `Operator`, every operator needs an action.

If the rule has no declared type, its type is the union of the types of all
`operand` items and operators.

## Concrete syntax tree

An operator without an action produces a node of the reserved type `Operator`.

| Property | Value |
|:--|:--|
| Rule name | The rule that contains the Pratt expression |
| `operator` field | The index of the operator in declaration order, counted across all levels (0-based) |
| Children | prefix: `[op, rhs]`; postfix: `[lhs, op]`; infix: `[lhs, op, rhs]` |

`op` is the value of the operator part: a `Match` node for `"+"`, a `Seq` node
for `"[" i:e "]"`, and so on. Captures in the operator part become fields of
that value.

The value of an `operand` item without an action appears unchanged; a node that
it creates is labeled with the name of the rule. The number of levels does not
make the CST deeper.

```pego
def e = pratt {
    operand @(?0-9)+
    level { infix left "+" }
    level { infix left "*" }
}
```

With this rule, `1+2*3` produces the following tree (in the S-expression
format of `pego parse -f sexpr`, where `@e` is the rule name):

```
(Operator "1"@e "+" (Operator "2"@e "*" "3"@e operator=1)@e operator=0)@e
```
