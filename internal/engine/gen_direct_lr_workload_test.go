package engine

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// PEGO_TYPED_LR_DIR preserves both same-generator routes for prebuilt paired
// benchmarks. Normal tests remove the fixture modules automatically.
func TestGeneratedTypedLRWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	src, err := os.ReadFile("../../examples/calculator/calc_lr.pego")
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("PEGO_TYPED_LR_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	testTypedLRWorkload(t, string(src), typedLRWorkloadFixture, dir)
}

func TestGeneratedTypedLRActionWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := os.Getenv("PEGO_TYPED_LR_DIR")
	if dir == "" {
		dir = t.TempDir()
	} else {
		dir = filepath.Join(dir, "actions")
	}
	testTypedLRWorkload(t, `type Doc = { Items []node[string]; N int }
rule main Doc = e:expr $$ => $e
rule expr Doc = previous:expr ";" items:(x:[a-z] #atomic -",") #repeat(3) / items:(x:[a-z] #atomic -",") #repeat(3) => Doc{Items: map($items, (e) => e.$x), N: len($items)}
`, typedLRActionWorkloadFixture, dir)
}

func testTypedLRWorkload(t *testing.T, src, fixture, dir string) {
	t.Helper()
	testGeneratedTypedBodyWorkload(t, src, fixture, dir, "(typed LR body inlined)", GenOptions{disableTypedLRBodies: true})
}

func testGeneratedTypedBodyWorkload(t *testing.T, src, fixture, dir, marker string, reference GenOptions, unaffectedMarkers ...int) {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, variant := range []string{"direct", "general"} {
		opts := GenOptions{}
		if variant == "general" {
			opts = reference
		}
		opts.Package, opts.Start, opts.Types, opts.Recognize = "lrfixture", "main", true, true
		code, err := generateCode(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		if len(unaffectedMarkers) != 0 {
			count = unaffectedMarkers[0]
		}
		got := strings.Count(string(code), marker)
		if (variant == "direct" && got <= count) || (variant == "general" && got != count) {
			t.Fatalf("%s: incorrect generated body route", variant)
		}
		path := filepath.Join(dir, variant)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, contents := range map[string][]byte{
			"parser.go": code, "lr_test.go": []byte(fixture), "go.mod": []byte(testGoMod("lrfixture")),
		} {
			if err := os.WriteFile(filepath.Join(path, name), contents, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("go", "test", "-count=1", "-run", "^Test.*Workload$", "-v")
		cmd.Dir = path
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", variant, err, out)
		}
		var observations []byte
		for _, line := range bytes.Split(out, []byte{'\n'}) {
			if bytes.HasPrefix(line, []byte("OBS ")) {
				observations = append(observations, line...)
				observations = append(observations, '\n')
			}
		}
		if len(observations) == 0 {
			t.Fatal("missing LR observations")
		}
		if want == nil {
			want = observations
		} else if !bytes.Equal(want, observations) {
			t.Fatalf("same-generator routes differ:\n%s\n%s", want, observations)
		}
	}
}

const typedLRActionWorkloadFixture = `package lrfixture

import (
	pegort "github.com/ornew/pego/runtime"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func lrInput(n int) string { return "a,b,c," + strings.Repeat(";d,e,f,", n) }

func TestLRWorkload(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		saved, err := ParseAST(lrInput(1), WithUnit(unit))
		if err != nil || saved.N != 3 || len(saved.Items) != 3 || saved.Items[0].Value != "d" {
			t.Fatalf("typed capture result: %v %v", saved, err)
		}
		before, _ := json.Marshal(saved)
		p := &pegort.TParser{Parser: &pegort.Parser{}}
		for _, input := range []string{lrInput(1), lrInput(128), "a,b,c,", "a,b,", "a,b,c,;", "a,b,c,d,", "a,b,c,;d,é,f,", ""} {
			v, err := p.X_run(trules[0], input, pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{})
			data, _ := json.Marshal(struct { Value any; Error string }{v, fmt.Sprint(err)})
			pooled, pooledErr := ParseAST(input, WithUnit(unit))
			pooledData, _ := json.Marshal(struct { Value any; Error string }{pooled, fmt.Sprint(pooledErr)})
			if string(data) != string(pooledData) { t.Fatal("pooled and reused LR parsers differ") }
			fmt.Printf("OBS %v %q %s\n", unit, input, data)
			if (err == nil) != (input == lrInput(1) || input == lrInput(128) || input == "a,b,c,") {
				t.Fatalf("acceptance %q: %v", input, err)
			}
			if p.Depth != 0 || len(p.Trail) != 0 || p.Frame != nil {
				t.Fatal("LR invocation left live frame, depth or undo state")
			}
			for _, u := range p.Trail[:cap(p.Trail)] {
				if u.X_f != nil || u.X_old != nil || u.X_slot != 0 { t.Fatal("discarded undo retained") }
			}
			p.X_recycle()
		}
		after, _ := json.Marshal(saved)
		if string(before) != string(after) { t.Fatal("returned LR AST changed") }
	}
}

var lrResult *Doc

func BenchmarkTypedLR(b *testing.B) {
	for _, n := range []int{1, 128} {
		input := lrInput(n)
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			b.Run(fmt.Sprintf("%d/%v", n, unit), func(b *testing.B) {
				if _, err := ParseAST(input, WithUnit(unit)); err != nil { b.Fatal(err) }
				b.ReportAllocs()
				for b.Loop() {
					v, err := ParseAST(input, WithUnit(unit))
					if err != nil { b.Fatal(err) }
					lrResult = v
				}
			})
		}
	}
}
`

const typedLRWorkloadFixture = `package lrfixture

import (
	pegort "github.com/ornew/pego/runtime"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func lrInput(n int) string { return "1" + strings.Repeat("+2*3", n) }

func TestLRWorkload(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		saved, err := ParseAST("1+2*3", WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(saved)
		p := &pegort.TParser{Parser: &pegort.Parser{}}
		for _, input := range []string{"1", "1+2*3", lrInput(32), "(1+2)*3", "2^-1", "1+", "1+2*", "(1+2", "é+1", ""} {
			v, err := p.X_run(trules[0], input, pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{})
			data, _ := json.Marshal(struct { Value any; Error string }{v, fmt.Sprint(err)})
			fmt.Printf("OBS %v %q %s\n", unit, input, data)
			if _, err := ParseAST(input, WithUnit(unit)); (err == nil) != (input == "1" || input == "1+2*3" || input == lrInput(32) || input == "(1+2)*3" || input == "2^-1") {
				t.Fatalf("acceptance %q: %v", input, err)
			}
			if p.Depth != 0 || len(p.Trail) != 0 || p.Frame != nil {
				t.Fatal("LR invocation left live frame, depth or undo state")
			}
			for _, u := range p.Trail[:cap(p.Trail)] {
				if u.X_f != nil || u.X_old != nil || u.X_slot != 0 {
					t.Fatal("LR undo tail retains discarded state")
				}
			}
			p.X_recycle()
		}
		after, _ := json.Marshal(saved)
		if string(before) != string(after) {
			t.Fatal("parser reuse changed returned LR AST")
		}
	}
}

var lrResult Expr

func BenchmarkTypedLR(b *testing.B) {
	for _, n := range []int{1, 128} {
		input := lrInput(n)
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			b.Run(fmt.Sprintf("%d/%v", n, unit), func(b *testing.B) {
				if _, err := ParseAST(input, WithUnit(unit)); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					v, err := ParseAST(input, WithUnit(unit))
					if err != nil { b.Fatal(err) }
					lrResult = v
				}
			})
		}
	}
}

// Unchanged public routes are controls for the typed-only selection.
func BenchmarkLRPublic(b *testing.B) {
	input := lrInput(128)
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		b.Run(fmt.Sprintf("Node/%v", unit), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() { if _, err := Parse(input, WithUnit(unit)); err != nil { b.Fatal(err) } }
		})
		b.Run(fmt.Sprintf("Recognize/%v", unit), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() { if err := Recognize(input, WithUnit(unit)); err != nil { b.Fatal(err) } }
		})
	}
}
`
