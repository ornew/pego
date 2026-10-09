# Process a file that does not fit in memory

**Problem.** You read a log or an export with millions of records and want to total, filter or load them, in a
constant amount of memory, whatever the size of the file.

Mark the repetition of records with `#stream` and call `ParseStream` with a function that receives each record as soon
as it has been parsed. The input before the record is dropped after it.

```pego
// stream.pego
type Level terminal
type Millis terminal
type Entry struct { Level Level, Millis Millis }

// Each entry is one line: time level service method path status millis.
def main = entry* #stream $$

def entry: Entry = -token " " l:severity " " -token " " -token " " -token " " -token " " ms:millis "ms\n"
    -> new Entry{Level: $l, Millis: $ms}
def token = (?^ \n)+
def severity: Level = "DEBUG" / "INFO" / "WARN" / "ERROR"
def millis: Millis = (?0-9)+
```

The lines are those of [Parse log lines fast](log-lines.md). This grammar keeps only the level and the time, and skips the
other fields with `-token`. The program counts the errors and adds up the times. To have a big file without a big file
on disk, `logFile` is an `io.Reader` that makes the lines as they are read; with a real file, pass the `*os.File`
(or a `bufio.Reader`, or `os.Stdin`) to `ParseStream` instead. With `-whole` the program reads everything and calls
`Parse`, for comparison.

```go
// main.go
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log"
	"runtime"
	"strconv"

	"github.com/ornew/pego"
)

//go:embed stream.pego
var grammar string

// logFile is an io.Reader that makes n log lines as they are read, so the input never exists in memory.
type logFile struct {
	n, i int
	buf  string
}

func (f *logFile) Read(b []byte) (int, error) {
	n := 0
	for n < len(b) {
		if f.buf == "" {
			if f.i == f.n {
				break
			}
			level := [...]string{"DEBUG", "INFO", "INFO", "INFO", "WARN", "ERROR"}[f.i%6]
			f.buf = fmt.Sprintf("2026-10-09T11:12:41Z %s api GET /users/%d 200 %dms\n", level, f.i, f.i%1000)
			f.i++
		}
		c := copy(b[n:], f.buf)
		n += c
		f.buf = f.buf[c:]
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func main() {
	n := flag.Int("n", 1_000_000, "number of lines")
	whole := flag.Bool("whole", false, "read everything and call Parse instead of ParseStream")
	flag.Parse()

	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}

	var lines, errors, totalMs int
	count := func(e *pego.Node) error {
		lines++
		if e.Field("Level").(*pego.Node).Text == "ERROR" {
			errors++
		}
		ms, _ := strconv.Atoi(e.Field("Millis").(*pego.Node).Text)
		totalMs += ms
		return nil
	}

	if *whole {
		data, _ := io.ReadAll(&logFile{n: *n})
		tree, err := p.Parse(string(data))
		if err != nil {
			log.Fatal(err)
		}
		// Parse collects the elements in a list.
		for _, e := range tree.Children[0].Children {
			count(e)
		}
	} else if err := p.ParseStream(&logFile{n: *n}, count); err != nil {
		log.Fatal(err)
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("%d lines, %d errors, %d ms in total\n", lines, errors, totalMs)
	fmt.Printf("memory obtained from the system: %d MiB\n", m.Sys>>20)
}
```

One million lines are 58 MB. On an Apple M3 Max with Go 1.27.1, including the time to make the lines:

```bash
go run . -n 1000000
```

```text
1000000 lines, 166666 errors, 499500000 ms in total
memory obtained from the system: 18 MiB
```

```bash
go run . -n 1000000 -whole
```

```text
1000000 lines, 166666 errors, 499500000 ms in total
memory obtained from the system: 941 MiB
```

With five million lines (290 MB) the streaming program still needs 17 MiB. For one million lines it took 1.2 seconds and
the whole-input program 0.8 seconds: streaming trades some time for memory that does not grow with the input. (The
memory figures vary by a few MiB from run to run.)

## How it works

- **`#stream` is a promise.** It says that each iteration of this repetition is final once it has matched: the parser
  never backtracks into it. That is what lets it deliver a record, forget it, and drop the input before it.
- **`ParseStream` returns an error, not a tree.** The records are only seen by your function. What comes before and
  after the repetition is parsed and checked, but not returned; if there is a header, make it an element of the
  repetition and tell the kinds apart by `Type()`.
- **Errors stop the stream.** At the first line that does not match, `ParseStream` returns a `*pego.SyntaxError` with the
  line and column in the whole input, after the records before it were delivered. Return an error from your function to
  stop early; `ParseStream` returns it unchanged.
- **The trailing `$$`** turns input that is not a sequence of entries into an error. Without it, the stream would end
  quietly at the first line that does not match.
- **Positions and memory.** `Start` and `End` of a record count from the start of the whole input. The parser keeps the
  record it is matching and a few more, so one huge record defeats streaming: keep records small.

## Variations

- **Cancel on a quiet connection.** `ParseStream` blocks in `Read`. To stop it from outside, make `Read` fail (close the
  connection or the write end of a pipe); see the streaming section of the [guides](../guide/README.md).
- **Skip bad records.** Add [`#recover`](several-errors.md) inside the record rule. The bad record becomes an `Error`
  node that your function receives, and `ParseStream` returns the recorded errors at the end.
- **Do not hold on to nodes.** The record is reachable only through your function; if you keep it (in a slice, say),
  memory grows again.
- **CSV.** [parsers/csv](../../parsers/csv/) has a grammar with a `#stream` repetition of records.

See [spec/attributes.md](../../spec/attributes.md) for the rules of `#stream`.
