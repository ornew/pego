# Debugging and Profiling Grammars

This guide is about finding out what a grammar does with an input: which rules it calls where, why an input is
rejected, and where the time of a parse goes. It covers three commands, `pego explain`, `pego trace` and
`pego profile`, and the Go API behind them (`pego.WithTrace`, `pego.WithProfile`).

Contents:

1. [The example grammar](#1-the-example-grammar)
2. [Why does an input fail?](#2-why-does-an-input-fail) `pego explain`
3. [What does the parser do?](#3-what-does-the-parser-do) `pego trace`
4. [Why is a parse slow?](#4-why-is-a-parse-slow) `pego profile`
5. [Reading the profile](#5-reading-the-profile)
6. [From Go](#6-from-go)
7. [Costs and limits](#7-costs-and-limits)

The examples use the `pego` command; build it with `go build -o pego ./cmd/pego`. Every command below was run as shown,
and its output is copied from the terminal (standard output and standard error together). Times in profiles will
differ on your machine.

## 1. The Example Grammar

A small configuration language with assignments, updates and calls:

```pego
// config.pego
rule main = ws (stmt ws)* $$
rule stmt = assign / update / call
rule assign = ident ws "=" ws value ";"
rule update = ident ws "+=" ws value ";"
rule call = ident ws "(" ws args? ws ")" ";"
rule args = value (ws "," ws value)*
rule value = [0-9]+ #atomic / ident
rule ident = ([a-z_]+ ("." [a-z_]+)*) #atomic
rule ws = [ \t\n]*
```

Two inputs, the second with a missing comma:

```
$ cat small.txt
server.port = 8080;
log.rotate(7, size);
$ cat bad.txt
server.port = 8080;
log.rotate(7 size);
```

## 2. Why Does an Input Fail?

A PEG parser backtracks, so a syntax error does not come from one place: it lists everything that was expected at the
farthest position any alternative reached (see [error messages](error-messages.md#what-a-syntax-error-is)).
When the list is long or surprising, `pego explain` tells you which rule calls contributed each item, and from where
they were called:

```
$ pego explain -g config.pego < bad.txt
2:14: syntax error: expected ")", ",", [ \t\n]

Calls that recorded what was expected at 2:14 (innermost first):

  ws 2:13: matched " ", then expected [ \t\n]
    in args 2:12
    in call 2:1
    in stmt 2:1
    in main 1:1

  args 2:12: matched "7", then expected ","
    in call 2:1
    in stmt 2:1
    in main 1:1

  ws 2:13: matched " ", then expected [ \t\n]
    in call 2:1
    in stmt 2:1
    in main 1:1

  call 2:1: expected ")"
    in stmt 2:1
    in main 1:1
```

Read it from the bottom of each entry: `main` called `stmt`, which tried `call` at 2:1; inside it, `args` matched `"7"`
and then wanted a `,` (the separator of its repetition), the `ws` calls would have taken more white space, and `call`
itself wanted the closing `)`. Each entry shows only what that call recorded itself; what nested calls recorded is
shown under them. So the input is a call whose argument list stopped after `7`, which is the missing comma.

Some things to know:

- An entry says `matched ..., then expected` when the call succeeded but tried to go further; repetitions and optional
  parts always do. Such items are why error messages mention white space: here `[ \t\n]` comes from `ws`. Writing
  `ws` as `(&[ \t\n] .)*` keeps it out of the lists (see
  [writing readable messages](error-messages.md#writing-readable-messages)).
- `[memo]` marks a call answered from the memo: its expectations were recorded when the rule was first evaluated at
  that position, perhaps for another caller.
- Calls inside a lookahead (`&`, `!`) record no expectations, so they never appear.
- If the start rule has no `$$`, the parser itself expects the end of the input after the start rule returns. When that
  is part of the error, the last line says so: `The start rule matched up to L:C, but the input does not end there.`
- Errors recovered with `#recover` are explained one by one, each marked `(recovered)`. The expectations of the
  expression a `#recover` recovered from are listed under the call that contains it, marked `(recovered by #recover)`.
- `-n` limits the number of entries per error (default 10).

## 3. What Does the Parser Do?

`pego trace` prints every rule call of a parse as an indented tree. A call whose nested calls are shown takes two
lines, its start (`rule line:col`) and its end (`rule -> result`); a call without shown nested calls takes one:

```
$ pego trace -g config.pego < small.txt
main 1:1
  ws 1:1 -> matched 1:1-1:1 ""
  stmt 1:1
    assign 1:1
      ident 1:1 -> matched 1:1-1:12 "server.port"
      ws 1:12 -> matched 1:12-1:13 " "
      ws 1:14 -> matched 1:14-1:15 " "
      value 1:15 -> matched 1:15-1:19 "8080"
    assign -> matched 1:1-1:20 "server.port = 8080;"
  stmt -> matched 1:1-1:20 "server.port = 8080;"
  ws 1:20 -> matched 1:20-2:1 "\n"
  stmt 2:1
    assign 2:1
      ident 2:1 -> matched 2:1-2:11 "log.rotate"
      ws 2:11 -> matched 2:11-2:11 ""
    assign -> failed
    update 2:1
      ident 2:1 -> matched 2:1-2:11 "log.rotate"
      ws 2:11 -> matched 2:11-2:11 ""
    update -> failed
    call 2:1
      ident 2:1 -> matched 2:1-2:11 "log.rotate"
      ws 2:11 -> matched 2:11-2:11 ""
      ws 2:12 -> matched 2:12-2:12 ""
      args 2:12
        value 2:12 -> matched 2:12-2:13 "7"
...
```

Ranges are `start-end` with the end exclusive, in lines and columns of the position unit (`-unit`). Already this short
trace shows a cost: on the second line `ident` reads `log.rotate` three times, once per alternative of `stmt`.
Section 4 comes back to it.

Other marks on a result:

- `[memo]`: the result came from the memo, without evaluating the rule.
- `[3 evaluations]`: a left-recursive rule evaluated its body three times while growing its match. The recursive calls
  inside it show as memo hits that return the match so far:

  ```
  $ pego trace -g lr.pego -i '1+2'      # rule sum =  sum "+" n / n
  main 1:1
    sum 1:1
      sum 1:1 -> failed [memo]
      n 1:1 -> matched 1:1-1:2 "1"
      sum 1:1 -> matched 1:1-1:2 "1" [memo]
      n 1:3 -> matched 1:3-1:4 "2"
      sum 1:1 -> matched 1:1-1:4 "1+2" [memo]
      n 1:1 -> matched 1:1-1:2 "1"
    sum -> matched 1:1-1:4 "1+2" [3 evaluations]
  main -> matched 1:1-1:4 "1+2"
  ```

- `(level n)` after a rule name: an Operators Rule call with a binding level (`e(name)`); `[lookahead]`: a call inside
  `&` or `!`.

Traces grow quickly, so narrow them down:

- `-max-depth n` hides calls nested deeper than `n`:

  ```
  $ pego trace -g config.pego -max-depth 2 < small.txt
  main 1:1
    ws 1:1 -> matched 1:1-1:1 ""
    stmt 1:1 -> matched 1:1-1:20 "server.port = 8080;"
    ws 1:20 -> matched 1:20-2:1 "\n"
    stmt 2:1 -> matched 2:1-2:21 "log.rotate(7, size);"
    ws 2:21 -> matched 2:21-3:1 "\n"
    stmt 3:1 -> failed
  main -> matched 1:1-3:1 "server.port = 8080;\nlog.rotate(7, siz..."
  ```

- `-rule name` shows only the calls of that rule and the calls nested in them; repeat it or separate names with
  commas. With `-rule`, `-max-depth` counts from the rule's calls:

  ```
  $ pego trace -g config.pego -rule stmt -max-depth 2 -i 'a.b.c(1);'
  stmt 1:1
    assign 1:1 -> failed
    update 1:1 -> failed
    call 1:1 -> matched 1:1-1:10 "a.b.c(1);"
  stmt -> matched 1:1-1:10 "a.b.c(1);"
  stmt 1:10
    assign 1:10 -> failed
    update 1:10 -> failed
    call 1:10 -> failed
  stmt -> failed
  ```

- `-failures` adds, to each failed call, the farthest position it reached and what it expected there:

  ```
  $ pego trace -g config.pego -rule args -failures < bad.txt
  args 2:12
    value 2:12 -> matched 2:12-2:13 "7"
    ws 2:13 -> matched 2:13-2:14 " "
  args -> matched 2:12-2:13 "7"
  pego: 2:14: syntax error: expected ")", ",", [ \t\n]
  ```

  (`args` matched, so there is no failure to show; the error comes after the trace.)

For tools, `-f json` prints one JSON object per event and line. Enter events have `event`, `rule`, `level` (if not 0),
`depth` (the absolute nesting depth, 1 for the start rule), `pos`, `line`, `col` and `lookahead`; exit events add
`matched`, `end`, `endLine`, `endCol`, `memo`, `evals` (body evaluations), `examined` (the end of the input the call
looked at), `reusedElements` when nonzero, and, with `-failures`, `failure`:

```
$ pego trace -g config.pego -f json -rule call -max-depth 1 -failures < bad.txt
{"event":"enter","rule":"call","depth":3,"pos":20,"line":2,"col":1}
{"event":"exit","rule":"call","depth":3,"pos":20,"line":2,"col":1,"matched":false,"end":20,"endLine":2,"endCol":1,"evals":1,"examined":34,"failure":{"pos":33,"line":2,"col":14,"expected":["\")\"","\",\"","[ \\t\\n]"]}}
pego: 2:14: syntax error: expected ")", ",", [ \t\n]
```

`trace` takes the flags of `parse` that select what is parsed (`-s`, `-i`, `-unit`, `-backend`) and exits with status
1 when the input has a syntax error. When a call reuses repetition elements, text output appends
`[N reused elements]`; JSON exit events append `reusedElements`. Both omit the addition for zero reuse, and JSON
enter events never include it. The trace formatter can display these events from a `Document` reparse; the ordinary
`trace` command parses its input once and normally has no repetition reuse. The backends do not all make the same calls: each skips some calls that cannot
match the next character (first-character dispatch), not always the same ones. Their traces can differ only in calls
that fail having examined no more than the next character, with the calls inside them; every other event is the same
on each backend, and results never differ.

## 4. Why Is a Parse Slow?

`pego profile` parses the input once with tracing and prints the cost of each rule. On 900 statements (35 KB):

```
$ pego profile -g config.pego -n 5 < config.txt
result: ok
examined 35291 positions in 3.87ms (profiling included)
12009 calls, 12009 body evaluations, 0 memo hits (0% of calls)
0 reused repetition elements (completed root calls)

    rule  calls  evals  memo  matched  failed  repeats  consumed  wasted     time     self  self%  reused
    args    300    300     0      300       0        0      4690       0   1.02ms  726.8µs   18.8       0
   ident   2103   2103     0     2100       3      902     50700       3  602.7µs  602.7µs   15.6       0
      ws   5401   5401     0     5401       0     1200      3000       0  548.6µs  548.6µs   14.2       0
  update    601    601     0      300     301        0     10500    7801  741.7µs  434.1µs   11.2       0
  assign    901    901     0      300     601        0     10800   17101  878.0µs  426.6µs   11.0       0
(4 more rules; -n 0 shows all)

What to look at:
- Most time is spent in args (19%), ident (16%), ws (14%), not counting the rules they call.
- Evaluated again where they had been evaluated before: ws (1200 of 5401 evaluations, up to 3 times at 3:26), ident (902 of 2103 evaluations, up to 3 times at 3:1). A memoized rule is usually evaluated at most twice at a position; a rule that is not is evaluated whenever its caller is retried there. Look for choices whose alternatives begin with the same calls, and factor the common part out.
- Failed after examining much input: assign (31 positions from 2:1), update (26 positions from 3:1). A call that fails late throws its work away, and what is tried next often matches the same input again. Check whether an alternative shares a long prefix with a later one, and whether a cut (#cut) after the part that identifies an alternative can stop the parser from trying the others.
```

The hints point at the same problem from two sides. `ident` and `ws` call no other rule, so the engine does not memoize
them (evaluating such a rule again only reads its input again), and every alternative of `stmt` reads the target again:
`ident` consumed 50,700 positions of a 35,290-position input. And `assign` and `update` fail only after reading the
whole target, throwing away 17,101 and 7,801 positions of work (`wasted`). The fix is to read the target once and then
choose:

```pego
// config_fast.pego (changed rules)
rule stmt = ident ws (assign / update / call)
rule assign = "=" ws value ";"
rule update = "+=" ws value ";"
rule call = "(" ws args? ws ")" ";"
```

```
$ pego profile -g config_fast.pego -n 5 < config.txt
result: ok
examined 35291 positions in 3.16ms (profiling included)
9304 calls, 9304 body evaluations, 0 memo hits (0% of calls)
0 reused repetition elements (completed root calls)

   rule  calls  evals  memo  matched  failed  repeats  consumed  wasted     time     self  self%  reused
   stmt    901    901     0      900       1        0     34390       1   2.57ms  778.0µs   24.6       0
     ws   4501   4501     0     4501       0      300      2700       0  634.8µs  634.8µs   20.1       0
   args    300    300     0      300       0        0      4690       0  756.4µs  425.7µs   13.5       0
   main      1      1     0        1       0        0     35290       0   3.16ms  367.8µs   11.6       0
  ident   1201   1201     0     1200       1        0     27000       1  357.3µs  357.3µs   11.3       0
(4 more rules; -n 0 shows all)

What to look at:
- Most time is spent in stmt (25%), ws (20%), args (13%), not counting the rules they call.
- Nothing else stands out: no rule is evaluated more than twice at a position, and no failed call examines much input.
```

The calls fell from 12,009 to 9,304, `ident` reads each position once, and nothing is wasted. The remaining time is
spread over rules that each do their share; `ws` is called often because the grammar asks for white space in many
places, which is normal.

Flags: `-sort` chooses the column to sort by (`self`, the default, `time`, `calls`, `evals`, `memo`, `failed`,
`repeats`, `consumed`, `wasted` or `name`), `-n` the number of rules shown (default 30, 0 for all), `-f json` prints the
same data as JSON (times in nanoseconds), and `-s`, `-i`, `-unit` and `-backend` work as for `parse`. The profile is
printed even when the input has a syntax error (the `result` line shows it), and the exit status is 0.

## 5. Reading the Profile

| Column | Meaning |
|:--|:--|
| `calls` | Calls of the rule, including calls of its value-free twin (the copy used where the value is discarded) |
| `evals` | Body evaluations: one per call that was not answered from the memo, plus one per step of a growing left recursion |
| `memo` | Calls answered from the memo |
| `matched`, `failed` | Calls that matched or failed |
| `repeats` | Evaluating calls at a position (and binding level) where the rule had been evaluated already in the same parse |
| `consumed` | Input matched by the calls that matched, summed (a call nested in a call of the same rule counts too) |
| `wasted` | Input examined by evaluating calls that failed, summed: from the call's position to the end of what it looked at, nested calls included |
| `time` | Time in the rule's calls, nested calls included; a call within a call of the same rule counts once |
| `self`, `self%` | Time in the rule's calls minus the time of the calls they made, and its share of the parse |
| `reused` | Repetition elements reused during completed calls, including nested calls; these per-rule counts are inclusive and must not be summed. |

How to use them:

- **Start from `self`.** It is where the time goes; `time` tells you which rule's subtree is expensive. Times include
  the overhead of measuring every call, which is large for small rules, so compare them with each other rather than
  with an unprofiled parse.
- **`repeats` is redundant work.** The engine memoizes most rules from the second call at a position, so a memoized
  rule normally has at most one repeat per position. Rules that call no other rule, and rules called from a single
  place, are not memoized (see [memoization](memoization.md)): they are evaluated again whenever their caller
  is. A rule evaluated more than twice at a position is flagged. The fix is usually in the caller: a choice whose
  alternatives begin alike, as in section 4.
- **`wasted` is backtracking.** Failing is normal (every choice fails before it succeeds), and failing on the first
  character costs one position. A large `wasted`, or a hint about a call that "failed after examining much input",
  means a rule recognized much of a construct before deciding it was the wrong one. Factor the common prefix, reorder
  the alternatives, or put a cut (`#cut`) after the part that identifies the alternative. `wasted` is inclusive: a failed
  call counts what its nested calls examined, so look at the innermost of the rules flagged first.
- **`memo` low and `evals` close to `calls`** is the normal state of a grammar without much backtracking. Many memo
  hits mean the memo is saving work, typically for rules shared by alternatives of a choice.
- **`calls` much larger than the input** for a rule that mostly fails means it is tried at many positions where it
  cannot match.

Work that is thrown away by a choice in a rule that still matches (the first alternative matched a few calls, then the
alternative failed, and a later one matched) is not counted in `wasted`: it shows as `repeats` of the calls the later
alternative makes again, or as failed calls of the alternatives themselves when they are rules, as `assign` and
`update` are here.

## 6. From Go

`pego.WithTrace` passes every event to a function; the methods of the event read the parser, so call them from the
function:

```go
_, err = p.Parse("a=1;b=x", pego.WithTrace(func(e pego.TraceEvent) {
	indent := strings.Repeat("  ", e.Depth-1)
	line, col := e.LineCol(e.Pos)
	switch {
	case e.Kind == pego.TraceEnter:
		fmt.Printf("%s%s at %d:%d\n", indent, e.Rule, line, col)
	case e.Matched:
		fmt.Printf("%s%s matched %q\n", indent, e.Rule, e.Text())
	default:
		fmt.Printf("%s%s failed: %v\n", indent, e.Rule, e.Failure())
	}
}))
```

For the grammar `main = pair (";" pair)* $$`, `pair = key "=" value` this prints (the full program is
`ExampleWithTrace` in `example_debug_test.go`):

```
main at 1:1
  pair at 1:1
    key at 1:1
    key matched "a"
    value at 1:3
    value matched "1"
  pair matched "a=1"
  pair at 1:5
    key at 1:5
    key matched "b"
    value at 1:7
    value failed: 1:7: syntax error: expected [0-9]
  pair failed: 1:7: syntax error: expected [0-9]
main failed: 1:7: syntax error: expected [0-9]
1:7: syntax error: expected [0-9]
```

A `TraceEvent` has `Kind` (`TraceEnter` or `TraceExit`), `Rule`, `Level`, `Depth`, `Pos` and `Lookahead`; exit events
also have `Matched`, `End`, `Memo`, `Evals`, `ReusedElements` and `Examined`, and the methods `Text` (the input matched), `Failure`
(the farthest failure recorded during the call, as a `*SyntaxError`, or nil) and `Recovered` (the errors `#recover`
recovered from during the call, whose expectations `Failure` does not include). `LineCol` converts a position.

`pego.WithProfile` adds a parse to a `pego.Profile`, which can collect any number of parses, for example a whole test
corpus:

```go
var prof pego.Profile
for _, input := range inputs {
	p.Parse(input, pego.WithProfile(&prof))
}
for _, r := range prof.Rules {
	fmt.Println(r.Rule, r.Calls, r.Evals, r.Repeats, r.Wasted, r.SelfTime)
}
for _, h := range prof.Hints() {
	fmt.Println(h)
}
```

`TraceExit.ReusedElements` counts repetition elements reused during that call, including nested calls.
`RuleProfile.ReusedElements` accumulates the same inclusive counts; parent and child rows can describe the same
reuse, so do not sum them. `Profile.ReusedElements` counts reuse once, from completed root exits. The profile JSON
key is `reusedElements`. These counts describe reuse in the current parse; memo hits do not replay earlier reuse.

Profile text prints the root-only total on its own summary line and the inclusive per-rule counts in the final
`reused` column. JSON includes `reusedElements` in the profile and each rule row, including zero values.

`Profile.Rules` lists the rules in the order they were first called; sort them as you need. Engine tracing works with
every backend, with `RecognizeOnly`, `ParseStream` and `Document` (the option given to `NewDocument` traces each
`Document.Parse`), and several `WithTrace` and `WithProfile` options can be combined. A `Profile` is not safe for
concurrent parses. Generated Go Node and typed parsers have separate opt-in
trace and profile APIs described below; they do not provide the engine
trace/profile API.

### Generated Go Node and Typed Traces

Generate a standalone Go parser with tracing hooks using `pego gen -observability`. Add `-types` to trace typed entry points too:

```bash
pego gen -g pairs.pego -pkg pairsparser -observability
pego gen -g pairs.pego -pkg pairsparser -types -observability
```

The generated package provides its own `TraceEvent` and options. Its callbacks
run synchronously and receive events for actual generated calls:

```go
_, err := pairsparser.Parse(input,
	pairsparser.WithTrace(func(e pairsparser.TraceEvent) {
		switch e.Kind {
		case pairsparser.TraceEnter:
			fmt.Printf("enter %s depth=%d pos=%d\n", e.Rule, e.Depth, e.Pos)
		case pairsparser.TraceExit:
			fmt.Printf("exit %s matched=%t end=%d\n", e.Rule, e.Matched, e.End)
		case pairsparser.TraceSkipped:
			fmt.Printf("skip %s at %d (%s)\n", e.Rule, e.Pos, e.SkipReason)
		}
	}),
	pairsparser.WithTraceFilter("pair"),
	pairsparser.WithTraceMaxDepth(4),
)
```

Filtering a rule includes its named descendants; unknown names match nothing.
With no filter names, all entered rules are delivered. Depth starts at one for
the start rule; zero or a negative limit is unlimited. `TraceEnter` and
`TraceExit` describe calls that ran. `TraceSkipped` is unpaired and currently
reports only first-character dispatch skips whose target can be identified.
In batch parses `ExaminedKnown` is false, so do not interpret `Examined` as a
complete input range; Document parses report it, as described below. An aborted parse may omit exit events. Callback panics propagate
unchanged after cleanup.

With `-types`, `ParseAST` and `ParseRuleAST` accept the same trace options. This
applies to both direct typed execution and Node-conversion fallback. Events
describe the rule calls made by the selected path; typed conversion itself is
not a separate rule event. The Node and typed traces can therefore differ.

For example, the same callback can observe typed parsing:

```go
_, err := pairsparser.ParseAST(input,
	pairsparser.WithTrace(func(e pairsparser.TraceEvent) {
		fmt.Printf("%v %s depth=%d pos=%d\n", e.Kind, e.Rule, e.Depth, e.Pos)
	}),
)
```

This Go mode supports Node batch `Parse`, `ParseRule` and, when generated
with `-recognize`, `Recognize`; with `-types`, it also supports `ParseAST` and
`ParseRuleAST`. It can be combined with `-document`; see [Tracing and Profiling
Generated Go Documents](#tracing-and-profiling-generated-go-documents). With
`-stream`, `ParseStream` accepts the same options for the whole stream; its
events report no examined range, and a profile counts the stream as one parse.
Generated Go traces also report their generated execution strategy rather than promise
event-for-event equivalence with engine traces. Generated TypeScript traces
and profiles have their own contracts and do not promise event-for-event
equivalence with engine traces. See [code generation](code-generation.md#the-pego-gen-command)
for the generation option. Focused synthetic measurements cover Go Node and
typed batch parsers. Typed no-callback runs added about 48 B/op with unchanged allocation
counts; callback cost depends on parser path and workload, and the measured
callbacks store scalar event fields rather than logging. These are not
workload-wide estimates. See
[design 043](../design/043-generated-parser-observability.md#performance-and-results).

#### Profiling Generated Go Batch Parsers

With `-observability`, generated Go Node, typed batch and Document packages
expose `Profile` and `WithProfile`. Use a zero-value profile to collect several
parses, then take an owned, deterministic snapshot. For Document usage, see
[Tracing and Profiling Generated Go Documents](#tracing-and-profiling-generated-go-documents).

```go
var profile pairsparser.Profile
_, err := pairsparser.Parse(input, pairsparser.WithProfile(&profile))
if err != nil {
	log.Fatal(err)
}
snapshot := profile.Snapshot()
data, err := json.Marshal(snapshot)
```

`Snapshot` sorts rule rows and skip reasons and copies their slices. The
snapshot separates entered calls from calls that completed, counts skips
independently of trace filters, and reports `ExaminedKnown`/`WastedKnown` as
false for rules called in batch parses, because their examined ranges are not
tracked.
Position counts use the unit of each parse; avoid combining profiles collected
with different units. A `Profile` is not safe for concurrent use, though
reentrant parses on one goroutine have independent invocation state.

Times include synchronous enter and exit callback durations when tracing is
also enabled. Root time excludes input setup, whole-input end checks and typed
conversion. A same-rule recursive call is counted once per invocation for its
rule time; self time subtracts child time. Completed-call counters are
published after the exit callback returns or panics, so a snapshot inside that
callback can omit that call until the callback unwinds. Callback panics still
propagate unchanged after cleanup. In focused synthetic runs, `WithProfile`
was 3.8–26.9× the ordinary package time depending on API and fixture, adding
about 45–395 KB/op and 25–50 allocations/op across the measured cases. The
current generator without a profile was near parity with the prior
trace-only generator. These are noisy, workload-specific comparisons, not a
general estimate; see [design 043](../design/043-generated-parser-observability.md#generated-go-profile-measurements).
TypeScript batch profiling has its own `Profile` API, described below.

#### Tracing and Profiling Generated Go Documents

Generate the Go Document with observability enabled:

```bash
pego gen -g pairs.pego -pkg pairsparser -document -observability
```

Constructor trace and profile options are defaults for later parses. They do
not run during construction, and an option passed to `Parse` overrides that
call only. Editing does not parse or emit events.

```go
var profile pairsparser.Profile
doc, err := pairsparser.NewDocument(input,
	pairsparser.WithTrace(func(e pairsparser.TraceEvent) {
		fmt.Printf("%v %s depth=%d pos=%d memo=%t evals=%d\n",
			e.Kind, e.Rule, e.Depth, e.Pos, e.Memo, e.Evals)
	}),
	pairsparser.WithProfile(&profile),
)
if err != nil {
	log.Fatal(err)
}
node, err := doc.Parse()
if err != nil {
	log.Fatal(err)
}
_ = node
_ = profile.Snapshot()
```

The trace reports calls made during each parse. A warm memoized call is
reported as a memo hit (`Memo=true`, `Evals=0`); its earlier nested calls are
not replayed. Direct typed parsing and Node-conversion fallback report their
actual parser calls, without a separate conversion event. For completed parses,
the invocation's increases in profile `Evals` and `MemoHits` match
`doc.Stats().Evaluated` and `.Reused`, respectively; an interrupted parse can
include calls that began but did not complete. Exit events report
`ExaminedKnown=true`, and `Examined` is the end of the input the Document
treats the call as having read: an edit starting before that position
discards the call's memo result, as does an edit at or before the call's
position. It is conservative, typically one position past what the call looked
at and possibly one past the end of the input, so use it to see why reuse
stopped rather than as an exact read count. Edits that complete a UTF-8
sequence just before them, and left-recursive results still growing, also
prevent reuse. Profiles of Document parses sum it into per-rule `Examined` and
`Wasted` (failed calls only); mixing batch parses into the same `Profile` makes
those values unknown. Generated Documents do not resume repetition bodies, so there is no
`ReusedElements` count.

A trace callback cannot call `Parse`, `ParseAST` or `Edit` on the Document
being parsed: those calls return `ErrDocumentBusy` and leave the Document
unchanged, while the outer parse continues. Schedule that work after the
current call or use another Document.
Per-call overrides do not change the constructor defaults, and temporary
observer/profile state is released when a parse returns or panics.

### Generated TypeScript Traces

Generate tracing hooks with `pego gen -lang ts -observability`; add `-types` to
trace typed-result parsing and `-recognize` to trace recognition calls. Tracing
is delivered through the per-call options APIs, not the legacy positional-unit
functions:

```bash
pego gen -lang ts -g pairs.pego -observability -types -recognize -o pairs.ts
```

```ts
import { parseASTWithOptions } from "./pairs.ts";

const result = parseASTWithOptions(input, {
  trace: (event) => console.log(event.kind, event.rule, event.depth, event.pos),
  traceRules: ["pair"],
  traceMaxDepth: 4,
});
```

`parseWithOptions`, `parseRuleWithOptions` and `recognizeWithOptions` accept the
same trace options. With `-types`, the configured-start `parseASTWithOptions`
does as well; TypeScript does not add a named typed-rule API. The legacy
positional-unit functions do not accept trace callbacks. `traceMaxDepth` must
be a safe integer; zero and negative values mean unlimited depth. Events use
camel-case fields and the selected position unit. The `"enter"` and `"exit"`
kinds describe actual calls; `"skipped"` currently identifies first-character
dispatch skips. Omitted or empty `traceRules` delivers all rules; a selected
rule includes all descendant calls, while unknown rule names select none.
`examinedKnown` is always false. Callback
events are copied values, callbacks run synchronously, and thrown values
propagate unchanged after cleanup. Typed conversion does not add a separate
event, and an aborted parse may have no exit for active calls. Generated
TypeScript traces do not promise event-for-event equivalence with the engine.
In focused synthetic runs,
no-callback option APIs ranged from 0.991–1.044× ordinary time, while parse
callback profiles ranged from 1.052–1.288× depending on workload and filter; paired
results were noisy and are not a general performance estimate. See
[design 043](../design/043-generated-parser-observability.md#typescript-batch-measurements)
for the measurement scope and limits.

#### Profiling Generated TypeScript Batch Parsers

Generate with `-observability` to export `Profile` and `ProfileSnapshot`.
Pass one profile to a per-call options API and read its owned snapshot after
the parse:

```ts
import { Profile, parseWithOptions } from "./pairs.ts";

const profile = new Profile();
const result = parseWithOptions(input, { profile });
const snapshot = profile.snapshot();
console.log(JSON.stringify(snapshot));
```

The same option works with `parseRuleWithOptions` and
`recognizeWithOptions`; with `-types`, it also works with
`parseASTWithOptions`. The legacy positional-unit APIs do not accept profiling
options. A `Profile` aggregates across parses, but cannot be shared by
concurrent calls. Its snapshot owns rule rows sorted by name, skip-reason
counts sorted by reason, and each `maxEvalsAt` location. Modifying a snapshot
does not change later snapshots. No input, callback, parser history or
per-call state is retained.

The camel-case counters mirror the generated Go profile. `entered` counts
started calls; `calls`, evaluation counts, matches/failures and timings cover
completed calls. Skips are counted independently of trace filters.
`examinedKnown` and `wastedKnown` are false. `time` and `selfTime` are in
milliseconds and use the host's monotonic `performance.now()` clock. Root time
excludes input setup, whole-input end checks and typed-result conversion; rule
time counts recursive calls of the same rule once per invocation, and self
time excludes child time. Both include synchronous trace callback work. When
the exit-time clock read succeeds, an exit is recorded after its callback
returns or throws. A snapshot taken inside that callback can omit the current
call until it unwinds. Callback throws retain their original value after
cleanup.

Profiling requires `performance.now()`; without it, a profiled invocation
returns an `Error`. The runtime has no `Date.now()` fallback. Clock access or
invocation failures preserve the thrown value, while non-finite or backward
readings fail explicitly and can leave a partial profile. If both a trace
callback and a later clock read fail, the callback's original thrown value
wins and that exit may be omitted from the profile. Focused Node 24.19 runs
measured profile paths at 1.58–4.02× ordinary time, depending on API and fixture. These synthetic runs did not measure
JavaScript allocations, retained heap or browser timing; see [design
043](../design/043-generated-parser-observability.md#typescript-profile-measurements)
for conditions and limits.

## 7. Costs and Limits

These costs and limits describe engine tracing and profiling; generated-parser
measurements are linked above.

- **Off, tracing costs nothing measurable.** A parse without `WithTrace` pays one pointer check per rule call that goes
  through the engine's general call path; plain calls of small rules skip that path and pay nothing. See
  [design 014](../design/014-tracing-and-profiling.md#cost) for the details.
- **On, a parse is several times slower**, mostly in the trace function. Profiles therefore show relative times.
- **The trace shows the calls the backend makes.** The backends do not skip the same calls that cannot match the next
  character, so their traces can differ. With and without tracing, the results and errors are the same on every
  backend, and so are the number of evaluations and memo reuses, the memo entries and the memoization decisions; the
  test suite checks this on all its grammars, and for `Document` checks the results and `Document.Stats` across
  edits on the backend corpus.
- **A `Document` reparse skips calls.** It reuses the unchanged elements of long repetitions without calling their
  rules (see [incremental parsing](incremental-reuse.md#what-is-reused-and-why)), so those
  calls do not become synthetic trace events, calls, evaluations or memo hits. `ReusedElements` reports the number
  of repetition elements reused separately; memo hits still appear as ordinary entered calls. A reused element can
  contain several omitted calls, so this is an element count rather than a count of skipped rule calls.
- **Aborted parses** (a runtime error in an action, an error from a stream's `emit` or reader, the nesting limit, a
  panic in the trace function, which propagates out of the parse) end the trace
  without the exit events of the calls in progress. Without a root exit, the parse contributes no reuse to
  `Profile.ReusedElements`, even if a completed child row already reported reuse.
- **Streams forget lines.** A stream parse discards the input it has consumed, and with it the line structure:
  `LineCol` gives 0, 0 for a position that is no longer (or not yet) held, and a profile location that could not be
  converted prints as `position N`.
- `pego explain` parses twice (once to find the errors, once traced), so it takes about as long as `trace`.
