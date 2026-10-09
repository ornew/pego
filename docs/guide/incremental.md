# Incremental parsing

`Parser.Parse` parses a whole text from scratch. An editor, a language server or a live preview parses a text that
changes a little at a time, and most of each new parse repeats the last. A `Document` holds the text and the memo table
of its last parse, applies edits, and on `Parse` runs only the parts of the grammar that the edits touched. The result
is the same as parsing the new text from scratch.

|  |  |
|:--|:--|
| Input | A text in memory that is edited again and again |
| API | `Parser.NewDocument`, `Document.Edit`, `Document.Parse`, `Document.Stats` |
| Grammar | Any grammar (some are much faster to reparse than others) |
| You get | A full tree for every version of the text |
| Saves | Time: unchanged parts are not parsed again |

A `Document` works with every backend ([Runtime](runtime.md)) and with both position units. It does not work with
`RecognizeOnly` (`NewDocument` returns an error) or with parsers produced by `GenerateGo`. If the problem is the
opposite one, an input too big to hold, see [Streaming](streaming.md): a `Document` is always parsed as a whole and
`#stream` has no effect on it, so the two do not combine.

The design is in [design record 007](../design/007-streaming-and-incremental-parsing.md).

- [The model](#the-model)
- [The API](#the-api)
- [A walkthrough with `Stats`](#a-walkthrough-with-stats)
- [What is reused, and why](#what-is-reused-and-why)
- [Writing grammars that reuse well](#writing-grammars-that-reuse-well)
- [Performance and limits](#performance-and-limits)
- [Recipe: an editor loop](#recipe-an-editor-loop)
- [See also](#see-also)

## The model

A `Document` is a text plus the memo table of its last parse. The memo table has an entry for every rule call: the rule,
the position, and the result, together with **the range of input the call looked at** (the matched text, plus every
character that was examined to decide, such as the character after a `+` that ended it, and lookahead). When the text is
edited, an entry stays valid if the edit lies outside the range it examined, and is dropped otherwise. The next `Parse`
runs the grammar from the start rule as usual, but each rule call that finds a valid entry returns its result without
running. A long repetition is not even run element by element: it **resumes** its run from the last parse (see
[What is reused, and why](#what-is-reused-and-why)). The result is always the same as parsing the new text from scratch (a randomized test in
`internal/engine/document_test.go` checks the node tree, the positions and the syntax errors against a fresh parse after
each of 2,000 random edits, on every backend).

## The API

| | |
|:--|:--|
| `p.NewDocument(text, opts...)` | A document holding `text`. Options: `WithUnit` (also the unit of `Edit`) and `WithBackend`. Fails for `RecognizeOnly`, for an unavailable backend (`Closure` on a parser saved without its AST), and for an unknown start rule. Nothing is parsed yet. |
| `doc.Edit(start, end, text)` | Replace `[start, end)` with `text`, in the document's unit. `start == end` inserts, an empty `text` deletes. An error if the range is outside the text, and with `Bytes`, if `start` or `end` is inside a character. Does not parse. |
| `doc.Parse()` | Parse the current text. Results and errors are those of `Parser.Parse`, including `SyntaxErrors` returned with a tree for recovered errors. |
| `doc.Stats()` | `ParseStats{Evaluated, Reused}` of the last `Parse`: rule bodies run, and results used again (memo results and the elements of resumed repetitions). |
| `doc.Text()` | The current text. |

A `Document` is not safe for concurrent use; a `Parser` is, so use one `Document` per goroutine and share the `Parser`.
Several `Edit` calls can precede one `Parse`.

An action error, a nesting-limit error, or a panic in a trace callback interrupts
the parse. A `Document` discards its memo and repetition caches after such an
interruption, because partial results can depend on unfinished left-recursion
seeds. The text and options stay available for retry or editing; trace panics
still propagate to the caller. Ordinary syntax errors and recovered errors are
completed parse outcomes and keep their reusable caches.

## A walkthrough with `Stats`

```go
package main

import (
	"fmt"
	"log"

	"github.com/ornew/pego"
)

const grammar = `
def main = line* $$
def line = (?a-z)+ "\n"
`

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		log.Fatal(err)
	}
	doc, err := p.NewDocument("ab\ncd\nef\n")
	if err != nil {
		log.Fatal(err)
	}

	step := func(what string) {
		node, err := doc.Parse()
		if err != nil {
			fmt.Printf("%-28s %+v  error: %v\n", what, doc.Stats(), err)
			return
		}
		fmt.Printf("%-28s %+v  %v\n", what, doc.Stats(), node)
	}

	step("first parse")
	step("parse again")
	doc.Edit(3, 4, "x") // "ab\nxd\nef\n"
	step("edit inside line 2")
	doc.Edit(0, 0, "zz\n") // "zz\nab\nxd\nef\n"
	step("insert a line at the start")
	doc.Edit(0, 3, "") // delete it again
	step("delete it again")
	doc.Edit(1, 2, "!") // "a!\nxd\nef\n"
	step("break line 1")
}
```

```
first parse                  {Evaluated:5 Reused:0}  (Seq [(Seq ["a" "b"] "\n")@line (Seq ["c" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
parse again                  {Evaluated:0 Reused:1}  (Seq [(Seq ["a" "b"] "\n")@line (Seq ["c" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
edit inside line 2           {Evaluated:2 Reused:3}  (Seq [(Seq ["a" "b"] "\n")@line (Seq ["x" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
insert a line at the start   {Evaluated:2 Reused:4}  (Seq [(Seq ["z" "z"] "\n")@line (Seq ["a" "b"] "\n")@line (Seq ["x" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
delete it again              {Evaluated:0 Reused:1}  (Seq [(Seq ["a" "b"] "\n")@line (Seq ["x" "d"] "\n")@line (Seq ["e" "f"] "\n")@line])@main
break line 1                 {Evaluated:2 Reused:0}  error: 1:2: syntax error: expected "\n", (?a-z)
```

- **First parse: 5 evaluations.** `main`, three `line`s and the fourth attempt at `line`, which fails at the end of the
  text. Failures are memoized too.
- **Parsing again: 0 evaluations, 1 reuse.** The entry for `main` at position 0 is valid, so the start rule is not even
  run.
- **Editing inside line 2** (`[3, 4)` replaced): 2 evaluations (`main` and the second `line`) and 3 reuses. The first
  line examined only `[0, 3)`, which ends where the edit starts, so it is kept as is. The third line and the failing
  attempt at the end lie after the edit, so they are kept with their positions shifted by the length change (zero here).
  `main` examined the whole text and the second line contains the edit, so both run again.
- **Inserting a line at the start:** `main` and the new line run; the three old lines and the failing attempt at the
  end are reused at their new positions.
- **Deleting it again: 0 evaluations.** The entry for the old `main`, shifted away by the insertion, was shifted back by
  the deletion and is exactly right again. Entries after an edit survive even if the parse after it did not use them,
  so undoing an edit can cost nothing.
- **Breaking line 1:** the result is the syntax error, as from a fresh parse. Nothing after the error is examined, so the
  rest of the memo table is simply left for later.

Reused results are not copied. Unchanged subtrees before an edit are shared with the new tree as they are, and
subtrees after it are shared too, **moved in place** to their new positions. A tree returned by an earlier `Parse`
therefore changes: the nodes the new parse reused carry their new positions. To keep a tree as it was (for example,
to keep showing the last good state), take a `Clone` before the next `Parse`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ornew/pego"
)

func js(n *pego.Node) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func main() {
	p, _ := pego.CompileSource(`
def main = line* $$
def line = (?a-z)+ "\n"`, "main")
	d, _ := p.NewDocument(strings.Repeat("ab\n", 100))
	t1, _ := d.Parse()
	snapshot := t1.Clone()
	before := js(t1)
	d.Edit(0, 0, "zzzz\n")
	t2, _ := d.Parse()
	l1, l2 := t1.Children[0].Children, t2.Children[0].Children
	fmt.Println("line 50 shared:", l1[50] == l2[51], "now starts at", l2[51].Start)
	fmt.Println("earlier tree unchanged:", js(t1) == before)
	fmt.Println("snapshot unchanged:", js(snapshot) == before)
}
```

```
line 50 shared: true now starts at 155
earlier tree unchanged: false
snapshot unchanged: true
```

Pointer comparison tells which nodes the new tree reused from the old one (`l1[50] == l2[51]`); compare `Start`, `End`
and content to see what else is the same. After the next `Parse`, an earlier tree is a mix: the nodes the new tree
reused are at their new positions, the others at their old ones, so a child can even lie outside its parent's range.
Use it only for such comparisons, or keep a `Clone`, which copies the whole tree (keeping subtrees shared within it
shared) and costs about as much memory as the tree. `Parse` writes the nodes it moves, so do not read an earlier
tree while `Parse` runs.

## What is reused, and why

For an edit of `[start, end)`, and a memo entry that examined `[from, examined)`:

| Entry | After the edit |
|:--|:--|
| `examined <= start` (all of it lies before the edit) | Kept as is |
| `from >= end` (all of it lies after the edit) | Kept, with positions moved by the change in length (when it is reused, its nodes are moved in place) |
| anything else (it overlaps the edit) | Dropped |

Consequences, all of which can be read off the numbers in the next section:

- **Reuse is per rule call.** Only rule results are memoized, so the unit of reuse is a rule. A grammar written as one
  big rule has nothing to reuse. `Document` memoizes every rule call, even those an ordinary `Parse` does not (rules that
  call no rule, and rules referenced only once), precisely so that these small rules can be reused.
- **Every ancestor of the edit runs again**, because it examined the edit. For a flat list of lines that is `main`, the
  line, and the rule inside it that contains the change: 3 evaluations, however large the file.
- **Looking ahead widens the range.** The range includes the character after a token (the check that ended a `+` or
  `*`), the characters examined by `&e` and `!e`, and, for `^` (beginning of line), the character before the match.
  Usually that is one character and harmless. A rule that looks far ahead is invalidated by edits far away.
- **Shifting is lazy and cheap.** `Edit` touches only the entries at the edited positions; every other entry is
  brought up to date (kept, shifted or dropped by the rules above, edit by edit) when it is next looked up.
- **Positional values block shifting.** A node's `Start` and `End` can be moved, but a position stored in an `int`
  field (`startPos`, `endPos` in an action or predicate) cannot. The rules that use them, and the rules that call those
  rules, are re-evaluated whenever they lie after an edit.
- **Recovered errors block shifting.** An entry that contains a recovered error is dropped when it lies after the edit,
  because the error message contains its position. It is one entry per bad line.
- **Variables are fine.** Rules that read [variables](../../spec/predicates.md#interaction-with-memoization) are memoized
  per combination of variable values and reused like any other (see the table below).

**Long repetitions resume.** The rule that contains the edit runs again, and with it any repetition in it, such as the
`line*` of a whole file. Run element by element, that would be a memo lookup per line, so a `Document` also records the
run of every repetition of 16 elements or more: where each element started and ended, the range it examined, its value
and its expectations. When the repetition runs again after one edit, the elements before the edit are taken as they
are, the parse continues from there, and as soon as an element ends where an old element after the edit began (moved by
the edit), the rest of the old run is taken too, its nodes moved in place. Each reused element counts in `Reused`. The
conditions are those of memo entries, applied per element; in addition, a repetition is not resumed when an element has
a predicate (`[...]`) or calls a rule that reads variables, when its run recovered an error or used a left recursion
that was still growing, and its elements are not moved past an edit that changed the length when their values may
contain positions. Every backend resumes repetitions.

## Writing grammars that reuse well

The table shows what one edit costs after a first parse, for eight grammars of a 1,000-line `item_a = 0` file. The
program inserts a digit on line 500 and prints `Stats`:

<details>
<summary>The program</summary>

```go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/ornew/pego"
)

// A 1,000-line document of lines like "item_a = 0".
func lines(corrupt int) string {
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		if i == corrupt {
			b.WriteString("item_? = ?\n") // does not match line
			continue
		}
		fmt.Fprintf(&b, "item_%c = %d\n", 'a'+i%26, i)
	}
	return b.String()
}

var variants = []struct {
	name, grammar, text string
}{
	{"one rule per line", `
def main = (line / blank)* $$
def line = name " = " num "\n"
def name = @(?a-z_)+
def num = @(?0-9)+
def blank = "\n"`, lines(-1)},

	{"line start anchor ^", `
def main = (line / blank)* $$
def line = ^ name " = " num "\n"
def name = @(?a-z_)+
def num = @(?0-9)+
def blank = "\n"`, lines(-1)},

	{"variable read in each line", `
def main = [x = 1] (line / blank)* $$
def line = name " = " num [x == 1] "\n"
def name = @(?a-z_)+
def num = @(?0-9)+
def blank = "\n"`, lines(-1)},

	{"startPos in an action", `
type At struct { Pos int, Text Match }
def main = (line / blank)* $$
def line = name " = " n:num "\n"
def name = @(?a-z_)+
def num: At = t:@(?0-9)+ -> new At{Pos: $t.startPos, Text: $t}
def blank = "\n"`, lines(-1)},

	{"right-recursive list", `
def main = lines $$
def lines = line lines?
def line = name " = " num "\n"
def name = @(?a-z_)+
def num = @(?0-9)+`, lines(-1)},

	{"lookahead to the end of text", `
def main = (line / blank)* $$
def line = name " = " num "\n" &((?^!)* ($$ / "!"))
def name = @(?a-z_)+
def num = @(?0-9)+
def blank = "\n"`, lines(-1)},

	{"#recover, bad line 900", `
def main = (line / blank)* $$
def line = (name " = " num "\n") #recover(skip=(?^\n)* "\n")
def name = @(?a-z_)+
def num = @(?0-9)+
def blank = "\n"`, lines(900)},

	{"everything in one rule", `
def main = (@(?a-z_)+ " = " @(?0-9)+ "\n")* $$`, lines(-1)},
}

func main() {
	fmt.Printf("%-28s %-20s %s\n", "grammar", "first parse", "after editing line 500")
	for _, v := range variants {
		p, err := pego.CompileSource(v.grammar, "main")
		if err != nil {
			log.Fatal(err)
		}
		doc, err := p.NewDocument(v.text)
		if err != nil {
			log.Fatal(err)
		}
		doc.Parse() // a recovered error is returned together with the tree; ignore it here
		first := doc.Stats()

		// Insert a digit before the number on line 500.
		start := strings.Index(v.text, "item_")
		for i := 1; i < 500; i++ {
			start += strings.IndexByte(v.text[start:], '\n') + 1
		}
		pos := start + len("item_a = ") // the text is ASCII, so bytes are code points
		if err := doc.Edit(pos, pos, "9"); err != nil {
			log.Fatal(err)
		}
		doc.Parse()
		after := doc.Stats()
		fmt.Printf("%-28s evaluated %-8d evaluated %-5d reused %d\n", v.name, first.Evaluated, after.Evaluated, after.Reused)
	}
}
```

</details>

```
grammar                      first parse          after editing line 500
one rule per line            evaluated 3003     evaluated 3     reused 1001
line start anchor ^          evaluated 3003     evaluated 3     reused 1001
variable read in each line   evaluated 3003     evaluated 3     reused 1001
startPos in an action        evaluated 3003     evaluated 1004  reused 1001
right-recursive list         evaluated 4004     evaluated 503   reused 501
lookahead to the end of text evaluated 3003     evaluated 502   reused 1500
#recover, bad line 900       evaluated 3002     evaluated 4     reused 1001
everything in one rule       evaluated 1        evaluated 1     reused 999
```

- Give every unit you want to reuse its own rule: a line, a statement, a declaration, a list item. Three evaluations
  against a thousand lines is the target.
- **Use a repetition for a list, not recursion.** With `lines = line lines?`, the call at each line examines everything
  after it, so every call before the edit is dropped (503 evaluations for an edit in the middle). `line*` keeps the
  list flat, and only the one rule that contains the repetition runs again.
- `^`, variables and `#recover` cost nothing noticeable (`#recover` costs one extra evaluation for a bad line after the
  edit).
- **Avoid `startPos` and `endPos` in rules that appear in every line.** Here the edit made every `line` after it, and
  its `num`, run again (1,004 evaluations). Take positions from the nodes instead (`Node.Start`, `Node.End`, shifted
  for you) when you walk the tree.
- **Avoid lookahead that can reach the end of the text.** Here every line examines up to the end of the document, so
  every line before the edit is dropped (502 evaluations, half the document).
- Everything in one rule (the last row) has "1 evaluation", and its repetition resumes: 999 of the 1,000 lines are
  reused, and only the edited one is parsed again. That works because the repetition is flat; anything nested inside
  a line (a rule-less expression, a list) is parsed again with its line. Rules remain the unit of reuse everywhere else,
  and `Stats` counts rule bodies, not work: judge a grammar by time as well.

## Performance and limits

Measured with this program (Apple silicon, Go 1.27.1, averages over repeated runs; it edits one digit in the middle of
the text and checks that the incremental tree equals a fresh parse):

<details>
<summary>The benchmark program</summary>

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	"github.com/ornew/pego"
)

// Three grammars for a file of "key = value" lines.
var grammars = []struct {
	name, grammar string
	line          func(i int) string
}{
	{"settings, one rule per line", `
def main = (setting / blank)* $$
def setting = key " = " value "\n"
def key = @(?a-z_)+
def value = @(?^\n)+
def blank = "\n"`,
		func(i int) string { return fmt.Sprintf("key_%c = value %d\n", 'a'+i%26, i) }},

	{"calc (Pratt expressions)", `
def main = (assign / blank)* $$
def assign = n:name " = " e:expr "\n"
def name = @(?a-z_)+
def blank = "\n"
def expr = pratt {
    operand @(?0-9)+
    operand "(" x:expr ")" -> $x
    level { infix left "+" / "-" }
    level { infix left "*" / "/" }
}`,
		func(i int) string { return fmt.Sprintf("x_%c = 1+2*(3-%d)*4/5+6\n", 'a'+i%26, i%10) }},

	{"settings, everything in one rule", `
def main = (@(?a-z_)+ " = " @(?^\n)+ "\n" / "\n")* $$`,
		func(i int) string { return fmt.Sprintf("key_%c = value %d\n", 'a'+i%26, i) }},
}

// avg returns the average duration of n runs of f.
func avg(n int, f func()) time.Duration {
	start := time.Now()
	for i := 0; i < n; i++ {
		f()
	}
	return time.Since(start) / time.Duration(n)
}

// live returns how many MiB of heap are still reachable after build has run and the garbage was collected.
func live(build func() any) float64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	keep := build()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(keep)
	return float64(after.HeapAlloc-before.HeapAlloc) / (1 << 20)
}

func main() {
	for _, g := range grammars {
		p, err := pego.CompileSource(g.grammar, "main")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(g.name)
		for _, lines := range []int{1000, 10000, 100000} {
			var b strings.Builder
			for i := 0; i < lines; i++ {
				b.WriteString(g.line(i))
			}
			text := b.String()
			runs := 200000 / lines

			parse := avg(runs, func() { p.Parse(text) })
			first := avg(runs, func() {
				doc, _ := p.NewDocument(text)
				doc.Parse()
			})

			// Replace the first digit after "= " in the middle of the text, over and over.
			doc, _ := p.NewDocument(text)
			doc.Parse()
			pos := strings.Index(text[len(text)/2:], "= ") + len(text)/2 + 2 // the text is ASCII
			var edit, reparse time.Duration
			for i := 0; i < runs; i++ {
				start := time.Now()
				doc.Edit(pos, pos+1, string(rune('0'+i%10)))
				edit += time.Since(start)
				start = time.Now()
				doc.Parse()
				reparse += time.Since(start)
			}
			st := doc.Stats()

			// The incremental result must equal a fresh parse.
			got, _ := doc.Parse()
			want, _ := p.Parse(doc.Text())
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)

			fmt.Printf("  %6d lines: Parse %-8v NewDocument+Parse %-8v Edit %-8v reparse %-8v (evaluated %d, reused %d) equal=%v\n",
				lines, parse.Round(10*time.Microsecond), first.Round(10*time.Microsecond),
				(edit / time.Duration(runs)).Round(10*time.Microsecond), (reparse / time.Duration(runs)).Round(10*time.Microsecond),
				st.Evaluated, st.Reused, string(gotJSON) == string(wantJSON))
		}
		if g.name == grammars[0].name {
			var b strings.Builder
			for i := 0; i < 100000; i++ {
				b.WriteString(g.line(i))
			}
			text := b.String()
			tree := live(func() any { n, _ := p.Parse(text); return n })
			both := live(func() any {
				doc, _ := p.NewDocument(text)
				n, _ := doc.Parse()
				return []any{doc, n}
			})
			fmt.Printf("  memory at 100000 lines (%.1f MiB of text): tree from Parse %.0f MiB, Document with its tree %.0f MiB\n",
				float64(len(text))/(1<<20), tree, both)
		}
	}
}
```

</details>

```
settings, one rule per line
    1000 lines: Parse 230µs    NewDocument+Parse 580µs    Edit 0s       reparse 20µs     (evaluated 3, reused 1001) equal=true
   10000 lines: Parse 2.47ms   NewDocument+Parse 5.21ms   Edit 30µs     reparse 70µs     (evaluated 3, reused 10001) equal=true
  100000 lines: Parse 25.06ms  NewDocument+Parse 41.75ms  Edit 330µs    reparse 610µs    (evaluated 3, reused 100001) equal=true
  memory at 100000 lines (1.9 MiB of text): tree from Parse 27 MiB, Document with its tree 151 MiB
calc (Pratt expressions)
    1000 lines: Parse 1.8ms    NewDocument+Parse 2.24ms   Edit 10µs     reparse 30µs     (evaluated 3, reused 1002) equal=true
   10000 lines: Parse 15.55ms  NewDocument+Parse 18.74ms  Edit 40µs     reparse 190µs    (evaluated 3, reused 10002) equal=true
  100000 lines: Parse 158.72ms NewDocument+Parse 169.44ms Edit 360µs    reparse 1.85ms   (evaluated 3, reused 100002) equal=true
settings, everything in one rule
    1000 lines: Parse 170µs    NewDocument+Parse 180µs    Edit 0s       reparse 10µs     (evaluated 1, reused 999) equal=true
   10000 lines: Parse 1.74ms   NewDocument+Parse 2.51ms   Edit 40µs     reparse 100µs    (evaluated 1, reused 9999) equal=true
  100000 lines: Parse 19.91ms  NewDocument+Parse 25.23ms  Edit 360µs    reparse 620µs    (evaluated 1, reused 99999) equal=true
```

Reading the table:

- **Neither step grows much with the document.** The program replaces one digit, so nothing after the edit moves:
  the reparse takes the lines before and after the edit from the resumed `line*` and parses one line, about 0.6 ms at
  100,000 lines. An edit that changes the length also moves the nodes of every reused line after it in place, which is
  linear in the size of the document. `Edit` splices the text and its offset table, and in the memo table touches only
  the entries at the edited positions (the others are brought up to date when they are next looked up): about 0.3 ms
  at 100,000 lines, most of it copying the text.
- **The gain depends on how much a reused unit costs.** With the Pratt-expression lines, `Edit` and the reparse together
  take about 2 ms against 159 ms for a fresh parse at 100,000 lines. With the cheap `key = value` lines, about 1 ms
  against 25 ms. Everything in one rule gains as much: its repetition resumes.
- **The first parse is slower than `Parse`** (up to about 1.5 times with cheap rules, close to equal otherwise), because
  every rule call is memoized (an ordinary parse memoizes only calls that repeat).
- **Memory is a multiple of the tree.** The line starting "memory" shows the live heap for the first grammar at 100,000
  lines: the tree from `Parse` alone against a `Document` that also holds the memo table (an entry per rule call), the
  records of long repetitions (about 80 bytes per element), the text as code points (four bytes each) and an offset
  table next to the string.
- **Bursts of edits are fine.** An `Edit` costs little on its own, and entries that several edits affect are brought
  up to date once, when they are looked up. Merging adjacent changes into one `Edit` still saves copying the text.

A real grammar, [parsers/json](../../parsers/json/json.pego), on an array of 5,000 objects (283 KB). Run it from the
root of the repository:

<details>
<summary>The program</summary>

```go
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ornew/pego"
)

// Run it from the root of the repository: go run ./jsondoc parsers/json/json.pego
func main() {
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		log.Fatal(err)
	}

	// An array of 5,000 objects.
	var b strings.Builder
	b.WriteString("[\n")
	for i := 0; i < 5000; i++ {
		if i > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, `  {"id": %d, "name": "item %d", "tags": ["a", "b"]}`, i, i)
	}
	b.WriteString("\n]\n")
	text := b.String()
	fmt.Printf("%d bytes\n", len(text))

	start := time.Now()
	if _, err := p.Parse(text); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Parse:               ", time.Since(start).Round(time.Millisecond))

	doc, err := p.NewDocument(text)
	if err != nil {
		log.Fatal(err)
	}
	start = time.Now()
	if _, err := doc.Parse(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("first Document.Parse:", time.Since(start).Round(time.Millisecond), doc.Stats())

	// Change the last digit of "id": 2500.
	pos := strings.Index(text, `"id": 2500`) + len(`"id": 250`) // the text is ASCII
	start = time.Now()
	if err := doc.Edit(pos, pos+1, "7"); err != nil {
		log.Fatal(err)
	}
	edit := time.Since(start)
	start = time.Now()
	if _, err := doc.Parse(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Edit:                ", edit.Round(10*time.Microsecond))
	fmt.Println("reparse:             ", time.Since(start).Round(10*time.Microsecond), doc.Stats())
}
```

</details>

```
$ go run ./jsondoc parsers/json/json.pego
282783 bytes
Parse:                16ms
first Document.Parse: 31ms {235006 10001}
Edit:                 60µs
reparse:              1.2ms {11 15016}
```

The full parse evaluates 235,006 rule bodies; after the edit 11 run and 15,016 results are reused.

Limits to keep in mind:

- Positions are in code points (default) or bytes, never UTF-16 units. An editor protocol that counts UTF-16 units needs
  a conversion, or a `Document` in `Bytes` mode with its own mapping.
- The whole text and the whole memo table are in memory. There is no way to drop old entries other than editing.
- `Parse` returns a complete tree each time; there is no API for a list of changed nodes beyond comparing trees.
- Not available with `RecognizeOnly`, with [streams](streaming.md), or in generated Go parsers.
- A `Document` is single-threaded. Parse in the goroutine that owns it.

## Recipe: an editor loop

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

const grammar = `
package settings

type Setting struct { Key Key, Value Value }
type Key terminal
type Value terminal

def main = (setting / blank)* $$
def setting: Setting = k:key " = " v:value "\n" -> new Setting{Key: $k, Value: $v}
def key: Key = @(?a-z_)+
def value: Value = @(?0-9)+
def blank = "\n"
`

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

## See also

- [Streaming](streaming.md): input too big to hold.
- [Linting grammars](linting.md): the `hint` findings that point at grammars that reuse poorly.
- [Errors and recovery](errors-and-recovery.md): `#error`, `#recover` and the shape of `SyntaxError`.
- [Runtime](runtime.md): backends, position units and the other parse options.
- [docs/performance.md](../performance.md): the performance log, including the Document hotspot.
- [Design record 007](../design/007-streaming-and-incremental-parsing.md).
