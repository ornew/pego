# Keep the Last Good Tree While the User Types

**Problem.** An editor shows something built from the tree of the text (an outline, highlighting, a list of symbols),
and the tree is rebuilt after every keystroke. Most states of a half-typed line are syntax errors, which return no tree.
You want to keep showing the last tree that parsed, and to see how little work each keystroke costs.

Wrap a `Document` in a small `Editor` type. It applies each change, reparses, keeps the last tree that parsed, and
reports what the parse cost. For a grammar that recovers from errors and returns a tree for every keystroke, see
[Reparse a document on every keystroke](as-you-type.md) instead. The program here simulates a user who opens a blank
line in the middle of a 1,000-line settings file and types a new setting one key at a time, with an error after almost
every key, and then retypes a value:

```go
// main.go
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
type Key = { Text string }
type Value = { Text string }

rule main =  (setting / blank)* $$
rule setting Setting =  k:key " = " v:value "\n" => Setting{Key: $k, Value: $v}
rule key Key =  t:([a-z_]+) #atomic => Key{Text: text($t)}
rule value Value =  t:([0-9]+) #atomic => Value{Text: text($t)}
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
		status = fmt.Sprintf("ok, %d entries", node.Elem(0).Len())
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
	fmt.Printf("%-18s evaluated %4d reused %4d  ok, %d entries\n", "(first parse)", doc.Stats().Evaluated, doc.Stats().Reused, node.Elem(0).Len())
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

```text
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

## How It Works

- Every keystroke is one `Edit` and one `Parse`. While the line is incomplete, `Parse` returns a `*SyntaxError` and no
  tree, so the editor keeps the last good tree. The first 499 settings are still reused (the parser stops at the first
  error and does not touch the rest); the cost is the same 3 evaluations.
- When the line becomes valid (after `3`), everything after it is reused again: 1,002 reuses, 3 evaluations.
- The last line of the output checks the contract: the incremental tree equals a fresh parse of the same text (the
  comparison here is by JSON, which includes positions).

## Variations and Limits

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

See [Incremental Parsing](../guide/incremental.md) for the `Document` API and
[Incremental Parsing Performance](../guide/incremental-performance.md) for what reuse costs on other grammars.
