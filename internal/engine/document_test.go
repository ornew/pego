package engine

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// incrementalGrammar is a grammar with left recursion, a Pratt expression, line starts (which
// examine the preceding character), captures, and position-dependent actions.
const incrementalGrammar = `
type Pos struct { At int, Name Match }
def main = (line / blank)* $$
def blank = "\n"
def line = ^ (assign / list / pos) "\n"
def assign = n:name " = " e:expr
def list = list "," item / item
def item = @(?a-z)+
def pos: Pos = "@" n:name -> new Pos{At: $n.startPos, Name: $n}
def name = @(?a-z)+
def expr = pratt {
    operand @(?0-9)+
    operand "(" x:expr ")" -> $x
    level { infix left "+" / "-" }
    level { infix left "*" }
    level { prefix "-" }
}`

func dump(t *testing.T, n *Node, err error) string {
	t.Helper()
	data, jerr := json.Marshal(n)
	if jerr != nil {
		t.Fatal(jerr)
	}
	s := string(data)
	if err != nil {
		s += " error: " + err.Error()
	}
	return s
}

func TestDocumentMatchesFreshParse(t *testing.T) {
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(b.String(), func(t *testing.T) { testDocumentMatchesFreshParse(t, b) })
	}
}

func testDocumentMatchesFreshParse(t *testing.T, b Backend) {
	prog := compile(t, incrementalGrammar)
	o := ParseOptions{Backend: b}
	rng := rand.New(rand.NewSource(1))
	pieces := []string{"a", "b", "x = ", "1", "+", "2*3", "(", ")", "-", ",", "\n", "@", "q", " "}
	text := "x = 1+2\nab,cd\n@pos\n\ny = (3)*-4\n"
	doc, err := prog.NewDocumentWith("main", text, o)
	if err != nil {
		t.Fatal(err)
	}
	doc.Parse()
	for i := 0; i < 2000; i++ {
		cur := []rune(doc.Text())
		start := rng.Intn(len(cur) + 1)
		end := start + rng.Intn(min(4, len(cur)-start)+1)
		ins := ""
		for k := rng.Intn(3); k > 0; k-- {
			ins += pieces[rng.Intn(len(pieces))]
		}
		if err := doc.Edit(start, end, ins); err != nil {
			t.Fatal(err)
		}
		n, err := doc.Parse()
		got := dump(t, n, err)
		fn, ferr := prog.Parse("main", doc.Text()) // result of parsing from scratch with the closure engine
		if want := dump(t, fn, ferr); got != want {
			t.Fatalf("edit %d: [%d,%d) -> %q\ntext %q\n got  %s\n want %s", i, start, end, ins, doc.Text(), got, want)
		}
	}
}

func TestDocumentReusesResults(t *testing.T) {
	prog := compile(t, incrementalGrammar)
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("x = 1+2*(3-4)\nab,cd,ef\n")
	}
	doc, err := prog.NewDocument("main", b.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatal(err)
	}
	full := doc.Stats().Evaluated
	// Replace one character in the middle.
	mid := len([]rune(doc.Text())) / 2
	for []rune(doc.Text())[mid] != '1' {
		mid++
	}
	if err := doc.Edit(mid, mid+1, "7"); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		t.Fatal(err)
	}
	st := doc.Stats()
	if st.Evaluated*10 > full || st.Reused == 0 {
		t.Errorf("evaluated %d of %d rules, reused %d", st.Evaluated, full, st.Reused)
	}
}

// TestDocumentPositionalActions checks that results of position-dependent actions are
// recomputed rather than shifted past an edit.
func TestDocumentPositionalActions(t *testing.T) {
	prog := compile(t, incrementalGrammar)
	doc, _ := prog.NewDocument("main", "@abc\n")
	doc.Parse()
	doc.Edit(0, 0, "\n\n")
	n, err := doc.Parse()
	if err != nil {
		t.Fatal(err)
	}
	pos := n.Children[0].Children[2].Children[0]
	if pos.Type != "Pos" || pos.Field("At") != 3 {
		t.Errorf("got %s", pos)
	}
}

func TestDocumentEditErrors(t *testing.T) {
	prog := compile(t, `def main = "a"*`)
	doc, _ := prog.NewDocument("main", "aaa")
	for _, r := range [][2]int{{-1, 0}, {2, 1}, {0, 4}} {
		if err := doc.Edit(r[0], r[1], ""); err == nil {
			t.Errorf("%v: expected an error", r)
		}
	}
	if _, err := prog.NewDocument("nope", ""); err == nil {
		t.Error("expected an error for an undefined rule")
	}
}

// TestDocumentEditKeepsMemo checks that an edit keeps exactly the memo entries the rules in
// Document's comment allow, at their new positions. The second grammar has a memoized rule that
// examines no input, whose entries at the edit position stay in place.
func TestDocumentEditKeepsMemo(t *testing.T) {
	cases := []struct{ grammar, text string }{
		{incrementalGrammar, "x = 1+2\nab,cd\n@pos\n\ny = (3)*-4\n"},
		{"def main = (sep word sep)* $$\ndef sep = none none\ndef none = \"\"\ndef word = @(?a-z)+ \" \"?", "ab cd ef gh "},
	}
	pieces := []string{"a", "b", "x = ", "1", "+", "(", ")", ",", "\n", "@", " ", "zz "}
	for _, c := range cases {
		doc, err := compile(t, c.grammar).NewDocument("main", c.text)
		if err != nil {
			t.Fatal(err)
		}
		doc.Parse()
		rng := rand.New(rand.NewSource(7))
		for i := 0; i < 2000; i++ {
			cur := []rune(doc.Text())
			start := rng.Intn(len(cur) + 1)
			end := start + rng.Intn(min(4, len(cur)-start)+1)
			ins := ""
			for k := rng.Intn(3); k > 0; k-- {
				ins += pieces[rng.Intn(len(pieces))]
			}
			delta := len([]rune(ins)) - (end - start)
			want := map[*memoEntry]int{}
			doc.memo.each(func(e *memoEntry) {
				switch {
				case e.growing:
				case e.examined <= start:
					want[e] = e.pos
				case e.from >= end && !e.positional && len(e.errs) == 0:
					want[e] = e.pos + delta
				}
			})
			if err := doc.Edit(start, end, ins); err != nil {
				t.Fatal(err)
			}
			got := map[*memoEntry]int{}
			for at, head := range doc.memo.slots {
				for e := head; e != nil; e = e.next {
					if e.pos != at {
						t.Fatalf("edit %d: entry for position %d is listed at %d", i, e.pos, at)
					}
					got[e] = at
				}
			}
			if len(got) != len(want) {
				t.Fatalf("edit %d: [%d,%d) -> %q: %d entries, want %d", i, start, end, ins, len(got), len(want))
			}
			for e, at := range want {
				if p, ok := got[e]; !ok || p != at {
					t.Fatalf("edit %d: [%d,%d) -> %q: entry at %d (kept %v), want %d", i, start, end, ins, p, ok, at)
				}
			}
			doc.Parse()
		}
	}
}

// TestDocumentEditText checks that after random edits, including multi-byte characters and
// invalid UTF-8, the Document's text and its offset tables are those of the edited text read from
// scratch, in both position units.
func TestDocumentEditText(t *testing.T) {
	prog := compile(t, `def main = .*`)
	pieces := []string{"a", "é", "日本", "\n", "\xff", "z\xe3\x81", ""}
	for _, unit := range []Unit{CodePoints, Bytes} {
		doc, err := prog.NewDocumentWith("main", "héllo, 世界\n", ParseOptions{Unit: unit})
		if err != nil {
			t.Fatal(err)
		}
		ref := []rune("héllo, 世界\n") // the expected text, in code points
		refBytes := []byte("héllo, 世界\n")
		rng := rand.New(rand.NewSource(3))
		for i := 0; i < 3000; i++ {
			piece := pieces[rng.Intn(len(pieces))]
			var start, end int
			if unit == Bytes {
				// Edit at character boundaries of the current bytes.
				var bounds []int
				for k := 0; k <= len(refBytes); k++ {
					if validBoundary(string(refBytes), Bytes, k) == nil {
						bounds = append(bounds, k)
					}
				}
				start = bounds[rng.Intn(len(bounds))]
				end = start
				for _, b := range bounds {
					if b >= start && b <= start+4 && rng.Intn(2) == 0 {
						end = b
					}
				}
				refBytes = append(append(append([]byte{}, refBytes[:start]...), piece...), refBytes[end:]...)
			} else {
				start = rng.Intn(len(ref) + 1)
				end = start + rng.Intn(min(3, len(ref)-start)+1)
				ref = append(append(append([]rune{}, ref[:start]...), []rune(piece)...), ref[end:]...)
			}
			if err := doc.Edit(start, end, piece); err != nil {
				t.Fatal(err)
			}
			want := newInput(string(refBytes), Bytes)
			if unit == CodePoints {
				want = newInput(string(ref), CodePoints)
			}
			got := doc.in
			for _, in := range []*input{&got, &want} { // the offset table is built on demand
				if in.srcOK && in.unit == CodePoints && in.offs == nil {
					in.buildOffs()
				}
			}
			if string(got.in) != string(want.in) || string(got.bs) != string(want.bs) || got.src != want.src ||
				got.srcOK != want.srcOK || !slices.Equal(got.offs, want.offs) {
				t.Fatalf("%v, edit %d: [%d,%d) -> %q: text or offsets differ from a fresh read", unit, i, start, end, piece)
			}
			if _, err := doc.Parse(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestDocumentKeepsInputTables checks that the tables a parse builds on demand (the code-point
// offsets of token text, the line starts of error positions) stay with the Document, so that
// edits and later parses update them instead of rebuilding them from the whole text.
func TestDocumentKeepsInputTables(t *testing.T) {
	doc, err := compile(t, `def main = (@(?a-z)+ "\n")* $$`).NewDocument("main", "ab\ncd\n!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Parse(); err == nil {
		t.Fatal("expected a syntax error")
	}
	if doc.in.offs == nil || doc.in.lines == nil {
		t.Fatalf("tables not kept: offsets %v, lines %v", doc.in.offs != nil, doc.in.lines != nil)
	}
	if err := doc.Edit(6, 7, "ef\n"); err != nil {
		t.Fatal(err)
	}
	if doc.in.offs == nil {
		t.Fatal("the offsets were dropped by an edit")
	}
	if n, err := doc.Parse(); err != nil || n.String() != `(Seq [(Seq "ab" "\n") (Seq "cd" "\n") (Seq "ef" "\n")])@main` {
		t.Fatalf("got %v, %v", n, err)
	}
}

// TestDocumentInputStartAnchors checks that results that depend on being at the start of the
// input (^^) or of a line (^) are not reused after text is inserted before them.
func TestDocumentInputStartAnchors(t *testing.T) {
	for _, anchor := range []string{"^^", "^"} {
		src := "def main = (z / a)*\ndef a = " + anchor + ` "b" / "b" "!"` + "\ndef z = \"q\""
		prog := compile(t, src)
		doc, err := prog.NewDocument("main", "b")
		if err != nil {
			t.Fatal(err)
		}
		doc.Parse()
		if err := doc.Edit(0, 0, "q"); err != nil {
			t.Fatal(err)
		}
		n, err := doc.Parse()
		fn, ferr := prog.Parse("main", doc.Text())
		if got, want := dump(t, n, err), dump(t, fn, ferr); got != want {
			t.Errorf("%s: got %s, want %s", anchor, got, want)
		}
	}
}

// TestDocumentMovesReusedTrees checks reused results after random batches of edits (several
// edits between parses) on grammars with empty nodes, which can lie at an insertion point on
// either side of it, and nodes reachable twice (as a child and a field).
func TestDocumentMovesReusedTrees(t *testing.T) {
	cases := []struct {
		grammar, text string
		pieces        []string
	}{
		{strings.Replace(incrementalGrammar, "(line / blank)*", "(line / blank / junk)*", 1) + "\ndef junk = @(?^\\n)* \"\\n\"",
			"x = 1+2\nab,cd\n@pos\n\ny = (3)*-4\n",
			[]string{"a", "b", "x = ", "1", "+", "2*3", "(", ")", "-", ",", "\n", "@", "q", " ", ""}},
		{`
type W struct { Pre Match, Word Match, Post Match, Gap Match }
def main = (sep w:word sep)* $$
def sep = none none
def none = @""
def word: W = p:none x:@(?a-z)+ q:none g:gap -> new W{Pre: $p, Word: $x, Post: $q, Gap: $g}
def gap = @" "*`, "ab cd  e ", []string{"a", "b", " ", "ab ", "", "zz"}},
		{`
def main = items:(item / junk)* tail:@""  $$
def junk = @(?^;)* ";"
def item = k:key ":" v:val? ";" kids:(-"," c:@(?0-9)*)*
def key = @(?a-z)* -> $0
def val = e:@(?0-9)* -> $e`, "a:1;,2,b:;:3;,;", []string{"a", "b", ":", ";", "1", ",", "x:2;", ",3", ":;", ""}},
	}
	// Edits leave the last character alone: each text ends with a terminator that lets it parse.
	for ci, c := range cases {
		prog := compile(t, c.grammar)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			doc, err := prog.NewDocumentWith("main", c.text, ParseOptions{Backend: b})
			if err != nil {
				t.Fatal(err)
			}
			doc.Parse()
			rng := rand.New(rand.NewSource(int64(11 + ci)))
			parsed := 0
			for i := 0; i < 600; i++ {
				var log []string
				for k := 1 + rng.Intn(3); k > 0; k-- {
					n := len([]rune(doc.Text())) - 1
					start := rng.Intn(n + 1)
					end := start
					if rng.Intn(2) == 0 {
						end += rng.Intn(min(3, n-start) + 1)
					}
					ins := c.pieces[rng.Intn(len(c.pieces))]
					if err := doc.Edit(start, end, ins); err != nil {
						t.Fatal(err)
					}
					log = append(log, fmt.Sprintf("[%d,%d)->%q", start, end, ins))
				}
				n, err := doc.Parse()
				if err == nil {
					parsed++
				}
				got := dump(t, n, err)
				fn, ferr := prog.Parse("main", doc.Text())
				if want := dump(t, fn, ferr); got != want {
					t.Fatalf("grammar %d, %v, round %d: %v\ntext %q\n got  %s\n want %s", ci, b, i, log, doc.Text(), got, want)
				}
			}
			if parsed < 400 {
				t.Errorf("grammar %d, %v: only %d of 600 rounds parsed", ci, b, parsed)
			}
		}
	}
}

// TestDocumentEarlierTrees checks what happens to a tree returned by an earlier parse: nodes
// reused after an edit are moved in place (so the earlier tree shares them), and a clone keeps
// the tree as it was, sharing included.
func TestDocumentEarlierTrees(t *testing.T) {
	prog := compile(t, `
def main = line* $$
def line = x:@(?a-z)+ "\n"`)
	doc, err := prog.NewDocument("main", strings.Repeat("ab\n", 10))
	if err != nil {
		t.Fatal(err)
	}
	t1, _ := doc.Parse()
	snap := t1.Clone()
	before := dump(t, t1, nil)
	if dump(t, snap, nil) != before {
		t.Fatal("clone differs")
	}
	if l := snap.Children[0].Children[3]; l.Field("x") != l.Children[0] {
		t.Error("clone does not share the captured child")
	}
	doc.Edit(0, 0, "zz\n")
	t2, _ := doc.Parse()
	old, cur := t1.Children[0].Children[3], t2.Children[0].Children[4]
	if old != cur || cur.Start != 12 || cur.Field("x").(*Node).Start != 12 {
		t.Errorf("line 3 of the earlier tree: shared %v, at %d", old == cur, cur.Start)
	}
	if dump(t, snap, nil) != before {
		t.Error("the clone changed")
	}
}
