# Parse log lines fast

**Problem.** A service writes one line per request, and you want to read a few fields of millions of lines, or only
check that every line is well formed. You want it fast, and you want typed values, not a tree to search.

Describe the line, generate a Go parser from the grammar, and call `ParseAST` (typed values) or `Recognize` (no values).
A line looks like this:

```text
2026-10-09T11:12:41Z INFO api GET /users/42 200 12ms
```

```pego
// logs.pego
type Time terminal
type Level terminal
type Service terminal
type Method terminal
type Path terminal
type Status terminal
type Millis terminal

type Line struct {
    Time    Time
    Level   Level
    Service Service
    Method  Method
    Path    Path
    Status  Status
    Millis  Millis
}
type Log struct { Lines []Line }

def main: Log = ls:line* $$ -> new Log{Lines: $ls}

def line: Line = t:time " " l:severity " " s:service " " m:method " " p:path " " st:status " " ms:millis "ms\n"
    -> new Line{Time: $t, Level: $l, Service: $s, Method: $m, Path: $p, Status: $st, Millis: $ms}

def time: Time = (?0-9){4} "-" (?0-9){2} "-" (?0-9){2} "T" (?0-9){2} ":" (?0-9){2} ":" (?0-9){2} "Z"
def severity: Level = "DEBUG" / "INFO" / "WARN" / "ERROR"
def service: Service = (?a-z\-)+
def method: Method = "GET" / "POST" / "PUT" / "DELETE"
def path: Path = (?^ \n)+
def status: Status = (?0-9){3}
def millis: Millis = (?0-9)+
```

Generate the parser with `-types` (Go types for the grammar's types and `ParseAST`) and `-recognize` (`Recognize`).
The tool is recorded in the module with `go get -tool github.com/ornew/pego/cmd/pego`, and the output directory must
exist:

```bash
mkdir logparser
go generate ./...
```

```go
// main.go
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"example.com/logs/logparser"
)

//go:generate go tool pego gen -g logs.pego -pkg logparser -types -recognize -o logparser/parser.go

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log, err := logparser.ParseAST(string(data))
	if err != nil {
		fmt.Fprintln(os.Stderr, err) // for example 3:12: syntax error: expected ...
		os.Exit(1)
	}

	count := map[string]int{}
	var slowest *logparser.Line
	slowestMs := -1
	for _, l := range log.Lines {
		count[l.Level.Text]++
		ms, _ := strconv.Atoi(l.Millis.Text) // the grammar guarantees digits
		if ms > slowestMs {
			slowest, slowestMs = l, ms
		}
	}
	for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		fmt.Printf("%-5s %d\n", level, count[level])
	}
	if slowest != nil {
		fmt.Printf("slowest: %s %s %s (%sms)\n", slowest.Method.Text, slowest.Path.Text, slowest.Status.Text, slowest.Millis.Text)
	}
}
```

With this `sample.log`:

```text
2026-10-09T11:12:41Z INFO api GET /users/42 200 12ms
2026-10-09T11:12:41Z INFO api POST /users 201 48ms
2026-10-09T11:12:42Z WARN billing GET /invoices?page=2 200 930ms
2026-10-09T11:12:43Z ERROR api GET /users/7 500 4ms
2026-10-09T11:12:43Z DEBUG cache DELETE /sessions/abc 204 1ms
```

```bash
go run . < sample.log
```

```text
DEBUG 1
INFO  2
WARN  1
ERROR 1
slowest: GET /invoices?page=2 200 (930ms)
```

A line that does not match is reported with its position, and nothing is counted:

```bash
printf '2026-10-09T11:12:41Z INFO api GET /x 200 12ms\n2026-10-09T11:12:41Z INFO api GET /x 20 12ms\n' | go run .
```

```text
2:40: syntax error: expected (?0-9)
```

## How it works

- **A generated parser is the fast one.** `pego gen` writes a Go file with no dependency on PEGO. With `-types`, every
  rule here has a Go type of its own, so `ParseAST` builds `*Line` values directly, without the generic nodes `Parse`
  would build first. The values are plain structs: `l.Level.Text`, `l.Millis.Text`.
- **Terminals stay text.** `Millis` is a terminal type, so its value is the matched digits, and you convert
  (`strconv.Atoi`) only the fields you use.
- **`Recognize` only checks.** If all you need is "is this file well formed", `logparser.Recognize(text)` returns the
  same error as `ParseAST` without building anything.

## How fast

A benchmark in the same directory parses 100,000 lines (5.7 MB) of the same shape, once per iteration, with each entry
point in turn (`b.Loop()`, `b.SetBytes(len(input))`): `p.Parse` and `p.Parse(input, pego.RecognizeOnly())` of the engine
(`pego.CompileSource`), and `Parse`, `ParseAST` and `Recognize` of the generated parser. This is the output of
`go test -bench . -benchmem` on an Apple M3 Max with Go 1.27.1, rounded, with the columns that are not used here left
out:

```text
BenchmarkEngineParse           72 ms    79 MB/s   158.7 MB/op   7007 allocs/op
BenchmarkEngineRecognizeOnly   45 ms   127 MB/s    22.7 MB/op     18 allocs/op
BenchmarkGeneratedParse        38 ms   150 MB/s   158.6 MB/op   6987 allocs/op
BenchmarkGeneratedParseAST     20 ms   285 MB/s    81.4 MB/op   3136 allocs/op
BenchmarkGeneratedRecognize    20 ms   285 MB/s    45.4 MB/op      4 allocs/op
```

Generating the parser halves the time, and typed values and recognition halve it again. Numbers differ between machines
and inputs; measure your own grammar and input (see [performance tuning](../performance.md) for how the repository does
it).

## Variations

- **Several formats.** Put each in its own grammar and package, or use `ParseRule` of the generated parser to start at
  another rule.
- **Lines you do not want to fail on.** Parse each line separately (a grammar whose `main` is one `line`), or add
  [error recovery](several-errors.md) so that a bad line becomes an `Error` node and parsing goes on.
- **More input than memory.** The generated parser needs the whole input in memory; use the engine and
  [`#stream`](big-files.md).
- **Without a generated file.** `p.Parse(text, pego.RecognizeOnly())` is the engine's recognition mode, and the typed
  structs can be replaced by the `Field` and `Children` of `*pego.Node`.

See [Code generation](../guide/code-generation.md) for what generated parsers support and
[Running parsers](../guide/runtime.md) for recognition mode.
