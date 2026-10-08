# Debugging and Profiling Grammars

This guide is about finding out what a grammar does with an input: which rules it calls where, why an input is
rejected, and where the time of a parse goes. It covers three commands, `pego explain`, `pego trace` and
`pego profile`, and the Go API behind them (`pego.WithTrace`, `pego.WithProfile`). The design is recorded in
[docs/design/014](../design/014-tracing-and-profiling.md).

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

## 1. The example grammar

A small configuration language with assignments, updates and calls:

```pego
// config.pego
def main = ws (stmt ws)* $$
def stmt = assign / update / call
def assign = ident ws "=" ws value ";"
def update = ident ws "+=" ws value ";"
def call = ident ws "(" ws args? ws ")" ";"
def args = value (ws "," ws value)*
def value = @(?0-9)+ / ident
def ident = @((?a-z_)+ ("." (?a-z_)+)*)
def ws = (? \t\n)*
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

## 2. Why does an input fail?

A PEG parser backtracks, so a syntax error does not come from one place: it lists everything that was expected at the
farthest position any alternative reached (see [errors-and-recovery.md](errors-and-recovery.md#1-what-a-syntax-error-is)).
When the list is long or surprising, `pego explain` tells you which rule calls contributed each item, and from where
they were called:

```
$ pego explain -g config.pego < bad.txt
2:14: syntax error: expected ")", ",", (? \t\n)

Calls that recorded what was expected at 2:14 (innermost first):

  ws 2:13: matched " ", then expected (? \t\n)
    in args 2:12
    in call 2:1
    in stmt 2:1
    in main 1:1

  args 2:12: matched "7", then expected ","
    in call 2:1
    in stmt 2:1
    in main 1:1

  ws 2:13: matched " ", then expected (? \t\n)
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
  parts always do. Such items are why error messages mention white space: here `(? \t\n)` comes from `ws`. Writing
  `ws` as `(&(? \t\n) .)*` keeps it out of the lists (see
  [writing readable messages](errors-and-recovery.md#3-writing-readable-messages)).
- `[memo]` marks a call answered from the memo: its expectations were recorded when the rule was first evaluated at
  that position, perhaps for another caller.
- Calls inside a lookahead (`&`, `!`) record no expectations, so they never appear.
- If the start rule has no `$$`, the parser itself expects the end of the input after the start rule returns. When that
  is part of the error, the last line says so: `The start rule matched up to L:C, but the input does not end there.`
- Errors recovered with `#recover` are explained one by one, each marked `(recovered)`. The expectations of the
  expression a `#recover` recovered from are listed under the call that contains it, marked `(recovered by #recover)`.
- `-n` limits the number of entries per error (default 10).

## 3. What does the parser do?

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
  $ pego trace -g lr.pego -i '1+2'      # def sum = sum "+" n / n
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

- `(level n)` after a rule name: a Pratt rule called with a binding level (`e(name)`); `[lookahead]`: a call inside
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
  pego: 2:14: syntax error: expected ")", ",", (? \t\n)
  ```

  (`args` matched, so there is no failure to show; the error comes after the trace.)

For tools, `-f json` prints one JSON object per event and line. Enter events have `event`, `rule`, `level` (if not 0),
`depth` (the absolute nesting depth, 1 for the start rule), `pos`, `line`, `col` and `lookahead`; exit events add
`matched`, `end`, `endLine`, `endCol`, `memo`, `evals` (body evaluations), `examined` (the end of the input the call
looked at) and, with `-failures`, `failure`:

```
$ pego trace -g config.pego -f json -rule call -max-depth 1 -failures < bad.txt
{"event":"enter","rule":"call","depth":3,"pos":20,"line":2,"col":1}
{"event":"exit","rule":"call","depth":3,"pos":20,"line":2,"col":1,"matched":false,"end":20,"endLine":2,"endCol":1,"evals":1,"examined":34,"failure":{"pos":33,"line":2,"col":14,"expected":["\")\"","\",\"","(? \\t\\n)"]}}
pego: 2:14: syntax error: expected ")", ",", (? \t\n)
```

`trace` takes the flags of `parse` that select what is parsed (`-s`, `-i`, `-unit`, `-backend`) and exits with status
1 when the input has a syntax error. The backends do not all make the same calls: each skips some calls that cannot
match the next character (first-character dispatch), not always the same ones, so their traces can differ in calls
that fail on the first character; results never differ.

## 4. Why is a parse slow?

`pego profile` parses the input once with tracing and prints the cost of each rule. On 900 statements (35 KB):

```
$ pego profile -g config.pego -n 5 < config.txt
result: ok
examined 35291 positions in 3.87ms (profiling included)
12009 calls, 12009 body evaluations, 0 memo hits (0% of calls)

    rule  calls  evals  memo  matched  failed  repeats  consumed  wasted     time     self  self%
    args    300    300     0      300       0        0      4690       0   1.02ms  726.8µs   18.8
   ident   2103   2103     0     2100       3      902     50700       3  602.7µs  602.7µs   15.6
      ws   5401   5401     0     5401       0     1200      3000       0  548.6µs  548.6µs   14.2
  update    601    601     0      300     301        0     10500    7801  741.7µs  434.1µs   11.2
  assign    901    901     0      300     601        0     10800   17101  878.0µs  426.6µs   11.0
(4 more rules; -n 0 shows all)

What to look at:
- Most time is spent in args (19%), ident (16%), ws (14%), not counting the rules they call.
- Evaluated again where they had been evaluated before: ws (1200 of 5401 evaluations, up to 3 times at 3:26), ident (902 of 2103 evaluations, up to 3 times at 3:1). A memoized rule is usually evaluated at most twice at a position; a rule that is not is evaluated whenever its caller is retried there. Look for choices whose alternatives begin with the same calls, and factor the common part out.
- Failed after examining much input: assign (31 positions from 2:1), update (26 positions from 3:1). A call that fails late throws its work away, and what is tried next often matches the same input again. Check whether an alternative shares a long prefix with a later one, and whether a cut (--) after the part that identifies an alternative can stop the parser from trying the others.
```

The hints point at the same problem from two sides. `ident` and `ws` call no other rule, so the engine does not memoize
them (evaluating such a rule again only reads its input again), and every alternative of `stmt` reads the target again:
`ident` consumed 50,700 positions of a 35,290-position input. And `assign` and `update` fail only after reading the
whole target, throwing away 17,101 and 7,801 positions of work (`wasted`). The fix is to read the target once and then
choose:

```pego
// config_fast.pego (changed rules)
def stmt = ident ws (assign / update / call)
def assign = "=" ws value ";"
def update = "+=" ws value ";"
def call = "(" ws args? ws ")" ";"
```

```
$ pego profile -g config_fast.pego -n 5 < config.txt
result: ok
examined 35291 positions in 3.16ms (profiling included)
9304 calls, 9304 body evaluations, 0 memo hits (0% of calls)

   rule  calls  evals  memo  matched  failed  repeats  consumed  wasted     time     self  self%
   stmt    901    901     0      900       1        0     34390       1   2.57ms  778.0µs   24.6
     ws   4501   4501     0     4501       0      300      2700       0  634.8µs  634.8µs   20.1
   args    300    300     0      300       0        0      4690       0  756.4µs  425.7µs   13.5
   main      1      1     0        1       0        0     35290       0   3.16ms  367.8µs   11.6
  ident   1201   1201     0     1200       1        0     27000       1  357.3µs  357.3µs   11.3
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

## 5. Reading the profile

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

How to use them:

- **Start from `self`.** It is where the time goes; `time` tells you which rule's subtree is expensive. Times include
  the overhead of measuring every call, which is large for small rules, so compare them with each other rather than
  with an unprofiled parse.
- **`repeats` is redundant work.** The engine memoizes most rules from the second call at a position, so a memoized
  rule normally has at most one repeat per position. Rules that call no other rule, and rules called from a single
  place, are not memoized (see [memoization](runtime.md#memoization)): they are evaluated again whenever their caller
  is. A rule evaluated more than twice at a position is flagged. The fix is usually in the caller: a choice whose
  alternatives begin alike, as in section 4.
- **`wasted` is backtracking.** Failing is normal (every choice fails before it succeeds), and failing on the first
  character costs one position. A large `wasted`, or a hint about a call that "failed after examining much input",
  means a rule recognized much of a construct before deciding it was the wrong one. Factor the common prefix, reorder
  the alternatives, or put a cut (`--`) after the part that identifies the alternative. `wasted` is inclusive: a failed
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
    value failed: 1:7: syntax error: expected (?0-9)
  pair failed: 1:7: syntax error: expected (?0-9)
main failed: 1:7: syntax error: expected (?0-9)
1:7: syntax error: expected (?0-9)
```

A `TraceEvent` has `Kind` (`TraceEnter` or `TraceExit`), `Rule`, `Level`, `Depth`, `Pos` and `Lookahead`; exit events
also have `Matched`, `End`, `Memo`, `Evals` and `Examined`, and the methods `Text` (the input matched), `Failure`
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

`Profile.Rules` lists the rules in the order they were first called; sort them as you need. Tracing works with every
backend, with `RecognizeOnly`, `ParseStream` and `Document` (the option given to `NewDocument` traces each
`Document.Parse`), and several `WithTrace` and `WithProfile` options can be combined. A `Profile` is not safe for
concurrent parses. Generated Go parsers (`pego gen`) have no tracing.

## 7. Costs and limits

- **Off, tracing costs nothing measurable.** A parse without `WithTrace` pays one pointer check per rule call that goes
  through the engine's general call path; plain calls of small rules skip that path and pay nothing. See
  [design 014](../design/014-tracing-and-profiling.md#cost) for the details.
- **On, a parse is several times slower**, mostly in the trace function. Profiles therefore show relative times.
- **The trace shows the calls the backend makes.** The backends do not skip the same calls that cannot match the next
  character, so their traces can differ; the results, errors and memo decisions are the same with and
  without tracing on every backend, which the test suite checks on all its grammars.
- **Aborted parses** (a runtime error in an action, an error from a stream's `emit`, the nesting limit) end the trace
  without the exit events of the calls in progress.
- **Streams forget lines.** A stream parse discards the input it has consumed, and with it the line structure:
  `LineCol` gives 0, 0 for a position that is no longer (or not yet) held, and a profile location that could not be
  converted prints as `position N`.
- `pego explain` parses twice (once to find the errors, once traced), so it takes about as long as `trace`.
