package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// streamRun is one stream parse of a differential test. The reader returns
// at most Chunk bytes per Read (all of the rest if zero); EOFWithData returns
// io.EOF with the last bytes, and Fail returns errStreamRead instead of EOF.
// StopAt makes the StopAt-th emit return errStreamStop; NilEmit passes no
// callback.
type streamRun struct {
	Case        int
	Input       []byte // bytes: JSON would replace invalid UTF-8 in a string
	Bytes       bool
	Chunk       int
	EOFWithData bool
	Fail        bool
	StopAt      int
	NilEmit     bool
}

var (
	errStreamRead = errors.New("read failed")
	errStreamStop = errors.New("stop")
)

// streamTestReader is shared, as source, with the generated test program.
const streamTestReader = `type chunkReader struct {
	s           string
	chunk       int
	eofWithData bool
	fail        bool
	calls       int
}

func (r *chunkReader) Read(b []byte) (int, error) {
	r.calls++
	if r.s == "" {
		if r.fail {
			return 0, errStreamRead
		}
		return 0, io.EOF
	}
	n := len(r.s)
	if r.chunk > 0 && r.chunk < n {
		n = r.chunk
	}
	n = copy(b, r.s[:n])
	r.s = r.s[n:]
	if r.s == "" && r.eofWithData && !r.fail {
		return n, io.EOF
	}
	return n, nil
}
`

type chunkReader struct {
	s           string
	chunk       int
	eofWithData bool
	fail        bool
	calls       int
}

func (r *chunkReader) Read(b []byte) (int, error) {
	r.calls++
	if r.s == "" {
		if r.fail {
			return 0, errStreamRead
		}
		return 0, io.EOF
	}
	n := len(r.s)
	if r.chunk > 0 && r.chunk < n {
		n = r.chunk
	}
	n = copy(b, r.s[:n])
	r.s = r.s[n:]
	if r.s == "" && r.eofWithData && !r.fail {
		return n, io.EOF
	}
	return n, nil
}

// streamResult records each emitted element with the number of reader calls
// made before it, and the final error.
type streamResult struct {
	Emits []json.RawMessage `json:"emits"`
	Calls []int             `json:"calls"`
	Err   string            `json:"err,omitempty"`
}

// streamMemoGrammar's elements backtrack into a memoized rule that reads
// variables, assign variables and produce nil.
const streamMemoGrammar = `rule main = {n := 0} item* #stream $$
rule item = (k:key ":" / k:key {n := n + 1} {last := text($k)} "=") val "\n" => nil
rule key = {n >= 0} word
rule word = [a-z]+ #atomic
rule val = [0-9]+ #atomic`

var streamCorpus = []genCase{
	{"records", records, []string{"#records\na=1\nbc=22\n", "#records\nx\n", "#records\n", "", "#records\na=1\nb"}},
	{"actions", `type Pair struct { Key Match; Value Match }
rule main = item* #stream $$
rule item Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" => Pair{Key: $k, Value: $v}`, []string{"a=1\nb=2\n", "a=1\nb=x\n", ""}},
	{"discarded text", `type T struct { H string; N int }
rule main = h:("a" "b") rs:r* #stream "!" => T{H: text($h), N: len($rs)}
rule r = "x" "y"`, []string{"abxyxy!", "ab!", "abxyx!", "ab" + strings.Repeat("xy", 3000) + "!"}},
	{"zero width", `rule main = ("a" / "") * #stream $$`, []string{"", "a", "aa", "ab"}},
	{"bounded", `rule main = ("é" / "x") #repeat(1, 2) #stream rest
rule rest = "!" $$`, []string{"!", "é!", "éx!", "éxé!", "xé"}},
	{"minimum failure", `rule main = "x" #repeat(3) #stream $$`, []string{"xx", "xxx", "xxxx", "y", ""}},
	{"lookahead", `rule main = item* #stream $$
rule item = "a" &"b" / "b" !"c" / "c"`, []string{"abab", "abc", "bc", "cab", "ac"}},
	{"line anchors", `rule main = line* #stream $$
rule line = ^ [a-z]* #atomic $ "\n"`, []string{"ab\ncd\n", "ab\n\nc\n", "ab\nc", "\n\n"}},
	{"left recursive elements", `rule main = (e ";") * #stream $$
rule e = e "+" t / t
rule t = [0-9]+ #atomic`, []string{"1+2;3;", "1+;", "1+2+3;4+5;"}},
	{"operators elements", `rule main = (e ";") * #stream $$
rule e = operators {
    operand [0-9]+ #atomic
    level { infix left "+" }
    level { prefix "-" }
}`, []string{"1+-2;3;", "-;", "1+2+3;"}},
	{"cut elements", `rule main = item* #stream $$
rule item = "a" -- "b" / "c"`, []string{"abc", "ac", "abab"}},
	{"variables", `rule main = {n := 0} item* #stream $$
rule item = "a" {n := n + 1} / "b" {n > 0}`, []string{"ab", "b", "aab"}},
	{"recovery", `rule main = stmt* #stream $$
rule stmt = ([a-z]+ #atomic ";") #recover(skip=[^;]+ ";")`, []string{"ab;1x;cd;9;", "1x;ab", "ab;"}},
	{"unicode", `rule main = ch* #stream $$
rule ch = "é" / "😀" / "\u{fffd}" / [a-z]`, []string{"aé😀b", "é\xe2\x82", "\xff\xfe", "a\xe2\x82\xacb", "\xf0\x9f\x98", "😀😀"}},
	{"errors after discard", `rule main = line* #stream $$
rule line = [a-z]+ #atomic "\n"`, []string{strings.Repeat("abc\n", 3000) + "x1\n", strings.Repeat("ab\n", 2000) + "\n"}},
	// Elements backtrack into a memoized rule that reads variables.
	{"memoized elements", streamMemoGrammar, []string{"a=1\nb:2\n", "a=1\n:2\n", strings.Repeat("ab=1\ncd:2\n", 1500)}},
	// The start rule is memoized at 0 after the memo before it was pruned.
	{"recursive start", `rule main = item* #stream
rule item = "(" main ")" / [a-z] / "[" main "]" / "(" [0-9]`, []string{strings.Repeat("x", 3000), strings.Repeat("(a)", 1000), "[a(b)]c(1", "(1"}},
	// Each growth of the start rule allocates its capture frame again.
	{"left recursive start", `rule main = a:(main "x")? b:item* #stream c:"."?
rule item = q:[a-z] #atomic`, []string{"ab.", "a", "", "ab.x"}},
}

// TestGeneratedStreamMatchesEngine compares generated ParseStream with the
// engine's ParseStream: emitted elements, the reader calls made before each,
// and the final error, for reader chunkings, reader errors, emit errors and a
// nil callback, in both position units.
func TestGeneratedStreamMatchesEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := append([]genCase(nil), streamCorpus...)
	for _, c := range genCorpus(t) {
		if strings.Contains(c.src, "#stream") {
			cases = append(cases, c)
		}
	}
	for name, opts := range map[string]GenOptions{"default": {}, "reference controls": {referenceWorkControls: true},
		"observability": {Observability: true}, "document": {Document: true, Recognize: true}} {
		t.Run(name, func(t *testing.T) {
			testGeneratedStreams(t, goBin, cases, opts)
		})
	}
}

func testGeneratedStreams(t *testing.T, goBin string, cases []genCase, opts GenOptions) {
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module streamtest\n\ngo 1.24\n")
	var imports, parsers strings.Builder
	var runs []streamRun
	var want []string
	var names []string
	for i, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts.Package, opts.Start, opts.Stream = fmt.Sprintf("g%d", i), "main", true
		code, err := Generate(g, opts)
		if err != nil {
			if strings.Contains(err.Error(), "needs a #stream repetition") {
				continue // the corpus grammar has #stream elsewhere
			}
			t.Fatalf("%s: %v", c.name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"streamtest/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, "\t%d: func(r io.Reader, emit func(any) error, b bool) error {\n\t\tu := g%d.CodePoints\n\t\tif b {\n\t\t\tu = g%[2]d.Bytes\n\t\t}\n\t\tvar e func(*g%[2]d.Node) error\n\t\tif emit != nil {\n\t\t\te = func(n *g%[2]d.Node) error { return emit(n) }\n\t\t}\n\t\treturn g%[2]d.ParseStream(r, e, g%[2]d.WithUnit(u))\n\t},\n", i, i)
		prog := compile(t, c.src, Options{noProjections: true})
		for _, in := range c.inputs {
			for _, bytes := range []bool{false, true} {
				variants := []streamRun{{Chunk: 0}, {Chunk: 1}, {Chunk: 2, EOFWithData: true}, {Chunk: 3, Fail: true}, {Chunk: 1, StopAt: 2}, {NilEmit: true}}
				if len(in) > 1000 {
					variants = []streamRun{{Chunk: 0}, {Chunk: 4093}, {Chunk: 7, StopAt: 1500}}
				}
				for _, v := range variants {
					v.Case, v.Input, v.Bytes = i, []byte(in), bytes
					runs = append(runs, v)
					want = append(want, engineStream(prog, v))
					names = append(names, fmt.Sprintf("%s %q %+v", c.name, in, v))
				}
			}
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
`+imports.String()+`)

var (
	errStreamRead = errors.New("read failed")
	errStreamStop = errors.New("stop")
)

`+streamTestReader+`
var parsers = map[int]func(io.Reader, func(any) error, bool) error{
`+parsers.String()+`}

type run struct {
	Case        int
	Input       []byte
	Bytes       bool
	Chunk       int
	EOFWithData bool
	Fail        bool
	StopAt      int
	NilEmit     bool
}

func main() {
	var runs []run
	data, _ := os.ReadFile(os.Args[1])
	if err := json.Unmarshal(data, &runs); err != nil {
		panic(err)
	}
	for _, v := range runs {
		r := &chunkReader{s: string(v.Input), chunk: v.Chunk, eofWithData: v.EOFWithData, fail: v.Fail}
		var res struct {
			Emits []json.RawMessage `+"`json:\"emits\"`"+`
			Calls []int             `+"`json:\"calls\"`"+`
			Err   string            `+"`json:\"err,omitempty\"`"+`
		}
		var emit func(any) error
		if !v.NilEmit {
			emit = func(n any) error {
				b, err := json.Marshal(n)
				if err != nil {
					panic(err)
				}
				res.Emits = append(res.Emits, b)
				res.Calls = append(res.Calls, r.calls)
				if len(res.Emits) == v.StopAt {
					return errStreamStop
				}
				return nil
			}
		}
		if err := parsers[v.Case](r, emit, v.Bytes); err != nil {
			res.Err = err.Error()
			if err == errStreamRead || err == errStreamStop {
				res.Err = "caller " + err.Error()
			} else if errors.Is(err, errStreamRead) || errors.Is(err, errStreamStop) {
				res.Err = "wrapped " + err.Error()
			}
		}
		b, _ := json.Marshal(res)
		fmt.Println(string(b))
	}
}
`)
	data, _ := json.Marshal(runs)
	write("runs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "runs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d\n%s", len(got), len(want), out)
	}
	for k := range want {
		if got[k] != want[k] {
			t.Errorf("%s\n generated %s\n engine    %s", names[k], got[k], want[k])
		}
	}
}

// engineStream runs v with the engine's ParseStream and formats the result as
// the generated test program does. Caller errors must be returned unchanged.
func engineStream(prog *Program, v streamRun) string {
	r := &chunkReader{s: string(v.Input), chunk: v.Chunk, eofWithData: v.EOFWithData, fail: v.Fail}
	var res streamResult
	var emit func(*Node) error
	if !v.NilEmit {
		emit = func(n *Node) error {
			b, err := json.Marshal(n)
			if err != nil {
				panic(err)
			}
			res.Emits = append(res.Emits, b)
			res.Calls = append(res.Calls, r.calls)
			if len(res.Emits) == v.StopAt {
				return errStreamStop
			}
			return nil
		}
	}
	unit := CodePoints
	if v.Bytes {
		unit = Bytes
	}
	if err := prog.ParseStreamWith("main", r, emit, ParseOptions{Unit: unit}); err != nil {
		res.Err = err.Error()
		if err == errStreamRead || err == errStreamStop {
			res.Err = "caller " + err.Error()
		} else if errors.Is(err, errStreamRead) || errors.Is(err, errStreamStop) {
			res.Err = "wrapped " + err.Error()
		}
	}
	b, _ := json.Marshal(res)
	return string(b)
}

func TestGenerateStreamOptions(t *testing.T) {
	g, err := syntax.Parse(`rule main = item* #stream $$
rule item = "a"
rule other = "b"`)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Generate(g, GenOptions{Package: "fixture", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"stream:", "streamInput", "func ParseStream", "\"bufio\""} {
		if strings.Contains(string(plain), marker) {
			t.Errorf("ordinary output contains %s", marker)
		}
	}
	code, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Stream: true})
	if err != nil || !strings.Contains(string(code), "func ParseStream(") || strings.Contains(string(code), "stream:") {
		t.Fatalf("stream output: %v", err)
	}
	for _, tc := range []struct {
		opts GenOptions
		want string
	}{
		{GenOptions{Start: "other", Stream: true}, "needs a #stream repetition"},
		{GenOptions{Start: "main", Stream: true, Types: true}, "does not support typed values"},
	} {
		tc.opts.Package = "fixture"
		if _, err := Generate(g, tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v, want %q", tc.opts, err, tc.want)
		}
	}
	if _, err := GenerateTS(g, GenOptions{Start: "main", Stream: true}); err == nil {
		t.Error("TypeScript accepted stream generation")
	}
}

// TestGenerateStreamKeepsBatchBodies checks that stream generation leaves
// the batch parsing code unchanged: every generated expression method and
// the rule table of the ordinary output appear unchanged in the stream output,
// whose stream body comes in addition.
func TestGenerateStreamKeepsBatchBodies(t *testing.T) {
	cases := append([]genCase(nil), streamCorpus...)
	for _, c := range genCorpus(t) {
		if strings.Contains(c.src, "#stream") {
			cases = append(cases, c)
		}
	}
	decls := func(code []byte) map[string]string {
		f, err := goparser.ParseFile(token.NewFileSet(), "", code, 0)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, d := range f.Decls {
			var key string
			switch d := d.(type) {
			case *goast.FuncDecl:
				// Generated expression and rule methods; the runtime has stream hooks.
				if d.Recv == nil || !strings.HasPrefix(d.Name.Name, "e") && !strings.HasPrefix(d.Name.Name, "r") || strings.HasPrefix(d.Name.Name, "re") {
					continue
				}
				key = d.Name.Name
			default:
				continue
			}
			var b strings.Builder
			printer.Fprint(&b, token.NewFileSet(), d)
			out[key] = b.String()
		}
		return out
	}
	n := 0
	for _, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range []GenOptions{{}, {Observability: true}, {referenceWorkControls: true}} {
			opts.Package, opts.Start = "p", "main"
			plain, err := Generate(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			opts.Stream = true
			stream, err := Generate(g, opts)
			if err != nil {
				if strings.Contains(err.Error(), "needs a #stream repetition") {
					continue
				}
				t.Fatal(err)
			}
			n++
			got := decls(stream)
			for name, body := range decls(plain) {
				if got[name] != body {
					t.Errorf("%s %+v: method %s differs with stream generation", c.name, opts, name)
				}
			}
			table := func(code []byte) string {
				src := string(code)
				i := strings.Index(src, "rules = []*rule{")
				return src[i : i+strings.Index(src[i:], "\n\t}\n")]
			}
			if table(plain) != table(stream) {
				t.Errorf("%s: rule table differs with stream generation", c.name)
			}
		}
	}
	if n == 0 {
		t.Fatal("no stream grammar")
	}
}

// TestGeneratedStreamAPI checks generated ParseStream behavior that the
// differential test does not cover: observation, work controls, callback
// panics and bounded memory.
func TestGeneratedStreamAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated standalone stream consumer")
	}
	g, err := syntax.Parse(`type Pair struct { Key Match; Value Match }
rule main = item* #stream $$
rule item Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" => Pair{Key: $k, Value: $v}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Stream: true, Observability: true})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":         "module fixture\n\ngo 1.27.1\n",
		"parser.go":      string(code),
		"parser_test.go": generatedStreamConsumer,
	}
	// Elements of varying size end at varying places in the allocation
	// chunks, and the reference frame allocator has no scratch to rewind.
	lists, err := syntax.Parse(`rule main = item* #stream $$
rule item = "(" a:atom b:atom c:list ")" "\n"
rule list = "[" xs:atom* "]"
rule atom = [a-z] #atomic " "?`)
	if err != nil {
		t.Fatal(err)
	}
	memo, err := syntax.Parse(streamMemoGrammar)
	if err != nil {
		t.Fatal(err)
	}
	code, err = Generate(memo, GenOptions{Package: "memo", Start: "main", Stream: true, Observability: true})
	if err != nil {
		t.Fatal(err)
	}
	files["memo/parser.go"] = string(code)
	files["memo/parser_test.go"] = generatedStreamMemoConsumer
	for _, pkg := range []string{"lists", "listsref"} {
		code, err := Generate(lists, GenOptions{Package: pkg, Start: "main", Stream: true, Observability: true, disableNodeFrameScratch: pkg == "listsref"})
		if err != nil {
			t.Fatal(err)
		}
		files[pkg+"/parser.go"] = string(code)
		files[pkg+"/parser_test.go"] = strings.ReplaceAll(generatedStreamListsConsumer, "package lists", "package "+pkg)
	}
	dir := t.TempDir()
	for name, source := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

const generatedStreamConsumer = `package fixture

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
)

const input = "a=1\nbc=22\nd=3\n"

func TestTraceAndProfile(t *testing.T) {
	var events []TraceEvent
	profile := &Profile{}
	n := 0
	err := ParseStream(strings.NewReader(input), func(*Node) error { n++; return nil },
		WithTrace(func(e TraceEvent) { events = append(events, e) }), WithProfile(profile))
	if err != nil || n != 3 {
		t.Fatal(err, n)
	}
	if len(events) == 0 || events[0].Rule != "main" || events[0].Kind != TraceEnter {
		t.Fatal(events)
	}
	depth, items := 0, 0
	for _, e := range events {
		switch e.Kind {
		case TraceEnter:
			depth++
			if e.Rule == "item" {
				items++
			}
		case TraceExit:
			depth--
		}
		if e.ExaminedKnown {
			t.Fatal("stream claimed an examined range", e)
		}
	}
	if depth != 0 || items != 4 { // three elements and the failed attempt at the end
		t.Fatal(depth, items, events)
	}
	if s := profile.Snapshot(); s.Parses != 1 {
		t.Fatal(s)
	}
}

func TestWorkControls(t *testing.T) {
	var limit *WorkLimitError
	n := 0
	err := ParseStream(strings.NewReader(strings.Repeat(input, 1000)), func(*Node) error { n++; return nil }, WithWorkLimits(WorkLimits{MaxSteps: 20000}))
	if !errors.As(err, &limit) || n == 0 || n >= 3000 {
		t.Fatal(err, n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	n = 0
	err = ParseStream(strings.NewReader(input), func(*Node) error { n++; cancel(); return nil }, WithContext(ctx))
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatal(err, n)
	}
	// A reader error wins over a cancellation observed after the same Read.
	ctx, cancel = context.WithCancel(context.Background())
	failed := errors.New("failed")
	err = ParseStream(readFunc(func([]byte) (int, error) { cancel(); return 0, failed }), func(*Node) error { return nil }, WithContext(ctx))
	if err != failed {
		t.Fatal(err)
	}
	if err := ParseStream(strings.NewReader(input), nil, WithMaxDepth(-1)); err == nil {
		t.Fatal("negative depth accepted")
	}
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(b []byte) (int, error) { return f(b) }

func TestCallbackPanic(t *testing.T) {
	type marker struct{}
	defer func() {
		if x := recover(); x != (marker{}) {
			t.Fatal(x)
		}
	}()
	ParseStream(strings.NewReader(input), func(*Node) error { panic(marker{}) })
	t.Fatal("no panic")
}

type lineReader struct {
	line string
	n    int
	buf  string
}

func (r *lineReader) Read(b []byte) (int, error) {
	for len(r.buf) < len(b) && r.n > 0 {
		r.buf += r.line
		r.n--
	}
	if r.buf == "" {
		return 0, io.EOF
	}
	n := copy(b, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// Retaining elements, input or memo entries would take tens of megabytes.
func TestMemoryIsBounded(t *testing.T) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		const n = 60000
		var at [2]uint64
		count := 0
		err := ParseStream(&lineReader{line: "key=42\n", n: n}, func(*Node) error {
			count++
			if count == n/3 || count == n {
				var ms runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&ms)
				at[count/n] = ms.HeapAlloc
			}
			return nil
		}, WithUnit(unit))
		if err != nil || count != n {
			t.Fatal(err, count)
		}
		if grown := int64(at[1]) - int64(at[0]); grown > 4<<20 {
			t.Errorf("unit %v: reachable memory grew by %d KiB", unit, grown>>10)
		}
	}
}
`

const generatedStreamListsConsumer = `package lists

import (
	"io"
	"runtime"
	"testing"
)

// listReader produces n elements with lists of pseudo-random length.
type listReader struct {
	n, seed int
	buf     []byte
}

func (r *listReader) Read(b []byte) (int, error) {
	for len(r.buf) < len(b) && r.n > 0 {
		r.seed = (r.seed*1103515245 + 12345) & 0x7fffffff
		r.buf = append(r.buf, "(a b ["...)
		for i := range r.seed % 40 {
			r.buf = append(r.buf, byte('a'+i%26), ' ')
		}
		r.buf = append(r.buf, "])\n"...)
		r.n--
	}
	if len(r.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(b, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// Retaining elements, through shared allocation chunks or profile
// bookkeeping, would take hundreds of megabytes.
func TestMemoryIsBounded(t *testing.T) {
	for _, profiled := range []bool{false, true} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			const n = 60000
			var at [2]uint64
			count := 0
			opts := []ParseOption{WithUnit(unit)}
			if profiled {
				opts = append(opts, WithProfile(&Profile{}))
			}
			err := ParseStream(&listReader{n: n, seed: 1}, func(*Node) error {
				count++
				if count == n/3 || count == n {
					var ms runtime.MemStats
					runtime.GC()
					runtime.ReadMemStats(&ms)
					at[count/n] = ms.HeapAlloc
				}
				return nil
			}, opts...)
			if err != nil || count != n {
				t.Fatal(err, count)
			}
			if grown := int64(at[1]) - int64(at[0]); grown > 4<<20 {
				t.Errorf("profiled %v, unit %v: reachable memory grew by %d KiB", profiled, unit, grown>>10)
			}
		}
	}
}
`

const generatedStreamMemoConsumer = `package memo

import (
	"io"
	"runtime"
	"testing"
)

type lineReader struct {
	line string
	n    int
	buf  string
}

func (r *lineReader) Read(b []byte) (int, error) {
	for len(r.buf) < len(b) && r.n > 0 {
		r.buf += r.line
		r.n--
	}
	if r.buf == "" {
		return 0, io.EOF
	}
	n := copy(b, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// Retaining memo entries, variable state or elements would take tens of
// megabytes.
func TestMemoryIsBounded(t *testing.T) {
	for _, profiled := range []bool{false, true} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			const n = 60000
			var at [2]uint64
			count := 0
			opts := []ParseOption{WithUnit(unit)}
			if profiled {
				opts = append(opts, WithProfile(&Profile{}))
			}
			err := ParseStream(&lineReader{line: "key=42\n", n: n}, func(*Node) error {
				count++
				if count == n/3 || count == n {
					var ms runtime.MemStats
					runtime.GC()
					runtime.ReadMemStats(&ms)
					at[count/n] = ms.HeapAlloc
				}
				return nil
			}, opts...)
			if err != nil || count != n {
				t.Fatal(err, count)
			}
			if grown := int64(at[1]) - int64(at[0]); grown > 4<<20 {
				t.Errorf("profiled %v, unit %v: reachable memory grew by %d KiB", profiled, unit, grown>>10)
			}
		}
	}
}
`
