# 048. Inline Grammar Actions

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-12

## Summary

The source form `(expression => term)` is an inline grammar action. It behaves
exactly as a call of a hidden rule whose body is the expression and whose
whole-rule action is the term, written at that position. The compiler lowers it
that way, so every backend runs it with the semantics of a rule.

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

## Hidden rules

A rule whose name begins with an underscore is hidden. Hidden rules are rules in
every other respect: they are memoized, may recur (also on the left), and a cut or
a recovery inside them is scoped to the rule. They differ in three ways.

- **Not an entry point.** A hidden rule cannot be the start rule of a grammar, a
  compiled grammar, a schema or a generated parser, and cannot be parsed by name
  (`ParseRule`, `Parse(name, ...)`). Each of these is an error that says the rule
  is hidden. `Program.Rules` does not list them.
- **No node of their own.** A hidden rule without an action, not even
  `=> ...`, makes no node. A call of it contributes what its expression would
  contribute written in its place as a group: its elements join the sequence
  that calls it, and its value, where it takes one element, is that of the group.
  The captures made inside it end with the rule and are not visible to the caller.
  A hidden rule with an action, which includes every inline action, yields the
  value of the action.
- **Not a type.** A hidden rule without an action adds no type to the schema, the AST or the
  typed value, and cannot declare a result type. Where a recursive call of one is an
  element of its own, it has the type of the group.

A sequence of elements that a hidden rule would contain in itself, as in
`rule _a = "x" _a`, has no fixed number of elements and is a compile-time error.
A call inside a choice, an option, a repetition or a capture is an element of its
own and may recur.

## Contract

`(e => A)` in a rule `r` means the same as a call of the hidden rule
`_r_n = e => A`, and with `rule a = _ => A` and `rule b = a => B`, the rule `b` is the same as
`rule b = (_ => A) => B`. The rules below follow from this.

- **Scope.** Inside `A` only what a rule body `e => A` sees is visible: the
  captures made inside `e`, and its items. The captures of the enclosing rule are
  not visible. The captures made inside `e` are scoped to the hidden rule and
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
  belongs to the hidden rule, left recursion through them is left recursion
  through a rule, and the call counts against the maximum depth.
- **Memoization.** The hidden rule is memoized like any other rule where the
  analysis allows; memoization is not observable.
- **Unused values.** An inline action in a lookahead (`&`, `!`) or a discard
  (`-`) is a compile-time error with the message "an inline action is not
  allowed in a lookahead or a discard, whose value is not used".
- **Name.** The hidden rule of the nth inline action of the rule `r`, inner
  actions first, is `_r_n`, with a trailing underscore added while a rule of
  that name is declared. A reference to a rule resolves against the rules
  declared in the grammar as written, so the name of a hidden rule that the
  compiler made never satisfies one. The name is shown where the program runs the
  rule: trace events, profiles and source information. A compile-time error and the
  text of a failed action name the inline action by the rule that holds it
  (`the inline action in rule r`, at the position of the action). Inside a type
  in a message the inline action stands for the type of its result
  (`node[(int, int)]`), and `pego sample -coverage` reports its rule as `the
  inline action in rule r` (`inline action n in rule r` when `r` holds several)
  and its alternatives as written.

## Implementation

`lowerInlineActions` (`internal/engine/inline_actions.go`) replaces each inline
action by a reference to a hidden rule placed after the rule that holds it,
before attribute normalization. It runs once, in the compiler, so the closure
and bytecode VMs and the generated Go (generic, typed and direct) parsers need
no case of their own for it. The program keeps the grammar as written
(`Program.SourceGrammar`, returned by `Parser.Grammar`, in every mode including
default values) and serializes it: the inline actions stay in the AST of a
compiled grammar, and a loader lowers them in the same way before applying the
stored rule flags, which are those of the lowered rules.

A hidden rule without an action is a splice rule (`rule.splice`). The shape
functions (`value_shape.go`) and the value types treat a call of it as the group of
its body, and the rule itself returns its body's result as it is: the tuple node
of its elements, which its callers flatten, or its one element. The bytecode
instruction `SPREAD` pushes the elements of that result after the call, the
closure matcher and the generated code flatten the children of the tuple node, and
a node that takes the tuple as its own takes a copy, since a memo entry shares it.
Rules record the text that names them in a failed action (`RuleInfo.Where`) and
whether they splice (`RuleInfo.Splice`).

The differential tests compare each grammar written with inline actions against
the same grammar with explicit hidden rules, and each hidden rule without an action
against its expression written in place, on every VM backend, and run the grammars in
the corpus of generated parsers.

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

An inline braced Value Action block is rejected. The name of the hidden rule is visible in trace events, profiles and source information.
