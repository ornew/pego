package engine

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

const records = `
def main = ^^ header items:record* #stream $$
def header = "#records\n"
def record = ^ k:@(?a-z)+ "=" v:@(?0-9)+ "\n"`

func TestParseStream(t *testing.T) {
	prog := compile(t, records)
	var got []string
	err := prog.ParseStream("main", strings.NewReader("#records\na=1\nbc=22\n"), func(n *Node) error {
		got = append(got, fmt.Sprintf("%s [%d,%d)", n, n.Start, n.End))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`(Seq "a" "=" "1" "\n" k="a" v="1")@record [9,13)`,
		`(Seq "bc" "=" "22" "\n" k="bc" v="22")@record [13,19)`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s", strings.Join(got, "\n"))
	}
	// An ordinary parse returns the same elements as a list.
	n, err := prog.Parse("main", "#records\na=1\nbc=22\n")
	if err != nil {
		t.Fatal(err)
	}
	if s := n.String(); !strings.Contains(s, `[(Seq "a" "=" "1" "\n" k="a" v="1")@record (Seq "bc"`) {
		t.Errorf("got %s", s)
	}
}

// TestParseStreamIsIncremental checks that elements are passed for the input received so far,
// before all the input has arrived.
func TestParseStreamIsIncremental(t *testing.T) {
	prog := compile(t, records)
	pr, pw := io.Pipe()
	next := make(chan struct{})
	go func() {
		io.WriteString(pw, "#records\nx=1\n")
		<-next // do not write the rest until "x" is received
		io.WriteString(pw, "y=2\n")
		pw.Close()
	}()
	emitted := make(chan string, 2)
	done := make(chan error)
	go func() {
		done <- prog.ParseStream("main", pr, func(n *Node) error {
			emitted <- n.Field("k").(*Node).Text
			return nil
		})
	}()
	if k := <-emitted; k != "x" {
		t.Fatalf("got %q", k)
	}
	close(next)
	if k := <-emitted; k != "y" {
		t.Fatalf("got %q", k)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestParseStreamErrorPosition checks that error lines and columns are correct even after input
// has been discarded.
func TestParseStreamErrorPosition(t *testing.T) {
	prog := compile(t, records)
	var b strings.Builder
	b.WriteString("#records\n")
	for i := 0; i < 5000; i++ {
		b.WriteString("abc=123\n")
	}
	b.WriteString("abc=x\n")
	count := 0
	err := prog.ParseStream("main", strings.NewReader(b.String()), func(*Node) error {
		count++
		return nil
	})
	var se *SyntaxError
	if !errors.As(err, &se) || se.Line != 5002 || se.Col != 5 {
		t.Fatalf("got %v", err)
	}
	if count != 5000 {
		t.Errorf("emitted %d", count)
	}
}

func TestParseStreamEmitError(t *testing.T) {
	prog := compile(t, records)
	stop := errors.New("stop")
	count := 0
	err := prog.ParseStream("main", strings.NewReader("#records\na=1\nb=2\n"), func(*Node) error {
		count++
		return stop
	})
	if err != stop || count != 1 {
		t.Errorf("got %v after %d", err, count)
	}
}

// TestParseStreamDiscardsInput checks that committed input is discarded and the buffer does not
// grow, even for large input.
func TestParseStreamDiscardsInput(t *testing.T) {
	prog := compile(t, records)
	r := &lineReader{header: "#records\n", line: "key=42\n", n: 100000}
	maxBuf := 0
	p := newStreamParser(prog, r, CodePoints)
	p.emit = func(*Node) error {
		maxBuf = max(maxBuf, len(p.in))
		return nil
	}
	if _, ok := p.call(prog.byName["main"], 0); !ok {
		t.Fatal(p.syntaxError())
	}
	if maxBuf > 10000 {
		t.Errorf("buffer grew to %d runes", maxBuf)
	}
	if n := p.memo.len(); n > 5000 {
		t.Errorf("memo has %d entries", n)
	}
}

type lineReader struct {
	header, line string
	n            int
	buf          string
}

func (r *lineReader) Read(b []byte) (int, error) {
	if r.buf == "" {
		switch {
		case r.header != "":
			r.buf, r.header = r.header, ""
		case r.n > 0:
			r.buf = r.line
			r.n--
		default:
			return 0, io.EOF
		}
	}
	n := copy(b, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func TestStreamCompileErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`def main = "a" #stream`, `#stream must be attached to a repetition`},
		{`def main = ("a"* #stream / "b") "c"`, `#stream is only allowed at the top level of a rule body`},
		{`def main = "a"* #stream "b"* #stream`, `a rule body can have only one #stream`},
		{`def main = "a"* #stream(x="y")`, `#stream takes no arguments`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
	prog := compile(t, `def main = "a"*`)
	if err := prog.ParseStream("main", strings.NewReader("a"), func(*Node) error { return nil }); err == nil || err.Error() != "rule main has no #stream repetition" {
		t.Errorf("got %v", err)
	}
}
