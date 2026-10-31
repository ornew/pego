# Check Grammars in Continuous Integration

**Problem.** A grammar is code that other code depends on, and its mistakes show up late as a wrong tree for some input.
You want every change to be checked for formatting, lint findings and the fixtures, and the build to fail when one of
them breaks.

Run three commands, each of which exits with a nonzero status when it finds something: `zp fmt -check`,
`zp lint -strict` and `zp test`. Put them in one script that CI and developers both run.

Take a repository with grammars in `grammars/` and one fixture suite per grammar next to it:

```text
example.com/project
├── go.mod
└── grammars/
    ├── config.zp
    ├── config.suite.json     the fixtures of config.zp
    └── check.sh
```

Record `zp` as a tool of the module, so that the version is pinned and nothing has to be installed:

```bash
go get -tool github.com/ornew/zeroparse/cmd/zp
```

The script runs the checks on every grammar and reports all failures, not only the first:

```bash
#!/bin/sh
# grammars/check.sh
status=0
for g in grammars/*.zp; do
    go tool zp fmt -check "$g" || status=1
    go tool zp lint -strict -g "$g" || status=1
done
for s in grammars/*.suite.json; do
    go tool zp test -f "$s" || status=1
done
exit $status
```

Run it from the root of the repository, in CI as one step:

```yaml
# .github/workflows/grammars.yml
name: grammars
on: [push, pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.27.1"
      - run: sh grammars/check.sh
```

## Expected Output

For a clean grammar the script prints only what `zp test` prints, and the exit status is 0. `zp test` prints one
`PASS` line for each case on each backend, one for each backend equivalence check, and a summary. For a suite of one case
on the `closure` backend:

```text
PASS empty file [closure]
all fixtures passed
```

Lint with a mistake in the grammar (an alternative that cannot match, and a rule that nothing calls) fails the build,
and the findings say where and how to fix them:

```text
grammars/config.zp:2:17: error: alternative 2 (`"<="`) can never match: alternative 1 (`"<"` at 2:11) matches first wherever it could [shadowed-alternative]
	fix: move it before alternative 1
grammars/config.zp:4:1: warning: rule unused is never used: main does not call it, directly or indirectly [unreachable-rule]
	fix: remove the rule, or call it from a rule that main reaches
zp: lint: 1 error, 1 warning, 0 hints
```

A file that is not formatted is listed, and the command tells how to fix it:

```text
grammars/config.zp
zp: fmt: 1 file not formatted; run zp fmt -w
```

## How It Works

- `zp fmt -check` lists the files whose formatting differs from the canonical layout and exits with status 1. It never
  changes a file, so it is safe in CI; developers run `zp fmt -w` to fix.
- `zp lint` exits with status 1 for an error. `-strict` also fails for a warning, which keeps the grammar free of
  unused rules. Hints never fail.
- `zp test` runs every case of the suite on the backends it names and exits with status 1 if one fails. With
  `"equivalence": true` it also fails when two backends disagree.
- The loops do not stop at the first failure, so one run shows everything that needs fixing.

## Variations and Limits

- To read the findings in another tool, use `zp lint -f json`: the findings are one JSON document on standard output
  (the summary line goes to standard error).
- To keep a deliberate exception, suppress the finding with a comment in the grammar rather than turning the check off
  for the whole grammar; see [Linting Grammars](../guide/linting.md#suppressing-findings).
- When generated parsers are committed, add a step that regenerates them and fails on a difference; see
  [Ship a Parser and Keep It Up to Date](ship-a-parser.md).
- The commands are described in [Testing Grammars](../guide/testing-grammars.md), [Linting
  Grammars](../guide/linting.md) and [The `zp` Command](../guide/command-line.md#fmt).
