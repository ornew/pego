# Performance Tuning Log

This document records every performance change made to the PEGO runtime: what was changed, why, and what it bought.
It also records experiments that did **not** pay off, so they are not repeated, and the hotspots that remain.
Update it in the same commit as any performance-related change.

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
- The numbers below are for the closure backend with code-point positions unless stated otherwise. The workloads are
  the ones in `bench/` (JSON 262 KB, minilang 89 KB, CSV 211 KB).

## Current state

Closure backend, code points, median of 3 runs on an otherwise idle machine at f9e6d63 (full tables in
[benchmarks.md](benchmarks.md)):

| Workload | Full parse | Allocated per parse | Allocations per parse | Recognition only | Generated Go parser |
|:--|--:|--:|--:|--:|--:|
| JSON | 62.9 ms | 42.0 MB | 153 k | 35.4 ms | 42.9 ms / 54 k allocations |
| minilang | 52.1 ms | 22.6 MB | 137 k | 34.0 ms | 35.1 ms / 19 k allocations |
| CSV | 34.9 ms | 32.3 MB | 131 k | 21.9 ms | 24.5 ms / 46 k allocations |

Starting point (first benchmark run): JSON took ~510 ms and allocated 206 MB in 2.6 M allocations.

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
  heap.

## Grammar authoring guidelines for performance

- Inside a captured expression, discard parts the action does not need with `-x` (typically whitespace and
  punctuation): `rest:(-ws "," -ws v:value)*`. The engine cannot drop them automatically, because a captured value
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
| ASCII bitmap for character classes in the closure engine (instead of scanning the ranges) | No measurable change when measured against the unmodified code in alternating runs (JSON, CSV); classes in practice have one to three ranges, which scan as fast as a bitmap lookup | Not adopted |
| Memoizing every rule (classic packrat) | 2–3× slower than the transient policy on all workloads; memo entries were never reused for leaf and single-reference rules | Replaced by the transient policy (change 1) |

## Remaining hotspots and next candidates

From profiles after change 8 (JSON and minilang, full parse):

1. **Rule-call overhead** (`call` → `invoke` → body closure): frame setup, environment and cut save/restore, depth and
   statistics counters. Candidate: inline calls to small, non-memoized, capture-free rules at compile time.
2. **Per-character closure dispatch** for literals and classes inside sequences and choices. Candidates: merge
   adjacent literals; precompute first-character sets for choices to skip alternatives that cannot match.
3. **AST construction** (`newStruct`, `Fields`, action evaluation contexts). Each action allocates an `evalCtx`; lambdas
   allocate a `local` per argument. Candidates: pool `evalCtx`; compile actions to Go closures over slot indices.
4. **Generated Go parsers** now have the engine's runtime (change 11). What remains is mostly shared with the engine:
   rule-call overhead, `Fields` slices for struct nodes, and error-recording (`expect`) on every failed literal.
   Generator-specific candidates: inline literals and calls to small rules into the calling method.
5. **Document edits** rebuild the whole memo table on every edit (`Document.Edit`), which dominates incremental
   reparse time. Candidate: shift positions lazily or keep the table in position order and splice it.

### Work in progress (handoff)

The current task is porting the generated parsers' techniques to the VMs and continuing general optimization.
Changes 13–16 are done. Next candidates, in order:

1. Inline class comparisons in the VM (`Class.has`) for small ASCII classes.
2. Rule-call overhead and inlining of small rules (item 1 above).
3. Document edit memo rebuild (item 5 above).

Timing is noisy on shared machines: compare B/op and allocs/op, or take the minimum of several interleaved runs
(before/after alternated with `git stash`).
