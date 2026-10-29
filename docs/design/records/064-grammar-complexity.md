# 064. Grammar Complexity: a Cost Model, a Classifier and Safeguards

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-28

## Summary

A PEGO parse does not always run in linear time. Packrat memoization bounds how often a rule body runs at a position,
but parts of the language do work that the memo does not share, so a small grammar can take quadratic, cubic or
exponential time, or build values of exponential size. This record defines what a parse costs, per construct. It
proposes:

- a sound static classifier that labels each start rule `Linear`, `Linear·Env`, `Poly(k)` or `Exp`, with a witness;
- two runtime changes that make linear what can be made linear: memoizing the members of left-recursion cycles, and
  reusing a repetition's run after backtracking;
- a lint warning for variables that grow themselves;
- a regression suite that measures work at n, 2n, 4n and 8n;
- an opt-in automatic work budget of c·n.

The remaining super-linear constructs are reported and bounded by the work budget instead.

## Motivation

### The current claim

The runtime comment on deferred memoization says that evaluating a memoized rule at most twice per position keeps
"parse time" linear (`internal/engine/runtime.go:490-495`). The bound on evaluations is correct. Linear time does not
follow from it. Ford's linearity argument assumes that every nonterminal does a constant amount of work per position
besides its calls, and that every repetition is desugared into a memoized right-recursive nonterminal. PEGO deviates in
four ways:

- Repetitions are loops inside a rule body ([Repetition](../../../spec/language/parser-expressions.md#repetition)). One
  evaluation of a body can scan any amount of input, and the memo does not share that scan with evaluations that start
  at later positions.
- Rules on a left-recursion cycle that are not its leader are not memoized
  (`CompileProgram` in `internal/compile/compile.go`: `memo = !DisableMemo && !cyclic`).
- A left-recursion leader grows a seed at each position, and Operators Rules fold operators at each position. The
  result at p shares nothing with the result at p+1.
- Rules that read variables are memoized per combination of values
  ([Interaction with memoization](../../../spec/language/predicates.md#interaction-with-memoization)). Bindings can
  make the combinations diverge without bound, and value actions can build values of any size.

### Measured growth

Revision `e71bf2f2`, Go 1.27.1, darwin/arm64, closure backend, code points. Each figure gives:

- the exact step threshold of `WorkLimits.MaxSteps`, found by bisection to 0.5%;
- the rule evaluations reported by `Profile.Evals`;
- the bytes allocated per parse.

Steps are deterministic for a fixed artifact, input and options
([Cancellation and work limits](../../../spec/runtime/parsing.md#cancellation-and-work-limits)). The ratios therefore
do not depend on timing noise.

| | Grammar | Input family | Steps as n grows | Order |
|:--|:--|:--|:--|:--|
| E0 (control) | `main = (A / "a")* $$; A = "a" "c"` | `a^n` | n=1k…8k: 15,040 → 30,080 → 60,160 → 120,320 | n |
| E1 | `main = (A / "a")* $$; A = "a"* "c"` | `a^n` | n=1k…4k: 2.02M → 8.06M → 32.1M; 3.6 GB allocated at 8k | n² |
| E7 | `main = (A / "a")* $$; A = (B / "a")* "c"; B = "a"* "d"` | `a^n` | n=100…400: 0.78M → 5.77M → 44.4M | n³ |
| E9 | `main = ("a" !Rest)* "a"* $$; Rest = "a"* "c"` | `a^n` | 1.02M → 4.03M → 16.1M | n² |
| E8 | `main = (X / "a")* $$; X = (Y #recover(skip=[a]+)) "c"; Y = "b"` | `a^n` | 0.56M → 2.13M → 8.26M | n² |
| E6 (control) | `main = (E "q" / "x")* $$; E = "x" E / "x"` | `x^n` | 43,136 → 86,272 → 172,544 | n |
| E6 | the same with `E = E "x" / "x"` | `x^n` | 6.55M → 26.2M → 104M; evaluations ≈ n²/2 | n² |
| E10 | the same with `E = #operators { operand "x"; level add { infix left "" } }` | `x^n` | n=500…2k: 0.51M → 2.02M → 8.06M | n² |
| E2 (control) | `main = C $$; A = "n"; C = "(" C ")" "y" / "(" C ")" "z" / A` | `(`^n `n` `)z`^n | n=8…16: 572 → 824 → 1,076 | n |
| E2 | the same with `A = C "+" / "n"`, which puts A and C on a left-recursion cycle led by A | the same | n=8…16: 7,520 → 29.8k → 119k → 477k → 1.91M | 2^n |
| E3 (control) | `var v string; R = "(" {v := "0"} R ")" "y" / "(" {v := "1"} R ")" "z" / "n" {len(v) >= 0}` | as E2 | 1,388 → 2,112 → 2,840 | n |
| E3 | the same with `{v := v + "0"}` and `{v := v + "1"}` | as E2 | n=6, 8, 10: 75k → 1.52M → 29.9M; memo hits 0 | ≥ 2^n |
| E4 | `var s string = "a"; R = "x" {s := s + s} R / {len(s) > 0}` | `x^n` | n=16…22: 0.39M → 1.58M → 6.31M → 25.2M; evaluations linear | 2^n value size |
| E5 | `main = xs:"x"* => len(foldl("a", $xs, (acc, x) => acc + acc))` | `x^n` | identical to E4, in one action | 2^n value size |

E2 grows the same way on the bytecode and iterative backends. The step limit stops every case above in proportion to
the work done, because the closure backend charges value construction by size.

A further case is a correctness defect rather than a cost. `var k int; R = {k := k + 1} R "x" / "x"` recurses
without consuming input until the nesting limit stops it. The time grows quadratically with the limit: 4, 14, 56 and
227 ms for limits of 1k, 2k, 4k and 8k. The left-recursion guard is found through the memo, whose key includes the
variable environment (`internal/engine/runtime.go:142-148`). A call with a different environment therefore does not
fail as [Left recursion](../../../spec/language/parser-expressions.md#left-recursion) requires. A separate change
rejects such a grammar at compile time: a binding of a variable that a left-recursion cycle reads is an error when the
binding may run before a left call into the cycle. A binding that runs only after the left call is allowed, because the
recursive call has then already been made. The same change keys the runtime guard by rule and position only. The model
below assumes both.

### Why this matters

A server that parses untrusted input with a fixed grammar cannot tell from the grammar whether an attacker controls
the exponent of its running time. The same is true of an editor that parses every keystroke with a user's grammar.
Work limits stop a runaway parse. They do not tell the grammar author that the grammar is the cause, or which rule to
change.

## Goals

- State what a parse costs in time and memory, per construct, and the conditions under which it is linear.
- Classify every start rule soundly: a `Linear` verdict is never wrong. Other verdicts may be pessimistic and carry a
  witness path.
- Make the constructs linear that can be made linear without changing results: repetitions re-scanned after
  backtracking, and left-recursion cycle members.
- Warn about variables whose binding grows from their own previous value.
- Offer an opt-in work budget derived from the input length.
- Prove the classifier's soundness on paper, and check it by measurement with deterministic step counts.

## Non-Goals

- Deciding the exact complexity of an arbitrary grammar. This is undecidable (see Soundness).
- Linear time for left-recursion growth and Operators Rule folds that a grammar backtracks over. The result at p
  cannot be shared with p+1; these constructs are classified and budgeted.
- Restricting what variables and actions may compute. Self-growing bindings are warned about, not rejected.
- Finite default budgets: parses stay unbounded unless the caller opts in.

## Design

### Cost Model

A parse is a tree of rule evaluations. Its time is the sum, over every body evaluation, of the evaluation's **local
work**: the terminals it matches, its loop iterations, the actions and predicates it evaluates and the memo lookups
it makes, excluding the bodies of the rules it calls. Its memory is the memo table, plus the values it builds, plus
the call stack. Memory never exceeds a constant times time, because every entry and every value costs at least one
unit of work to create.

Per construct:

| Construct | Evaluations per position | Local work per evaluation | Shared by the memo? |
|:--|:--|:--|:--|
| Memoized rule (`Memo`, outside a left-recursion cycle) | ≤ 2 per (rule, position, level, environment): the first call is not stored, the second is (`runtime.go:490-495`) | body size, plus loops | yes, for the whole body |
| Transient rule (called from one site, or calling no rule) | bounded by its caller's evaluations of that site | as above | through its caller |
| Left-recursion leader | 1 growth per (rule, position, environment) | one body evaluation per growth step; steps ≤ span + 1 | the final result only |
| Non-leader member of a left-recursion cycle | every call, and once per growth step of its leader | as above | no |
| Repetition, `Scan`, `ClassRun` | once per evaluation of the enclosing body | proportional to the span it matches | no |
| Lookahead | once per evaluation of the enclosing body | the work of its body; consumes nothing | no |
| `#recover(skip=e)` | once per failure of its base | the base's work plus the skip's span | no |
| Operators Rule | as a memoized rule, per binding level | proportional to the operators folded | the result per level only |
| Predicate or value action | once per evaluation of its body | proportional to the sizes of the values it reads and builds | no |
| Variable environment | multiplies memo keys by the number of distinct environments at a position | equality of the environment | per environment |

**Spans** are what makes local work unbounded. A construct whose local work grows with its span is called a **scanning
construct**: an unbounded repetition, `Scan`, a left-recursion leader, an Operators Rule, a recovery skip, and a lookahead
whose body contains one. A parse is linear when no input position is covered by the spans of more than a constant
number of evaluations of each scanning construct, and the number of environments per position is bounded.

### Conditions for Linearity

A start rule is `Linear` when every rule reachable from it satisfies these four conditions.

- **L1, finite environments.** No variable that a memoized rule reads is bound, along a recursion of the call graph,
  from an expression that reads the variable's own previous value. Bindings from literals, from captures and from other
  variables are allowed; their number of distinct values at one position is treated as a constant K. When K depends on
  the input, the verdict is `Linear·Env`, linear times the number of environments that occur at one position. An example
  is `indent := len($s)`, whose values are bounded by the line length. A binding such as `v := v + "0"` along a recursion
  (E3) violates L1 and gives `Exp`.
- **L2, memoized cycles.** Every rule that can be evaluated twice at one position, outside its own left-recursion
  growth, is memoized. Today a non-leader member of a left-recursion cycle with two call sites that can reach the same
  later position violates L2 (E2). The runtime change below removes this case.
- **L3, non-overlapping scans.** No scanning construct stands in a **backtracking context**. A backtracking context is
  one where its evaluation can be abandoned and the parser can then start another evaluation of the same construct
  strictly inside the span it covered:
  - a choice alternative that is not the last;
  - a repetition element or a sequence item that something able to fail follows;
  - a lookahead body;
  - the base of a `#recover`.

  The context of a construct is the chain of such contexts from it up to the nearest memoized boundary, including the
  callers of its rule. A context is safe if it is committing:
  - the alternatives that follow have first terminals disjoint from the construct's continuation;
  - or a cut follows the first terminal of the alternative;
  - or what can fail after the construct cannot fail after the construct has consumed input.

  A repetition after the run-reuse change below is not a scanning construct for L3, because a restart inside its run
  reuses the run.
- **L4, bounded values.** No predicate or value action makes a value whose size can double per evaluation. Examples are
  `s + s` or `concat(a, a)` on a value that flows back into the same binding or fold accumulator (E4, E5). An
  accumulator extended by bounded pieces (`acc + text(x)`) is quadratic in time because strings are copied, and gives
  `Poly(2)` for that action.

### Classifier

Verdicts form a chain, from best to worst:

```text
Linear  <  Linear·Env(vars)  <  Poly(k, witness)  <  Exp(witness)
```

- `Poly(k)` is assigned when L3 or L4 fails. k is one more than the deepest nesting of unsafe scanning constructs along a
  path of the call graph: E1 has k = 2 and E7 has k = 3. Each unsafe level multiplies the input positions where an
  inner scan can restart.
- `Exp` is assigned when L1 fails, when L2 fails on a cycle whose re-entry multiplies, or when L4 finds a doubling.
  The witness names the rules, the binding or the call sites involved.
- A start rule's verdict is the worst verdict of the rules it reaches. A rule's verdict also feeds the verdicts of its
  callers, because it is part of their local work.

For a `Linear` start rule the classifier also emits a constant c_G, an upper bound on work per input position. It is
the sum, over memoized and transient rules, of twice the local work of one body evaluation without loops, plus the
per-iteration work of each loop. The automatic budget uses c_G.

**Soundness argument (sketch).** The work of a parse is the sum, over every body evaluation, of its local work. For a
grammar that satisfies L1–L4, with the left-recursion guard keyed by rule and position:

1. A memoized rule is evaluated at most 2K times per (position, level) outside growth, by deferred memoization and L1.
   A transient rule is evaluated at most as often as the call site in its unique caller. The chain of callers ends at a
   memoized rule or the start rule. Under L2 no other rule is evaluated more than once per growth step.
2. The non-loop local work of one evaluation is bounded by the size of its body.
3. Loop and scan work is bounded by spans. Under L3, the evaluations of one scanning construct at distinct starts have
   spans that overlap only at their ends, or the overlap is answered by a memo hit or a reused run. Summed over a parse,
   each construct therefore covers each position a constant number of times. Left-recursion growth is a scanning
   construct, so the same argument bounds its steps.
4. Under L4 every value built is at most linear in the spans of the captures it reads, and its construction is charged
   to the evaluation that builds it.

Summing gives work ≤ c_G · K · (n + 1), and memory ≤ work.

The conditions are sufficient, not necessary. They over-approximate in these ways:

- Predicates are assumed to be able to both succeed and fail.
- First terminals are path-insensitive.
- A backtracking context is assumed to be reachable even when the input can never reach it.

Exact classification is undecidable. For a well-formed grammar G, consider `main = W Gadget`, where W must parse a word
of L(G) and Gadget is E1. This grammar is super-linear exactly when L(G) is non-empty, and emptiness of a PEG's language
is undecidable [Ford 2004]. The classifier can only be sound in one direction. A `Poly` or `Exp` verdict is therefore a
hint with a witness, not a proof that a slow input exists.

### Where It Plugs In

- `internal/compile/analysis.go` already computes, over the grammar AST:
  - nullability, rules that always succeed, the left-call graph, its SCCs and leaders, and the cycle members (`AnalyzeControlled`);
  - the variables each rule reads (`RuleVariables`);
  - the transient rules (`compile.go`).

  The classifier is a further pass over the same data, run on demand and not at every compile. L1 extends
  `RuleVariables` with the expressions each binding reads. L2 reads `cyclic` and `leaders`.
- Core IR carries the facts that L3 needs, per node:
  - first terminals and nullability ([Attributes and Effects](../../../spec/core-ir/attributes.md#first-terminals));
  - effect summaries that say which nodes can fail and consume (`internal/ir/core/attrs.go`, `Analyze`);
  - the call graph with SCCs (`internal/ir/core/graph.go`);
  - the memo policy per rule (`Memo`, `Transient`, `Leader`, `Vars`: [Policy](../../../spec/core-ir/modules.md#policy),
    `internal/ir/core/core.go:104-109`).

  Core IR is built for generated parsers (`internal/engine/core_build.go`). The classifier runs as a Core analysis that
  invalidates nothing. Its verdicts are attributes of rules, which the verifier ignores. Running on Core rather than
  on the AST means it judges the module that is actually executed, after rewrites such as `Scans`.
- `pego lint` reports the results through checks next to `long-lookahead` (`internal/lint/checks.go:1186`) and
  `right-recursion` (`internal/lint/checks.go:996`):
  - `growing-variable`, a warning: a binding that reads the variable it binds, along a recursion or a fold, and makes
    it longer by a value that reads the variable itself (`s := s + s`, `n := n * 2`). Linear growth such as
    `indent := indent + "  "` is not reported. This is a certain fact about the grammar, so it meets the warning
    contract of [Severities](../../../spec/runtime/lint.md#severities).
  - `super-linear`, a hint: the `Poly(k)` or `Exp` verdict of a start rule, with its witness path. A hint because the
    slow input may not exist.
- The `right-recursion` hint gains a caveat. Right recursion stays linear under memoization in batch parses (E6 control).
  The repetition or left recursion it suggests is a scanning construct that L3 must check.

### Runtime Improvements

**Memoizing left-recursion cycle members.** A non-leader member of a left-recursion cycle is evaluated again in every
growth step of its leader, and its result can depend on the leader's growing seed. It is therefore not memoized today.
The proposal memoizes it with an entry stamped by the growth generation of the leader at the position where the cycle
was entered:

- an entry is reused only while that generation is current;
- outside any active growth at that position, the entry is a plain memo entry.

This is the "involved set" of Warth et al. [2008], restricted to positions with active growth. Documents and
interruption already discard state that depends on unfinished growth, and keep doing so. Cost: one memo entry per call
of a cycle member, and a generation comparison on lookup. Grammars without left recursion pay nothing. E2 becomes
linear.

**Reusing a repetition's run after backtracking.** A Document already records the run of a repetition of 16 or more
elements: positions, examined range, expectations and values per element, about 80 bytes per element. It reuses the
run after an edit (`docs/optimizations/054`, `059`, `070`). Batch parses can use the same records to answer a restart
inside a run:

- When a repetition site starts at a position that is an element boundary of a recorded run of the same site, with the
  same environment and the same remaining count for bounded repetitions, the suffix of the run is the result.
- The elements before the restart are dropped; element values are shared.
- Recording is deferred like memo entries: a site records its runs only after it has restarted inside an earlier run
  once.

Cost: nothing on a grammar that never restarts inside a run. On one that does, 80 bytes per element of the runs it
records. E1, E7, E8 and E9 become linear, because the inner scan at p+1 is the suffix of the scan at p. A run cannot be
reused across a capture scope boundary whose captures the elements read, nor while a left-recursion seed it depends on
is growing; such runs are not recorded.

**What stays super-linear.** Left-recursion growth and Operators Rule folds that a grammar abandons and restarts (E6,
E10) build a different result at each position. They stay `Poly(2)`. The classifier names the rule and the
backtracking context, and the work budget bounds them. Variables violating L1 and doubling values violating L4 stay
exponential by design, and are warned about.

### Work Budgets

The single work budget, which replaces the separate input-work and step counters, keeps charges for value construction
proportional to the size built: string concatenation, `concat` and `list` materialization, and fold accumulators. A
per-call or per-iteration charge alone lets E4 cost 2^n while charging n.

The automatic budget is opt-in: a parse option sets the budget to c · (n + 1) + k for the input length n. The default
c is c_G of a `Linear` start rule, times a safety factor. For other verdicts the caller must give c explicitly. Without
the option, parses stay unbounded, as today. Exhausting the automatic budget returns the ordinary work-limit error. When
profiling is active, the error names the rule with the most repeated evaluations at one position, as `Profile` reports
them (`internal/engine/profile.go:66-72`).

### Proposed Specification Text

The following section is proposed for [Parsing](../../../spec/runtime/parsing.md), after Memoization. It becomes
normative when the classifier and the two runtime changes land; until then it is a draft.

> ## Complexity
>
> The **work** of a parse is the number of body evaluations of rules, the input that terminals examine, the
> iterations of repetitions and the size of the values that actions and predicates build. Work bounds both the time and
> the memory of a parse: memory never exceeds a constant times the work.
>
> - A rule is evaluated at most twice per position, binding level and combination of the values of the variables it
>   reads, except while a left-recursive rule grows at that position.
> - A parse of a grammar that `pego lint` classifies as linear does work at most proportional to the length of the
>   input, with a constant that depends on the grammar only.
> - Other grammars can do more. A repetition, a left-recursive rule or an Operators Rule whose match the grammar
>   abandons and then starts again inside it can make work quadratic or worse. A variable bound, along a recursion,
>   from its own previous value can make the number of combinations, and so work, exponential. An action can build a
>   value whose size is exponential in the input.
> - Every parse terminates. A call of a left-recursive rule at a position where that rule is already being evaluated
>   fails, whatever the values of variables.
>
> Work limits ([Cancellation and work limits](../../../spec/runtime/parsing.md#cancellation-and-work-limits)) bound the work of any parse. Values are
> charged in proportion to their size, so a limit also bounds the memory that values occupy.

### Compatibility and Backend Support

The classifier and the lint checks add findings and change no parse. Memoizing cycle members and reusing runs change
only cost, which [Memoization](../../../spec/runtime/parsing.md#memoization) permits. Node identity becomes different
where entries are shared, and it is already unspecified. The closure backend, both VMs and generated Go implement the
runtime changes. Generated TypeScript follows when it implements Documents, whose run records it lacks. Compiled
grammars carry the memo flag of cycle members, and the format may change before the first release.

### Implementation Sequence

1. The step-ratio regression suite, with the E-series as negative controls (no behavior change).
2. Memoizing left-recursion cycle members; E2 moves to the positive set.
3. Repetition-run reuse in batch parses; E1, E7, E8 and E9 move to the positive set.
4. The classifier as a Core analysis, and the `growing-variable` and `super-linear` lint checks.
5. The automatic budget option, after the single work budget lands.
6. The Complexity section of the specification, and corrected comments where the code claims linear time.

## Alternatives Considered

- **Memoize every repetition suffix, as Ford's desugaring does.** This gives linear time for all of L3 except
  left-recursion growth. It costs a memo entry per element of every repetition. Optimization 018 measured that most
  memo entries are never used, so this is rejected as the default. Deferred run reuse obtains the same bound only where
  restarts happen.
- **Elastic or bounded memo tables** [Kuramitsu 2015] and **cut-driven memo release** [Mizushima et al. 2010]. These
  bound memory, not time. The memo never changes results, so eviction is semantically safe and stays open as a later
  memory control (see Decisions).
- **Per-construct budgets** (left-recursion growth steps, recovery skip length). These add options a user cannot
  calibrate. A single budget plus a verdict naming the construct gives the same protection with one knob.
- **Rejecting super-linear grammars.** Classification is pessimistic and many such grammars are fine on real inputs,
  so rejection would refuse correct grammars. Lint findings and budgets keep the choice with the author.
- **Analysis only on the grammar AST.** Simpler, but it would judge a module different from the one that runs after
  Core rewrites. The AST pass stays the source of L1 and L2, which need no rewriting.

## Testing

Proposed:

- **Step-ratio suite.** For each grammar of the shared corpora (`internal/corpora`) and each E-series grammar, generate
  inputs of sizes n, 2n, 4n and 8n with the input generator. Measure steps with bisection over the work limit, or with a
  test-only counter. Assert:
  - for a start rule classified `Linear`, a ratio ≤ 2.2 per doubling;
  - for each negative control, the order in the table above.

  Steps are deterministic, so the suite is stable without timing. It runs per backend and for generated Go.
- **Classifier soundness by differential testing.** Generate random grammars, classify them, and measure step growth on
  generated and mutated inputs. A `Linear` verdict with super-linear growth is a classifier bug. The E-series must never
  be classified `Linear` before its fix, and must become `Linear` after it.
- **Worst-case search.** Mutate inputs to maximize steps per input length, in the manner of SlowFuzz and PerfFuzz
  [unverified citations], for corpus grammars classified non-linear. Confirmed witnesses become fixtures.
- **Equivalence.** The runtime changes keep values, errors and recovered errors identical with and without them, on
  every backend, in Documents across edits, and under interruption during left-recursion growth.

## Literature

Citations are from memory and are not verified against the publications; titles and venues should be checked before
this record is accepted.

- B. Ford. Packrat Parsing: Simple, Powerful, Lazy, Linear Time. ICFP 2002. Linear time when every nonterminal,
  including desugared repetitions, is memoized.
- B. Ford. Parsing Expression Grammars: A Recognition-Based Syntactic Foundation. POPL 2004. Well-formedness implies
  termination. Emptiness of a PEG's language is undecidable.
- A. Warth, J. Douglass, T. Millstein. Packrat Parsers Can Support Left Recursion. PEPM 2008. Seed growing. Linearity
  is not preserved in general.
- S. Medeiros, F. Mascarenhas, R. Ierusalimschy. Left Recursion in Parsing Expression Grammars. Science of Computer
  Programming, 2014.
- K. Mizushima, A. Maeda, Y. Yamaguchi. Packrat Parsers Can Handle Practical Grammars in Mostly Constant Space. PASTE
  2010. Cut releases the memo table; cuts are inserted where first sets are disjoint.
- K. Kuramitsu. Packrat Parsing with Elastic Sliding Window. Journal of Information Processing, 2015.
- R. Grimm. Better Extensibility through Modular Syntax. PLDI 2006. Selective memoization with transient productions.
- R. Becket, Z. Somogyi. DCGs + Memoing = Packrat Parsing, but Is It Worth It? PADL 2008.
- N. Weideman, B. van der Merwe, M. Berglund, B. Watson. Analyzing Matching Time Behavior of Backtracking Regular
  Expression Matchers by Using Ambiguity of NFA. CIAA 2016. Exponential and polynomial degrees from ambiguity.
- V. Wüstholz, O. Olivo, M. Heule, I. Dillig. Static Detection of DoS Vulnerabilities in Programs that Use Regular
  Expressions. TACAS 2017.
- Y. Shen et al. ReScue: Crafting Regular Expression DoS Attacks. ASE 2018.
- J. Davis, F. Servant, D. Lee. Using Selective Memoization to Defeat Regular Expression Denial of Service (ReDoS).
  IEEE S&P 2021. Memoizing selected states gives linear-time backtracking; the closest analogue of this design.
- T. Petsios et al. SlowFuzz, CCS 2017; C. Lemieux et al. PerfFuzz, ISSTA 2018. Worst-case input search.

None of these analyzes PEG grammars for super-linear behavior. The regex results transfer in shape: a polynomial degree
comes from nested scanning constructs that can restart inside each other, as in E1 and E7, and that degree is what
`Poly(k)` reports. Ordered choice and greedy repetition remove the ambiguity *within* one construct that drives regex
blow-ups. That leaves unmemoized re-entry and diverging state as the exponential sources.

## Decisions

1. **The specification promises a bound only for certified grammars.** A parse of a grammar that the classifier
   certifies as linear does work proportional to the input length. Other grammars carry no promise beyond termination.
   The classifier's soundness is therefore normative once it lands. The alternatives were no promise, which leaves
   users nothing to check, and a promise for every grammar, which restarted left-recursion growth and Operators Rule
   folds make impossible.
2. **`super-linear` is a hint.** The lint contract requires a warning to be certain, and a `Poly` or `Exp` verdict
   may be pessimistic. The finding carries the witness path. Neither a measurement-confirmed warning nor a separate
   report command is used.
3. **Run reuse is on by default.** Recording is deferred until a site restarts inside an earlier run, and an
   optimization control disables it, as the memo has. It is adopted only if the corpora and benchmark grammars, which
   never restart inside runs, show no slowdown. Without that evidence it stays behind the control.
4. **Memory has no bound beyond work.** Work bounds memory linearly. A memo-entry cap with eviction, which is
   semantically safe except for growing seeds and Documents, is reconsidered when a Document or stream workload
   shows memo memory as the limit. A byte budget for values is not added.
5. **The automatic budget uses c_G times a safety factor.** The factor is documented, and the caller can override c.
   For grammars that are not certified linear, the caller must give c. A corpus-measured constant was rejected
   because it is wrong for unusual grammars.
6. **Restarted left-recursion growth and Operators Rule folds are classified and budgeted.** E6 and E10 stay
   `Poly(2)`, reported with a witness and bounded by the work budget. Reusing end positions in recognition mode was
   rejected: it would be linear only without values, and would add a second growth path to maintain.

## Limitations

- The classifier is sound only in one direction. A `Poly` or `Exp` verdict may have no slow input.
- The soundness argument is a proof sketch. The regression and differential tests check it; it is not mechanized.
- The constant c_G depends on each backend's charging table, so the automatic budget is not portable between backends
  or releases.
- The citations in the Literature section are unverified.
