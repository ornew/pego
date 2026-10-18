# Rule Expressions

The right-hand side of a rule is a Rule Expression. This guide is about writing the matching part of a grammar:
choosing between alternatives, repeating, looking ahead, committing with a cut, handling white space and tokens,
and reading keywords. Each section ends with the mistake that goes with it. What the expressions produce is the
subject of [Trees and Actions](trees-and-actions.md); the exact meaning of every expression is defined in
[Rule Expressions](../../spec/parser-expressions.md).

All examples run with `pego parse -g <file> -s <rule> -f sexpr -i <input>`.

## Ordered Choice

`a / b` tries `a`, and only if `a` fails tries `b`. The first alternative that succeeds wins, and the choice never
returns to try another one, even if the rest of the sequence then fails. This is the largest difference from a
context-free grammar, where `<` and `<=` can be listed in any order.

```pego
// choice.pego
rule bad  = "<" / "<="
rule good = "<=" / "<"
rule m1 = bad $$
rule m2 = good $$
```

```bash
pego parse -g choice.pego -s m1 -f sexpr -i '<='
pego parse -g choice.pego -s m2 -f sexpr -i '<='
```

```text
pego: 1:2: syntax error: expected end of input
(Seq "<="@good)@m2
```

`bad` matches `<` and succeeds, so the `=` is left over. Put the longer alternative first, or the more specific one,
whenever one alternative matches a prefix of another. `pego lint` finds such alternatives and reports them as
shadowed (see [Linting Grammars](linting.md)).

## Repetition and Lookahead

`a*` and `a+` take as many iterations as they can and never give any back. A sequence that needs the last
iteration for itself therefore cannot work:

```pego
// greedy.pego
rule x = [a-z]* [a-z] $$
```

```bash
pego parse -g greedy.pego -s x -f sexpr -i 'abc'
```

```text
pego: 1:4: syntax error: expected [a-z]
```

`[a-z]*` consumes `abc`, and there is nothing left for `[a-z]`. State where the repetition ends instead. A negative
lookahead `!a` succeeds without consuming input when `a` does not match, which makes "everything up to a marker" a
repetition of "not the marker, then any character":

```pego
// until.pego
rule b = (!"y" .)* "y" $$
```

```bash
pego parse -g until.pego -s b -f sexpr -i 'abcy'
```

```text
(Seq [(Seq "a") (Seq "b") (Seq "c")] "y")@b
```

`&a` is the positive form: it requires `a` to match at the position and consumes nothing. Lookahead leaves no value
in the tree and does not move the position, whatever the expression inside it does.

## Cut

A choice commits to the first alternative that succeeds. A cut, `#cut`, commits earlier: once parsing has passed it,
the other alternatives of the innermost enclosing choice are not tried. Use it where the start of an alternative
already tells which construct is meant.

```pego
// cut.pego
rule nocut   = "if" ws "(" ws "x" ws ")" / "if" ws "x"
rule withcut = "if" ws #cut "(" ws "x" ws ")" / "if" ws "x"
rule m3 = nocut $$
rule m4 = withcut $$
rule ws = [ \t]*
```

```bash
pego parse -g cut.pego -s m3 -f sexpr -i 'if x'
pego parse -g cut.pego -s m4 -f sexpr -i 'if x'
```

```text
(Seq (Seq "if" [" "]@ws "x")@nocut)@m3
pego: 1:4: syntax error: expected "(", [ \t]
```

Without the cut, `if x` falls through to the second alternative and parses as a different construct. With it,
the grammar says that after `if` a parenthesis must follow, and the error names it. A cut also keeps a repetition from
stopping quietly: if an iteration fails after a cut, the whole repetition fails instead of ending before that
iteration. The scope of a cut is defined in [Cut](../../spec/parser-expressions.md#cut).

## White Space and Tokens

A PEG has no separate tokenizer, so white space is written into the grammar. The usual convention is that a token
rule matches its text and trailing white space, or that a `ws` rule is placed explicitly between the elements of a
sequence. Whichever you choose, apply it everywhere, because the farthest-failure error report and the positions
of nodes follow where white space is consumed.

```pego
// list.pego
rule list = "[" ws (item (ws "," ws item)*)? ws "]" $$
rule item = [0-9]+ #atomic
rule ws = [ \t]*
```

```bash
pego parse -g list.pego -s list -f sexpr -i '[1, 2,3]'
pego parse -g list.pego -s list -f sexpr -i '[1, 2,]'
```

```text
(Seq "[" []@ws (Seq "1"@item [(Seq []@ws "," [" "]@ws "2"@item) (Seq []@ws "," []@ws "3"@item)]) []@ws "]")@list
pego: 1:7: syntax error: expected [ \t], [0-9]
```

Mark the rules that read a token as `#atomic` so that the token is one terminal and not a sequence of characters, and
discard white space with `-` when it should not appear in the tree (see [Trees and Actions](trees-and-actions.md)).
White space inside a tree is only noise for the program that reads it.

## Keywords and Word Boundaries

A literal matches text, not words. `"in"` also matches the start of `index`, so a keyword followed by an identifier
silently accepts a longer word:

```pego
// keyword.pego
rule kwbad  = "in" ws id $$
rule kwgood = "in" !idc ws id $$
rule id  = [a-z]+ #atomic
rule idc = [a-z0-9_]
rule ws  = [ \t]*
```

```bash
pego parse -g keyword.pego -s kwbad -f sexpr -i 'index'
pego parse -g keyword.pego -s kwgood -f sexpr -i 'index'
pego parse -g keyword.pego -s kwgood -f sexpr -i 'in index'
```

```text
(Seq "in" []@ws "dex"@id)@kwbad
pego: 1:1: syntax error
(Seq "in" [" "]@ws "index"@id)@kwgood
```

Check the boundary with a negative lookahead `!idc` after the keyword. Do the reverse for identifiers: an identifier
rule must not match a keyword, so that `in` is not read as a name (`rule id = !kw [a-z]+`). The bare
`syntax error` of the second command comes from the lookahead, which records no expected item; see the next section.

## Writing for Good Errors

PEGO reports the farthest failure of a parse with everything that was expected there. The shape of the grammar
decides how good that report is:

- Fail as late as possible. A cut after the first distinguishing token keeps a later alternative from reporting a
  failure earlier in the input.
- Label what a person would call the thing. `@error(message="expected an expression")` replaces a long list of
  tokens with one sentence.
- Give a failed predicate or lookahead a label, because they record no expected item and otherwise leave only
  `syntax error`.

[Errors and Recovery](errors-and-recovery.md) covers labels, cuts, recovery and the other tools in detail.

## Pitfalls

| Pitfall | What happens | What to do |
|:--|:--|:--|
| Listing a short alternative before a longer one | The short one wins and the rest is left over, as in `"<" / "<="` | Put the longer or more specific alternative first |
| `a* a` | The repetition consumes everything `a` could match, so the last `a` fails | Describe where the repetition ends, for example with `(!end a)* end` |
| Keywords as bare literals | `"in"` matches the start of `index` | Follow the keyword with `!idc` and exclude keywords from identifiers |
| Left recursion with the base case first | The rule never reaches the recursive alternative | Put the recursive alternative first, or use an [Operators Rule](operators-rule.md) for operators |
| A cut that is too early | A valid input fails because no other alternative is tried | Place the cut after the token that distinguishes the alternative |
| White space handled in some places only | Inputs with a space or a newline fail in the places that forgot it | Choose one convention and apply it to every token and separator |
| Expecting backtracking into a repetition or choice | The choice or repetition is not re-evaluated for a later failure | Restructure the grammar with lookahead; PEG never gives iterations back |

## Related Topics

- [Trees and Actions](trees-and-actions.md): what the expressions produce and how to shape it.
- [Operators Rule](operators-rule.md): operator expressions and precedence.
- [Context-Sensitive Parsing](context-sensitive.md): predicates, variables and lookahead for context-dependent
  syntax.
- [Errors and Recovery](errors-and-recovery.md): labels, cuts and recovery for readable errors.
- [Linting Grammars](linting.md): finding unreachable alternatives and other likely mistakes.
