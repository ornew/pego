# Testing and Linting a Grammar

You have been checking grammars by running them on a few inputs. This lesson turns those checks into a suite that
you can rerun, and adds two tools that find mistakes without any input: the linter and the formatter.

## What You Will Build

A fixture suite for the configuration grammar, a lint run that finds a real mistake, and a formatted grammar file.

## Before You Start

Complete [Building Typed Trees](typed-trees.md): you need `config.pego`.

## A Fixture Suite

A suite is a JSON file next to the grammar. Each case has an input and the outcome you expect: `accepted` for a clean
match, `rejected` for a syntax error, and `recovered` for a match that needed `#recover`. Save as `suite.json`:

```json
{
  "version": 1,
  "grammar": "config.pego",
  "backends": ["closure", "bytecode"],
  "equivalence": true,
  "cases": [
    {"name": "empty file", "input": "", "expect": {"outcome": "accepted"}},
    {"name": "typical file", "input": "host = \"localhost\"\nport = 8080\n", "expect": {"outcome": "accepted"}},
    {"name": "comment only", "input": "# nothing here\n", "expect": {"outcome": "accepted"}},
    {"name": "double equals", "input": "port == 8080\n", "expect": {"outcome": "rejected"}},
    {"name": "unterminated string", "input": "name = \"oops", "expect": {"outcome": "rejected"}}
  ]
}
```

```bash
pego test -f suite.json
```

```text
PASS empty file [closure]
PASS empty file [bytecode]
PASS typical file [closure]
PASS typical file [bytecode]
PASS comment only [closure]
PASS comment only [bytecode]
PASS double equals [closure]
PASS double equals [bytecode]
PASS unterminated string [closure]
PASS unterminated string [bytecode]
PASS empty file [backend equivalence: closure, bytecode]
PASS typical file [backend equivalence: closure, bytecode]
PASS comment only [backend equivalence: closure, bytecode]
PASS double equals [backend equivalence: closure, bytecode]
PASS unterminated string [backend equivalence: closure, bytecode]
all fixtures passed
```

Each case runs on both backends, and `"equivalence": true` also requires that the two backends produce the same tree and
errors. A failing case prints `FAIL` with what differed and the command exits with status 1, so the suite works in a
continuous integration job as it is. Cases can also pin the tree, the root range and the positions of errors; `pego test
-update` refreshes those expectations from the observed results.

## Linting

`pego lint` examines a grammar without running it. Save a grammar with two mistakes as `buggy.pego`:

```pego
rule main = "a" ws op ws "b" $$
rule op = "<" / "<="
rule ws = " "*
rule unused = "x"
```

```bash
pego lint -g buggy.pego
```

```text
buggy.pego:2:17: error: alternative 2 (`"<="`) can never match: alternative 1 (`"<"` at 2:11) matches first wherever it could [shadowed-alternative]
	fix: move it before alternative 1
buggy.pego:4:1: warning: rule unused is never used: main does not call it, directly or indirectly [unreachable-rule]
	fix: remove the rule, or call it from a rule that main reaches
pego: lint: 1 error, 1 warning, 0 hints
```

The first finding is the mistake of the previous lessons in disguise: `"<"` matches the start of `"<="`, so the
second alternative is unreachable. Each finding has a position, a severity, the name of the check in brackets and a
suggested fix. The command exits with status 1 if there is an error, and with `-strict` also for a warning. The grammars
of this tutorial are clean:

```bash
pego lint -g config.pego
pego lint -g robust.pego
```

A run with no findings prints nothing and exits with status 0.

## Formatting

`pego fmt` rewrites a grammar into the canonical layout and keeps comments. `-l` lists the files whose formatting
differs, `-w` rewrites them in place and `-check` lists them and exits with status 1, which suits CI:

```bash
pego fmt -l config.pego robust.pego
pego fmt -w config.pego
```

## Next Steps

[Shipping a Parser](shipping.md) turns the grammar into code that other programs can use. The guides
[Testing Grammars](../guide/testing-grammars.md) and [Linting Grammars](../guide/linting.md) cover properties, golden
files, fuzzing and every check, and the cookbook has recipes for [testing hand-written code against a
grammar](../cookbook/test-with-samples.md) and [linting in CI](../cookbook/lint-in-ci.md).
