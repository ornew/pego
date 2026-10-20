package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// TestGeneratedTraceOptionsNeedObservability checks that a parser generated without observability
// rejects the trace and profile options on every entry point instead of returning partial data,
// and that one generated with it accepts them.
func TestGeneratedTraceOptionsNeedObservability(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	g, err := syntax.Parse("type N = { Text string }\nrule main N = t:(\"a\"+) #atomic => N{Text: text($t)}\n")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(testGoMod("traceopts")))
	for _, observe := range []bool{false, true} {
		pkg := map[bool]string{false: "plain", true: "observed"}[observe]
		files, err := Generate(g, GenOptions{Module: "example.com/traceopts/" + pkg, Package: pkg, Start: "main", Runtime: RuntimeImport, Types: true, Document: true, Recognize: true, Observability: observe})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			write(filepath.Join(pkg, f.Path), f.Content)
		}
		want := "false"
		if observe {
			want = "true"
		}
		write(filepath.Join(pkg, "trace_test.go"), []byte(`package `+pkg+`

import (
	"strings"
	"testing"

	pegort "github.com/ornew/pego/runtime"
)

func TestTraceOptions(t *testing.T) {
	observed := `+want+`
	var events int
	var profile pegort.Profile
	trace := func(pegort.TraceEvent) { events++ }
	for name, opts := range map[string][]ParseOption{
		"trace":       {pegort.WithTrace(trace)},
		"profile":     {pegort.WithProfile(&profile)},
		"filter":      {pegort.WithTraceFilter("main")},
		"trace depth": {pegort.WithTraceMaxDepth(1)},
		"both":        {pegort.WithTrace(trace), pegort.WithProfile(&profile)},
	} {
		entries := map[string]func() error{
			"Parse":     func() error { _, err := Parse("aa", opts...); return err },
			"ParseRule": func() error { _, err := ParseRule("main", "aa", opts...); return err },
			"ParseAST":  func() error { _, err := ParseAST("aa", opts...); return err },
			"Recognize": func() error { return Recognize("aa", opts...) },
			"NewDocument": func() error { _, err := NewDocument("aa", opts...); return err },
			"Document.Parse": func() error {
				d, err := NewDocument("aa")
				if err != nil {
					return err
				}
				_, err = d.Parse(opts...)
				return err
			},
		}
		for entry, run := range entries {
			err := run()
			switch {
			case observed && err != nil:
				t.Errorf("%s with %s: %v", entry, name, err)
			case !observed && (err == nil || !strings.Contains(err.Error(), "only supported by a parser generated with observability")):
				t.Errorf("%s with %s: got %v, want the observability error", entry, name, err)
			}
		}
	}
	if observed && (events == 0 || profile.Snapshot().Parses == 0) {
		t.Errorf("observed parser reported nothing: %d events", events)
	}
	if !observed && (events != 0 || profile.Snapshot().Parses != 0) {
		t.Errorf("a parser without observability reported %d events", events)
	}
	// The other options still work.
	if _, err := Parse("aa", WithMaxDepth(10)); err != nil {
		t.Error(err)
	}
}
`))
	}
	goVetAndTest(t, dir, true)
}
