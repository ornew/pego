package document_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	. "github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/starttest"
	"github.com/ornew/pego/internal/syntax"
	"github.com/ornew/pego/internal/value"
)

// The tests in this file's package parse with the engine's programs. These helpers are the
// engine's own test helpers, which an external test package cannot share.

// valueModes are the modes of compiled grammars that build values: parse mode serves plain parses
// and document mode traced ones (servedBy).
var valueModes = []CompiledMode{CompiledParse, CompiledDocument}

// servedBy returns o as a parse that a program loaded in mode serves: a recognize-mode program
// serves recognition, and a document-mode program traced parses, so o gets a trace that does
// nothing unless it has one.
func servedBy(mode CompiledMode, o ParseOptions) ParseOptions {
	switch mode {
	case CompiledRecognize:
		o.Recognize = true
	case CompiledDocument:
		if o.Trace == nil {
			o.Trace = func(TraceEvent) {}
		}
	}
	return o
}

// saveLoad saves prog in mode with the start rule start and loads it again.
func saveLoad(t *testing.T, prog *Program, start string, mode CompiledMode) *Program {
	t.Helper()
	data, err := prog.Marshal(start, mode)
	if err != nil {
		t.Fatalf("save in %s mode: %v", mode, err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatalf("load in %s mode: %v", mode, err)
	}
	return loaded
}

// wantClosureRefused fails unless err is the error of the closure backend on a loaded program.
func wantClosureRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "closure backend needs the grammar source") {
		t.Errorf("closure backend on a loaded program: got %v, want the closure backend refused", err)
	}
}

func newChangeTestDocument(t *testing.T, prog *Program, text string, o ParseOptions, limit int) *ChangeDocument {
	t.Helper()
	d, err := prog.NewChangeDocumentWith("main", text, o, limit)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// A consumer rebuilds on any source invalidation, changed/unavailable outcome,
// or a version gap. Compare both indexes and highlights with fresh computation.
func changeConsumerViews(s *Snapshot) (map[string]int, []string) {
	index := map[string]int{}
	var highlights []string
	stack := []SnapshotNode{s.Root()}
	for len(stack) != 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !n.Valid() {
			continue
		}
		if n.IsTerminal() {
			index[n.Text()]++
			highlights = append(highlights, n.Text())
		}
		for i := n.ChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	return index, highlights
}

// incrementalGrammar is a grammar with left recursion, an Operators Rule expression, line starts (which
// examine the preceding character), captures, and position-dependent actions.
const incrementalGrammar = `type Pos = { At int; Name node[string] }
rule main = (line / blank)* $$
rule blank = "\n"
rule line = ^ (assign / list / pos) "\n"
rule assign = n:name " = " e:expr
rule list = list "," item / item
rule item = [a-z]+ #atomic
rule pos Pos = "@" n:name => Pos{At: $n.Start, Name: $n}
rule name = [a-z]+ #atomic
rule expr = #operators {
    operand [0-9]+ #atomic
    operand "(" x:expr ")" => $x
    level { infix left "+" / "-" }
    level { infix left "*" }
    level { prefix "-" }
}`

func dump(t *testing.T, n *value.Node, err error) string {
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

func resultJSON(n *value.Node, err error) string {
	out := map[string]any{"node": n}
	if err != nil {
		out["err"] = err.Error()
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// parseTest parses PEGO source for a test whose subject is not start rules: unless the source
// has declaration attributes, every rule becomes a start rule, the first one the default.
func parseTest(src string) (*grammar.Grammar, error) {
	g, err := syntax.Parse(src)
	return starttest.MarkAll(g), err
}

// mustCompile compiles PEGO source with source information, so that results show rule names and
// capture labels.
func mustCompile(t *testing.T, src string, opts ...compile.Options) *Program {
	t.Helper()
	var o compile.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	o.SourceInfo = true
	return compileBare(t, src, o)
}

func mustParse(src string) *grammar.Grammar {
	g, err := parseTest(src)
	if err != nil {
		panic(err)
	}
	return g
}

func negativeMaxima() []int {
	values := []int{-1, -2, -1 << 31}
	if strconv.IntSize == 64 {
		large := int64(-1) << 32
		values = append(values, int(large), -int(^uint(0)>>1)-1)
	}
	return values
}

func grammarWithNegativeMaximum(t testing.TB, src string, maximum int) *grammar.Grammar {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range g.Rules() {
		compile.WalkExpr(r.Expr, func(e grammar.Expr) {
			if rep, ok := e.(*grammar.Repeat); ok {
				rep.Max = maximum
			}
		})
	}
	return g
}

var snapshotBenchmarkResult any

var snapshotBenchmarkPosition int64

// snapshotPhaseContext changes state only while the freezer is on the stack.
// This separates a parse-time cancellation from a freeze-time cancellation.
type snapshotPhaseContext struct {
	context.Context
	done       chan struct{}
	seen       bool
	panicValue any
	phase      string
}

func assertTraceParserDetached(t *testing.T, p *core.Parser, aborted bool) {
	t.Helper()
	if p == nil {
		t.Fatal("trace callback did not expose a parser")
	}
	if p.Aborted != aborted {
		t.Fatalf("aborted=%t want %t", p.Aborted, aborted)
	}
	var inspect func(reflect.Value, string)
	inspect = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
			if !v.IsNil() {
				t.Errorf("finished parser retains %s (%s)", path, v.Kind())
			}
		case reflect.String:
			if v.Len() != 0 {
				t.Errorf("finished parser retains %s (string)", path)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				inspect(v.Field(i), path+"."+v.Type().Field(i).Name)
			}
		case reflect.Array:
			for i := range v.Len() {
				inspect(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	inspect(reflect.ValueOf(*p), "parser")
}

func traceLifecycleRecover(f func()) (got any) { defer func() { got = recover() }(); f(); return }

type budgetContextAccessorContext struct {
	context.Context
	done chan struct{}
	fn   func() error
}

type budgetContextPanicToken struct{ phase string }

type budgetContextDonePanic struct {
	context.Context
	token *budgetContextPanicToken
}

func newPollingContext(at int) *pollingContext {
	return &pollingContext{Context: context.Background(), done: make(chan struct{}), at: at}
}

func budgetDocumentVariants(t *testing.T, run func(*testing.T, ParseOptions)) {
	t.Helper()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", b, u), func(t *testing.T) { run(t, ParseOptions{Backend: b, Unit: u}) })
		}
	}
}

func snapshotVariants(t *testing.T, run func(*testing.T, core.ParseOptions)) {
	t.Helper()
	for _, backend := range []core.Backend{core.Closure, core.Bytecode, core.BytecodeIterative} {
		for _, unit := range []core.PositionUnit{core.CodePoints, core.Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) { run(t, core.ParseOptions{Backend: backend, Unit: unit}) })
		}
	}
}

// compileBare compiles PEGO source with the options as given: without source information unless
// they request it.
func compileBare(t *testing.T, src string, opts ...compile.Options) *Program {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	var o compile.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	prog, err := Compile(g, o)
	if err != nil {
		t.Fatalf("compile error: %v\n%s", err, src)
	}
	return prog
}

const records = `rule main = ^^ header items:record* #stream $$
rule header = "#records\n"
rule record = ^ k:[a-z]+ #atomic "=" v:[0-9]+ #atomic "\n"`

type pollingContext struct {
	context.Context
	done   chan struct{}
	checks int
	at     int
}

// corruptVMLiteral points the first literal instruction of the module of prog past the string
// table, bypassing the saved-module validator, so the VM faults when it runs that instruction. It
// returns a function that restores the instruction.
func corruptVMLiteral(t *testing.T, prog *Program) (restore func()) {
	t.Helper()
	m := prog.Module()
	for i, in := range m.Code {
		if in.Op == bytecode.OpStr {
			m.Code[i].A = int32(len(m.Strings))
			return func() { m.Code[i] = in }
		}
	}
	t.Fatal("module has no literal instruction")
	return nil
}

func newBudgetContextErrPanic(token *budgetContextPanicToken, panicAt int) *budgetContextErrPanic {
	return &budgetContextErrPanic{Context: context.Background(), done: make(chan struct{}), token: token, panicAt: panicAt}
}

func budgetContextRecover(run func()) (got any) {
	defer func() { got = recover() }()
	run()
	return nil
}

type budgetDocumentReuseContext struct {
	context.Context
	observe func()
}

type budgetContextErrPanic struct {
	context.Context
	done    chan struct{}
	armed   bool
	panicAt int
	calls   int
	token   *budgetContextPanicToken
	control *core.BudgetControl
	atPanic uint64
	since   uint64
}

func (c *budgetContextErrPanic) Done() <-chan struct{} { return c.done }

func (c *budgetContextErrPanic) Err() error {
	if !c.armed {
		return nil
	}
	c.calls++
	if c.calls == c.panicAt {
		if c.control != nil {
			c.atPanic, c.since = c.control.Used, c.control.SinceCheck
		}
		panic(c.token)
	}
	return nil
}

func (c *pollingContext) Done() <-chan struct{} { return c.done }

func (c *pollingContext) Err() error {
	c.checks++
	if c.at > 0 && c.checks >= c.at {
		return context.Canceled
	}
	return nil
}

func (c *snapshotPhaseContext) Done() <-chan struct{} { return c.done }

func (c *snapshotPhaseContext) Err() error {
	var callers [24]uintptr
	count := runtime.Callers(2, callers[:])
	frames := runtime.CallersFrames(callers[:count])
	for {
		f, more := frames.Next()
		phase := c.phase
		if phase == "" {
			phase = "freezeSnapshotGraph"
		}
		if strings.Contains(f.Function, phase) {
			c.seen = true
			if c.panicValue != nil {
				panic(c.panicValue)
			}
			return context.Canceled
		}
		if !more {
			break
		}
	}
	return nil
}

func (c *budgetContextAccessorContext) Done() <-chan struct{} { return c.done }

func (c *budgetContextAccessorContext) Err() error {
	if c.fn != nil {
		return c.fn()
	}
	return nil
}

func (c *budgetContextDonePanic) Done() <-chan struct{} { panic(c.token) }

func (c *budgetDocumentReuseContext) Err() error { c.observe(); return c.Context.Err() }
