package gogen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// ZP_SPARSE_MEMO_GENERATED_DIR preserves the generated fixture for
// separately built timing and allocation measurements.
func TestGeneratedSparseMemo(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedSparseMemo)
}

func testGeneratedSparseMemo(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	src, err := os.ReadFile("../../../parsers/typescript/typescript.zp")
	if err != nil {
		t.Fatal(err)
	}
	g, err := parseTest(string(src))
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("ZP_SPARSE_MEMO_GENERATED_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	var fingerprint string
	for _, name := range []string{"optimized"} {
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "memofixture", Start: "main", Types: true,
			Recognize: true})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(code), "TParse(trules") {
			t.Fatal("fixture does not exercise the typed runtime")
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		fixture := sparseMemoGeneratedFixture
		for file, data := range map[string][]byte{
			"go.mod":    []byte(gentest.GoMod("memofixture")),
			"parser.go": code, "memo_test.go": []byte(fixture),
		} {
			if err := os.WriteFile(filepath.Join(path, file), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "^TestSparseGenerated")
		cmd.Dir = path
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s generated control: %v\n%s", name, err, output)
		}
		for _, line := range strings.Split(string(output), "\n") {
			if at := strings.Index(line, "MEMO_RESULT "); at >= 0 {
				fingerprint += line[at:] + "\n"
			}
		}
	}
	if fingerprint == "" {
		t.Fatal("generated fixture reported no results")
	}
}

const sparseMemoGeneratedFixture = `package memofixture

import (
	zprt "github.com/ornew/zeroparse/runtime"
    "crypto/sha256"
    "encoding/json"
    "fmt"
    "os"
    "strings"
    "testing"
)

func TestSparseGeneratedResults(t *testing.T) {
    if zpGrammar.X_nseen <= 64 || zpRecGrammar.X_nseen <= 64 { t.Fatal("fixture no longer exercises sparse tables") }
    inputs := []string{
        "const 日本語 = \"😀\"; const v = [1, 2].map(x => x + 1);",
        strings.Repeat("const 日本語 = 1 + 2;\n", 1200),
        "function f( { return 1;", "",
    }
    for _, unit := range []PositionUnit{CodePoints, Bytes} {
        for i, input := range inputs {
            for pass := 0; pass < 3; pass++ {
                node, err := Parse(input, WithUnit(unit))
                ast, aerr := ParseAST(input, WithUnit(unit))
                rerr := Recognize(input, WithUnit(unit))
                data, jerr := json.Marshal([]any{node, fmt.Sprint(err), ast, fmt.Sprint(aerr), fmt.Sprint(rerr)})
                if jerr != nil { t.Fatal(jerr) }
                t.Logf("MEMO_RESULT %d/%d/%d %x", unit, i, pass, sha256.Sum256(data))
            }
        }
    }
}

func TestSparseGeneratedInputSelection(t *testing.T) {
    p := &zprt.TParser{Parser: &zprt.Parser{}, Ext: &tslabs{}}
    for _, input := range []string{
        "/*" + strings.Repeat("a", 1020) + "*/",
        "/*" + strings.Repeat("a", 1021) + "*/",
        "/*" + strings.Repeat("é", 512) + "*/",
        "/*" + strings.Repeat("a", 1020) + "*/",
    } {
        for _, unit := range []PositionUnit{CodePoints, Bytes} {
            _, err := p.X_run(trules[0], input, zprt.X_parseOptions{X_expectations: true, X_unit: unit, X_maxDepth: zpGrammar.X_maxDepth}, &tslabs{})
            if err != nil { t.Fatal(err) }
            want := len([]rune(input)) > 1024
            if unit == Bytes { want = len(input) > 1024 }
            if p.X_memo.X_sparse != want { t.Fatal("typed input cutoff mismatch") }
            p.X_recycle()
        }
    }
}

// An optional real corpus exercises retained scratch across successful and
// failed large parses, with small parses between them, in the same parser.
func TestSparseGeneratedRealCorpusReuse(t *testing.T) {
    path := os.Getenv("ZP_SPARSE_MEMO_INPUT")
    if path == "" { t.Skip("ZP_SPARSE_MEMO_INPUT not set") }
    data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
    for _, unit := range []PositionUnit{CodePoints, Bytes} {
        p := &zprt.TParser{Parser: &zprt.Parser{}}
        for i, input := range []string{string(data), "const x=1;", string(data)+"\nfunction (", "const x=1;", string(data)} {
            _, err := p.X_run(trules[0], input, zprt.X_parseOptions{X_expectations: true, X_unit: unit, X_maxDepth: zpGrammar.X_maxDepth}, &tslabs{})
            if (err != nil) != (i == 2) { t.Fatalf("unit=%d step=%d err=%v", unit, i, err) }
            p.X_recycle()
            if p.X_memo.X_bitPages.X_n != 0 { t.Fatal("used pages survived recycle") }
            for _, dir := range p.X_memo.X_pages {
                if dir.X_last != nil || dir.X_at != 0 { t.Fatal("cached pointer survived recycle") }
                for _, page := range dir.X_pages { if page != nil { t.Fatal("directory pointer survived recycle") } }
            }
            for _, chunk := range p.X_memo.X_bitPages.X_chunks {
                for _, page := range chunk {
                    for _, word := range page { if word != 0 { t.Fatal("seen bits survived recycle") } }
                }
            }
        }
    }
}

func BenchmarkSparseMemoGenerated(b *testing.B) {
    inputs := []struct{name, text string}{{"synthetic", strings.Repeat("const 日本語 = 1 + 2;\n", 12000)}, {"small", "const 日本語 = 1 + 2;\n"}}
    if path := os.Getenv("ZP_SPARSE_MEMO_INPUT"); path != "" {
        data, err := os.ReadFile(path); if err != nil { b.Fatal(err) }
        inputs = append(inputs, struct{name, text string}{"compiler", string(data)})
    }
    for _, input := range inputs {
        for _, unit := range []PositionUnit{CodePoints, Bytes} {
            for _, api := range []string{"Node", "AST", "Recognize"} {
                b.Run(fmt.Sprintf("%s/%s/%d", input.name, api, unit), func(b *testing.B) {
                    run := func() error {
                        switch api {
                        case "Node": _, err := Parse(input.text, WithUnit(unit)); return err
                        case "AST": _, err := ParseAST(input.text, WithUnit(unit)); return err
                        default: return Recognize(input.text, WithUnit(unit))
                        }
                    }
                    if err := run(); err != nil { b.Fatal(err) }
                    b.SetBytes(int64(len(input.text)))
                    b.ReportAllocs()
                    for b.Loop() { if err := run(); err != nil { b.Fatal(err) } }
                })
            }
        }
    }
}
`
