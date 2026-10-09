# 018. Grammar Linting

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-08

## Summary

`pego.Lint` and `pego lint` report likely mistakes in a grammar that compiles: alternatives of an ordered choice that
can never match, expressions that can never match or have no effect, captures whose values are discarded, rules the
start rule never uses, and grammar shapes that slow down incremental parsing. Each finding has a severity, the name of
the check that reported it, a message, the position in the source, the rule it is in, and usually a suggested fix.
Comments of the form `// lint:ignore check reason` suppress findings. The guide [Linting grammars](../guide/linting.md)
lists the checks with examples.

```go
fs, err := pego.Lint(g, "main")                                 // err: the grammar does not compile
fs, err = pego.Lint(g, "main", pego.DisableChecks("right-recursion"))
for _, f := range fs {
	fmt.Println(f) // 3:18: error: alternative 2 ("ab") can never match: ... [shadowed-alternative]
}
```

```
$ pego lint -g grammar.pego
grammar.pego:3:18: error: alternative 2 (`"ab"`) can never match: alternative 1 (`"a"` at 3:12) matches first wherever it could [shadowed-alternative]
	fix: move it before alternative 1
```

## Motivation

PEG mistakes are silent. An ordered choice commits to the first alternative that matches, so `"=" / "=="` never
matches `==` as one token, and `ident / "if"` never produces the keyword; the grammar compiles, parses most inputs,
and fails on some much later, far from the cause. The same holds for `!x?` (which never succeeds), `$$ "x"` (which
never matches), a repetition of something that can match nothing, or a capture that the action forgets to read.
`pego profile` finds where a grammar is slow on a given input; nothing pointed out these mistakes before a test did.

## Principles

**Findings of severity error and warning are proven, not guessed.** Every analysis is an over- or an
under-approximation chosen so that what a check reports is certain: "not nullable" comes from an over-approximation of
nullability, "always succeeds" from an under-approximation of success. A check that cannot prove its case stays silent.
The only heuristic findings are the performance hints, which have their own severity.

**Severity says how sure the finding is, and whether the grammar is wrong.**

| Severity | Meaning | `pego lint` exit status |
|:--|:--|:--|
| error | Certain: part of the grammar can never take effect (a dead alternative, an expression that can never match) | 1 |
| warning | Certain about the fact (a capture is never read, an element can match nothing), but the grammar may still do what was meant | 0 (1 with `-strict`) |
| hint | A suggestion about performance; the grammar is correct as written | 0 |

**Lint runs on grammars that compile.** `Lint` compiles the grammar first and returns the compile errors instead of
findings. Checks can then assume that rules exist, captures are well placed and types check; the type checker already
rejects most mistakes in actions (an undefined capture, a missing field), so the linter does not repeat them.

**The nesting limit is ignored.** Any rule call can fail when calls nest deeper than `WithMaxDepth`; no claim of the
form "always succeeds" would survive that. The analyses assume the limit is not reached, as a grammar author does.

## Analyses

The checks share one analysis of the grammar AST (`internal/lint/analysis.go`). It works on the AST alone, so it
applies to grammars from source, JSON or a `.pegoc` with the AST (without source positions, findings then have none).

- **nullable** (may succeed without consuming input) is a least fixed point over the rules, as in the engine, but
  computed separately for "at the end of the input" and "elsewhere". Without that split, a line rule such as
  `!$$ field ("\n" / $$)` looks nullable (its last item can match nothing at the end), while it can match nothing
  nowhere: `!$$` excludes the end. `examples/csv` is written exactly so, and the split is what keeps it quiet.
- **canMatch** (may succeed at all) is a least fixed point: a rule that can only match by matching itself first, as in
  `def a = "(" a ")"`, never matches. A second fixed point, in which `_|_` and `!` of an expression that always
  succeeds are taken to match, leaves only the rules whose sole problem is recursion without a base case.
- **matchPrefix(e, t)** tells whether `e` certainly succeeds on every input that begins with the string `t`, and if
  so whether it consumes exactly a known part of `t`. With `t = ""` it is "always succeeds". It follows rule calls,
  respects cuts (a cut can make an optional expression, a repetition or a choice fail, unless a nested choice,
  repetition, optional expression or lookahead absorbs it), and treats predicates, anchors, Pratt expressions and calls
  of left-recursive rules as never certain.
- **fails(e, t)** is its counterpart for certain failure, used for negative lookaheads (`"if" !(?a-z)` is not certain
  to succeed on `if`).
- **prefix(e)** is a string that every match of `e` begins with (an under-approximation: often `""`), and whether
  every match is exactly that string.
- **same(x, y)** compares expressions structurally, ignoring `@`, `-`, `#error` and `#stream`, which do not change what
  matches, and capture names when neither side has a predicate.

Calls of left-recursive rules are never certain to succeed because, while a rule grows its left recursion, a call of
it inside the cycle returns the match found so far, which is a failure at first. `def r = r / "a"` matches `a`
through its second alternative, although "`r` matches wherever `"a"` does" holds for the finished rule. The randomized
test below found this case.

## Checks

| Check | Severity | What is proven |
|:--|:--|:--|
| `shadowed-alternative` | error | An alternative never matches because an earlier one matches first wherever it could (or it is never tried at all) |
| `never-matches` | error | An anchor contradicts its neighbors, or a rule only matches by matching itself first |
| `useless-lookahead` | error, warning | `!x` never succeeds (x always does); `&x` always succeeds; `!x` always succeeds (x never matches) |
| `nullable-repetition` | warning | The element of a repetition can match without consuming input |
| `redundant-optional` | warning | `x?` where x always succeeds and has a value |
| `unused-capture` | warning | A capture whose value cannot be read |
| `duplicate-capture` | warning | One match captures the same name twice |
| `char-class` | warning | Overlapping ranges; a range between letters or digits of different kinds |
| `unreachable-rule` | warning | The start rule never calls the rule |
| `right-recursion` | hint | A rule continues a list by calling itself at its end |
| `positions` | hint | A rule that is used repeatedly reads `startPos` or `endPos` |
| `long-lookahead` | hint | A lookahead repeats over any character |
| `lint-directive` | warning | A `lint:ignore` comment names an unknown check or suppresses nothing |

### `shadowed-alternative`

For alternatives `x` before `y` in a choice, `y` can never match if `x` matches wherever `y` could. The check proves it
in three ways:

1. `x` always succeeds (`x?`, `x*`, `_`, `""`): the alternatives after it are never even tried.
2. `x` and `y` begin with the same items (by `same`), and `x` has no more: `ident / ident "(" args ")"`. Both start
   in the same state at the same position, so if `y` matches, its first items matched, and so does `x`. Duplicates are
   the case where `y` has no more items either.
3. After the common items, the rest of `x` certainly succeeds on every input that begins with `prefix` of the rest of
   `y`: `"a" / "ab"`, `"-" / "->"`, `ident / "if"` (`(?a-z)+` succeeds on any input that begins with `if`),
   `(?0-9)+ / "0x" hex+`.

The same reasoning covers the operands of a Pratt expression (an ordered choice), and Pratt operators: the longest
operator part wins and the first declared among equals, so a second operator of the same kind group (prefix, or
infix and postfix) with the same expression is never selected. Operators of different levels are compared only when
the rule is never called with a level, since `expr(level)` can leave out the first one.

The suggested fix is to move the alternative before the one that shadows it (or remove a duplicate). Two caveats are
not visible in the finding itself:

- A dead alternative still adds what it expected to syntax errors (`expected "a", "ab"`), so removing it changes error
  messages (for the better).
- The engine decides how to grow left recursion from the rules that each rule can call first, counting calls in
  alternatives that are never tried. Removing such an alternative can change which rule grows the recursion, and so
  the parse result, as the randomized test showed. When the dead alternative calls a left-recursive rule before
  consuming input, the fix says so.

Overlapping first characters alone (`"ab" / "ac"`) are not reported: both can match.

### `never-matches`

- `$$` followed by items that cannot match without consuming input at the end of the input; `^^` after items that
  must consume input; `$` followed by items that must begin with a character other than a line break; `^` after items
  that always consume the same text, which does not end with a line feed.
- Recursion without a base case: rules every match of which would contain a match of themselves (or of another such
  rule), as in `def list = item "," list`. The rules that cannot match even if `_|_` matched are grouped by mutual
  recursion, and a group is reported only if it still cannot match when every rule outside it is assumed to. A rule
  that cannot match only because it calls such a rule, like `args = expr ("," args)?` with a broken `expr`, is not
  reported, although it calls itself.

An explicit `_|_` is never reported: writing it is deliberate (`_|_ #error(...)`, or a probe like
`compound_stmt _|_` in `examples/python`, which parses for its errors and then fails).

### `useless-lookahead`

`!x` where `x` always succeeds can never succeed, so the sequence around it never matches (`!x?` instead of `!x`):
an error. `&x` where `x` always succeeds, and `!x` where `x` can never match, have no effect: warnings. A positive
lookahead with captures is left alone, since captures made in it stay in effect.

### `nullable-repetition` and `redundant-optional`

The engine stops a repetition at the first iteration that consumes nothing, so `(x?)*` does not loop, but that
iteration still adds an element (`nil`) to the list, and the grammar usually meant `x*`. `x?` where `x` always succeeds
is the same as `x`, value included, when `x` has a value; when it has none (a lookahead), `x?` adds a `nil` child that
`x` does not, so it is not reported.

### `unused-capture` and `duplicate-capture`

A capture's value can be read by the action or by a predicate of its scope, or, without an action, as a field of the
rule's value. It cannot be read, and is reported, when:

- the rule (or Pratt line) has an action and neither the action nor a predicate of the scope refers to it (names of
  `map`/`foldl` parameters do not count);
- the rule has a terminal type and no action: the terminal keeps no captures;
- it is in the element of a repetition whose value is discarded: the repetition is not inside a capture, and the
  action uses no `$n`.

The common real case is `v:(ws x:expr)?` where the action reads `$x`: the inner capture belongs to the rule's scope,
so `v:` is unnecessary. `examples/python` has fourteen of them; they are reported (the test lists them) and not
silenced.

`duplicate-capture` reports a name captured twice in one match of a scope, in sequence or nested; alternatives of a
choice exclude each other and repetitions are scopes of their own. The later capture overwrites the earlier.

### `char-class` and `unreachable-rule`

`(?a-za-f)` and `(?__)` list characters twice (the fix shows the merged class). `(?A-z)` also matches
`` [\]^_` ``, and `(?0-Z)` the punctuation between digits and capitals: a range whose ends are letters or digits of
different kinds; the fix shows the class without the punctuation.

`unreachable-rule` is a warning, not an error: a grammar can have several entry points (`Parser.WithStart`). Rules only
called by unreachable rules are reported too, with a message that says so.

### Performance hints

They complement `pego profile`, which needs an input, with what the grammar's shape implies for `Document` reuse (see
[Writing grammars that reuse well](../guide/incremental.md#writing-grammars-that-reuse-well)):

- `right-recursion`: a rule that continues a list by calling itself at its end, after a call of another rule
  (`lines = line lines?`, `items = item ("," items)?`). Each call spans the rest of the list. Prefix operators
  (`"-" unary`) are left alone.
- `positions`: a rule that reads `startPos` or `endPos` and is called repeatedly (from the element of a repetition, or
  on a recursion cycle). The message counts the rules that call it, whose results cannot be shifted either.
- `long-lookahead`: a lookahead that itself repeats `.` or a negated class that admits a line feed, as
  `&((?^!)* ($$ / "!"))`. Repetitions in rules that the lookahead calls are not followed: whitespace and comment rules
  (`&(ws ":")`) end soon in practice, and following them reported every such lookahead in `examples/golang` and
  `examples/python`.

## Suppression

Findings are suppressed with comments, since the formatter keeps comments (`grammar.Grammar.AllComments`) and a
suppression belongs next to the code it excuses:

```pego
// lint:ignore unused-capture the value is kept for debugging
def stmt = s:simple k:keyword -> $s

def main = a:"x" b:"y" -> $a // lint:ignore unused-capture

// lint:file-ignore unreachable-rule several entry points
```

- `lint:ignore check[,check...] [reason]` in the comments before a definition applies to the whole rule; elsewhere it
  applies to its own line (a comment at the end of a line) and the next line.
- `lint:file-ignore` applies to the whole grammar.
- A directive that names an unknown check, or suppresses nothing, is reported (`lint-directive`), so suppressions do
  not outlive the code they excused. A directive for a disabled check is not reported as unused.

`DisableChecks` (`pego lint -disable`) turns checks off for the whole run, for grammars without comments (JSON) and for
projects that do not want a kind of finding at all.

## Testing

- Table tests give every check positive and negative cases, including the cuts, left recursion, captures read by
  predicates, and directives.
- `TestExamples` lints every grammar under `examples/` and compares the findings with a list checked by hand (the
  fourteen captures in `python.pego`); any other finding is a false positive.
- `TestSoundness` generates random grammars of three rules over `a`, `b` and the line feed, with choices, sequences,
  repetitions, lookaheads, anchors, cuts, `@`, `#error` and `#recover`. For each certain finding it changes the
  grammar in a way that is equivalent exactly when the finding is true (it keeps cuts and left calls: `x` becomes
  `x _|_`, `!x` becomes `!(x / _)`, `&x` becomes `&(x / _)`, `x?` becomes `x`), and, for shadowed alternatives, also
  applies the suggested removal where it is safe. The two grammars must parse every input of up to four characters the
  same way. It runs 3,000 grammars by default (`-soundness=40000` checked about 100,000 findings). It found four bugs
  during development: calls of left-recursive rules taken as certain, left recursion computed after results that
  depended on it had been memoized, `x?` taken as `x` for an `x` without a value, and the left-recursion caveat above.
- Deliberate failures: making optional expressions ignore cuts, nullability ignore `!`, or calls of left-recursive
  rules certain each makes the tests fail.

## Not checked

- **Actions that always produce nil, and captures that are never set**: the type checker infers the types of actions
  and rejects undefined captures at compile time; `-> nil` is deliberate.
- **Rules that defeat memoization**: memoization is decided per rule by the engine (left recursion, variables) and
  is not something a grammar author chooses; `pego profile` shows the cost on real input.
- **Escape mistakes**: the lexer already rejects unknown escapes such as `\d`.
- **Alternatives whose first characters overlap**: overlap alone is not a mistake.
- **Cross-rule anchors** (`def a = "x" $$` called as `a "y"`): only anchors and their neighbors in one sequence are
  checked.

## Alternatives considered

- **Reporting from `Compile`**: compile errors stop compilation; most findings should not, and hints need not be seen
  every time. A separate entry point keeps compilation fast and quiet.
- **A public `lint` package, like `sample`**: `sample` only needs the public API; the linter needs nothing internal
  either, but it is one function, so `pego.Lint` keeps it next to `Compile`, with the implementation in
  `internal/lint`.
- **Positions from the nearest leaf**: the AST used to record positions only for rule calls, literals, classes,
  captures and predicates. The linter needs to point at `!`, `?`, `*` and anchors, so the parser now records the
  positions of prefix operators, atoms and anchors, and of the operand that postfix operators apply to.
- **Ranges**: the AST has start positions only, so findings have a position, not a range.

## Limitations

- `matchPrefix` and `prefix` give up on predicates, Pratt expressions, level-restricted calls and left recursion, so
  shadowing through them is not reported.
- Comment directives need source; for JSON and compiled grammars use `DisableChecks`.
- The hints are heuristics: a right-recursive rule for a right-associative operator is correct, and a lookahead that
  repeats `.` may be bounded by what follows.
