# Testing Grammars

A grammar is a program, and it goes wrong in the ways programs do: it accepts input it should reject, rejects input it
should accept, builds a tree that is not the one you meant, reports an error at the wrong place, or changes behavior
after an edit that looked harmless. This guide builds a test suite for a grammar in layers, each catching a different
kind of mistake, from a table of examples to fuzzing, and shows how to check that the other forms of a grammar (another
backend, a compiled file, generated code, an incremental reparse) behave like the one you tested.

| Layer | Catches | Effort |
|:--|:--|:--|
| [Examples](#examples-what-is-accepted-and-rejected) | The language you meant: accepted, rejected, and where the error is | A few lines per case |
| [Golden files](#golden-files) | Any change to a tree or an error message, in whole documents | One file per input; `-update` rewrites them |
| [Properties](#properties-that-hold-for-every-input) | Panics, wrong positions, backends and recognition that disagree | Written once, runs on every input |
| [Generated inputs](#generated-inputs) | Parts of the grammar that no example reaches, and the error paths | A few lines, with `package sample` |
| [Fuzzing](#fuzzing) | Inputs nobody thought of | The same property function |
| [Reference implementations](#reference-implementations) | Differences from another parser of the same language | A comparison function |
| [Other forms of the grammar](#other-forms-of-the-grammar) | A compiled file, generated code or an incremental parse that differs from the engine | One comparison per form |
| [The grammar itself](#the-grammar-itself) | Mistakes in the grammar, formatting, stale generated files | Commands in CI |

- [The grammar under test](#the-grammar-under-test)
- [Examples: what is accepted and rejected](#examples-what-is-accepted-and-rejected)
- [Golden files](#golden-files)
- [Properties that hold for every input](#properties-that-hold-for-every-input)
- [Generated inputs](#generated-inputs)
- [Fuzzing](#fuzzing)
- [Reference implementations](#reference-implementations)
- [Other forms of the grammar](#other-forms-of-the-grammar)
- [The grammar itself](#the-grammar-itself)
- [In CI](#in-ci)
- [How the repository tests its grammars](#how-the-repository-tests-its-grammars)

## The grammar under test

The examples use `pairs.pego` of the [runtime guide](runtime.md), which reads `key=value` pairs separated by optional
semicolons and whitespace:

```pego
type Pair struct { Key Match, Value Match }

def main = ws ps:pair+ $$ -> $ps
def pair: Pair = k:key "=" v:value ";"? ws -> new Pair{Key: $k, Value: $v}
def key = @(?a-zあ-ん)+
def value = @(?0-9)+
def ws = (? \t\n)*
```

The tests live in a Go module next to it. Each file below is part of the package `pairs_test`, and the grammar is
embedded and compiled once, because a `*pego.Parser` is safe for concurrent use:

```
example.com/pairs
├── go.mod
├── pairs.pego
├── pairs_test.go
├── golden_test.go
├── properties_test.go
├── sample_test.go
├── document_test.go
├── compiled_test.go
└── testdata/
    ├── basic.txt        basic.golden
    ├── blank-lines.txt  blank-lines.golden
    └── error.txt        error.golden
```

## Examples: what is accepted and rejected

Start with a table of inputs and what they should produce. For the inputs that must be accepted, compare the tree (its
`String()` form is compact and has no positions); for those that must be rejected, check where the error is, and what
was expected there when the message matters:

```go
package pairs_test

import (
	_ "embed"
	"errors"
	"slices"
	"testing"

	"github.com/ornew/pego"
)

//go:embed pairs.pego
var grammarSrc string

// parser is shared by every test: a *pego.Parser is safe for concurrent use.
var parser = func() *pego.Parser {
	p, err := pego.CompileSource(grammarSrc, "main")
	if err != nil {
		panic(err)
	}
	return p
}()

func TestAccepts(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a=1", `[(Pair Key="a"@key Value="1"@value)]`},
		{"abc=12; あい=3", `[(Pair Key="abc"@key Value="12"@value) (Pair Key="あい"@key Value="3"@value)]`},
		{" \n a=1 \n b=2", `[(Pair Key="a"@key Value="1"@value) (Pair Key="b"@key Value="2"@value)]`},
	} {
		node, err := parser.Parse(tc.in)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.in, err)
			continue
		}
		if got := node.String(); got != tc.want {
			t.Errorf("%q:\n got %s\nwant %s", tc.in, got, tc.want)
		}
	}
}

func TestRejects(t *testing.T) {
	for _, tc := range []struct {
		in        string
		line, col int
		expected  []string
	}{
		{"", 1, 1, []string{"(? \\t\\n)", "(?a-zあ-ん)"}},
		{"a", 1, 2, []string{`"="`, "(?a-zあ-ん)"}},
		{"a=", 1, 3, []string{"(?0-9)"}},
		{"a=1\nb=x", 2, 3, []string{"(?0-9)"}},
	} {
		_, err := parser.Parse(tc.in)
		var se *pego.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: got %v, want a syntax error", tc.in, err)
			continue
		}
		if se.Line != tc.line || se.Col != tc.col {
			t.Errorf("%q: error at %d:%d, want %d:%d", tc.in, se.Line, se.Col, tc.line, tc.col)
		}
		if !slices.Equal(se.Expected, tc.expected) {
			t.Errorf("%q: expected %q, want %q", tc.in, se.Expected, tc.expected)
		}
	}
}
```

Some advice on choosing the cases:

- **Every alternative and every repetition's boundary**: zero, one and several elements, and the first and last
  characters a class allows (`a`, `z`, and the character before and after them).
- **What must not match.** Test the things a lookahead or an ordered choice is there to exclude, such as a keyword that
  must not be read as an identifier.
- **Errors.** `SyntaxError` has `Line`, `Col`, `Pos` and `Expected`; the [errors guide](errors-and-recovery.md) shows how
  to shape them. Asserting on `Expected` ties the test to the grammar's details (the first case above includes the
  whitespace class, because `ws` comes first), so do it for the errors you took care to make good, and check only the
  position for the rest.
- **Positions.** A tree test that ignores `Start` and `End` misses a whole class of mistakes; see
  [Properties](#properties-that-hold-for-every-input) and use `encoding/json` of a node (it includes positions) where
  they matter.

When one of these fails, the output names the input:

```
--- FAIL: TestAccepts (0.00s)
    pairs_test.go:32: "abc=12; あい=3": unexpected error: 1:9: syntax error: expected (? \t\n), (?a-z), end of input
```

(That was after narrowing the grammar's `key` rule to `@(?a-z)+`.)

## Golden files

Examples of one line do not scale to whole documents. A golden test parses each file of `testdata/` and compares the
result with a file next to it: the tree, or the error. When the result changes on purpose, you rewrite the files with a
flag and review the diff in version control, as you would any other change:

```go
package pairs_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// TestGolden parses every testdata/*.txt and compares the tree (or the error) with testdata/*.golden.
func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/*.txt")
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no inputs in testdata: %v", err)
	}
	for _, path := range inputs {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".txt"), func(t *testing.T) {
			in, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if node, err := parser.Parse(string(in)); err != nil {
				got = "error: " + err.Error() + "\n"
			} else {
				got = node.String() + "\n"
			}
			golden := strings.TrimSuffix(path, ".txt") + ".golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("%s differs from %s (go test -update rewrites it; review the diff)\n got: %swant: %s", path, golden, got, want)
			}
		})
	}
}
```

```bash
$ go test -run TestGolden -update .   # create or rewrite the golden files
$ cat testdata/basic.golden testdata/error.golden
[(Pair Key="abc"@key Value="12"@value) (Pair Key="あい"@key Value="3"@value)]
error: 2:3: syntax error: expected (?0-9)
```

A change that matters shows up as a failure, here after narrowing the `key` rule to `@(?a-z)+`:

```
--- FAIL: TestGolden (0.00s)
    --- FAIL: TestGolden/basic (0.00s)
        golden_test.go:43: testdata/basic.txt differs from testdata/basic.golden (go test -update rewrites it; review the diff)
             got: error: 1:9: syntax error: expected (? \t\n), (?a-z), end of input
            want: [(Pair Key="abc"@key Value="12"@value) (Pair Key="あい"@key Value="3"@value)]
```

Notes:

- Golden tests are only as good as the review of `-update`. Read the diff: a golden file that is regenerated without
  looking proves nothing.
- Print what you care about. `node.String()` has no positions; `json.MarshalIndent(node, "", "  ")` has them (and the
  rule of each node), at the price of larger files. A grammar with `#recover` should also print the recovered errors
  (`var recovered pego.SyntaxErrors; errors.As(err, &recovered)`): a parse can return a tree and an error together.
- Keep a few inputs that **fail**, with the error in the golden file. Error messages are part of a parser's interface.
- The same layout is used by [examples/](../../examples/) and by every module of [parsers/](../../parsers/); the pego
  command makes a golden file too: `pego parse -g pairs.pego -f sexpr < testdata/basic.txt`.

## Properties that hold for every input

Some things should be true of every input, whether or not it is in your tests. Write them once, as a function that
takes an input and fails the test if one of them does not hold, and call it from every layer below:

```go
package pairs_test

import (
	"fmt"
	"testing"

	"github.com/ornew/pego"
)

// checkInput checks what must hold for every input, valid or not.
func checkInput(t testing.TB, in string) {
	t.Helper()

	// Byte positions, so that a node's text is input[Start:End].
	node, err := parser.Parse(in, pego.WithUnit(pego.Bytes))

	// Every backend returns the same tree and the same error.
	for _, b := range []pego.Backend{pego.Bytecode, pego.BytecodeIterative} {
		other, oerr := parser.Parse(in, pego.WithUnit(pego.Bytes), pego.WithBackend(b))
		if fmt.Sprint(node, err) != fmt.Sprint(other, oerr) {
			t.Fatalf("%q: backend %v returned %v, %v; the closure backend %v, %v", in, b, other, oerr, node, err)
		}
	}

	// Recognition agrees with parsing.
	if _, rerr := parser.Parse(in, pego.RecognizeOnly(), pego.WithUnit(pego.Bytes)); fmt.Sprint(rerr) != fmt.Sprint(err) {
		t.Fatalf("%q: Parse returned %v, RecognizeOnly %v", in, err, rerr)
	}

	if err != nil {
		// A syntax error points into the input.
		if se, ok := err.(*pego.SyntaxError); ok && (se.Pos < 0 || se.Pos > len(in)) {
			t.Fatalf("%q: error position %d is outside the input", in, se.Pos)
		}
		return
	}
	checkNode(t, in, node, 0, len(in))
}

// checkNode checks that a node lies within its parent and that a terminal's text is the input it covers.
func checkNode(t testing.TB, in string, n *pego.Node, lo, hi int) {
	t.Helper()
	if n == nil {
		return
	}
	if int(n.Start) < lo || n.Start > n.End || int(n.End) > hi {
		t.Fatalf("%q: node %s covers [%d,%d), outside [%d,%d)", in, n, n.Start, n.End, lo, hi)
	}
	if n.IsTerminal() && n.Text != in[n.Start:n.End] {
		t.Fatalf("%q: terminal %s has text %q, but [%d,%d) is %q", in, n, n.Text, n.Start, n.End, in[n.Start:n.End])
	}
	for _, c := range n.Children {
		checkNode(t, in, c, int(n.Start), int(n.End))
	}
	for _, f := range n.Fields {
		if c, ok := f.Value.(*pego.Node); ok {
			checkNode(t, in, c, int(n.Start), int(n.End))
		}
	}
}

func TestProperties(t *testing.T) {
	for _, in := range []string{"", "a", "a=", "a=1", "abc=12; あい=3", "a=1\nb=x", "a=1 b=2 c", "あ=1", "=1"} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) { checkInput(t, in) })
	}
}
```

Each check came from a kind of bug:

| Check | Catches |
|:--|:--|
| The parse returns, with a tree or an error | A panic or an endless loop, reported by the test run |
| All backends return the same tree and error | A bug in one of the three engines, or an action that depends on something other than its inputs (the three are specified to agree: see the [runtime guide](runtime.md#backends)) |
| `RecognizeOnly` agrees with `Parse` | Recognition and a full parse that diverge: a bug in one of them, or an action that fails at run time (recognition does not run actions) |
| The error position lies inside the input | Off-by-one positions of errors, most often at the end of input |
| Every node lies inside its parent, and a terminal's `Text` is the input at its position | Wrong `Start` and `End`, such as a rule that consumes trailing space but reports a range without it |

Add the properties of your own grammar. Typical ones: formatting the tree back to text and parsing it again gives the
same tree; a tree's children come in order of position; every identifier of the tree is a word, not a keyword.

`RecognizeOnly` does not run actions. If an action can fail at run time (`division by zero`), `Parse` fails and
recognition succeeds, and the check above fails for the right reason. For such a grammar, compare only the syntax
errors, or skip inputs that fail with an error that is not a `*pego.SyntaxError`.

## Generated inputs

Examples written by hand miss what you did not think of. `package sample` generates inputs the grammar accepts, and
near misses that it rejects ([sampling and fuzzing](sampling-and-fuzzing.md) covers the generator in detail). With
`WithCoverage` it also tells which rules and alternatives its inputs never used, which makes it a measure of your test
suite as well: if a generated corpus misses an alternative, so will most of your tests.

```go
package pairs_test

import (
	"testing"

	"github.com/ornew/pego/sample"
)

// TestGeneratedInputs runs the checks on inputs generated from the grammar, and requires that they exercise
// every rule and alternative of it.
func TestGeneratedInputs(t *testing.T) {
	g, err := sample.New(parser, sample.WithSeed(1), sample.WithCoverage())
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := g.Generate(100)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		if _, err := parser.Parse(in); err != nil {
			t.Errorf("generated input %q does not parse: %v", in, err)
		}
		checkInput(t, in)
	}
	if c := g.Coverage(); len(c.MissedRules) > 0 || len(c.MissedAlternatives) > 0 {
		t.Errorf("the generated inputs miss part of the grammar:\n%s", c.Report())
	}

	// Near misses: inputs the grammar rejects, for the error paths.
	invalid, err := g.GenerateInvalid(100)
	if err != nil {
		t.Fatal(err)
	}
	for _, inv := range invalid {
		checkInput(t, inv.Input)
	}
}
```

The test fails with the report of what was missed when the inputs do not reach every rule and alternative, so a new
alternative you add to the grammar must be exercised by the generator (it nearly always is; if not, the
[sampling guide](sampling-and-fuzzing.md#how-it-works-and-what-it-cannot-do) says why). Keep the seed fixed, so that the
test is deterministic, and try other seeds from time to time.

On the command line:

```bash
$ pego sample -g pairs.pego -n 3 -seed 1 -coverage
"e=7qi=8542"
"びv=1500;\t\t"
"fsm=1"
rules 5/5 (100%), alternatives 0/0 (100%)
$ pego sample -g pairs.pego -n 2 -seed 1 -invalid
"h;"
"=208;\n\t"
```

The first command prints inputs the grammar accepts and the coverage on the last line; the second, with `-invalid`,
prints near misses. (Alternatives count only choices and Pratt expressions; `pairs.pego` has none, so the report counts
rules.)

## Fuzzing

Go's fuzzer mutates the inputs of a seed corpus and keeps those that reach new code. Seeded with generated inputs, it
starts from realistic documents instead of random bytes. Run the property function on every input it produces:

```go
func FuzzParse(f *testing.F) {
	sample.Seed(f, parser, 20, sample.WithMaxLen(64)) // adds up to 20 generated inputs to the corpus
	f.Add("a=")
	f.Add("あ=1")
	f.Fuzz(func(t *testing.T, in string) { checkInput(t, in) })
}
```

`go test` runs the fuzz target on its seeds only, as an ordinary test. To fuzz, name it:

```bash
$ go test -run '^$' -fuzz=FuzzParse -fuzztime=3s -parallel=1 .
fuzz: elapsed: 0s, gathering baseline coverage: 0/22 completed
fuzz: elapsed: 0s, gathering baseline coverage: 22/22 completed, now fuzzing with 1 workers
fuzz: elapsed: 3s, execs: 27275 (9089/sec), new interesting: 28 (total: 50)
fuzz: elapsed: 4s, execs: 27275 (0/sec), new interesting: 28 (total: 50)
PASS
```

When the fuzzer finds an input that fails, it saves it in `testdata/fuzz/FuzzParse/` and every later `go test` runs it:
commit the file, as a regression test. The fuzzer works best with short inputs: bound the seeds with `WithMaxLen`.

## Reference implementations

If another parser of the language exists (the standard library's, or the language's own), the strongest test is to
compare the two on the same inputs: accepted or not, and what the result says. Feed both the generated inputs, the
near misses and the fuzzer's inputs. A test of this kind finds mistakes that no hand-written example does, because the
reference is right where you misread the specification. The [ready-made parsers](../../parsers/) are checked this way
against `encoding/json` (JSON), `encoding/csv`, `encoding/xml`, `go/parser`, CPython's `ast`, the TypeScript compiler,
cel-go and `cue/parser`, and against conformance suites ([parsers/README.md](../../parsers/README.md)). The shape of the
test is the one of the [sampling guide](sampling-and-fuzzing.md#from-go):

```go
for _, in := range inputs {
	if !json.Valid([]byte(in)) {
		t.Errorf("encoding/json rejects %q", in)
	}
}
for _, inv := range invalid {
	if json.Valid([]byte(inv.Input)) {
		t.Errorf("encoding/json accepts %q (%s of %q)", inv.Input, inv.Mutation, inv.Base)
	}
}
```

When the reference and your grammar disagree, decide which is right before changing either: the difference is either a
bug in the grammar or a leniency of the reference, and a documented leniency is a result of the test too.

## Other forms of the grammar

The engine is the reference for the other forms of a grammar, and each has a test that says so. Run them on the same
corpus: the generated inputs, the golden inputs and what the fuzzer found.

**Incremental parsing.** After any edit, `Document.Parse` must return what `Parse` returns on the new text. Edit
generated inputs at random and compare ([incremental parsing](incremental.md)):

```go
package pairs_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/ornew/pego/sample"
)

// TestDocumentMatchesFreshParse edits generated inputs at random and checks that reparsing the document gives the
// result of parsing the new text from scratch.
func TestDocumentMatchesFreshParse(t *testing.T) {
	inputs, err := sample.Generate(parser, 50, sample.WithSeed(1))
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for _, in := range inputs {
		doc, err := parser.NewDocument(in)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := doc.Parse(); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		for range 20 {
			runes := []rune(doc.Text())
			start := rng.IntN(len(runes) + 1)
			end := start + rng.IntN(min(3, len(runes)-start)+1)
			insert := []string{"", "=", "1", ";", " ", "a", "あ"}[rng.IntN(7)]
			if err := doc.Edit(start, end, insert); err != nil {
				t.Fatal(err)
			}
			got, gerr := doc.Parse()
			want, werr := parser.Parse(doc.Text())
			if fmt.Sprint(got, gerr) != fmt.Sprint(want, werr) {
				t.Fatalf("after editing to %q: document %v, %v; fresh parse %v, %v", doc.Text(), got, gerr, want, werr)
			}
		}
	}
}
```

**A compiled grammar** must behave like the grammar it was made from ([compiled grammars](compiled-grammars.md)). This
test uses the form without the AST, which runs on the bytecode backend:

```go
package pairs_test

import (
	"fmt"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

// TestLoadedMatchesCompiled checks that a grammar saved and loaded again behaves like the one compiled from source.
func TestLoadedMatchesCompiled(t *testing.T) {
	data, err := parser.Marshal(pego.WithoutAST())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := pego.LoadParser(data)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := sample.Generate(parser, 100, sample.WithSeed(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range append(inputs, "", "a=", "a=1 b") {
		want, werr := parser.Parse(in)
		got, gerr := loaded.Parse(in)
		if fmt.Sprint(got, gerr) != fmt.Sprint(want, werr) {
			t.Errorf("%q: loaded %v, %v; compiled %v, %v", in, got, gerr, want, werr)
		}
	}
}
```

**Generated code** ([code generation](code-generation.md#keeping-generated-code-up-to-date)) has two tests: one that
the committed file is up to date, and one that it matches the engine on the inputs above.

**Streaming** ([streaming](streaming.md)): when a stream parse succeeds, the elements delivered to `ParseStream` must be
those of the repetition in the tree that `Parse` returns for the same input. Copy the nodes you keep with
`Node.Clone`.

## The grammar itself

Check the grammar as a file, too:

- **`pego lint`** finds alternatives that can never match, contradictory anchors, nullable repetitions and unused rules
  ([linting grammars](linting.md)). `pego.Lint` makes it a test: see the example in that guide.
- **`pego fmt -l`** lists the files whose formatting differs from the canonical one. It exits with status 0 even when it
  lists files, so a CI step has to test the output:

  ```bash
  test -z "$(pego fmt -l *.pego)" || { echo "run pego fmt -w"; exit 1; }
  ```

- **Generated files** (`pego gen`, `pego compile`) are checked for staleness by a test that regenerates in memory and
  compares: [code generation](code-generation.md#keeping-generated-code-up-to-date) and
  [compiled grammars](compiled-grammars.md#embedding-with-goembed).

## In CI

```bash
go vet ./...
go test ./...                                             # examples, golden files, properties, generated inputs
go test -run '^$' -fuzz=FuzzParse -fuzztime=30s .         # a short fuzz run; the corpus of failures is kept in testdata/fuzz
pego lint -g pairs.pego                                   # exits with status 1 on an error finding (-strict: also on warnings)
test -z "$(pego fmt -l *.pego)"
```

Fuzz for longer on a schedule rather than on every commit.

## How the repository tests its grammars

Everything above is used in the PEGO repository itself, so there are examples of each to read:

| What | Where |
|:--|:--|
| Golden files for every example grammar, with `-update` | [examples/examples_test.go](../../examples/examples_test.go) |
| Golden files for each ready-made parser, run on every backend of the engine and on the generated parser | [parsers/parsers_test.go](../../parsers/parsers_test.go) |
| Tests against reference implementations and conformance suites, with fuzz targets | `*_test.go` of each module in [parsers/](../../parsers/), for example [parsers/json/json_test.go](../../parsers/json/json_test.go) |
| Fuzz target seeded with generated inputs, comparing the three backends | [sample/example_test.go](../../sample/example_test.go) |
| Backends, saved and loaded grammars and documents compared with a fresh parse, on every grammar | `internal/engine/` tests, for example `compiled_test.go` and `document_test.go` |

## See also

- [Sampling inputs and fuzzing](sampling-and-fuzzing.md): the generator, coverage and near-miss inputs in detail.
- [Errors and recovery](errors-and-recovery.md): shaping the errors that these tests check.
- [Linting grammars](linting.md), [code generation](code-generation.md) and
  [compiled grammars](compiled-grammars.md): keeping the other artifacts honest.
- [Debugging and profiling](debugging.md): when a test fails and you need to know why (`pego explain`, `pego trace`).
