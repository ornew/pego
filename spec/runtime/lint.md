# Grammar Lint

The linter reads a grammar and reports likely mistakes that the compiler accepts:
an alternative that can never match, a capture that is never read, a rule that
nothing calls. It does not parse input. This chapter defines the entry points,
the severities, each check and the comment directives that suppress findings.

## Scope and Terminology

A **finding** is one report of the linter. It has a position (`Pos`, the line and
column of the construct, zero for a grammar without source positions), a
severity, the name of a check, the name of the rule that contains the construct,
a message and, when one exists, a suggested fix. A **check** is one rule of the
linter, identified by a name.

## Contract

### Entry Points

| | Command line | Go |
|:--|:--|:--|
| Lint a grammar | `pego lint -g g.pego` | `lint.Config.Run(ctx, g, start)` |
| Machine-readable output | `-f json` | `[]lint.Finding` |
| Turn checks off | `-disable check,...` (or the flag repeated) | `lint.Config.Disable` |
| Fail on warnings too | `-strict` | Check `Finding.Severity` |
| List the checks | `-list` | `lint.Checks()` |

- `lint.Config.Run` takes a context, a grammar AST and the start rule and returns the findings.
  If the grammar does not compile, or the start rule is not a start rule of the grammar, it returns
  an error and no findings. Cancelling the context stops the compilation and the checks and
  discards all findings. An empty start rule selects the default start rule.
- `lint.Config.RunUnchecked` returns the same findings as `Run` for a grammar that compiles and a
  start rule of it, without compiling the grammar. It does not check either condition; its findings
  for any other input are unspecified. Cancelling its context discards all findings.
- `pego lint` accepts PEGO source and grammar JSON; a compiled grammar holds no
  grammar and is an error. The start rule is the one `-s` names, otherwise the
  default start rule of the grammar. A grammar that does not compile is reported with its compile
  errors.
- Each finding prints on one line as `file:line:col: severity: message [check]`,
  followed by a line `fix: ...` when there is a suggested fix. A run that exits
  with status 1 ends with the error line `pego: lint: N errors, N warnings, N hints`
  on the standard error, where each count is written as `1 error`, `1 warning` or `1 hint`
  when it is one and in the plural otherwise; a run that exits with status 0 prints no summary.
- `-f json` prints one JSON document with the members `grammar`, `start` and
  `findings`; each finding has `line`, `col`, `severity`, `check`, `rule`,
  `message` and, if present, `fix`.
- `pego lint` exits with status 1 if there is a finding of severity `error`, and
  with 0 otherwise. With `-strict`, a warning also gives status 1. A hint never
  does.
- A grammar decoded from JSON has no source
  positions: its findings have the zero `Pos` and name the rule they are in. Such
  a grammar has no comments, so it cannot carry [directives](#suppressing-findings).

### Severities

| Severity | Meaning |
|:--|:--|
| `error` | Certain: part of the grammar can never take effect |
| `warning` | The fact is certain, but the grammar may still do what was meant |
| `hint` | A suggestion about performance, mostly for incremental parsing; the grammar is correct as written |

An error or a warning is proven from the grammar alone; when the linter cannot be
sure, it reports nothing. The linter assumes that rule calls do not nest deeper
than the nesting limit, which can make any call fail.

### Checks

| Check | Severity | Reports |
|:--|:--|:--|
| `shadowed-alternative` | error | An alternative that can never match because an earlier one matches first |
| `never-matches` | error | Anchors that contradict their neighbors in a sequence; recursion without a base case |
| `useless-lookahead` | error, warning | `!x` that never succeeds (error), `&x` or `!x` that always succeeds (warning) |
| `nullable-repetition` | warning | A repetition whose element can match without consuming input |
| `redundant-optional` | warning | `x?` where `x` always succeeds |
| `growing-variable` | warning | A binding that can at least double the size of its variable on each recursion or iteration |
| `unused-capture` | warning | A capture whose value nothing reads |
| `duplicate-capture` | warning | A name captured twice in one match |
| `char-class` | warning | Overlapping ranges in a class; ranges such as `[A-z]` that include punctuation |
| `unreachable-rule` | warning | A rule that the start rule never calls, directly or indirectly |
| `right-recursion` | hint | A list written as right recursion |
| `positions` | hint | `.Start` or `.End` read in a rule that is used repeatedly |
| `long-lookahead` | hint | A lookahead that repeats `.` or a class that admits line breaks |
| `super-linear` | hint | A cause of cost that can grow faster than the input from the start rule |
| `case-insensitive-literal` | hint | A run of `[sS]`-style classes that spells a word |
| `lint-directive` | warning | A directive that names an unknown check or suppresses nothing |

#### `shadowed-alternative`

An alternative of an ordered choice can never match if an earlier alternative
matches wherever it could. It is reported in three cases:

1. An earlier alternative always succeeds, so later ones are never tried:
   `"+"? / "-"`.
2. An earlier alternative is a prefix of a later one: `"=" / "=="`, `word / "true"`,
   `ident / ident "(" args ")"`, `[0-9]+ / "0x" hex+`.
3. An alternative is the same as an earlier one.

The same reasoning applies to the operand items of an [Operators Rule](../language/operators-rule.md),
which form an ordered choice, and to its operators: the longest operator part wins,
and among operators that match the same text the one declared first wins, so a
second `infix left "+"` in one level is never selected.

Removing a dead alternative removes what it added to the expected items of a
syntax error. In a left-recursive rule, the engine decides how to grow the
recursion from the rules that each rule can call first, excluding bodies of
zero-count repetitions and choice suffixes whose earlier alternative is proven to
succeed on every input; possible recovery skip calls also take part. The linter
does not prove every input-dependent dead branch, so removing or moving such a
branch MAY change a parse if the branch calls a left-recursive rule before
consuming input.

#### `never-matches`

A sequence that cannot match: `items $$ "\n"` (nothing follows the end of the input),
`header ^^ body` (after `header` consumed input, this is not the beginning),
`text $ "x"` (`$` is followed by a line break or the end) and `"a" ^ "b"`.
A rule that can match only by matching itself first has no base case:
`rule list = item "," list`, `rule expr = expr "+" term`. A rule that fails only
because it calls such a rule is not reported. An explicit `_|_` is never reported.

#### `useless-lookahead`

`!"x"?` never succeeds because `"x"?` always does (error). `&ws*` has no effect
and `!_|_` always succeeds (warnings).

#### `nullable-repetition`

`item?*`, `word*+` and `line*` where `line` can match nothing. The engine ends a
repetition at the first iteration that consumes no input, so it does not loop, but
that iteration still adds an element to the list. A rule such as
`!$$ [^\n]* ("\n" / $$)` cannot match nothing and is not reported.

#### `redundant-optional`

`"x"*?`. An optional lookahead `(&x)?` is not reported, because it adds a `nil`
element that `&x` does not have.

#### `growing-variable`

A binding of a `string` variable whose expression joins the variable's own
value at least twice, as in `s := s + s`, when the variable can hold a
non-empty value and the bound value reaches a later evaluation of the same
binding. That is so, outside a negative lookahead, when a sequence of the rule
body has the binding in one element and a call that can reach its rule again
in a later element, each at any depth of groups, options and choices, or when
the binding is part of the element of a repetition in its rule. Each
evaluation can then at least double the size of the variable, so that its size
can grow exponentially with the input:

```pego
var s string = "a"

@start
rule R = "x" {s := s + s} R / ""
```

A variable can hold a non-empty value at the binding when its initial value is
not empty, or when a binding of a non-empty value is visible there: one that
precedes it in the same rule body, or in a rule that calls its rule, directly or
indirectly, before the call. A binding in a rule that has already returned, such
as one in `T` for `R` in `main = T R`, is not visible, because bindings in a
called rule are not visible to its caller. A binding that extends the variable
by a bounded piece, such as `indent := indent + "  "`, is not reported. Nor is a
binding after the recursive call, whose value no later evaluation of it sees,
nor one in a different alternative of a choice from the call, as in
`R = {s := s + s} "q" / "x" R / ""`, because a failed alternative undoes its
bindings before the next is tried. Bindings of `int` and `bool` variables are
not reported; their effect on the number of combinations of values is a matter
of cost, which `super-linear` reports.

#### `unused-capture`

A capture is reported when nothing can read its value: it is not referred to by
the action or a predicate of its scope, and the rule has no action: the capture of a
rule without an action names an element of the rule's value, and the list of a repetition whose action discards it is not read. A `map` or `foldl`
parameter is not a capture.

#### `duplicate-capture`

The same name captured twice in one match, as in `a:key "=" a:value`. For nested
captures the inner capture is stored first and the outer capture then overwrites
it; the finding points to the outer capture and identifies the inner one.
Alternatives of a choice exclude each other (`a:x / a:y` is not reported), and each
iteration of a repetition is a scope of its own.

#### `char-class`

`[a-zA-Za-f_]` (overlap) and `[A-z]` (matches the characters between `Z` and `a`).
The fix shows the class without the overlap or the punctuation.

#### `unreachable-rule`

A rule that the start rule never calls. The finding can be suppressed for a rule
or disabled as a check, which is the way to declare further entry points.

#### `right-recursion`

`rule lines = line lines?` and `rule args = expr ("," args)?`: a list of n items
nests n calls, and an edit of a `Document` evaluates again every call that begins
before the edit. A prefix operator (`"-" unary`) is not reported; a
right-associative binary operator written this way is.

#### `positions`

A rule that reads `.Start` or `.End` and is used repeatedly (in the element of
a repetition or on a recursion cycle): a `Document` cannot shift its results past
an edit, nor those of the rules that call it.

#### `long-lookahead`

A lookahead that repeats `.` or a negated class that admits line breaks, such as
`&([^!]* ($$ / "!"))`, can examine any amount of input. Lookaheads through other
rules are not reported.

#### `super-linear`

A cause of a cost that can grow faster than the length of the input, found by
the [classification](parsing.md#classification) of the start rule. There is one
finding per cause, at the scanning source, binding or operation that causes it.
Its message names the class and the witness path, as positions of the rules
and expressions on it. In

```pego
@start
rule main = (A / "a")* $$

rule A = "a"* "c"
```

the finding is at `"a"*` in `A` and says that the cost can grow as n²: when
`"c"` fails, the repetition in `main` evaluates `A` again inside the input that
`"a"*` examined.

Only causes of the classes polynomial and exponential are reported: a
witness that the cost can grow faster than the input, such as a growing
variable, a copy of a value of its own recursion, or a scan that can start
again inside the input that it examined. The finding is a hint because these
classes can be pessimistic: no slow input need exist. The class unknown is not
reported, because the classifier then has no such witness, only a condition
for linear cost that it cannot establish. It is shown, with that reason, only
when the classification is requested.

#### `case-insensitive-literal`

Two or more consecutive classes in a sequence that each match one ASCII letter
in both cases, such as `[sS] [eE] [lL] [eE] [cC] [tT]`, spell a word
case-insensitively. Write `"select"i` (or `"select" #ignorecase`). Any other
item between classes ends the run.

### Suppressing Findings

A comment that starts with `lint:ignore` followed by check names separated by commas,
and an optional reason, suppresses their findings.

| Placement | Scope |
|:--|:--|
| In the comments before a definition | The whole rule |
| Elsewhere | The line of the comment, if the comment ends the line, and the next line |
| `// lint:file-ignore check reason` | The whole grammar |

A directive that names an unknown check, or that suppresses nothing, is reported
as `lint-directive`.

## Validation and Errors

| Condition | Result |
|:--|:--|
| The grammar does not compile | `lint.Config.Run` returns the compile errors and no findings |
| `-disable` or `Config.Disable` names an unknown check | An error |
| A compiled grammar | Error: `lint needs the grammar source (.pego or .json)` |

## Compatibility and Support

The linter is available from the Go API and the `pego` command. Generated parsers
do not include it. The linter does not find what it cannot prove (a choice whose
alternatives start with overlapping characters, shadowing through predicates, an
Operators Rule with level-restricted calls, or left-recursive rules), mistakes the
compiler already rejects, anchors that conflict across rules, or the performance of
a grammar on real input.

## Conformance

The lint tests of the repository establish each check on positive and negative
grammars, including that the checks do not report the grammars that they must not.
