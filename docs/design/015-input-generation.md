# 015. Grammar-Based Input Generation

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-08

## Summary

The package `github.com/ornew/pego/sample` generates inputs that a grammar accepts, to test code that consumes parse
results and to seed fuzz tests, and `pego sample` exposes it on the command line. A generator walks the grammar AST
from the start rule of a `*pego.Parser`, makes random but reproducible decisions, and returns an input only after the
parser has accepted it. It can bias its decisions toward rules and alternatives that no input has exercised yet and
report what was missed, and it can produce near-miss invalid inputs by mutating valid ones. The guide
[Sampling inputs and fuzzing](../guide/sampling-and-fuzzing.md) shows how to use it.

```go
inputs, err := sample.Generate(p, 20, sample.WithSeed(1))          // distinct inputs that p accepts
g, err := sample.New(p, sample.WithCoverage())                      // or a generator with state
inputs, err = g.Generate(50); report := g.Coverage()                // what the inputs exercised
invalid, err := g.GenerateInvalid(10)                               // near-miss inputs p rejects
sample.Seed(f, p, 50)                                               // seed corpus of a fuzz test
```

## Why a package, not methods on `Parser`

Generation is a testing tool with its own options (seed, bounds, coverage) and results (coverage reports, mutations),
and it needs nothing internal: only `Parser.Grammar`, `Parser.Start` and `Parser.Parse`. A separate package keeps the
core API small, keeps programs that only parse from linking it, and shows that the public API is enough. The fuzzing
helper takes an interface satisfied by `*testing.F`, so importing `sample` does not import `testing`.

A parser loaded from a `.pegoc` saved without the AST cannot be sampled: the bytecode alone does not describe the
grammar well enough (actions, levels, captures).

## Why validation by parsing is mandatory

Walking a PEG grammar like a context-free grammar produces many strings that the parser rejects or reads differently:

- An ordered choice commits to the first alternative that matches: in `("a" / "ab") "c"`, the text `abc` derived
  through the second alternative never parses.
- Repetition is greedy and never gives back: no input derived from `"a"* "a"` parses, and `(?a-z)+ " "? (?a-z)+`
  needs the space.
- Lookaheads, predicates and variables depend on the text around them (`!keyword ident`, `[len($s) == indent]`,
  matching XML tags), and actions can fail at run time.

So every input is parsed with the parser, and only inputs that parse without errors (also without errors recovered by
`#recover`) are returned. Everything below is about producing candidates that pass that check often, and fast.

## Design

### The search

The generator is written in continuation-passing style: generating an expression calls a continuation that generates
the rest of the input and reports whether a complete candidate was found. Each decision (an alternative of a choice, an
iteration count, taking an optional expression, a character) tries its options in turn; an option whose continuation
fails is undone and the next one is tried. All state (the text, captures, variables, pending checks) is either a
persistent list or restored on return, so undoing is free. One *attempt* is a depth-first search bounded by a budget
of steps (20,000); a generator makes up to `WithAttempts` attempts per input (100), each continuing the same random
sequence, so the result depends only on the parser, the options and the seed, for a given version of the package and
of Go: the random numbers come from `math/rand/v2` (PCG), whose methods' algorithms Go does not promise to keep across
releases, and any change to the search changes the inputs too. Tests that need stable inputs should store them.

The checks described below are re-evaluated each time a character is written, so the matcher that evaluates them can
do far more work than the generation: with a pending lookahead per character over a long text, the work grows with the
cube of the length, and one attempt once took 16 seconds while counting only 20,000 generation steps. The matcher's
steps therefore have a budget of their own, 1,000 times the generation budget (a matcher step is a few nanoseconds),
which bounds an attempt to a fraction of a second. A grammar that needs more, such as a negative lookahead over the
whole input before each of 800 characters, finds no input instead of taking seconds per attempt.

Depth-first search alone is exponential when a doomed decision is followed by many equivalent ones: a line comment
`"//" (?^\n)*` before a token on the same line is detected only when the token is written, and every character and
length of the comment would be tried first. Three rules keep doomed subtrees small:

- A decision stops trying options once its failed options have taken more than 4,096 steps; the decision before it,
  or the next attempt, tries something else.
- A character class or `.` tries a second character only if the first one failed by itself (a check rejected it at
  once), not when the rest of the input failed.
- A repetition goes beyond the number of iterations it aimed for only when stopping failed right away (within 8
  steps, as when a predicate after it wants a longer match) or within two extra iterations (two tokens that ran together
  need the whitespace between them).
- A repetition takes at most `8 × WithMaxRepeat + 4` iterations beyond its minimum (28 by default), and recursive rule
  calls nest at most `WithMaxDepth + 8` deep (13 by default). Both are hard limits on the search, not only on the
  inputs: a repetition whose stop keeps failing right away (a line comment whose next character fails the check, or an
  indentation that a predicate rejects at every width) would otherwise iterate until the budget runs out, and
  backtracking would try deeper and deeper calls before a dead alternative. Without the iteration limit, generating 50
  inputs for the Python grammar took 7.7 seconds instead of 0.15; without the nesting limit, coverage mode on
  `s = "(" s ")" / "x" / "y" / "(" s ")" "!"` took 25 seconds or more instead of one. A grammar whose inputs need longer
  repetitions or deeper nesting raises `WithMaxRepeat` or `WithMaxDepth`.

### Bounds

Repetitions aim for their minimum plus a geometric number of iterations (each further one with probability 1/2, at most
`WithMaxRepeat`, default 3). Recursion is bounded by `WithMaxDepth` (default 5), which counts calls of rules that are
already active, not the chain of rules between them; one level of nesting still counts once for each rule that it enters
again (an array directly in an array of the JSON example enters `value`, `array` and `elements` again, which counts 3,
while an object in an array counts 1). Once the depth or the length of the text (`WithMaxLen`, default 512 bytes) is
reached, the generator takes the shortest way to finish: alternatives in order of their minimal text length once the
text is long enough, or of their minimal derivation height (the fewest nested rule calls) once the recursion is deep
enough, each computed by a fixed point over the rules; optional expressions skipped; repetitions at their minimum.
Constructor analysis seeds its bounded worklist in callee-first DFS order, then reevaluates only callers of changed
estimates. Recursive calls still iterate to the same fixed point. Reverse edges are stored contiguously, and graph
construction deduplicates repeated calls without changing the grammar's target numbering or decision order.
(Ordering by height alone, a long literal beat a short alternative that needs one more rule call.) Both bounds are soft:
the search goes deeper or longer when the input requires it (`[d >= 12]` after twelve nested parentheses), up to the
hard limits on nesting and iterations described above.

The hard bounds therefore are the budget (`WithBudget`, exposed because an input of 25,000 repeated literals needs more
than the default), the nesting limit (`WithMaxDepth + 8`) and the iteration limit (`8 × WithMaxRepeat + 4`); each can be
raised through its option, and each is documented with it.

### Steering by PEG semantics

The generator prunes candidates that the parser would read differently from how they were derived:

- **Checks.** When it takes alternative *i* of an ordered choice, alternatives 0 to *i*−1 must not match there; when
  it stops a repetition below its maximum or skips an optional expression, the element must not match there with
  progress; `!e` must not match there; `$` must be followed by a line break or the end. A check usually cannot be
  decided when it is made (the text after it does not exist yet), so it stays pending and is re-evaluated each time the
  text grows, and finally at the end of the input. A violated check fails the decision that wrote the text.
- **The partial matcher.** Checks are evaluated by a small backtracking matcher over the AST with three outcomes:
  matched, failed, and *more* (the answer depends on text not generated yet). It answers *unknown* for predicates, Pratt
  expressions, left recursion (a rule called again at the same position), cuts, `#recover` and work over a bound; such
  a check is dropped and the parser decides. The matcher's answers are heuristics: a wrong answer costs a retry, never
  a wrong result. An expression that matches anywhere, if only the empty string (`"a"?`, `x*`, sequences and choices of
  such, without cuts), violates a check that it must not match at once: the matcher would answer *more* until the text
  after it exists, and in `x = "a"? / "b"?` the second alternative, which the parser never reaches, would be chosen
  and undone again and again (twenty such `x` in a row found no input).
- **Positive lookahead.** `&e` generates a text for `e`, then removes it and forces the input that follows to start
  with it. Captures and variables made in `e` stay in effect, as in the parser, so `&(s:spaces) [len($s) > indent]`
  works as in [examples/outline](../../examples/outline/outline.pego). Forcing guesses the text before the rest of the
  input exists, so `&(. .) "ab"` almost never worked; when forcing fails and `e` makes no captures and defines no
  variables, the generator falls back to a check that `e` match whatever the rest of the input turns out to be.
  (Using only the check made the Python grammar's coverage slightly worse: forcing `&")"` or `&(ws ":")` guides
  what follows.)
- **Predicates and variables.** Predicates are evaluated on the generated text with three-valued logic: literals,
  captures (with the kind of value the parser would build: a terminal, a list with its length, `nil`, a node), variables
  (scoped per rule invocation and undone on backtracking), `len`, `text`, comparisons, arithmetic and boolean operators.
  A predicate fails a decision only when it certainly fails; values the generator cannot compute (results of actions,
  fields, other functions) make it unknown, and the parser decides.
- **Repeated texts.** A predicate such as `[text($e) == text($n)]` (matching XML tags) almost never holds for two
  independently generated names. For a rule call captured by a capture that a predicate of the same rule reads, the
  generator may repeat the text of an earlier call of the same rule in the same rule invocation.
- **Anchors.** `^^` and `^` are checked against the text before them; `$$` fixes the end of the input, after which
  nothing can be written.
- **Pratt expressions** are generated as a flat chain: prefix operators, an operand, then postfix operators and infix
  operators each followed by another operand, with the skip expression before each operand and operator part. The
  parser builds the tree from the binding levels, so any chain is accepted except where an operator part is read
  differently or an `infix none` operator would chain; the generator tracks the latter per level (an operator of a
  looser level in between allows it again). A level-restricted call (`e(add)`) restricts the outer infix/postfix
  tail to that level and tighter ones; prefixes at every operand position remain unrestricted. A prefix at level
  `l` opens a RHS tail strictly tighter than `l`, even below the caller's entry. The flat chain carries the weakest
  bound opened by its prefixes. Each new primary starts without a prefix bound; prefixes in that primary clear
  nonassociative restrictions only at levels inside their fresh RHS, preserving restrictions outside it. Explicit
  rule calls initialize their own entry independently. Prefix and tail counts retain their existing per-chain
  bounds. Where a chain ends, no further postfix operator part may match after the skip, and no infix
  operator part followed by the start of an operand (the parser ends the expression before an infix operator that no
  operand follows, so `a+` is accepted by `e "+"`). Prefix and postfix parts that would match the empty string are not
  used, as in the parser. Stop checks omit already-closed nonassociative infix parts while retaining other infix
  and postfix parts, including ones at the same level. Static continuation parts are cached by entry minimum;
  filtered choices own temporary backing storage and never add cache entries for arbitrary combinations of
  closed levels. A caller can therefore continue with the same token after a closed nonassociative application.
  Two remaining approximations can omit valid derivations: the union of candidate checks does not reproduce
  longest-part selection before minimum/nonassociative eligibility, and the flat none mask can retain a tighter
  restriction after returning from an infix RHS. The parser remains the final acceptance check.
- **Left recursion** needs nothing special: the recursion is bounded like any other, and the parser grows the seed.

### Coverage

The targets are the rules, the alternatives of ordered choices, and the operands and operators of Pratt expressions.
Rules that the start rule reaches only inside negative lookaheads (`!keyword`) or the skip of `#recover` are reported
as unreachable, and rules that are called but can never match (bodies that end in `_|_` to report an error) as
impossible; neither counts in the totals. Alternatives that can never match, and everything inside an expression that
can never match (`"(" ("a" / "b") _|_`), are not counted either; a rule that can match but is called only there is
unreachable. For a Pratt rule, reachable calls and their minimum levels are propagated together: skip, operands
and viable prefixes are visited at every entry, while infix/postfix parts are visited only if a reachable entry
or prefix RHS permits their level. Calls and nested choices inside blocked operator parts do not contribute
reachable targets, and an unrestricted self-call there cannot widen the rule's entry. A prefix whose shortest
match is empty may still consume on another alternative, so its RHS remains conservatively possible.
Each rule has a reach set used to bias the search, computed as a fixed point over
the call graph; reach sets of subexpressions are computed on demand.
Height/length estimates and these broad bias reach sets use the callee-first dependency worklist. Dense bitsets
remain per rule; the separate joint rule/entry analysis determines public coverage exclusions. The constructor's analysis precedes
generation attempts and is outside `WithBudget`.

Coverage is recorded on the derivation the generator followed for each accepted input. The parser takes the same way
unless a check could not be decided, so the numbers are close to, but not guaranteed to be, the parser's own coverage;
instrumenting the runtimes for coverage was not worth the cost in every backend.

With `WithCoverage`, decisions prefer, in order, options whose own target is not exercised yet, options whose reach
contains such a target, and the rest (at random within each group). Optional expressions and repetitions that reach an
unexercised target are taken with probability 3/4 rather than always: when several of them together violate a predicate
(`[len($a) + len($g) + len($n) <= 1]` in the Python grammar), always taking them all would fail every attempt.

A target that ten failed attempts tried while it was not exercised yet is *settled*: it is no longer preferred, and
neither are options because they reach it. The parser may never take it, because of a cut, or because an earlier
alternative of the choice matches whatever it matches; in `s = "(" s ")" / "x" / "y" / "(" s ")" "!"` the last
alternative is dead, and the search finds out only after generating a whole nested `s`. Both kinds of failure count:
candidates the parser rejects, and searches that end without a candidate. Counting only rejected candidates, coverage
mode found 2 inputs for that grammar where plain generation found 10, because every recursive alternative kept leading
to the dead one. The threshold is a trade-off: at four failures, targets of the Python grammar that are reachable but
rarely found were settled too early, and coverage fell from 90% to 88% of its alternatives.

### Invalid inputs

`Invalid` takes a valid input and applies one mutation at rune boundaries: deleting one rune or a span of up to eight,
inserting or substituting a token (a literal or character of the grammar, or a common delimiter), duplicating a span,
swapping two runes, or truncating. It keeps the result only if the parser rejects it, and labels it with the mutation
and the error. Single mutations of valid inputs exercise error reporting and recovery much more than random bytes do.

### Command line

`pego sample -g grammar [-s rule] [-n 10] [-seed N] [-max-depth D] [-max-repeat R] [-max-len L] [-coverage] [-invalid]
[-f lines|json]` prints distinct inputs. The lines format writes each input as a Go-quoted string, because inputs
contain line breaks and arbitrary characters; the JSON format writes one document with the start rule, the seed, the
inputs (or the invalid inputs with their base, mutation and error) and, with `-coverage`, the coverage. In the lines
format the coverage report goes to standard error, so that standard output stays a list of inputs. `-invalid` replaces
the valid inputs instead of adding to them, for the same reason. The seed defaults to 0, so that runs are reproducible
unless asked otherwise.

## Results

Coverage in coverage mode, seed 1 (`pego sample -g <grammar> -n <count> -seed 1 -coverage`):

| Grammar | Rules (50 inputs) | Alternatives (50) | Rules (200) | Alternatives (200) |
|:--|--:|--:|--:|--:|
| calculator/calc.pego | 4/4 | 13/13 | 4/4 | 13/13 |
| calculator/calc_lr.pego | 13/13 | 17/17 | 13/13 | 17/17 |
| csv/csv.pego | 6/6 | 7/7 | 6/6 | 7/7 |
| json/json.pego | 19/19 | 14/14 | 19/19 | 14/14 |
| minilang/minilang.pego | 30/30 | 45/45 | 30/30 | 45/45 |
| outline/outline.pego | 8/8 | 2/2 | 8/8 | 2/2 |
| xml/xml.pego | 15/15 | 19/19 | 15/15 | 19/19 |
| python/python.pego | 175/184 (95%) | 182/202 (90%) | 176/184 (96%) | 184/202 (91%) |
| golang/go.pego | 158/204 (77%) | 160/228 (70%) | 188/204 (92%) | 201/228 (88%) |

The Python grammar misses `elif`/`else` clauses, augmented assignments and `except` handlers: each needs several
predicates and indentation checks to hold together after a long prefix. The Go grammar's statements are reached late:
type expressions are long, so inputs often hit the length limit before a function body. (The numbers were measured again
after the fixes that followed a review; before them, Go reached 64% and 80% of its rules.)

What the checks and predicate evaluation buy, on 30 inputs in coverage mode (seed 1): without them (but with the
other mechanisms), most candidates for the larger grammars are rejected by the parser.

| Grammar | Rules, with | Rejected candidates, with | Rules, without | Rejected candidates, without |
|:--|--:|--:|--:|--:|
| golang/go.pego | 63% | 0 of 30 | 16% | 389 of 419 |
| python/python.pego | 95% | 4 of 37 | 12% | 118 of 150 |
| minilang/minilang.pego | 100% | 0 of 32 | 77% | 240 of 290 |
| outline/outline.pego | 100% | 0 of 30 | 100% | 29 of 59 |
| xml/xml.pego | 100% | 0 of 30 | 100% | 14 of 44 |

The calculator, CSV and JSON grammars need neither: no candidate is rejected either way.

What the search rules bought, on 30 inputs in coverage mode (rules covered), in the order they were added:

| Version | Go | Python |
|:--|--:|--:|
| Checks, forced lookahead and predicates only | 11% | 9% (one input found) |
| + second character only after an immediate failure | 28% | 9% |
| + bounded extra iterations and a retry limit per decision | 56% | 90% |
| + up to two extra iterations after a slow failure | 67% | 94% |

The retry limit was then raised from 1,024 to 4,096 steps, which left Go's average over five seeds (50 inputs) at 74%
of its rules and raised Python's from 92% to 95%, with fewer failed attempts; raising it to 8,192 or the budget to
50,000 steps changed nothing but the time. Generating 50 inputs takes about 0.3 s for the Go and Python grammars and a
few milliseconds for the others.

## Alternatives considered

- **Generate and check only**, without checks or predicate evaluation: simple, but most candidates for the Go,
  Python and minilang grammars are rejected, and coverage collapses (see [Results](#results)).
- **Backjumping** to the decision that created a violated check: unsound, because the decisions made after the check
  wrote the text it reads (`!keyword ident` is violated by the identifier's letters, and a different letter fixes it).
- **Learning weights**: ordering alternatives by how often they led to an accepted input. It made no difference beyond
  the noise between seeds in coverage mode and reduced variety without it (Python 85% → 81% of rules, averaged
  over five seeds), so it was removed.
- **A larger default length limit**: with 1,024 bytes, Go's coverage rose (77% of rules, averaged over five seeds)
  but Python's fell (92%) and generation took three times as long; with 2,048 bytes, attempts exhausted their budget
  and coverage collapsed (Go 26%, Python 12%). `WithMaxLen` lets a user trade one for the other.

## Limitations

- Lookaheads, predicates and variables are handled by evaluation where the generator can compute them and otherwise
  only by generating and checking: predicates over the results of actions or struct fields, and checks the matcher
  cannot decide (inside Pratt expressions, left-recursive rules, cuts, `#recover`), cost retries.
- Checks are sound with respect to the derivation, not the language: they may reject a text that the parser accepts
  through another derivation (whitespace written by a Pratt skip after an empty `ws`, for example). Such texts are only
  found through another derivation.
- Inputs are not uniformly distributed over the language, are not minimized, and repeat less than they would at
  random only because `Generate` returns distinct inputs.
- Predicates are evaluated with code point positions, and inputs are verified with the default options of `Parse`;
  a grammar whose result depends on the position unit, or a `#stream` grammar used with `ParseStream`, is verified
  only for `Parse`.
- A `Generator` is not safe for concurrent use.
