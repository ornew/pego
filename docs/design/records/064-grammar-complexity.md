# 064. Grammar Complexity: a Cost Model, a Classifier and Safeguards

- **Status**: Partially Implemented
- **Author**: @ornew
- **Date**: 2026-10-28

## Summary

A ZeroParse parse does not always run in linear time. Packrat memoization bounds how often a rule body runs at a position,
but parts of the language have a cost that the memo does not share, so a small grammar can take quadratic, cubic or
exponential time, or build values of exponential size. This record defines what a parse costs, per construct. It
proposes:

- a sound static classifier that labels each start rule `Linear`, `Linear·Env`, `Unknown`, `Poly(k)` or `Exp`, with a
  witness or a reason;
- two runtime changes that make linear what can be made linear: memoizing the members of left-recursion cycles, and
  reusing a repetition's run after backtracking;
- a lint warning for variables that grow themselves;
- a regression suite that measures cost at n, 2n, 4n and 8n;
- an opt-in automatic budget of c·n.

The remaining super-linear constructs are reported and bounded by the budget instead.

## Motivation

### The current claim

The runtime comment on deferred memoization says that evaluating a memoized rule at most twice per position keeps
"parse time" linear (`internal/engine/core/runtime.go:490-495`). The bound on evaluations is correct. Linear time does not
follow from it. Ford's linearity argument assumes that every nonterminal has a constant cost per position
besides its calls, and that every repetition is desugared into a memoized right-recursive nonterminal. ZeroParse deviates in
four ways:

- Repetitions are loops inside a rule body ([Repetition](../../../spec/language/parser-expressions.md#repetition)). One
  evaluation of a body can scan any amount of input, and the memo does not share that scan with evaluations that start
  at later positions.
- Rules on a left-recursion cycle that are not its leader are not memoized while a growth at their position can
  change their result ([Evaluations of Rules](../../../spec/runtime/parsing.md#evaluations-of-rules)); they are
  evaluated again at each step of that growth.
- A left-recursion leader grows a seed at each position, and Operators Rules fold operators at each position. The
  result at p shares nothing with the result at p+1.
- Rules that read variables are memoized per combination of values
  ([Interaction with memoization](../../../spec/language/predicates.md#interaction-with-memoization)). Bindings can
  make the combinations diverge without bound, and value actions can build values of any size.

### Measured growth

Revision `e71bf2f2`, Go 1.27.1, darwin/arm64, closure backend, code points. Each figure gives:

- the exact threshold of `Limits.Budget`, found by bisection to 0.5%;
- the rule evaluations reported by `Profile.Evals`;
- the bytes allocated per parse.

Steps are deterministic for a fixed artifact, input and options
([Cancellation and budget limits](../../../spec/runtime/parsing.md#cancellation-and-budget-limits)). The ratios therefore
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

E2 grows the same way on the bytecode and iterative backends. The budget limit stops every case above in proportion to
the cost, because the closure backend charges value construction by size.

A further case is a correctness defect rather than a cost. `var k int; R = {k := k + 1} R "x" / "x"` recurses
without consuming input until the nesting limit stops it. The time grows quadratically with the limit: 4, 14, 56 and
227 ms for limits of 1k, 2k, 4k and 8k. The left-recursion guard is found through the memo, whose key includes the
variable environment (`internal/engine/core/runtime.go:142-148`). A call with a different environment therefore does not
fail as [Left recursion](../../../spec/language/parser-expressions.md#left-recursion) requires. A separate change
rejects such a grammar at compile time: a binding of a variable that a left-recursion cycle reads is an error when the
binding may run before a left call into the cycle. A binding that runs only after the left call is allowed, because the
recursive call has then already been made. The same change keys the runtime guard by rule and position only. The model
below assumes both.

### Why this matters

A server that parses untrusted input with a fixed grammar cannot tell from the grammar whether an attacker controls
the exponent of its running time. The same is true of an editor that parses every keystroke with a user's grammar.
Budget limits stop a runaway parse. They do not tell the grammar author that the grammar is the cause, or which rule to
change.

## Goals

- State what a parse costs in time and memory, per construct, and the conditions under which it is linear.
- Classify every start rule soundly: a `Linear` verdict is never wrong. Other verdicts may be pessimistic and carry a
  witness path.
- Make the constructs linear that can be made linear without changing results: repetitions re-scanned after
  backtracking, and left-recursion cycle members.
- Warn about variables whose binding grows from their own previous value.
- Offer an opt-in budget derived from the input length.
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
cost**: the terminals it matches, its loop iterations, the actions and predicates it evaluates and the memo lookups
it makes, excluding the bodies of the rules it calls. Its memory is the memo table, plus the values it builds, plus
the call stack. Memory never exceeds a constant times time, because every entry and every value costs at least one
budget unit to create.

Per construct:

| Construct | Evaluations per position | Local cost per evaluation | Shared by the memo? |
|:--|:--|:--|:--|
| Memoized rule (`Memo`, outside a left-recursion cycle) | ≤ 2 per (rule, position, level, environment): the first call is not stored, the second is (`runtime.go:490-495`) | body size, plus loops | yes, for the whole body |
| Transient rule (called from one site, or calling no rule) | bounded by its caller's evaluations of that site | as above | through its caller |
| Left-recursion leader | 1 growth per (rule, position, environment) | one body evaluation per growth step; steps ≤ span + 1 | the final result only |
| Non-leader member of a left-recursion cycle | every call, and once per growth step of its leader | as above | no |
| Repetition, `Scan`, `ClassRun` | once per evaluation of the enclosing body | proportional to the span it matches | no |
| Lookahead | once per evaluation of the enclosing body | the cost of its body; consumes nothing | no |
| `#recover(skip=e)` | once per failure of its base | the base's cost plus the skip's span | no |
| Operators Rule | as a memoized rule, per binding level | proportional to the operators folded | the result per level only |
| Predicate or value action | once per evaluation of its body | proportional to the sizes of the values it reads and builds | no |
| Variable environment | multiplies memo keys by the number of distinct environments at a position | equality of the environment | per environment |

**Spans** are what makes local cost unbounded. A construct whose local cost grows with its span is called a **scanning
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
  growth, is memoized. A non-leader member of a left-recursion cycle with two call sites that can reach the same
  later position (E2) satisfies L2 because members are memoized outside the growth at their position.
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
  `s + s` on a value that flows back into the same binding (E4), and `acc + acc` or `concat(acc, acc)` on a fold
  accumulator (E5). An
  accumulator extended by bounded pieces (`acc + text(x)`) is quadratic in time because strings are copied, and gives
  `Poly(2)` for that action.

### Classifier

Verdicts form a chain, from best to worst:

```text
Linear  <  Linear·Env(vars)  <  Unknown(reason)  <  Poly(k, witness)  <  Exp(witness)
```

- `Unknown` is assigned when a condition fails without a witness of growth, as when units that are not committing nest
  without bound through a recursion of rule calls. A witnessed `Poly` or `Exp` cause outranks it: a witness is evidence
  of growth, while `Unknown` is only the absence of a proof.

- `Poly(k)` is assigned when L3 or L4 fails. k is one more than the deepest nesting of unsafe scanning constructs along a
  path of the call graph: E1 has k = 2 and E7 has k = 3. Each unsafe level multiplies the input positions where an
  inner scan can restart.
- `Exp` is assigned when L1 fails, when L2 fails on a cycle whose re-entry multiplies, or when L4 finds a doubling.
  The witness names the rules, the binding or the call sites involved.
- A start rule's verdict is the worst verdict of the rules it reaches. A rule's verdict also feeds the verdicts of its
  callers, because it is part of their local cost.

The automatic budget needs, for a `Linear` start rule, a constant c_G, an upper bound on cost per input position. It is
the sum, over memoized and transient rules, of twice the local cost of one body evaluation without loops, plus the
per-iteration cost of each loop. The classifier does not compute c_G, because the automatic budget is not implemented.

**Soundness argument (sketch).** The cost of a parse is the sum, over every body evaluation, of its local cost. For a
grammar that satisfies L1–L4, with the left-recursion guard keyed by rule and position:

1. A memoized rule is evaluated at most 2K times per (position, level) outside growth, by deferred memoization and L1.
   A transient rule is evaluated at most as often as the call site in its unique caller. The chain of callers ends at a
   memoized rule or the start rule. Under L2 no other rule is evaluated more than once per growth step.
2. The non-loop local cost of one evaluation is bounded by the size of its body.
3. Loop and scan cost is bounded by spans. Under L3, the evaluations of one scanning construct at distinct starts have
   spans that overlap only at their ends, or the overlap is answered by a memo hit or a reused run. Summed over a parse,
   each construct therefore covers each position a constant number of times. Left-recursion growth is a scanning
   construct, so the same argument bounds its steps.
4. Under L4 every value built is at most linear in the spans of the captures it reads, and its construction is charged
   to the evaluation that builds it.

Summing gives cost ≤ c_G · K · (n + 1), and memory ≤ cost.

The conditions are sufficient, not necessary. They over-approximate in these ways:

- Predicates are assumed to be able to both succeed and fail.
- First terminals are path-insensitive.
- A backtracking context is assumed to be reachable even when the input can never reach it.

Exact classification is undecidable. For a well-formed grammar G, consider `main = W Gadget`, where W must parse a word
of L(G) and Gadget is E1. This grammar is super-linear exactly when L(G) is non-empty, and emptiness of a PEG's language
is undecidable [Ford 2004]. The classifier can only be sound in one direction. A `Poly` or `Exp` verdict is therefore a
hint with a witness, not a proof that a slow input exists.

### Where It Plugs In

- The classifier (`internal/compile/complexity*.go`) is a pass over the grammar that the compiler runs: inline actions
  lowered to hidden rules and attributes normalized. It reuses the analyses of `internal/compile/analysis.go`
  (nullability, rules that always succeed, the left-call graph, its SCCs, leaders and cycle members) and runs only when
  asked, not at every compile. It assumes that the memo answers exactly the rules that every parse memoizes, and a test
  compares that assumption with the flags of the compiled rules, so the two cannot drift apart. Findings in hidden rules
  are reported in the rules that hold the inline actions.
- `zp lint` reports the results through two checks:
  - `growing-variable`, a warning on by default: a binding of a `string` variable that joins the variable's own value at
    least twice (`s := s + s`), when the variable can hold a non-empty value there and the bound value reaches a later
    evaluation of the same binding. Growth by a bounded piece, such as `indent := indent + "  "`, is not reported, nor
    are `int` and `bool` bindings, whose effect is on cost. This is a certain fact about the grammar, so it meets the
    warning contract of [Severities](../../../spec/runtime/lint.md#severities). It needs only the bindings that the
    classifier collects, so a default lint run skips the rest of the classification.
  - `super-linear`, a hint at each cause of a `Poly(k)` or `Exp` verdict, with its witness path. A hint because the
    slow input may not exist. It runs only when the classification is requested, with `zp lint -classify` or
    `lint.Config.Classify`, which also shows the verdict, the degree of a polynomial verdict and the first witnesses
    or reasons.
- The `right-recursion` hint gains a caveat. Right recursion stays linear under memoization in batch parses (E6 control).
  The repetition or left recursion it suggests is a scanning construct that L3 must check.

### Runtime Improvements

**Memoizing left-recursion cycle members.** A non-leader member of a left-recursion cycle is memoized
([optimization record 130](../../optimizations/130-memoizing-members-of-left-recursion-cycles.md)). Instead of
stamping entries with a growth generation, the parser never stores a result that depends on a seed that can still
change:

- a result that read the seed of a growth in progress when its call started is not stored;
- while a leader grows at a position, a member result there is evaluated again instead of reused;
- a result that read the result of a leader that can grow again at its position with another result is not stored.

Documents and interruption already discard state that depends on unfinished growth, and keep doing so. Cost: one
memo entry per stored call of a cycle member, and the depth bookkeeping of each memoized call, which grammars without
left recursion also pay. E2 is linear.

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
backtracking context, and the budget bounds them. Variables violating L1 and doubling values violating L4 stay
exponential by design, and are warned about.

### Budgets

The single budget keeps charges for value construction
proportional to the size built: string concatenation, `concat` and `list` materialization, and fold accumulators. A
per-call or per-iteration charge alone lets E4 cost 2^n while charging n.

The automatic budget is opt-in: a parse option sets the budget to c · (n + 1) + k for the input length n. The default
c is c_G of a `Linear` start rule, times a safety factor. For other verdicts the caller must give c explicitly. Without
the option, parses stay unbounded, as today. Exhausting the automatic budget returns the ordinary budget limit error. When
profiling is active, the error names the rule with the most repeated evaluations at one position, as `Profile` reports
them (`internal/engine/profile.go:66-72`).

### Specification

[Time and Memory](../../../spec/runtime/parsing.md#time-and-memory) specifies the cost of a parse, the conditions for
linear cost and the classification; [Grammar Lint](../../../spec/runtime/lint.md) specifies the two checks. The
specification promises linear cost only for the classes `Linear` and `Linear·Env`, under the memoization that it
describes.

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
4. The classifier over the grammar that the compiler runs, and the `growing-variable` and `super-linear` lint checks.
5. The automatic budget option, after the single budget lands.
6. The Complexity section of the specification, and corrected comments where the code claims linear time.

Steps 1 and 4 and the specification of step 6 are implemented; the two runtime changes and the automatic budget are
not, so E1, E2, E7, E8 and E9 remain negative controls.

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
- **Analysis on Core IR.** It would judge the module after rewrites such as `Scans`, but Core IR is built only for
  generated parsers, and the conditions are stated on the grammar. The grammar after lowering and normalization is
  what every backend compiles; the memo-policy test keeps the classifier aligned with the compiled rules.

## Testing

- **Step-ratio suite** (`internal/complexitytest`). Each E-series control and each corpus grammar is measured at input
  sizes n, 2n and 4n on the three backends, with steps found by bisection over the budget limit. A control must show
  its order from the table above; a corpus grammar's worst cost per byte may grow by at most 1.1 from 2n to 4n. Steps
  are deterministic, so the suite is stable without timing.
- **Classifier verdicts on the controls.** Each control has an expected class: a positive control is classified
  `Linear` or `Linear·Env`, a negative control never is, and a polynomial degree matches the measured order.
- **Classifier soundness by differential testing.** Random grammars over a small alphabet, in the shapes of the
  E-series and with string and list rules whose actions and predicates copy, index, measure and compare the values of
  their recursions, are classified, and every grammar classified linear is measured on several input families on the
  closure and bytecode backends: its cost per byte at 4n stays within 1.2 times that at n or 2n. A `Linear` verdict
  with super-linear growth is a classifier bug. A negative control treats the grammars classified `Poly` or `Exp` as
  linear and must fail, so the test can catch a classifier that claims too much.

Not yet in place:

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
   certifies as linear has a cost proportional to the input length. Other grammars carry no promise beyond termination.
   The classifier's soundness is therefore normative. The alternatives were no promise, which leaves
   users nothing to check, and a promise for every grammar, which restarted left-recursion growth and Operators Rule
   folds make impossible.
2. **`super-linear` is a hint, shown only on request.** The lint contract requires a warning to be certain, and a
   `Poly` or `Exp` verdict may be pessimistic. The finding carries the witness path. Because many grammars that work
   well on real input are classified `Poly`, the hint and the verdict appear only with `zp lint -classify` or
   `lint.Config.Classify`, and a default lint run stays free of estimates. `growing-variable` is certain and stays on.
3. **Run reuse is on by default.** Recording is deferred until a site restarts inside an earlier run, and an
   optimization control disables it, as the memo has. It is adopted only if the corpora and benchmark grammars, which
   never restart inside runs, show no slowdown. Without that evidence it stays behind the control.
4. **Memory has no bound beyond cost.** Budget bounds memory linearly. A memo-entry cap with eviction, which is
   semantically safe except for growing seeds and Documents, is reconsidered when a Document or stream workload
   shows memo memory as the limit. A byte budget for values is not added.
5. **The automatic budget uses c_G times a safety factor.** The factor is documented, and the caller can override c.
   For grammars that are not certified linear, the caller must give c. A corpus-measured constant was rejected
   because it is wrong for unusual grammars.
6. **Restarted left-recursion growth and Operators Rule folds are classified and budgeted.** E6 and E10 stay
   `Poly(2)`, reported with a witness and bounded by the budget. Reusing end positions in recognition mode was
   rejected: it would be linear only without values, and would add a second growth path to maintain.

## Limitations

- The classifier is sound only in one direction. A `Poly` or `Exp` verdict may have no slow input.
- The soundness argument is a proof sketch. The regression and differential tests check it; it is not mechanized.
- The constant c_G depends on each backend's charging table, so the automatic budget is not portable between backends
  or releases.
- The citations in the Literature section are unverified.
