# Sampling Inputs and Fuzzing

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

## On the Command Line

`pego sample` prints distinct inputs that the grammar accepts, ten by default (`-n`), each as a Go-quoted string on its
own line, so that inputs containing line breaks stay on one line. With the JSON grammar of
[parsers/json](../../parsers/json/json.pego):

```bash
pego sample -g parsers/json/json.pego -n 8
```

```text
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

`-seed` selects the random decisions; the default is 0. With the same version of PEGO, the same grammar, options and
seed give the same inputs on every run and machine, whichever Go release built it, so a seed is enough to reproduce a
test case. Across versions of PEGO they may differ, as the generator's algorithm changes as it improves. Keep the
inputs themselves (as test files, or in a fuzz corpus) where they must not change. `-f json` prints a JSON document instead:

```bash
pego sample -g parsers/json/json.pego -n 2 -seed 5 -f json
```

```text
{
  "start": "main",
  "seed": 5,
  "inputs": [
    "null",
    "{\"\":-0e941\t\t,\"\"\n\r : []\t \n}"
  ],
  "stats": {
    "candidates": 2,
    "rejected": 0,
    "failed": 0,
    "exhausted": 0,
    "duplicates": 0,
    "checkCuts": 2,
    "predicateCuts": 0,
    "ruleCuts": [
      {
        "rule": "ws",
        "checks": 2,
        "predicates": 0
      }
    ]
  }
}
```

`stats` counts the work, as `Stats` does in Go (see [From Go](#from-go)). `ruleCuts` splits the cuts by the rule
whose body holds the check or predicate, the rules with the most cuts first, as `Generator.Cuts` does.

`-s` selects the start rule (the one saved in a `.pegoc`, otherwise the grammar's default start rule). A grammar compiled with
`pego compile -no-ast` cannot be sampled: generation needs the grammar AST.

If the grammar accepts fewer distinct inputs than requested, or the generator cannot find more, `pego sample` prints
what it found and says so on standard error (`pego sample: found only 3 distinct inputs`). If it finds none at all, it
fails: the grammar may accept nothing (`rule main = "a"* "a"`: the repetition takes every `a`), or every input may fail
an action at run time. The error tells what became of the attempts:

```text
pego: sample: no input found that the parser accepts in 100 attempts: 0 candidates rejected by the parser, 0 searches out of budget, 100 searches without a way to finish (2900 ways cut by checks, 0 by predicates; most cuts in main (2900 by checks, 0 by predicates))
```

Here no search found a way to finish, and checks cut thousands of ways: the generator saw that the parser would not
match what it was writing, all of them in `main`. The error names up to three rules with the most cuts; a check
belongs to the rule whose body holds the lookahead, ordered choice, optional, repetition or `$$` behind it, even when
the text that decides it comes from another rule. Candidates rejected by the parser point at what the generator cannot check, such as
actions; searches out of budget at inputs too long for `-budget`; many cuts by predicates at predicates that the
search rarely satisfies.

## Controlling Size and Shape

Three options shape the inputs. They are soft: past a bound, the generator prefers the shortest way to finish the
input, but it goes further when the input requires it (a predicate that counts the nesting, or a longer match).

| Flag | Option | Default | Bounds |
|:--|:--|:--|:--|
| `-max-depth` | `WithMaxDepth` | 5 | Recursion: calls of rules that are already active (an array directly in an array of the JSON example counts 3, since `value`, `array` and `elements` are each entered again) |
| `-max-repeat` | `WithMaxRepeat` | 3 | Iterations a repetition aims for beyond its minimum, and operators in a chain of an Operators Rule |
| `-max-len` | `WithMaxLen` | 512 | Length of the input in bytes |

A repetition takes each further iteration with probability 1/2, so short lists are the most common. For readable
calculator expressions:

```bash
pego sample -g examples/calculator/calc.pego -n 5 -seed 7 -max-len 30 -max-repeat 1
```

```text
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
  expression generated or Operators Rule scope inspected). An input that needs more, such as `"a" #repeat(25000)`, is found only
  with a larger budget.
- A repetition never takes more than `8 × max-repeat + 4` iterations beyond its minimum (28 by default), even when a
  predicate wants more: `x:"a"* {len($x) == 29}` needs `-max-repeat 4`.
- Recursive rule calls never nest more than `max-depth + 8` deep (13 by default): an input that must nest 15 levels
  deep needs `-max-depth 7`.

## Coverage

With `-coverage`, the generator prefers rules and alternatives that no input has exercised yet, and `pego sample`
reports on standard error what the inputs exercised and what they missed. Take a list whose items may be words,
numbers or booleans:

```pego
@start
rule main = item ("," item)*
rule item = word / number / "true" / "false"
rule word = [a-z]+ #atomic
rule number = [0-9]+ #atomic
```

```bash
pego sample -g items.pego -n 5 -coverage
```

```text
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
kind; here the fix is to try the keywords first: `rule item =  ("true" / "false") ![a-z] / word / number `.

The report counts the rules that the start rule reaches, the alternatives of ordered choices, and the operands and
operator parts declared in Operators Rules. It lists separately, and does not count:

- `unreachable`: rules that generation cannot exercise, because the start rule does not call them or calls them only
  inside a negative lookahead (`!keyword`), the skip of `#recover`, or an expression that can never match;
- `never match`: rules that the start rule calls but that can never match, such as rules that end in `_|_` to report
  an error. Alternatives that can
  never match are not counted either.

An [inline action](actions.md) counts as a rule, because the parser runs it as one, and the report names it by
the rule that holds it: `the inline action in rule main`, or `inline action 2 in rule main` when the rule holds
several. The alternatives are printed as written, with the inline action in place.

For the XML grammar of [parsers/xml](../../parsers/xml/xml.pego), 50 inputs cover all of it:

```bash
pego sample -g parsers/xml/xml.pego -n 50 -seed 1 -coverage > /dev/null
```

```text
rules 74/74 (100%), alternatives 89/89 (100%)
```

Not every grammar is like that. The Python grammar of [parsers/python](../../parsers/python/python.pego) keeps the
indentation of the current block packed in a variable and compares it in predicates (`stmt_sep`, `indented_block`),
which the generator rarely satisfies by chance, so it misses the rules that need them:

```bash
pego sample -g parsers/python/python.pego -n 50 -seed 1 -coverage > /dev/null
```

```text
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

## Near-Miss Invalid Inputs

`-invalid` generates inputs that the grammar rejects: each is an accepted input with one mutation (a deletion,
insertion, substitution, duplication, swap or truncation), kept only if the parser rejects the result. In the JSON
format, each comes with the input it was made from, the mutation (positions are byte offsets in the base) and the
error:

```bash
pego sample -g parsers/json/json.pego -n 3 -invalid -f json
```

```text
{
  "start": "main",
  "seed": 0,
  "invalid": [
    {
      "input": " [ { \n]",
      "base": " [  \n]",
      "mutation": "insert \"{\" at 3",
      "error": "2:1: syntax error: expected \"\\\"\", \"}\", [ \\t\\r\\n]"
    },
    {
      "input": "t\"✵5\\u58CD⭧g\"",
      "base": "\n\"✵5\\u58CD⭧g\"",
      "mutation": "replace \"\\n\" at 0 with \"t\"",
      "error": "1:1: syntax error: expected \"-\", \"0\", \"[\", \"\\\"\", \"false\", \"null\", \"true\", \"{\", [ \\t\\r\\n], [1-9]"
    },
    {
      "input": "\r\n {} r\n",
      "base": "\r\n {} \n",
      "mutation": "insert \"r\" at 6",
      "error": "2:5: syntax error: expected [ \\t\\r\\n], end of input"
    }
  ]
}
```

Use them to check error messages and positions, and recovery with `#recover`: a recovered error counts as rejected, so
the inputs of a grammar with `#recover` include ones that produce both a tree and errors. A grammar that accepts every
input, such as `rule main = .*`, has no near misses: `pego sample -invalid` then fails with "no mutation found that the
parser rejects" (`sample.ErrNoInvalid` in Go), not with the error for grammars that accept nothing.

## From Go

`sample.Generate` is the shortest way: it returns up to `n` distinct inputs that the parser accepts, starting at the
parser's start rule (use `Parser.WithStart` for another).

```go
p, err := pego.CompileSource(`@start
rule main =  pair ("," " "? pair)*
rule pair =  key "=" value
rule key =  [a-z]+ #atomic
rule value =  [0-9]+ #atomic / "\"" [^"\n]* #atomic "\"" / "true" / "false" `, "main")
if err != nil {
	log.Fatal(err)
}
inputs, err := sample.Generate(p, 5, sample.WithSeed(1))
if err != nil {
	log.Fatal(err) // wraps sample.ErrNoInput (test with errors.Is) if nothing was found
}
for _, in := range inputs {
	fmt.Printf("%q\n", in)
}
```

```text
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

```text
rules 4/4 (100%), alternatives 2/4 (50%)
missed value: choice 0: [0-9]+ #atomic
missed value: choice 3: "false"
```

`Coverage` returns the numbers and lists as a struct (`Rules`, `RulesCovered`, `MissedAlternatives`, ...), and
`Stats` counts candidates, rejected candidates, failed searches and those of them that ran out of budget,
duplicates, and the ways the search abandoned because a check (a lookahead, the order of a choice, `$$`) or a predicate
showed that the parser would not match them. `Cuts` splits those cuts by rule as `[]sample.RuleCuts`, the rules
with the most cuts first. A `Generator` is not safe for concurrent use; create
one per goroutine, with different seeds.

`Invalid` and `GenerateInvalid` return near-miss invalid inputs as `sample.Invalid` values:

```go
g, err := sample.New(p, sample.WithSeed(1))
invalid, err := g.GenerateInvalid(3)
for _, inv := range invalid {
	fmt.Printf("%q (%s): %v\n", inv.Input, inv.Mutation, inv.Err)
}
```

```text
"h=\"\" " (insert " " at 4): 1:5: syntax error: expected ",", end of input
"n" (truncate at 1): 1:2: syntax error: expected "=", [a-z]
"u=truee" (duplicate "e" at 5): 1:7: syntax error: expected ",", end of input
```

## Testing Code That Consumes Parse Results

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
go test ./sample -run '^$' -fuzz=FuzzCalculator -fuzztime=3s -parallel=1
```

```text
fuzz: elapsed: 0s, gathering baseline coverage: 30/30 completed, now fuzzing with 1 workers
fuzz: elapsed: 3s, execs: 42159 (14049/sec), new interesting: 49 (total: 79)
PASS
```

Replace the body of the fuzz function with what you want to hold for every input: your code that consumes the tree
must not panic, must not loop, and must handle `SyntaxError` and `SyntaxErrors` (from `#recover`). Keep seeds short
with `WithMaxLen`: the fuzzer works best on short inputs.

To fuzz incremental parsing, use `pego edit-fuzz`, which starts from generated inputs too and edits them; see
[Fuzzing Edits](#fuzzing-edits).

## Fuzzing Edits

A `Document` has one rule: its result equals a fresh parse of its text. The rule can be checked mechanically for any grammar
(see [Incremental Parsing](incremental.md)). `pego edit-fuzz` applies random sequences of edits, undos
and redos to a `Document` and, at the initial text and at each checkpoint, compares its result with a fresh parse of
`doc.Text()`: values with their full spans, and errors with their positions, expectations and messages. It runs
every backend in both position units:

```bash
pego edit-fuzz -g examples/calculator/calc.pego -seed 1
```

```text
ok closure/codepoints: 100 sequences
ok closure/bytes: 100 sequences
ok bytecode/codepoints: 100 sequences
ok bytecode/bytes: 100 sequences
ok bytecode-iterative/codepoints: 100 sequences
ok bytecode-iterative/bytes: 100 sequences
```

The initial texts are the empty text and inputs generated from the grammar (see
[sampling inputs](sampling-and-fuzzing.md)), or the files given with `-corpus` (a file, or a directory read in name
order). Replacements mix parts of those texts with partial and complete multibyte characters and random bytes, so
edits split, join and break UTF-8 sequences. `-backend` and `-unit` take comma-separated lists to narrow the run;
`-sequences` and `-operations` set its size.

A seed is not a reproducer: generation may change between releases. With `-save-failures dir`, the first failing
sequence is saved as JSON, and `-minimize` then shrinks it, keeping the same kind of failure, and saves the result
next to it. Without `-save-failures`, the message includes the failing sequence as JSON. Here a deliberately broken
`Document` fails:

```bash
pego edit-fuzz -g calc.pego -save-failures failures -minimize
```

```text
pego: calc.pego (seed 0): closure/codepoints: document result differs from a fresh parse at operation 21 (text "…") in error.error.col:
document: 1
fresh:    2
saved to failures/editfuzz-0be4ccb60eadff90.json
minimized to 4 operations in failures/editfuzz-9d0bcab55578f1b5.json
```

Replaying the minimized sequence fails the same way:

```bash
pego edit-fuzz -g calc.pego -replay failures/editfuzz-9d0bcab55578f1b5.json
```

```text
pego: failures/editfuzz-9d0bcab55578f1b5.json: closure/codepoints: document result differs from a fresh parse at operation 3 (text " �6\t") in error.error.col:
document: 1
fresh:    2
```

The message names the first difference, here the column of the syntax error, with the two values there; the
command exits with status 1. The minimized sequence starts from a tab, inserts `6` before it, checks, inserts a
space and an invalid byte, and checks again:

```json
{
  "version": 1,
  "grammar_sha256": "f4ab154a3405db596d8950b251e7da79836105408eb0fbc56ec0cf6d3e9b272e",
  "grammar_normalization": "none",
  "start": "main",
  "backend": "closure",
  "unit": "codepoints",
  "seed": 0,
  "text_encoding": "base64",
  "initial_base64": "CQ==",
  "operations": [
    { "kind": "edit", "start": 0, "end": 0, "text_base64": "Ng==" },
    { "kind": "checkpoint" },
    { "kind": "edit", "start": 0, "end": 0, "text_base64": "IKw=" },
    { "kind": "checkpoint" }
  ]
}
```

`-replay file` runs a saved sequence, which records its start rule, backend and unit; with `-minimize`, it
minimizes the sequence into the file's directory unless `-save-failures` is given. The file records the exact text
and replacements in base64, so invalid UTF-8 survives, and the SHA-256 of the exact grammar source: replaying with
another grammar, even one differing only in whitespace, is refused. A saved file is never replaced.

Edit positions are in the sequence's unit and refer to the text just before the edit; in `bytes`, they must fall on
character boundaries, as for `Document.Edit`. `undo` restores the text before the latest edit not yet undone and
`redo` reapplies it; a new edit clears the redo history. Both edit the same `Document`: undo with the single edit
that restores the earlier text. After every edit, undo and redo, the Document's text must be the edited text (in
`codepoints`, with each invalid byte replaced by U+FFFD). A wrong text and a panic in an edit or a parse are
failures too, reported in `text` and in `panic.edit`, `panic.document`, `panic.fresh` or `panic.both`. Some sequences start from generated texts joined into a few
kilobytes, which `Document` stores differently from short texts; a `-corpus` of large files tests more of them.

The same is available from Go in package `editfuzz`, for example in a test that keeps saved failures as regression
cases:

```go
src, err := os.ReadFile("grammar.pego")
if err != nil {
	t.Fatal(err)
}
err = editfuzz.Fuzz(src, editfuzz.Options{Start: "main", Backend: pego.BytecodeIterative, Unit: pego.Bytes,
	Seed: 1, SaveFailures: t.TempDir()})
if err != nil {
	t.Fatal(err) // a *editfuzz.Failure, with the saved path
}
paths, _ := filepath.Glob("testdata/editfuzz/*.json")
for _, path := range paths {
	seq, err := editfuzz.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := editfuzz.Replay(src, seq); err != nil {
		t.Errorf("%s: %v", path, err)
	}
}
```

`Replay` returns a `*editfuzz.Mismatch` for a failure and an `*editfuzz.InvalidError` for a sequence that does not
apply; `Minimize` takes a failing sequence and an attempt limit. Callbacks of extensions may run a different
number of times in the two parses; only results and errors are compared.

## How It Works, and What It Cannot Do

The generator walks the grammar from the start rule, making a random decision at each ordered choice, repetition,
optional expression and Operators Rule, and searches for a way to finish the input that the parser will accept; it
then parses the candidate and retries if the parser rejects it. While it writes, it steers away from inputs that the
parser would read differently:

- after taking an alternative of an ordered choice, the earlier alternatives must not match there;
- after stopping a repetition or skipping an optional expression, the element must not match there (repetitions are
  greedy);
- `!e` must not match; for `&e`, a text for `e` is generated and the input must continue with it;
- predicates are evaluated on the generated text, with captures, variables, `len`, `text`, comparisons and arithmetic,
  so indentation (as in [examples/outline](../../examples/outline/outline.pego)) and matching tags (as in
  [parsers/xml](../../parsers/xml/xml.pego)) work;
- Operators Rule expressions are generated as chains of operands and operators, avoiding chains of `infix none` operators,
  with the outer infix/postfix tail filtered by level-restricted calls. Prefixes remain available at every operand
  position, including prefixes below the entry level. Their right operands can use operators tighter than the
  prefix's own level; coverage includes these RHS paths and the rules they call. Operators and nested targets
  that no reachable entry or prefix RHS permits are excluded. See the
  [prefix exception](operators-rule.md#level-restricted-calls) for the parser's contract.

After an already-used nonassociative operator closes its level, the stop check allows a caller to continue with
that token: `e "<" e` with `infix none "<"` can generate `a<a<a`. Other applicable infix and postfix operators still
prevent an early stop.

Nonassociative restrictions belong to each Operators Rule invocation, including prefix and infix right operands. Returning
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
unfinished future tail; its endpoint is only a witness. Unsupported cuts, predicates, Operators Rule references, left
recursion, recovery and exhausted matching remain inconclusive. Every returned input is still parsed and accepted.

What the generator cannot evaluate, it leaves to the parser: predicates over values built by actions or over struct
fields, and lookaheads into Operators Rules or left-recursive rules. Those constructs are handled only by generating
and checking, which costs attempts; a grammar where most choices depend on them yields fewer inputs per second and
lower coverage. In practice:

- **No input found** (`sample.ErrNoInput`, or `pego sample` fails): the error counts what became of the attempts.
  Check that some input parses at all: the grammar may accept nothing, or every input may fail an action at run time
  (candidates rejected by the parser). If the inputs it needs are long (searches out of budget), raise the budget
  (`WithBudget`) or, for long repetitions, `WithMaxRepeat`; if they are merely rare, raise `WithAttempts`.
- **Few distinct inputs**: the language may be small (`"a" / "b"` has two inputs). `Generate` stops after
  `WithAttempts` (100) inputs in a row that it already returned.
- **Missed alternatives** that should be reachable: an earlier alternative of the choice may match their text (as with
  `word` and `"true"` above), or reaching them may need many predicates to hold at once. More inputs or other seeds
  help with the latter.
- **Inputs are not uniform** over the language and not minimized: they are test data, not a statistical sample.
