# Streaming

`Parser.Parse` takes a whole string and returns a whole tree. When the input is too big to hold, or arrives as a
stream you want to process as it comes, `Parser.ParseStream` reads an `io.Reader` and hands your function each element
of a repetition as soon as it matches. `Parser.Iterate` lets you request one element at a time. Both forget committed
elements; retained input and tree memory depend on the size of one element,
not on the size of the input. By default, recovered diagnostics are retained
until EOF; `WithDiagnosticLimit` can bound the retained diagnostic tail.

|  |  |
|:--|:--|
| Input | An `io.Reader`, possibly unbounded |
| API | `Parser.ParseStream`, `Parser.Iterate`, and `pego parse -stream` on the command line |
| Grammar | The start rule has a `#stream` repetition |
| You get | Each element of that repetition, as soon as it matches |
| Saves | Memory: neither the input nor the tree ever exists whole |

`#stream` has no effect on `Parse`, and `ParseStream` works with every backend ([Runtime](running-parsers.md)) and with both
position units. It does not work with `RecognizeOnly` (`ParseStream` returns an error). A parser produced by `GenerateGo` has a
`ParseStream` and `Iterate` of its own, with the same diagnostic options, when generated with `-stream`, and with
`-types` also `ParseStreamAST` and `IterateAST` for typed elements ([Code generation](generated-streams.md)). If the problem is the opposite one, a text that stays in memory and is edited again and again, see
[Incremental parsing](incremental.md): a `Document` is always parsed as a whole, so the two do not combine.

The design is in [design record 007](../design/007-streaming-and-incremental-parsing.md); the pragma is specified in
[spec/pragmas.md](../../spec/pragmas.md#stream).

Recognition treats `#stream` as an ordinary repetition and invokes no callbacks. Captures needed by element
predicates still work, but element values are not collected or decorated with captures. This applies to both VM
backends as well as Closure; recognition from a saved grammar requires its AST.

- [A First Example](#a-first-example)
- [Pulling One Element at a Time](#pulling-one-element-at-a-time)
- [What Is Emitted, and When](#what-is-emitted-and-when)
- [Rules and Limits](#rules-and-limits)
- [Positions and Text](#positions-and-text)
- [Errors](#errors)
- [Memory](#memory)
- [Cancelling a Stream](#cancelling-a-stream)
- [See Also](#see-also)

## A First Example

`#stream` marks a repetition at the top level of the start rule. Everything the repetition matches is handed to your
function one element at a time, and forgotten.

```pego
package kv

type Pair = { Key Key; Value Value }
type Key terminal
type Value terminal

rule main = pair* #stream $$

rule pair Pair = k:key "=" v:value "\n" => Pair{Key: $k, Value: $v}
rule key Key = [a-z_]+ #atomic
rule value Value = [0-9]+ #atomic
```

From the command line, `pego parse -stream` prints one line per element (JSON by default, `-f sexpr` for
S-expressions). Elements that matched before an error are printed, then the error is reported:

```bash
printf 'a=1\nbb=22\nccc=x\n' | pego parse -g kv.pego -stream
```

```text
{"type":"Pair","start":0,"end":4,"fields":{"Key":{"type":"Key","rule":"key","start":0,"end":1,"text":"a"},"Value":{"type":"Value","rule":"value","start":2,"end":3,"text":"1"}}}
{"type":"Pair","start":4,"end":10,"fields":{"Key":{"type":"Key","rule":"key","start":4,"end":6,"text":"bb"},"Value":{"type":"Value","rule":"value","start":7,"end":9,"text":"22"}}}
pego: 3:5: syntax error: expected [0-9]
```

From Go (in a module that requires `github.com/ornew/pego`):

```go
package main

import (
	"fmt"
	"os"

	"github.com/ornew/pego"
)

const grammar = `package kv

type Pair = { Key Key; Value Value }
type Key terminal
type Value terminal

rule main =  pair* #stream $$

rule pair Pair =  k:key "=" v:value "\n" => Pair{Key: $k, Value: $v}
rule key Key =  [a-z_]+ #atomic
rule value Value =  [0-9]+ #atomic `

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	err = p.ParseStream(os.Stdin, func(n *pego.Node) error {
		key := n.Field("Key").(*pego.Node)
		value := n.Field("Value").(*pego.Node)
		fmt.Printf("[%d,%d) %s = %s\n", n.Start, n.End, key.Text, value.Text)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

```bash
printf 'a=1\nbb=22\nccc=x\n' | go run .
```

```text
[0,4) a = 1
[4,10) bb = 22
error: 3:5: syntax error: expected [0-9]
```

Notice that the third line is reported as an error at `3:5`, even though the parser had thrown away the first two lines
by then: line and column numbers, like node positions, refer to the whole input.

## Pulling One Element at a Time

`Parser.Iterate` provides a pull interface for the same `#stream` grammar. Creating an iterator validates the entry point but does not read. Each `Next` advances parsing to one committed element or the terminal result. The `ok` value distinguishes a successful nil element from end of stream:

```go
it, err := p.Iterate(reader)
if err != nil {
    return err
}
defer it.Close()
for {
    node, ok, err := it.Next()
    if err != nil {
        return err
    }
    if !ok {
        break
    }
    // node may be nil when the element's value was discarded.
    consume(node)
}
```

The example assumes `p`, `reader` and `consume` are in scope. Repeated calls to `Next` after completion return the same terminal error or clean EOF. `Close` is idempotent; if called early, later `Next` returns `ErrStreamClosed`. The iterator does not close its reader. It is owned by one goroutine: calls to `Next` and `Close` must not overlap, including from a callback. `Close` cannot interrupt a blocked `Read`; arrange cancellation through the reader you own. PEG lookahead and buffering may read beyond the element returned by a `Next` call.

## What Is Emitted, and When

- **One element per iteration.** The element is the value one iteration of the repetition contributes to the `List` of
  an ordinary parse: a struct node if the element is a rule with an action, a CST node (`Seq`, `Match`, ...) otherwise.
  If the element has no value (for example `(-"a")* #stream`) your function receives a `nil` node, so check for `nil`
  if your elements can be value-free.
- **As soon as it matches.** The callback runs in the middle of the parse, before the parser tries the next element.
  A slow callback slows the parse, and it also stops reading: nothing is read from the `io.Reader` while your function
  runs.
- **Only the elements.** `ParseStream` returns just an `error`: there is no tree, and the elements are not collected
  into the repetition's `List`. The values of whatever comes before or after the repetition (a header, a footer) are
  parsed and checked, but thrown away. To see a header, make it an element of the
  repetition and tell the kinds of element apart by the type of the node in the callback.
- **An element is final.** Once the callback has seen an element, the parser never goes back before it. This is why the
  grammar must say where the cut points are with `#stream`: PEG backtracks, and without such a promise no part of the
  result is final until the end of the input. The corollary is that **what you receive is provisional until
  `ParseStream` returns nil**: the input can still turn out to be invalid after the last element (see
  [Errors](#errors)).

Input is read only as far as it is needed. Elements that end in a fixed literal are delivered the moment their last
character arrives; an element that ends in a repetition (`+`, `*`, `?`) needs to see one more character first, because
that is how the parser knows the repetition has ended. This matters when input trickles in, as over a network.
This delivery behavior is the same in both position units: decoding an ASCII character or a complete UTF-8
sequence does not wait for unrelated future bytes. An incomplete UTF-8 prefix waits for the remaining bytes or
the reader's actual EOF; a non-EOF reader error terminates the parse (see [Errors](#errors)).
This program feeds a pipe in chunks 200 ms apart and shows when each element is delivered:

```go
package main

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ornew/pego"
)

// run feeds the chunks to the parser through a pipe, 200 ms apart, and prints when each element arrives.
func run(grammar string, chunks ...string) {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	pr, pw := io.Pipe()
	start := time.Now()
	go func() {
		for i, c := range chunks {
			if i > 0 {
				time.Sleep(200 * time.Millisecond)
			}
			fmt.Printf("%4dms  write %q\n", time.Since(start).Milliseconds()/100*100, c)
			io.WriteString(pw, c)
		}
		pw.Close()
	}()
	err = p.ParseStream(pr, func(n *pego.Node) error {
		fmt.Printf("%4dms  element [%d,%d)\n", time.Since(start).Milliseconds()/100*100, n.Start, n.End)
		return nil
	})
	fmt.Println("        err:", err)
}

func main() {
	fmt.Println("elements that end with a literal:")
	run(`rule main =  line* #stream $$
rule line =  [a-z]+ #atomic "\n" `, "ab\nc", "d\n", "ef\n")

	fmt.Println("elements that end with a repetition:")
	run(`rule main =  word* #stream $$
rule word =  [a-z]+ #atomic " "? `, "ab ", "cd", " ef")
}
```

```text
elements that end with a literal:
   0ms  write "ab\nc"
   0ms  element [0,3)
 200ms  write "d\n"
 200ms  element [3,6)
 400ms  write "ef\n"
 400ms  element [6,9)
        err: <nil>
elements that end with a repetition:
   0ms  write "ab "
   0ms  element [0,3)
 200ms  write "cd"
 400ms  write " ef"
 400ms  element [3,6)
 400ms  element [6,8)
        err: <nil>
```

In the first run `ab\n` is delivered before `c` has even been completed. In the second, `cd` is held back until the
next chunk shows it is not followed by another letter, and `ef` until the end of the input.

## Rules and Limits

The rules that matter when you write a grammar, in short:

- `#stream` goes on a repetition (`*`, `+` or `#repeat(n,m)`) that is the body of a rule or an item of its top-level
  sequence, such as `items:record* #stream`, and a rule has at most one.
- Only the **start rule** streams. `ParseStream` fails with `rule main has no #stream repetition` if the start rule has
  none, even when a rule it calls does; use `Parser.WithStart` to stream from another rule. Everywhere else the
  repetition is an ordinary one.
- After the repetition ends, the rest of the start rule must match as usual, normally `$$`. With finite bounds,
  `"a" #repeat(1,2) #stream "a" $$` accepts `aaa` and delivers two elements, and a later failure does not retract the
  elements already delivered.
- `#recover` cannot wrap the `#stream` repetition, because rewinding could reach input that is already discarded. Put
  recovery inside each repeated element or its rule.
- An element that can match nothing is delivered once and then ends the repetition. Without a guard like the `!$$` in
  [parsers/csv](../../parsers/csv/csv.pego), a record rule whose fields and line end can all match nothing delivers
  one extra empty record at the end of the input.
- `WithUnit`, `WithBackend` and `WithMaxDepth` work; `RecognizeOnly` is an error. After an element only the one
  character before it is kept, so `^` still works at the start of the next element.
- A `Parser` can run many `ParseStream` calls at once.

The [specification](../../spec/streaming.md#contract) states each of these exactly.

Misplaced `#stream` pragmas are compile errors; the specification lists them under
[Validation and Errors](../../spec/streaming.md#validation-and-errors).

## Positions and Text

`Node.Start` and `Node.End`, `startPos` and `endPos` in actions, `SyntaxError.Pos`, `Line` and `Col` all count from the
start of the whole input, whichever part has been dropped (the parser keeps a running line and column for the dropped
part). `WithUnit(pego.Bytes)` switches all of them to bytes: the `!` in `éé!` is at column 3 counted in code points and at
column 5 counted in bytes.

The parser does not keep text for you. Terminal nodes (`Match`, terminal types, `(...) #atomic`) carry their text in `Node.Text`;
structural nodes (`Seq`, `List`) do not, and by the time your callback runs the input of an element is already being
discarded. If you need the raw text of an element, capture it as a terminal in the grammar. For the same reason,
`text(...)` in an action of the start rule, which runs after the elements have gone, returns only the part of the
node's input that is still held (possibly `""`).

## Errors

```go
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ornew/pego"
)

func compile(src string) *pego.Parser {
	p, err := pego.CompileSource(src, "main")
	if err != nil {
		panic(err)
	}
	return p
}

// failingReader returns data and then err instead of io.EOF.
type failingReader struct {
	data string
	err  error
}

func (r *failingReader) Read(b []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}
	n := copy(b, r.data)
	r.data = r.data[n:]
	return n, nil
}

func main() {
	lines := compile(`rule main =  line* #stream $$
rule line =  [a-z0-9]+ #atomic "\n" `)
	print := func(n *pego.Node) error {
		fmt.Printf("  element [%d,%d)\n", n.Start, n.End)
		return nil
	}

	// 1. The input stops matching the grammar after some elements were handed over.
	fmt.Println("syntax error:")
	err := lines.ParseStream(strings.NewReader("ab\ncd\n!\n"), print)
	var se *pego.SyntaxError
	if errors.As(err, &se) {
		fmt.Printf("  %v (line %d, column %d, position %d)\n", err, se.Line, se.Col, se.Pos)
	}

	// 2. The callback returns an error: parsing stops and ParseStream returns the error as is.
	fmt.Println("callback error:")
	errEnough := errors.New("enough")
	seen := 0
	err = lines.ParseStream(strings.NewReader("a\nb\nc\nd\n"), func(*pego.Node) error {
		if seen++; seen == 2 {
			return errEnough
		}
		return nil
	})
	fmt.Printf("  err == errEnough: %v, callbacks: %d\n", err == errEnough, seen)

	// 3. The reader fails.
	fmt.Println("reader error:")
	errReset := errors.New("connection reset")
	err = lines.ParseStream(&failingReader{data: "ab\ncd\nef", err: errReset}, print)
	fmt.Printf("  errors.Is(err, errReset): %v\n", errors.Is(err, errReset))

	// 4. Errors recovered with #recover are reported after the last element.
	fmt.Println("recovered errors:")
	entries := compile(`rule main =  entry* #stream $$
rule entry =  (name "=" num "\n") #recover(skip=[^\n]* "\n")
rule name =  [a-z]+ #atomic
rule num =  [0-9]+ #atomic `)
	err = entries.ParseStream(strings.NewReader("a=1\nb=x\nc=3\nd\n"), func(n *pego.Node) error {
		fmt.Printf("  element %v\n", n)
		return nil
	})
	var recovered pego.SyntaxErrors
	if errors.As(err, &recovered) {
		for _, e := range recovered {
			fmt.Printf("  recovered: %v\n", e)
		}
	}
}
```

```text
syntax error:
  element [0,3)
  element [3,6)
  3:1: syntax error: expected [a-z0-9], end of input (line 3, column 1, position 6)
callback error:
  err == errEnough: true, callbacks: 2
reader error:
  element [0,3)
  element [3,6)
  errors.Is(err, errReset): true
recovered errors:
  element (Seq "a"@name "=" "1"@num "\n")@entry
  element Error"b=x\n"{message=`2:3: syntax error: expected [0-9]`}@entry
  element (Seq "c"@name "=" "3"@num "\n")@entry
  element Error"d\n"{message=`4:2: syntax error: expected "=", [a-z]`}@entry
  recovered: 2:3: syntax error: expected [0-9]
  recovered: 4:2: syntax error: expected "=", [a-z]
```

1. **The input does not match.** Elements that matched before the failure have already been delivered. The error is a
   `*pego.SyntaxError` with the absolute position, line and column. Here the first two lines were delivered, and the
   error is reported at line 3: the repetition ended at the `!`, so the parser expected either another element or the
   end of input. A trailing `$$` is the usual way to make "unparsable input" an error rather than a silent stop.
2. **Your callback returns an error.** Parsing stops at once and `ParseStream` returns your error unchanged, so
   `err == errEnough` and `errors.Is` both work. No further element is delivered. This is also how you stop early on
   purpose: return a sentinel error and compare against it.
3. **The reader fails.** Any error from `Read` other than `io.EOF` is returned as it is. Elements completed before it are
   delivered; an element cut in two by the failure is not.
4. **Recovered errors.** With [`#recover`](../../spec/pragmas.md#recover) inside the element, a bad element becomes
   an `Error` node (delivered like any other element) and parsing continues. At the end, `ParseStream` returns the
   recorded errors as `pego.SyntaxErrors`, even though every element was delivered. Treat that as "success with
   diagnostics" and use `errors.As(err, &recovered)` to tell it from a fatal `*SyntaxError`. By default all recovered
   errors are kept until the end, so a very large input with an error on every line holds one `SyntaxError` per line.
   See [Recovering from Errors](recovery.md).

Use `pego.WithDiagnosticCallback(func(e *pego.SyntaxError) error { ... })` with
`ParseStream` or `Iterate` to receive each committed recovery immediately before
its element. `Next` invokes the diagnostic callback while advancing to that
element. Backtracked recoveries are not reported. The callback receives a copy
that you may keep or modify without changing the retained result. If it returns
an error, parsing stops with that error and the corresponding element is not
delivered. Recoveries in a successful suffix are reported before the terminal
result.

By default, the terminal error retains every recovered diagnostic as `pego.SyntaxErrors`, so an input with an error on
every line holds them all. Add `pego.WithDiagnosticLimit(n)` to retain only the latest `n`. If any were dropped, the
terminal error is a `*pego.TruncatedSyntaxErrors`, which you inspect with `errors.As` (`Errors`, `Total`, `Dropped`).
The callback still receives every committed recovery, and errors that the callback stores itself are not bounded. The
diagnostic options are for streams only; `Parse`, `ParsePrefix` and `Document` reject them. `WithContext` and
`WithWorkLimits` cover the callbacks too, but a callback or reader that is blocked in your code cannot be preempted. The
[specification](../../spec/streaming.md#recovered-errors) defines the counts, their saturation, tracing and the order of
the reports.

A panic from your reader or callback propagates with its original value on every backend,
including values implementing Go's `runtime.Error`. Already delivered elements remain valid;
the parser does not close your reader.

Because what you received is provisional, side effects of the callback deserve some thought. If an element has to be
written to a database only when the whole input is valid, buffer by batches and commit at the end, or write to a staging
table. If a valid prefix is acceptable, the position in the error tells you how much of the input was good: everything
before `SyntaxError.Pos` that was delivered is final.

## Memory

An action may construct temporary nodes and return `nil`; the callback then receives `nil` and the temporary nodes are
released. Variable assignments do not accumulate over the records already consumed, but large variable values, active
rollback points and memo keys keep their own data alive. To measure these costs in your workload, see the
[performance measurement method](../performance.md#how-to-measure) and the
[specification](../../spec/streaming.md#memory).

What a stream parse releases as it goes:

- the **input** before the element just delivered (except one character), by compacting the read buffer once at least
  half of it has been consumed;
- the **memo table** entries before that position, in blocks of about 1,024 positions;
- the **element itself**, as soon as your callback returns, provided you do not keep it.

What it holds: the element being matched (an element is matched in memory, so the largest element bounds the working
set, and one huge element, such as a whole document wrapped in a single rule, defeats streaming), the memo entries of
the last few elements, your callback's own state, and the list of recovered errors.

The effect on a real input, a 27 MiB CSV file with 1,000,000 records (Apple silicon, Go 1.27.1, with the grammar of
[parsers/csv](../../parsers/csv/csv.pego) and a callback that sums a column): `ParseStream` finished in about 1.4 seconds
with a peak resident size of about 13 MB, and `Parse` finished in about 0.8 seconds with about 1.0 GB. Streaming is not faster: it trades a little
time for memory, and the memory is bounded by the working set rather than by the size of the input.

The nodes handed to you are allocated in chunks, and a chunk stays reachable while anything in it is. The parser starts
new chunks at element boundaries from time to time, so the elements you drop do not stay reachable through chunks shared
with later ones. To check the working set of your own grammar, count what is still reachable after a forced garbage
collection. This program does that for two grammars over the same input: one whose elements are CST nodes with
captures, and one whose element rule has an action:

```go
package main

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/ornew/pego"
)

// Two grammars for the same input: the elements are either CST nodes or struct nodes built by an action.
var grammars = map[string]string{
	"cst": `rule main =  item* #stream $$
rule item =  ^ k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" `,
	"action": `type Pair = { Key Key; Value Value }
type Key terminal
type Value terminal
rule main =  item* #stream $$
rule item Pair =  k:key "=" v:value "\n" => Pair{Key: $k, Value: $v}
rule key Key =  [a-z]+ #atomic
rule value Value =  [0-9]+ #atomic `,
}

// repeat is an io.Reader that yields n copies of line without holding them in memory.
type repeat struct {
	line string
	n    int
	buf  string
}

func (r *repeat) Read(b []byte) (int, error) {
	if r.buf == "" {
		if r.n == 0 {
			return 0, io.EOF
		}
		r.buf, r.n = r.line, r.n-1
	}
	n := copy(b, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func main() {
	p, err := pego.CompileSource(grammars[os.Args[1]], "main")
	if err != nil {
		panic(err)
	}
	count := 0
	err = p.ParseStream(&repeat{line: "key=42\n", n: 400000}, func(*pego.Node) error {
		count++
		if count%100000 == 0 {
			var ms runtime.MemStats
			runtime.GC() // measure what is still reachable, not what is waiting to be collected
			runtime.ReadMemStats(&ms)
			fmt.Printf("%s: %7d records, %4d MiB live\n", os.Args[1], count, ms.HeapAlloc>>20)
		}
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}
}
```

```bash
go run ./mem cst
```

```text
cst:  100000 records,    0 MiB live
cst:  200000 records,    0 MiB live
cst:  300000 records,    0 MiB live
cst:  400000 records,    0 MiB live
```

```bash
go run ./mem action
```

```text
action:  100000 records,    0 MiB live
action:  200000 records,    0 MiB live
action:  300000 records,    0 MiB live
action:  400000 records,    0 MiB live
```

Both stay flat: what is reachable is the parser's working set, not the elements already delivered. If you stream
gigabytes, measuring your own grammar with a helper like the one above is still worthwhile; what it should show is a
number that does not grow with the input.

## Cancelling a Stream

Pass `pego.WithContext(ctx)` to interrupt engine work cooperatively, or
`pego.WithWorkLimits(pego.WorkLimits{MaxInputWork: ..., MaxSteps: ...})` to bound
one invocation. A zero limit is unlimited. Work counts depend on the backend,
position unit and implementation; reused or copied storage also consumes work.
Exhaustion matches `pego.ErrWorkLimit` through `errors.Is`.
Text materialization also consumes work, including building a cached stream
snapshot and its CodePoints position table. Reading an already cached substring
requires no additional materialization work; snapshot creation never reads ahead.

Each underlying reader call consumes one step before entering the reader,
including empty reads, EOF and retries for incomplete characters. When no steps
remain, that next call is prevented. Bytes consumed by read-ahead do not count
as examined input until the engine examines them.

The engine checks immediately before and after each underlying reader and emit
call, including reader retries for empty reads or incomplete characters. An emit
error or a reader's non-EOF error wins over cancellation first observed after
that same call, including a reader returning data together with an error.
Elements already passed to emit remain caller-owned, including when cancellation
is observed after emit succeeds. Restarting requires replaying the input yourself;
the current reader offset is not a continuation token. Buffered read-ahead means
an input-work limit does not cap bytes consumed from the reader.

First-use backend preparation is synchronous and outside work limits. A context
cancelled before a valid call prevents that preparation; cancellation during it
is observed after preparation finishes. Compilation through `CompileSource`
remains separate from the parse invocation.

The parser still blocks inside `Read` while it waits for input. A context cannot
make an arbitrary reader or callback return. To give up on a quiet connection,
make `Read` fail from outside: close the connection, set a read deadline, or close
the write end of the pipe with an error. The parser never closes your reader or
starts a goroutine for you. Stopping a stream that waits for a quiet reader needs both: cancel the context, and make the reader fail.

## See Also

- [Incremental parsing](incremental.md): reparsing a text after edits.
- [spec/pragmas.md](../../spec/pragmas.md#stream): the `#stream` behavioral pragma.
- [Error Messages](error-messages.md): `@error` and the shape of `SyntaxError`.
- [Recovering from Errors](recovery.md): `#recover` and recovered errors.
- [Runtime](running-parsers.md): backends, position units and the other parse options.
- [Design record 007](../design/007-streaming-and-incremental-parsing.md).
