# Ship a parser and keep it up to date

**Problem.** You wrote a grammar and want your program, or your library, to use it without making users depend on PEGO,
and without a stale parser after somebody changes the grammar.

Generate Go code from the grammar with `pego gen`, commit it, regenerate it with `go generate`, and let a test fail
when the committed file does not match the grammar. The generated parser imports only the standard library.

The grammar reads versions such as `1.2.3` and `1.2.3-rc.1`:

```pego
// version.pego
type Num terminal
type Ident terminal
type Version struct { Major Num, Minor Num, Patch Num, Pre *Ident }

def main: Version = v:version $$ -> $v

def version: Version = major:num "." minor:num "." patch:num pre:pre?
    -> new Version{Major: $major, Minor: $minor, Patch: $patch, Pre: $pre}
def pre: Ident = -"-" i:ident -> $i

def num: Num = "0" / (?1-9) (?0-9)*
def ident: Ident = (?a-z0-9.)+
```

The layout is a module with the grammar, the code that uses the parser, and the generated parser in a package of its
own:

```text
example.com/ship
├── go.mod
├── version.pego
├── main.go
├── generated_test.go
└── versionparser/       created by go generate
    └── parser.go
```

Record `pego` as a tool of the module, so that `go generate` runs the version you pinned and nobody installs anything:

```bash
go mod init example.com/ship
go get -tool github.com/ornew/pego/cmd/pego
mkdir versionparser
```

Then the program, with the `go:generate` line that makes the parser (`-types` adds Go types and `ParseAST`):

```go
// main.go
package main

import (
	"fmt"
	"os"

	"example.com/ship/versionparser"
)

//go:generate go tool pego gen -g version.pego -pkg versionparser -types -o versionparser/parser.go

func main() {
	v, err := versionparser.ParseAST(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("major %s, minor %s, patch %s", v.Major.Text, v.Minor.Text, v.Patch.Text)
	if v.Pre != nil {
		fmt.Printf(", pre-release %s", v.Pre.Text)
	}
	fmt.Println()
}
```

```bash
go generate ./...
go run . 1.2.3-rc.1
go run . 1.2.3
go run . 01.2.3
```

```text
major 1, minor 2, patch 3, pre-release rc.1
major 1, minor 2, patch 3
1:2: syntax error: expected "."
```

A test keeps the generated file honest. It generates the code again in memory, with the options of the `go:generate`
line, and compares it with the file:

```go
// generated_test.go
package main

import (
	_ "embed"
	"os"
	"testing"

	"github.com/ornew/pego"
)

//go:embed version.pego
var grammar string

// versionparser/parser.go is what pego generates from version.pego. The options must be those of the go:generate line.
func TestGeneratedIsUpToDate(t *testing.T) {
	g, err := pego.ParseGrammar(grammar)
	if err != nil {
		t.Fatal(err)
	}
	want, err := pego.GenerateGo(g, "versionparser", "main", pego.WithTypes())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("versionparser/parser.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("versionparser/parser.go is out of date: run go generate ./...")
	}
}
```

If somebody changes the grammar (here, the characters of `ident`) and does not run `go generate`:

```text
--- FAIL: TestGeneratedIsUpToDate (0.01s)
    generated_test.go:29: versionparser/parser.go is out of date: run go generate ./...
FAIL
FAIL	example.com/ship	0.652s
FAIL
```

## How it works

- **No dependency at run time.** `go list -deps .` for the program lists no package of PEGO: none is linked into it.
  `go.mod` still requires PEGO, for the tool and for the test.
- **The generator is deterministic.** The same grammar and options give the same file, which is what makes the
  comparison in the test, and a review of a diff of `parser.go`, meaningful.
- **Regenerate after upgrading PEGO.** The runtime in the generated file comes from the version of `pego` that wrote it.
  Without the test, a new version of the tool leaves the old runtime in place until somebody runs `go generate`; with it,
  the test fails. Pinning the tool with `go get -tool` keeps every developer and CI on the same version.
- **Without the test**, `go generate ./... && git diff --exit-code` in CI says the same.
- **The same tree.** The generated `Parse` returns the same tree and errors as the engine for the same grammar, so you
  can develop with the engine and the `pego` tools (`pego parse`, `pego trace`) and ship the generated code.

## When the grammar must stay data

If you want the grammar to be a file you load, not code (users supply it, or it changes without a rebuild), compile it
once to a `.pegoc` file and embed or ship that, which skips parsing and checking the grammar at start-up:

```bash
go tool pego compile -g version.pego -o version.pegoc
```

```go
//go:embed version.pegoc
var compiled []byte

p, err := pego.LoadParser(compiled) // then p.Parse(...) as with CompileSource
```

Loading is faster than compiling, and fastest without the grammar's syntax tree (`-no-ast`, which allows only the
bytecode backends). For the Python grammar of [parsers/python](../../parsers/python/) (85 KB of source), on an Apple M3
Max with Go 1.27.1:

| Preparing the parser | Time |
|:--|--:|
| `pego.CompileSource` | 19 ms |
| `pego.LoadParser` of the `.pegoc` | 8 ms |
| `pego.LoadParser` of the `.pegoc` made with `-no-ast` | 1.0 ms |

A generated parser needs no preparation at all, and is also the fastest at parsing.

## Variations

- **Several grammars**: one `go:generate` line and one package each; [parsers/generate.go](../../parsers/generate.go) does
  this for the ready-made parsers.
- **A recognizer**: add `-recognize` for `Recognize`, which only checks, as in [Parse log lines fast](log-lines.md).
- **TypeScript**: `pego gen -lang ts` generates a module with no dependencies; see
  [TypeScript parsers](../guide/typescript.md).
- **A parity test**: compare the generated parser with the engine on a list of inputs, to catch a difference after an
  upgrade (see the code generation guide).

See [Code generation](../guide/code-generation.md) for the generated API, what the generated parser does not support
(`#stream`, `Document`), and how typed values are produced.
