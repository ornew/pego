# 005. Pratt Expressions (`pratt`)

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-07

> **Note (2026-10)**: Implemented in `internal/engine/pratt.go`. The CST field names were changed to match the engine rewrite (see the CST section of [spec/pratt.md](../../spec/pratt.md)).

## Summary

Add **Pratt expressions** (`pratt { ... }`) to PEGO to declare operator precedence and associativity.

Inside a Pratt expression, the *shapes* of operands and operators are written as ordinary parser expressions (PEG), while *how they are combined* (precedence and associativity) is decided by the Pratt parsing algorithm.
A Pratt expression is itself a parser expression, so the two can be nested freely: statements and declarations in PEG, expressions in Pratt, and the parts inside expressions (parentheses, call arguments and so on) in PEG again.

```pego
def expr: Expr = pratt {
    skip    ws
    operand number / ident / "(" e:expr ")" -> $e

    level { infix right "?" then:expr ":"  -> new Cond{Cond: $lhs, Then: $then, Else: $rhs} }
    level { infix left  "||"               -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix left  "&&"               -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix none  "==" / "!="        -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix left  "+" / "-"          -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix left  "*" / "/"          -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { prefix      "-" / "!"          -> new Unary{Op: $op, X: $rhs} }
    level { infix right "**"               -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level {
        postfix "(" args:(expr (-"," expr)*)? ")" -> new Call{Fn: $lhs, Args: $args}
        postfix "[" i:expr "]"                    -> new Index{X: $lhs, Index: $i}
        postfix "." name:ident                    -> new Member{X: $lhs, Name: $name}
    }
}
```

## Motivation

The standard way to express operator precedence in PEG is to write one rule per precedence level and have the rules call each other hierarchically.

```pego
def expr   = term   (("+" / "-") term)*
def term   = factor (("*" / "/") factor)*
def factor = "-" factor / primary
...
```

This approach has the following problems:

1. **Verbose and hard to change**: it needs one rule per precedence level. Inserting a level requires rewriting the references in the rules before and after it.
2. **Deep CSTs**: under the "one rule, one node" principle, even a simple input such as `1` produces one node per level: `expr → term → factor → primary`.
3. **Slow**: parsing any operand goes through a rule call at every level.
4. **Associativity is buried in structure**: left associativity is written as repetition plus `foldl`, right associativity as right recursion, and non-associativity with `?`. Each associativity is written differently and cannot be read from a declaration.

A Pratt parser solves all of these with a table of binding powers (precedence) and associativity per operator, and runs in time proportional to the length of the input.
On the other hand, the syntax of operands and of the inside of operators (parentheses, argument lists, subscripts, the middle part of a ternary operator and so on) is more naturally written in PEG.
The design therefore combines the strengths of both.

## Syntax

```
PrattExpr   = "pratt" "{" PrattItem* "}"
PrattItem   = Skip / Operand / Level
Skip        = "skip" Expr
Operand     = "operand" Expr ("->" Action)?
Level       = "level" Name? "{" Operator* "}"
Operator    = "prefix"  Expr ("->" Action)?
            / "postfix" Expr ("->" Action)?
            / "infix" Assoc Expr ("->" Action)?
Assoc       = "left" / "right" / "none"
```

- A `PrattExpr` can appear only at the top level of the right-hand side of a rule definition (`def e = pratt { ... }`). It cannot appear inside another expression.
- `Expr` is an ordinary parser expression. The expression written for an operator is called the **operator part**.
- Items are separated by newlines (`skip`, `operand`, `level`, `prefix`, `postfix` and `infix` are keywords that appear only at the start of an item).
- An action after `->` belongs to the operator or operand on that line.

### `skip`

PEGO has no scanner, so whitespace handling must be explicit.
The expression given to `skip` is **implicitly inserted before each operand and before each operator part** in the Pratt expression, and its result is discarded (as with `-`).
If `skip` is omitted, nothing is inserted. It is not inserted inside an operator part (for example, between `"("` and `args` in `"(" args ")"`).

### `operand`

An element that appears at the start of an expression when no prefix operator applies (the atomic part of Pratt's *nud*).
Multiple operands form an ordered choice in declaration order.

### `level`

A precedence level. **Levels are listed from weakest to strongest** (a later level binds more tightly).
Expressing precedence by the order of levels, rather than by numeric binding powers, makes it easy to insert a level.
Operators in the same level have the same precedence. A level may mix prefix, infix and postfix operators.
Numeric binding powers cannot be written ([decision 2](#2-no-numeric-binding-powers)).

A level can be named (`level assignment { ... }`). Names are unique within a rule.

### Calls with a level (`rule(level)`)

When a parser expression calls a rule that contains a Pratt expression with a level name, as in `expr(assignment)`, the expression is parsed **using only the operators of that level and stronger levels** (parsing starts at `parse(level(assignment) - 1)`).
A call without a level, `expr`, starts at `parse(0)`.

```pego
def expr: Expr = pratt {
    operand number / ident / "(" e:expr ")" -> $e
    level { infix left "," -> new Comma{L: $lhs, R: $rhs} }
    level assignment { infix right "=" -> new Assign{L: $lhs, R: $rhs} }
    level { infix left "+" -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level {
        // Inside arguments, the comma operator is not parsed
        postfix "(" args:(expr(assignment) (-"," expr(assignment))*)? ")"
            -> new Call{Fn: $lhs, Args: $args}
    }
}
```

### Operator kinds

| Kind | Form | Description |
|:--|:--|:--|
| `prefix P` | `P rhs` | Prefix operator |
| `postfix P` | `lhs P` | Postfix operator |
| `infix left P` | `lhs P rhs` | Left-associative infix operator (`a-b-c` = `(a-b)-c`) |
| `infix right P` | `lhs P rhs` | Right-associative infix operator (`a**b**c` = `a**(b**c)`) |
| `infix none P` | `lhs P rhs` | Non-associative infix operator (`a==b==c` does not chain) |

The operator part `P` is an arbitrary parser expression and may call the Pratt expression's own rule or other rules.
This makes it possible to express so-called mixfix operators.

```pego
infix right "?" then:expr ":"          // ternary operator  lhs ? then : rhs
postfix "[" i:expr "]"                 // subscript         lhs[i]
prefix "if" c:expr "then" t:expr "else" // if expression    if c then t else rhs
infix left _                           // function application  f x (empty operator part)
```

## Semantics

Number the levels 1, 2, …, N from the bottom (the first level written is 1). Let `level(o)` be the level number of operator `o`.
Evaluation of a Pratt expression starts with `parse(0)`.

```
parse(min):
    // --- head (nud) ---
    skip
    if one of the prefix operator parts matches (selected as described below) then
        p := that prefix operator
        rhs := parse(level(p))           // take in only operators stronger than p
        on failure, abandon p and try the operands
        lhs := action of p($op, $rhs)
    else
        lhs := operand                    // on failure, the whole Pratt expression fails
    // --- tail (led) ---
    loop:
        save := position
        skip
        o := the matching infix or postfix operator part (selected as described below)
        if there is no o or level(o) <= min then position := save; break
        if o is infix none and an infix none of the same level was just applied then position := save; break
        if o is postfix then lhs := action of o($lhs, $op); continue
        rhs := parse(level(o) - 1 if o is right, otherwise level(o))
        if rhs fails then position := save; break     // return lhs without applying o
        lhs := action of o($lhs, $op, $rhs)
    return lhs
```

### Selecting an operator (longest match)

At the head position all prefix operator parts are tried, and at a tail position all infix and postfix operator parts are tried; **the one with the longest match is selected**. On a tie, the one declared first is selected.
The operator is determined from the input alone, **before** the binding power is checked. If the binding power is insufficient, the loop exits without switching to a shorter candidate.

This is the same behavior as an ordinary Pratt parser over a token stream, and it distinguishes `-` from `->` correctly even when they are in different levels, regardless of declaration order.
It differs from PEG's ordered choice, but since the declaration order of operators is also their precedence order, an ordered choice would make the two conflict ([decision 3](#3-operators-are-selected-by-longest-match)).

### Prefix operators and level limits

A prefix operator at the head position is not restricted by `min`.
For example, even if `**` is in a stronger level than unary `-`, the right operand of `2 ** -1` can be parsed as an expression that begins with the prefix operator `-`.

### Backtracking and cut

- If an operator part matches but its right operand fails to parse, the operator is not applied: parsing backs up to before the operator and returns `lhs` (as in PEG, the failure is absorbed by backtracking).
- If a [cut](../../spec/parser-expressions.md) (`--`) was passed inside the operator part, parsing does not back up and the whole Pratt expression fails. For example, writing `infix left "+" --` makes an input with no expression after `+` an error.

### Non-associative operators

If an `infix none` operator of the same level follows immediately after an `infix none` operator was applied, the loop exits (the operator is not consumed).
`a == b == c` matches up to `a == b`, and the remaining `== c` fails in the outer context (typically at `$$`).

## Actions and captures

The following names are reserved in operator actions.

| Name | Contents | Available in |
|:--|:--|:--|
| `$lhs` | Value of the left operand | `infix`, `postfix` |
| `$rhs` | Value of the right operand | `infix`, `prefix` |
| `$op` | Match of the operator part (a terminal, as if `@` were applied) | All |

Captures inside the operator part (`then:expr` and so on) can also be referenced from the action.
`$lhs`, `$rhs` and `$op` cannot be used as capture names. Positional references with `$n` cannot be used in operator actions.

## Types

- For `def e: T = pratt { ... }`, the results of `operand` and of every operator action must be of type `T`.
- `$lhs` and `$rhs` have type `T`.
- If the rule has no type (`node`), an operator without an action produces the default CST node (next section). If the rule has a type, every operator must have an action.

## CST

Applying an operator without an action produces a node of the reserved node type `Operator`.

| Field | Contents |
|:--|:--|
| Rule name | The rule that contains the Pratt expression |
| `operator` field | The operator's index in declaration order |
| Children | `[lhs, op]` (postfix), `[op, rhs]` (prefix), `[lhs, op, rhs]` (infix) |

Operands appear as they are (as the nodes produced by `operand`).
With a Pratt expression, the input `1` yields a single node from `operand`, and the CST does not deepen in proportion to the number of precedence levels.

## Relation to other features

### Nesting with PEG

- A Pratt expression is an ordinary parser expression that can be written on the right-hand side of a rule. Other rules call it as an ordinary rule.
- `operand` and the operator parts are PEG and can call other rules or the Pratt expression itself. A call without a level starts at `parse(0)` (the inside of parentheses and similar constructs is parsed again from the weakest level).

### Left recursion

- Calling the Pratt expression itself (or a rule that contains it) at the start of an `operand` is left recursion, which is a compile error. Constructs that take an expression on the left are written with `infix` or `postfix`.
- If a rule that contains a Pratt expression is in a left-recursive relationship with other rules outside the Pratt expression, the existing left-recursion mechanism applies ([003](003-packrat-parsing.md)).

### Memoization

Calls to a rule that contains a Pratt expression are memoized keyed by **the pair of start position and level (`min`)**.
A call without a level is treated as `min = 0`.
The recursive `parse(min)` calls inside the Pratt loop are not memoized.
When several candidates are tried for the longest operator match, rules called from operator parts are memoized as usual.

## Implementation plan (outline)

- `grammar`: add `PrattRule` (`Skip`, `Operands`, `Levels []PrattLevel`, and each operator's kind, associativity, operator part and action), with JSON support.
- Compiler: compile `skip`, `operand` and each operator part into small subprograms, and give `Program` an operator table (level number, kind, associativity).
- VM: execute the Pratt loop natively as a dedicated instruction (for example `OpPratt <table ID>`). Keep `min` in the call frame.
  Try operator parts with the existing choice/backtrack mechanism, and compare each candidate's end position for the longest match.
  Combine operator parts that start with literals into a prefix tree (trie) at compile time, and use the first input character to narrow the candidates to try.
- Add the level to the memo key (only for rules with a Pratt expression).

Desugaring into a hierarchy of PEG rules is also possible, but it is not adopted because it cannot preserve longest-match operator selection or the shallow CST of "one rule, one node".

## Decisions

This section records the options considered and the reasons for the choices made.

### 1. Levels can be named, and calls may specify a level

- Rejected: not allowing this, and writing subexpressions with a restricted range as separate Pratt rules. The operator table would be duplicated.
- **Adopted**: `level name { ... }` and `rule(name)`. Real languages commonly need this, for example function arguments in C and JavaScript (parsed from assignment expressions upward, excluding the comma operator) or the body of a Python `lambda`. The cost is adding the level to the memo key.

### 2. No numeric binding powers

- Rejected: numeric binding powers such as `bp(left, right)`. They would undo the benefit of easy level insertion.
- **Adopted**: level order only. The typical seemingly asymmetric case (`2 ** -1`) can be parsed under the current specification because prefix operators are not restricted by the level. Most other cases are covered by calls with a level. This will be revisited if a concrete need arises.

### 3. Operators are selected by longest match

- Rejected: ordered choice in declaration order. It is consistent with PEG and fast, but because declaration order also encodes precedence, users would have to work around conflicts such as `-` versus `->` with `!`.
- Rejected: trying only the operators usable at the current level, in declaration order (equivalent to desugaring into per-level rules). It misparses when whatever follows a shorter operator happens to parse.
- **Adopted**: longest match. Correctness does not depend on the user's care. Performance is recovered by narrowing candidates with a trie.

### 4. A Pratt expression can appear only at the top level of a rule's right-hand side

- Rejected: allowing it anywhere. This would require self-reference syntax (such as `self`) and working out the relationship with "one rule, one node".
- **Adopted**: top level only. Adding one rule achieves the same effect, and the specification and implementation stay simple. The restriction can be relaxed later if needed.

### 5. The default CST lists `[lhs, op, rhs]` in `_$Children`

- Rejected: separate reserved fields (`_$Lhs`, `_$Op`, `_$Rhs`). This adds reserved fields.
- Rejected: making the operator a string field and keeping only the operands as children. The operator's position would be lost.
- **Adopted**: the node has the same shape as a `Seq` node, so generic traversal works, and the operator's position, which editor use cases need, is kept. The operator is identified by `_$OperatorID`.

## Open questions

1. **Error reporting**: messages for cases such as a missing right operand after an operator. The candidate approach is to use engine-generated messages by default and let an [attribute](../../spec/attributes.md) on the operator's line (`#error`) override them. The default on failure of the right operand is to back up (a cut `--` makes it an explicit error). To be decided together with the overall design of error reporting ("Detailed error reporting" in the roadmap).
