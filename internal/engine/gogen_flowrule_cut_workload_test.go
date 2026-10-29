package engine

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// PEGO_TYPED_CUT_DIR preserves both latest-generator variants so their test
// binaries can be built once and measured in alternating benchmark runs.
func TestGeneratedTypedCutWorkload(t *testing.T) { eachMode(t, testGeneratedTypedCutWorkload) }

func testGeneratedTypedCutWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := parseTest(`type Row = { Key node[string]; Value node[string] }
type Doc = { Rows []Row }
rule main Doc = rows:row+ $$ => Doc{Rows: $rows.Value}
rule row Row = "let" " " #cut key:[a-z]+ #atomic ":" value:[0-9]+ #atomic ";" / "var" " " #cut key:[a-z]+ #atomic ":" value:[0-9]+ #atomic ";" => Row{Key: $key, Value: $value}
`)
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("PEGO_TYPED_CUT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	var want []byte
	for _, variant := range []string{"direct"} {
		code, err := generateCode(g, gen.GenOptions{Package: "cutfixture", Start: "main", Types: true})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, variant)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, contents := range map[string][]byte{
			"parser.go": code, "cut_test.go": []byte(typedCutWorkloadFixture), "go.mod": []byte(testGoMod("cutfixture")),
		} {
			if err := os.WriteFile(filepath.Join(path, name), contents, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// Emit deterministic values and errors separately from go test's timing
		// line, which differs between otherwise equivalent binaries.
		cmd := exec.Command("go", "test", "-count=1", "-run", "^TestCutWorkload$", "-v")
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
			t.Fatal("missing workload observations")
		}
		if want == nil {
			want = observations
		} else if !bytes.Equal(want, observations) {
			t.Fatalf("latest-generator variants differ:\n%s\n%s", want, observations)
		}
	}
}

const typedCutWorkloadFixture = `package cutfixture

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func cutInput(n int) string { return strings.Repeat("let alpha:123;var beta:456;", n) }

func TestCutWorkload(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		saved, err := ParseAST(cutInput(1), WithUnit(unit))
		if err != nil || len(saved.Rows) != 2 || saved.Rows[0].Key.Value != "alpha" || saved.Rows[1].Value.Value != "456" {
			t.Fatalf("typed rows: %v %v", saved, err)
		}
		before, _ := json.Marshal(saved)
		for _, input := range []string{cutInput(1), cutInput(32), "let alpha:;", "let alpha:123", "var beta:x;", "let é:1;", "", "let alpha:123;?"} {
			v, err := ParseAST(input, WithUnit(unit))
			data, _ := json.Marshal(struct { Value any; Error string }{v, fmt.Sprint(err)})
			fmt.Printf("OBS %v %q %s\n", unit, input, data)
			if (input == cutInput(1) || input == cutInput(32)) != (err == nil) {
				t.Fatalf("acceptance %q: %v", input, err)
			}
		}
		after, _ := json.Marshal(saved)
		if string(before) != string(after) {
			t.Fatal("parser reuse changed returned AST")
		}
	}
}

var cutResult *Doc

func BenchmarkTypedCuts(b *testing.B) {
	for _, n := range []int{1, 256} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			b.Run(fmt.Sprintf("%d/%v", n, unit), func(b *testing.B) {
				input := cutInput(n)
				b.ReportAllocs()
				b.SetBytes(int64(len(input)))
				for b.Loop() {
					v, err := ParseAST(input, WithUnit(unit))
					if err != nil || len(v.Rows) != 2*n {
						b.Fatalf("parse: %v", err)
					}
					cutResult = v
				}
			})
		}
	}
}
`
