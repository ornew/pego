# 111. Terminal Programs in Generated Go Parsers

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem

A keyword list in a grammar is often spelled as a trie of case-insensitive character
classes. The SQL grammar's reserved and unreserved keywords are one example:

```text
rule kw_u = ([aA] ([bB] ([oO] [rR] [tT] / ...) / ...) / [bB] ... ) !ident_cont
```

Generated straight-line code for each alternative of every choice of such a trie:

- saves and restores the position;
- reads the next character for the choice's first-character guard;
- tests the guard;
- reads and tests the same character again for the class that begins the alternative;
- declares a few variables.

The seven keyword rules of the duckdb parser took about 214 KB of `parser.go`. The
largest of them, a 1,300-leaf trie, took 142 KB on its own.

## Mechanism

**Eligible expressions.** The expression must be all of the following:

- made only of literals, character classes, sequences, choices, options, negative
  lookaheads, discards and atomic groups;
- contain a choice;
- have at least 16 literals and classes;
- produce no value (a discard, a lookahead, or a rule whose value is unused).

Such an expression needs no state but the position. In the typed body of a lean typed Go
parser (no work controls, no Document, no observability), the generator lowers it to a
`TerminalProgram` (`runtime/terminal.go`), which `SkipTerminal` interprets. Frame bodies,
which left-recursive rules and Operators Rules use, keep straight-line code.

The program is static data: a `[]uint16` of operations, each holding its own length, plus the
classes and literals it uses. Each alternative of a choice carries the guard that the
straight-line code would give it. When the alternative begins with the guard's class, a
single operation tests the guard and consumes the character.

**Dispatch tables.** For a choice that begins with at least four guarded alternatives,
`Prepare` builds an ASCII dispatch table once, at package initialization: for each
character, the alternatives whose guards admit it, in order. The table is built from the
same `Class.Has` that the guards use, so it cannot disagree with them. A table indexes its
entries with 16-bit offsets; a choice whose table would have more than 65,535 entries gets
none and tests its guards in turn. The generated code declares
`termNData = TerminalProgram{...}` and `termN = termNData.Prepare()`.

**Correctness.** `SkipTerminal` consumes the same input as the straight-line code and
records the same expectations at the same positions. It departs from the straight-line
order only where the departure cannot be observed:

- **Guarded classes.** A guard that admits the character has already tested the class that
  begins its alternative, so the class consumes it without testing again. The straight-line
  guard also requires `p.Depth <= p.MaxDepth`, which always holds in generated code, because
  every increment of the depth is checked.
- **Skipped alternatives.** A failed guard only records its expectation at the position of
  the choice. Where `ExpectOrdinary` records nothing there, skipping it has no effect: inside
  a lookahead (`Silent > 0`), or behind the farthest failure, which only grows. In these quiet
  choices the table skips straight to the admitted alternatives. Elsewhere every guard is
  tested in order, so the farthest-failure position and the expected sets are those of the
  straight-line code.

## Results

**Generated source.** Bytes and lines of `parser.go` (`-typed-only`):

| Parser | Bytes before | Bytes after | Lines before | Lines after |
|:--|--:|--:|--:|--:|
| duckdb | 2,190,528 | 2,004,595 (−8.5%) | 112,902 | 102,941 |
| typescript | 1,340,814 | 1,310,067 (−2.3%) | 67,245 | 65,164 |
| python | 798,059 | 792,174 (−0.7%) | 38,136 | 37,731 |
| golang | 698,572 | 694,523 (−0.6%) | 35,990 | 35,703 |
| cue | 486,676 | 486,676 | 23,672 | 23,672 |
| cel | 145,241 | 141,550 (−2.5%) | 7,289 | 7,024 |

The cue grammar has no eligible expression.

**Binaries.** Test binaries of the parser modules (`go test -c -trimpath`) and summed
non-debug section sizes of a program that imports only the parser package:

| Parser | Test binary before | Test binary after | Program sections before | Program sections after |
|:--|--:|--:|--:|--:|
| duckdb | 19,234,994 | 18,989,378 | 6,589,682 | 6,411,518 |
| typescript | 11,053,394 | 11,025,346 | 4,651,180 | 4,609,801 |
| python | 15,181,394 | 15,183,906 | 3,799,612 | 3,790,616 |
| golang | 12,494,274 | 12,496,146 | 3,342,627 | 3,341,336 |
| cue | 13,893,138 | 13,909,666 | 3,021,563 | 3,021,739 |
| cel | 12,722,162 | 12,739,506 | 2,303,256 | 2,300,617 |

The interpreter is about 5 KB of code, and only parsers that declare a program link it. In
the cue program, which declares none, no terminal symbol is linked; its 176 extra bytes are
metadata that names the new `SkipTerminal` method of the parser type.

Measurement notes on file sizes:

- Mach-O segments on arm64 are padded to 16 KB pages, so file sizes move in steps of a page.
  The cel and cue test binaries are one page larger although their programs shrink or grow
  by less than 3 KB.
- Test binaries also import the root package, which embeds the runtime's source files for
  embed-mode generation. They therefore carry the new source file as text, about 7.7 KB.

**Parse speed.** Minimum of eight rounds, 1 s each, with the binaries in a rotating order
(sixteen rounds for cel and typescript). The reference is an identical copy of the
unchanged binary (control):

| Parser | Candidate / base (geomean) | Control / base (geomean) | Worst input, candidate | Worst input, control |
|:--|--:|--:|--:|--:|
| duckdb (37 inputs) | 0.994 | 1.012 | 1.025 | 1.046 |
| typescript | 1.016 | 1.014 | 1.016 | 1.014 |
| python (4 inputs) | 0.992 | 1.002 | 1.007 | 1.013 |
| golang | 1.006 | 1.008 | 1.006 | 1.008 |
| cel (2 inputs) | 1.017 | 1.014 | 1.034 | 1.031 |

The candidate deviates from the base no more than the control does: no geomean is more
than 0.3 points above the control's, and the largest deviation of the candidate, 3.4% on one cel
input, is matched by the control's 3.1% on the same input. Parse speed is unchanged within
noise.

Allocations per parse vary from run to run in all three binaries; the identical control
varies in the same way. Their distributions overlap. Matching a prepared program allocates
nothing beyond the expectation list, which the straight-line code grows in the same way.

## Correctness and Tradeoffs

The tests do the following:

- Compare lean typed parsers with and without programs, and both with the engine's values
  and error messages, in both position units. The inputs fail inside guards, options and
  lookaheads, contain invalid UTF-8, and reach a keyword choice both in a lookahead and
  behind the farthest failure.
- Pin the eligibility rule: values, frame bodies, small or choice-free expressions,
  observability, work controls and Documents keep straight-line code. They also pin that a
  program is static data prepared at initialization.
- Check the dispatch tables directly, including a choice at the 65,535-entry limit and one
  past it, and run a grammar with a 1,200-alternative choice that gets no table.
- Check that matching a program allocates nothing once the expectation list has grown.
- Check that every increment of the depth, in the runtime and in generated code, is followed
  by the check that panics past the limit, which the guards of a program rely on.
- Check that programs appear only in typed bodies. Streaming parses run the Node bodies, so a
  parser with `#stream` reaches its programs only through `ParseAST`.

Mutants that break any of these conditions, or the interpreter's use of the tables, make a
test fail.

## Decision and Limits

Adopted for lean typed Go parsers. Parsers with work controls or Documents still charge work
for each expression, and keep the straight-line code. The TypeScript generator is unchanged.

The dispatch tables and the guards that consume their class are what keep parse speed
unchanged. An interpreter that tests the guards one by one is 6% slower on duckdb, and 19%
slower on one input.
