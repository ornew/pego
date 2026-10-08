# Performance Tuning Log

This document records every performance change made to the PEGO runtime: what was changed, why, and what it bought.
It also records experiments that did **not** pay off, so they are not repeated, and the hotspots that remain.
Update it in the same commit as any performance-related change, including the table of where each optimization
applies.

For end-to-end comparisons between backends and with the Go standard library, see [benchmarks.md](benchmarks.md).

## How to measure

```bash
# Full benchmark suite (all backends, both position units, stdlib counterparts)
go test ./bench -run '^$' -bench . -benchmem -count 3

# A single workload, with CPU and allocation profiles
go test ./bench -run '^$' -bench 'BenchmarkParse/JSON/closure/codepoints' -benchtime 20x \
    -cpuprofile cpu.out -memprofile mem.out -o bench.test
go tool pprof -top -nodecount 30 bench.test cpu.out
go tool pprof -sample_index=alloc_space -top -nodecount 30 bench.test mem.out
```

Notes:

- The reference machine is a shared 4-vCPU VM. Wall-clock numbers vary by 10–20% between runs, and more when other
  processes are busy. Prefer `B/op` and `allocs/op`, which are deterministic, and confirm time changes with `-count 3`
  or more.
- Results must not change: every optimization is covered by the equivalence tests (`go test ./...`), which compare
  all backends, both position units, memoization on and off, and recognition against full parses.
- A change to shared runtime code (input, memo table, calls) should be measured on the incremental and stream
  benchmarks too (`BenchmarkIncremental`, `BenchmarkStream`), not only on batch parsing: they reach code paths the
  batch benchmarks do not (see the pitfall in change 30).
- `go test -bench` splits the pattern at `/` and matches each part against one level of the benchmark name, so an
  alternation that spans levels (`(Parse/JSON|Recognize/CSV)`) matches nothing; run such sets separately.
- The engine benchmarks read the grammars from `examples/` when they run, so when comparing a change to a grammar, run
  the benchmark binary while the old grammar is checked out (for example between `git stash` and `git stash pop`), not
  just a binary built from the old code. Generated parsers embed their grammar.
- The numbers below are for the closure backend with code-point positions unless stated otherwise. The workloads are
  the ones in `bench/` (JSON 262 KB, minilang 89 KB, CSV 211 KB).

## Current state

The current figures for every backend and workload are in [benchmarks.md](benchmarks.md), which `go run ./bench/report`
regenerates from a fresh run. The entries below record the effect of each change when it was made; entries before
change 13 were measured on a 4-vCPU Intel Xeon virtual machine and are several times slower in absolute terms.

Starting point (first benchmark run, on the virtual machine): JSON took ~510 ms and allocated 206 MB in 2.6 M allocations.

## Where each optimization applies

The runtimes are separate code: the closure backend (`compile.go`, `runtime.go`), the recursive and iterative bytecode
VMs (`vm.go`, `ivm.go`), the generated parsers' runtime (`genrt/runtime.go`, behind `Parse` and `Recognize`) and the
typed runtime of generated parsers (`genrt/typed.go`, behind `ParseAST`). An optimization made in one of them is not
automatically in the others. This table records, for every change in the log below, where it is in effect.

✓ applied · ✗ not applied (see the note) · – not applicable (the backend has no such code path or feature)

| # | Optimization | Closure | VM | Iter. VM | Generated | Typed | Notes |
|--:|:--|:-:|:-:|:-:|:-:|:-:|:--|
| 1 | Value-free bodies, twins, transient rules | ✓ | ✓ | ✓ | ✓ | ✓ | generated since 11 |
| 2 | Pooled frames of the iterative VM | – | – | ✓ | – | – | |
| 3 | Node fields as a slice | ✓ | ✓ | ✓ | ✓ | – | the typed runtime builds no nodes for its results |
| 4 | Expectations as interned IDs | ✓ | ✓ | ✓ | ✓ | ✓ | generated since 11 |
| 5 | Memo table with per-position chains | ✓ | ✓ | ✓ | ✓ | ✓ | |
| 6 | Slab allocation (nodes, lists, frames) | ✓ | ✓ | ✓ | ✓ | ✓ | typed: pooled arenas (48) |
| 7 | Recognition mode | ✓ | ✓ | ✓ | ✓ | – | generated: `-recognize` (47) |
| 8 | Unmemoized fast path, scan loops, nesting limit | ✓ | ✓ | ✓ | ✓ | ✓ | VMs: `SCAN` |
| 9 | Value-free Pratt lines | ✓ | ✓ | ✓ | ✓ | ✓ | |
| 10 | Zero-copy token text, reused evaluation context | ✓ | ✓ | ✓ | ✓ | ✓ | VMs: text only (their evaluator has its own stack, 14) |
| 12 | Memoizing rules that read variables | ✓ | ✓ | ✓ | ✓ | ✓ | |
| 13 | Pratt attempts by value | ✓ | ✓ | ✓ | ✓ | ✓ | iterative VM: pooled Pratt frames |
| 14, 27 | Operands on one expression stack | ✓ | ✓ | ✓ | – | – | generated code evaluates actions as Go expressions |
| 15, 28 | Allocation-free action plumbing (field chunks, list built-ins on a stack) | ✓ | ✓ | ✓ | ✓ | ✓ | |
| 16 | Start positions and lambdas from chunks | – | ✓ | ✓ | – | – | |
| 17, 31 | Fast character-class tests | ✓ | ✓ | ✓ | ✓ | ✓ | closure: specialized tests (31); VMs: ASCII bitmaps (17); generated: inline comparisons (11) |
| 18 | Memoizing from the second call at a position | ✓ | ✓ | ✓ | ✓ | ✓ | not for `Document` and streams, which keep every entry; generated since 21 |
| 19 | Plain call path (`invokePlain`) | ✓ | ✓ | ✓ | ✓ | ✓ | iterative VM since 24 (`plainCallFrame`, 34) |
| 20, 23, 30, 45, 56 | `Document` edits: memo and text spliced, offsets on demand, trees moved in place, memo entries brought up to date lazily | ✓ | ✓ | ✓ | – | – | generated parsers have no `Document` |
| 22 | Bounded memory in streams | ✓ | ✓ | ✓ | – | – | generated parsers have no streams |
| 26 | Lambda calls without allocation | ✓ | ✓ | ✓ | – | – | VMs: lambdas from chunks (16); generated lambdas are Go function literals |
| 32 | Literals compared in one loop | ✓ | ✓ | ✓ | ✓ | ✓ | generated: its own loop |
| 33 | Smaller VM entries | – | ✓ | ✓ | – | – | |
| 35 | Left-recursion growth without allocation | ✓ | ✓ | ✓ | ✓ | ✓ | |
| 36, 37 | Unboxed text comparison, no intermediate lists in `concat` | ✓ | ✓ | ✓ | ✓ | ✓ | VMs since 44 (instruction set 2) |
| 38, 42, 43, 46 | Direct calls of plain rules | ✓ | ✓ | ✓ | ✓ | ✓ | generated: a method per rule (46); typed: a method per rule for every rule (48) |
| 39 | Line table for error positions | ✓ | ✓ | ✓ | ✓ | ✓ | streams still scan from the committed position |
| 40, 41 | Offset table built while decoding | ✓ | ✓ | ✓ | ✓ | ✓ | engine: full parses only; recognition builds it on demand (30) |
| 48 | Struct constructors per type, frames reused, scratch pooled across parses | ✗ | ✗ | ✗ | ✗ | ✓ | see below |
| 52, 53 | Projected repetitions (`map($rest, (r) => $r.f)`) | ✓ | ✓ | ✓ | ✓ | ✓ | VMs: `NEXT` mode 3 (instruction set 3) |
| 49–51 | First-character dispatch in choices | ✓ | ✓ | ✓ | ✓ | ✓ | VMs: `GUARD` (instruction set 3) |
| 54, 59 | `Document`: resuming long repetitions | ✓ | ✓ | ✓ | – | – | VMs since 59 (sites found in the bytecode); generated parsers have no `Document` |
| 55 | Tracing hook (cost only) | ✓ | ✓ | ✓ | – | – | generated parsers have no tracing |
| 57, 58 | Scratch memory pooled across whole-input parses (input, offsets, memo, value stack) | ✓ | ✓ | ✓ | ✓ | ✓ | typed: since 48; generated `Parse` and `Recognize`: 58 |
| 63, 64 | Smaller `Node` (88 bytes: `int32` positions, interned type and rule names) | ✓ | ✓ | ✓ | ✓ | – | generated `Parse` since 64; the typed runtime builds no nodes |
| 60 | Direct rules: a rule's body inlined into its call method, captures in Go variables, the action in place | – | – | – | ✗ | ✓ | typed: not for rules with a cut or `#recover`, Pratt rules and left-recursion leaders; not tried for generated `Parse` |
| 61 | Character tests read code points without calling `peek` | – | – | – | ✗ | ✓ | typed: direct rules only; generated `Parse` still calls `peek` |
| 62 | Short literals compared in place | – | – | – | ✗ | ✓ | typed: direct rules, up to 4 code points, code points only (the other backends match literals with their own loop, 32) |

Not applied, and why:

- **Reusing frames when their rule returns (48):** in the Node runtimes, capture frames share their chunks with the
  child lists of the result, so they cannot be freed or pooled without separating them; freeing frames in the engine
  was measured slower (see the experiments table). The scratch memory that results do not share is pooled (57, 58).
- **Per-rule call methods for generated `Parse`** (the typed runtime's `typedCall`) measured within noise (48).
- Experiments that did not pay off in some backends are in the experiments table at the end.

## Change log

Each entry lists the commit, the change, the reason, and the measured effect at that point.

### 1. Value-free rule bodies, value-free twins, transient rules (0b2c903)

- **Value-free bodies.** The body of a terminal-type rule, or of a rule whose action does not use `$n`, is compiled
  without building values. Captures still build theirs.
- **Twins.** A CST rule (no action, no terminal type) called where its value is discarded (`@`, `-`, `!`, value-free
  bodies) gets a value-free twin with its own memo entries. Rules in left-recursive cycles never get twins, because
  mixing a valued and a value-free version inside one cycle would change seed-growing semantics.
- **Transient rules.** Rules that call no other rule, or that are referenced only once in the grammar, are not memoized
  in normal parses: their entries can never be reused (a single-reference rule is re-invoked at a position only when its
  unique caller is, and the caller chain ends at a memoized rule or the start rule). `Document` still memoizes them so
  edits can reuse results.
- **Shared empty frame** for capture-free scopes.
- Effect: JSON 442 → 144 ms, 183 → 76 MB.

### 2. Frame pooling in the iterative VM (8fab402)

- Call frames, body frames and body state were heap objects per rule call. They now come from a per-parser free list.
- Effect: iterative VM allocation went from ~2.3× the recursive VM to parity.

### 3. Node fields as a slice (54a5774)

- `Node.Fields` was a `map[string]any`; each struct node paid for a map (several hundred bytes). It is now a slice of
  name/value pairs with `Get`; JSON output (sorted keys) and `Node.Field` are unchanged.
- Effect: JSON 72 → 64 MB.

### 4. Expected sets as interned IDs on a shared stack (11a6ce3)

- Error reporting records what was expected at the farthest failure. Every memoized call and every `#error`/`#recover`
  region used to start a fresh `[]string`.
- Expectations are now compile-time IDs into a per-backend description table. Isolated regions are windows of one stack;
  only sets kept by the memo or a pending recovery are copied into an append-only arena.
- Effect: minilang 59 → 42 MB, ~128 → ~103 ms. Error messages are identical.

### 5. Memo table with per-position chains (cf77e4f)

- The memo was a Go map keyed by (rule, position, level): hashing, map growth and one allocation per entry.
- Entries now hang off a slice indexed by position and are allocated in slabs of 256. Stream pruning drops a prefix of
  the slice. Kept expectation sets are copied into fixed-size chunks, and a repeat of the previous set is shared.
- Effect: JSON ~130 → ~85 ms, minilang ~103 → ~65 ms; about a third of parse time.

### 6. Slab allocation for nodes, child lists and frames (5d03bf2)

- Nodes, child slices and capture frames come from per-parser chunks, in the spirit of an arena: a chunk stays alive
  while any of its nodes is referenced (the lifetime of the parse result). Repetition children are gathered on a
  shared stack and copied out once. Memo entries were packed into 128 bytes.
- Effect: allocations per parse dropped by about three quarters (JSON 940 k → 250 k); time −5 to −10%.

### 7. Recognition mode (3f7e9bf)

- `RecognizeOnly` (`pego parse -check`) runs a separate program derived from the same grammar that builds no values
  and runs no actions, and returns the same syntax errors. Rules whose values predicates read still run normally.
- Effect: 1.5–1.8× faster than a full parse (JSON 78 → 44 ms). Compare it with `json.Valid`, not with parsers that
  build trees.

### 8. Fast path for unmemoized calls, `SCAN`, nesting limit (d2db87d)

- **Unmemoized calls** skip the memo bookkeeping: the examined range only grows, so it needs no save/restore, and
  expectations need no isolation.
- **Single-character repetitions** (`(?class)*`, `.+`, ...) in value-free positions run as a scan loop without
  per-iteration backtracking state (closure: `scanRepeat`; bytecode: the new `SCAN` instruction).
- **Nesting limit.** Rule-call depth is capped (`WithMaxDepth`, default 100,000; 10,000,000 for the iterative VM).
  This is a robustness fix rather than a speedup: a corrupted `.pegoc` (for example a cleared left-recursion flag)
  used to recurse until the Go stack overflowed.
- Effect: recognition JSON 39 → ~32 ms, CSV 24 → 18 ms, minilang 40 → ~31 ms; full parses a few percent.

### 9. Value-free Pratt lines; discarding trivia in example grammars (936857c)

- A Pratt operator line with an action never uses the line's value (`$op` is built from the matched range and `$n` is
  not allowed there), and neither does an operand line whose action does not use `$n`. Such lines are now compiled
  without values (closure `leanLine`; bytecode uses the same predicate).
- The example grammars captured repetitions such as `rest:(ws "," ws m:member)*`. A captured value keeps its whole CST
  (it can be inspected with `.children` or `text`), so every whitespace character became a `Match` node. They now
  discard trivia with `-ws` (see the authoring guideline below). The resulting ASTs are identical.
- Effect: JSON 50.5 → 42.8 MB, minilang 32.8 → 23.0 MB per parse.

### 10. Zero-copy token text; reusable evaluation context

- **Token text.** `Match.Text` and terminal texts were built by converting `[]rune` (code points) or `[]byte` back to a
  string. The input now keeps the source string, plus a rune-to-byte offset table in code-point mode, and token text
  is a substring. Inputs with invalid UTF-8 in code-point mode keep copying, so U+FFFD replacement is unchanged; stream
  inputs also keep copying. Trade-off: token strings keep the whole input alive, like any Go substring.
- **Evaluation context.** Each action and predicate allocated an `evalCtx`. Evaluations never nest and no reference to
  the context survives (function values cannot be stored in fields or variables), so one context per parser is reused.
- Effect: allocations per parse JSON 230 k → 153 k, minilang 176 k → 137 k, CSV 166 k → 131 k.

### 11. Generated parsers catch up with changes 4–10

- Generated Go parsers (`internal/engine/genrt` plus the code from `gen.go`) had kept the old runtime: `[]string`
  expected sets copied per memoized call, a Go map for the memo, a heap object per node, child list and frame, and
  token text copied out of `[]rune`. They had become 1.5–2.3× slower than the closure backend while allocating up to
  5× as often.
- Ported to the generated runtime:
  - expected sets as interned IDs; the generator emits the description table (`descs`), and isolated records,
    `keep` with its arena chunks and the reuse of the last kept set work as in the engine (change 4);
  - the memo table with per-position chains and slab-allocated, packed entries; there is no `Document` or stream,
    so no examined range, shift or pruning (change 5);
  - slabs for nodes, child lists and frames, and `kidStack` for repetition children (change 6);
  - the unmemoized fast path in `call`, scan loops for value-free single-character repetitions, and the nesting limit
    (`nesting too deep: more than 100000 rule calls`, the engine's default) (change 8);
  - value-free Pratt lines (`leanLine`), and captures in value-free positions returning no value (changes 1, 9);
  - token text as substrings of the input with a rune-to-byte offset table (copying only for invalid UTF-8), and one
    reused action context per parser (change 10).
- Beyond the engine:
  - character classes are generated as inline range comparisons instead of a loop over a range table, both in
    single matches and in scan loops;
  - Pratt `longest` returns the best attempt by value and saves captures across `reset` in one buffer per parser,
    so trying an operator line without captures allocates nothing (the engine allocates a `prattAttempt` per
    match and a slice per try). On minilang this alone took allocations from 82 k to 19 k per parse.
- `TestGeneratedParsersMatchEngine` now also covers scan loops and the nesting limit (at and one beyond it).
- Effect (generated parser, code points; closure backend in the same run in parentheses):

  | Workload | Time | Allocated per parse | Allocations per parse |
  |:--|--:|--:|--:|
  | JSON | ~110 → ~40 ms (~68 ms) | 53.1 → 35.5 MB (39.8 MB) | 798 k → 54 k (153 k) |
  | minilang | ~152 → ~33 ms (~50 ms) | 51.1 → 17.8 MB (21.5 MB) | 710 k → 19 k (137 k) |
  | CSV | ~71 → ~25 ms (~52 ms) | 28.0 → 26.8 MB (31.3 MB) | 388 k → 46 k (131 k) |
  | XML | ~117 → ~37 ms (~47 ms) | 32.2 → 28.4 MB (30.4 MB) | 490 k → 32 k (64 k) |

  Generated parsers again allocate less than the closure backend (in bytes and in count) and are the fastest
  backend again, about 1.3–2× faster than the closure backend (times are noisy on the shared VM; the closure
  numbers in this run were higher than in earlier entries).

### 12. Memoizing rules that read variables

- Rules that read variables, directly or through callees, were never memoized, because their results depend on the
  environment. Context-sensitive grammars (indentation-based ones in particular) could then take exponential time,
  and the Python example had to be written in a parse-once-then-fold style to avoid it.
- The memo key now includes the values of the variables the rule may read (`rule.vars`, computed statically and
  sorted); definitions made inside a rule are undone when it returns, so these values determine the result. Generated
  parsers do the same (the generator emits each rule's variable list).
- Effect: removes the exponential worst case for variable-reading rules; no change for grammars without variables.

### 13. Allocation-free Pratt attempts and pooled Pratt frames in the engine and VMs

- Ports the generated parsers' Pratt technique (change 11) to the engine and both VMs. `parser.longest` returns the
  winning `prattAttempt` by value instead of allocating one per candidate, and saves captures across a reset in a
  reusable buffer (`parser.saved`) instead of a fresh slice.
- The iterative VM pools its skip, longest, nud and Pratt frames (generic `reuse[T]`, released in `release`), holds
  attempts by value, and pushes no skip frame when the Pratt expression has no `skip`.
- Pitfall: putting the `prattAttempt` into `iresult` (returned on every VM step) made the iterative VM about 20%
  slower on every grammar, Pratt or not. The attempt is passed through `parser.attempt` instead, so `iresult` stays
  small.
- Effect (min of 6, codepoints, allocs/op before → after):

  | Workload | closure | bytecode | iterative |
  |:--|:--|:--|:--|
  | Arith_Pratt | 173k → 134k | 269k → 231k | 484k → 231k; 79 → 67 ms |
  | Minilang | 137k → 75k | 203k → 140k | 274k → 140k; 92 → 75 ms |
  | JSON (no Pratt) | unchanged | unchanged | unchanged; 107 → 101 ms |

### 14. One operand stack for VM expression code

- The bytecode backends evaluate actions and predicates with stack-based expression code (`vmProgram.eval`). Each
  evaluation started with a nil `[]any` and grew it by appending, `ENew` copied the field values and `ECall` the
  arguments into fresh slices, `ENew` built its field-name list each time, every lambda call (`vmFunc.apply`) built
  a new locals slice, and every Pratt operator action a 3-element one for `$lhs`, `$rhs`, `$op`. On JSON these
  accounted for about half of the VM's allocations, and were the whole difference from the closure engine.
- Operands now live on one stack per parser (`parser.estack`). An evaluation works above the current top and
  returns the stack to its height. `ENew` and `ECall` pass the top of the stack to `newStruct` and the built-ins,
  which do not retain it; during a built-in call the arguments stay on the stack and lambdas called by it push
  above them. Lambda and operator locals are pushed onto the same stack. Field-name lists are resolved once when
  the program is loaded.
- `EFUNC` copies the locals when there are any, because a function can outlive the lambda that created it (a lambda
  may return it) while its locals are popped. Lambdas in rule actions have no locals and do not copy.
- Effect (min of 6 interleaved runs, codepoints, Apple M3 Max; allocs/op and time before → after):

  | Workload | bytecode | iterative | closure (unchanged) |
  |:--|:--|:--|:--|
  | JSON | 292k → 147k; 28.5 → 27.2 ms | 292k → 147k; 40.0 → 39.0 ms | 153k |
  | CSV | 201k → 116k; 13.7 → 12.8 ms | 201k → 116k; 16.5 → 15.8 ms | 131k |
  | XML | 165k → 72k; 25.9 → 24.3 ms | 165k → 72k; 35.9 → 34.5 ms | 64k |
  | Arith_Pratt | 231k → 94k; 20.2 → 17.6 ms | 231k → 95k; 29.0 → 26.6 ms | 134k |
  | Minilang | 140k → 68k; 26.3 → 24.8 ms | 140k → 68k; 33.6 → 32.4 ms | 75k |

  The VMs now allocate less often than the closure engine on JSON, CSV, Pratt and minilang.

### 15. Allocation-free action plumbing in all backends

- With change 14 the VMs' remaining allocations were mostly in code shared with the closure engine, around actions
  rather than in the actions' own values:
  - an action whose body is not a sequence got its element list (`$1`) as a fresh one-element slice
    (`finish`, `lineResult`); on JSON this was over a third of all allocations;
  - field lists of struct nodes (`newStruct`) and of captures (`attachCaptures`) were separate `make` calls;
  - `function.apply(args ...any)` is an interface call, so the variadic argument slice escaped on every lambda call;
  - `list`, `map` and `concat` built their element slices with `make`/`append` and `newList` then copied them;
  - a Pratt operator action allocated a `local` for each of `$op`, `$lhs`, `$rhs`.
- Now: the one-element list is a buffer in the parser (`parser.one`; the list is only read during the action, and `$0`
  copies it); field lists come from chunks like nodes (`parser.fields`, with capacity limited to the list so an
  append past it reallocates instead of overwriting a neighbour); `apply` takes two arguments; the list built-ins
  gather elements on `kidStack` and hand the copied-out slice to the list node; the operator locals live in the
  parser (`parser.oplocals`), like the evaluation context.
- Effect (min of 6 interleaved runs, codepoints, Apple M3 Max; allocs/op before → after, time in parentheses):

  | Workload | closure | bytecode | iterative |
  |:--|:--|:--|:--|
  | JSON | 153k → 38k (22.7 → 22.0 ms) | 147k → 32k (27.2 → 26.5 ms) | 147k → 32k (38.3 → 38.1 ms) |
  | CSV | 131k → 56k (11.3 → 10.8 ms) | 116k → 41k (12.9 → 12.3 ms) | 116k → 41k (15.7 → 15.2 ms) |
  | XML | 64k → 39k (19.0 → 19.2 ms) | 72k → 47k (24.8 → 24.0 ms) | 72k → 47k (34.4 → 34.0 ms) |
  | Arith_Pratt | 134k → 41k (14.1 → 12.9 ms) | 94k → 0.7k (17.8 → 17.0 ms) | 95k → 0.8k (27.0 → 26.4 ms) |
  | Minilang | 75k → 21k (19.0 → 18.2 ms) | 68k → 14k (24.6 → 23.9 ms) | 68k → 14k (32.3 → 31.6 ms) |

  Bytes per parse drop by 2–13% (XML +0.3%: partly used field chunks).

### 16. Start positions and lambdas from chunks in the VMs

- `PUSHPOS` pushed the start position of a sequence, repetition or atomic expression onto the value stack (`[]any`),
  and converting an `int` to an interface allocates for values above 255. It now pushes a `*int` taken from a chunk
  (`parser.newPos`); the consumers (`SEQ`, `ATOMIC`, `ENDREPEAT`) dereference it.
- `EFUNC` allocated each `vmFunc`; lambdas never outlive the evaluation, and they now come from a chunk too
  (`parser.newFunc`).
- Effect (min of 6 interleaved runs, codepoints, Apple M3 Max): allocations per parse JSON 32k → 1.9k, CSV 41k → 1.2k,
  XML 47k → 19k, minilang 14k → 1.4k, for both VMs. Bytes and time are unchanged (within noise); the gain is fewer
  objects for the GC.
- The remaining XML allocations are `text(...)` results: a string converted to an interface needs a header on the
  heap (comparisons of two texts avoid it since change 36).

### 17. ASCII bitmaps for character classes in the VMs

- `CLASS` and `SCAN` tested a character against a class by looping over its ranges (`Class.has`). The VM now
  precomputes, per class, a 128-bit bitmap of the characters below 128 (negation included) when the program is
  loaded, and tests ASCII characters with one bit lookup; other characters still use the ranges.
- The same idea in the closure engine measured no gain (see the experiments table); in the VMs, where the class is
  reached through the module's class table, it is a small but consistent gain.
- Effect (min of 8 interleaved runs, Apple M3 Max): recognition 1.3–4.6% faster on JSON, CSV, XML and minilang for both
  VMs (e.g. CSV bytecode 8.81 → 8.40 ms, XML 22.3 → 21.4 ms); full parses −1 to −4% (within noise on some).

### 18. Memoizing from the second call at a position

- Counting memo entries and reuses showed that most entries are never used: JSON stored 68k entries per parse and
  reused none, CSV 30k and none, the Pratt calculator 6.6k and none, minilang reused 1.8k of 46k. Only grammars
  that really backtrack over rules (left recursion: 110k reuses; XML: 12k) use them. Each entry costs 128 bytes,
  its slot, and the isolation of its expectations.
- In whole-input parses (`ParseWith`, including recognition), a memoized rule is now not memoized on its first call
  at a position: a bit set (`memoTable.seen`, one bit per position and per rule that can be deferred) records the
  call, and the rule is memoized when it is called there again. A rule is therefore evaluated at most twice per
  position and parse time stays linear.
- Deferring costs an extra evaluation where a rule is called again. Per rule and per parse, the calls and repeated
  calls are counted, and once more than one call in sixteen is a repeat, the rule is memoized from the first call
  for the rest of the parse (the threshold was one in eight with repeats counted twice until change 24). Without this, the left-recursive calculator was 20–33% slower; with it, it is within
  0–5%. Thresholds of 8 and 32 measured the same.
- Left-recursion leaders are always memoized (the memo drives seed growing). `Document` (which needs every entry for
  reuse after edits) and streams keep memoizing on the first call.
- Consequences found in review: results that had depended on which call was memoized changed. Rule labels on nodes
  returned by actions (fixed: action results are final), errors recovered inside and outside lookaheads (fixed:
  such entries are reused only in the same context), node identity across separate calls (`==` on nodes; now
  stated as unspecified in the specification), and the nesting limit near its value (a memo hit does not nest;
  documented on `WithMaxDepth`).
- Effect (min of 6 interleaved runs, Apple M3 Max, code points; time before → after, bytes per parse in
  parentheses):

  | Workload | closure | bytecode | iterative |
  |:--|:--|:--|:--|
  | JSON | 22.5 → 18.1 ms (41.2 → 25.8 MB) | 26.0 → 22.0 ms | 37.7 → 34.2 ms |
  | CSV | 11.0 → 9.2 ms (31.5 → 22.5 MB) | 12.0 → 10.1 ms | 14.7 → 13.2 ms |
  | XML | 19.3 → 18.2 ms (32.0 → 26.8 MB) | 23.6 → 21.6 ms | 33.4 → 32.4 ms |
  | Arith_Pratt | 13.2 → 12.0 ms (19.4 → 14.2 MB) | 16.8 → 15.7 ms | 25.9 → 24.7 ms |
  | Arith_LeftRec | 33.3 → 35.2 ms (unchanged) | 34.5 → 35.6 ms | 45.8 → 45.7 ms |
  | Minilang | 18.6 → 18.0 ms (20.8 → 18.9 MB) | 24.0 → 24.3 ms | 31.7 → 32.3 ms |

  Recognition gains more, because memo entries were most of its allocations: JSON 15.9 → 12.0 ms and 17.7 → 2.4 MB
  (closure), CSV 6.8 → 4.8 ms and 10.6 → 1.6 MB. (CSV and Arith_Pratt were measured before the per-rule switch was
  added; with no repeated calls the switch never fires on them.)

### 19. A plain call path for unmemoized rules without captures

- A rule call went through `call` → `invoke` → `invokeBegin` / body / `invokeEnd`: a capture frame was allocated or
  shared and switched to, an `invokeState` was built and copied, and the failure handling of `call` came on top. In
  profiles of recognition this chain was about a fifth of the time.
- An unmemoized call (transient rules, and first calls since change 18) of a rule whose scope has no captures now goes
  through `invokePlain`, which does the same steps inline and leaves the caller's frame current (a body without
  captures never writes one). Depth limit, cut, environment, trail, recovered errors and `finish` behave as before.
  The closure backend and the recursive VM use it; the iterative VM has its own call frames.
- Effect (min of 6 interleaved runs, Apple M3 Max): full parses 2.5–9.5% faster (JSON closure 17.7 → 16.5 ms, bytecode
  21.8 → 19.8 ms; XML closure 17.8 → 16.2 ms), recognition 5.5–15.6% faster (JSON closure 11.8 → 10.0 ms, bytecode
  16.4 → 13.8 ms).

### 20. Splicing the memo table on Document edits

- `Document.Edit` rebuilt the memo table on every edit: it walked all entries and `put` each kept one into a new
  table, which allocated a new slot array and searched each chain for an existing key. In the incremental benchmark
  (a one-character edit to minilang followed by a reparse) this was about 30% of the time.
- The table is now spliced in place (`memoTable.splice`): each chain is filtered with the same keep/shift/drop rules,
  the slots from the end of the edit on move by the length difference (one `copy`), and the few entries that stay at
  the edit position (rules that examined no input) are put back. `TestDocumentEditKeepsMemo` checks after random edits
  that exactly the entries the rules allow are kept, at the right positions; it fails if those entries are lost.
- Effect (min of 6 interleaved runs, Apple M3 Max): edit plus reparse 3.50 → 2.29 ms (closure), 3.37 → 2.34 ms
  (bytecode), 3.46 → 2.33 ms (iterative); 6.0 → 3.9 MB allocated per edit.
- What remains is mostly shifting reused subtrees (`shiftNode` copies a reused node tree with its positions moved)
  and copying the input.

### 21. Changes 18 and 19 in the generated parsers

- The generated runtime (`internal/engine/genrt`) now defers memoization to the second call at a position with the
  same per-rule switch to eager memoization (the generator emits each rule's `seen` number and their count), and calls
  unmemoized rules without captures through `invokePlain`. The generated files in `bench/gen` and
  `examples/json/generated` were regenerated.
- Effect (min of 8 interleaved runs, Apple M3 Max, code points; time and bytes per parse):

  | Workload | Before | After |
  |:--|--:|--:|
  | JSON | 14.8 ms, 37.0 MB | 11.2 ms, 23.8 MB |
  | CSV | 7.2 ms, 27.4 MB | 5.7 ms, 19.4 MB |
  | XML | 11.8 ms, 29.4 MB | 10.7 ms, 25.3 MB |
  | Arith_Pratt | 8.9 ms, 16.9 MB | 7.6 ms, 11.9 MB |
  | Arith_LeftRec | 18.2 ms | 18.8 ms (unchanged bytes) |
  | Minilang | 13.2 ms, 18.6 MB | 12.8 ms, 17.2 MB |

### 22. Bounded memory and less copying in stream parsing

- **Retention.** A stream parse is meant to hold only its working set, but the reachable heap grew with the number of
  elements on several grammars (found while writing the streaming guide): about 78 MB per 100,000 records for a grammar
  with a header and captured fields, on every backend. Nodes, child lists, frames and field lists come from chunks
  (change 6), a chunk stays alive while anything in it is referenced, and it keeps alive what its objects point to.
  Consecutive elements shared chunks, so the chunk being filled reached the previous element's chunks, and so on back
  to the first element. At an element boundary, once the node chunk is nearly used up or the elements since the last
  split filled more than one chunk, the parser now starts new chunks of every kind (`splitChunks`), so chains stay
  within a group of elements. `TestParseStreamMemoryIsBounded` checks the reachable heap on every backend; it fails
  without the split.
- **Copying.** `discard` copied the whole read-ahead buffer into a new slice at every committed element. It now
  compacts in place, and only once at least half of the buffer can go.
- Effect (min of 6 interleaved runs, Apple M3 Max): streaming 50,000 CSV records 168.9 → 117.7 ms (closure), 180.6 →
  129.9 ms (bytecode), 215.3 → 160.8 ms (iterative), and 686 → 351 MB allocated per parse. The reachable heap no longer
  grows: 233 MB → 0.5 MB after 300,000 records of the grammar above. A 27 MiB, 1,000,000-record CSV file now streams with
  a peak resident size of about 61 MB (0.8 GB before; a whole-input `Parse` takes 2.2 GB).

### 23. Splicing the text of a Document in place

- After change 20, most of an edit's cost was the text: `Edit` copied the code points into a new slice, converted them
  back to a string, and rebuilt the code-point-to-byte offset table by decoding the whole string. It now replaces the
  edited range in the code points (or bytes) in place, builds the new string by concatenating substrings of the old
  one, and splices the offset table, adding the byte difference to the entries after the edit
  (`input.replace`). Nodes keep referring to the old string, which does not change. `TestDocumentEditText` checks
  after random edits with multi-byte characters and invalid UTF-8, in both units, that the text and offsets equal
  those of the edited text read from scratch.
- Effect (the program in the streaming guide, 100,000 lines, Apple M3 Max): `Edit` 12.3 → 2.7 ms (one rule per line),
  16.5 → 3.4 ms (Pratt lines), 9.2 → 0.3 ms (no memo entries); JSON document of 283 KB 6.6 → 2.7 ms per edit.

### 24. The plain call path in the iterative VM

- The iterative VM sent every rule call, memoized or not, through `callBegin` and `callEnd` (examined-range
  bookkeeping, state copies) and through the full `invokeBegin` / `invokeEnd`. Its call frame now takes the same paths
  as `parser.call`: unmemoized calls of rules without captures run the steps of `invokePlain`, other unmemoized calls
  skip the memo bookkeeping, and only memoized calls use `callBegin` and `callEnd`.
- `callBegin` used to decide again whether to memoize, so the recursive backends called `firstCall` twice for each
  memoized call and counted every repeat twice (change 18). The caller now decides once. The threshold for eager
  memoization went from 8 to 16 so that rules switch as before (minilang allocates the same as before).
- Effect (min of 6 interleaved runs, Apple M3 Max), iterative VM: full parses JSON 33.2 → 26.0 ms, XML 31.6 → 25.5 ms,
  Arith_Pratt 24.1 → 21.7 ms, Arith_LeftRec 44.7 → 39.8 ms, minilang 30.7 → 28.2 ms; recognition JSON 28.5 → 20.3 ms,
  XML 29.7 → 23.2 ms. Other backends unchanged.

### 25. Discarding separators in the example grammars

- Counting the nodes allocated against those in the final tree showed that about half of the JSON parser's nodes were
  thrown away. Among them, every element after the first in `rest:(-ws "," -ws m:member)*` kept the `","` as a `Match`
  child of the iteration's `Seq` (a captured repetition keeps its whole CST, see the guideline below): 15,550 nodes per
  parse in JSON and 25,005 in CSV. The JSON, CSV and minilang examples now discard the separator (`-","`); the ASTs
  are unchanged.
- The rest of the discarded nodes are the iteration `Seq` nodes themselves and intermediate lists built by `list`,
  `map` and `concat` in actions.
- Effect (min of 6 runs alternating the old and new grammars, Apple M3 Max, code points): bytes per parse JSON 25.8 →
  23.6 MB, CSV 22.5 → 19.0 MB, minilang 18.9 → 18.6 MB (closure); CSV 3.5% faster on the closure and bytecode backends,
  JSON and minilang unchanged within noise. Generated parsers: JSON 11.2 → 10.9 ms, CSV 5.5 → 5.2 ms.

### 26. Lambda calls without allocation in the closure backend

- Each lambda call (`closure.apply`) copied the evaluation context and allocated a `local` for each parameter, and
  each lambda expression allocated a `closure`. On JSON this was 36k of the closure backend's 37k allocations per
  parse.
- Contexts and parameters of a call now come from LIFO arenas in the parser (`arena[T]`), freed when the call
  returns; closures come from an arena freed at the next evaluation (`useCtx`), since a lambda cannot outlive the
  action or predicate that created it (lambdas are only arguments of `map`, `foldl` and `foldr`, and functions
  cannot be stored). A lambda created inside another lambda's call keeps a heap copy of that call's context (`keep`).
- Effect (min of 8 interleaved runs, Apple M3 Max, closure backend, code points): JSON 15.9 → 15.3 ms, 37k → 1.3k
  allocations, 23.6 → 21.4 MB; CSV 8.0 → 7.3 ms, 56k → 0.8k allocations, 19.0 → 15.6 MB; minilang 21k → 8k
  allocations. XML and Arith_Pratt use no lambdas and are unchanged.

### 27. Struct and call operands on the expression stack in the closure backend

- The closure backend's evaluator built two slices for every `new T{...}` (field names and values) and one for every
  built-in call (arguments). Values and arguments now go on the parser's expression stack (`parser.estack`, as in
  the VMs since change 14) and field names into a reused buffer; `newStruct` and the built-ins retain neither.
- Effect (min of 8 interleaved runs, Apple M3 Max, closure backend): Arith_Pratt 11.1 → 10.6 ms, 40.6k → 0.6k
  allocations, 14.2 → 12.2 MB; XML 38.9k → 19.1k allocations; minilang 8.3k → 1.1k allocations; times otherwise
  within noise. All backends now allocate fewer than 1.5k objects per parse on JSON, CSV, Pratt and minilang; XML's
  remaining 19k are `text(...)` results (a string stored in an interface needs a heap header).

### 28. Field lists and list built-ins without allocation in the generated parsers

- The generated runtime still allocated a field list per struct node and per node with captures, and a slice per
  `list`, `map` and `concat` call (changes 15 and 27 in the engine). Field lists now come from chunks
  (`parser.fields`), and the list built-ins gather their elements on `kidStack` and hand the copied-out slice to
  the list node. A predicate that fails with an evaluation error pops what a failed built-in gathered.
- Effect (min of 8 interleaved runs, Apple M3 Max, generated parsers, code points): JSON 10.8 → 10.3 ms, 53.9k → 1.3k
  allocations; CSV 5.2 → 4.9 ms, 45.6k → 0.8k; XML 10.5 → 10.0 ms, 31.5k → 19.1k; Arith_Pratt 7.5 → 7.2 ms, 22.8k →
  0.6k; minilang 12.2 → 11.9 ms, 19.3k → 1.1k. Bytes change by −1% to +3% (partly used chunks).

### 29. Shifting reused subtrees into the parser's chunks

- After an edit, a memo result after the edit is reused with its positions shifted: `shiftNode` copies its node tree.
  Every copied node, child list and field list was a separate heap allocation, and every copy made a new map of the
  nodes already copied (to keep shared subtrees shared). In the incremental benchmark this was 89% of the bytes
  allocated per edit and reparse.
- Copies now come from the parser's chunks (`newNode`, `nodes`, `fields`), and the map is kept by the parser and
  cleared between copies (and dropped when it grew past 1,024 entries, so clearing stays cheap).
- Effect (min of 8 interleaved runs, Apple M3 Max, one-character edit to minilang plus reparse): 1.72 → 1.20 ms
  (closure), 1.76 → 1.20 ms (bytecode), 1.77 → 1.25 ms (iterative); 20k → 0.15k allocations, 3.0 → 2.4 MB.

### 30. Building the code-point offset table on demand

- In code-point mode, preparing the input made three passes over it: converting it to code points, checking that it
  is valid UTF-8, and building the code-point-to-byte offset table that token text uses (change 10). In profiles of
  recognition this was about 4.5% of the time, though recognition rarely needs token text.
- The input is now decoded and checked in one pass, and the offset table is built the first time `text` needs it
  (and before a `Document` edit splices it).
- Effect (min of 6 interleaved runs, Apple M3 Max): recognition JSON 9.6 → 9.1 ms (closure), 13.6 → 12.9 ms
  (bytecode), CSV 4.2 → 3.8 ms (closure), 5.2 → 4.8 ms (bytecode), with 1 MB less allocated per parse; full parses and
  XML recognition (whose predicates read token text) unchanged within noise.
- Pitfall: `Document.Parse` gives the parser a copy of the input, so the table built during a parse was lost and every
  edit and every reparse of a `Document` rebuilt it from the whole text (an edit to a 100,000-line document went from
  0.3 to 2.2 ms). The incremental benchmarks were not in the measurements of this change. The `Document` now keeps
  the tables a parse builds (`TestDocumentKeepsInputTables`).

### 31. Specialized character-class tests in the closure backend

- The closure backend tested a character against a class by looping over the class's ranges in the grammar AST.
  Classes with one or two ranges (most of them) now compare against constants captured by the test function; larger
  classes test ASCII characters with a bitmap and others against a copy of the ranges.
- An ASCII bitmap alone had measured no gain (see the experiments table); removing the loop and the AST access for the
  common small classes is what pays.
- Effect (min of 10 interleaved runs, Apple M3 Max, closure backend): full parses 1.2–4.9% faster (CSV 7.1 → 6.8 ms,
  XML 15.1 → 14.7 ms), recognition 1.6–9.4% faster (CSV 3.7 → 3.3 ms, XML 12.7 → 12.1 ms).

### 32. Matching literals against loaded input in one loop

- `matchLiteral` checked one character at a time, each time recording the examined range, checking that the input was
  loaded far enough (stream input is read lazily) and indexing relative to the discarded prefix. When the whole input
  is loaded (every parse except streams), it now compares the literal in one loop and updates the position and the
  examined range once, with the same results (a mismatch still leaves the position after the matched prefix and
  examines the first differing character).
- Effect (min of 8 interleaved runs, Apple M3 Max): closure and recursive bytecode backends 1.5–7% faster (minilang
  closure 16.2 → 15.3 ms, Arith_Pratt closure 10.4 → 9.8 ms, recognition of Arith_Pratt 7.5 → 7.0 ms), except CSV on
  bytecode (unchanged within noise).

### 33. Smaller VM entries

- Every choice and every repetition iteration pushes an entry onto the VM's entry stack. Entries were 112 bytes,
  mostly fields that only `#error` labels, `#recover` and skip entries use; in profiles, pushing them was the most
  expensive line of the VM loop. Those fields moved to a separate stack (`parser.labs`, indexed from the entry), and
  the stack heights in the saved state are `int32`: an entry is now 56 bytes.
- Effect (min of 6 interleaved runs, Apple M3 Max): both VMs 1–5% faster on full parses (JSON bytecode 18.8 →
  18.0 ms, XML 19.0 → 18.2 ms) and 2–6% on recognition, including the error-recovery workload.

### 34. One frame per plain call in the iterative VM

- A rule call in the iterative VM pushed a call frame, which pushed a body frame: two frame pushes, pops and dynamic
  dispatches per call. The most common call, an unmemoized call of a rule without captures (change 24), is now run
  by the body frame alone (`plainCallFrame`), which does the call's first steps when it is pushed and the last ones
  when the body finishes. The caller decides once whether to memoize and passes the decision to the call frame.
- Effect (min of 8 interleaved runs, Apple M3 Max, iterative VM): full parses JSON 24.3 → 22.0 ms, CSV 9.9 → 9.2 ms,
  XML 24.1 → 21.9 ms, Arith_Pratt 20.3 → 19.6 ms; recognition JSON 18.4 → 14.5 ms, CSV 6.1 → 5.3 ms, XML 21.2 →
  18.9 ms; minilang unchanged within noise.

### 35. Left-recursion growth without allocation

- Growing the seed of a left-recursive rule allocated a `growState` per call of the leader and a memo entry on the
  heap for the seed and for every longer result, instead of taking entries from the memo table's chunks: 84k of the
  86k allocations per parse of the left-recursive calculator.
- The grow state is now a value (in the caller's frame, or in the iterative VM's call frame) and the entries come from
  the memo table's chunks.
- Effect (min of 10 interleaved runs, Apple M3 Max, Arith_LeftRec): full parses 6–9% faster (closure 28.9 → 26.6 ms),
  recognition 6–10% faster (closure 21.8 → 19.8 ms); 86k → 2.2k allocations.

### 36. Comparing texts without boxing them

- `text(...)` returns a string as an interface value, which allocates for the string header. The XML example compares
  the name of every end tag with its start tag (`[text($e) == text($n)]`), which was 18k of its 19k allocations per
  parse. A comparison `text(a) == text(b)` (or `!=`) is now evaluated as a string comparison by the closure backend
  and emitted as one by the code generator. The VMs still box the strings (avoiding it there would take a new
  instruction).
- Effect (min of 12 interleaved runs, Apple M3 Max, XML): closure 14.1 → 13.8 ms, generated 9.9 → 9.6 ms, recognition
  11.7 → 11.4 ms; 19k → 1.1k allocations.

### 37. No intermediate lists in `concat`

- `concat(list($first), map($rest, (r) => $r.x))`, the usual way to build a list from a first element and the rest,
  built two lists only for `concat` to copy their elements: about 19k list nodes per JSON parse. When an argument of
  `concat` is itself a call of `list`, `map` or `concat`, the closure backend now pushes that call's elements
  directly onto the stack `concat` collects on, and the code generator emits the same pushes. This is done only when
  every argument is such a call (the common case): with any other argument, `concat` checks it only after evaluating
  all of them, and fusing would report a different error when two arguments fail. The VMs still build the
  intermediate lists (fusing them there would take new instructions).
- Effect (min of 12 interleaved runs, Apple M3 Max): JSON 14.4 → 14.1 ms (closure), 10.0 → 9.8 ms (generated), 21.4 →
  20.0 MB per parse; CSV 6.6 → 6.2 ms (closure), 4.8 → 4.5 ms (generated), 15.6 → 14.0 MB; minilang unchanged within
  noise.

### 38. Calling plain rules directly from closure call sites

- Rules that are never memoized in an ordinary parse and have no captures always end up in `invokePlain` (change 19),
  but every call went through `call`'s checks first. Such rules are now marked when the program is built
  (`rule.plain`), and the closure backend's call sites call `invokePlain` directly unless a `Document` parses (which
  memoizes them).
- Effect (min of 12 interleaved runs, Apple M3 Max, closure backend): full parses 0–3% faster, recognition 2–4% (JSON
  8.4 → 8.1 ms).

### 39. A line table for error positions

- Every syntax error, recovered ones included, computed its line and column by scanning the input from the start.
  With many recovered errors this is quadratic: on the error-recovery benchmark (minilang with one line in seven
  broken) it was 14% of the time. On fully loaded input the line and column now come from a table of line starts,
  built at the first error, by binary search; stream input, whose start is discarded, keeps scanning from the
  committed position. Generated parsers do the same.
- Effect (min of 10–12 interleaved runs, Apple M3 Max, Recovery workload): full parses 15–32% faster (closure 19.2 →
  14.6 ms, generated 16.9 → 11.5 ms), recognition 17–30% faster (closure 15.5 → 10.9 ms).

### 40. One pass to prepare code-point input in generated parsers

- Generated parsers prepared code-point input in three passes, as the engine did before change 30: converting to code
  points, checking UTF-8 validity, and building the offset table for token text. Full parses always need the table,
  so it is still built eagerly, but now in the same pass as the conversion and the check.
- Effect (min of 16 interleaved runs, Apple M3 Max, generated parsers): JSON 10.1 → 9.6 ms, CSV 4.6 → 4.2 ms, XML 10.1 →
  9.7 ms.

### 41. Building the offset table while decoding, for full parses

- Since change 30 the code-point offset table is built on demand, which spares recognition a pass but costs full parses
  (which nearly always need token text) a separate pass at their first token. Full parses and `Document` now build it
  in the decoding pass (`newTextInput` with offsets), as generated parsers do (change 40); recognition keeps building
  it on demand.
- Effect (min of 10 interleaved runs, Apple M3 Max, code points): full parses 1–5% faster on the closure and bytecode
  backends (XML closure 15.3 → 14.5 ms, JSON closure 15.4 → 14.7 ms); recognition and incremental parsing unchanged.

### 42. Generated calls of plain rules

- Generated parsers called every rule through `call`, which checks whether the rule is memoized before reaching
  `invokePlain` for rules that never are (change 21). The generator knows which rules those are (`rule.plain`,
  change 38; generated parsers have no `Document`), so their call sites now call `invokePlain` directly.
- Effect (min of 12 interleaved runs, Apple M3 Max, generated parsers): JSON 9.7 → 9.3 ms, XML 9.7 → 9.1 ms, others
  0–2% faster.

### 43. Direct calls of plain rules in the VMs

- As in the closure backend (change 38) and generated parsers (change 42), the recursive VM calls rules that are
  never memoized in an ordinary parse and have no captures through `invokePlain` directly, and the iterative VM
  starts them with `plainCallFrame` without asking whether to memoize.
- Effect (min of 10 interleaved runs, Apple M3 Max): both VMs 0.5–2.5% faster on full parses and 1.4–4.6% on
  recognition (JSON bytecode 12.2 → 11.7 ms).

### 44. Changes 36 and 37 in the VMs (instruction set 2)

- The VMs now compare `text(a) == text(b)` without boxing the strings and build no intermediate lists for a `concat`
  of `list`, `map` and `concat` calls, through six new expression instructions (`ETEXTCHK`, `ETEXTEQ`,
  `ELISTBEGIN`, `ELISTPUSH`, `EMAPPUSH`, `ELISTEND`; see [bytecode.md](bytecode.md)). The instruction-set version
  of compiled files is now 2, and files of version 1 still load.
- Effect (min of 20 interleaved runs, Apple M3 Max, recursive VM, during a busy day): XML 19.2k → 1.2k allocations,
  JSON 22.0 → 20.5 MB and CSV 16.2 → 14.6 MB per parse; time within ±3% (CSV and XML 2% faster, JSON 3% slower).

### 45. Moving reused trees in place after document edits

- A `Document` reparse copied the node tree of every reused result after an edit with shifted positions
  (`shiftNode`), so it allocated in proportion to everything after the edit. Nodes are now moved in place
  (`moveResult`): each node records how many document edits its positions account for (`Node.gen`, in padding, so
  `Node` stays 120 bytes), and the document keeps its edits. A non-empty node moves by the edits that lie before it,
  which its positions tell; empty nodes at an insertion point are ambiguous and are copied instead. Edits that keep
  the length no longer mark entries as shifted.
- A review found that the first version collapsed the descendants of an empty result (captures made in a
  lookahead) onto one point, and that it replayed every edit since the last parse for every node, so a parse after
  5,000 edits took 56 ms instead of 0.7 ms on 20,000 lines. Empty nodes now move their descendants by the same
  shift, and a memo entry's own accumulated shift (with the generation at which its result was last current) gives
  the shift of most nodes; edits are replayed only for nodes moved through another result since, once per
  generation.
- Trees returned by earlier parses now change when the document is parsed again; `Node.Clone` keeps a copy.
- Effect (min of 12 interleaved runs, Apple M3 Max, minilang, one-character edit and reparse): 1.34 → 0.90 ms on the
  closure backend, 1.30 → 0.92 ms (recursive VM), 1.24 → 0.94 ms (iterative VM); 2.4 MB → 0.34 MB and 151 → 49
  allocations per edit. Most of what remains is the `madvise` calls of the Go runtime reusing scavenged memory, which
  is specific to macOS, and `memoTable.splice` walking every memo entry.

### 46. Direct calls of plain rules in generated parsers

- Generated parsers called rules that are never memoized and have no captures (`rule.plain`) through
  `invokePlain`, which runs the body through the rule's function value (a closure that calls the body's method).
  The generator now writes a method per such rule (`r<id>`) that does what `invokePlain` does and calls the body's
  method directly; for a rule without a value it also skips `finish`. Pratt rules still use `invokePlain`.
- Effect (min of 20 interleaved runs, Apple M3 Max): 0.3–6% faster on every workload (JSON 9.36 → 8.80 ms, XML
  9.22 → 8.95 ms). Generated files grow by a few lines per such rule.

### 47. Recognition in generated parsers

- Generated parsers had no recognition mode. `pego gen -recognize` (`pego.WithRecognize()`) now also generates the
  recognizer (`Program.recognizer`, the program in which no rule builds a value) into a second rule table, behind
  `Recognize(input) error`. The generator follows the engine there: captures that no predicate reads are not
  recorded, and Pratt lines of rules without a value build nothing (the generated runtime's `lineResult` and
  `prattBuild` now check `novalue`, as the engine's do).
- Effect (min of 3 runs, Apple M3 Max): JSON 4.9 ms against 8.8 ms for `Parse` and 8.6 ms for the closure backend's
  recognition; CSV 1.8 ms (`Parse` 4.0 ms), XML 7.3 ms (8.9 ms), Outline 1.7 ms (3.9 ms). The parity test checks that
  `Recognize` returns the errors of the engine's recognition on every corpus input.

### 48. A typed runtime for ParseAST

- `ParseAST` (`pego gen -types`) converted the tree of `Parse`, about 6% slower than `Parse`. For grammars whose
  results have Go types of their own, it now runs a typed runtime that builds those values directly
  (design record 012). Steps that mattered, on JSON (262 KB):
  - the runtime alone, with values as `any`: 9.8 ms, 22.4 k allocations (variadic struct constructors);
  - generated constructors `tmk_T`: 9.7 ms, 1.1 k allocations, 15 MB;
  - projected repetitions (`map($rest, (r) => $r.f)`): 9.0 ms, 12 MB;
  - frames reused when their rule returns: 7.6 MB (time within noise);
  - scratch memory pooled across parses: 2.4 MB (time within noise; the remaining cost is not allocation);
  - rule calls with methods of their own, direct actions, one recovery per parse for action errors and constructors
    of action results taking the rule's range: 8.5 → 7.6 ms.
- Effect (min of 4 runs, Apple M3 Max), `ParseAST` against `Parse` of the same generated parser: JSON 9.9 → 7.7 ms
  (20.0 → 2.4 MB), XML 10.0 → 8.0 ms (25.2 → 3.2 MB), Arith_LeftRec 18.7 → 15.0 ms (39.4 → 4.1 MB), Outline 4.5 →
  3.5 ms (13.5 → 2.6 MB), Arith_Pratt 7.8 → 8.0 ms (12.2 → 3.1 MB).
- Later, struct constructors that make the result of a Pratt line's action also take the rule's range directly:
  Arith_Pratt 7.7 → 7.2 ms (min of 12 interleaved runs), now faster than `Parse` (7.8 ms). The same per-rule call
  methods for the Node runtime's `Parse` measured within ±4% (noise) and were not adopted.
- On macOS most of the time a large allocation costs is the `madvise` calls of the Go runtime; profiles there
  attribute it to the code that touches new memory, and GOGC=off makes it worse (every span is new).

### 49. First-character dispatch in generated choices

- In generated parsers, an alternative of a choice that must begin with a given terminal (a literal or a
  character class, possibly behind sequences, captures, `@`, `-` and calls of rules that are neither
  left-recursive nor Pratt) is skipped when the next character cannot start it, recording the expectation it
  would have recorded. Such an alternative fails at once without its terminal, so nothing else is observable; the
  calls it skips would only have set memo bookkeeping, and the skip is not taken when those calls would exceed the
  nesting limit. `value = object / array / string / number / bool / null` in JSON, for instance, no longer calls
  four rules to parse a number.
- Effect (min of 12 interleaved runs, Apple M3 Max): `Parse` JSON 9.1 → 7.9 ms (−13%), XML −10%, Minilang and
  Recovery −9%, Arith_Pratt −6%, CSV −4%; `ParseAST` JSON 7.3 → 6.4 ms (−12%), XML −10%, Arith_Pratt −11%; fewer
  bytes and allocations (fewer memo entries). `Recognize` uses the same code.

### 50. First-character dispatch in the closure backend

- Change 49 in the closure backend. The choice peeks at the next character once (which records it as examined, so a
  `Document` still invalidates the result when that character changes) and skips the alternatives that cannot
  start there, recording their expectations. The bytecode VMs are unchanged (it would need an instruction). Rule
  bodies that are skipped are no longer counted in `Document.Stats`, so the counts in the incremental guide changed
  slightly (one fewer evaluation or reuse for a blank line's alternative).
- Effect (min of 12 interleaved runs, Apple M3 Max, closure backend): JSON 14.5 → 13.4 ms (−7%), Minilang −5.5%,
  Recovery −6.5%, XML and Arith_LeftRec −3%; `Document` reparse after an edit 0.95 → 0.72 ms (−23%), since skipped
  alternatives are neither memoized nor looked up; recognition within ±5%.

### 51. First-character dispatch in the VMs (instruction set 3)

- Changes 49 and 50 in the VMs: a new instruction, `GUARD c, d, l`, is emitted before the `CHOICE` of an
  alternative that must begin with a given terminal (the analysis, `firstTerminal`, is shared by every backend). The
  instruction-set version of compiled files is now 3; files of versions 1 and 2 still load (they simply have no
  guards).
- Effect (min of 10 interleaved runs, Apple M3 Max): JSON 18.5 → 16.1 ms (recursive VM, −13%) and 22.4 → 19.0 ms
  (iterative VM, −15%); Minilang −8% on both; Arith_Pratt −4% and −7%; XML −1% and −7%.

### 52. Projected repetitions in generated `Parse`

- The projection of change 48 (a repetition captured only to be mapped to one field of its elements gathers that
  field directly) now applies to the generated parsers' Node runtime too. A review of the typed runtime found the
  conditions incomplete, and they are now: the action reads the capture only as `map($x, (e) => $e.f)` and uses no
  `$n` (which can reach the same list), the names involved are captured once in the rule, and `f` is captured in
  every element with a value that is never nil (otherwise an element without a record makes `$e.f` an error).
- Effect (min of 8 interleaved runs, Apple M3 Max, generated `Parse`): CSV 3.9 → 2.7 ms (−30%, 14.0 → 8.7 MB), JSON
  7.8 → 7.0 ms (−10%, 18.4 → 15.1 MB), Minilang −4%; grammars without such repetitions unchanged.

### 53. Projected repetitions in the engine and the VMs

- Change 52 in every backend. The analysis moved to `project.go` (shared by the closure backend, the bytecode
  compiler and the code generator). It decides from the grammar alone (the engine compiles before type checking):
  a rule's value is never nil when it has a terminal type or its action makes a struct or returns such a capture,
  through `#error` and recursion. The closure backend records the projected `map` calls and its evaluator takes the
  list as it is; the VMs mark the repetition with `NEXT` mode 3 (push the field's slot) and compile the `map` with
  `(e) => $e` in place of the projection, so they need no other new instruction.
- With projections in every backend, results produced with them could only be checked against a program without
  them: the corpus tests now compare every backend and the generated parsers with the engine compiled with
  `noProjections`, and fail if the conditions are loosened (a nil field, `$n`).
- Effect (min of 10 interleaved runs, Apple M3 Max): CSV −22% (closure, 6.1 → 4.7 ms), −16% and −14% (VMs), 14.0 →
  8.7 MB; JSON −9% (closure, 13.5 → 12.3 ms), −8% and −6% (VMs); Minilang within noise.

### 54. Resuming long repetitions in a `Document`

- A reparse runs again the rule that contains the edit, and any repetition in it ran again element by element: for a
  file's `line*`, a memo lookup per line. A profile of a 100,000-line settings file put half of the reparse in that
  loop (`callBegin`, memo lookups). A repetition of 16 elements or more now records its run in a `Document` parse
  (per element: positions, examined range, expectations, value); after one edit it reuses the elements before the edit,
  parses from there, and once an element ends where an old element after the edit began, reuses the rest of the old
  run with its nodes moved in place (design 007, "Resuming repetitions"). The old run's array is updated in place, and
  the stack that collects repetition values is kept by the `Document` across parses instead of growing from empty
  each time (that growth alone was 4.3 MB per reparse at 100,000 lines).
- Effect (min of 12 interleaved runs, Apple M3 Max, closure backend): one-character insert/delete in the middle of a
  100,000-line settings file 11.0 → 5.1 ms (−54%, 8.3 → 3.8 MB); `BenchmarkIncrementalLong` (a new benchmark: a
  50,000-record CSV) 15.1 → 8.4 ms (−44%); the guide's same-length edit at 100,000 lines 9.2 → 0.6 ms for the reparse.
  Minilang `Document` +2% (its repetitions are short and are recorded only when they reach 16 elements, but every
  element pays for keeping its examined range apart); batch parses unchanged. The records cost about 80 bytes per
  element of a long repetition (Document heap at 100,000 lines 143 → 151 MiB).
- What remains of an edit at 100,000 lines is `Edit` itself (the memo splice visits every entry, about 30% of the
  insert/delete benchmark), moving the nodes after an edit that changed the length, and the new list of children.

### 55. Tracing hook on the call path

- Tracing (design 014) adds one `p.tr != nil` test at the entry of `parser.call` and one on the iterative VM's
  non-plain call path. The plain call sites test `p.noPlain` instead of `p.memoAll` (same cost); `noPlain` is set by
  `Document` and by tracing, so a traced parse sends every call through `call`. Nothing else on the untraced path
  changes, and generated parsers are untouched.
- Effect (min of 12 interleaved runs, Apple M3 Max): JSON and Minilang, closure and recursive VM, −1.3% to +0.3%
  (noise); a first 8-run pass showed up to +6% that did not reproduce. Incremental and stream benchmarks within ±4%
  in both directions.

### 56. Applying a `Document` edit to memo entries when they are looked up

- After change 54, an edit in the middle of a long document spent most of its time in `Edit`: `memoTable.splice`
  visited every memo entry to keep, shift or drop it (about 300,000 at 100,000 lines), and moved every chain after the
  edit. Now the memo table is a gap buffer with the gap at the last edit, so the positions after an edit move by moving
  the gap (the distance from the previous edit), and the edit decides only the entries at the edited positions. Every
  other entry records how many edits it accounts for (`memoEntry.vgen`, 8 more bytes per entry), and `callBegin`
  applies the edits since, with the same rules in the same order, when it finds the entry (`advanceEntry`); one that
  an edit invalidated is a miss. Lookups in other parses pay one comparison.
- Effect (min of 6 interleaved runs, Apple M3 Max): `BenchmarkIncremental` (Minilang) 0.73 → 0.17 ms (closure, −77%),
  −74% and −71% (VMs); `BenchmarkIncrementalLong` (CSV) 8.2 → 3.2 ms (closure, −61%), −31% and −37% (VMs); the
  100,000-line insert/delete 4.9 → 2.7 ms; `Edit` alone at 100,000 lines 3 ms → 0.33 ms (most of what remains copies
  the text), and 2.4 ms → 70 µs on the guide's 283 KB JSON. Allocation per reparse unchanged in steady state (the
  first edit widens the slot array once); batch parses and streams within noise.

### 57. Pooling the scratch memory of whole-input parses

- Each `Parse` allocated the decoded input and its offset table, the memo table (slots, entry chunks, the bit set of
  first calls) and the stack that collects repetition values, and dropped them when it returned: nothing in a result
  refers to them (node text slices the input string, errors are built before returning). A `Program` now keeps them
  in a `sync.Pool` (`newPooledParser`, `releaseScratch`): the memo's slots and entry chunks are cleared and reused,
  and buffers over 4M elements are not kept. Recognition uses its program's pool. `Document` and streams keep their
  own memory as before.
- Effect (min of 6 interleaved runs, Apple M3 Max, code points): full parses −1% to −8% (Minilang −8%/−7%/−6% on the
  three backends, XML −6%/−5%/−4%, JSON −2%/−1%/−1%), allocation −14% to −38% (Minilang 15.3 → 9.5 MB); recognition
  −5% to −11% (Arith_LeftRec −11%, 24.7 → 3.4 MB).

### 58. Pooling the scratch memory of generated `Parse` and `Recognize`

- Change 57 in the generated parsers' Node runtime: `parse` takes its parser from a `sync.Pool` and returns it
  (`release`), keeping the decoded input and offsets, the memo slots and entry chunks, the bit set of first calls and
  the stacks, and dropping the node, list, field and frame slabs, which the result uses. `Parse` and `Recognize` share
  the pool; the per-rule call counters are dropped when the rule table changes. The runtime now always imports `sync`.
- Effect (min of 6 interleaved runs, Apple M3 Max): `Parse` XML −9%/−12% (code points/bytes), Arith_LeftRec −12%/−15%,
  Recovery −9%/−8%, CSV −8% (code points), Minilang −6%/−5%, JSON within noise (+2%); `Recognize` XML −17%,
  Arith_LeftRec −15%, Minilang −10%, JSON −4% with one allocation per parse; `ParseAST` unchanged (it pooled already).

### 59. Resuming long repetitions in the VMs

- Change 54 in both bytecode VMs, which share `exec`: the VM finds the repetitions it can resume when it loads a
  module, from the bytecode alone (a `REPEAT`, its `ITER`, the element code, `NEXT` and `ENDREPEAT`; the element must
  contain no `PRED` or `ASSIGN` and call no rule that reads variables, and its elements are moved past a
  length-changing edit only if it calls no positional rule), so the instruction set is unchanged. In a `Document`
  parse, `REPEAT` takes over the old run and reuses the elements before the edit, `ITER` resynchronizes and starts an
  element whose examined range and expectations are kept apart (a new entry kind, `eRunIter`, undoes that on
  failure, and a cut marks it like an `ITER`), `NEXT` records the element and `ENDREPEAT` stores the run. The steps are
  shared with the closure backend (`runState` in `resume.go`).
- Effect (min of 6 interleaved runs, Apple M3 Max): `BenchmarkIncrementalLong` (50,000-record CSV) 12.0 → 4.1 ms on
  the recursive VM (−66%) and 12.5 → 4.1 ms on the iterative VM (−67%); `BenchmarkIncremental` (Minilang) −11% and
  −15%; batch parses and streams within noise.

### 60. Direct rules in the typed runtime

- The typed runtime ran every rule as the Node runtime does: a method per expression, captures in a frame with a
  trail that marks and resets undo, and the action through `useCtx` and `tctx.result`, reading the frame. A CPU
  profile of JSON `ParseAST` spread the time over those methods, `setCapture` and `reset`, `newFrame` and
  `freeFrame`, and the action plumbing (about 10%). A rule without a cut or `#recover` that is neither a Pratt rule
  nor a left-recursion leader is now compiled into the method that calls it (design 012, "Direct rules";
  `gen_direct.go`): failures jump to labels, captures are Go variables that backtracking points save and restore,
  and the action is a Go expression over them, without the checks of `tctx.result` when it makes a struct with the
  rule's range. Every rule of the JSON, XML and outline grammars is direct; in the calculators, all but the Pratt
  expression and the three left-recursive rules.
- The parity test of `ParseAST` gained grammars for the new code paths (`testdata/typed/direct_*.pego`: captures and
  an environment restored by choices, optionals and repetition elements, elements with captures of their own that
  become records, `$n`, built-ins, errors in actions, recovered errors undone and replayed from the memo, `#error`,
  anchors, bounded repetitions and alternatives that can never be tried), and now runs in both position units.
  Leaving out any one restoration (captures, environment, recovered errors), the clearing of element captures at
  each iteration, or sharing one character variable among choices makes it fail.
- Effect (min of 6 interleaved runs, Apple M3 Max): `ParseAST` JSON 6.44 → 3.86 ms (−40%), XML 6.96 → 4.34 ms (−38%),
  Arith_LeftRec 14.3 → 10.3 ms (−28%), Outline 3.32 → 2.46 ms (−26%), Arith_Pratt 6.40 → 6.03 ms (−6%); XML 4.3 →
  3.2 MB (no frames), the others unchanged in bytes. JSON `ParseAST` now takes the time of `Recognize` (3.9 ms) and
  about half that of `Parse` (7.2 ms). Generated `Parse` and `Recognize` are unchanged.

### 61. Reading code points directly in direct rules

- After change 60, `peek` (the character at the position, in either position unit) was 9% of a JSON `ParseAST`
  profile. The compiler does not inline it: its cost is 110 against a budget of 80, mostly for decoding a multi-byte
  character in `Bytes`, and still 88 with that moved into a function of its own, because the call alone costs 57.
  Direct rules now read the character from `p.in[p.pos]` while `p.pos < len(p.in)`, and call `peek` otherwise, in
  character classes, `.`, scan loops and the first-character tests of choices. `p.in` is empty in `Bytes`, which
  `run` now ensures rather than relying on how a pooled parser was cleared.
- Effect (min of 8 interleaved runs, Apple M3 Max, `ParseAST`): JSON 3.75 → 3.30 ms (−12%), XML 4.41 → 3.99 ms
  (−10%), Outline −6%, Arith_LeftRec −3%, Arith_Pratt −2% (its Pratt loop is the general code).

### 62. Comparing short literals in place in direct rules

- `matchLiteral` was the next call in the JSON `ParseAST` profile (about 4%), mostly for one-character literals such
  as `","` and `":"`. Direct rules now compare a literal of up to four code points with `p.in` in place and call
  `matchLiteral` only when it does not match there (which records the expectation) or in `Bytes`. A test grammar
  covers literals of one to five code points, non-ASCII ones and literals cut off by the end of the input, in both
  units.
- Effect (min of 12 interleaved runs, Apple M3 Max, `ParseAST`): JSON 3.34 → 3.16 ms (−5%), XML 3.89 → 3.80 ms
  (−2%), Outline 2.14 → 1.97 ms (−8%); the calculators within noise (min of 8 runs).

### 63. A smaller `Node`

- `Node` was 120 bytes: two name strings (`Type`, `Rule`), `int` positions, the text, children, fields and flags. The
  pair of names now sits behind one pointer to an interned kind (`nodeKind`; rules precompute the kinds they give
  their nodes, and programs those of the grammar's types, so a parse never looks one up), and `Start` and `End` are
  `int32`: 88 bytes. This changes the API: `Type()` and `Rule()` are methods, and positions are `int32` (Go accepts
  them as slice indexes; arithmetic with `int`s needs a conversion). JSON and `String` output are unchanged
  (`MarshalJSON` writes the same members).
- Measured beforehand with a synthetic tree of 400,000 nodes: building −15%, node memory −22%, walking −7%, a GC with
  the tree live unchanged; `int32` positions alone saved nothing, since a chunk of 256 nodes of 112 bytes falls into
  the same allocation size class as one of 120 bytes.
- Effect (min of 6 interleaved runs, Apple M3 Max): allocation per parse −11% to −20% (JSON 13.0 → 10.8 MB, XML 16.4 →
  13.5 MB, CSV 7.1 → 5.7 MB, streams 168 → 152 MB); time within ±3% on every workload and backend.

### 64. A smaller `Node` in generated parsers

- Change 63 in the generated Node runtime, whose `Node` is a type of the generated package: the kinds of a rule's nodes
  are set up in `init` with the rule table, those of struct types with `structFields`, and the emitted code makes CST
  nodes with the shared kinds of the reserved types. The typed converters read `Type()` and convert positions to the
  `int` spans of the typed values.
- Effect (min of 6 interleaved runs, Apple M3 Max): `Parse` JSON −2%/−3% (code points/bytes), CSV −8%/−12%, XML
  −7%/−5%, Arith_Pratt −3%/−4%, Arith_LeftRec −2%/−4%, Minilang within noise, Outline and Recovery +1% to +5%
  (noise-level); allocation −10% to −21% (JSON 12.8 → 10.7 MB, Outline 12.8 → 10.2 MB); `ParseAST` and `Recognize`
  unchanged within noise.

### 65. Position conversion in the language server

- `pego lsp` converted between LSP positions (UTF-16 code units) and PEGO positions (code points) by walking each line
  from its start for every token, diagnostic and range: quadratic on long lines (a 64 KB line took 0.7 s to analyze and
  1.25 s for semantic tokens; an 800 KB line did not open within 10 s). The text index now keeps a checkpoint every 64
  bytes with the cumulative counts of UTF-16 units and code points, so a conversion walks at most 64 bytes.
- Effect: a 1 MB single-line grammar (260,000 tokens) is analyzed and tokenized in 0.35 s instead of over 10 s;
  `BenchmarkAnalyze` (go.pego and python.pego) unchanged at about 11 ms.

## Grammar authoring guidelines for performance

- Inside a captured expression, discard parts the action does not need with `-x` (typically whitespace and
  punctuation): `rest:(-ws -"," -ws v:value)*`. The engine cannot drop them automatically, because a captured value
  exposes its full CST.
- Prefer terminal types (`type Name terminal`) or `@(...)` for tokens: their bodies are matched without building
  values.
- Write actions that use captures rather than `$n` where possible; a body whose action does not use `$n` is matched
  without building its CST.

## Experiments that did not pay off

| Experiment | Result | Decision |
|:--|:--|:--|
| Go arenas (`GOEXPERIMENT=arenas`) for per-parse scratch memory (memo entries, memo slots, expectation chunks), freed at the end of `Parse` | No measurable change in time or allocation: memo entries were already slab-allocated, and the tree must stay on the heap because it is returned | Not adopted. Requires an experimental build flag for no gain |
| Option to skip recording expected sets (report only the failure position) | After change 4: about −10% time on minilang, no change on JSON and CSV | Not adopted. Not worth an API knob; the full information is cheap enough |
| ASCII bitmap for character classes in the closure engine (instead of scanning the ranges) | No measurable change when measured against the unmodified code in alternating runs (JSON, CSV); classes in practice have one to three ranges, which scan as fast as a bitmap lookup | Not adopted then; change 31 specializes small classes instead, which does pay |
| Freeing capture frames when their rule invocation returns (LIFO arenas for frames and their slots, marked in `invokeBegin` and freed in `invokeEnd`) | −16 to −23% bytes per parse, but 1–6% slower on every workload and backend (recognition included), even with a fast path that skips freeing when nothing was allocated: the per-call bookkeeping and the clearing of freed slots cost more than the garbage collector saved | Not adopted |
| Storing all-ASCII input in code points as bytes (positions are the same in both units), with `len` of strings still counting code points | −9% bytes per parse on ASCII versions of the JSON, CSV and XML inputs (no rune array or offset table), but 0–4% slower: matching bytes checks for multi-byte characters at every step, which indexing code points does not | Not adopted |
| Iterative VM: holding body frames by value in the VM stack (a tagged entry instead of a pooled frame behind an interface) | 5–19% slower: each entry is about 150 bytes, and copying and clearing it on every push and pop costs more than the interface call and the pool it saves | Not adopted |
| Iterative VM: calling body frames directly (a type assertion before the interface call) and returning them to the pool without the type switch | Within ±4% (noise) | Not adopted |
| Estimating a smaller `Node` (120 → about 88 bytes with `int32` positions and interned type and rule names) by the opposite change: 32 bytes of padding | Closure: JSON +1%, XML +4%, Arith_Pratt +1%, Minilang and the long `Document` within noise, about +12% bytes; so shrinking would gain a few percent at most | Adopted later as change 63, accepting the API change for the memory it saves |
| Inlining small plain rules into direct typed rules (after change 62; a body without calls of up to 16 expressions, also tried transitively) | `ParseAST` JSON −1% to −7% depending on the run, the other benchmarks within ±3% | Not adopted: no consistent gain for more generated code |
| Setting the fields of the reused action context in place in direct typed rules, instead of copying a whole `tctx` | `ParseAST` JSON −5%, Outline +5%, the others within ±2% | Not adopted (noise) |
| Memoizing every rule (classic packrat) | 2–3× slower than the transient policy on all workloads; memo entries were never reused for leaf and single-reference rules | Replaced by the transient policy (change 1) |

## Remaining hotspots and next candidates

From profiles after change 41 (JSON, XML, minilang, error recovery; full parse and recognition):

1. **Garbage collection and memory acquisition.** With the default `GOGC`, the runtime's own work (marking, and
   `madvise` when spans are reused) is a large share of benchmark profiles. Allocations per parse are down to about a
   thousand objects, so what remains is bytes: nodes (120 bytes each, a public struct) dominate. Freeing scratch
   memory early did not pay (see the experiments table).
2. **Expectation recording** (`expect`): 5–8% in most profiles, mostly the duplicate check against the expectations
   already recorded at the farthest position. An index of the last append per expectation does not help, because it
   goes stale whenever the farthest position advances, which is the common case.
3. **The iterative VM** dispatches every frame through an interface (the VMs got changes 36 and 37 in change 44).
   Two ways of avoiding it did not pay (see the experiments table): the remaining cost is the work each frame does,
   such as saving and restoring the parser state that the recursive model keeps in Go locals.
4. **Document edits** walk every memo entry (`memoTable.splice`) to drop or shift it, and a reparse walks the reused
   trees after the edit to move them (change 45). Both are linear in the document; positions relative to a parent
   would avoid them, at the cost of an API change (`Node.Start` and `End` would no longer be absolute).
5. **Rule calls** still go through a function per call; inlining small rules into their callers at compile time
   (closure backend) or in generated code remains a candidate.

Timing is noisy on shared machines: compare B/op and allocs/op, or take the minimum of several interleaved runs
(before/after alternated with `git stash`).
