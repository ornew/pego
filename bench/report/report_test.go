package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const root = "../.."

// TestDocumentIsUpToDate checks that docs/benchmarks.md is what the template renders from
// bench/results.txt: the document is generated, not edited by hand.
func TestDocumentIsUpToDate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, "bench", "results.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := render(root, data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "docs", "benchmarks.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("docs/benchmarks.md is out of date: run go run ./bench/report -in bench/results.txt " +
			"(or go run ./bench/report to measure again)")
	}
}

// TestMissingBenchmark checks that rendering fails, naming the benchmark, when the results lack
// one the document shows.
func TestMissingBenchmark(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, "bench", "results.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, l := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(l, "BenchmarkStream/encoding_csv") {
			kept = append(kept, l)
		}
	}
	_, err = render(root, []byte(strings.Join(kept, "\n")))
	if err == nil || !strings.Contains(err.Error(), "Stream/encoding_csv") {
		t.Errorf("got %v, want an error naming Stream/encoding_csv", err)
	}
}

func TestParseResults(t *testing.T) {
	r, err := parseResults([]byte(`# date: 2026-01-02
# commit: abc
cpu: Some CPU
BenchmarkParse/A/x-16   	 10	 300 ns/op	 2.00 MB/s	 100 B/op	 3 allocs/op
BenchmarkParse/A/x-16   	 10	 100 ns/op	 3.00 MB/s	 120 B/op	 3 allocs/op
BenchmarkParse/A/x-16   	 10	 200 ns/op	 1.00 MB/s	 110 B/op	 3 allocs/op
BenchmarkPrepare/p-16   	 10	 50 ns/op	 1234 file-bytes	 10 B/op	 1 allocs/op
`))
	if err != nil {
		t.Fatal(err)
	}
	a := r.bench["Parse/A/x"]
	if a == nil || a.ns != 200 || a.mbs != 2 || a.bytesPerOp != 110 || a.allocs != 3 || a.runs != 3 {
		t.Errorf("Parse/A/x: %+v", a)
	}
	if p := r.bench["Prepare/p"]; p == nil || p.metrics["file-bytes"] != 1234 {
		t.Errorf("Prepare/p: %+v", p)
	}
	if r.meta["date"] != "2026-01-02" || r.meta["commit"] != "abc" || r.meta["cpu"] != "Some CPU" {
		t.Errorf("meta: %v", r.meta)
	}
}

func TestFormats(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{fmtTime(14_300_000), "14.3 ms"},
		{fmtTime(730_000), "730 µs"},
		{fmtTime(6_832_000), "6.83 ms"},
		{fmtTime(100_385_000), "100 ms"},
		{fmtBytes(262_144), "262 KB"},
		{fmtBytes(20_000_000), "20.0 MB"},
		{fmtCount(767), "767"},
		{fmtCount(53_340), "53 k"},
		{fmtCount(1_728), "1.7 k"},
		{fmtFactor(1.2649), "1.26"},
		{fmtSig(160.4, 2), "160"},
		{fmtSig(1.84, 2), "1.8"},
		{rangeOf("1.0", "1.0"), "1.0"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
