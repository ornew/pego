# 048. Inline Grammar Actions

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

The source form `(expression => term)` is an inline grammar action. It behaves
exactly as a reference to an anonymous rule whose body is the expression and
whose whole-rule action is the term, written at that position. The compiler
lowers it that way, so every backend runs it with the semantics of a rule.

```pego
type Number = { Text node[string] }
type Word = { Text node[string] }
type Expr ~ Number | Word
type Main = { Value Expr }

rule number Number = t:([0-9]+) #atomic => Number{Text: $t}
rule word Word = t:([a-z]+) #atomic => Word{Text: $t}
rule item Expr = (n:number => $n) / (w:word => $w)
rule main Main = item:item => Main{Value: $item}
```

The whole-rule action remains `=> term` after the complete ordinary rule
expression. Operators Rule actions apply separately to operand and operator
entries. Operator-entry actions keep their specialized `$lhs`, `$rhs` and `$op`
context. A declaration-level action on an Operators Rule may be omitted or use
the explicit `=> ...` default-CST form; any other action is rejected at compile
time. Only a single-term Value Action can be inline; an inline braced Value
Action block is a compile-time error, as a whole-rule one is.

## Contract

`(e => A)` in a rule `r` means the same as a call to a rule `r_inline_n = e => A`,
and with `rule a = _ => A` and `rule b = a => B`, the rule `b` is the same as
`rule b = (_ => A) => B`. The rules below follow from this.

- **Scope.** Inside `A` only what a rule body `e => A` sees is visible: the
  captures made inside `e`, and its items. The captures of the enclosing rule are
  not visible. The captures made inside `e` are scoped to the anonymous rule and
  end at the closing parenthesis. The captures made before the parentheses are
  not closed: the expressions after them and the whole-rule action can still read
  them.
- **Value and type.** The inline action is one positional element of the
  enclosing sequence, like a rule reference, and its value is the result of `A`.
  `A` returns a node or `nil` and is checked at compile time like the action of a
  rule; the result type is the type of `A`, or the default value type for
  `=> ...`. The typed value, the AST and the schema treat the item as a reference
  to a rule of that type.
- **Matching.** `A` runs after `e` matches. A failing `e` runs no action, and its
  failure, expectations and error position are those of the failing rule. A
  choice selects its branch by PEG matching before the action runs. An action
  error is fatal and does not retry another branch or enter recovery.
- **Cut, recovery, recursion, depth.** The boundary is a rule boundary. A cut
  inside the parentheses commits only within them, a recovery inside them
  belongs to the anonymous rule, left recursion through them is left recursion
  through a rule, and the call counts against the maximum depth.
- **Memoization.** The anonymous rule is memoized like any other rule where the
  analysis allows; memoization is not observable.
- **Unused values.** An inline action in a lookahead (`&`, `!`) or a discard
  (`-`) is a compile-time error with the message "an inline action is not
  allowed in a lookahead or a discard, whose value is not used".
- **Name.** The anonymous rule of the nth inline action of the rule `r`, inner
  actions first, is `r_inline_n`, with a trailing underscore added while a rule
  of that name exists. The name is what a rule's name is wherever a rule's name is
  shown: the `rule` of a node, the text of an action error, trace events and the
  output schema.

## Implementation

`lowerInlineActions` (`internal/engine/inline_actions.go`) replaces each inline
action by a reference to a hidden rule placed after the rule that holds it,
before attribute normalization. It runs once, in the compiler, so the closure
and bytecode VMs and the generated Go (generic, typed and direct) and
TypeScript parsers need no case of their own and agree with the explicit
rewrite by construction. The program keeps the grammar as written
(`Program.SourceGrammar`, returned by `Parser.Grammar`) and serializes it: the
inline actions stay in the AST of a compiled grammar, and a loader lowers them in
the same way before applying the stored rule flags, which are those of the
lowered rules. The bytecode and the instruction set are unchanged.

The differential tests compare each grammar written with inline actions against
the same grammar with explicit rules on every VM backend, and run the grammars in
the corpus of generated Go and TypeScript parsers.

## Alternatives

- **Whole-rule actions only.** This is simpler, but cannot reduce a matched
  sequence before later sequence elements.
- **A suffix on one primary.** It cannot name the complete preceding sequence as
  the reduction input.
- **Braced local actions.** Braces overlap with type constructors and
  declaration bodies. Parentheses delimit the expression and action term.
- **A dedicated instruction and backend support.** An inline instruction that
  reduces the wrapped value and clears the captures needs its own frame handling,
  capture rules and recovery behavior in each backend, and these rules differ
  from a rule's. Lowering to a rule reuses the rule machinery and its tests.

## Limitations

An inline braced Value Action block is rejected. The anonymous rule's name is visible where rule names are shown.
