# Choosing a Backend

A backend is the way a compiled grammar is executed. This guide compares them, helps you choose one, and explains the
nesting limit that bounds recursion. [Recognition Mode](recognition-mode.md) and [Memoization](memoization.md) apply to every
backend.

The examples use `pairs.zp` and a parser `p` compiled as in [Running Parsers](running-parsers.md):

```zp
type Pair = { Key node[string]; Value node[string] }

@start
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
| Closure | `zeroparse.Closure` | Compiles Rule Expressions into Go closures | Yes |
| Bytecode (recursive) | `zeroparse.Bytecode` | A VM running [portable bytecode](../../spec/bytecode/README.md); rule calls use Go recursion | No |
| Bytecode (iterative) | `zeroparse.BytecodeIterative` | The same bytecode on a VM with its own call stack | No |
| Generated Go | (not an option) | A Go parser generated ahead of time by `zp gen` | At generation time |

The first three are selected per parse:

```go
n, err := p.Parse("abc=12", zeroparse.WithBackend(zeroparse.BytecodeIterative))
```

`zeroparse.DefaultBackend`, used when you give no option, is the **closure backend for a parser compiled from a grammar and
recursive bytecode for a parser loaded from a `.zpc` file**, which runs on the bytecode backends only. On the command
line, `-backend closure|bytecode|bytecode-iterative` does the same.

### Which One

The measurements are in [benchmarks.md](../benchmarks.md) and their analysis in
[performance.md](../performance.md#where-zeroparse-stands); the figures below are summarized from them (one machine, so
treat them as orders of magnitude rather than promises). Run `go test ./bench -bench . -benchmem` to measure your own
grammar and input.

| You want | Choose | Why |
|:--|:--|:--|
| The best default for a Go program that loads a grammar at start-up | Closure (the default) | It is faster than both VMs on every benchmarked workload. |
| The fastest parsing, with a fixed grammar | [Generated Go](code-generation.md) | Current benchmarks show it at 0.44–0.61× closure time in CodePoints and 0.50–0.66× in Bytes; direct typed values are 0.69–0.98× generated `Parse`. |
| Input that can nest very deeply (untrusted JSON-like data, generated code) | Bytecode (iterative) | Recursive rule calls live on the VM's own stack, not the Go stack. |
| A grammar distributed as a data file, not source | Bytecode, from a `.zpc` | See the [compiled grammars guide](compiled-grammars.md). |
| Checking validity only | Any backend with [`RecognizeOnly`](recognition-mode.md) | No tree is built. |

Across both units, the recursive bytecode VM is 1.13–1.35× slower than the closure backend in the benchmarks and the iterative VM 1.33–1.76×
slower; you pay that for portability (the same bytecode is specified for other runtimes in
[bytecode.md](../../spec/bytecode/README.md)) and, for the iterative VM, for the independence from the Go stack. Do not pick bytecode
inside a Go program for speed.

### Deep Nesting and `WithMaxDepth`

Recursive grammars (parentheses, nested arrays, nested blocks) use recursive rule calls that nest as deep as the input. The
closure and recursive bytecode backends use the Go stack for those calls. To keep runaway input from exhausting
memory, a parse fails with an error once calls of recursive rules nest deeper than a limit: **100,000 by default,
10,000,000 for `BytecodeIterative`**. `WithMaxDepth(n)` changes it. A rule is recursive when it is on a cycle of the
call graph, directly or through other rules. Only entries of recursive rules count; a rule that is on no cycle,
such as `main` below or a helper for whitespace, can sit on the stack only a bounded number of times and does not.
This is the grammar `rule main = nested $$` with `rule nested = "(" nested ")" / "x"` (one counted call per level
of parentheses, plus the innermost one):

| Input (parentheses around `x`) | Closure | Bytecode | Iterative |
|:--|:--|:--|:--|
| 50,000 deep | ok | ok | ok |
| 200,000 deep | `nesting too deep: more than 100000 nested recursive rule calls` | same error | ok |
| 1,000,000 deep | same error | same error | ok |

```go
n, err := p.Parse(input, zeroparse.WithBackend(zeroparse.BytecodeIterative), zeroparse.WithMaxDepth(100))
// nesting too deep: more than 100 nested recursive rule calls
```

An Operators Rule adds one active call level, and reading a prefix operand or an infix right operand adds another
Operators Rule level. These levels count on every backend, including the iterative VM's heap frames. Each operator of a
prefix or right-associative chain (`---x`, `a ^ b ^ c ^ ...`) adds a nested level; left-associative chains return
from each right operand before reading the next operator. Completed frames and ordinary failed branches release
depth; exceeding the limit aborts the parse and discards its execution state.

Two things to know:

- Lowering the limit is safe on every backend and bounds active nested calls and Operators Rule frames. Input length and
  total parsing budget require separate limits. A nesting error is an ordinary `error`, so treat it like any parse failure.
- **Raising the limit on the closure and recursive bytecode backends is limited by the Go stack.** Go's goroutine
  stack is capped at 1 GB by default; running past it is not a recoverable error but a fatal `stack overflow` that
  kills the process. In a trial with the grammar above, the closure backend parsed 300,000 levels with
  `WithMaxDepth(1_000_000)`, but 1,000,000 levels with `WithMaxDepth(2_000_000)` on the recursive bytecode backend
  crashed. If you need depth beyond the default, use `BytecodeIterative` rather than raising the limit.

Generated parsers have the same default of 100,000. The `MaxDepth` field of `gen.Go` or `zp gen -max-depth` sets a
generated default; generated Go `WithMaxDepth` and TypeScript options-aware entry points override it per invocation
(see [code generation](generated-go-api.md#invocation-options) and
[TypeScript depth options](typescript.md#deep-nesting-and-the-javascript-stack)). Their recursive host stacks still
bound acceptance even when the configured limit is higher.

## Related Topics

- [Running Parsers](running-parsers.md): compiling and calling a parser.
- [Recognition Mode](recognition-mode.md): checking input without building a tree.
- [Memoization](memoization.md): what the packrat memo table costs and saves.
- [Compiled Grammars](compiled-grammars.md): saving a parser and loading it again.
- [Generating Parsers](code-generation.md): the fastest backend for a fixed grammar.
- [Debugging and Profiling](debugging.md): finding why a parse is slow.
