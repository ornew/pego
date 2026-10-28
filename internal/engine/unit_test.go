package engine

import (
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/value"
)

func TestBytePositions(t *testing.T) {
	prog := mustCompile(t, `rule main = w:[^ ]+ #atomic " " x:.`)
	n, err := prog.ParseWith("main", "日本 語", ParseOptions{Unit: Bytes})
	if err != nil {
		t.Fatal(err)
	}
	w := n.Field("w").(*value.Node)
	x := n.Field("x").(*value.Node)
	if value.D(w).Start != 0 || value.D(w).End != 6 || value.D(x).Start != 7 || value.D(x).End != 10 || value.D(x).Text != "語" {
		t.Errorf("w [%d,%d) x [%d,%d) %q", value.D(w).Start, value.D(w).End, value.D(x).Start, value.D(x).End, value.D(x).Text)
	}
	n, err = prog.ParseWith("main", "日本 語", ParseOptions{Unit: CodePoints})
	if err != nil || value.D(n.Field("x").(*value.Node)).Start != 3 {
		t.Errorf("codepoints: %v %v", n, err)
	}
	_, err = prog.ParseWith("main", "日本 ", ParseOptions{Unit: Bytes})
	var se *SyntaxError
	if !errors.As(err, &se) || se.Pos != 7 || se.Line != 1 || se.Col != 8 {
		t.Errorf("got %v", err)
	}
}

// TestInvalidUTF8 checks that invalid UTF-8 is read as a 1-byte U+FFFD.
func TestInvalidUTF8(t *testing.T) {
	prog := mustCompile(t, `rule main = "a" x:. "b"`)
	n, err := prog.ParseWith("main", "a\xffb", ParseOptions{Unit: Bytes})
	if err != nil {
		t.Fatal(err)
	}
	if x := n.Field("x").(*value.Node); value.D(x).Start != 1 || value.D(x).End != 2 {
		t.Errorf("x [%d,%d)", value.D(x).Start, value.D(x).End)
	}
	checkUnits(t, prog, "main", "a\xffb", Closure)
	checkUnits(t, prog, "main", "a\xffb", Bytecode)
	checkUnits(t, prog, "main", "a\xffb", BytecodeIterative)
}

func TestLenInUnits(t *testing.T) {
	prog := mustCompile(t, `type N = { N int }
rule main = s:.* #atomic => N{N: len($s) + len(text($s))}`)
	for unit, want := range map[PositionUnit]int{CodePoints: 4, Bytes: 12} {
		n, err := prog.ParseWith("main", "日本", ParseOptions{Unit: unit})
		if err != nil || n.Field("N") != want {
			t.Errorf("%v: got %v %v", unit, n, err)
		}
	}
}

func TestDocumentInBytes(t *testing.T) {
	prog := mustCompile(t, incrementalGrammar)
	rng := rand.New(rand.NewSource(2))
	pieces := []string{"a", "x = ", "1", "+", "é", "日", "\n", "@", "(", ")"}
	doc, err := prog.NewDocumentWith("main", "x = 1+2\nab,cd\n@pos\n", ParseOptions{Unit: Bytes})
	if err != nil {
		t.Fatal(err)
	}
	doc.Parse()
	for i := 0; i < 500; i++ {
		cur := doc.Text()
		start := rng.Intn(len(cur) + 1)
		for start < len(cur) && !utf8RuneStart(cur[start]) {
			start++
		}
		end := start
		for end < len(cur) && rng.Intn(2) == 0 {
			end++
			for end < len(cur) && !utf8RuneStart(cur[end]) {
				end++
			}
		}
		ins := pieces[rng.Intn(len(pieces))]
		if err := doc.Edit(start, end, ins); err != nil {
			t.Fatal(err)
		}
		n, err := doc.Parse()
		fn, ferr := prog.ParseWith("main", doc.Text(), ParseOptions{Unit: Bytes})
		if a, b := resultJSON(n, err), resultJSON(fn, ferr); a != b {
			t.Fatalf("edit %d: text %q\n got  %s\n want %s", i, doc.Text(), a, b)
		}
	}
	if err := doc.Edit(0, 0, "日"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Edit(1, 1, "x"); err == nil || !strings.Contains(err.Error(), "not at a character boundary") {
		t.Errorf("got %v", err)
	}
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

func TestStreamInBytes(t *testing.T) {
	prog := mustCompile(t, records)
	var got []string
	err := prog.ParseStreamWith("main", strings.NewReader("#records\nä=1\nb=2\n"), func(n *value.Node) error {
		got = append(got, n.String())
		return nil
	}, ParseOptions{Unit: Bytes})
	var se *SyntaxError
	// ä does not match [a-z]. The error column is in bytes.
	if !errors.As(err, &se) || se.Line != 2 || se.Col != 1 {
		t.Fatalf("got %v %v", got, err)
	}
	got = nil
	var b strings.Builder
	b.WriteString("#records\n")
	for i := 0; i < 3000; i++ {
		b.WriteString("ab=12\n")
	}
	b.WriteString("ab=日\n")
	err = prog.ParseStreamWith("main", strings.NewReader(b.String()), func(*value.Node) error { return nil }, ParseOptions{Unit: Bytes})
	if !errors.As(err, &se) || se.Line != 3002 || se.Col != 4 || se.Pos != len("#records\n")+3000*6+3 {
		t.Errorf("got %v (pos %d)", err, se.Pos)
	}
}
