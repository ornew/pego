package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_TYPED_OUTPUT_RETENTION_DIR preserves the generated fixture for the
// retained-heap and parse-throughput benchmarks below. Normal tests clean it up.
func TestGeneratedTypedOutputRetentionFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := syntax.Parse(`
type Token terminal
type Pair struct { Left Token, Right Token }
type Values struct { Items []Token }
def main = leaf
def leaf: Token = x:discarded x:small -> $x
def structure: Pair = discarded y:small -> new Pair{Left: $y, Right: $y}
def list: Values = discarded ys:small+ -> new Values{Items: $ys}
def discarded: Token = large "!" / large
def large: Token = "é"+
def small: Token = "b"
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "typedoutput", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("PEGO_TYPED_OUTPUT_RETENTION_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{
		"parser.go": code,
		"output_retention_test.go": []byte(typedOutputRetentionFixture),
		"go.mod": []byte("module typedoutput\n\ngo 1.27.1\n"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestTypedOutputOwnership$")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated typed output fixture: %v\n%s", err, out)
	}
}

const typedOutputRetentionFixture = `package typedoutput

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const largeRunes = 1 << 20 // two MiB of UTF-8 input

func unitStart(unit Unit) int {
	if unit == Bytes {
		return 2 * largeRunes
	}
	return largeRunes
}

func sampleInput(kind string, runes int) string {
	suffix := "b"
	if kind == "list" {
		suffix = "bb"
	}
	return strings.Repeat("é", runes) + suffix
}

func ruleForKind(kind string) string {
	if kind == "terminal" {
		return "leaf"
	}
	return kind
}

// parseAndClone returns only the caller-owned result. Cloning every Text field
// reachable from that result excludes ordinary input-substring retention; any
// large input still live afterwards must be reachable through an owned slab.
func parseAndClone(kind string, unit Unit, runes int) any {
	input := sampleInput(kind, runes)
	value, err := ParseRuleAST(ruleForKind(kind), input, WithUnit(unit))
	if err != nil {
		panic(fmt.Errorf("parse %s: %w", kind, err))
	}
	cloneReachableText(value)
	runtime.KeepAlive(input)
	return value
}

// warmTypedOutput uses an identical parser/result path before heap snapshots.
// Its frame returns before the baseline GC, leaving no result or input alive.
func warmTypedOutput(kind string, unit Unit, control bool) {
	_ = parseAndClone(kind, unit, largeRunes)
	if control {
		_ = controlResult(kind, unitStart(unit))
	}
}

func cloneReachableText(value any) {
	switch v := value.(type) {
	case *Token:
		v.Text = strings.Clone(v.Text)
	case *Pair:
		v.Left.Text = strings.Clone(v.Left.Text)
		if v.Right != v.Left {
			v.Right.Text = strings.Clone(v.Right.Text)
		}
	case *Values:
		for _, item := range v.Items {
			item.Text = strings.Clone(item.Text)
		}
	default:
		panic(fmt.Sprintf("unexpected result type %T", value))
	}
}

// controlResult creates the equivalent result graph independently of parser
// output slabs. Pair deliberately shares its Token pointer, like the grammar.
func controlResult(kind string, start int) any {
	token := func(offset int) *Token {
		return &Token{Span: Span{Start: start + offset, End: start + offset + 1}, Text: "b"}
	}
	switch kind {
	case "terminal":
		return token(0)
	case "structure":
		x := token(0)
		return &Pair{Span: Span{Start: 0, End: start + 1}, Left: x, Right: x}
	case "list":
		return &Values{Span: Span{Start: 0, End: start + 2}, Items: []*Token{token(0), token(1)}}
	default:
		panic("unknown result kind: " + kind)
	}
}

func TestTypedOutputOwnership(t *testing.T) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		for _, kind := range []string{"terminal", "structure", "list"} {
			t.Run(fmt.Sprintf("%s/%v", kind, unit), func(t *testing.T) {
				value := parseAndClone(kind, unit, largeRunes)
				want := controlResult(kind, unitStart(unit))
				if !reflect.DeepEqual(value, want) {
					t.Fatalf("generated result differs from individually allocated control:\n got %#v\nwant %#v", value, want)
				}
				switch v := value.(type) {
				case *Token:
					if v.Text != "b" || v.Start != unitStart(unit) || v.End != v.Start+1 {
						t.Fatalf("terminal: %+v", v)
					}
				case *Pair:
					if v.Left == nil || v.Left != v.Right {
						t.Fatal("struct lost shared terminal identity")
					}
				case *Values:
					if len(v.Items) != 2 || v.Items[0] == nil || v.Items[1] == nil || v.Items[0] == v.Items[1] {
						t.Fatal("list values or element identities changed")
					}
				}
				if _, err := ParseRuleAST(ruleForKind(kind), "?", WithUnit(unit)); err == nil {
					t.Fatal("invalid follow-up input accepted")
				}
				if _, err := ParseRuleAST(ruleForKind(kind), sampleInput(kind, 8), WithUnit(unit)); err != nil {
					t.Fatalf("small follow-up parse: %v", err)
				}
				if !reflect.DeepEqual(value, want) {
					t.Fatal("later parses changed the escaped result")
				}
			})
		}
	}
}

// BenchmarkTypedOutputParse is a normal hot-path measurement with a small
// capture control and the large-retention input. Keep the returned value live
// through each iteration so it has the same escape contract as ParseAST users.
func BenchmarkTypedOutputParse(b *testing.B) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		for _, runes := range []int{8, largeRunes} {
			for _, kind := range []string{"terminal", "structure", "list"} {
				name := fmt.Sprintf("%s/%v/%d-runes", kind, unit, runes)
				b.Run(name, func(b *testing.B) {
					input := sampleInput(kind, runes)
					rule := ruleForKind(kind)
					b.ReportAllocs()
					var value any
					for b.Loop() {
						var err error
						value, err = ParseRuleAST(rule, input, WithUnit(unit))
						if err != nil {
							b.Fatal(err)
						}
					}
					runtime.KeepAlive(input)
					runtime.KeepAlive(value)
				})
			}
		}
	}
}

// BenchmarkTypedOutputRetainedHeap reports a post-loop, post-GC live-heap
// delta for the last escaped result. The timed loop is ordinary parsing; the
// final collection and heap snapshot happen after the timer stops.
func BenchmarkTypedOutputRetainedHeap(b *testing.B) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		for _, kind := range []string{"terminal", "structure", "list"} {
			for _, control := range []bool{false, true} {
				name := fmt.Sprintf("%s/%v/control=%t", kind, unit, control)
				b.Run(name, func(b *testing.B) {
					warmTypedOutput(kind, unit, control)
					runtime.GC()
					runtime.GC()
					runtime.GC()
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					var value any
					for b.Loop() {
						if control {
							value = controlResult(kind, unitStart(unit))
						} else {
							value = parseAndClone(kind, unit, largeRunes)
						}
					}
					b.StopTimer()
					runtime.GC()
					runtime.GC()
					runtime.GC()
					runtime.ReadMemStats(&after)
					b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "retained-B")
					runtime.KeepAlive(value)
				})
			}
		}
	}
}
`
