# 119. Case-Insensitive Literals in Generated Parsers

- **Status**: Implemented
- **Date**: 2026-10-24

## Problem and Mechanism

A keyword written as `"select"i` can also be spelled as a sequence of character classes,
`[sS][eE][lL][eE][cC][tT]`. Before this change, generated parsers had to take the class
form to be case-insensitive at all, and the grammar grew with the keyword list.

The generated parser matches a folded literal through `MatchLiteralFold`: the literal is
stored with each ASCII letter lowercased, and each input character is folded the same way
before it is compared. The matcher reads characters through `Peek`, as the runtime's other
non-exact paths do, so streams, Document parsing and work accounting use the same code as
exact literals on those paths. The node text is the consumed input. The class form and the
folded form accept the same inputs (`TestIgnoreCaseGeneratedGo` and
`TestIgnoreCaseGeneratedTS` check them against the closure backend).

The folded form does not use the in-place comparison of short literals (M130). Each
character costs one `Peek` and one fold. The generated first-character condition of a
folded alternative is two comparisons (`ch == 115 || ch == 83`).

Limits: the typed Go path reads the Core first set, which is empty for a folded literal,
so its choices run without the first-character guard, and terminal programs (record 111)
do not take folded literals. Both are conservative and affect speed only. They are a
follow-up optimization item.

## Method

The grammar has twenty keywords as folded literals, or as class-form keywords for the
comparison, with an identifier and whitespace rule:

```pego
rule main = item*
rule item = kw / ident / ws
rule kw = ("select"i / "from"i / "where"i / "group"i / "order"i / "having"i / "limit"i
    / "insert"i / "update"i / "delete"i / "create"i / "table"i / "values"i / "join"i
    / "on"i / "and"i / "or"i / "not"i / "null"i / "as"i) ![a-zA-Z0-9_]
rule ident = [a-zA-Z_] [a-zA-Z0-9_]* #atomic
rule ws = [ \n]+ #atomic
```

The class variant replaces each `"kw"i` with `[Kk][Ee][Yy]...`, the same letters in the same
order. Both are generated with the default untyped Go options, package name and start rule
`main`, and built in one scratch module that imports the worktree's runtime.

The input is a SQL-like text of about 16 KiB: the words `SELECT name from Users WHERE id and
Group by ORDER having limit rows insert INTO values Table join ON or NOT null as alpha Beta
gamma` in a fixed pattern, with a newline every ninth word. It parses with both variants.

Timing: `go test -run '^$' -bench 'Fold|Class' -count=8 -benchtime=1s` under the benchmark
lock, one process, paired by benchmark name. Each benchmark parses the same input once per
iteration.

## Results

| Variant | ns/op, median of 8 | Minimum | Range | Generated `parser.go` bytes |
|:--|--:|--:|--:|--:|
| Case-insensitive literals | 640,109 | 613,958 | 613,958-715,343 | 32,319 |
| Class form | 973,428 | 941,049 | 941,049-986,267 | 84,540 |

The folded parser runs in about two thirds of the class-form time (a median of 34% less)
on this input. The generated source is 62% smaller. Only this one grammar and input were
measured; the cause of the speed difference was not isolated.

## Correctness and Tradeoffs

- The fold is ASCII-only, as for the suffix: `"é"i` does not match `É`, and the Kelvin
  sign does not match `k` (tests: `TestIgnoreCaseGeneratedGo`, `TestIgnoreCaseRandomAgreement`).
- A mismatch records the expectation of the whole folded literal with its suffix, so
  diagnostics show `"select"i`.
- Breaking the fold in the Go runtime or the TypeScript runtime fails the generated-code
  tests, and breaking the uppercase range of the bytecode first-character class fails the
  agreement tests.

## Decision and Limits

The runtime helper is the `Peek` loop. A second helper with an in-place comparison was not
built: the folded form already beats the class form, and the loop is shared with the
non-exact paths. Folded literals in the Core first set and in terminal programs are the
follow-up optimization item; the measured gap that motivates them is not yet measured.
