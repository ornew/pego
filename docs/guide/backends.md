# Backends

A backend is the way a compiled grammar is executed. This guide compares them, helps you choose one, explains the
nesting limit that bounds recursion, and covers recognition mode and memoization, which apply to every backend.

The examples use `pairs.pego` and a parser `p` compiled as in [Running Parsers](running-parsers.md):

```pego
type Pair = { Key Match; Value Match }

rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

## The Backends

All of them produce **identical results**: the same trees
(including positions), the same syntax errors and the same recovered errors. The test suite checks this, and the
closure backend serves as the reference. They differ in speed, memory, how they treat deep nesting, and what they need.

| Backend | Option | How it runs | Needs the grammar AST |
|:--|:--|:--|:--|
| Closure | `pego.Closure` | Compiles Rule Expressions into Go closures | Yes |
| Bytecode (recursive) | `pego.Bytecode` | A VM running [portable bytecode](../../spec/bytecode.md); rule calls use Go recursion | No |
| Bytecode (iterative) | `pego.BytecodeIterative` | The same bytecode on a VM with its own call stack | No |
| Generated Go | (not an option) | A Go parser generated ahead of time by `pego gen` | At generation time |

The first three are selected per parse:

```go
n, err := p.Parse("abc=12", pego.WithBackend(pego.BytecodeIterative))
```

`pego.DefaultBackend`, used when you give no option, is the **closure backend when the grammar AST is available and
recursive bytecode otherwise** (that is, for a parser loaded from a `.pegoc` file saved without the AST). On the command
line, `-backend closure|bytecode|bytecode-iterative` does the same.

### Which One

The measurements are in [benchmarks.md](../benchmarks.md) and their analysis in
[performance.md](../performance.md#where-pego-stands); the figures below are summarized from them (one machine, so
treat them as orders of magnitude rather than promises). Run `go test ./bench -bench . -benchmem` to measure your own
grammar and input.

| You want | Choose | Why |
|:--|:--|:--|
| The best default for a Go program that loads a grammar at start-up | Closure (the default) | It is faster than both VMs on every benchmarked workload. |
| The fastest parsing, with a fixed grammar | [Generated Go](code-generation.md) | Current benchmarks show it at 0.41–0.59× closure time in CodePoints and 0.45–0.64× in Bytes; direct typed values are 0.52–0.88× generated `Parse`. |
| Input that can nest very deeply (untrusted JSON-like data, generated code) | Bytecode (iterative) | Rule calls live on the VM's own stack, not the Go stack. |
| A grammar distributed as a data file, not source | Bytecode, from a `.pegoc` | See the [compiled grammars guide](compiled-grammars.md). |
| Checking validity only | Any backend with [`RecognizeOnly`](#recognition-mode) | No tree is built. |

Across both units, the recursive bytecode VM is 1.08–1.39× slower than the closure backend in the benchmarks and the iterative VM 1.19–1.94×
slower; you pay that for portability (the same bytecode is specified for other runtimes in
[bytecode.md](../../spec/bytecode.md)) and, for the iterative VM, for the independence from the Go stack. Do not pick bytecode
inside a Go program for speed.

### Deep Nesting and `WithMaxDepth`

Recursive grammars (parentheses, nested arrays, nested blocks) use one rule call per level of nesting. The closure and
recursive bytecode backends use the Go stack for those calls. To keep runaway input from exhausting memory, a parse
fails with an error once rule calls nest deeper than a limit: **100,000 by default, 10,000,000 for
`BytecodeIterative`**. `WithMaxDepth(n)` changes it. This is the grammar `rule main = nested $$` with
`rule nested = "(" nested ")" / "x"` (one rule call per level of parentheses, plus the calls of `main` and of the
innermost `x`):

| Input (parentheses around `x`) | Closure | Bytecode | Iterative |
|:--|:--|:--|:--|
| 50,000 deep | ok | ok | ok |
| 200,000 deep | `nesting too deep: more than 100000 rule calls` | same error | ok |
| 1,000,000 deep | same error | same error | ok |

```go
n, err := p.Parse(input, pego.WithBackend(pego.BytecodeIterative), pego.WithMaxDepth(100))
// nesting too deep: more than 100 rule calls
```

An Operators Rule adds one active call level, and reading a prefix operand or an infix right operand adds another
Operators Rule level. These levels count on every backend, including the iterative VM's heap frames. Each operator of a
prefix or right-associative chain (`---x`, `a ^ b ^ c ^ ...`) adds a nested level; left-associative chains return
from each right operand before reading the next operator. Completed frames and ordinary failed branches release
depth; exceeding the limit aborts the parse and discards its execution state.

Two things to know:

- Lowering the limit is safe on every backend and bounds active nested calls and Operators Rule frames. Input length and
  total parsing work require separate limits. A nesting error is an ordinary `error`, so treat it like any parse failure.
- **Raising the limit on the closure and recursive bytecode backends is limited by the Go stack.** Go's goroutine
  stack is capped at 1 GB by default; running past it is not a recoverable error but a fatal `stack overflow` that
  kills the process. In a trial with the grammar above, the closure backend parsed 300,000 levels with
  `WithMaxDepth(1_000_000)`, but 1,000,000 levels with `WithMaxDepth(2_000_000)` on the recursive bytecode backend
  crashed. If you need depth beyond the default, use `BytecodeIterative` rather than raising the limit.

Generated parsers have the same default of 100,000. `pego.WithGeneratedMaxDepth` or `pego gen -max-depth` sets a
generated default; generated Go `WithMaxDepth` and TypeScript options-aware entry points override it per invocation
(see [code generation](generated-go-api.md#invocation-options-and-migration) and
[TypeScript depth options](typescript.md#deep-nesting-and-the-javascript-stack)). Their recursive host stacks still
bound acceptance even when the configured limit is higher.

## Recognition Mode

`RecognizeOnly()` checks whether the input matches without building a tree. `Parse` then returns a `nil` node and the
same syntax errors as a full parse. It is faster and allocates less: the benchmarks measure recognition at 1.4–1.8×
faster than a full parse on the closure backend.

```go
n, err := p.Parse("abc=12", pego.RecognizeOnly())
fmt.Println(n, err) // nil <nil>
n, err = p.Parse("abc=", pego.RecognizeOnly())
fmt.Println(n, err) // nil 1:5: syntax error: expected [0-9]
```

What it does not do:

- **Actions are conditionally evaluated.** Actions whose values are not needed to evaluate predicates are skipped. Actions needed by predicates still run and can report run-time errors. With the grammar
  `rule main Ratio =  a:[0-9]+ #atomic "/" b:[0-9]+ #atomic $$ => Ratio{Num: len($a) / (len($b) - len($b))} `, the input `12/34`
  fails a full parse with `action in main: division by zero` but is accepted by recognition (`pego parse -check` prints
  `ok`). Use recognition for syntax checks, and a full parse where action errors matter.
- Captures that [predicates](../../spec/predicates.md) read are still built, since a predicate can depend on them.
- Recognition needs either the grammar AST or a saved recognition bundle. A version 3 AST-less `.pegoc` can recognize
  on bytecode backends; a legacy AST-less file without a bundle returns
  `recognition needs the grammar or a recognition bundle; recompile this compiled grammar from source`. The [compiled
  grammars guide](compiled-grammars.md#with-and-without-the-ast) explains the distinction.
- It cannot be combined with `ParseStream` or `NewDocument`: both return errors (`recognition is not supported for
  stream parsing`, `recognition is not supported for documents`).

Typical uses are validating uploads or configuration before doing real work, linting loops, and pre-filtering large
inputs. On the command line: `pego parse -g grammar.pego -check < input`, which prints `ok` or the syntax error (and
exits with status 1).

## Memoization

PEGO parses with **packrat memoization**: the result of a rule at an input position is remembered, so the rule is
evaluated at most twice at that position, however much backtracking the grammar does. This is what keeps parsing time
roughly linear in the input. It is on by default, **automatic, and has no public switch**. The engine decides per rule:

- Rules that call no other rule, and rules referenced only once in the grammar, are not memoized in a normal parse,
  because their entries could never be reused.
- Other rules are memoized at a position from their second call there: most rules are called only once per position,
  and an entry that is never reused only costs memory. A rule whose calls do repeat is memoized from the first call
  for the rest of the parse.
- Rules in a left-recursive cycle are not memoized one by one; only the cycle's leader, the rule that grows the seed, is.
- Rules that read [variables](../../spec/predicates.md#interaction-with-memoization) are memoized per combination of
  those variables' values, so memoization never changes the result; it only changes the cost.

What this means for you:

- **Memory grows with input size.** The memo table, like the tree, is proportional to the input. For very large or
  unbounded input use a `#stream` repetition and `ParseStream`, which discards memo entries and input already
  handed to you (see [streaming.md](streaming.md)).
- **Incremental parsing reuses the memo.** `Document` keeps it between edits, and memoizes the rules that a normal parse
  skips, so that more of a previous parse can be reused.
- **Generated parsers memoize in the same way.** Memoization decisions are made by the same analysis and are saved in
  `.pegoc` files.
- The design is in [003-packrat-parsing.md](../design/003-packrat-parsing.md), and the history of how the engine's
  speed was tuned (including the memo table) in [performance.md](../performance.md).

## Related Topics

- [Running Parsers](running-parsers.md): compiling and calling a parser.
- [Compiled Grammars](compiled-grammars.md): saving a parser and loading it again.
- [Generating Parsers](code-generation.md): the fastest backend for a fixed grammar.
- [Debugging](debugging.md): finding why a parse is slow.
