# Streaming

`Parser.Parse` takes a whole string and returns a whole tree. When the input is too big to hold, or arrives as a
stream you want to process as it comes, `Parser.ParseStream` reads an `io.Reader` and hands your function each element
of a repetition as soon as it matches, then forgets it. Memory use depends on the size of one element, not on the size
of the input.

|  |  |
|:--|:--|
| Input | An `io.Reader`, possibly unbounded |
| API | `Parser.ParseStream`, and `pego parse -stream` on the command line |
| Grammar | The start rule has a `#stream` repetition |
| You get | Each element of that repetition, as soon as it matches |
| Saves | Memory: neither the input nor the tree ever exists whole |

`#stream` has no effect on `Parse`, and `ParseStream` works with every backend ([Runtime](runtime.md)) and with both
position units. It does not work with `RecognizeOnly` (`ParseStream` returns an error) or with parsers produced by
`GenerateGo`. If the problem is the opposite one, a text that stays in memory and is edited again and again, see
[Incremental parsing](incremental.md): a `Document` is always parsed as a whole, so the two do not combine.

The design is in [design record 007](../design/007-streaming-and-incremental-parsing.md); the attribute is specified in
[spec/attributes.md](../../spec/attributes.md#stream).

- [A first example](#a-first-example)
- [What is emitted, and when](#what-is-emitted-and-when)
- [Rules and limits](#rules-and-limits)
- [Positions and text](#positions-and-text)
- [Errors](#errors)
- [Memory](#memory)
- [Recipe: summing a CSV column](#recipe-summing-a-csv-column)
- [Recipe: a header followed by records](#recipe-a-header-followed-by-records)
- [Recipe: cancelling a stream](#recipe-cancelling-a-stream)
- [See also](#see-also)

## A first example

`#stream` marks a repetition at the top level of the start rule. Everything the repetition matches is handed to your
function one element at a time, and forgotten.

```pego
package kv

type Pair struct { Key Key, Value Value }
type Key terminal
type Value terminal

def main = pair* #stream $$

def pair: Pair = k:key "=" v:value "\n" -> new Pair{Key: $k, Value: $v}
def key: Key = @(?a-z_)+
def value: Value = @(?0-9)+
```

From the command line, `pego parse -stream` prints one line per element (JSON by default, `-f sexpr` for
S-expressions). Elements that matched before an error are printed, then the error is reported:

```bash
$ printf 'a=1\nbb=22\nccc=x\n' | pego parse -g kv.pego -stream
{"type":"Pair","start":0,"end":4,"fields":{"Key":{"type":"Key","rule":"key","start":0,"end":1,"text":"a"},"Value":{"type":"Value","rule":"value","start":2,"end":3,"text":"1"}}}
{"type":"Pair","start":4,"end":10,"fields":{"Key":{"type":"Key","rule":"key","start":4,"end":6,"text":"bb"},"Value":{"type":"Value","rule":"value","start":7,"end":9,"text":"22"}}}
pego: 3:5: syntax error: expected (?0-9)
```

From Go (in a module that requires `github.com/ornew/pego`):

```go
package main

import (
	"fmt"
	"os"

	"github.com/ornew/pego"
)

const grammar = `
package kv

type Pair struct { Key Key, Value Value }
type Key terminal
type Value terminal

def main = pair* #stream $$

def pair: Pair = k:key "=" v:value "\n" -> new Pair{Key: $k, Value: $v}
def key: Key = @(?a-z_)+
def value: Value = @(?0-9)+
`

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
$ printf 'a=1\nbb=22\nccc=x\n' | go run .
[0,4) a = 1
[4,10) bb = 22
error: 3:5: syntax error: expected (?0-9)
```

Notice that the third line is reported as an error at `3:5`, even though the parser had thrown away the first two lines
by then: line and column numbers, like node positions, refer to the whole input.

## What is emitted, and when

- **One element per iteration.** The element is the value one iteration of the repetition contributes to the `List` of
  an ordinary parse: a struct node if the element is a rule with an action, a CST node (`Seq`, `Match`, ...) otherwise.
  If the element has no value (for example `(-"a")* #stream`) your function receives a `nil` node, so check for `nil`
  if your elements can be value-free.
- **As soon as it matches.** The callback runs in the middle of the parse, before the parser tries the next element.
  A slow callback slows the parse, and it also stops reading: nothing is read from the `io.Reader` while your function
  runs.
- **Only the elements.** `ParseStream` returns just an `error`: there is no tree, and the elements are not collected
  into the repetition's `List`. The values of whatever comes before or after the repetition (a header, a footer) are
  parsed and checked, but thrown away. To see a header, make it an element; see
  [the header recipe](#recipe-a-header-followed-by-records).
- **An element is final.** Once the callback has seen an element, the parser never goes back before it. This is why the
  grammar must say where the cut points are with `#stream`: PEG backtracks, and without such a promise no part of the
  result is final until the end of the input. The corollary is that **what you receive is provisional until
  `ParseStream` returns nil**: the input can still turn out to be invalid after the last element (see
  [Errors](#errors)).

Input is read only as far as it is needed. Elements that end in a fixed literal are delivered the moment their last
character arrives; an element that ends in a repetition (`+`, `*`, `?`) needs to see one more character first, because
that is how the parser knows the repetition has ended. This matters when input trickles in, as over a network.
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
	run(`
def main = line* #stream $$
def line = @(?a-z)+ "\n"`, "ab\nc", "d\n", "ef\n")

	fmt.Println("elements that end with a repetition:")
	run(`
def main = word* #stream $$
def word = @(?a-z)+ " "?`, "ab ", "cd", " ef")
}
```

```
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

## Rules and limits

| | |
|:--|:--|
| Where `#stream` can appear | On a repetition (`*`, `+` or `{n,m}`) that is the body of a rule, an item of the body's top-level sequence, or the expression captured by such an item (`items:record* #stream`). Anywhere else (inside a choice or a group, or on something that is not a repetition) is a compile error, as is a second `#stream` in the same rule or any argument to it. |
| Which rule streams | Only the **start rule** of the parse, at call depth 1. `ParseStream` fails with `rule main has no #stream repetition` if the start rule has none, even when a rule it calls does. Use `Parser.WithStart` to stream from another rule. In every other call of a rule that contains `#stream`, the repetition is an ordinary one. |
| What may follow | Anything. After the repetition ends, the rest of the start rule must match as usual, normally `$$`. |
| Finite bounds | `{n,m}` delivers at most `m` elements and fails if fewer than `n` match. `{0}` executes no elements. Input beyond the maximum is left for the suffix: `"a"{1,2} #stream "a" $$` accepts `aaa` and delivers two elements. A later suffix or minimum failure does not retract elements already delivered. |
| Elements that can match nothing | If the maximum permits an element, an element that matches the empty string is delivered once and then ends the repetition. Without a guard like the `!$$` in [parsers/csv](../../parsers/csv/csv.pego), a record rule whose fields and line end can all match nothing delivers one extra empty record at the end of input. |
| Options | `WithUnit` and `WithBackend` work; `WithMaxDepth` applies; `RecognizeOnly` is an error. |
| Lookbehind | After an element, the input before it is dropped except for the one character before it, so `^` (beginning of line) still works at the start of the next element. Nothing in the language looks further back. |
| Concurrency | A `Parser` can run many `ParseStream` calls at once. Each call has its own state. |

The compile errors are, in order: `#stream must be attached to a repetition (*, +, or {n,m})`, `#stream is only allowed
at the top level of a rule body`, `a rule body can have only one #stream` and `#stream takes no arguments`.

## Positions and text

`Node.Start` and `Node.End`, `startPos` and `endPos` in actions, `SyntaxError.Pos`, `Line` and `Col` all count from the
start of the whole input, whichever part has been dropped (the parser keeps a running line and column for the dropped
part). `WithUnit(pego.Bytes)` switches all of them to bytes: the `!` in `éé!` is at column 3 counted in code points and at
column 5 counted in bytes.

The parser does not keep text for you. Terminal nodes (`Match`, terminal types, `@(...)`) carry their text in `Node.Text`;
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
	lines := compile(`
def main = line* #stream $$
def line = @(?a-z0-9)+ "\n"`)
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
	entries := compile(`
def main = entry* #stream $$
def entry = (name "=" num "\n") #recover(skip=(?^\n)* "\n")
def name = @(?a-z)+
def num = @(?0-9)+`)
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

```
syntax error:
  element [0,3)
  element [3,6)
  3:1: syntax error: expected (?a-z0-9), end of input (line 3, column 1, position 6)
callback error:
  err == errEnough: true, callbacks: 2
reader error:
  element [0,3)
  element [3,6)
  errors.Is(err, errReset): true
recovered errors:
  element (Seq "a"@name "=" "1"@num "\n")@entry
  element Error"b=x\n"{message=`2:3: syntax error: expected (?0-9)`}@entry
  element (Seq "c"@name "=" "3"@num "\n")@entry
  element Error"d\n"{message=`4:2: syntax error: expected "=", (?a-z)`}@entry
  recovered: 2:3: syntax error: expected (?0-9)
  recovered: 4:2: syntax error: expected "=", (?a-z)
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
4. **Recovered errors.** With [`#recover`](../../spec/attributes.md#recover) inside the element, a bad element becomes
   an `Error` node (delivered like any other element) and parsing continues. At the end, `ParseStream` returns the
   recorded errors as `pego.SyntaxErrors`, even though every element was delivered. Treat that as "success with
   diagnostics" and use `errors.As(err, &recovered)` to tell it from a fatal `*SyntaxError`. All recovered errors are kept until the end, so a
   very large input with an error on every line holds one `SyntaxError` per line.
   See [Errors and recovery](errors-and-recovery.md).

Because what you received is provisional, side effects of the callback deserve some thought. If an element has to be
written to a database only when the whole input is valid, buffer by batches and commit at the end, or write to a staging
table. If a valid prefix is acceptable, the position in the error tells you how much of the input was good: everything
before `SyntaxError.Pos` that was delivered is final.

## Memory

An action may construct temporary nodes and return `nil`. The callback receives
that nil value, and the action's construction tracker releases its references
before delivery. Clearing that tracker preserves nodes the action returns;
it does not change their fields or positions. Variables hold scalar values.

Repeated variable assignments keep one current binding per name. Environments
saved for backtracking or a caller remain immutable, but the current environment
does not retain all previous assignments from consumed records. Lookup cost
depends on distinct variable names. Large variable values, active rollback
points and memo keys can still keep their own data alive; see
[performance log entry 69](../performance.md#69-bound-persistent-variable-binding-histories).

What a stream parse releases as it goes:

- the **input** before the element just delivered (except one character), by compacting the read buffer once at least
  half of it has been consumed;
- the **memo table** entries before that position, in blocks of about 1,024 positions;
- the **element itself**, as soon as your callback returns, provided you do not keep it.

What it holds: the element being matched (an element is matched in memory, so the largest element bounds the working
set, and one huge element, such as a whole document wrapped in a single rule, defeats streaming), the memo entries of
the last few elements, your callback's own state, and the list of recovered errors.

The effect on a real input, a 27 MiB CSV file with 1,000,000 records (Apple silicon, Go 1.27.1; the program is in the
[recipe](#recipe-summing-a-csv-column)): `ParseStream` finished in about 1.2 to 2.0 seconds with a peak resident size of
about 58 MB, and `Parse` finished in about 1.0 second with about 1.9 GB. Streaming is not faster: it trades a little
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
	"cst": `
def main = item* #stream $$
def item = ^ k:@(?a-z)+ "=" v:@(?0-9)+ "\n"`,
	"action": `
type Pair struct { Key Key, Value Value }
type Key terminal
type Value terminal
def main = item* #stream $$
def item: Pair = k:key "=" v:value "\n" -> new Pair{Key: $k, Value: $v}
def key: Key = @(?a-z)+
def value: Value = @(?0-9)+`,
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

```
$ go run ./mem cst
cst:  100000 records,    0 MiB live
cst:  200000 records,    0 MiB live
cst:  300000 records,    0 MiB live
cst:  400000 records,    0 MiB live
$ go run ./mem action
action:  100000 records,    0 MiB live
action:  200000 records,    0 MiB live
action:  300000 records,    0 MiB live
action:  400000 records,    0 MiB live
```

Both stay flat: what is reachable is the parser's working set, not the elements already delivered. If you stream
gigabytes, measuring your own grammar with a helper like the one above is still worthwhile; what it should show is a
number that does not grow with the input.

## Recipe: summing a CSV column

The CSV grammar of [parsers/csv](../../parsers/csv/csv.pego) streams record by record. This program, with a copy of
`csv.pego` next to it, sums the third column and reports the first bad record with its offset; with `-whole` it uses
`Parse` instead, to compare.

```go
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/ornew/pego"
)

//go:embed csv.pego
var grammar string

func main() {
	whole := flag.Bool("whole", false, "read all the input and call Parse instead of ParseStream")
	flag.Parse()

	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var rows int
	var total float64
	// sum adds up the third column of a record. The first record is the header.
	sum := func(rec *pego.Node) error {
		rows++
		if rows == 1 {
			return nil
		}
		fields := rec.Field("Fields").(*pego.Node).Children
		if len(fields) != 3 {
			return fmt.Errorf("record %d (offset %d): want 3 fields, got %d", rows, rec.Start, len(fields))
		}
		v, err := strconv.ParseFloat(fields[2].Text, 64)
		if err != nil {
			return fmt.Errorf("record %d (offset %d): %w", rows, rec.Start, err)
		}
		total += v
		return nil
	}

	if *whole {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// main returns a File whose Records field holds all the records.
		tree, err := p.Parse(string(data))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, rec := range tree.Field("Records").(*pego.Node).Children {
			if err := sum(rec); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	} else if err := p.ParseStream(os.Stdin, sum); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d records, total %.2f\n", rows-1, total)
}
```

Test data, a header and then records such as `7,"item, no. 7",7.25`, and a run (on macOS, `/usr/bin/time -l` reports the
peak memory as "maximum resident set size"):

```bash
$ awk 'BEGIN { print "id,name,amount"; for (i = 1; i <= 1000000; i++) printf "%d,\"item, no. %d\",%d.25\n", i, i % 100, i % 1000 }' > big.csv
$ /usr/bin/time -l ./csvsum < big.csv
1000000 records, total 499750000.00
$ ./csvsum -whole < big.csv
1000000 records, total 499750000.00
```

Errors carry enough to locate the bad input, both from the callback and from the parser:

```bash
$ printf 'id,name,amount\n1,a,2.5\n2,b,oops\n3,c,1\n' | ./csvsum
record 3 (offset 23): strconv.ParseFloat: parsing "oops": invalid syntax
$ printf 'id,name,amount\n1,a,2.5\n2,"b\n' | ./csvsum
4:1: syntax error: expected "\"", "\"\"", (?^")
```

(The record number counts the header. The callback's error is wrapped with the record's `Start` offset; the parser's
error says the quoted field never ended.) Line-oriented formats work the same way; the parts that matter are a record
rule that cannot match the empty string at the end of input (`!$$`), an `eol` that also accepts `$$`, and `$$` after the
repetition.

## Recipe: a header followed by records

`ParseStream` does not return the header. Make it an element and tell the kinds apart by node type:

```go
package main

import (
	"fmt"
	"strings"

	"github.com/ornew/pego"
)

const grammar = `
package kv

type Header struct { Version Version }
type Version terminal
type Pair struct { Key Key, Value Value }
type Key terminal
type Value terminal

def main = ^^ item* #stream $$
def item = header / pair
def header: Header = "#version " v:version "\n" -> new Header{Version: $v}
def version: Version = @(?0-9)+
def pair: Pair = k:key "=" v:value "\n" -> new Pair{Key: $k, Value: $v}
def key: Key = @(?a-z_)+
def value: Value = @(?0-9)+
`

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		panic(err)
	}
	err = p.ParseStream(strings.NewReader("#version 2\na=1\nb=2\n"), func(n *pego.Node) error {
		switch n.Type() {
		case "Header":
			fmt.Println("header, version", n.Field("Version").(*pego.Node).Text)
		case "Pair":
			fmt.Println("pair", n.Field("Key").(*pego.Node).Text, "=", n.Field("Value").(*pego.Node).Text)
		}
		return nil
	})
	fmt.Println("err:", err)
}
```

```
header, version 2
pair a = 1
pair b = 2
err: <nil>
```

Here `item = header / pair` is the repeated element, so a stream whose header appears later, or twice, is accepted by the
grammar; if that matters, check it in the callback (or give the first element its own rule outside the repetition and
accept that its value is not delivered).

## Recipe: cancelling a stream

The parser blocks inside `Read` while it waits for input, and returning an error from the callback only helps when an
element completes. To give up on a quiet connection, make `Read` fail from outside: close the connection, set a read
deadline, or close the write end of the pipe with an error. `ParseStream` returns whatever `Read` returned, so the cause
survives:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ornew/pego"
)

const grammar = `
def main = line* #stream $$
def line = @(?a-z0-9)+ "\n"
`

// parseContext streams r into emit and gives up when ctx is cancelled. The parser blocks inside r.Read, so
// cancelling needs the reader to fail: cancelRead must make a blocked Read return the error (closing the
// connection, or the write end of a pipe, does).
func parseContext(ctx context.Context, p *pego.Parser, r io.Reader, cancelRead func(error), emit func(*pego.Node) error) error {
	stop := context.AfterFunc(ctx, func() { cancelRead(ctx.Err()) })
	defer stop()
	return p.ParseStream(r, func(n *pego.Node) error {
		if err := ctx.Err(); err != nil { // also stop between elements
			return err
		}
		return emit(n)
	})
}

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	pr, pw := io.Pipe()
	go func() {
		io.WriteString(pw, "one\ntwo\n")
		// ... and then the producer goes quiet.
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err = parseContext(ctx, p, pr, func(err error) { pw.CloseWithError(err) }, func(n *pego.Node) error {
		fmt.Println("line", n.Children[0].Text)
		return nil
	})
	fmt.Println("err:", err, errors.Is(err, context.DeadlineExceeded))
}
```

```
line one
line two
err: context deadline exceeded true
```

## See also

- [Incremental parsing](incremental.md): reparsing a text after edits.
- [spec/attributes.md](../../spec/attributes.md#stream): the `#stream` attribute.
- [Errors and recovery](errors-and-recovery.md): `#error`, `#recover` and the shape of `SyntaxError`.
- [Runtime](runtime.md): backends, position units and the other parse options.
- [Design record 007](../design/007-streaming-and-incremental-parsing.md).
