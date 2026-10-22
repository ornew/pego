# Parse Log Lines Fast

**Problem.** A service writes one line per request, and you want to read a few fields of millions of lines, or only
check that every line is well formed. You want it fast, and you want typed values, not a tree to search.

Describe the line, generate a Go parser from the grammar, and call `ParseAST` (typed values) or `Recognize` (no values).
A line looks like this:

```text
2026-10-09T11:12:41Z INFO api GET /users/42 200 12ms
```

```pego
// logs.pego
type Time = node[string]
type Level = node[string]
type Service = node[string]
type Method = node[string]
type Path = node[string]
type Status = node[string]
type Millis = node[string]

type Line = {
    Time    Time
    Level   Level
    Service Service
    Method  Method
    Path    Path
    Status  Status
    Millis  Millis
}
type Log = { Lines []Line }

@start
rule main Log = ls:line* $$ => Log{Lines: $ls.Value}

rule line Line = (
    t:time " " l:severity " " s:service " " m:method " " p:path " " st:status " " ms:millis "ms\n"
) => Line{Time: $t, Level: $l, Service: $s, Method: $m, Path: $p, Status: $st, Millis: $ms}

rule time Time = ([0-9] #repeat(4) "-" [0-9] #repeat(2) "-" [0-9] #repeat(2) "T" [0-9] #repeat(2) ":" [0-9] #repeat(2) ":" [0-9] #repeat(2) "Z") #atomic
rule severity Level = ("DEBUG" / "INFO" / "WARN" / "ERROR") #atomic
rule service Service = ([a-z\-]+) #atomic
rule method Method = ("GET" / "POST" / "PUT" / "DELETE") #atomic
rule path Path = ([^ \n]+) #atomic
rule status Status = ([0-9] #repeat(3)) #atomic
rule millis Millis = ([0-9]+) #atomic
```

Generate the parser with `-types` (Go types for the grammar's types and `ParseAST`) and `-recognize` (`Recognize`).
The tool is recorded in the module with `go get -tool github.com/ornew/pego/cmd/pego`, and `pego gen` creates the output
directory:

```bash
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

//go:generate go tool pego gen -g logs.pego -module example.com/logs/logparser -types -recognize -o logparser

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
		count[l.Level.Value]++
		ms, _ := strconv.Atoi(l.Millis.Value) // the grammar guarantees digits
		if ms > slowestMs {
			slowest, slowestMs = l, ms
		}
	}
	for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		fmt.Printf("%-5s %d\n", level, count[level])
	}
	if slowest != nil {
		fmt.Printf("slowest: %s %s %s (%sms)\n", slowest.Method.Value, slowest.Path.Value, slowest.Status.Value, slowest.Millis.Value)
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
2:40: syntax error: expected [0-9]
```

## How It Works

- **A generated parser is the fast one.** `pego gen` writes a Go package with no dependency on PEGO. With `-types`, every
  rule here has a Go type of its own, so `ParseAST` builds `*Line` values directly, without the generic nodes `Parse`
  would build first. The values are plain structs: `l.Level.Value`, `l.Millis.Value`.
- **Strings stay strings.** `Millis` is a string node, so its `Value` is the matched digits, and you convert
  (`strconv.Atoi`) only the fields you use.
- **`Recognize` only checks.** If all you need is "is this file well formed", `logparser.Recognize(text)` returns the
  same error as `ParseAST` without building anything.

## How Fast

A benchmark in the same directory parses 100,000 lines (5.7 MB) of the same shape, once per iteration, with each entry
point in turn (`b.Loop()`, `b.SetBytes(len(input))`): `p.Parse` and `p.Parse(input, pego.RecognizeOnly())` of the engine
(`pego.CompileSource`), and `Parse`, `ParseAST` and `Recognize` of the generated parser. This is the output of
`go test -bench . -benchmem` on an Apple M3 Max with Go 1.27.1, rounded, with the columns that are not used here left
out:

```text
BenchmarkEngineParse          100 ms    57 MB/s   205.0 MB/op  709789 allocs/op
BenchmarkEngineRecognizeOnly   52 ms   110 MB/s    22.9 MB/op      40 allocs/op
BenchmarkGeneratedParse        48 ms   118 MB/s   195.6 MB/op  708688 allocs/op
BenchmarkGeneratedParseAST     24 ms   240 MB/s    81.7 MB/op    3192 allocs/op
BenchmarkGeneratedRecognize    14 ms   414 MB/s    45.7 MB/op       4 allocs/op
```

Generating the parser halves the time, and typed values and recognition halve it again. Numbers differ between machines
and inputs; measure your own grammar and input (see [Debugging and Profiling](../guide/debugging.md) for profiling a parse).

## Variations and Limits

- **Several formats.** Put each in its own grammar and package, or use `ParseRule` of the generated parser to start at
  another rule.
- **Lines you do not want to fail on.** Parse each line separately (a grammar whose `main` is one `line`), or add
  [error recovery](several-errors.md) so that a bad line becomes an `error` value and parsing goes on.
- **More input than memory.** The generated parser needs the whole input in memory; use the engine and
  [`#stream`](big-files.md).
- **Without a generated file.** `p.Parse(text, pego.RecognizeOnly())` is the engine's recognition mode, and the typed
  structs can be replaced by the `Field` and `Elems` of `*pego.Node`.

See [Code generation](../guide/code-generation.md) for what generated parsers support and
[Running parsers](../guide/running-parsers.md) for recognition mode.
