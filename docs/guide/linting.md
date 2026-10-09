# Linting grammars

A PEG grammar can compile, parse most inputs, and still contain an alternative that can never match: an ordered choice
commits to the first alternative that matches, so in `"=" / "=="` the second alternative is dead, and in
`word / "true"` the keyword never comes out as a keyword. Such mistakes show up much later, as a wrong tree for some
input. The linter finds them, and other likely mistakes, by reading the grammar alone.

| | Command line | Go |
|:--|:--|:--|
| Lint a grammar | `pego lint -g g.pego` | `pego.Lint(g, "main")` |
| Machine-readable output | `-f json` | `[]pego.Finding` |
| Turn checks off | `-disable check,...` | `pego.DisableChecks(...)` |
| Fail on warnings too | `-strict` | check `Finding.Severity` |
| List the checks | `-list` | `pego.LintChecks()` |

Findings can also be suppressed with comments in the grammar ([Suppressing findings](#suppressing-findings)). The
design, and how the checks are kept free of false positives, is in
[design record 018](../design/018-grammar-linting.md).

- [On the command line](#on-the-command-line)
- [From Go](#from-go)
- [Severities](#severities)
- [The checks](#the-checks)
- [Suppressing findings](#suppressing-findings)
- [What the linter does not find](#what-the-linter-does-not-find)

## On the command line

Take a list whose items may be words, numbers or booleans:

```pego
def main = item ("," item)*
def item = word / number / "true" / "false"
def word = @(?a-z)+
def number = @(?0-9)+
```

```bash
$ pego lint -g items.pego
items.pego:2:28: error: alternative 3 (`"true"`) can never match: alternative 1 (`word` at 2:12) matches first wherever it could [shadowed-alternative]
	fix: move it before alternative 1
items.pego:2:37: error: alternative 4 (`"false"`) can never match: alternative 1 (`word` at 2:12) matches first wherever it could [shadowed-alternative]
	fix: move it before alternative 1
pego: lint: 2 errors, 0 warnings, 0 hints
```

`word` matches `true` itself, so the parser never reaches the keywords. Each finding is one line,
`file:line:col: severity: message [check]`, followed by a suggested fix when there is one. Here the fix is to try the
keywords first, and to make sure that they are whole words: `def item = ("true" / "false") !(?a-z) / word / number`.
(`pego sample -coverage` finds the same mistake by generating inputs: see
[Sampling inputs and fuzzing](sampling-and-fuzzing.md#coverage).)

`pego lint` exits with status 1 if it finds an error, and 0 otherwise; with `-strict`, warnings fail too. Hints never
fail. The grammar can be PEGO source, JSON or a compiled grammar with the AST; the start rule is `main` unless `-s`
says otherwise or a compiled grammar saved one. A grammar that does not compile is reported with its compile errors,
like the other commands do.

With `-f json`, the findings are printed as one JSON document:

```bash
$ pego lint -g items.pego -f json
{
  "grammar": "items.pego",
  "start": "main",
  "findings": [
    {
      "line": 2,
      "col": 28,
      "severity": "error",
      "check": "shadowed-alternative",
      "rule": "item",
      "message": "alternative 3 (`\"true\"`) can never match: alternative 1 (`word` at 2:12) matches first wherever it could",
      "fix": "move it before alternative 1"
    },
    ...
  ]
}
```

`-disable` turns checks off (`-disable right-recursion,positions`, or the flag repeated), and `-list` prints every
check with its severity.

## From Go

`pego.Lint` takes a grammar AST and the start rule. It returns an error, and no findings, if the grammar does not
compile:

```go
g, err := pego.ParseGrammar(src)
if err != nil {
	log.Fatal(err)
}
findings, err := pego.Lint(g, "main", pego.DisableChecks("right-recursion"))
if err != nil {
	log.Fatal(err) // compile errors
}
for _, f := range findings {
	fmt.Println(f) // 2:28: error: alternative 3 (`"true"`) can never match: ... [shadowed-alternative]
	if f.Severity == pego.SeverityError {
		// f.Pos, f.Rule, f.Check, f.Message, f.Fix
	}
}
```

A test can keep a grammar free of mistakes:

```go
func TestGrammarLint(t *testing.T) {
	g, err := pego.ParseGrammar(grammarSource)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := pego.Lint(g, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Severity >= pego.SeverityWarning {
			t.Error(f)
		}
	}
}
```

A grammar decoded from JSON or loaded from a compiled grammar has no source positions; its findings have the zero
`Pos` and name the rule they are in.

## Severities

| Severity | Meaning |
|:--|:--|
| `error` | Certain: part of the grammar can never take effect. An alternative that can never match, an expression that can never match. |
| `warning` | Certain about the fact, though the grammar may still do what was meant: a capture that is never read, a repetition whose element can match nothing, a rule that is never used. |
| `hint` | A suggestion about performance, mostly for [incremental parsing](incremental.md#writing-grammars-that-reuse-well). The grammar is correct as written. |

Errors and warnings are proven from the grammar: when the linter cannot be sure, it says nothing. That means it misses
some mistakes (see [What the linter does not find](#what-the-linter-does-not-find)), but what it reports is worth
fixing. It assumes that rule calls do not nest deeper than the [nesting limit](runtime.md), which can make any call
fail.

## The checks

| Check | Severity | Finds |
|:--|:--|:--|
| [`shadowed-alternative`](#shadowed-alternative) | error | Alternatives that can never match because an earlier one matches first |
| [`never-matches`](#never-matches) | error | Anchors that contradict their neighbors; recursion without a base case |
| [`useless-lookahead`](#useless-lookahead) | error, warning | `!x` that never succeeds, `&x` or `!x` that always does |
| [`nullable-repetition`](#nullable-repetition) | warning | A repetition whose element can match without consuming input |
| [`redundant-optional`](#redundant-optional) | warning | `x?` where `x` always succeeds |
| [`unused-capture`](#unused-capture) | warning | Captures whose values are discarded |
| [`duplicate-capture`](#duplicate-capture) | warning | A name captured twice in one match |
| [`char-class`](#char-class) | warning | Overlapping ranges; `(?A-z)` and similar ranges |
| [`unreachable-rule`](#unreachable-rule) | warning | Rules the start rule never calls |
| [`right-recursion`](#right-recursion) | hint | Lists written as right recursion |
| [`positions`](#positions) | hint | `startPos` and `endPos` in rules used repeatedly |
| [`long-lookahead`](#long-lookahead) | hint | Lookaheads that scan any character |
| [`lint-directive`](#suppressing-findings) | warning | `lint:ignore` comments that name an unknown check or suppress nothing |

### `shadowed-alternative`

An alternative of an ordered choice can never match if an earlier alternative matches wherever it could: the choice
commits to the earlier one. The linter proves it in three ways.

An earlier alternative always succeeds, so the later ones are never even tried:

```pego
def sign = "+"? / "-"        // "+"? matches nothing when there is no "+": "-" is never tried
```

Fix it by making the alternative fail where it matches nothing (`"+" / "-"`, then `sign?` where the sign is
optional).

An earlier alternative is a prefix of a later one:

```pego
def op = "=" / "=="                    // "=" matches the start of "=="
def arrow = "-" / "->"
def call = ident / ident "(" args ")"  // the same first item, and nothing more
def num = (?0-9)+ / "0x" hex+          // (?0-9)+ matches the "0" of "0x"
def item = word / "true"               // word = @(?a-z)+ matches "true"
```

Put the longer alternative first: `"==" / "="`, `ident "(" args ")" / ident` (or `ident ("(" args ")")?`), and
keywords before identifiers, with a check that the keyword is a whole word (`"true" !(?a-z0-9_) / word`).

An alternative is the same as an earlier one (a duplicate). The same reasoning applies to the `operand` items of a
Pratt expression, which form an ordered choice, and to Pratt operators: the longest operator part wins, and among
operators that match the same text the one declared first, so a second `infix left "+"` in the same level is never
selected.

Two things to know when you remove or move a dead alternative:

- It no longer adds what it expected to syntax errors (`expected "=", "=="` becomes `expected "="`), which is usually
  better.
- In a left-recursive rule, the engine decides how to grow the recursion from the rules that each rule can call first,
  excluding zero-count repetition bodies and choice suffixes whose earlier alternative is proven to succeed on every
  input. Possible recovery skip calls also participate. The compiler does not prove every input-dependent dead branch;
  removing or moving such a branch can still change the parse if it calls a left-recursive rule before consuming input.
  The fix cautions about these calls; compare the results.

### `never-matches`

An anchor that contradicts its neighbors in a sequence makes the sequence impossible:

```pego
def file = items $$ "\n"     // nothing can follow the end of the input
def doc = header ^^ body     // header consumed input, so this is not the beginning
def line = text $ "x"        // $ is followed by a line break or the end, not "x"
def l2 = "a" ^ "b"           // after "a", this is not the beginning of a line
```

Move `$$` to the end (`items "\n"? $$`), and `^^` to the beginning.

A rule that can only match by matching itself first has no base case and never matches:

```pego
def list = item "," list     // every list needs a longer list
def expr = expr "+" term     // left recursion that never starts
```

Add an alternative that does not recurse: `def list = item ("," list)?`, `def expr = expr "+" term / term`. Rules
that fail only because they call such a rule are not reported.

An explicit `_|_` is never reported: it is written on purpose, as in `_|_ #error(message="...")`.

### `useless-lookahead`

```pego
def a = !"x"? y      // error: "x"? always succeeds, so !"x"? never does
def b = &ws* y       // warning: ws* always succeeds, so &ws* has no effect
def c = !_|_ y       // warning: _|_ never matches, so !_|_ always succeeds
```

The usual cause is an operand that can match nothing (`?`, `*`): negate the expression itself (`!"x"`).

### `nullable-repetition`

```pego
def list = (item?)*          // item? can match nothing
def text = (word*)+
def lines = line*            // def line = (?^\n)* "\n"? can match nothing too
```

The engine stops a repetition at the first iteration that consumes no input, so these do not loop forever, but that
iteration still adds an element to the list (a `nil`, or an empty match), and the grammar usually meant `item*`,
`word*`, or a line that consumes at least its line break. The linter knows that a line such as
`!$$ (?^\n)* ("\n" / $$)` cannot match nothing (it is not at the end, so it needs the `"\n"`), so it does not report
`line*` for it.

### `redundant-optional`

```pego
def a = ("x"*)? y            // "x"* always succeeds: the ? does nothing
```

Remove the `?`. An optional lookahead (`(&x)?`) is not reported: it adds a `nil` child that `&x` does not have.

### `unused-capture`

A capture is reported when nothing can read its value:

```pego
def ret = "return" v:(ws x:expr)? -> new Return{Value: $x}   // v: is never read
def kv = k:key "=" v:value -> $k                             // v: is never read
def num: Number = d:@(?0-9)+                                 // a terminal type keeps no captures
def all = (x:item)* -> nil                                   // the list of the repetition is discarded
```

The first is the most common: the captures inside the optional group belong to the rule itself, so the outer `v:` is
not needed. A capture counts as read when the action or a predicate of its scope refers to it (a `map` or `foldl`
parameter of the same name does not count), or, in a rule without an action, as a field of the rule's value.

### `duplicate-capture`

```pego
def pair = a:key "=" a:value       // the second a: overwrites the first
```

Alternatives of a choice exclude each other (`a:x / a:y` is fine), and each iteration of a repetition is a scope of its
own.

### `char-class`

```pego
def id = (?a-zA-Za-f_)       // a-f overlaps a-z
def word = (?A-z)+           // A-z also matches [\]^_` between Z and a
```

The fix shows the class without the overlap, or without the punctuation: `(?A-Za-z)`.

### `unreachable-rule`

Rules that the start rule never calls, directly or indirectly. A grammar with several entry points
(`Parser.WithStart`) can suppress the finding for those rules, or disable the check.

### `right-recursion`

```pego
def lines = line lines?
def args = expr ("," args)?
```

Each call spans the rest of the list, so a list of n items nests n calls deep, and after an edit a `Document`
evaluates again every call that begins before the edit (503 of 1,000 lines in the measurement in
[Writing grammars that reuse well](incremental.md#writing-grammars-that-reuse-well)). Write the list as a
repetition: `line*`, `expr ("," expr)*`. A prefix operator (`"-" unary`) is not reported, but a right-associative
binary operator written this way is; a hint, not a mistake.

### `positions`

A rule that reads `startPos` or `endPos` and is used repeatedly (in the element of a repetition, or on a recursion
cycle): a `Document` cannot shift its results past an edit, nor those of the rules that call it, so it evaluates them
again after every edit before them. Take positions from the tree instead (`Node.Start` and `Node.End` are shifted for
you).

### `long-lookahead`

```pego
def line = text "\n" &((?^!)* ($$ / "!"))   // looks ahead to the next "!", across lines
```

A lookahead that repeats `.` or a negated class that admits line breaks can examine any amount of input, and an edit
anywhere in what it examined invalidates the rule's results in a `Document`. Lookaheads through other rules
(`&(ws ":")`) are not reported: those are usually whitespace and tokens, which end soon.

## Suppressing findings

A comment that starts with `lint:ignore` and the names of checks suppresses their findings, with an optional reason:

```pego
// lint:ignore unreachable-rule called with WithStart("expr") by the REPL
def expr_entry = expr $$

def stmt = s:simple k:keyword -> $s // lint:ignore unused-capture k is kept for the tree view

def main =
    // lint:ignore nullable-repetition,unused-capture an empty item marks the end
    items:(item?)* $$ -> nil
```

- In the comments before a definition, the directive applies to the whole rule.
- Elsewhere, it applies to its own line (a comment at the end of a line) and to the next line.
- `// lint:file-ignore check reason` applies to the whole grammar.

A directive that names an unknown check, or that suppresses nothing, is reported as `lint-directive`, so that
suppressions do not outlive what they excused. Grammars without comments (JSON, compiled grammars) use `-disable` or
`pego.DisableChecks` instead.

## What the linter does not find

- Mistakes it cannot prove: a choice between alternatives whose first characters overlap (`"ab" / "ac"`) is fine;
  shadowing through predicates, Pratt expressions, level-restricted calls and left-recursive rules is not analyzed.
- Mistakes the compiler already rejects: undefined rules and captures, captures inside `!`, `@` or `-`, type errors
  in actions, unknown escapes.
- Anchors across rules: `def a = "x" $$` called as `a "y"` is not reported.
- Performance on real input: use [`pego profile`](debugging.md) for that.
