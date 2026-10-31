package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syntheticTS returns TypeScript results for every workload and mode, with the given number of runs.
func syntheticTS(runs int) string {
	var b strings.Builder
	b.WriteString("# date: 2026-02-03\n# commit: abc\n# node: v24.0.0\n# v8: 13.0\n# cores: 8\n")
	for r := 0; r < runs; r++ {
		for _, w := range tsWorkloads {
			for _, m := range tsModes {
				fmt.Fprintf(&b, "BenchmarkTS/%s/%s 10 %d ns/op %d.50 MB/s\n", m.name, w.name, 1_000_000*(r+1), r+1)
			}
		}
	}
	return b.String()
}

func syntheticSizes() string {
	var b strings.Builder
	b.WriteString("# commit: abc\n# go: go1.27.1\n")
	for _, p := range append([]string{"runtime"}, sizeParsers...) {
		for _, m := range []string{"go-source", "go-binary", "ts-raw", "ts-gzip"} {
			if p == "runtime" && strings.HasPrefix(m, "go") {
				continue
			}
			fmt.Fprintf(&b, "size %s %s 12345\n", p, m)
		}
	}
	return b.String()
}

func goResults(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "bench", "results.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRenderTypeScriptAndSizeSections(t *testing.T) {
	doc, err := renderAll(root, goResults(t), []byte(syntheticTS(3)), []byte(syntheticSizes()))
	if err != nil {
		t.Fatal(err)
	}
	got := string(doc)
	for _, want := range []string{
		"### Generated TypeScript (Node v24.0.0)",
		"The generated TypeScript parsers were measured with Node v24.0.0.",
		"Each figure is the median of 3 runs",
		"| JSON | 2.00 ms (2.5 MB/s) |", // the median of 1, 2 and 3 ms and of 1.5, 2.5 and 3.5 MB/s
		"### Generated Parser Size",
		"| duckdb | 12.3 KB | 12.3 KB | 12.3 KB | 12.3 KB |",
		"weighs 12.3 KB (12.3 KB gzipped)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("document lacks %q", want)
		}
	}
	// The TypeScript benchmarks have no memory figures and stay out of the table of Go results.
	if strings.Contains(got, "| `TS/") {
		t.Error("the table of all results lists TypeScript benchmarks")
	}
}

func TestRenderWithoutTypeScriptOrSizes(t *testing.T) {
	doc, err := renderAll(root, goResults(t), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(doc), "TypeScript") || strings.Contains(string(doc), "Generated Parser Size") {
		t.Error("the document shows sections whose results were not given")
	}
}

func TestRenderRejectsIncompleteTypeScriptResults(t *testing.T) {
	var kept []string
	for _, l := range strings.Split(syntheticTS(3), "\n") {
		if !strings.HasPrefix(l, "BenchmarkTS/Recognize/XML ") {
			kept = append(kept, l)
		}
	}
	_, err := renderAll(root, goResults(t), []byte(strings.Join(kept, "\n")), nil)
	if err == nil || !strings.Contains(err.Error(), "TS/Recognize/XML") {
		t.Errorf("got %v, want an error naming TS/Recognize/XML", err)
	}

	lines := strings.Split(syntheticTS(3), "\n")
	dropped := false
	kept = nil
	for _, l := range lines {
		if !dropped && strings.HasPrefix(l, "BenchmarkTS/Parse/JSON ") {
			dropped = true
			continue
		}
		kept = append(kept, l)
	}
	_, err = renderAll(root, goResults(t), []byte(strings.Join(kept, "\n")), nil)
	if err == nil || !strings.Contains(err.Error(), "TS/Parse/JSON has 2 runs, want 3") {
		t.Errorf("got %v, want a missing-run error", err)
	}
}

func TestRenderRejectsForeignTypeScriptNames(t *testing.T) {
	_, err := renderAll(root, goResults(t), []byte("BenchmarkParse/JSON/closure/codepoints 1 10 ns/op\n"), nil)
	if err == nil || !strings.Contains(err.Error(), "does not start with TS/") {
		t.Errorf("got %v", err)
	}
}

func TestRenderRejectsMissingSize(t *testing.T) {
	var kept []string
	for _, l := range strings.Split(syntheticSizes(), "\n") {
		if l != "size cel ts-gzip 12345" {
			kept = append(kept, l)
		}
	}
	_, err := renderAll(root, goResults(t), nil, []byte(strings.Join(kept, "\n")))
	if err == nil || !strings.Contains(err.Error(), "size cel ts-gzip") {
		t.Errorf("got %v, want an error naming size cel ts-gzip", err)
	}
}

func TestParseBenchLinesWithoutBenchmem(t *testing.T) {
	line := "BenchmarkTS/Parse/A 5 100 ns/op 2.00 MB/s\n"
	r, err := parseBenchLines([]byte(line), false)
	if err != nil {
		t.Fatal(err)
	}
	if a := r.bench["TS/Parse/A"]; a == nil || a.ns != 100 || a.mbs != 2 || a.bytesPerOp != 0 {
		t.Errorf("TS/Parse/A: %+v", a)
	}
	if _, err := parseResults([]byte(line)); err == nil || !strings.Contains(err.Error(), "missing required B/op") {
		t.Errorf("parseResults accepted a line without B/op: %v", err)
	}
	if _, err := parseBenchLines([]byte("BenchmarkTS/Parse/A 5 MB/s\n"), false); err == nil {
		t.Error("accepted a line without a time")
	}
}

func TestParseSizes(t *testing.T) {
	meta := map[string]string{}
	got, err := parseSizes([]byte("# commit: abc\nsize json go-source 10\nsize json ts-raw 20\n"), meta)
	if err != nil {
		t.Fatal(err)
	}
	if got["json"]["go-source"] != 10 || got["json"]["ts-raw"] != 20 || meta["size.commit"] != "abc" {
		t.Errorf("sizes %v, meta %v", got, meta)
	}
	for name, data := range map[string]string{
		"duplicate":    "size json ts-raw 1\nsize json ts-raw 2\n",
		"short":        "size json ts-raw\n",
		"not a number": "size json ts-raw many\n",
		"empty":        "# commit: abc\n",
	} {
		if _, err := parseSizes([]byte(data), map[string]string{}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestParserGenArgsAdaptsGoOptionsToTypeScript(t *testing.T) {
	args, err := parserGenArgs(root, "cel")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if !strings.HasSuffix(got, "cel/cel.zp -types") || !strings.Contains(got, "-g "+filepath.Join(root, "parsers", "cel", "cel.zp")) {
		t.Errorf("cel args: %s", got)
	}
	for _, bad := range []string{"-module", "-runtime", "-nodoc", "-typed-only", "-o "} {
		if strings.Contains(got, bad) {
			t.Errorf("cel args keep %s: %s", bad, got)
		}
	}
	if args, _ := parserGenArgs(root, "json"); !strings.Contains(strings.Join(args, " "), "-recognize -budget-controls") {
		t.Errorf("json args: %v", args)
	}
	if _, err := parserGenArgs(root, "nonexistent"); err == nil {
		t.Error("unknown parser: no error")
	}
}

func TestEverySizeParserHasAGoParser(t *testing.T) {
	for _, name := range sizeParsers {
		if _, err := os.Stat(filepath.Join(root, "parsers", name, "parser.go")); err != nil {
			t.Error(err)
		}
		if _, err := parserGenArgs(root, name); err != nil {
			t.Error(err)
		}
	}
}

func TestSizeOfFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 10000)), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, gz, err := sizeOfFile(path)
	if err != nil || raw != 10000 || gz <= 0 || gz >= 200 {
		t.Errorf("raw %d, gzip %d, err %v", raw, gz, err)
	}
}

func TestLoadResultsTreatsSiblingFilesAsOptional(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "results.txt")
	if err := os.WriteFile(goPath, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ts, sizes, err := loadResults(goPath)
	if err != nil || ts != nil || sizes != nil {
		t.Fatalf("ts %q, sizes %q, err %v", ts, sizes, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "results-ts.txt"), []byte("ts"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ts, _, err = loadResults(goPath); err != nil || string(ts) != "ts" {
		t.Fatalf("ts %q, err %v", ts, err)
	}
	if _, _, _, err := loadResults(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("missing Go results: no error")
	}
}

// TestHeadersShareTheCommitDescription checks that a header names the commit it is given, so that
// the files of one capture agree even though the capture itself changes tracked files.
func TestHeadersShareTheCommitDescription(t *testing.T) {
	var a, b bytes.Buffer
	writeHeader(&a, "abc123")
	writeHeader(&b, "abc123")
	for _, h := range []string{a.String(), b.String()} {
		if !strings.Contains(h, "# commit: abc123\n") {
			t.Errorf("header lacks the given commit:\n%s", h)
		}
	}
}

func TestDescribeCommitMarksUncommittedChanges(t *testing.T) {
	if got := describeCommit(root); got == "" || got == "unknown" {
		t.Fatalf("describeCommit = %q", got)
	}
}
