# Fuzz the Edits of a Grammar in Your Tests

**Problem.** An editor reparses a `Document` after every keystroke, and the result must always be what a fresh parse of
the same text gives. Incremental reuse is where a grammar with state (a variable, a predicate that reads one) can go
wrong in ways that a few hand-written edits never reach. You want a test that applies thousands of edits, undos and
redos, a regression test for each failure it finds, and a place to write down a case you thought of yourself.

The `editfuzz` package does what `pego edit-fuzz` does, inside `go test`. It applies random sequences of edits to a
`Document`, and at each checkpoint compares its result with a fresh parse of `doc.Text()`: the values with their spans,
and the errors with their positions and messages. The grammar here is the indentation grammar of
[Parse indentation-based input](indentation.md), which keeps the current depth in a variable. That is the kind of
grammar where a reused result can disagree with a fresh one.

```pego
// nested.pego
type Entry = {
    Key      node[string]
    Value    *node[string]
    Children []Entry
}

rule main = {indent := 0} es:block blank* $$ => $es

// The entries at the current indentation.
rule block = v1:(blank* e:entry)+ => map($v1, (x) => x.$e)

// A line indented exactly at the current depth, with the lines below it that are indented deeper.
rule entry Entry = (
    s:spaces {len($s) == indent} @error(message="inconsistent indentation")
    k:key ":" " "* v:value? eol cs:children?
) => Entry{Key: $k, Value: $v, Children: concat($cs)}

// If the next line is indented deeper, read it and its siblings with that depth as the new indentation.
// A variable is visible only in the rule that defines it and the rules it calls, so when this rule returns, the
// parent's indentation is back.
rule children = &(blank* s:spaces) {len($s) > indent} {indent := len($s)} b:block => $b

rule spaces = " "* #atomic
rule key = [a-z_]+ #atomic
rule value = [^\n]+ #atomic
rule eol = "\n" / $$
rule blank = [ \t]* "\n"
```

The files that the edits start from are real inputs of the format:

```text
// testdata/corpus/settings.txt
server:
  host: example.com
  port: 8080
  tls:
    cert: /etc/cert.pem
    key: /etc/key.pem
logging:
  level: info
name: demo
```

The test fuzzes every backend in both position units, replays the failures that were saved earlier, and replays one
case written by hand:

```go
// edits_test.go
package nested

import (
	_ "embed"
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/editfuzz"
)

//go:embed nested.pego
var grammar []byte

// corpus reads the real files that the fuzzer starts its edits from.
func corpus(t *testing.T) [][]byte {
	t.Helper()
	paths, err := filepath.Glob("testdata/corpus/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	var texts [][]byte
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, data)
	}
	return texts
}

// Random edits, undos and redos: after each checkpoint, the Document must give the result of a fresh parse.
func TestEditsMatchFreshParses(t *testing.T) {
	texts := corpus(t)
	backends := []struct {
		name    string
		backend pego.Backend
	}{{"closure", pego.Closure}, {"bytecode", pego.Bytecode}, {"bytecode-iterative", pego.BytecodeIterative}}
	units := []struct {
		name string
		unit pego.PositionUnit
	}{{"codepoints", pego.CodePoints}, {"bytes", pego.Bytes}}
	for _, b := range backends {
		for _, u := range units {
			t.Run(b.name+"/"+u.name, func(t *testing.T) {
				err := editfuzz.Fuzz(grammar, editfuzz.Options{
					Start: "main", Backend: b.backend, Unit: u.unit, Seed: 1, Corpus: texts,
					Sequences:    200,
					SaveFailures: "testdata/editfuzz", // a failing sequence becomes a file to commit
					Minimize:     true,
				})
				if err != nil {
					t.Fatal(err) // a *editfuzz.Failure, with the path of the saved sequence
				}
			})
		}
	}
}

// Every saved failure stays a regression test.
func TestSavedSequences(t *testing.T) {
	paths, err := filepath.Glob("testdata/editfuzz/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		seq, err := editfuzz.Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := editfuzz.Replay(grammar, seq); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

// A case written by hand: dedent a nested line, undo it, and redo it, checking after every step.
func TestDedent(t *testing.T) {
	seq := &editfuzz.Sequence{
		GrammarSHA256: editfuzz.GrammarHash(grammar),
		Start:         "main",
		Backend:       pego.Closure,
		Initial:       []byte("a:\n  b: 1\n  c: 2\nd: 3\n"),
		Operations: []editfuzz.Operation{
			{Kind: editfuzz.Edit, Start: 3, End: 5, Text: nil}, // "  b: 1" loses its indentation
			{Kind: editfuzz.Checkpoint},
			{Kind: editfuzz.Undo},
			{Kind: editfuzz.Checkpoint},
			{Kind: editfuzz.Redo},
			{Kind: editfuzz.Checkpoint},
		},
	}
	if err := editfuzz.Replay(grammar, seq); err != nil {
		t.Fatal(err)
	}
}
```

Run it with `go test -v`:

```bash
go test -v
```

## Expected Output

```text
=== RUN   TestEditsMatchFreshParses
=== RUN   TestEditsMatchFreshParses/closure/codepoints
=== RUN   TestEditsMatchFreshParses/closure/bytes
=== RUN   TestEditsMatchFreshParses/bytecode/codepoints
=== RUN   TestEditsMatchFreshParses/bytecode/bytes
=== RUN   TestEditsMatchFreshParses/bytecode-iterative/codepoints
=== RUN   TestEditsMatchFreshParses/bytecode-iterative/bytes
--- PASS: TestEditsMatchFreshParses (2.03s)
    --- PASS: TestEditsMatchFreshParses/closure/codepoints (0.33s)
    --- PASS: TestEditsMatchFreshParses/closure/bytes (0.30s)
    --- PASS: TestEditsMatchFreshParses/bytecode/codepoints (0.36s)
    --- PASS: TestEditsMatchFreshParses/bytecode/bytes (0.33s)
    --- PASS: TestEditsMatchFreshParses/bytecode-iterative/codepoints (0.36s)
    --- PASS: TestEditsMatchFreshParses/bytecode-iterative/bytes (0.34s)
=== RUN   TestSavedSequences
--- PASS: TestSavedSequences (0.00s)
=== RUN   TestDedent
--- PASS: TestDedent (0.00s)
PASS
ok  	example.com/nested	2.282s
```

Nothing was saved, because nothing failed: `testdata/editfuzz` is created only when a sequence fails. The times differ
from run to run.

## How It Works

- `Fuzz` runs `Sequences` sequences of `Operations` operations each, from a seed. The initial texts are the files of the
  corpus, so the edits split, join and break lines of real input; without a corpus, they are inputs generated from the
  grammar. A fixed seed makes the run repeatable for a given release, but the seed is not the reproducer.
- A failure is a `*editfuzz.Failure`. With `SaveFailures`, the sequence is written as a JSON file that records the
  exact text and replacements in base64, and the hash of the grammar; with `Minimize`, a shorter sequence that fails the
  same way is saved next to it. Commit that file in `testdata/editfuzz`, and `TestSavedSequences` replays it from then
  on, even after the generator changes.
- `Replay` runs a `Sequence`, so a case can also be written as Go, as `TestDedent` does: the text, the edits with their
  ranges in the unit of the sequence, and a checkpoint wherever the result must equal a fresh parse. `Undo` and `Redo`
  restore the text of the latest edit and reapply it.
- `go test` caches a passing run. Add `-count=1` to run the fuzzer again, and change `Seed` to explore other sequences.

## Variations and Limits

- Start with `pego edit-fuzz -g nested.pego -corpus testdata/corpus` on the command line; it runs the same checks, and
  `-replay` runs a saved sequence. See [Fuzzing edits](../guide/sampling-and-fuzzing.md#fuzzing-edits).
- A grammar that is only edited in one unit needs only that unit; the bytes unit finds the edits that split a multibyte
  character, so test it for input that is not ASCII.
- Callbacks of extensions may run a different number of times in the incremental and the fresh parse. Only the results and
  the errors are compared, not the calls.
- To make incremental reuse cheaper, not only correct, see [Writing grammars that reuse well](../guide/incremental-reuse.md#writing-grammars-that-reuse-well).
