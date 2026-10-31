# What Incremental Parsing Reuses

An edit leaves most memo entries of a `Document` valid, and which ones survive depends on how far each rule looked at the
input. This guide states exactly what is kept and why, and how to write a grammar so that an edit costs little. It
assumes the `Document` API of [Incremental Parsing](incremental.md).

## What Is Reused, and Why

For an edit of `[start, end)`, and a memo entry that examined `[from, examined)`:

| Entry | After the edit |
|:--|:--|
| `examined <= start` (all of it lies before the edit) | Kept as is |
| `from >= end` (all of it lies after the edit) | Kept, with positions moved by the change in length (when it is reused, its nodes are moved in place) |
| anything else (it overlaps the edit) | Dropped |

Consequences, all of which can be read off the numbers in the next section:

- **Reuse is per rule call.** Only rule results are memoized, so the unit of reuse is a rule. A grammar written as one
  big rule has nothing to reuse. `Document` memoizes every rule call, even those an ordinary `Parse` does not (rules that
  call no rule, and rules referenced only once), precisely so that these small rules can be reused. The exception is the
  rules of a left-recursive cycle other than its leader, the rule that grows the seed: their results depend on the
  growing match, so no parse memoizes them.
- **Every ancestor of the edit runs again**, because it examined the edit. For a flat list of lines that is `main`, the
  line, and the rule inside it that contains the change: 3 evaluations, however large the file.
- **Looking ahead widens the range.** The range includes the character after a token (the check that ended a `+` or
  `*`), the characters examined by `&e` and `!e`, and, for `^` (beginning of line), the character before the match.
  Usually that is one character and harmless. A rule that looks far ahead is invalidated by edits far away.
- **Shifting is lazy and cheap.** `Edit` touches only the entries at the edited positions; every other entry is
  brought up to date (kept, shifted or dropped by the rules above, edit by edit) when it is next looked up.
- **Positional values block shifting.** A node's `Start` and `End` can be moved, but a position stored in an `int`
  field (`.Start`, `.End` in an action or predicate) cannot. The rules that use them, and the rules that call those
  rules, are re-evaluated whenever they lie after an edit.
- **Recovered errors block shifting.** An entry that contains a recovered error is dropped when it lies after the edit,
  because the error message contains its position. It is one entry per bad line.
- **Variables are fine.** Rules that read [variables](../../spec/language/predicates.md#interaction-with-memoization) are memoized
  per combination of variable values and reused like any other (see the table below).

**Long repetitions resume.** The rule that contains the edit runs again, and with it any repetition in it, such as the
`line*` of a whole file. Run element by element, that would be a memo lookup per line, so a `Document` also records the
run of every repetition of 16 elements or more: where each element started and ended, the range it examined, its value
and its expectations. When the repetition runs again after one edit, the elements before the edit are taken as they
are, the parse continues from there, and as soon as an element ends where an old element after the edit began (moved by
the edit), the rest of the old run is taken too, its nodes moved in place. Each reused element counts in `Reused`. The
conditions are those of memo entries, applied per element; in addition, a repetition is not resumed when an element has
a Predicate Action (`{...}`) or calls a rule that reads variables, when its run recovered an error or used a left recursion
that was still growing, and its elements are not moved past an edit that changed the length when their values may
contain positions. Every backend resumes repetitions.

Calling `Parse` again without an edit keeps the current generation's repetition records, even when the start
rule returns entirely from the memo. It does not consume the next edit's opportunity to resume. Records are
still eligible only across one edit; two edits without an intervening parse do not promote an older run.
An aborted parse and an edit-log reset discard the records together with the memo.

## Writing Grammars That Reuse Well

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

	"github.com/ornew/zeroparse"
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
	{"one rule per line", `@start
rule main =  (line / blank)* $$
rule line =  name " = " num "\n"
rule name =  [a-z_]+ #atomic
rule num =  [0-9]+ #atomic
rule blank =  "\n" `, lines(-1)},

	{"line start anchor ^", `@start
rule main =  (line / blank)* $$
rule line =  ^ name " = " num "\n"
rule name =  [a-z_]+ #atomic
rule num =  [0-9]+ #atomic
rule blank =  "\n" `, lines(-1)},

	{"variable read in each line", `var x int
@start
rule main = {x := 1} (line / blank)* $$
rule line =  name " = " num {x == 1} "\n"
rule name =  [a-z_]+ #atomic
rule num =  [0-9]+ #atomic
rule blank =  "\n" `, lines(-1)},

	{".Start in an action", `type At = { Pos int; Text node }
@start
rule main =  (line / blank)* $$
rule line =  name " = " n:num "\n"
rule name =  [a-z_]+ #atomic
rule num At =  t:[0-9]+ #atomic => At{Pos: $t.Start, Text: $t}
rule blank =  "\n" `, lines(-1)},

	{"right-recursive list", `@start
rule main =  lines $$
rule lines =  line lines?
rule line =  name " = " num "\n"
rule name =  [a-z_]+ #atomic
rule num =  [0-9]+ #atomic `, lines(-1)},

	{"lookahead to the end of text", `@start
rule main =  (line / blank)* $$
rule line =  name " = " num "\n" &([^!]* ($$ / "!"))
rule name =  [a-z_]+ #atomic
rule num =  [0-9]+ #atomic
rule blank =  "\n" `, lines(-1)},

	{"#recover, bad line 900", `@start
rule main =  (line / blank)* $$
rule line =  (name " = " num "\n") #recover(skip=[^\n]* "\n")
rule name =  [a-z_]+ #atomic
rule num =  [0-9]+ #atomic
rule blank =  "\n" `, lines(900)},

	{"everything in one rule", `@start
rule main =  ([a-z_]+ #atomic " = " [0-9]+ #atomic "\n")* $$ `, lines(-1)},
}

func main() {
	fmt.Printf("%-28s %-20s %s\n", "grammar", "first parse", "after editing line 500")
	for _, v := range variants {
		p, err := zp.CompileSource(v.grammar, "main")
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

```text
grammar                      first parse          after editing line 500
one rule per line            evaluated 3003     evaluated 3     reused 1001
line start anchor ^          evaluated 3003     evaluated 3     reused 1001
variable read in each line   evaluated 3003     evaluated 3     reused 1001
.Start in an action          evaluated 3003     evaluated 1004  reused 1001
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
- **Avoid `.Start` and `.End` in rules that appear in every line.** Here the edit made every `line` after it, and
  its `num`, run again (1,004 evaluations). Take positions from the nodes instead (`Node.Start()`, `Node.End()`, shifted
  for you) when you walk the tree.
- **Avoid lookahead that can reach the end of the text.** Here every line examines up to the end of the document, so
  every line before the edit is dropped (502 evaluations, half the document).
- Everything in one rule (the last row) has "1 evaluation", and its repetition resumes: 999 of the 1,000 lines are
  reused, and only the edited one is parsed again. That works because the repetition is flat; anything nested inside
  a line (a rule-less expression, a list) is parsed again with its line. Rules remain the unit of reuse everywhere else,
  and `Stats` counts rule bodies, not work: judge a grammar by time as well.

## Related Topics

- [Incremental Parsing](incremental.md): the model and the API.
- [Incremental Parsing Performance](incremental-performance.md): what reuse saves in time.
- [Linting Grammars](linting.md): the `hint` findings that point at grammars that reuse poorly.
