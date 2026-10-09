# Compiled Grammars (`.pegoc`)

Compiling a grammar from source parses it, analyzes it, type-checks it and translates it to bytecode. A **compiled
grammar** is the result of that work saved to a file: a `.pegoc`. Loading one gives you the same `*pego.Parser` without
repeating the work, so a program can start faster, and a grammar can travel as a data file or a `go:embed` byte slice
without its source.

This guide shows how to save and load a compiled grammar, what a file without the grammar AST can and cannot do, what
it costs and saves, how files stay compatible between releases, and when to use one rather than
[`pego gen`](code-generation.md) or compiling from source at start-up.

- [Save and load](#save-and-load)
- [With and without the AST](#with-and-without-the-ast)
- [Size and load time](#size-and-load-time)
- [Embedding with `go:embed`](#embedding-with-goembed)
- [Versions and compatibility](#versions-and-compatibility)
- [Loading untrusted data](#loading-untrusted-data)
- [Compiled grammar, generated code or source?](#compiled-grammar-generated-code-or-source)
- [Troubleshooting](#troubleshooting)

The examples use the grammar `pairs.pego` of the [runtime guide](runtime.md):

```pego
type Pair struct { Key Match, Value Match }

def main = ws ps:pair+ $$ -> $ps
def pair: Pair = k:key "=" v:value ";"? ws -> new Pair{Key: $k, Value: $v}
def key = @(?a-zあ-ん)+
def value = @(?0-9)+
def ws = (? \t\n)*
```

## Save and load

On the command line, `pego compile` writes the file, and every command that takes a grammar (`-g`) accepts it:

```bash
$ pego compile -g pairs.pego -o pairs.pegoc
$ pego parse -g pairs.pegoc -f sexpr -i 'abc=12; あい=3'
[(Pair Key="abc"@key Value="12"@value) (Pair Key="あい"@key Value="3"@value)]
```

The start rule is saved with the grammar (`-s` chooses a rule other than `main`), and `pego parse`, `pego compile` and
`pego gen` use it when you give no `-s` of your own:

```bash
$ pego compile -g pairs.pego -s pair -o pair.pegoc
$ pego parse -g pair.pegoc -f sexpr -i 'abc=12'
(Pair Key="abc"@key Value="12"@value)
$ pego parse -g pair.pegoc -s main -f sexpr -i 'abc=12; x=1'
[(Pair Key="abc"@key Value="12"@value) (Pair Key="x"@key Value="1"@value)]
```

In Go, `Parser.MarshalBinary` returns the bytes and `pego.LoadParser` restores the parser, with the saved start rule:

```go
p, err := pego.CompileSource(src, "main")
data, err := p.MarshalBinary() // bytecode, grammar AST and start rule

q, err := pego.LoadParser(data)
node, err := q.Parse("abc=12")
fmt.Println(node, err) // [(Pair Key="abc"@key Value="12"@value)] <nil>
```

A few more facts:

- The same parser always produces the same bytes, and a parser that was loaded and marshaled again produces the bytes
  it was loaded from. This is what makes the [up-to-date test](#embedding-with-goembed) possible.
- `pego.IsCompiled(data)` tells a compiled grammar from other data by its magic number. The commands use it to decide
  whether `-g` names source, JSON or a compiled grammar.
- A loaded parser is an ordinary `*pego.Parser`: it is safe for concurrent use, `WithStart` changes its start rule, and
  `Parse`, `ParseStream`, `NewDocument` and the options of the [runtime guide](runtime.md) work on it, within the limits
  of the next section.
- A parser that was loaded from a file is the same as one compiled from source for every grammar of `examples/` and of
  the engine tests (the node tree, positions and errors are compared); `internal/engine/compiled_test.go` is the test.

## With and without the AST

A compiled grammar always holds the bytecode. It holds the grammar AST (and the results of static analysis) too, unless
you leave it out:

```go
full, err := p.MarshalBinary()               // bytecode + grammar AST + start rule
slim, err := p.Marshal(pego.WithoutAST())    // bytecode + start rule
```

```bash
pego compile -g pairs.pego -o pairs.pegoc                # with the AST
pego compile -g pairs.pego -no-ast -o pairs-noast.pegoc  # without it
```

The AST serves the closure backend, recognition mode and every tool that works on the grammar; the bytecode serves the
two bytecode backends. What you keep and lose without it:

| | With the AST | Without the AST (`WithoutAST`, `-no-ast`) |
|:--|:--|:--|
| `Parse`, `ParseStream`, `NewDocument` | yes | yes (bytecode backends) |
| Default backend | closure (the fastest of the three) | recursive bytecode |
| `WithBackend(Closure)` | yes | error: `the closure backend needs the grammar, which the compiled grammar omits` |
| `WithBackend(Bytecode)`, `WithBackend(BytecodeIterative)` | yes | yes |
| `RecognizeOnly()`, `pego parse -check` | yes | error: `recognition needs the grammar, which the compiled grammar omits` |
| `Parser.Grammar()` | the grammar, without source positions | `nil` |
| `pego.Lint`, `pego lint` | yes | no |
| `pego.GenerateGo`, `pego.GenerateTS`, `pego gen` | yes (the output is the same as from source) | no |
| `pego convert` back to `.pego` or `.json` | yes, without comments or layout | no: `the compiled grammar omits the AST` |
| `pego sample` | yes | no: `the parser has no grammar AST` |
| `pego explain`, `pego trace`, `pego profile` | yes | yes |
| `WithStart`, `Marshal` again | yes | yes |

`pego compile -g file.pegoc -no-ast` drops the AST of an existing file. There is no way back: a file without the AST
cannot be compiled again with one, so keep the source.

Choose the file without the AST for a build artifact that is only ever executed, and the full file when something may
want to read the grammar again (a tool that lints, formats or generates from it) or when the closure backend's speed
matters more than the load time. The effects on size and load time are in the next section. The effect on parse speed
is the difference between the backends, which the [runtime guide](runtime.md#which-one) measures: on its benchmarks the
recursive bytecode VM is 1.1 to 1.3 times slower than the closure backend. If you load a file without the AST and want
the iterative VM, say so: `pego.WithBackend(pego.BytecodeIterative)`.

An example of what a parser loaded without the AST does and does not do:

```go
q, _ := pego.LoadParser(slim)
fmt.Println(q.Grammar() == nil) // true

_, err := q.Parse("abc=12", pego.WithBackend(pego.Closure))
fmt.Println(err) // the closure backend needs the grammar, which the compiled grammar omits

_, err = q.Parse("abc=12", pego.RecognizeOnly())
fmt.Println(err) // recognition needs the grammar, which the compiled grammar omits

node, err := q.Parse("abc=12") // runs on the bytecode backend
fmt.Println(node, err)         // [(Pair Key="abc"@key Value="12"@value)] <nil>
```

## Size and load time

What a compiled grammar costs in size, measured for four grammars of the repository (a file with the AST is larger than
the source, and a file without it is smaller):

| Grammar | Source | `.pegoc` | `.pegoc` without the AST |
|:--|--:|--:|--:|
| `pairs.pego` (this guide) | 219 bytes | 552 bytes | 410 bytes |
| `examples/minilang` | 4,416 bytes | 6,751 bytes | 4,899 bytes |
| `parsers/python` | 84,841 bytes | 129,065 bytes | 94,379 bytes |
| `parsers/duckdb` | 222,544 bytes | 501,989 bytes | 369,530 bytes |

The time until a parser has made its first parse, measured by the program below (Apple M3 Max, Go 1.27.1, one run of
`testing.Benchmark` per row; the figures vary by some percent from run to run). Each row prepares a parser from scratch
and parses a one-line input, so the work that happens on the first parse after loading (assembling closures or
preparing the VM) is included:

| Grammar | Compile from source | Load `.pegoc` | Load `.pegoc` without the AST |
|:--|--:|--:|--:|
| `pairs.pego` | 34 µs | 38 µs | 27 µs |
| `examples/minilang` | 638 µs | 228 µs | 93 µs |
| `parsers/python` | 17.3 ms | 7.1 ms | 0.97 ms |
| `parsers/duckdb` | 58.6 ms | 35.3 ms | 4.8 ms |

The savings grow with the grammar. For a grammar as small as `pairs.pego` there is nothing to save, and for a large
one the file without the AST loads an order of magnitude faster than compiling and allocates a tenth or less. A file
with the AST takes about as long as compiling for a small grammar, because it also rebuilds the AST, and the gap opens
as the grammar grows. The [benchmarks](../benchmarks.md#preparation-time-until-a-grammar-is-ready-to-use)
measure the same on the JSON and minilang grammars of the benchmark suite, including the memory of each step, and the
[analysis](../performance.md#where-pego-stands) summarizes it.

The program (run it as `go run . grammar.pego 'input'` in a module that requires PEGO; the start rule is `main`):

<details>
<summary>The measuring program</summary>

```go
package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/ornew/pego"
)

func main() {
	path, input := os.Args[1], os.Args[2]
	src, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		panic(err)
	}
	full, _ := p.MarshalBinary()
	slim, _ := p.Marshal(pego.WithoutAST())
	fmt.Printf("source %d bytes, .pegoc %d bytes, .pegoc without AST %d bytes\n", len(src), len(full), len(slim))

	// Each run makes a parser ready and parses one input, so the setup that
	// happens on the first parse is included.
	measure := func(name string, prepare func() *pego.Parser) {
		r := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				if _, err := prepare().Parse(input); err != nil {
					b.Fatal(err)
				}
			}
		})
		fmt.Printf("%-28s %8d ns/op %8d B/op %6d allocs/op\n", name, r.NsPerOp(), r.AllocedBytesPerOp(), r.AllocsPerOp())
	}
	measure("compile from source", func() *pego.Parser {
		p, _ := pego.CompileSource(string(src), "main")
		return p
	})
	measure("load .pegoc", func() *pego.Parser {
		p, _ := pego.LoadParser(full)
		return p
	})
	measure("load .pegoc without AST", func() *pego.Parser {
		p, _ := pego.LoadParser(slim)
		return p
	})
}
```

For `parsers/duckdb/duckdb.pego` and the input `SELECT 1;` it printed:

```
source 222544 bytes, .pegoc 501989 bytes, .pegoc without AST 369530 bytes
compile from source          58616781 ns/op 68464729 B/op 417683 allocs/op
load .pegoc                  35276369 ns/op 28399086 B/op 164313 allocs/op
load .pegoc without AST       4849960 ns/op  5489814 B/op  36362 allocs/op
```

</details>

Whether this matters depends on how often the program starts. A long-running service compiles once and parses millions
of times, and 60 ms is nothing. A command-line tool, a function that starts for each request or a test that compiles a
large grammar in every process pays it every time.

## Embedding with `go:embed`

The usual pattern is to compile when the grammar changes, commit the `.pegoc` next to the source, and embed it. The
layout of a module:

```
example.com/pairs
├── go.mod
├── pairs.pego
├── pairs.pegoc          # created by go generate, committed
├── generate.go
├── main.go
└── compiled_test.go
```

Record `pego` as a tool of the module (`go get -tool github.com/ornew/pego/cmd/pego`), so that `go generate` runs the
pinned version, and declare the step:

```go
// generate.go
package main

//go:generate go tool pego compile -g pairs.pego -no-ast -o pairs.pegoc
```

Embed the file and load it once, when the program starts:

```go
// main.go
package main

import (
	_ "embed"
	"fmt"
	"log"

	"github.com/ornew/pego"
)

//go:embed pairs.pegoc
var compiled []byte

var parser = func() *pego.Parser {
	p, err := pego.LoadParser(compiled)
	if err != nil {
		panic(err)
	}
	return p
}()

func main() {
	node, err := parser.Parse("abc=12; あい=3")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(node)
}
```

```bash
$ go generate ./...
$ go run .
[(Pair Key="abc"@key Value="12"@value) (Pair Key="あい"@key Value="3"@value)]
```

`go:embed` needs the file to exist when the package is built, so commit the `.pegoc`. Because a `.pegoc` is binary,
tell your review tool, for example with `*.pegoc binary` in `.gitattributes`, and expect its diff to say only that it
changed.

Add a test that fails when somebody changes the grammar and forgets to regenerate. It compiles the source in memory and
compares the bytes with the committed file, which works because the output is deterministic:

```go
// compiled_test.go
package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/ornew/pego"
)

// pairs.pegoc is up to date with pairs.pego.
func TestCompiledIsUpToDate(t *testing.T) {
	src, err := os.ReadFile("pairs.pego")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		t.Fatal(err)
	}
	want, err := p.Marshal(pego.WithoutAST()) // the options of the go:generate line
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compiled, want) {
		t.Error("pairs.pegoc is out of date; run go generate ./...")
	}
}
```

After changing `pairs.pego` (here: `def value = @(?0-9a-f)+`) without running `go generate`, the test fails:

```
--- FAIL: TestCompiledIsUpToDate (0.00s)
    compiled_test.go:26: pairs.pegoc is out of date; run go generate ./...
FAIL
FAIL	example.com/pairs	0.174s
FAIL
```

The same test also catches an upgrade of PEGO that changes the bytes of an unchanged grammar (see below). In CI,
`go generate ./... && git diff --exit-code` does the same without a test.

A file does not have to be embedded. A program can read a `.pegoc` from disk or download it and call `LoadParser`, so
that a grammar changes without rebuilding the program. Then [only load files you trust](#loading-untrusted-data), and
handle the error of `LoadParser`: it is the only place a bad file is noticed.

## Versions and compatibility

A file carries two version numbers after its magic number:

| Number | Now | Meaning |
|:--|--:|:--|
| Format version | 2 | The layout of the file. A runtime loads files of version 1 and 2 and refuses any other. |
| Instruction-set version | 3 | The instructions the bytecode may use. A runtime loads files of every instruction-set version up to its own: new versions only add instructions, so older files keep working (they just lack the newer, faster instructions). |

The consequences:

- **A newer PEGO loads the files of an older one.** It does not make them faster than they were: a file built by an old
  release has the bytecode that release produced.
- **An older PEGO does not load the files of a newer one.** It reports `unsupported instruction set version` or
  `unsupported compiled grammar version`.
- **The same grammar can produce different bytes in a new release**, when the compiler improves. Rebuild the file when
  you upgrade PEGO, as you would regenerate [generated code](code-generation.md#keeping-generated-code-up-to-date).
  The up-to-date test above fails after such an upgrade until you do.
- **Build and load with the same release** when you can. The tests check that a loaded file behaves like the grammar it
  was made from for the release that wrote it; files from other releases are loaded on the strength of the version
  numbers alone.

The format is specified in [bytecode.md](../bytecode.md#file-format) and its design in
[design record 009](../design/009-compiled-grammar-format.md).

## Loading untrusted data

Loading checks the magic number, the versions, a CRC-32 checksum, the structure (every index is in range, every jump
goes forward, no trailing bytes) and the nesting depth. It does **not** type-check the grammar again, and it does not
verify that the bytecode uses the VM's stacks correctly. A file made to contain type errors can make actions fail while
parsing, and made-up bytecode is not guaranteed to terminate.

Only load files that you or your build produced: the output of `pego compile`, `MarshalBinary` or `Marshal`. The
checksum is not a signature: it detects accidents, not tampering. If a file travels over a network, sign it or compare
a hash that you obtained another way. If users bring their own grammars, accept source, compile it with
`CompileSource`, which type-checks, and (if needed) save the result yourself.

## Compiled grammar, generated code or source?

Three ways to get a grammar into a Go program:

| | Compile from source at start-up | Compiled grammar (`.pegoc`) | Generated code (`pego gen`) |
|:--|:--|:--|:--|
| What the program holds | The `.pego` text | A binary file, embedded or read | Go source, compiled in |
| Start-up | Compiles: tens of microseconds to tens of milliseconds, by size | Loads: a fraction of that (see above) | None |
| Parsing speed | Closure backend | Closure with the AST, bytecode without | The fastest: 28 to 45% faster than the closure backend ([benchmarks](../benchmarks.md)) |
| Depends on PEGO at run time | Yes | Yes (the engine and `LoadParser`) | No: standard library only |
| Change the grammar without rebuilding the program | Yes (read the file) | Yes (read the file) | No |
| Streaming, incremental parsing, backend choice, recognition mode | Yes | Yes (recognition needs the AST) | No |
| Errors in the grammar are found | At start-up | When you build the file | When you generate |
| Typical use | Development, tools, tests, grammars that users supply | A fixed grammar in a program with short runs or many processes; grammars that ship as data | A fixed grammar on a hot path, a small binary, no dependency on PEGO |

Rules of thumb:

- **Use source** while the grammar changes, and whenever the cost of compiling is not noticeable: the grammar is small,
  or the process is long-lived. It is the simplest.
- **Use a `.pegoc`** when start-up is noticeable (a command-line tool, a plugin, a serverless function, a test binary),
  when the grammar has to be a data file, or when you want to keep PEGO's features that generated code lacks
  ([streaming and incremental parsing](streaming.md), [backends](runtime.md#backends)) and still skip the compile.
- **Use `pego gen`** when the parser is on a hot path or you want no dependency on PEGO at run time: it has no
  start-up and the fastest parsing. The cost is that the grammar is frozen into the binary, and that the generated
  source is large: `parsers/python/parser.go` is 3,948,176 bytes, against 84,841 bytes for `python.pego` and 94,379 for
  its `.pegoc` without the AST, and `parsers/duckdb/parser.go` is 13,216,774 bytes against 222,544 and 369,530.

The three give the same trees and the same errors, so you can start with source, move to a file when start-up shows up
in a profile, and to generated code when parsing speed does.

## Troubleshooting

| Message | Cause |
|:--|:--|
| `not a compiled PEGO grammar` | The data does not start with the magic number: it is a source file, JSON, or something else. |
| `unsupported compiled grammar version N (want 2)` | The file was written by a different format version. Rebuild it with your release. |
| `unsupported instruction set version N (want 1 to 3)` | The file was written by a newer release. Upgrade PEGO or rebuild the file. |
| `invalid compiled grammar: checksum mismatch` | The file is truncated or changed (a bad download, an editor that converted line endings, a merge). |
| `invalid compiled grammar: ...` (other) | The structure is wrong, though the checksum matches. Rebuild the file. |
| `the closure backend needs the grammar, which the compiled grammar omits` | You asked a file without the AST for `Closure`. Use a bytecode backend, or the full file. |
| `recognition needs the grammar, which the compiled grammar omits` | `RecognizeOnly()` or `pego parse -check` on a file without the AST. Use the full file, or parse and ignore the tree. |
| `the compiled grammar omits the AST` | `pego convert`, `pego gen` or `pego lint` on a file without the AST. Use the source, or a full file. |

The errors of a damaged file, as `LoadParser` reports them (from a program that changes a copy of `pairs.pegoc`):

```
intact                 <nil>
format version 3       unsupported compiled grammar version 3 (want 2)
one byte changed       invalid compiled grammar: checksum mismatch
truncated              invalid compiled grammar: checksum mismatch
PEGO source            not a compiled PEGO grammar
```

## See also

- [Running parsers](runtime.md): the Go API, backends, position units and the `pego` command.
- [Code generation](code-generation.md): the other way to skip compiling at run time.
- [Benchmarks](../benchmarks.md#preparation-time-until-a-grammar-is-ready-to-use) and
  [performance notes](../performance.md#where-pego-stands): preparation times and sizes.
- [bytecode.md](../bytecode.md#file-format): the file format, and [design record 009](../design/009-compiled-grammar-format.md).
