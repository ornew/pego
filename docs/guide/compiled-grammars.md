# Compiled Grammars (`.zpc`)

Compiling a grammar from source parses it, analyzes it, type-checks it and translates it to bytecode. A **compiled
grammar** is the result of that work saved to a file: a `.zpc`. Loading one gives you the same `*zp.Parser` without
repeating the work, so a program can start faster, and a grammar can travel as a data file or a `go:embed` byte slice
without its source.

A compiled grammar holds the bytecode of one **mode**, chosen when you save it: `parse` (the default) for parsing,
`recognize` for recognition, `document` for incremental Documents and traced or profiled parses. It holds no grammar
AST and no output schema: tools that read the grammar take the source.

This guide shows how to save and load a compiled grammar, which operations each mode serves, what it costs and saves,
how files stay compatible between releases, and when to use one rather than [`zp gen`](code-generation.md) or
compiling from source at start-up.

- [Save and load](#save-and-load)
- [Modes](#modes)
- [Size and load time](#size-and-load-time)
- [Embedding with `go:embed`](#embedding-with-goembed)
- [Versions and compatibility](#versions-and-compatibility)
- [Loading untrusted data](#loading-untrusted-data)
- [Compiled grammar, generated code or source?](#compiled-grammar-generated-code-or-source)
- [Troubleshooting](#troubleshooting)

The examples use the grammar `pairs.zp` of the [Running Parsers](running-parsers.md):

```zp
type Pair = { Key node[string]; Value node[string] }

@start(default)
rule main = ws ps:pair+ $$ => $ps
@start
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

## Save and Load

On the command line, `zp compile` writes the file, and `zp parse` accepts it as its grammar (`-g`):

```bash
zp compile -g pairs.zp -o pairs.zpc
zp parse -g pairs.zpc -f sexpr -i 'abc=12; あい=3'
```

```text
[Pair{Key: "abc", Value: "12"} Pair{Key: "あい", Value: "3"}]
```

The start rule is saved with the bytecode (`-s` chooses another rule marked `@start` than the default), and
`zp parse` and `zp compile` use it when you give no `-s` of your own:

```bash
zp compile -g pairs.zp -s pair -o pair.zpc
zp parse -g pair.zpc -f sexpr -i 'abc=12'
```

```text
Pair{Key: "abc", Value: "12"}
```

```bash
zp parse -g pair.zpc -s main -f sexpr -i 'abc=12; x=1'
```

```text
[Pair{Key: "abc", Value: "12"} Pair{Key: "x", Value: "1"}]
```

In Go, `Parser.MarshalBinary` returns the bytes of a parse-mode file and `zp.LoadParser` restores the parser, with
the saved start rule:

```go
p, err := zp.CompileSource(src, "main")
data, err := p.MarshalBinary() // the bytecode of parse mode and the start rule

q, err := zp.LoadParser(data)
node, err := q.Parse("abc=12")
fmt.Println(node, err) // [Pair{Key: "abc", Value: "12"}] <nil>
```

A few more facts:

- The same parser always produces the same bytes for a mode, and a parser that was loaded and saved again produces the
  bytes it was loaded from. This is what makes the [up-to-date test](#embedding-with-goembed) possible.
- `zp.IsCompiled(data)` tells a compiled grammar from other data by its magic number. The commands use it to decide
  whether `-g` names source, JSON or a compiled grammar.
- A loaded parser is an ordinary `*zp.Parser`: it is safe for concurrent use, `WithStart` changes its start rule, and
  it runs the operations of its mode with the options of the [Running Parsers](running-parsers.md).
- For those operations, a parser loaded from a file preserves the same tree, positions and errors as one compiled from
  source. Keep a round-trip comparison in your own test suite for the grammars your application depends on.

## Modes

Choose the mode for what the program does with the parser:

```go
parsing, err := p.Marshal(zp.CompiledParse)       // the same as p.MarshalBinary()
recognizing, err := p.Marshal(zp.CompiledRecognize)
documents, err := p.Marshal(zp.CompiledDocument)
```

```bash
zp compile -g pairs.zp -o pairs.zpc                            # parse mode
zp compile -g pairs.zp -mode recognize -o pairs-check.zpc      # for zp parse -check
zp compile -g pairs.zp -mode document -o pairs-doc.zpc         # for Documents, trace, profile, explain
```

| Mode | Serves |
|:--|:--|
| `parse` (`zp.CompiledParse`) | `Parse`, `ParsePrefix`, `ParseStream`, `Iterate`; `zp parse`, `zp parse -stream` |
| `recognize` (`zp.CompiledRecognize`) | `Parse` and `ParsePrefix` with `RecognizeOnly()`; `zp parse -check` |
| `document` (`zp.CompiledDocument`) | `NewDocument`, and `Parse`, `ParsePrefix`, `ParseStream`, `Iterate` with `WithTrace` or `WithProfile`; `zp trace`, `zp profile`, `zp explain` |

A file serves its mode only. Anything else returns an error that names the file's mode, the mode the operation needs
and how to get it, rather than falling back:

```go
q, _ := zp.LoadParser(parsing)
_, err := q.NewDocument("abc=12")
fmt.Println(err)
// the compiled grammar is in parse mode; documents need document mode: compile it with -mode document (zp.CompiledDocument), or compile the grammar from source
```

`q.CompiledMode()` returns the mode of a loaded parser and `true`, and `false` for a parser compiled from source, which
serves every mode. `zp.ParseCompiledMode` turns the name of a mode into the value.

Some things need the grammar itself, so they work only from source, whatever the mode:

| | Parser loaded from a `.zpc` |
|:--|:--|
| Backend | Recursive bytecode by default; `WithBackend(zp.BytecodeIterative)` selects the iterative VM |
| `WithBackend(Closure)` | error: `the closure backend needs the grammar source; a compiled grammar runs on the bytecode backends` |
| `RecognizeOnly()` with `WithTrace` or `WithProfile` | error: traced or profiled recognition needs the grammar source |
| `Parser.Grammar()` | `nil` |
| `OutputSchema` | an error that matches `ErrSchemaUnavailable` |
| `lint.Config.Run`, `gen.Go.Generate`, `gen.TypeScript.Generate`, `sample.New` | need the grammar, which a loaded parser does not have |
| `zp convert`, `zp gen`, `zp lint`, `zp sample`, `zp schema` | error: the command needs the grammar source (`.zp` or `.json`) |

A loaded parser is saved again only in its own mode: `zp compile -g pairs-check.zpc -mode recognize -o copy.zpc`
copies a recognize-mode file byte for byte, and asking for another mode is an error that writes no output. To change
the mode, compile from the source.

The effect on parse speed is the difference between the backends, which the [Choosing a Backend](backends.md#which-one)
measures: on its benchmarks the recursive bytecode VM is 1.1 to 1.3 times slower than the closure backend that a parser
compiled from source uses by default.

## Size and Load Time

A recognize-mode file holds the bytecode of the grammar's recognizer, which builds no values, so it is the smallest.
A parse-mode and a document-mode file of a grammar currently hold the same module and have the same size.

| Parser | parse | recognize | document |
|---|---:|---:|---:|
| cel | 12,838 | 8,054 | 12,838 |
| csv | 767 | 535 | 767 |
| cue | 60,895 | 51,797 | 60,895 |
| duckdb | 387,682 | 235,530 | 387,682 |
| golang | 80,479 | 80,147 | 80,479 |
| json | 2,222 | 1,419 | 2,222 |
| python | 96,031 | 95,761 | 96,031 |
| typescript | 145,524 | 144,800 | 145,524 |
| xml | 11,259 | 9,262 | 11,259 |
| yaml | 26,178 | 17,151 | 26,178 |

The columns are the exact file sizes in bytes of `zp compile -mode parse|recognize|document` for the ready-made
parsers. The benchmark that reproduces the serialization and load measurements is
`go test -run '^$' -bench '^BenchmarkCompiledRecognitionSerialization$' -benchmem ./internal/engine`. The program below
measures your grammar with the current writer; results depend on grammar size.

The program (run it as `go run . grammar.zp 'input'` in a module that requires ZeroParse; the start rule is the grammar's
default start rule):

<details>
<summary>The measuring program</summary>

```go
package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/ornew/zeroparse"
)

func main() {
	path, input := os.Args[1], os.Args[2]
	src, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	p, err := zp.CompileSource(string(src), "")
	if err != nil {
		panic(err)
	}
	data, _ := p.MarshalBinary()
	fmt.Printf("source %d bytes, .zpc %d bytes\n", len(src), len(data))

	// Each run makes a parser ready and parses one input, so the setup that
	// happens on the first parse is included.
	measure := func(name string, prepare func() *zp.Parser) {
		r := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				if _, err := prepare().Parse(input); err != nil {
					b.Fatal(err)
				}
			}
		})
		fmt.Printf("%-20s %8d ns/op %8d B/op %6d allocs/op\n", name, r.NsPerOp(), r.AllocedBytesPerOp(), r.AllocsPerOp())
	}
	measure("compile from source", func() *zp.Parser {
		p, _ := zp.CompileSource(string(src), "")
		return p
	})
	measure("load .zpc", func() *zp.Parser {
		p, _ := zp.LoadParser(data)
		return p
	})
}
```

</details>

Loading the cel grammar takes a median of 76 µs from a parse-mode file, 48 µs from a recognize-mode file and 68 µs
from a document-mode file, against 3.8 ms to compile it from source (darwin/arm64, median of 200 loads and of 50
compilations, on a loaded machine). Whether this matters depends on how often the program starts. A long-running
service loads once and parses millions of times, so the difference is negligible. A command-line tool, a function that
starts for each request or a test that loads a large grammar in every process pays it every time.

## Embedding With `go:embed`

The usual pattern is to compile when the grammar changes, commit the `.zpc` next to the source, and embed it. The
layout of a module:

```text
example.com/pairs
├── go.mod
├── pairs.zp
├── pairs.zpc          # created by go generate, committed
├── generate.go
├── main.go
└── compiled_test.go
```

Record `zp` as a tool of the module (`go get -tool github.com/ornew/zeroparse/cmd/zp`), so that `go generate` runs the
pinned version, and declare the step:

```go
// generate.go
package main

//go:generate go tool zp compile -g pairs.zp -o pairs.zpc
```

Embed the file and load it once, when the program starts:

```go
// main.go
package main

import (
	_ "embed"
	"fmt"
	"log"

	"github.com/ornew/zeroparse"
)

//go:embed pairs.zpc
var compiled []byte

var parser = func() *zp.Parser {
	p, err := zp.LoadParser(compiled)
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
go generate ./...
go run .
```

```text
[Pair{Key: "abc", Value: "12"} Pair{Key: "あい", Value: "3"}]
```

`go:embed` needs the file to exist when the package is built, so commit the `.zpc`. Because a `.zpc` is binary,
tell your review tool, for example with `*.zpc binary` in `.gitattributes`, and expect its diff to say only that it
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

	"github.com/ornew/zeroparse"
)

// pairs.zpc is up to date with pairs.zp.
func TestCompiledIsUpToDate(t *testing.T) {
	src, err := os.ReadFile("pairs.zp")
	if err != nil {
		t.Fatal(err)
	}
	p, err := zp.CompileSource(string(src), "main")
	if err != nil {
		t.Fatal(err)
	}
	want, err := p.Marshal(zp.CompiledParse) // the mode of the go:generate line
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compiled, want) {
		t.Error("pairs.zpc is out of date; run go generate ./...")
	}
}
```

After changing `pairs.zp` (here: `rule value =  [0-9a-f]+ #atomic `) without running `go generate`, the test fails:

```text
--- FAIL: TestCompiledIsUpToDate (0.00s)
    compiled_test.go:26: pairs.zpc is out of date; run go generate ./...
FAIL
FAIL	example.com/pairs	0.174s
FAIL
```

The same test also catches an upgrade of ZeroParse that changes the bytes of an unchanged grammar (see below). In CI,
`go generate ./... && git diff --exit-code` does the same without a test.

A file does not have to be embedded. A program can read a `.zpc` from disk or download it and call `LoadParser`, so
that a grammar changes without rebuilding the program. Then [only load files you trust](#loading-untrusted-data), and
handle the error of `LoadParser`: it is the only place a bad file is noticed.

## Versions and Compatibility

A file carries two version numbers after its magic number:

| Number | Now | Meaning |
|:--|--:|:--|
| Format version | 7 | The layout of the file. A runtime loads version 7 only. |
| Instruction-set version | 29 | The bytecode of the value model (see the [specification](../../spec/runtime/compiled-grammars.md#versions-and-compatibility)). Every module declares it. |

The consequences:

- **A file with another instruction-set version must be compiled again.** ZeroParse refuses a file of an older instruction
  set with `unsupported instruction set version N: the file was compiled by an older zp; compile the grammar again`.
- **ZeroParse refuses a file of another format version or a newer instruction set.** It reports
  `unsupported compiled grammar version N (want 7): compile the grammar again` or `unsupported instruction set version N`.
  A runtime with a narrower implementation `int` rejects repetition bounds outside its range.
- **A file keeps its source information and default-value flag.** A grammar compiled with source information or with default values loads with them.
- **The same grammar can produce different bytes in a new release**, when the compiler improves. Rebuild the file when
  you upgrade ZeroParse, as you would regenerate [generated code](code-generation.md#keeping-generated-code-up-to-date).
  The up-to-date test above fails after such an upgrade until you do.
- **Build and load with the same release** when you can. The tests check that a loaded file behaves like the grammar it
  was made from for the release that wrote it; files from other releases are loaded on the strength of the version
  numbers alone.

Saved files retain the left-recursion classification chosen by the compiler that wrote them. Loading a file does not rerun that analysis.

The format is specified in [bytecode.md](../../spec/bytecode/README.md#file-format) and its design in
[design record 009](../design/records/009-compiled-grammar-format.md).

## Loading Untrusted Data

Loading rejects recovery regions that enclose a streaming commitment, such as `#recover` on the `#stream`
repetition. Move recovery into the repeated element and recompile the grammar. Element-local recovery is supported in every
mode.

Loading checks the magic number, the format version, a CRC-32 checksum, the mode, the instruction-set version and the
structure (every index is in range, every jump goes forward except the one that closes a repetition and jumps back, the
start rule is a start rule of the module, no trailing bytes). It does **not** type-check the grammar again, and it does not
verify that the bytecode uses the VM's stacks correctly. A file made to contain type errors can make actions fail while
parsing, and made-up bytecode is not guaranteed to terminate. The nesting limit relies on the recursion marks stored in the compiled grammar, so a forged file can remove it; load only files you trust.

Only load files that you or your build produced: the output of `zp compile`, `MarshalBinary` or `Marshal`. The
checksum is not a signature: it detects accidents, not tampering. If a file travels over a network, sign it or compare
a hash that you obtained another way. If users bring their own grammars, accept source, compile it with
`CompileSource`, which type-checks, and (if needed) save the result yourself.

## Compiled Grammar, Generated Code or Source?

Three ways to get a grammar into a Go program:

| | Compile from source at start-up | Compiled grammar (`.zpc`) | Generated code (`zp gen`) |
|:--|:--|:--|:--|
| What the program holds | The `.zp` text | A binary file, embedded or read | Go source, compiled in |
| Start-up | Compiles: tens of microseconds to tens of milliseconds, by size | Loads: a fraction of that (see above) | None |
| Parsing speed | Closure backend | Bytecode backends | Generated Go takes 0.44–0.61× closure time in CodePoints and 0.50–0.66× in Bytes across current workloads ([benchmarks](../benchmarks.md)) |
| Depends on ZeroParse at run time | Yes | Yes (the engine and `LoadParser`) | No: standard library only |
| Change the grammar without rebuilding the program | Yes (read the file) | Yes (read the file) | No |
| Streaming, incremental parsing, backend choice, recognition mode | Yes | Yes, on the bytecode backends, each in the mode that serves it | No |
| Errors in the grammar are found | At start-up | When you build the file | When you generate |
| Typical use | Development, tools, tests, grammars that users supply | A fixed grammar in a program with short runs or many processes; grammars that ship as data | A fixed grammar on a hot path, a small binary, no dependency on ZeroParse |

Rules of thumb:

- **Use source** while the grammar changes, and whenever the cost of compiling is not noticeable: the grammar is small,
  or the process is long-lived. It is the simplest.
- **Use a `.zpc`** when start-up is noticeable (a command-line tool, a plugin, a serverless function, a test binary),
  when the grammar has to be a data file, or when you want to keep ZeroParse's features that generated code lacks
  ([streaming and incremental parsing](streaming.md), [backends](backends.md)) and still skip the compile.
- **Use `zp gen`** when the parser is on a hot path or you want no dependency on ZeroParse at run time: it has no
  start-up and the fastest parsing. The cost is that the grammar is frozen into the binary, and that the generated
  source is large: the `parser.go` of `parsers/python` and of `parsers/duckdb` is dozens to hundreds of times the size of
  the `.zp` file it comes from.

The three give the same trees and the same errors, so you can start with source, move to a file when start-up shows up
in a profile, and to generated code when parsing speed does.

## Troubleshooting

| Message | Cause |
|:--|:--|
| `not a compiled ZeroParse grammar` | The data does not start with the magic number: it is a source file, JSON, or something else. |
| `unsupported compiled grammar version N (want 7): compile the grammar again` | The file was written by a different format version. Rebuild it with your release. |
| `unsupported instruction set version N` | The file requires an unsupported or newer instruction set. Upgrade ZeroParse or rebuild the file. |
| `invalid compiled grammar: checksum mismatch` | The file is truncated or changed (a bad download, an editor that converted line endings, a merge). |
| `invalid compiled grammar: ...` (other) | The structure is wrong, though the checksum matches. Rebuild the file. |
| `the compiled grammar is in parse mode; documents need document mode: ...` | The file's mode does not serve the operation. Compile the grammar with the `-mode` the message names. |
| `the compiled grammar is in M mode; traced or profiled recognition needs the grammar source: ...` | `RecognizeOnly` with `WithTrace` or `WithProfile`. Compile the grammar from source. |
| `the closure backend needs the grammar source; a compiled grammar runs on the bytecode backends` | You asked a loaded parser for `Closure`. Use a bytecode backend, or compile from source. |
| `output schema unavailable: a compiled grammar holds no schema; export the schema from the grammar source` | `OutputSchema` on a loaded parser. Export the schema from the source. The error matches `ErrSchemaUnavailable`. |
| `...: a compiled grammar holds only the bytecode of one mode; gen needs the grammar source (.zp or .json)` | `zp convert`, `zp gen`, `zp lint`, `zp sample` or `zp schema` on a `.zpc`. Use the source. |
| `the compiled grammar is in M mode and is saved again only in that mode: ...` | `zp compile` of a `.zpc` with another `-mode`. Pass the file's own mode, or compile from the source. |

The errors of a damaged file, as `LoadParser` reports them (from a program that changes a copy of `pairs.zpc`):

```text
intact                 <nil>
format version 3       unsupported compiled grammar version 3 (want 7): compile the grammar again
one byte changed       invalid compiled grammar: checksum mismatch
truncated              invalid compiled grammar: checksum mismatch
ZeroParse source            not a compiled ZeroParse grammar
```

## See Also

- [Running parsers](running-parsers.md): the Go API, backends, position units and the `zp` command.
- [Code generation](code-generation.md): the other way to skip compiling at run time.
- [Benchmarks](../benchmarks.md#preparation-time-until-a-grammar-is-ready-to-use) and
  [performance notes](../performance.md#where-zeroparse-stands): preparation times and sizes.
- [bytecode.md](../../spec/bytecode/README.md#file-format): the file format, and [design record 009](../design/records/009-compiled-grammar-format.md).
