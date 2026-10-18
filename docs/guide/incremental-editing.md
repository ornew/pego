# Editor Loops and Edit Fuzzing

A `Document` is meant to be driven by an editor, and its one rule is that its result equals a fresh parse. This guide
shows an editor loop that wraps a `Document`, and the tool that checks the rule for a grammar with random edits. It
assumes the `Document` API of [Incremental Parsing](incremental.md).

## An Editor Loop

An `Editor` wraps a `Document`, applies each change, reparses, keeps the last tree that parsed, and reports what the
parse cost. It simulates a user opening a blank line in the middle of a 1,000-line settings file and typing a new setting
one key at a time, with an error after almost every key, and then retyping a value:

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ornew/pego"
)

const grammar = `package settings

type Setting = { Key Key; Value Value }
type Key terminal
type Value terminal

rule main =  (setting / blank)* $$
rule setting Setting =  k:key " = " v:value "\n" => Setting{Key: $k, Value: $v}
rule key Key =  [a-z_]+ #atomic
rule value Value =  [0-9]+ #atomic
rule blank =  "\n" `

// lineStart returns the position, in code points, of the first character of the 1-based line.
func lineStart(text string, line int) int {
	pos := 0
	for _, r := range text {
		if line == 1 {
			break
		}
		pos++
		if r == '\n' {
			line--
		}
	}
	return pos
}

// Editor keeps a Document and the last tree that parsed.
type Editor struct {
	doc  *pego.Document
	last *pego.Node
}

// Apply replaces [start, end) with text and reparses.
func (e *Editor) Apply(start, end int, text string) error {
	if err := e.doc.Edit(start, end, text); err != nil {
		return err
	}
	node, err := e.doc.Parse()
	st := e.doc.Stats()
	if node != nil {
		e.last = node // a tree, possibly with recovered errors
	}
	var status string
	switch {
	case err == nil:
		status = fmt.Sprintf("ok, %d entries", len(node.Children[0].Children))
	default:
		var se *pego.SyntaxError
		if errors.As(err, &se) {
			status = fmt.Sprintf("error at %d:%d, keeping the last good tree", se.Line, se.Col)
		} else {
			status = "error: " + err.Error()
		}
	}
	fmt.Printf("%-18q evaluated %4d reused %4d  %s\n", text, st.Evaluated, st.Reused, status)
	return nil
}

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var b strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "key_%c = %d\n", 'a'+i%26, i)
	}
	doc, err := p.NewDocument(b.String())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	node, err := doc.Parse()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%-18s evaluated %4d reused %4d  ok, %d entries\n", "(first parse)", doc.Stats().Evaluated, doc.Stats().Reused, len(node.Children[0].Children))
	ed := &Editor{doc: doc, last: node}

	// Open a blank line before line 500, then type a new setting into it, one keystroke at a time.
	pos := lineStart(doc.Text(), 500)
	if err := ed.Apply(pos, pos, "\n"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, ch := range "timeout = 30" {
		if err := ed.Apply(pos, pos, string(ch)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		pos++
	}
	// Change the value 30 to 45 with a single replacement of two characters.
	if err := ed.Apply(pos-2, pos, "45"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// The incremental result is the same as a fresh parse.
	got, _ := json.Marshal(ed.last)
	fresh, err := p.Parse(doc.Text())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	want, _ := json.Marshal(fresh)
	fmt.Println("same as a fresh parse:", string(got) == string(want))
}
```

```
(first parse)      evaluated 3003 reused    0  ok, 1000 entries
"\n"               evaluated    4 reused 1001  ok, 1001 entries
"t"                evaluated    3 reused  499  error at 500:2, keeping the last good tree
"i"                evaluated    3 reused  499  error at 500:3, keeping the last good tree
"m"                evaluated    3 reused  499  error at 500:4, keeping the last good tree
"e"                evaluated    3 reused  499  error at 500:5, keeping the last good tree
"o"                evaluated    3 reused  499  error at 500:6, keeping the last good tree
"u"                evaluated    3 reused  499  error at 500:7, keeping the last good tree
"t"                evaluated    3 reused  499  error at 500:8, keeping the last good tree
" "                evaluated    3 reused  499  error at 500:8, keeping the last good tree
"="                evaluated    2 reused  500  error at 500:8, keeping the last good tree
" "                evaluated    3 reused  500  error at 500:11, keeping the last good tree
"3"                evaluated    3 reused 1002  ok, 1001 entries
"0"                evaluated    3 reused 1002  ok, 1001 entries
"45"               evaluated    3 reused 1002  ok, 1001 entries
same as a fresh parse: true
```

What to see in the output:

- Every keystroke is one `Edit` and one `Parse`. While the line is incomplete, `Parse` returns a `*SyntaxError` and no
  tree, so the editor keeps the last good tree. The first 499 settings are still reused (the parser stops at the first
  error and does not touch the rest); the cost is the same 3 evaluations.
- When the line becomes valid (after `3`), everything after it is reused again: 1,002 reuses, 3 evaluations.
- The last line of the output checks the contract: the incremental tree equals a fresh parse of the same text (the
  comparison here is by JSON, which includes positions).

Adapting it to a real editor:

- Keep one `Document` per open file and one goroutine per `Document`, with the `Parser` shared.
- Convert your editor's positions to the document's unit before `Edit`. In `Bytes` mode, a position inside a character
  is an error rather than a silent corruption.
- If the grammar uses `#recover`, `Parse` returns a tree and a `pego.SyntaxErrors` together: keep the tree, show the
  errors.
- Debounce: reparse after the user pauses, not after every key, and merge the edits that arrived meanwhile.
- The kept tree shares its nodes with the document, and later parses move the nodes they reuse to their new
  positions. If the editor needs the last good tree exactly as it was (for example, to map positions in the text it
  was parsed from), keep `node.Clone()` instead.
- If something looks wrong, compare with `Parser.Parse` on `doc.Text()`. The two must agree.

## Fuzzing Edits

That last rule can be checked mechanically for any grammar. `pego edit-fuzz` applies random sequences of edits, undos
and redos to a `Document` and, at the initial text and at each checkpoint, compares its result with a fresh parse of
`doc.Text()`: values with their full spans, and errors with their positions, expectations and messages. It runs
every backend in both position units:

```bash
$ pego edit-fuzz -g examples/calculator/calc.pego -seed 1
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
$ pego edit-fuzz -g calc.pego -save-failures failures -minimize
pego: calc.pego (seed 0): closure/codepoints: document result differs from a fresh parse at operation 21 (text "…") in error.error.col:
document: 1
fresh:    2
saved to failures/editfuzz-0be4ccb60eadff90.json
minimized to 4 operations in failures/editfuzz-9d0bcab55578f1b5.json
$ pego edit-fuzz -g calc.pego -replay failures/editfuzz-9d0bcab55578f1b5.json
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

## Related Topics

- [Incremental Parsing](incremental.md): the model and the API.
- [Sampling Inputs and Fuzzing](sampling-and-fuzzing.md): generating inputs for a grammar.
- [Error Messages](error-messages.md): `@error` and the shape of `SyntaxError`.
- [Recovering from Errors](recovery.md): `#recover` and recovered errors.
