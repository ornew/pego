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
non-exact paths do, so streams, Document parsing and budget accounting use the same code as
exact literals on those paths. The node text is the consumed input. The class form and the
folded form accept the same inputs (`TestIgnoreCaseGeneratedGo` and
`TestIgnoreCaseGeneratedTS` check them against the closure backend).

The folded form does not use the in-place comparison of short literals (M130). Each
character costs one `Peek` and one fold. The generated first-character condition of a
folded alternative is two comparisons (`ch == 115 || ch == 83`).

The typed Go path reads the Core first set. For a folded literal whose first character is an
ASCII letter, the first set holds that letter in lower case with a fold flag, and the guard is
the same two comparisons. Any other first character is an exact one-character first set: the
fold is ASCII-only, so a non-ASCII letter keeps only its own code point. Terminal programs
(record 111) do not take folded literals, so a large value-free choice of keywords takes the
general path.

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

## Folded Literals in the Core First Set

The grammar is `parsers/duckdb/duckdb.pego` with its 350 keyword rules, such as
`rule SELECT = [sS][eE][lL][eE][cC][tT] ![...]`, rewritten as `rule SELECT = "select"i ![...]`
by a script (a regular expression over the rule lines). Both builds generate the typed Go
parser (`-types -typed-only -runtime import`) and the untyped recognizer (`-recognize`), and
the same grammar file is compiled by the closure and bytecode backends at run time. The input
is the duckdb benchmark script of about 256 KB, as written and lower-cased. Timing: six rounds
interleaved over the build before the change, the build after it and a second copy of the build
before it (the noise band), `-benchtime=2s`, medians in ms:

| Benchmark | Before | After | After vs before | Noise copy vs before |
|:--|--:|--:|--:|--:|
| Generated typed, upper case | 62.66 | 58.05 | -7.4% | +0.2% |
| Generated typed, lower case | 63.01 | 58.09 | -7.8% | -0.8% |
| Generated recognize, upper / lower | 42.87 / 42.69 | 42.49 / 42.54 | -0.9% / -0.4% | +0.1% / +0.4% |
| Closure recognize, upper / lower | 114.03 / 112.84 | 114.91 / 112.72 | +0.8% / -0.1% | -0.4% / -0.9% |
| Bytecode recognize, upper / lower | 138.43 / 137.67 | 137.63 / 137.82 | -0.6% / +0.1% | -0.5% / -0.1% |
| Iterative bytecode recognize, upper / lower | 165.47 / 165.06 | 164.89 / 165.40 | -0.4% / +0.2% | 0.0% / -0.4% |

Only the typed Go parser changes: it ran without the guard before. The closure, both
bytecode backends and the generated recognizer differ inside the noise band. The generated typed `parser.go` grows from 1.39 MB to
1.48 MB with the guards. The generated code of `parsers/duckdb` as written, with class-form
keywords, is byte-identical before and after.

Tests: `TestPassesGuardFoldedLiteral` checks the first sets, and `TestIgnoreCaseGeneratedGo`,
`TestIgnoreCaseGeneratedTS` and `TestIgnoreCaseGeneratedTypedGo` check mixed-case input against
the closure backend. Leaving the upper case out of the guard makes them fail.

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
non-exact paths.

Folded literals stay out of terminal programs (record 111). A program that takes them, with
an operation that folds each input character as `MatchLiteralFold` does, was built and
measured on the keyword grammar of the method above, written as a discarded choice of 19
folded keywords (`-kw`) over the same 16 KiB input, in the typed Go parser: the straight-line
code takes a median of 464 microseconds and the program 691 (+49%), against a noise band of
+2.8%, from eight interleaved rounds of three runs. The interpreted program costs more than
the guarded straight-line code it replaces, so the exclusion is a decision, not a gap.
