# Embed a Grammar in a Program and Share the Parser

**Problem.** A program needs to parse a few small formats (a duration, a size) in many places and from many goroutines.
You want the grammar in its own file, compiled once, with a test that fails if the grammar is broken, and one parser
that every part of the program shares.

Keep the grammar in a `.pego` file and embed it with `go:embed`, so that `pego fmt`, `pego lint` and the editor support
work on a real file. Compile it once, at package initialization, and give each format its own start rule. A `*pego.Parser`
is safe for concurrent use, and `WithStart` returns a parser for another start rule that shares the compiled grammar.

```pego
// units.pego
package units

type Number = node[string]
type Unit = node[string]
type Quantity = { Value Number; Unit Unit }

rule duration []Quantity = qs:quantity+ $$ => $qs.Value
rule size Quantity = q:quantity $$ => $q

rule quantity Quantity = v:number u:unit => Quantity{Value: $v, Unit: $u}
rule number Number = ([0-9]+) #atomic
rule unit Unit = ("ms" / "s" / "m" / "h" / "KiB" / "MiB" / "GiB") #atomic
```

```go
// units.go
package main

import (
	_ "embed"

	"github.com/ornew/pego"
)

//go:embed units.pego
var grammar string

// Compiling is the expensive step, and it happens once. WithStart returns a parser for another start
// rule that shares the compiled grammar.
var (
	durationParser = mustCompile("duration")
	sizeParser     = must(durationParser.WithStart("size"))
)

func mustCompile(start string) *pego.Parser {
	return must(pego.CompileSource(grammar, start))
}

func must(p *pego.Parser, err error) *pego.Parser {
	if err != nil {
		panic(err) // a grammar error is a bug in the program, found by the test below
	}
	return p
}
```

```go
// units_test.go
package main

import "testing"

// TestGrammar fails the build if the grammar does not compile, and checks one input of each format.
func TestGrammar(t *testing.T) {
	if _, err := durationParser.Parse("1h30m"); err != nil {
		t.Error(err)
	}
	if _, err := sizeParser.Parse("10MiB"); err != nil {
		t.Error(err)
	}
	if _, err := sizeParser.Parse("10 MiB"); err == nil {
		t.Error("a space must not be accepted")
	}
}
```

```go
// main.go
package main

import (
	"fmt"
	"sync"
)

func main() {
	type job struct {
		kind, input string
	}
	jobs := []job{{"duration", "1h30m"}, {"size", "10MiB"}, {"duration", "250ms"}, {"size", "10 MiB"}, {"duration", "90"}}
	results := make([]string, len(jobs))

	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := durationParser
			if j.kind == "size" {
				p = sizeParser
			}
			n, err := p.Parse(j.input)
			if err != nil {
				results[i] = fmt.Sprintf("%-8s %-8q error: %v", j.kind, j.input, err)
				return
			}
			results[i] = fmt.Sprintf("%-8s %-8q %v", j.kind, j.input, n)
		}()
	}
	wg.Wait()
	for _, r := range results {
		fmt.Println(r)
	}
}
```

## Expected Output

The results are printed in the order of the jobs, whatever order the goroutines finish in. Run `go run .` and `go test -race`:

```text
duration "1h30m"  [Quantity{Unit: Unit("h"), Value: Number("1")} Quantity{Unit: Unit("m"), Value: Number("30")}]
size     "10MiB"  Quantity{Unit: Unit("MiB"), Value: Number("10")}
duration "250ms"  [Quantity{Unit: Unit("ms"), Value: Number("250")}]
size     "10 MiB" error: 1:3: syntax error: expected "GiB", "KiB", "MiB", "h", "m", "ms", "s", [0-9]
duration "90"     error: 1:3: syntax error: expected "GiB", "KiB", "MiB", "h", "m", "ms", "s", [0-9]
```

## How It Works

- `//go:embed units.pego` puts the file in the binary, so the program needs no file at run time, and the grammar is still a
  file that tools can read.
- `CompileSource(grammar, start)` parses, checks and compiles the grammar, which costs far more than a parse. Doing it in a
  package-level variable means it happens once, before `main`.
- A compiled `*pego.Parser` may be used by any number of goroutines at once, as `main` does, and so may the parser that
  `WithStart` derives from it.
- The test runs `go test` on every change, so a broken grammar fails the build, not the first request.

## Variations and Limits

- `WithStart` returns a new parser, so call it once for each format and keep the result, not once for each input.
- To avoid compiling at start-up at all, generate Go code or save a `.pegoc` file; see [Ship a parser and keep it up to
  date](ship-a-parser.md).
- The grammar is data: to load one at run time instead, call `CompileSource` on text you read. The guide
  [Running Parsers](../guide/running-parsers.md) lists the other ways to compile a grammar.
