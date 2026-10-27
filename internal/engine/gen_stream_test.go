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
)

// streamRun is one stream parse of a differential test. The reader returns
// at most Chunk bytes per Read (all of the rest if zero); EOFWithData returns
// io.EOF with the last bytes, and Fail returns errStreamRead instead of EOF.
// StopAt makes the StopAt-th emit return errStreamStop; NilEmit passes no
// callback. Diag records the committed recoveries with a diagnostic callback,
// whose DiagStopAt-th call returns errDiagStop, and Limit sets a diagnostic
// limit if LimitSet. Iterate pulls the elements with an iterator instead,
// closing it where emit would stop.
type streamRun struct {
	Case        int
	Input       []byte // bytes: JSON would replace invalid UTF-8 in a string
	Bytes       bool
	Chunk       int
	EOFWithData bool
	Fail        bool
	StopAt      int
	NilEmit     bool
	Diag        bool
	DiagStopAt  int
	Limit       int
	LimitSet    bool
	Iterate     bool
}

// streamDiagVariants are the stream runs with diagnostic options and
// iterators of a differential test.
var streamDiagVariants = []streamRun{{Diag: true}, {Diag: true, Chunk: 1, DiagStopAt: 1}, {Diag: true, NilEmit: true},
	{LimitSet: true, Limit: 1}, {LimitSet: true, Limit: 0}, {LimitSet: true, Limit: 2, NilEmit: true},
	{Iterate: true}, {Iterate: true, Chunk: 1, StopAt: 2}, {Iterate: true, Diag: true, LimitSet: true, Limit: 1}}

var (
	errStreamRead = errors.New("read failed")
	errStreamStop = errors.New("stop")
	errDiagStop   = errors.New("diagnostic stop")
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

// streamTestDrain is shared, as source, with the generated test program.
const streamTestDrain = `
// drain pulls every element of an iterator and passes it to emit. An emit
// error closes the iterator, which must then report it closed, and is
// returned. Repeated calls after the end return the same result.
func drain[T any](next func() (T, bool, error), close func(), emit func(T) error, closed func(error) bool) error {
	for {
		n, ok, err := next()
		if !ok {
			if _, ok2, err2 := next(); ok2 || fmt.Sprintf("%T %v", err2, err2) != fmt.Sprintf("%T %v", err, err) {
				panic("unstable iterator result")
			}
			close()
			if _, _, err3 := next(); fmt.Sprintf("%T %v", err3, err3) != fmt.Sprintf("%T %v", err, err) {
				panic("close changed the result")
			}
			return err
		}
		if err := emit(n); err != nil {
			close()
			close()
			if _, ok, err := next(); ok || !closed(err) {
				panic("closed iterator returned " + fmt.Sprint(ok, err))
			}
			return err
		}
	}
}
`

// drain pulls every element of an iterator and passes it to emit. An emit
// error closes the iterator, which must then report it closed, and is
// returned. Repeated calls after the end return the same result.
func drain[T any](next func() (T, bool, error), close func(), emit func(T) error, closed func(error) bool) error {
	for {
		n, ok, err := next()
		if !ok {
			if _, ok2, err2 := next(); ok2 || fmt.Sprintf("%T %v", err2, err2) != fmt.Sprintf("%T %v", err, err) {
				panic("unstable iterator result")
			}
			close()
			if _, _, err3 := next(); fmt.Sprintf("%T %v", err3, err3) != fmt.Sprintf("%T %v", err, err) {
				panic("close changed the result")
			}
			return err
		}
		if err := emit(n); err != nil {
			close()
			close()
			if _, ok, err := next(); ok || !closed(err) {
				panic("closed iterator returned " + fmt.Sprint(ok, err))
			}
			return err
		}
	}
}

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
	Diags []string          `json:"diags,omitempty"`
	Err   string            `json:"err,omitempty"`
}

// streamMemoGrammar's elements backtrack into a memoized rule that reads
// variables, bind variables and produce nil.
const streamMemoGrammar = `var n int
var last string
rule main = {n := 0} item* #stream $$
rule item = (k:key ":" / k:key {n := n + 1} {last := text($k)} "=") val "\n" => nil
rule key = {n >= 0} word
rule word = [a-z]+ #atomic
rule val = [0-9]+ #atomic`

var streamCorpus = []genCase{
	{"records", records, []string{"#records\na=1\nbc=22\n", "#records\nx\n", "#records\n", "", "#records\na=1\nb"}},
	{"actions", `type Pair = { Key node[string]; Value node[string] }
rule main = item* #stream $$
rule item Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" => Pair{Key: $k, Value: $v}`, []string{"a=1\nb=2\n", "a=1\nb=x\n", ""}},
	{"discarded text", `type T = { H string; N int }
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
rule e = #operators {
    operand [0-9]+ #atomic
    level { infix left "+" }
    level { prefix "-" }
}`, []string{"1+-2;3;", "-;", "1+2+3;"}},
	{"cut elements", `rule main = item* #stream $$
rule item = "a" #cut "b" / "c"`, []string{"abc", "ac", "abab"}},
	{"variables", `var n int
rule main = {n := 0} item* #stream $$
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
	// The groups and attributes around the stream repetition or its sequence do not change what
	// streams.
	{"stream in an attributed sequence", `rule main = (([a-z] #atomic)* #stream "!") @diagnostic(name="x") $$`, []string{"ab!", "!", "", "a"}},
	{"stream in an attributed group", `rule main = (item* #stream) @diagnostic(name="items") "." $$
rule item = [a-z] #atomic`, []string{"ab.", "a", ".", "ab"}},
	// Shapes around the stream repetition and its elements: a diagnostic or an error label on the repetition
	// itself, captures with an action in each element, a recursive start rule that keeps captures, the
	// environment changed by an element and read by a predicate and a lookahead after it, recovery in the
	// element rule, and recovery in the start rule.
	{"diagnostic on the repetition", `rule main = (item* #stream @diagnostic(name="items")) "." $$
rule item = [a-z] #atomic`, []string{"ab.", "a", ".", "ab1"}},
	{"error label on the repetition", `rule main = (item* #stream @error(message="expected items")) "." $$
rule item = [a-z] #atomic`, []string{"ab.", "a", ".", "ab1"}},
	{"element captures and actions", `rule main = h:"#" rs:item* #stream t:"!"? => ($h, len($rs), $t)
rule item = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic ";" => ($k, $v)`, []string{"#a=1;b=22;!", "#a=1;b=;", "#", "#a=1;b=2"}},
	{"recursive start with captures", `rule main = a:item* #stream b:"."? => ($a, $b)
rule item = ("(" m:main ")" => $m) / (x:[a-z] #atomic => $x)`, []string{"a(b)c.", "((a)", "(a(b))", strings.Repeat("(", 40) + "a" + strings.Repeat(")", 40)}},
	{"environment, predicate and lookahead", `var n int
rule main = {n := 0} item* #stream &"!" $$ "!"
rule item = "a" {n := n + 1} / "b" {n > 1} / "c" !"d"`, []string{"aab!", "ab!", "aacc!", "acd!", "b"}},
	{"recovery in the element rule", `rule main = stmt* #stream $$
rule stmt = (word ";") #recover(skip=[^;]* ";")
rule word = [a-z]+ #atomic`, []string{"ab;1;cd;", "1;ab", "ab;;", ";"}},
	{"recovery in the start rule", `rule main = (word ";" #recover(skip=[^;]* ";"))* #stream $$
rule word = [a-z]+ #atomic`, []string{"ab;1;cd;", "1;ab", "ab;;", ";"}},
	// Each growth of the start rule allocates its capture frame again.
	// A leader that grows through an optional prefix of itself before the streamed elements, with a mandatory end.
	{"left recursive start choice", `rule main = (main "+")? item* #stream "."
rule item = q:[a-z] #atomic`, []string{"ab.", "ab.+c.", "ab.+c.+d.", ".", "+", "ab", "ab.+", ""}},
	// An indirect cycle and mutual leaders through the start rule, whose elements stream after the cycle.
	{"indirect leader start", `rule main = (a "y")? item* #stream "."
rule a = main "x" / "n"
rule item = q:[a-z] #atomic`, []string{"ab.", "nxy.", "ab.xyab.", "ab.xy", "n", "", "ab.x"}},
	{"mutual leader start", `rule main = (a "w")? item* #stream "."
rule a = main "x" / a "v" / "n"
rule item = q:[a-z] #atomic`, []string{"nw.", "nvw.", "nvvw.", "ab.xwc.", "nwab.xvw.", "nwab", "nv", "w.", ""}},
	// Cuts and recovery in streamed elements and in a leader start rule.
	{"stream element with a cut and recovery", `rule main = stmt* #stream $$
rule stmt = ("(" #cut word ")") #recover(skip=[^)]* ")") / word ";"
rule word = [a-z]+ #atomic`, []string{"(ab)cd;", "(1)ab;", "(ab", "(1", "cd;(ab)", ";", ""}},
	{"leader start rule with recovery", `rule main = (main "+")? item* #stream "."
rule item = ([a-z] ";") #recover(skip=[^;.]* ";")`, []string{"a;b;.", "a;b;.+c;.", "ab;.", "1;.", "a;.+", "a;.+1;.", ""}},
	{"left recursive start", `rule main = a:(main "x")? b:item* #stream c:"."?
rule item = q:[a-z] #atomic`, []string{"ab.", "a", "", "ab.x"}},
}

// TestGeneratedStreamMatchesEngine compares generated ParseStream with the
// engine's ParseStream: emitted elements, the reader calls made before each,
// and the final error, for reader chunkings, reader errors, emit errors and a
// nil callback, in both position units.
func TestGeneratedStreamMatchesEngine(t *testing.T) { eachMode(t, testGeneratedStreamMatchesEngine) }

func testGeneratedStreamMatchesEngine(t *testing.T) {
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
	for name, opts := range map[string]GenOptions{"default": {},
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
	write("go.mod", testGoMod("streamtest"))
	var imports, parsers strings.Builder
	var runs []streamRun
	var want []string
	var names []string
	for i, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts.Package, opts.Start, opts.Stream, opts.SourceInfo = fmt.Sprintf("g%d", i), "main", true, true // compile gives the engine source information
		code, err := generateCode(g, opts)
		if err != nil {
			if strings.Contains(err.Error(), "needs a #stream repetition") {
				continue // the corpus grammar has #stream elsewhere
			}
			t.Fatalf("%s: %v", c.name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"streamtest/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, `	%[1]d: func(r io.Reader, emit func(any) error, v run, diag func(string) error) error {
		u := g%[1]d.CodePoints
		if v.Bytes {
			u = g%[1]d.Bytes
		}
		opts := []g%[1]d.ParseOption{g%[1]d.WithUnit(u)}
		if diag != nil {
			opts = append(opts, g%[1]d.WithDiagnosticCallback(func(e *g%[1]d.SyntaxError) error {
				err := diag(e.Error())
				// The callback owns its copy.
				e.Expected, e.Messages, e.Pos = append(e.Expected, "mutated"), nil, -1
				return err
			}))
		}
		if v.LimitSet {
			opts = append(opts, g%[1]d.WithDiagnosticLimit(v.Limit))
		}
		if v.Iterate {
			it, err := g%[1]d.Iterate(r, opts...)
			if err != nil {
				return err
			}
			return drain(it.Next, it.Close, func(n *g%[1]d.Node) error { return emit(n) }, func(err error) bool { return err == g%[1]d.ErrStreamClosed })
		}
		var e func(*g%[1]d.Node) error
		if emit != nil {
			e = func(n *g%[1]d.Node) error { return emit(n) }
		}
		return g%[1]d.ParseStream(r, e, opts...)
	},
`, i)
		prog := compile(t, c.src, Options{noProjections: true})
		for _, in := range c.inputs {
			for _, bytes := range []bool{false, true} {
				variants := append([]streamRun{{Chunk: 0}, {Chunk: 1}, {Chunk: 2, EOFWithData: true}, {Chunk: 3, Fail: true}, {Chunk: 1, StopAt: 2}, {NilEmit: true}}, streamDiagVariants...)
				if len(in) > 1000 {
					variants = []streamRun{{Chunk: 0}, {Chunk: 4093}, {Chunk: 7, StopAt: 1500}, {Iterate: true, Chunk: 4093, Diag: true, LimitSet: true, Limit: 3}}
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
	errDiagStop   = errors.New("diagnostic stop")
)

`+streamTestReader+streamTestDrain+`
var parsers = map[int]func(io.Reader, func(any) error, run, func(string) error) error{
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
	Diag        bool
	DiagStopAt  int
	Limit       int
	LimitSet    bool
	Iterate     bool
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
			Diags []string          `+"`json:\"diags,omitempty\"`"+`
			Err   string            `+"`json:\"err,omitempty\"`"+`
		}
		var diag func(string) error
		if v.Diag {
			diag = func(e string) error {
				res.Diags = append(res.Diags, fmt.Sprint(len(res.Emits), " ", e))
				if len(res.Diags) == v.DiagStopAt {
					return errDiagStop
				}
				return nil
			}
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
		if err := parsers[v.Case](r, emit, v, diag); err != nil {
			res.Err = err.Error()
			if err == errStreamRead || err == errStreamStop || err == errDiagStop {
				res.Err = "caller " + err.Error()
			} else if errors.Is(err, errStreamRead) || errors.Is(err, errStreamStop) || errors.Is(err, errDiagStop) {
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
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
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
	o := ParseOptions{Unit: CodePoints, DiagnosticLimit: v.Limit, DiagnosticLimitSet: v.LimitSet}
	if v.Bytes {
		o.Unit = Bytes
	}
	if v.Diag {
		o.DiagnosticCallback = func(e *SyntaxError) error {
			res.Diags = append(res.Diags, fmt.Sprint(len(res.Emits), " ", e.Error()))
			e.Expected, e.Messages, e.Pos = append(e.Expected, "mutated"), nil, -1
			if len(res.Diags) == v.DiagStopAt {
				return errDiagStop
			}
			return nil
		}
	}
	var err error
	if v.Iterate {
		var it *StreamIterator
		if it, err = prog.Iterate("main", r, o); err == nil {
			err = drain(it.Next, it.Close, emit, func(err error) bool { return err == ErrStreamClosed })
		}
	} else {
		err = prog.ParseStreamWith("main", r, emit, o)
	}
	if err != nil {
		res.Err = err.Error()
		if err == errStreamRead || err == errStreamStop || err == errDiagStop {
			res.Err = "caller " + err.Error()
		} else if errors.Is(err, errStreamRead) || errors.Is(err, errStreamStop) || errors.Is(err, errDiagStop) {
			res.Err = "wrapped " + err.Error()
		}
	}
	b, _ := json.Marshal(res)
	return string(b)
}

func TestGenerateStreamOptions(t *testing.T) { eachMode(t, testGenerateStreamOptions) }

func testGenerateStreamOptions(t *testing.T) {
	g, err := parseTest(`rule main = item* #stream $$
rule item = "a"
rule other = "b"`)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := generateCode(g, GenOptions{Package: "fixture", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"stream:", "streamInput", "func ParseStream", "\"bufio\"", "WithDiagnostic", "func Iterate", "StreamIterator"} {
		if strings.Contains(string(plain), marker) {
			t.Errorf("ordinary output contains %s", marker)
		}
	}
	code, err := generateCode(g, GenOptions{Package: "fixture", Start: "main", Stream: true})
	if err != nil || !strings.Contains(string(code), "func ParseStream(") || strings.Contains(string(code), "stream:") {
		t.Fatalf("stream output: %v", err)
	}
	for _, tc := range []struct {
		opts GenOptions
		want string
	}{
		{GenOptions{Start: "other", Stream: true}, "needs a #stream repetition"},
	} {
		tc.opts.Package = "fixture"
		if _, err := generateCode(g, tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v, want %q", tc.opts, err, tc.want)
		}
	}
	if _, err := generateTSBundle(g, GenOptions{Start: "main", Stream: true}); err == nil {
		t.Error("TypeScript accepted stream generation")
	}
	typed, err := generateCode(g, GenOptions{Package: "fixture", Start: "main", Stream: true, Types: true})
	if err != nil || !strings.Contains(string(typed), "func ParseStreamAST(") || !strings.Contains(string(typed), "type StreamValue = ") {
		t.Fatalf("typed stream output: %v", err)
	}
	if typed, err := generateCode(g, GenOptions{Package: "fixture", Start: "main", Types: true}); err != nil || strings.Contains(string(typed), "ParseStreamAST") {
		t.Fatalf("typed output without streams: %v", err)
	}
}

// typedStreamCorpus has #stream grammars whose element types cover the typed
// mappings. batch extracts the repetition from the value bv of the batch
// ParseAST; empty skips the batch comparison.
var typedStreamCorpus = []struct {
	name, src, batch string
	inputs           []string
}{
	{"structs", `type Pair = { Key node[string]; Value node[string] }
rule main = item* #stream
rule item Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" => Pair{Key: $k, Value: $v}`, "bv.Value", []string{"a=1\nbc=22\n", "a=1\nb=x\n", "", strings.Repeat("ab=12\n", 1500)}},
	{"unions", `type A = { Text string }
type B = { Text string }
type U ~ A | B
rule main = item* #stream
rule item U = a / b
rule a A = t:("a") #atomic => A{Text: text($t)}
rule b B = t:("b") #atomic => B{Text: text($t)}`, "bv.Value", []string{"abba", "abc", ""}},
	{"records", `rule main = item* #stream
rule item = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic ";"`, "bv.Value", []string{"a=1;bc=22;", "a=1;b", strings.Repeat("ab=1;", 2000)}},
	{"inline records", `rule main = (k:[a-z] v:[0-9]?)* #stream $$`, "", []string{"a1bc2", "a1!"}},
	{"recovered", `type S = { N node[string] }
rule main = (stmt #recover(skip=[^;]+ ";"))* #stream
rule stmt S = n:[a-z]+ #atomic ";" => S{N: $n}`, "bv.Value", []string{"ab;1x;cd;9;", "1x;ab", "ab;"}},
	{"nil", `rule main = item* #stream
rule item = "a" => nil`, "bv.Value", []string{"aaa", "ab"}},
	{"matches", `rule main = ("a" / "é")* #stream`, "bv.Value", []string{"aéa", "é\xc3", "a\xffa"}},
	{"lists", `rule main = item* #stream
rule item = xs:[a-z]* ";" => $xs`, "bv.Value", []string{"ab;;c;", "ab;c"}},
	{"aliases", `type P = { A node[string]; B node[string] }
rule main = item* #stream
rule item P = x:[a-z] => P{A: $x, B: $x}`, "bv.Value", []string{"abc", "a1"}},
	{"surrounded", `rule main = h:"#" xs:item* #stream t:"!"?
rule item = [a-z] #atomic`, "", []string{"#ab!", "#", "#a?"}},
	{"captured", `type I = { C node[string] }
rule main = "<" xs:(item* #stream) ">"
rule item I = c:[a-z] ";" => I{C: $c}`, "", []string{"<a;b;>", "<>", "<a;b"}},
}

// TestGeneratedTypedStreamMatchesBatch compares the elements that generated
// ParseStreamAST passes to emit with the repetition of the batch ParseAST
// value, and its errors and number of elements with ParseStream, for reader
// chunkings, reader errors, emit errors and a nil callback, in both units.
func TestGeneratedTypedStreamMatchesBatch(t *testing.T) {
	eachMode(t, testGeneratedTypedStreamMatchesBatch)
}

func testGeneratedTypedStreamMatchesBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("typedstream"))
	var imports, parsers strings.Builder
	var runs []streamRun
	var names []string
	for i, c := range typedStreamCorpus {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code, err := generateCode(g, GenOptions{Package: fmt.Sprintf("g%d", i), Start: "main", Stream: true, Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"typedstream/g%d\"\n", i, i)
		batch := "nil"
		if c.batch != "" {
			// The start rule's value is a node of the repetition: bv.Value is its list.
			batch = "func(input string, b bool) (any, error) {\n\t\tbv, err := g%[1]d.ParseAST(input, g%[1]d.WithUnit(unit%[1]d(b)))\n\t\tif bv == nil {\n\t\t\treturn nil, err\n\t\t}\n\t\treturn " + c.batch + ", err\n\t}"
		}
		fmt.Fprintf(&parsers, `	%[1]d: {
		typed: func(r io.Reader, emit func(any) error, b bool) error {
			var e func(g%[1]d.StreamValue) error
			if emit != nil {
				e = func(v g%[1]d.StreamValue) error { return emit(v) }
			}
			return g%[1]d.ParseStreamAST(r, e, g%[1]d.WithUnit(unit%[1]d(b)))
		},
		nodes: func(r io.Reader, emit func(any) error, b bool) error {
			var e func(*g%[1]d.Node) error
			if emit != nil {
				e = func(n *g%[1]d.Node) error { return emit(n) }
			}
			return g%[1]d.ParseStream(r, e, g%[1]d.WithUnit(unit%[1]d(b)))
		},
		batch: `+batch+`,
	},
`, i)
		write(fmt.Sprintf("unit%d.go", i), fmt.Sprintf("package main\n\nimport g%[1]d \"typedstream/g%[1]d\"\n\nfunc unit%[1]d(b bool) g%[1]d.PositionUnit {\n\tif b {\n\t\treturn g%[1]d.Bytes\n\t}\n\treturn g%[1]d.CodePoints\n}\n", i))
		for _, in := range c.inputs {
			for _, bytes := range []bool{false, true} {
				variants := []streamRun{{Chunk: 0}, {Chunk: 1}, {Chunk: 2, EOFWithData: true}, {Chunk: 3, Fail: true}, {Chunk: 1, StopAt: 2}, {NilEmit: true}}
				if len(in) > 1000 {
					variants = []streamRun{{Chunk: 0}, {Chunk: 7, StopAt: 1500}}
				}
				for _, v := range variants {
					v.Case, v.Input, v.Bytes = i, []byte(in), bytes
					runs = append(runs, v)
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
	errDiagStop   = errors.New("diagnostic stop")
)

`+streamTestReader+`
type parser struct {
	typed, nodes func(io.Reader, func(any) error, bool) error
	batch        func(string, bool) (any, error)
}

var parsers = map[int]parser{
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

type result struct {
	Emits    []json.RawMessage `+"`json:\"emits\"`"+`
	Err      string            `+"`json:\"err\"`"+`
	Nodes    int               `+"`json:\"nodes\"`"+`
	NodeErr  string            `+"`json:\"nodeErr\"`"+`
	Batch    json.RawMessage   `+"`json:\"batch,omitempty\"`"+`
	BatchErr string            `+"`json:\"batchErr\"`"+`
	Retained json.RawMessage   `+"`json:\"retained\"`"+`
}

func errString(err error) string {
	switch {
	case err == nil:
		return ""
	case err == errStreamRead || err == errStreamStop:
		return "caller " + err.Error()
	case errors.Is(err, errStreamRead) || errors.Is(err, errStreamStop):
		return "wrapped " + err.Error()
	}
	return err.Error()
}

func encode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func main() {
	var runs []run
	data, _ := os.ReadFile(os.Args[1])
	if err := json.Unmarshal(data, &runs); err != nil {
		panic(err)
	}
	for _, v := range runs {
		p := parsers[v.Case]
		var res result
		var values []any
		emit := func(store bool) func(any) error {
			if v.NilEmit {
				return nil
			}
			n := 0
			return func(x any) error {
				n++
				if store {
					res.Emits = append(res.Emits, encode(x))
					values = append(values, x)
				} else {
					res.Nodes++
				}
				if n == v.StopAt {
					return errStreamStop
				}
				return nil
			}
		}
		res.Err = errString(p.typed(&chunkReader{s: string(v.Input), chunk: v.Chunk, eofWithData: v.EOFWithData, fail: v.Fail}, emit(true), v.Bytes))
		// Values kept until the parse ended still encode as when emitted.
		res.Retained = encode(values)
		res.NodeErr = errString(p.nodes(&chunkReader{s: string(v.Input), chunk: v.Chunk, eofWithData: v.EOFWithData, fail: v.Fail}, emit(false), v.Bytes))
		if p.batch != nil {
			bv, err := p.batch(string(v.Input), v.Bytes)
			res.Batch, res.BatchErr = encode(bv), errString(err)
		}
		fmt.Println(string(encode(res)))
	}
}
`)
	data, _ := json.Marshal(runs)
	write("runs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "runs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(runs) {
		t.Fatalf("got %d results, want %d\n%s", len(lines), len(runs), out)
	}
	compared := 0
	for k, line := range lines {
		var res struct {
			Emits    []json.RawMessage `json:"emits"`
			Err      string            `json:"err"`
			Nodes    int               `json:"nodes"`
			NodeErr  string            `json:"nodeErr"`
			Batch    json.RawMessage   `json:"batch"`
			BatchErr string            `json:"batchErr"`
			Retained json.RawMessage   `json:"retained"`
		}
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			t.Fatalf("%s: %v\n%s", names[k], err, line)
		}
		v := runs[k]
		if res.Err != res.NodeErr || len(res.Emits) != res.Nodes {
			t.Errorf("%s\n typed  %d elements, %s\n nodes  %d elements, %s", names[k], len(res.Emits), res.Err, res.Nodes, res.NodeErr)
		}
		emits, _ := json.Marshal(res.Emits)
		if res.Emits == nil {
			emits = []byte("null")
		}
		if string(res.Retained) != string(emits) {
			t.Errorf("%s: retained values changed\n emitted  %s\n retained %s", names[k], emits, res.Retained)
		}
		// Only a parse read to its end with every element emitted has
		// the batch value's elements.
		if res.Batch == nil || v.Fail || v.StopAt != 0 || v.NilEmit {
			continue
		}
		if res.Err != res.BatchErr {
			t.Errorf("%s\n stream %s\n batch  %s", names[k], res.Err, res.BatchErr)
			continue
		}
		if res.Err != "" && string(res.Batch) == "null" {
			continue // a failed batch parse has no value
		}
		var batch []json.RawMessage
		if err := json.Unmarshal(res.Batch, &batch); err != nil {
			t.Fatalf("%s: batch value %s: %v", names[k], res.Batch, err)
		}
		if len(batch) != len(res.Emits) {
			t.Errorf("%s\n stream %s\n batch  %s", names[k], emits, res.Batch)
			continue
		}
		for j := range batch {
			if string(batch[j]) != string(res.Emits[j]) {
				t.Errorf("%s element %d\n stream %s\n batch  %s", names[k], j, res.Emits[j], batch[j])
			}
		}
		compared++
	}
	if compared == 0 {
		t.Fatal("no batch comparison")
	}
}

// TestGenerateStreamKeepsBatchBodies checks that stream generation leaves
// the batch parsing code unchanged: every generated expression method and
// the rule table of the ordinary output appear unchanged in the stream output,
// whose stream body comes in addition.
func TestGenerateStreamKeepsBatchBodies(t *testing.T) {
	eachMode(t, testGenerateStreamKeepsBatchBodies)
}

func testGenerateStreamKeepsBatchBodies(t *testing.T) {
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
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range []GenOptions{{}, {Observability: true}} {
			opts.Package, opts.Start = "p", "main"
			plain, err := generateCode(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			opts.Stream = true
			stream, err := generateCode(g, opts)
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
				// the specs, the string lists and the rare fields of the rules
				i := strings.Index(src, "var rulesSpec = [...]rt.RuleSpec{")
				j := i + strings.Index(src[i:], "var rulesExtras = [...]rt.RuleExtra{")
				return src[i : j+strings.Index(src[j:], "\n\n")]
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
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds a generated standalone stream consumer")
	}
	g, err := parseTest(`type Pair = { Key node[string]; Value node[string] }
rule main = item* #stream $$
rule item Pair = k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n" => Pair{Key: $k, Value: $v}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(g, GenOptions{Package: "fixture", Start: "main", Stream: true, Observability: true})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":         testGoMod("fixture"),
		"parser.go":      string(code),
		"parser_test.go": generatedStreamConsumer,
	}
	// Elements of varying size end at varying places in the allocation
	// chunks, and the reference frame allocator has no scratch to rewind.
	lists, err := parseTest(`rule main = item* #stream $$
rule item = "(" a:atom b:atom c:list ")" "\n"
rule list = "[" xs:atom* "]"
rule atom = [a-z] #atomic " "?`)
	if err != nil {
		t.Fatal(err)
	}
	memo, err := parseTest(streamMemoGrammar)
	if err != nil {
		t.Fatal(err)
	}
	code, err = generateCode(memo, GenOptions{Package: "memo", Start: "main", Stream: true, Observability: true})
	if err != nil {
		t.Fatal(err)
	}
	files["memo/parser.go"] = string(code)
	typed, err := parseTest(`type P = { A node[string]; B node[string]; Xs []node[string] }
rule main = item* #stream $$
rule item P = x:[a-z] ys:[0-9]* ";" => P{A: $x, B: $x, Xs: $ys.Value}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err = generateCode(typed, GenOptions{Package: "typed", Start: "main", Stream: true, Types: true})
	if err != nil {
		t.Fatal(err)
	}
	files["typed/parser.go"] = string(code)
	recovering, err := parseTest(`type S = { N node[string] }
rule main = (stmt #recover(skip=[^;]+ ";"))* #stream $$
rule stmt S = n:[a-z]+ #atomic ";" => S{N: $n}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err = generateCode(recovering, GenOptions{Package: "recovering", Start: "main", Stream: true, Types: true, Document: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	files["recovering/parser.go"] = string(code)
	files["recovering/parser_test.go"] = generatedRecoveringStreamConsumer
	files["typed/parser_test.go"] = generatedTypedStreamConsumer
	files["memo/parser_test.go"] = generatedStreamMemoConsumer
	for _, pkg := range []string{"lists"} {
		code, err := generateCode(lists, GenOptions{Package: pkg, Start: "main", Stream: true, Observability: true})
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

const generatedRecoveringStreamConsumer = `package recovering

import (
	pegort "github.com/ornew/pego/runtime"
	"errors"
	"io"
	"runtime"
	"strings"
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

// Retaining every recovered error would take tens of megabytes.
func TestDiagnosticLimitBoundsMemory(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		const n = 100000
		var at [2]uint64
		count, diags := 0, 0
		err := ParseStream(&lineReader{line: "ab;1x;", n: n}, func(*Node) error {
			count++
			if count == 2*n/3 || count == 2*n {
				var ms runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&ms)
				at[count/(2*n)] = ms.HeapAlloc
			}
			return nil
		}, WithUnit(unit), pegort.WithDiagnosticLimit(3), pegort.WithDiagnosticCallback(func(*SyntaxError) error { diags++; return nil }))
		var truncated *pegort.TruncatedSyntaxErrors
		if !errors.As(err, &truncated) || truncated.Total != n || truncated.Dropped != n-3 || truncated.CountsSaturated || count != 2*n || diags != n {
			t.Fatal(err, count, diags)
		}
		var list SyntaxErrors
		if !errors.As(err, &list) || len(list) != 3 || list[2].Pos != 6*n-3 || list[1].Pos != 6*n-9 {
			t.Fatal(list)
		}
		if grown := int64(at[1]) - int64(at[0]); grown > 4<<20 {
			t.Errorf("unit %v: reachable memory grew by %d KiB", unit, grown>>10)
		}
	}
}

func TestDiagnosticOptionsAreStreamOnly(t *testing.T) {
	for _, opt := range []ParseOption{pegort.WithDiagnosticLimit(0), pegort.WithDiagnosticCallback(func(*SyntaxError) error { return nil })} {
		check := func(name string, err error) {
			if err == nil || !strings.Contains(err.Error(), "only supported for stream parsing") {
				t.Errorf("%s: %v", name, err)
			}
		}
		_, err := Parse("ab;", opt)
		check("Parse", err)
		_, err = ParseRule("stmt", "ab;", opt)
		check("ParseRule", err)
		_, err = ParseAST("ab;", opt)
		check("ParseAST", err)
		check("Recognize", Recognize("ab;", opt))
		d, err := NewDocument("ab;", opt)
		check("NewDocument", err)
		if d, err = NewDocument("ab;"); err != nil {
			t.Fatal(err)
		}
		_, err = d.Parse(opt)
		check("Document.Parse", err)
		_, err = d.ParseAST(opt)
		check("Document.ParseAST", err)
	}
	// A nil callback is not an option to reject.
	if _, err := Parse("ab;", pegort.WithDiagnosticCallback(nil)); err != nil {
		t.Error(err)
	}
	read := false
	r := readFunc(func([]byte) (int, error) { read = true; return 0, io.EOF })
	if err := ParseStream(r, nil, pegort.WithDiagnosticLimit(-1)); err == nil || !strings.Contains(err.Error(), "nonnegative") {
		t.Error(err)
	}
	if _, err := Iterate(r, pegort.WithDiagnosticLimit(-1)); err == nil {
		t.Error("Iterate accepted a negative limit")
	}
	if _, err := IterateAST(r, WithMaxDepth(-1)); err == nil {
		t.Error("IterateAST accepted a negative depth")
	}
	if read {
		t.Error("invalid options read the input")
	}
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(b []byte) (int, error) { return f(b) }

func TestIterateAST(t *testing.T) {
	var want []StreamValue
	err := ParseStreamAST(strings.NewReader("ab;1x;cd;"), func(v StreamValue) error { want = append(want, v); return nil })
	var errs SyntaxErrors
	if !errors.As(err, &errs) || len(errs) != 1 || len(want) != 3 {
		t.Fatal(err, want)
	}
	var diags []*SyntaxError
	it, err := IterateAST(strings.NewReader("ab;1x;cd;"), pegort.WithDiagnosticCallback(func(e *SyntaxError) error { diags = append(diags, e); return nil }))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		v, ok, err := it.Next()
		if !ok {
			if i != 3 || err == nil || err.Error() != errs.Error() || len(diags) != 1 {
				t.Fatal(i, err, diags)
			}
			break
		}
		if i == 1 {
			if _, isError := v.(*Error); !isError || len(diags) != 1 || diags[0].Pos != 3 {
				t.Fatal(v, diags)
			}
		}
		if s, isS := v.(*S); isS && s.N.Value != want[i].(*S).N.Value {
			t.Fatal(i, s, want[i])
		}
	}
	// Closing early abandons the parse.
	it, _ = IterateAST(strings.NewReader("ab;cd;"))
	if _, ok, err := it.Next(); !ok || err != nil {
		t.Fatal(ok, err)
	}
	it.Close()
	if _, ok, err := it.Next(); ok || err != pegort.ErrStreamClosed {
		t.Fatal(ok, err)
	}
	// A callback panic reaches the caller of Next unchanged.
	type marker struct{}
	nodes, _ := Iterate(strings.NewReader("1x;"), pegort.WithDiagnosticCallback(func(*SyntaxError) error { panic(marker{}) }))
	func() {
		defer func() {
			if x := recover(); x != (marker{}) {
				t.Fatal(x)
			}
		}()
		nodes.Next()
		t.Fatal("no panic")
	}()
	if _, ok, _ := nodes.Next(); ok {
		t.Fatal("iterator continued after a panic")
	}
}
`

const generatedTypedStreamConsumer = `package typed

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseStreamAST(t *testing.T) {
	var got []*P
	err := ParseStreamAST(strings.NewReader("a12;b;"), func(v StreamValue) error { got = append(got, v); return nil })
	if err != nil || len(got) != 2 {
		t.Fatal(err, got)
	}
	// A node in two fields converts to one value.
	if got[0].A != got[0].B || got[0].A.Value != "a" || len(got[0].Xs) != 2 || got[0].Xs[1].Value != "2" || got[1].Xs != nil && len(got[1].Xs) != 0 {
		t.Fatal(got[0], got[1])
	}
	if got[1].Start != 4 || got[1].A.Start != 4 {
		t.Fatal(got[1])
	}
	if err := ParseStreamAST(strings.NewReader("a;1"), nil); err == nil {
		t.Fatal("nil emit accepted a syntax error")
	}
}

// Converting elements is charged to the work limits like parsing them.
func TestParseStreamASTWorkControls(t *testing.T) {
	input := strings.Repeat("a"+strings.Repeat("1", 50)+";", 200)
	steps := func(f func(limits WorkLimits) error) int {
		lo, hi := 1, 1<<20
		for lo < hi {
			mid := (lo + hi) / 2
			var limit *WorkLimitError
			if err := f(WorkLimits{MaxSteps: uint64(mid)}); errors.As(err, &limit) {
				lo = mid + 1
			} else if err != nil {
				t.Fatal(err)
			} else {
				hi = mid
			}
		}
		return lo
	}
	nodes := steps(func(l WorkLimits) error {
		return ParseStream(strings.NewReader(input), func(*Node) error { return nil }, WithWorkLimits(l))
	})
	values := steps(func(l WorkLimits) error {
		return ParseStreamAST(strings.NewReader(input), func(StreamValue) error { return nil }, WithWorkLimits(l))
	})
	if values <= nodes {
		t.Fatalf("typed stream needs %d steps, node stream %d", values, nodes)
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	err := ParseStreamAST(strings.NewReader(input), func(StreamValue) error { n++; cancel(); return nil }, WithContext(ctx))
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatal(err, n)
	}
	stop := errors.New("stop")
	if err := ParseStreamAST(strings.NewReader(input), func(StreamValue) error { return stop }); err != stop {
		t.Fatal(err)
	}
}
`

const generatedStreamConsumer = `package fixture

import (
	pegort "github.com/ornew/pego/runtime"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
)

const input = "a=1\nbc=22\nd=3\n"

func TestTraceAndProfile(t *testing.T) {
	var events []pegort.TraceEvent
	profile := &pegort.Profile{}
	n := 0
	err := ParseStream(strings.NewReader(input), func(*Node) error { n++; return nil },
		pegort.WithTrace(func(e pegort.TraceEvent) { events = append(events, e) }), pegort.WithProfile(profile))
	if err != nil || n != 3 {
		t.Fatal(err, n)
	}
	if len(events) == 0 || events[0].Rule != "main" || events[0].Kind != pegort.TraceEnter {
		t.Fatal(events)
	}
	depth, items := 0, 0
	for _, e := range events {
		switch e.Kind {
		case pegort.TraceEnter:
			depth++
			if e.Rule == "item" {
				items++
			}
		case pegort.TraceExit:
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
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
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
	pegort "github.com/ornew/pego/runtime"
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
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			const n = 60000
			var at [2]uint64
			count := 0
			opts := []ParseOption{WithUnit(unit)}
			if profiled {
				opts = append(opts, pegort.WithProfile(&pegort.Profile{}))
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
	pegort "github.com/ornew/pego/runtime"
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
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			const n = 60000
			var at [2]uint64
			count := 0
			opts := []ParseOption{WithUnit(unit)}
			if profiled {
				opts = append(opts, pegort.WithProfile(&pegort.Profile{}))
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
