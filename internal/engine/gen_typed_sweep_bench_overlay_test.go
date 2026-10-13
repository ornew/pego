package engine

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func typedSweepOverlayReplace(src, old, new string) (string, error) {
	if strings.Count(src, old) != 1 {
		return src, fmt.Errorf("benchmark overlay anchor count")
	}
	return strings.Replace(src, old, new, 1), nil
}

func ordinaryASTOverlay(src []byte) ([]byte, error) {
	s := string(src)
	var err error
	s, err = typedSweepOverlayReplace(s,
		"// ast parses into typed values with the generated parser's ParseAST (nil if not generated).\n\tast func(input string) error",
		"// ast parses into typed values with the generated parser's ParseAST (nil if not generated).\n\t// The bool selects byte positions while preserving each workload's input and\n\t// success/error contract.\n\tast func(input string, bytes bool) error")
	if err != nil {
		return nil, err
	}
	astRE := regexp.MustCompile(`ast:\s+func\(s string\) error \{ return genErr\((gjson|gcsv|gxml|gcalc|gcalclr|gminilang|goutline)\.ParseAST\(s\)\) \},`)
	matches := astRE.FindAllStringSubmatch(s, -1)
	if len(matches) != 7 {
		return nil, fmt.Errorf("ordinary AST closure count = %d, want 7", len(matches))
	}
	s = astRE.ReplaceAllStringFunc(s, func(m string) string {
		pkg := astRE.FindStringSubmatch(m)[1]
		return "ast: func(s string, b bool) error {\n" +
			" if b { return genErr(" + pkg + ".ParseAST(s, " + pkg + ".WithUnit(" + pkg + ".Bytes))) }\n" +
			" return genErr(" + pkg + ".ParseAST(s))\n},"
	})
	s, err = typedSweepOverlayReplace(s, "ast: func(s string) error {", "ast: func(s string, b bool) error {")
	if err != nil {
		return nil, err
	}
	s, err = typedSweepOverlayReplace(s, "if v, _ := gminilang.ParseAST(s); v == nil {", "u := gminilang.CodePoints\n\t\t\tif b { u = gminilang.Bytes }\n\t\t\tif v, _ := gminilang.ParseAST(s, gminilang.WithUnit(u)); v == nil {")
	if err != nil {
		return nil, err
	}
	s, err = typedSweepOverlayReplace(s,
		"if err := w.ast(input); err != nil {\n\t\t\t\t\tt.Errorf(\"generated ParseAST: %v\", err)\n\t\t\t\t}",
		"for _, u := range units {\n\t\t\t\t\tif err := w.ast(input, u.u == pego.Bytes); err != nil {\n\t\t\t\t\t\tt.Errorf(\"generated ParseAST/%s: %v\", u.name, err)\n\t\t\t\t\t}\n\t\t\t\t}")
	if err != nil {
		return nil, err
	}
	oldBench := "if w.ast != nil {\n\t\t\t\tb.Run(\"generated_ast\", func(b *testing.B) {\n\t\t\t\t\tb.SetBytes(int64(len(input)))\n\t\t\t\t\tb.ReportAllocs()\n\t\t\t\t\tfor b.Loop() {\n\t\t\t\t\t\tif err := w.ast(input); err != nil {\n\t\t\t\t\t\t\tb.Fatal(err)\n\t\t\t\t\t\t}\n\t\t\t\t\t}\n\t\t\t\t})\n\t\t\t}"
	newBench := "if w.ast != nil {\n\t\t\t\tfor _, u := range units {\n\t\t\t\t\tu := u\n\t\t\t\t\tb.Run(\"generated_ast/\"+u.name, func(b *testing.B) {\n\t\t\t\t\t\tb.SetBytes(int64(len(input)))\n\t\t\t\t\t\tb.ReportAllocs()\n\t\t\t\t\t\tfor b.Loop() {\n\t\t\t\t\t\t\tif err := w.ast(input, u.u == pego.Bytes); err != nil { b.Fatal(err) }\n\t\t\t\t\t\t}\n\t\t\t\t\t})\n\t\t\t\t}\n\t\t\t}"
	s, err = typedSweepOverlayReplace(s, oldBench, newBench)
	if err != nil {
		return nil, err
	}
	return format.Source([]byte(s))
}

func writeOrdinaryOverlay(t *testing.T, root, repoRoot, variant string, pkgs []string) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(repoRoot, "bench", "bench_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	overlaySource, err := ordinaryASTOverlay(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, variant)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	benchFile := filepath.Join(dir, "bench_test.go.overlay.go")
	if err := os.WriteFile(benchFile, overlaySource, 0o644); err != nil {
		t.Fatal(err)
	}
	replace := map[string]string{}
	add := func(from, to string) {
		a, err := filepath.Abs(from)
		if err != nil {
			t.Fatal(err)
		}
		b, err := filepath.Abs(to)
		if err != nil {
			t.Fatal(err)
		}
		replace[a] = b
	}
	add(filepath.Join(repoRoot, "bench", "bench_test.go"), benchFile)
	for _, pkg := range pkgs {
		add(filepath.Join(repoRoot, "bench", "gen", pkg, "parser.go"), filepath.Join(dir, "bench", "gen", pkg, "parser.go"))
	}
	data, err := json.Marshal(struct{ Replace map[string]string }{replace})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go-overlay.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
