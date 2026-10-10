# Sampling inputs and fuzzing

A grammar describes the inputs it accepts, so it can also produce them. Generated inputs are useful wherever you need
many realistic documents: to test code that consumes parse results (an evaluator, a converter, a type checker), to
compare a parser with another implementation of the same language, and to seed a fuzz test so that the fuzzer starts
from inputs that get past the first token. Near-miss invalid inputs, valid ones with one mutation, exercise error
reporting and recovery.

| | Command line | Go (`github.com/ornew/pego/sample`) |
|:--|:--|:--|
| Inputs the grammar accepts | `pego sample -g g.pego` | `sample.Generate`, `Generator.Next`, `Generator.Generate` |
| Prefer what was not exercised yet, and report it | `-coverage` | `sample.WithCoverage()`, `Generator.Coverage` |
| Near-miss inputs the grammar rejects | `-invalid` | `Generator.Invalid`, `Generator.GenerateInvalid` |
| Seed a fuzz test | | `sample.Seed(f, p, n)` |

Every input you get has been parsed with the parser and accepted without errors, also without errors recovered by
`#recover`. The same grammar, options and seed give the same inputs (see [below](#on-the-command-line) for how far
that holds).

The design is in [design record 015](../design/015-input-generation.md).
How generated inputs fit into a test suite for a grammar is in [testing grammars](testing-grammars.md).

- [On the command line](#on-the-command-line)
- [Controlling size and shape](#controlling-size-and-shape)
- [Coverage](#coverage)
- [Near-miss invalid inputs](#near-miss-invalid-inputs)
- [From Go](#from-go)
- [Testing code that consumes parse results](#testing-code-that-consumes-parse-results)
- [Fuzzing](#fuzzing)
- [How it works, and what it cannot do](#how-it-works-and-what-it-cannot-do)

## On the command line

`pego sample` prints distinct inputs that the grammar accepts, ten by default (`-n`), each as a Go-quoted string on its
own line, so that inputs containing line breaks stay on one line. With the JSON grammar of
[parsers/json](../../parsers/json/json.pego):

```bash
$ pego sample -g parsers/json/json.pego -n 8
" [  \n]"
"\tfalse"
"\"\\r\"\t "
" [\t]"
"{} \n"
" \n{\t\t\t}\t\r"
"null"
"{\n\r\"\\uEA57^1\"\t\t:1E+71,\"\"\n:null\n \r,\t\"*ଦ\":\t\t\t{\"\"\t:\n\n\nnull\t,\"\\r\":null,\"an<5\": true,\"\":\rnull}\t\t\t,\"ጴ,G\":\t\n true}"
```

The inputs are random, not pretty: the grammar allows whitespace between tokens, so the inputs contain it, and
characters are drawn mostly from printable ASCII with some multi-byte characters mixed in, and otherwise from any code
point the character class matches (never a surrogate). `strconv.Unquote` reads each line back.

`-seed` selects the random decisions; the default is 0. With the same version of PEGO, built with the same Go release,
the same grammar, options and seed give the same inputs on every run and machine, so a seed is enough to reproduce a
test case. Across versions they may differ: the generator's algorithm changes as it improves, and it draws its random
numbers with `math/rand/v2`, whose algorithms Go does not promise to keep. Keep the inputs themselves (as test files,
or in a fuzz corpus) where they must not change. `-f json` prints a JSON document instead:

```bash
$ pego sample -g parsers/json/json.pego -n 2 -seed 5 -f json
{
  "start": "main",
  "seed": 5,
  "inputs": [
    "null",
    "{\"\":-0e941\t\t,\"\"\n\r : []\t \n}"
  ]
}
```

`-s` selects the start rule (`main` by default, or the one saved in a `.pegoc`). A grammar compiled with
`pego compile -no-ast` cannot be sampled: generation needs the grammar AST.

If the grammar accepts fewer distinct inputs than requested, or the generator cannot find more, `pego sample` prints
what it found and says so on standard error (`pego sample: found only 3 distinct inputs`). If it finds none at all, it
fails: the grammar may accept nothing (`def main = "a"* "a"`: the repetition takes every `a`), or every input may fail
an action at run time.

## Controlling size and shape

Three options shape the inputs. They are soft: past a bound, the generator prefers the shortest way to finish the
input, but it goes further when the input requires it (a predicate that counts the nesting, or a longer match).

| Flag | Option | Default | Bounds |
|:--|:--|:--|:--|
| `-max-depth` | `WithMaxDepth` | 5 | Recursion: calls of rules that are already active (an array directly in an array of the JSON example counts 3, since `value`, `array` and `elements` are each entered again) |
| `-max-repeat` | `WithMaxRepeat` | 3 | Iterations a repetition aims for beyond its minimum, and operators in a chain of a Pratt expression |
| `-max-len` | `WithMaxLen` | 512 | Length of the input in bytes |

A repetition takes each further iteration with probability 1/2, so short lists are the most common. For readable
calculator expressions:

```bash
$ pego sample -g examples/calculator/calc.pego -n 5 -seed 7 -max-len 30 -max-repeat 1
"\n(12\r-38.3 ) +11.04 "
" 46.8"
"\n(\r(-\t(\r((2) --\t36.89) ) )+1 )"
"(\r2.06)\r^ ((86.1)\r^\n( ( 8.37) ))"
"\r(\r8\r+\t(\n(\t1\n^6.52\t)^(30\t)\t)\r)"
```

Smaller bounds give smaller, more numerous distinct inputs; larger ones exercise deep nesting and long lists. Very
large lengths make each attempt slower and can lower coverage on large grammars: the generator works within a budget of
steps per attempt, and the defaults are a good compromise for the grammars in [examples](../../examples/).

`New` first analyzes the grammar's minimum derivation depths, text lengths and coverage reach sets. It processes
acyclic callees before callers and propagates changed estimates through a bounded worklist for recursive grammars.
This avoids repeated whole-grammar sweeps caused by declaration order. `WithBudget` bounds generation attempts;
it does not bound constructor analysis. Dense coverage sets still grow with the number of rules and targets.
Minimum-length estimates saturate at 2³⁰ bytes on both Go integer widths, so
recursive dependencies and large repetition bounds cannot wrap into negative estimates.

Three limits are hard, and each has a knob:

- An attempt gives up after a budget of steps (`-budget`, `WithBudget`, default 20,000; a step is roughly one
  expression generated or Pratt scope inspected). An input that needs more, such as `"a"{25000}`, is found only
  with a larger budget.
- A repetition never takes more than `8 × max-repeat + 4` iterations beyond its minimum (28 by default), even when a
  predicate wants more: `x:"a"* [len($x) == 29]` needs `-max-repeat 4`.
- Recursive rule calls never nest more than `max-depth + 8` deep (13 by default): an input that must nest 15 levels
  deep needs `-max-depth 7`.

## Coverage

With `-coverage`, the generator prefers rules and alternatives that no input has exercised yet, and `pego sample`
reports on standard error what the inputs exercised and what they missed. Take a list whose items may be words,
numbers or booleans:

```pego
def main = item ("," item)*
def item = word / number / "true" / "false"
def word = @(?a-z)+
def number = @(?0-9)+
```

```bash
$ pego sample -g items.pego -n 5 -coverage
"3,rs,51,6"
"3,57"
"v,hc,a,0"
"05,e"
"12,ed"
rules 4/4 (100%), alternatives 2/4 (50%)
missed item: choice 2: "true"
missed item: choice 3: "false"
```

No input exercises `"true"` or `"false"`, however many you ask for: `word` comes first in the ordered choice and
matches `true` itself, so the parser never reaches the later alternatives. A missed alternative is often a bug of this
kind; here the fix is to try the keywords first: `def item = ("true" / "false") !(?a-z) / word / number`.

The report counts the rules that the start rule reaches, the alternatives of ordered choices, and the operands and
operators of Pratt expressions. It lists separately, and does not count:

- `unreachable`: rules that generation cannot exercise, because the start rule does not call them or calls them only
  inside a negative lookahead (`!keyword`), the skip of `#recover`, or an expression that can never match;
- `never match`: rules that the start rule calls but that can never match, such as rules that end in `_|_` to report
  an error. Alternatives that can
  never match are not counted either.

For the XML grammar of [parsers/xml](../../parsers/xml/xml.pego), 50 inputs cover all of it:

```bash
$ pego sample -g parsers/xml/xml.pego -n 50 -seed 1 -coverage > /dev/null
rules 74/74 (100%), alternatives 89/89 (100%)
```

Not every grammar is like that. The Python grammar of [parsers/python](../../parsers/python/python.pego) keeps the
indentation of the current block packed in a variable and compares it in predicates (`stmt_sep`, `indented_block`),
which the generator rarely satisfies by chance, so it misses the rules that need them:

```bash
$ pego sample -g parsers/python/python.pego -n 50 -seed 1 -coverage > /dev/null
rules 93/336 (28%), alternatives 86/471 (18%)
missed rule: stmt_sep
missed rule: block
missed rule: indented_block
...
unreachable: keyword
```

More inputs reach more (200 inputs cover 43% of the rules), but such a grammar is better tested with real inputs: the
tests of the ready-made parsers use the specifications and the reference implementations of their languages.

With `-f json`, the report is the `coverage` member of the document. Coverage is measured on the way the generator
derived each input, which is the parser's way unless the generator could not tell (see the
[limits](#how-it-works-and-what-it-cannot-do)).

## Near-miss invalid inputs

`-invalid` generates inputs that the grammar rejects: each is an accepted input with one mutation (a deletion,
insertion, substitution, duplication, swap or truncation), kept only if the parser rejects the result. In the JSON
format, each comes with the input it was made from, the mutation (positions are byte offsets in the base) and the
error:

```bash
$ pego sample -g parsers/json/json.pego -n 3 -invalid -f json
{
  "start": "main",
  "seed": 0,
  "invalid": [
    {
      "input": " [ { \n]",
      "base": " [  \n]",
      "mutation": "insert \"{\" at 3",
      "error": "2:1: syntax error: expected \"\\\"\", \"}\", (? \\t\\r\\n)"
    },
    {
      "input": "t\"✵5\\u58CD⭧g\"",
      "base": "\n\"✵5\\u58CD⭧g\"",
      "mutation": "replace \"\\n\" at 0 with \"t\"",
      "error": "1:1: syntax error: expected \"-\", \"0\", \"[\", \"\\\"\", \"false\", \"null\", \"true\", \"{\", (? \\t\\r\\n), (?1-9)"
    },
    {
      "input": "\r\n {} r\n",
      "base": "\r\n {} \n",
      "mutation": "insert \"r\" at 6",
      "error": "2:5: syntax error: expected (? \\t\\r\\n), end of input"
    }
  ]
}
```

Use them to check error messages and positions, and recovery with `#recover`: a recovered error counts as rejected, so
the inputs of a grammar with `#recover` include ones that produce both a tree and errors. A grammar that accepts every
input, such as `def main = .*`, has no near misses: `pego sample -invalid` then fails with "no mutation found that the
parser rejects" (`sample.ErrNoInvalid` in Go), not with the error for grammars that accept nothing.

## From Go

`sample.Generate` is the shortest way: it returns up to `n` distinct inputs that the parser accepts, starting at the
parser's start rule (use `Parser.WithStart` for another).

```go
p, err := pego.CompileSource(`
def main = pair ("," " "? pair)*
def pair = key "=" value
def key = @(?a-z)+
def value = @(?0-9)+ / "\"" @(?^"\n)* "\"" / "true" / "false"
`, "main")
if err != nil {
	log.Fatal(err)
}
inputs, err := sample.Generate(p, 5, sample.WithSeed(1))
if err != nil {
	log.Fatal(err) // sample.ErrNoInput if nothing was found
}
for _, in := range inputs {
	fmt.Printf("%q\n", in)
}
```

```
"h=\"\""
"in=true, i=true"
"utc=false"
"i=false"
"fjmv=false"
```

A `Generator` keeps state across calls: the random sequence, and what the accepted inputs exercised.

```go
g, err := sample.New(p, sample.WithSeed(1))  // options: WithSeed, WithMaxDepth, WithMaxRepeat, WithMaxLen,
                                              // WithBudget, WithAttempts, WithCoverage
in, err := g.Next()                           // one input ("h=\"\""); may repeat earlier ones
inputs, err := g.Generate(1)                  // distinct inputs (["in=true, i=true"])
fmt.Print(g.Coverage().Report())              // what the two inputs exercised
```

```
rules 4/4 (100%), alternatives 2/4 (50%)
missed value: choice 0: @(?0-9)+
missed value: choice 3: "false"
```

`Coverage` returns the numbers and lists as a struct (`Rules`, `RulesCovered`, `MissedAlternatives`, ...), and
`Stats` counts candidates, rejected candidates and failed searches. A `Generator` is not safe for concurrent use; create
one per goroutine, with different seeds.

`Invalid` and `GenerateInvalid` return near-miss invalid inputs as `sample.Invalid` values:

```go
g, err := sample.New(p, sample.WithSeed(1))
invalid, err := g.GenerateInvalid(3)
for _, inv := range invalid {
	fmt.Printf("%q (%s): %v\n", inv.Input, inv.Mutation, inv.Err)
}
```

```
"h=\"\" " (insert " " at 4): 1:5: syntax error: expected ",", end of input
"n" (truncate at 1): 1:2: syntax error: expected "=", (?a-z)
"u=truee" (duplicate "e" at 5): 1:7: syntax error: expected ",", end of input
```

## Testing code that consumes parse results

Generated inputs are good test data for whatever you do with a tree: they reach constructs that hand-written tests
forget, and a seed reproduces any failure. Two kinds of checks work without knowing the expected output:

- **Properties** of your code that hold for every valid input: an evaluator does not panic and returns a value or a
  well-formed error; pretty-printing and parsing again gives the same tree; a converter's output is valid in the target
  format.
- **Differential tests** against another implementation of the same language. This test (from
  [sample/sample_test.go](../../sample/sample_test.go)) checks that the JSON grammar of [parsers/json](../../parsers/json/) and Go's
  `encoding/json` agree, on 200 generated documents and on 200 near-miss invalid ones:

```go
func TestJSONAgreesWithEncodingJSON(t *testing.T) {
	p := ... // the parser of parsers/json/json.pego
	g, err := sample.New(p, sample.WithSeed(1), sample.WithCoverage())
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := g.Generate(200)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		if !json.Valid([]byte(in)) {
			t.Errorf("encoding/json rejects %q", in)
		}
	}
	invalid, err := g.GenerateInvalid(200)
	if err != nil {
		t.Fatal(err)
	}
	for _, inv := range invalid {
		if json.Valid([]byte(inv.Input)) {
			t.Errorf("encoding/json accepts %q (%s of %q)", inv.Input, inv.Mutation, inv.Base)
		}
	}
}
```

Use `WithCoverage` in such tests, so that a few hundred inputs reach most of the grammar, and keep the seed fixed so
that the test is deterministic. To explore further, run the same test with other seeds now and then.

## Fuzzing

Go's fuzzer mutates the inputs of its seed corpus. Started from nothing, it rarely gets past the first token of a
grammar; started from generated inputs, it mutates real documents. `sample.Seed` adds up to `n` distinct generated
inputs to the corpus of a fuzz test, each as one `string` argument, checks again that each parses, fails the test if it
cannot generate any, and returns them:

```go
// FuzzCalculator checks that the calculator never panics, whatever the input, that all backends agree,
// and that the generated inputs it is seeded with parse.
func FuzzCalculator(f *testing.F) {
	src, err := os.ReadFile("../examples/calculator/calc.pego")
	if err != nil {
		f.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		f.Fatal(err)
	}
	// Valid inputs, and near-miss invalid ones for the error paths.
	valid := map[string]bool{}
	for _, in := range sample.Seed(f, p, 20, sample.WithMaxLen(64)) {
		valid[in] = true
	}
	g, err := sample.New(p, sample.WithSeed(1), sample.WithMaxLen(64))
	if err != nil {
		f.Fatal(err)
	}
	invalid, err := g.GenerateInvalid(10)
	if err != nil {
		f.Fatal(err)
	}
	for _, inv := range invalid {
		f.Add(inv.Input)
	}
	f.Fuzz(func(t *testing.T, in string) {
		n, err := p.Parse(in) // must not panic
		if valid[in] && err != nil {
			t.Fatalf("generated input %q does not parse: %v", in, err)
		}
		for _, b := range []pego.Backend{pego.Bytecode, pego.BytecodeIterative} {
			m, err2 := p.Parse(in, pego.WithBackend(b))
			if fmt.Sprint(n, err) != fmt.Sprint(m, err2) {
				t.Fatalf("%q: backend %v returned %v, %v; the closure backend %v, %v", in, b, m, err2, n, err)
			}
		}
	})
}
```

The test is [sample/example_test.go](../../sample/example_test.go). `go test` runs the fuzz target on its seeds only,
which makes it an ordinary test of the generated inputs; `-fuzz` starts fuzzing:

```bash
$ go test ./sample -run '^$' -fuzz=FuzzCalculator -fuzztime=3s -parallel=1
fuzz: elapsed: 0s, gathering baseline coverage: 30/30 completed, now fuzzing with 1 workers
fuzz: elapsed: 3s, execs: 42159 (14049/sec), new interesting: 49 (total: 79)
PASS
```

Replace the body of the fuzz function with what you want to hold for every input: your code that consumes the tree
must not panic, must not loop, and must handle `SyntaxError` and `SyntaxErrors` (from `#recover`). Keep seeds short
with `WithMaxLen`: the fuzzer works best on short inputs.

## How it works, and what it cannot do

The generator walks the grammar from the start rule, making a random decision at each ordered choice, repetition,
optional expression and Pratt expression, and searches for a way to finish the input that the parser will accept; it
then parses the candidate and retries if the parser rejects it. While it writes, it steers away from inputs that the
parser would read differently:

- after taking an alternative of an ordered choice, the earlier alternatives must not match there;
- after stopping a repetition or skipping an optional expression, the element must not match there (repetitions are
  greedy);
- `!e` must not match; for `&e`, a text for `e` is generated and the input must continue with it;
- predicates are evaluated on the generated text, with captures, variables, `len`, `text`, comparisons and arithmetic,
  so indentation (as in [examples/outline](../../examples/outline/outline.pego)) and matching tags (as in
  [parsers/xml](../../parsers/xml/xml.pego)) work;
- Pratt expressions are generated as chains of operands and operators, avoiding chains of `infix none` operators,
  with the outer infix/postfix tail filtered by level-restricted calls. Prefixes remain available at every operand
  position, including prefixes below the entry level. Their right operands can use operators tighter than the
  prefix's own level; coverage includes these RHS paths and the rules they call. Operators and nested targets
  that no reachable entry or prefix RHS permits are excluded. See the
  [prefix exception](expressions.md#4-level-restricted-calls) for the parser's contract.

After an already-used nonassociative operator closes its level, the stop check allows a caller to continue with
that token: `e "<" e` with `infix none "<"` can generate `a<a<a`. Other applicable infix and postfix operators still
prevent an early stop.

Nonassociative restrictions belong to each Pratt invocation, including prefix and infix right operands. Returning
from a tighter RHS restores the parent's restriction: with low `infix none "<"` and higher `infix none "="`, the
generator can produce `a<a=a=a`, using `=` once inside `<`'s RHS and once in the enclosing frame. It still rejects
`a=a=a`, which repeats `=` in the same frame. Explicit rule calls start their own state. Scope traversal consumes
the generation budget; the operator-count bound remains per chain.

For overlapping parts, stop checks compare all infix/postfix candidates by consumed length and declaration
order before checking the winning part's level or nonassociative restriction. A longer disabled part can therefore
belong to the caller even when it starts with a shorter enabled part. Skip, competing parts, scope traversal and
recursive RHS checks share one bounded matcher allowance; incomplete input keeps the check pending, while unsupported matching
leaves the decision to the parser.

An infix continuation must have a successful whole RHS. With prefix `"~"` and infix `"!"`, the generator can
produce `a!~` for `e "!~"`: the attempted RHS has no operand, so the ordinary infix rewinds for the caller.
Conversely, `a!~a` has a complete RHS and cannot be assigned to that caller suffix. The bounded check follows
nested prefix and infix/postfix paths for supported syntax, preserving declaration ties, binding, operand
fallback and nonassociative scopes. It does not equate a prefix token or successful primary with RHS success;
a later cut can still make that RHS fail. When led parts cannot commit, the check can prove success despite an
unfinished future tail; its endpoint is only a witness. Unsupported cuts, predicates, Pratt references, left
recursion, recovery and exhausted matching remain inconclusive. Every returned input is still parsed and accepted.

What the generator cannot evaluate, it leaves to the parser: predicates over values built by actions or over struct
fields, and lookaheads into Pratt expressions or left-recursive rules. Those constructs are handled only by generating
and checking, which costs attempts; a grammar where most choices depend on them yields fewer inputs per second and
lower coverage. In practice:

- **No input found** (`sample.ErrNoInput`, or `pego sample` fails): check that some input parses at all. The grammar
  may accept nothing, or every input may fail an action at run time. If the inputs it needs are long, raise the
  budget (`WithBudget`) or, for long repetitions, `WithMaxRepeat`; if they are merely rare, raise `WithAttempts`.
- **Few distinct inputs**: the language may be small (`"a" / "b"` has two inputs). `Generate` stops after
  `WithAttempts` (100) inputs in a row that it already returned.
- **Missed alternatives** that should be reachable: an earlier alternative of the choice may match their text (as with
  `word` and `"true"` above), or reaching them may need many predicates to hold at once. More inputs or other seeds
  help with the latter.
- **Inputs are not uniform** over the language and not minimized: they are test data, not a statistical sample.
