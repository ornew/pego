package engine

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

func TestParseStream(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
	var got []string
	err := prog.ParseStream("main", strings.NewReader("#records\na=1\nbc=22\n"), func(n *value.Node) error {
		got = append(got, fmt.Sprintf("%s [%d,%d)", n, value.D(n).Start, value.D(n).End))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`(k:"a" "=" v:"1" "\n")@record [9,13)`,
		`(k:"bc" "=" v:"22" "\n")@record [13,19)`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s", strings.Join(got, "\n"))
	}
	// An ordinary parse returns the same elements as a list.
	n, err := prog.Parse("main", "#records\na=1\nbc=22\n")
	if err != nil {
		t.Fatal(err)
	}
	if s := n.String(); !strings.Contains(s, `[(k:"a" "=" v:"1" "\n")@record (k:"bc"`) {
		t.Errorf("got %s", s)
	}
}

// TestParseStreamIsIncremental checks that elements are passed for the input received so far,
// before all the input has arrived.
func TestParseStreamIsIncremental(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
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
		done <- prog.ParseStream("main", pr, func(n *value.Node) error {
			emitted <- value.D(n.Field("k").(*value.Node)).Text
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
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
	var b strings.Builder
	b.WriteString("#records\n")
	for i := 0; i < 5000; i++ {
		b.WriteString("abc=123\n")
	}
	b.WriteString("abc=x\n")
	count := 0
	err := prog.ParseStream("main", strings.NewReader(b.String()), func(*value.Node) error {
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
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
	stop := errors.New("stop")
	count := 0
	err := prog.ParseStream("main", strings.NewReader("#records\na=1\nb=2\n"), func(*value.Node) error {
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
	t.Parallel()
	prog := mustCompile(t, gentest.Records)
	r := &lineReader{header: "#records\n", line: "key=42\n", n: 100000}
	maxBuf := 0
	p := core.NewStreamParser(&prog.sharedProgram, r, CodePoints)
	p.Emit = func(*value.Node) error {
		maxBuf = max(maxBuf, len(p.In))
		return nil
	}
	if _, ok := p.Call(prog.closureRule("main"), 0); !ok {
		t.Fatal(p.SyntaxError())
	}
	if maxBuf > 10000 {
		t.Errorf("buffer grew to %d runes", maxBuf)
	}
	if n := p.Memo.Len(); n > 5000 {
		t.Errorf("memo has %d entries", n)
	}
}

// TestParseStreamMemoryIsBounded checks that the memory reachable during a stream parse does not
// grow with the number of elements emitted, for elements with captures, with actions, and with
// plain CST values, on every backend.
func TestParseStreamMemoryIsBounded(t *testing.T) {
	grammars := map[string]string{
		"records": gentest.Records,
		"action": `type Pair = { Key node[string]; Value node[string] }
rule main = item* #stream $$
rule item Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" => Pair{Key: $k, Value: $v}`,
		"lines": `rule main = line* #stream $$
rule line = [a-z0-9=]+ #atomic "\n"`,
		// Each element backtracks into a memoized, variable-reading rule, binds
		// variables and produces nil.
		"memo, variables and nil": `var n int
var last string
rule main = {n := 0} item* #stream $$
rule item = (k:key ":" / k:key {n := n + 1} {last := text($k)} "=") val "\n" => nil
rule key = {n >= 0} word
rule word = [a-z]+ #atomic
rule val = [0-9]+ #atomic`,
	}
	for name, src := range grammars {
		prog := mustCompile(t, src)
		if r := prog.ByName["key"]; r != nil && (!r.Memo || r.Transient || len(r.Vars) == 0) {
			t.Fatalf("%s: key must be memoized, non-transient and read variables", name)
		}
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			header := ""
			if name == "records" {
				header = "#records\n"
			}
			const n = 60000
			var at [2]uint64
			count := 0
			err := prog.ParseStreamWith("main", &lineReader{header: header, line: "key=42\n", n: n}, func(*value.Node) error {
				count++
				if count == n/3 || count == n {
					var ms runtime.MemStats
					runtime.GC()
					runtime.ReadMemStats(&ms)
					at[count/n] = ms.HeapAlloc
				}
				return nil
			}, ParseOptions{Backend: b})
			if err != nil || count != n {
				t.Fatalf("%s, %v: %d elements, %v", name, b, count, err)
			}
			// Retaining the elements would take tens of megabytes here.
			if grown := int64(at[1]) - int64(at[0]); grown > 4<<20 {
				t.Errorf("%s, %v: reachable memory grew by %d KiB over %d elements", name, b, grown>>10, n-n/3)
			}
		}
	}
}

// TestParseStreamTextOfDiscardedInput checks that text(...) of a node whose input has been
// discarded by the stream returns what is still held instead of failing.
func TestParseStreamTextOfDiscardedInput(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type T = { H string; N int }
rule main = h:("a" "b") rs:r* #stream "!" => T{H: text($h), N: len($rs)}
rule r = "x" "y"`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		input := "ab" + strings.Repeat("xy", 5000) + "!"
		err := prog.ParseStreamWith("main", strings.NewReader(input), func(*value.Node) error { return nil }, ParseOptions{Backend: b})
		if err != nil {
			t.Errorf("%v: %v", b, err)
		}
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
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule main = "a" #stream`, `#stream must be attached to a repetition`},
		{`rule main = ("a"* #stream / "b") "c"`, `#stream is only allowed at the top level of a rule body`},
		{`rule main = "a"* #stream "b"* #stream`, `a rule body can have only one #stream`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
	prog := mustCompile(t, `rule main = "a"*`)
	if err := prog.ParseStream("main", strings.NewReader("a"), func(*value.Node) error { return nil }); err == nil || err.Error() != "rule main has no #stream repetition" {
		t.Errorf("got %v", err)
	}
}

