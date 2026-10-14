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
type Pair struct { Key Match; Value Match }

rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

## Save and Load

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
- A parser loaded from a file preserves the same tree, positions and errors as one compiled from source. Keep a
  round-trip comparison in your own test suite for the grammars your application depends on.

## With and Without the AST

A compiled grammar always holds the bytecode. It holds the grammar AST (and the results of static analysis) too, unless
you leave it out:

```go
full, err := p.MarshalBinary()               // bytecode + grammar AST + start rule
slim, err := p.Marshal(pego.WithoutAST())    // bytecode + optional recognition bundle + start rule
```

```bash
pego compile -g pairs.pego -o pairs.pegoc                # with the AST
pego compile -g pairs.pego -no-ast -o pairs-noast.pegoc  # without it
```

The AST serves the closure backend and tools that inspect or transform the grammar. The bytecode serves the two bytecode
backends. Format version 3 and later can also carry an independent recognition bundle, so recognition without the AST
depends on whether that bundle is present and whether it contains the selected start rule. Version 4 adds initializer
assignments. What you keep and lose without the AST:

| | With the AST | Without AST or recognition bundle | Without AST, with a version-3+ recognition bundle |
|:--|:--|:--|:--|
| `Parse`, `ParseStream`, `NewDocument` | yes | yes (bytecode backends) | yes (bytecode backends) |
| Default backend | closure | recursive bytecode | recursive bytecode |
| `WithBackend(Closure)` | yes | error: `the closure backend needs the grammar, which the compiled grammar omits` | same error |
| `WithBackend(Bytecode)`, `WithBackend(BytecodeIterative)` | yes | yes | yes |
| `RecognizeOnly()`, `pego parse -check` | yes | unavailable | yes for a start rule in the bundle |
| Version-4 initializers | yes | yes | yes |
| `Parser.Grammar()` | the grammar, without source positions | `nil` | `nil` |
| `pego.Lint`, `pego lint` | yes | no | no |
| `pego.GenerateGo`, `pego.GenerateTS`, `pego gen` | yes (the output is the same as from source) | no | no |
| `pego convert` back to `.pego` or `.json` | yes, without comments or layout | no: `the compiled grammar omits the AST` | same error |
| `pego sample` | yes | no: `the parser has no grammar AST` | same error |
| `pego explain`, `pego trace`, `pego profile` | yes | yes | yes |
| `WithStart`, `Marshal` again | yes | yes | yes |

`pego compile -g file.pegoc -no-ast` drops the AST of an existing file while preserving any recognition bundle and
initializer table. There is no way back: a file without the AST cannot be compiled again with one, so keep the source.
An AST-less file without a recognition bundle cannot gain recognition by changing its start rule; recompile the source
to produce a bundle.

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

recognized, err := q.Parse("abc=12", pego.RecognizeOnly()) // requires a recognition bundle
fmt.Println(recognized == nil, err) // true <nil>

node, err := q.Parse("abc=12") // runs on the bytecode backend
fmt.Println(node, err)         // [(Pair Key="abc"@key Value="12"@value)] <nil>
```

## Size and Load Time

Compiled-file size and load time depend on the grammar and on which sections the file contains: bytecode, an optional
AST, an optional version-3 recognition bundle, and the version-4 initializer table. Measure the artifact produced for
your grammar and selected options rather than relying on figures from an older format. The program below measures
the current format for a grammar of your choice.

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

</details>

Whether this matters depends on how often the program starts. A long-running service compiles once and parses millions
of times, and 60 ms is nothing. A command-line tool, a function that starts for each request or a test that compiles a
large grammar in every process pays it every time.

## Embedding With `go:embed`

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

After changing `pairs.pego` (here: `rule value =  [0-9a-f]+ #atomic `) without running `go generate`, the test fails:

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

## Versions and Compatibility

A file carries two version numbers after its magic number:

| Number | Now | Meaning |
|:--|--:|:--|
| Format version | 4 | The layout of the file. Version 3 adds an optional recognition bundle; version 4 adds the initializer table. A runtime loads supported files of versions 1 through 4. |
| Instruction-set version | 5 | The instructions the bytecode may use. Version 4 adds full-width repetition bounds; version 5 adds terminal construction. A runtime loads files of every instruction-set version up to its own. |

The consequences:

- **Recognition without the AST requires a bundle.** Version-3/4 files may carry an independent recognition module and
  start-rule map. If the requested start is present, `RecognizeOnly()` and `pego parse -check` can run without the AST.
  Older AST-less files, or files written without the bundle, cannot recognize; recompile the source to create a new bundle.
- **Version 4 runs initializers before each selected start-rule invocation.** The assignments use a fresh variable
  environment and are also applied during recognition and stream parsing.
- **A newer PEGO loads the files of an older one.** It does not make them faster than they were: a file built by an old
  release has the bytecode that release produced.
- **An older PEGO refuses files requiring a newer format or instruction set.**
  It reports `unsupported instruction set version` or `unsupported compiled grammar version`.
  A grammar with bounds above 2³¹−1 requires instruction set 4; ordinary modules
  retain version 3. A runtime with a narrower implementation `int` rejects
  bounds outside its range. Recompile older files containing large bounds:
  their already-truncated bytecode cannot recover the original values.
- **The same grammar can produce different bytes in a new release**, when the compiler improves. Rebuild the file when
  you upgrade PEGO, as you would regenerate [generated code](code-generation.md#keeping-generated-code-up-to-date).
  The up-to-date test above fails after such an upgrade until you do.
- **Build and load with the same release** when you can. The tests check that a loaded file behaves like the grammar it
  was made from for the release that wrote it; files from other releases are loaded on the strength of the version
  numbers alone.

Saved files retain the left-recursion classification chosen by the compiler that wrote them, including files with
the AST. To adopt corrected classification of unreachable alternatives or recovery skip calls, recompile the source
with the current compiler and regenerate standalone parsers. Loading an older file does not rerun this analysis;
the file format and bytecode versions are unchanged by these classification fixes.

The format is specified in [bytecode.md](../../spec/bytecode.md#file-format) and its design in
[design record 009](../design/009-compiled-grammar-format.md).

## Loading Untrusted Data

Loading rejects recovery regions that enclose a streaming commitment, including older files made with
`#recover` on the `#stream` repetition. Move recovery into the repeated element and recompile those files.
Element-local recovery remains supported with or without the AST; the file format and instruction set are unchanged.

Loading checks the magic number, the versions, a CRC-32 checksum, the structure (every index is in range, every jump
goes forward, no trailing bytes) and the nesting depth. It does **not** type-check the grammar again, and it does not
verify that the bytecode uses the VM's stacks correctly. A file made to contain type errors can make actions fail while
parsing, and made-up bytecode is not guaranteed to terminate.

Only load files that you or your build produced: the output of `pego compile`, `MarshalBinary` or `Marshal`. The
checksum is not a signature: it detects accidents, not tampering. If a file travels over a network, sign it or compare
a hash that you obtained another way. If users bring their own grammars, accept source, compile it with
`CompileSource`, which type-checks, and (if needed) save the result yourself.

## Compiled Grammar, Generated Code or Source?

Three ways to get a grammar into a Go program:

| | Compile from source at start-up | Compiled grammar (`.pegoc`) | Generated code (`pego gen`) |
|:--|:--|:--|:--|
| What the program holds | The `.pego` text | A binary file, embedded or read | Go source, compiled in |
| Start-up | Compiles the grammar | Loads a compiled artifact; measure its current cost | None |
| Parsing speed | Closure backend | Closure with the AST, bytecode without | Generated Go takes 0.41–0.60× closure time in CodePoints and 0.47–0.62× in Bytes across current workloads ([benchmarks](../benchmarks.md)) |
| Depends on PEGO at run time | Yes | Yes (the engine and `LoadParser`) | No: standard library only |
| Change the grammar without rebuilding the program | Yes (read the file) | Yes (read the file) | No |
| Streaming, incremental parsing, backend choice, recognition mode | Yes | Yes; recognition requires a v3+ bundle | No |
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
  source can be much larger than its grammar source.

The three give the same trees and the same errors, so you can start with source, move to a file when start-up shows up
in a profile, and to generated code when parsing speed does.

## Troubleshooting

| Message | Cause |
|:--|:--|
| `not a compiled PEGO grammar` | The data does not start with the magic number: it is a source file, JSON, or something else. |
| `unsupported compiled grammar version N (want 4)` | The file was written by a different format version. Rebuild it with your release. |
| `unsupported instruction set version N (want 1 to 5)` | The file requires a newer instruction set. Upgrade PEGO or rebuild the file. |
| `invalid compiled grammar: checksum mismatch` | The file is truncated or changed (a bad download, an editor that converted line endings, a merge). |
| `invalid compiled grammar: ...` (other) | The structure is wrong, though the checksum matches. Rebuild the file. |
| `the closure backend needs the grammar, which the compiled grammar omits` | You asked a file without the AST for `Closure`. Use a bytecode backend, or the full file. |
| `recognition needs the grammar or a recognition bundle; recompile this compiled grammar from source` | `RecognizeOnly()` or `pego parse -check` on a file without an AST and without a usable recognition bundle. Recompile the source to write a bundle. |
| `the compiled grammar omits the AST` | `pego convert`, `pego gen` or `pego lint` on a file without the AST. Use the source, or a full file. |

The errors of a damaged file, as `LoadParser` reports them (from a program that changes a copy of `pairs.pegoc`):

```
intact                 <nil>
format version 5       unsupported compiled grammar version 5 (want 4)
one byte changed       invalid compiled grammar: checksum mismatch
truncated              invalid compiled grammar: checksum mismatch
PEGO source            not a compiled PEGO grammar
```

## See Also

- [Running parsers](runtime.md): the Go API, backends, position units and the `pego` command.
- [Code generation](code-generation.md): the other way to skip compiling at run time.
- [Benchmarks](../benchmarks.md#preparation-time-until-a-grammar-is-ready-to-use) and
  [performance notes](../performance.md#where-pego-stands): preparation times and sizes.
- [bytecode.md](../../spec/bytecode.md#file-format): the file format, and [design record 009](../design/009-compiled-grammar-format.md).
