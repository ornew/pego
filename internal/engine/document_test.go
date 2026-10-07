package engine

import (
	"encoding/json"
	"math/rand"
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
