# Ship a Parser and Keep It Up to Date

**Problem.** You wrote a grammar and want your program, or your library, to use it without making users depend on ZeroParse,
and without a stale parser after somebody changes the grammar.

Generate Go code from the grammar with `zp gen`, commit it, regenerate it with `go generate`, and let a test fail
when the committed file does not match the grammar. The generated package imports only the standard library.

The grammar reads versions such as `1.2.3` and `1.2.3-rc.1`:

```zp
// version.zp
type Num = node[string]
type Ident = node[string]
type Version = { Major Num; Minor Num; Patch Num; Pre *Ident }

@start
rule main Version = v:version $$ => $v

rule version Version = (
    major:num "." minor:num "." patch:num pre:pre?
) => Version{Major: $major, Minor: $minor, Patch: $patch, Pre: $pre}
rule pre Ident = -"-" i:ident => $i

rule num Num = ("0" / [1-9] [0-9]*) #atomic
rule ident Ident = ([a-z0-9.]+) #atomic
```

The layout is a module with the grammar, the code that uses the parser, and the generated parser in a package of its
own:

```text
example.com/ship
├── go.mod
├── version.zp
├── main.go
├── generated_test.go
└── versionparser/       created by go generate
    ├── parser.go
    ├── api.go
    └── internal/runtime/
```

Record `zp` as a tool of the module, so that `go generate` runs the version you pinned and nobody installs anything:

```bash
go mod init example.com/ship
go get -tool github.com/ornew/zeroparse/cmd/zp
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

//go:generate go tool zp gen -g version.zp -module example.com/ship/versionparser -types -o versionparser

func main() {
	v, err := versionparser.ParseAST(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("major %s, minor %s, patch %s", v.Major.Value, v.Minor.Value, v.Patch.Value)
	if v.Pre != nil {
		fmt.Printf(", pre-release %s", v.Pre.Value)
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

A test keeps the generated files honest. It generates the package again in memory, with the options of the
`go:generate` line, and compares every file with the one on disk:

```go
// generated_test.go
package main

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/gen"
)

//go:embed version.zp
var grammar string

// The files of versionparser are what zp generates from version.zp. The module path and the options must be those
// of the go:generate line.
func TestGeneratedIsUpToDate(t *testing.T) {
	g, err := zp.ParseGrammar(grammar)
	if err != nil {
		t.Fatal(err)
	}
	want, err := gen.Go{Module: "example.com/ship/versionparser", Types: true}.Generate(g, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range want { // parser.go, api.go and the runtime, with paths relative to the package
		got, err := os.ReadFile(filepath.Join("versionparser", filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, f.Content) {
			t.Errorf("versionparser/%s is out of date: run go generate ./...", f.Path)
		}
	}
}
```

If somebody changes the grammar (here, the characters of `ident`) and does not run `go generate`:

```text
--- FAIL: TestGeneratedIsUpToDate (0.02s)
    generated_test.go:33: versionparser/parser.go is out of date: run go generate ./...
FAIL
FAIL	example.com/ship	0.652s
FAIL
```

## How It Works

- **No dependency at run time.** `go list -deps .` for the program lists no package of ZeroParse: none is linked into it.
  `go.mod` still requires ZeroParse, for the tool and for the test.
- **The generator is deterministic.** The same grammar and options give the same file, which is what makes the
  comparison in the test, and a review of a diff of `parser.go`, meaningful.
- **Regenerate after upgrading ZeroParse.** The copy of the runtime in `versionparser/internal/runtime` comes from the version of `zp` that wrote it.
  Without the test, a new version of the tool leaves the old runtime in place until somebody runs `go generate`; with it,
  the test fails. Pinning the tool with `go get -tool` keeps every developer and CI on the same version.
- **Without the test**, `go generate ./... && git diff --exit-code` in CI says the same.
- **The same tree.** The generated `Parse` returns the same tree and errors as the engine for the same grammar, so you
  can develop with the engine and the `zp` tools (`zp parse`, `zp trace`) and ship the generated code.

## When the Grammar Must Stay Data

If you want the grammar to be a file you load, not code (users supply it, or it changes without a rebuild), compile it
once to a `.zpc` file and embed or ship that, which skips parsing and checking the grammar at start-up:

```bash
go tool zp compile -g version.zp -o version.zpc
```

```go
//go:embed version.zpc
var compiled []byte

p, err := zp.LoadParser(compiled) // then p.Parse(...) as with CompileSource
```

The file serves `Parse` and `ParseStream` on the bytecode backends; compile it with `-mode recognize` for
`zp.RecognizeOnly()` or `-mode document` for documents and traces. Loading is much faster than compiling: for the
Python grammar of [parsers/python](../../parsers/python/), on an Apple M3 Max with Go 1.27.1, `zp.CompileSource` took
about 60 ms and `zp.LoadParser` of the `.zpc` under 1 ms in every mode.

A generated parser needs no preparation at all, and is also the fastest at parsing.

## Variations and Limits

- **Several grammars**: one `go:generate` line and one package each; [parsers/generate.go](../../parsers/generate.go) does
  this for the ready-made parsers.
- **A recognizer**: add `-recognize` for `Recognize`, which only checks, as in [Parse log lines fast](log-lines.md).
- **TypeScript**: `zp gen -lang ts` generates a module with no dependencies; see
  [TypeScript parsers](../guide/typescript.md).
- **A parity test**: compare the generated parser with the engine on a list of inputs, to catch a difference after an
  upgrade (see the code generation guide).

See [Code generation](../guide/code-generation.md) for the generated API, what the generated parser does not support
(`#stream`, `Document`), and how typed values are produced.
