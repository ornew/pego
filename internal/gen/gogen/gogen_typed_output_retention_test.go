package gogen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

var typedAllocatorControls = []struct {
	name                   string
	objects, lists, legacy bool
}{
	{"slabs", false, false, false},
	{"legacy", false, false, true},
	{"objects", true, false, false},
	{"slices", false, true, false},
	{"individual", true, true, false},
}

// PEGO_TYPED_OUTPUT_RETENTION_DIR preserves the generated fixture for the
// retained-heap and parse-throughput benchmarks below. Normal tests clean it up.
func TestGeneratedTypedOutputRetentionFixture(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedOutputRetentionFixture)
}

func testGeneratedTypedOutputRetentionFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := parseTest(`type Token = { Text string }
type Pair = { Text string; Left Token; Right Token }
type Values = { Items []Token }
type LargeValues = { Items []Pair }
type SmallValues = { Items []Pair }
rule main = leaf
rule leaf Token = x:discarded x:small => $x
rule match node[string] = discardedMatch y:"b" #atomic => $y
rule discardedMatch node[string] = x:"é"+ #atomic => $x
rule structure Pair = discardedPair y:small => Pair{Text: "b", Left: $y, Right: $y}
rule list Values = discarded ys:small+ => Values{Items: $ys.Value}
rule slice SmallValues = discardedHolder filler filler filler filler filler filler filler xs:smallPair+ => SmallValues{Items: $xs.Value}
rule discardedHolder LargeValues = xs:largePair #repeat(1) => LargeValues{Items: $xs.Value}
rule largePair Pair = x:"é"+ #atomic => Pair{Text: text($x)}
rule filler Pair = "f" => Pair{Text: "f"}
rule smallPair Pair = "b" => Pair{Text: "b"}
rule discardedPair Pair = x:"é"+ #atomic => Pair{Text: text($x)}
rule discarded Token = t:(large "!" / large) #atomic => Token{Text: text($t)}
rule large Token = t:("é"+) #atomic => Token{Text: text($t)}
rule small Token = t:("b") #atomic => Token{Text: text($t)}
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, conversion := range []bool{false, true} {
		for _, variant := range typedAllocatorControls {
			name := variant.name
			if conversion {
				name += "-conversion"
			}
			t.Run(name, func(t *testing.T) {
				code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "typedoutput", Start: "main", Types: true,
					ConvertTypes: conversion, DisableTypedObjectSlabs: variant.objects, DisableTypedListSlabs: variant.lists, DisableTypedOutputBootstrap: variant.legacy})
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(code), "disableTypedObjectSlabs") || strings.Contains(string(code), "disableTypedListSlabs") || strings.Contains(string(code), "disableTypedOutputBootstrap") {
					t.Fatal("generation-only allocator controls leaked into the parser")
				}
				if variant.legacy && strings.Contains(string(code), "s.bootstrap") {
					t.Fatal("reference output used untracked bootstrap allocations")
				}
				dir := os.Getenv("PEGO_TYPED_OUTPUT_RETENTION_DIR")
				if dir == "" {
					dir = t.TempDir()
				} else {
					dir = filepath.Join(dir, name)
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				for name, contents := range map[string][]byte{
					"parser.go":                code,
					"output_retention_test.go": []byte(typedOutputRetentionFixture + "\nconst forceOutputConversion = " + strconv.FormatBool(conversion) + "\n"),
					"go.mod":                   []byte(gentest.GoMod("typedoutput")),
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
			})
		}
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

func unitStart(unit PositionUnit) int {
	if unit == Bytes {
		return 2 * largeRunes
	}
	return largeRunes
}

func sampleInput(kind string, runes int) string {
	suffix := "b"
	if kind == "list" {
		suffix = "bb"
	} else if kind == "slice" {
		suffix = "fffffffbb"
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
func parseAndClone(kind string, unit PositionUnit, runes int) any {
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
func warmTypedOutput(kind string, unit PositionUnit, control bool) {
	_ = parseAndClone(kind, unit, largeRunes)
	if control {
		_ = controlResult(kind, unitStart(unit))
	}
}

func cloneReachableText(value any) {
	switch v := value.(type) {
	case *Token:
		v.Text = strings.Clone(v.Text)
	case *TNode[string]:
		v.Value = strings.Clone(v.Value)
	case *Pair:
		v.Text = strings.Clone(v.Text)
		v.Left.Text = strings.Clone(v.Left.Text)
		if v.Right != v.Left {
			v.Right.Text = strings.Clone(v.Right.Text)
		}
	case *Values:
		for _, item := range v.Items {
			item.Text = strings.Clone(item.Text)
		}
	case *SmallValues:
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
	case "match":
		return &TNode[string]{Span: Span{Start: start, End: start + 1}, Value: "b"}
	case "structure":
		x := token(0)
		return &Pair{Span: Span{Start: 0, End: start + 1}, Text: "b", Left: x, Right: x}
	case "list":
		return &Values{Span: Span{Start: 0, End: start + 2}, Items: []*Token{token(0), token(1)}}
	case "slice":
		return &SmallValues{Span: Span{Start: 0, End: start + 9}, Items: []*Pair{
			{Span: Span{Start: start + 7, End: start + 8}, Text: "b"},
			{Span: Span{Start: start + 8, End: start + 9}, Text: "b"},
		}}
	default:
		panic("unknown result kind: " + kind)
	}
}

func TestTypedOutputOwnership(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, kind := range []string{"terminal", "match", "structure", "list", "slice"} {
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
				case *TNode[string]:
					if v.Value != "b" || v.Start != unitStart(unit) || v.End != v.Start+1 {
						t.Fatalf("match: %+v", v)
					}
				case *Pair:
					// Conversion preserves field values but may materialize a
					// shared CST node independently for each typed field.
					if v.Left == nil || v.Right == nil || (!forceOutputConversion && v.Left != v.Right) {
						t.Fatal("struct lost shared terminal identity")
					}
					if v.Start != 0 || v.End != unitStart(unit)+1 || v.Left.Start != unitStart(unit) || v.Left.End != unitStart(unit)+1 || v.Text != "b" {
						t.Fatalf("pair spans/text: %+v", v)
					}
				case *Values:
					if len(v.Items) != 2 || v.Items[0] == nil || v.Items[1] == nil || v.Items[0] == v.Items[1] {
						t.Fatal("list values or element identities changed")
					}
					if cap(v.Items) != len(v.Items) || v.Start != 0 || v.End != unitStart(unit)+2 {
						t.Fatal("list slice extent or rule span changed")
					}
					_ = append(v.Items, &Token{Text: "c"})
					if len(v.Items) != 2 || v.Items[0].Text != "b" || v.Items[1].Text != "b" {
						t.Fatal("append changed saved list result")
					}
				case *SmallValues:
					if len(v.Items) != 2 || cap(v.Items) != len(v.Items) || v.Start != 0 || v.End != unitStart(unit)+9 {
						t.Fatalf("slice result: %#v len=%d cap=%d", v.Items, len(v.Items), cap(v.Items))
					}
					for i, item := range v.Items {
						if item == nil || item.Text != "b" || item.Start != unitStart(unit)+7+i || item.End != item.Start+1 {
							t.Fatalf("slice item %d: %+v", i, item)
						}
					}
					if v.Items[0] == v.Items[1] {
						t.Fatal("slice elements share a Pair pointer")
					}
					_ = append(v.Items, &Pair{Text: "c"})
					if len(v.Items) != 2 || v.Items[0].Text != "b" || v.Items[1].Text != "b" {
						t.Fatal("append changed saved slice result")
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
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, runes := range []int{8, largeRunes} {
			for _, kind := range []string{"terminal", "match", "structure", "list", "slice"} {
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
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, kind := range []string{"terminal", "match", "structure", "list", "slice"} {
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
