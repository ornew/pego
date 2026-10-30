# 112. Guarded First Classes in Generated Go Parsers

- **Status**: Implemented
- **Date**: 2026-10-22

## Problem

A choice in a generated parser guards each alternative with a test of the next character
(the first-character guard): the guard reads the character once and skips every alternative
that cannot begin with it. When an alternative begins with a character class, the guard tests
exactly that class. The class then read the same character again, tested it again and
recorded an expectation that could never be recorded, because the guard had already admitted
the character.

## Mechanism

In a parser without budget controls and without Documents, where reading and testing a character
charge no budget, the guards of a choice keep the size of the character they read. When an
alternative begins with a class (the class itself, or the first item of a sequence), that class
consumes the character by the kept size and does nothing else. The rest of a run of classes is
matched by `ClassesSkip` as before.

The guard admits the character only if the class does, under the condition
`p.Depth <= p.MaxDepth`, which holds wherever generated code runs because every increment of
the depth is checked (the engine test `TestDepthChecks` pins this). The class therefore cannot
fail there, and the expectation it would record on failure is never recorded in either form.
Values, positions, errors and expected sets are unchanged.

Only the first expression written for the alternative may consume the guard's character: the
generator forgets the guarded class at any other expression, so a class elsewhere in the
alternative or after it is matched as written. Parsers with budget controls or Documents keep the
second read, which charges its input spend.

## Results

Bytes and lines of `parser.go` (`-typed-only`):

| Parser | Bytes before | Bytes after | Lines before | Lines after |
|:--|--:|--:|--:|--:|
| typescript | 1,183,066 | 1,175,599 (−0.6%) | 58,388 | 58,064 |
| python | 785,557 | 782,449 (−0.4%) | 37,348 | 37,237 |
| cel | 136,982 | 136,423 (−0.4%) | 6,797 | 6,778 |
| golang | 657,453 | 655,376 (−0.3%) | 33,560 | 33,474 |
| cue | 481,913 | 480,578 (−0.3%) | 23,413 | 23,367 |
| duckdb | 1,978,082 | 1,977,149 (−0.05%) | 101,456 | 101,417 |

Most choices of the duckdb keyword lists are terminal programs (record 111), which this does
not change.

Test binaries of the parser modules (`go test -c -trimpath`) and summed non-debug section sizes
of a program that imports only the parser package:

| Parser | Test binary before | Test binary after | Program sections before | Program sections after |
|:--|--:|--:|--:|--:|
| typescript | 11,025,314 | 10,975,170 | 4,609,457 | 4,584,316 |
| python | 15,183,906 | 15,166,386 | 3,790,568 | 3,781,777 |
| duckdb | 18,989,362 | 18,972,850 | 6,411,342 | 6,407,260 |
| golang | 12,496,146 | 12,495,954 | 3,340,872 | 3,336,328 |
| cue | 13,909,666 | 13,909,378 | 3,021,707 | 3,017,004 |
| cel | 12,739,506 | 12,739,378 | 2,300,617 | 2,298,613 |

**Parse speed.** Minimum of eight rounds, 1 s each, with the binaries in a rotating order. The
reference is an identical copy of the unchanged binary (control):

| Parser | Candidate / base (geomean) | Control / base (geomean) | Worst input, candidate | Worst input, control |
|:--|--:|--:|--:|--:|
| duckdb (37 inputs) | 1.002 | 1.000 | 1.010 | 1.020 |
| typescript | 0.999 | 1.007 | 0.999 | 1.007 |
| python (4 inputs) | 0.999 | 0.996 | 1.008 | 1.006 |
| golang | 0.992 | 0.998 | 0.992 | 0.998 |
| cel (2 inputs) | 0.999 | 0.996 | 1.002 | 1.001 |

Every input is within the spread of the control, so parse speed is unchanged within noise.

## Correctness and Tradeoffs

The tests do the following:

- Compare lean typed parsers with and without guarded classes in both position units, and
  their values and error messages with the engine's in code points. The grammars have
  alternatives that begin with classes of one-, two- and three-byte characters, with built,
  captured and discarded values, and with runs of classes. Inputs include invalid bytes,
  truncated sequences at the end of the input and empty remaining input, against a class
  that contains U+FFFD and a negated class that admits it: the guard reads the character
  with the same decoder as the class, so the kept size is the size the class would consume.
- Check that every case uses a guarded class, and that parsers with budget controls or
  Documents do not.

Mutants that consume a fixed size, let the guard decode an invalid byte with its trailing
continuation bytes or reject a truncated sequence at the end of the input, take another item as the guarded class, match the guarded
class inside a run, apply the change with budget controls, or forget the guarded class at the
alternative itself make a test fail. Removing the rule that forgets the guarded class at other
expressions changes no generated parser in the repository, because each class node is written
once; the rule keeps a node written a second time from consuming a character that no guard
read.

## Decision and Limits

Adopted for Go parsers without budget controls and Documents. The TypeScript generator is
unchanged. A class behind a capture or a rule call is not consumed by the guard.
