# Test hand-written code against a grammar

**Problem.** You have a hand-written function that decides whether a string is valid (a version, an identifier, a
duration, an ID), and a specification of the format. You want to know that the two agree, including on the inputs
that your tests did not think of.

Write the specification as a grammar, and let PEGO generate inputs from it: inputs the grammar accepts, and inputs that
are one mutation away from one and that the grammar rejects. The function must say yes to the first and no to the
second. This is the grammar of [Ship a parser and keep it up to date](ship-a-parser.md), and the function is a regular
expression:

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

```go
// version.go
package version

import "regexp"

var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.]+)?$`)

// Valid reports whether s is a version such as 1.2.3 or 1.2.3-rc.1.
func Valid(s string) bool {
	return versionRe.MatchString(s)
}
```

The test generates 200 valid inputs and 200 near misses from the grammar. `sample.WithCoverage` makes the generator prefer
rules and alternatives that no input has used yet, and a fixed seed makes the test deterministic. `FuzzValid` is the
same check as a fuzz test, started from generated inputs.

```go
// version_test.go
package version

import (
	_ "embed"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

//go:embed version.pego
var grammar string

func compile(t testing.TB) *pego.Parser {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestValidMatchesGrammar checks Valid against the grammar, on inputs the grammar accepts and on inputs that are
// one mutation away from them.
func TestValidMatchesGrammar(t *testing.T) {
	g, err := sample.New(compile(t), sample.WithSeed(1), sample.WithCoverage())
	if err != nil {
		t.Fatal(err)
	}
	valid, err := g.Generate(200)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range valid {
		if !Valid(in) {
			t.Errorf("Valid(%q) = false; the grammar accepts it", in)
		}
	}
	invalid, err := g.GenerateInvalid(200)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range invalid {
		if Valid(in.Input) {
			t.Errorf("Valid(%q) = true; the grammar rejects it (%s of %q: %v)", in.Input, in.Mutation, in.Base, in.Err)
		}
	}
}

// FuzzValid starts from generated inputs, so that the fuzzer gets past the first character.
func FuzzValid(f *testing.F) {
	p := compile(f)
	sample.Seed(f, p, 20)
	f.Fuzz(func(t *testing.T, in string) {
		_, err := p.Parse(in)
		if got, want := Valid(in), err == nil; got != want {
			t.Errorf("Valid(%q) = %v, but the grammar says %v", in, got, want)
		}
	})
}
```

The regular expression accepts leading zeros, which the grammar does not (`num` is `"0"` or a digit from 1 to 9 and
more digits). The test finds that at once:

```bash
go test .
```

```text
--- FAIL: TestValidMatchesGrammar (0.01s)
    version_test.go:44: Valid("01757.0.0") = true; the grammar rejects it (insert "0" at 0 of "1757.0.0": 1:2: syntax error: expected ".")
    version_test.go:44: Valid("11.00.1633") = true; the grammar rejects it (insert "0" at 3 of "11.0.1633": 1:5: syntax error: expected ".")
    version_test.go:44: Valid("00.48.9") = true; the grammar rejects it (duplicate "0" at 0 of "0.48.9": 1:2: syntax error: expected ".")
    version_test.go:44: Valid("0.1.00") = true; the grammar rejects it (duplicate "0" at 4 of "0.1.0": 1:6: syntax error: expected "-", end of input)
    ...
FAIL
FAIL	example.com/fuzz	0.552s
```

(The output continues with five more lines like these.) Each message names the input, the mutation and the valid
input it came from, and the error the grammar reports. Fix the regular expression:

```go
var versionRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z0-9.]+)?$`)
```

```bash
go test -v -run TestValid .
```

```text
=== RUN   TestValidMatchesGrammar
--- PASS: TestValidMatchesGrammar (0.02s)
PASS
ok  	example.com/fuzz	0.375s
```

`go test` also runs `FuzzValid` on its seed corpus, the generated inputs. To let the fuzzer mutate them, name it:

```bash
go test -run '^$' -fuzz FuzzValid -fuzztime 5s .
```

With the fixed expression, five seconds of fuzzing found no input on which the function and the grammar disagree.

## How it works

- **The grammar is the oracle.** For any string, `p.Parse` says whether the format allows it. A generated input is
  accepted by construction (every input `Generate` returns has been parsed and accepted), and a near miss is rejected
  (`GenerateInvalid` keeps a mutation only if the parser rejects it), so no expected values are written by hand.
- **Near misses find the edge cases.** The grammar accepts `0` and `10`, not `01`. A mutation such as "insert `0` at the
  start" produces that input from a valid one, which is exactly the case a hand-written test forgets.
- **Reproducible.** The same version of PEGO, built with the same Go release, gives the same inputs for the same grammar,
  options and seed. A failure that matters should be kept as a test case of its own, as the inputs may change when you
  upgrade.
- **The fuzz seed.** `sample.Seed` adds generated inputs to the corpus of a fuzz test. A fuzzer started from nothing
  rarely gets past the first character of a format like this; started from valid inputs, it mutates real ones.

## Variations

- **The same check from the command line**: `pego sample -g version.pego -n 20` prints inputs, and `-invalid -f json`
  near misses with their mutation and error. `-coverage` reports the rules and alternatives that the inputs did not
  reach, which often points at an alternative that can never match.
- **Compare with a real implementation.** If the format has one (`encoding/json`, `go/parser`, `net/url`), call it in
  place of `Valid` and compare the answers, as the ready-made parsers do against `encoding/json`, `go/parser` and
  others.
- **Test code that consumes the tree.** An evaluator, a converter or a formatter can be run on generated inputs, and
  checked for properties that must hold for all of them (no panic; format, parse, format gives the same text).
- **Grammars with context.** The generator evaluates predicates (indentation, matching tags) on what it generates, but
  not predicates over values built by actions; for those it generates and checks, and finds fewer inputs.

See [Sampling inputs and fuzzing](../guide/sampling-and-fuzzing.md) for the options, coverage reports and limits.
