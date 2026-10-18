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
| Lint a grammar | `pego lint -g g.pego` | `pego.Lint(g, start, opts...)` |
| Machine-readable output | `-f json` | `[]pego.Finding` |
| Turn checks off | `-disable check,...` (or the flag repeated) | `pego.DisableChecks(...)` |
| Fail on warnings too | `-strict` | Check `Finding.Severity` |
| List the checks | `-list` | `pego.LintChecks()` |

- `pego.Lint` takes a grammar AST and the start rule and returns the findings. If
  the grammar does not compile, it returns an error and no findings.
- `pego lint` accepts PEGO source, grammar JSON and a compiled grammar that holds
  the AST. The start rule is `main` unless `-s` names another or a compiled
  grammar saved one. A grammar that does not compile is reported with its compile
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
- A grammar decoded from JSON or loaded from a compiled grammar has no source
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
| `unused-capture` | warning | A capture whose value nothing reads |
| `duplicate-capture` | warning | A name captured twice in one match |
| `char-class` | warning | Overlapping ranges in a class; ranges such as `[A-z]` that include punctuation |
| `unreachable-rule` | warning | A rule that the start rule never calls, directly or indirectly |
| `deprecated-any` | warning | A use of the deprecated type `any` |
| `right-recursion` | hint | A list written as right recursion |
| `positions` | hint | `startPos` or `endPos` read in a rule that is used repeatedly |
| `long-lookahead` | hint | A lookahead that repeats `.` or a class that admits line breaks |
| `lint-directive` | warning | A directive that names an unknown check or suppresses nothing |

#### `shadowed-alternative`

An alternative of an ordered choice can never match if an earlier alternative
matches wherever it could. It is reported in three cases:

1. An earlier alternative always succeeds, so later ones are never tried:
   `"+"? / "-"`.
2. An earlier alternative is a prefix of a later one: `"=" / "=="`, `word / "true"`,
   `ident / ident "(" args ")"`, `[0-9]+ / "0x" hex+`.
3. An alternative is the same as an earlier one.

The same reasoning applies to the operand items of an [Operators Rule](operators-rule.md),
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
child that `&x` does not have.

#### `unused-capture`

A capture is reported when nothing can read its value: it is not referred to by
the action or a predicate of its scope, and the rule has no action that would
expose it as a field of the rule's value. A terminal type keeps no captures, and
the list of a repetition whose action discards it is not read. A `map` or `foldl`
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

#### `deprecated-any`

Every reference to the type `any`, which is deprecated in favor of the top type
`_`. The message is "the type any is deprecated: use _, the top type", and the fix
reads "write _ and narrow its values with x.(T); pego fix rewrites the uses that
need no cast".

#### `right-recursion`

`rule lines = line lines?` and `rule args = expr ("," args)?`: a list of n items
nests n calls, and an edit of a `Document` evaluates again every call that begins
before the edit. A prefix operator (`"-" unary`) is not reported; a
right-associative binary operator written this way is.

#### `positions`

A rule that reads `startPos` or `endPos` and is used repeatedly (in the element of
a repetition or on a recursion cycle): a `Document` cannot shift its results past
an edit, nor those of the rules that call it.

#### `long-lookahead`

A lookahead that repeats `.` or a negated class that admits line breaks, such as
`&([^!]* ($$ / "!"))`, can examine any amount of input. Lookaheads through other
rules are not reported.

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
| The grammar does not compile | `pego.Lint` returns the compile errors and no findings |
| `-disable` or `DisableChecks` names an unknown check | An error |
| A compiled grammar without the AST | Error: `the compiled grammar omits the AST` |

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
