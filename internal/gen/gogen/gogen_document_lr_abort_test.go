package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestGeneratedDocumentLeftRecursionAbortedGrowth is the generated Document
// counterpart of TestDocumentLeftRecursionAbortedGrowth: interrupting a parse
// while a left-recursive rule grows, by cancellation or a trace panic at the
// second provisional result, or by every step limit short of completion, must
// leave a Document whose later parses match batch parses, also after edits,
// for Node and typed Documents.
func TestGeneratedDocumentLeftRecursionAbortedGrowth(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated Documents")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	grammars := []struct{ src, text string }{
		{`rule main = x $$
rule x = x "+" "a" / "a"`, "a+a+a+a"},
		{`rule main = x $$
rule x = y "+" "a" / "a"
rule y = x`, "a+a+a+a"},
		{`rule main = x $$
rule x = x "+" #cut "a" / "a"`, "a+a+a+a"},
		{`rule main = x $$
rule x = x "+" t / t
rule t = t "*" "a" / "a"`, "a*a+a+a*a*a"},
		// Typed rules reach typed Documents' own rule calls.
		{`type A = { Text string }
type S = { L E; R A }
type E ~ S | A
rule main E = v:x $$ => $v
rule x E = sum / a
rule sum S = l:y "+" r:a => S{L: $l, R: $r}
rule y E = x
rule a A = t:("a") #atomic => A{Text: text($t)}`, "a+a+a+a"},
	}
	files := map[string]string{"go.mod": gentest.GoMod("fixture")}
	for i, g := range grammars {
		parsed, err := parseTest(g.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, typed := range []bool{false, true} {
			pkg, route := fmt.Sprintf("g%d", i), generatedDocumentNodeRoute
			if typed {
				pkg, route = fmt.Sprintf("t%d", i), generatedDocumentTypedRoute
			}
			code, err := gentest.GenerateCode(t, parsed, gen.GenOptions{Package: pkg, Start: "main", Document: true, Types: typed, Observability: true})
			if err != nil {
				t.Fatal(err)
			}
			files[pkg+"/parser.go"] = string(code)
			consumer := strings.ReplaceAll(generatedDocumentGrowthConsumer, "package fixture", "package "+pkg)
			files[pkg+"/parser_test.go"] = strings.ReplaceAll(consumer, "INPUT", fmt.Sprintf("%q", g.text))
			files[pkg+"/route_test.go"] = "package " + pkg + route
		}
	}
	dir := t.TempDir()
	for name, source := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goBin, "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The routes parse a Document as the interrupted parse does and compare its
// other results with batch parses.
const (
	generatedDocumentNodeRoute = `
import "testing"

func interruptible(d *Document, opts ...ParseOption) error { _, err := d.Parse(opts...); return err }
func matchesTyped(*testing.T, *Document, PositionUnit) {}
`
	generatedDocumentTypedRoute = `
import (
	"fmt"
	"reflect"
	"testing"
)

func interruptible(d *Document, opts ...ParseOption) error { _, err := d.ParseAST(opts...); return err }

func matchesTyped(t *testing.T, d *Document, unit PositionUnit) {
	t.Helper()
	v, err := d.ParseAST()
	want, wantErr := ParseAST(d.Text(), WithUnit(unit))
	if !reflect.DeepEqual(v, want) || fmt.Sprint(err) != fmt.Sprint(wantErr) {
		t.Fatalf("text %q: typed document %#v / %v; batch %#v / %v", d.Text(), v, err, want, wantErr)
	}
}
`
)

const generatedDocumentGrowthConsumer = `package fixture

import (
	pegort "github.com/ornew/pego/runtime"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func matchesBatch(t *testing.T, d *Document, unit PositionUnit) {
	t.Helper()
	node, err := d.Parse()
	fresh, freshErr := Parse(d.Text(), WithUnit(unit))
	got, _ := json.Marshal(node)
	want, _ := json.Marshal(fresh)
	if string(got) != string(want) || fmt.Sprint(err) != fmt.Sprint(freshErr) {
		t.Fatalf("text %q: document %s / %v; batch %s / %v", d.Text(), got, err, want, freshErr)
	}
	matchesTyped(t, d, unit)
}

// interrupt parses an edited Document with opts, which must interrupt the
// parse, and checks later parses and edits against batch parses.
func interrupt(t *testing.T, unit PositionUnit, opts func(context.CancelFunc) []ParseOption) (completed bool, err error) {
	d, err := NewDocument(INPUT, WithUnit(unit))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Parse(); err != nil {
		t.Fatal(err)
	}
	if err := d.Edit(len(d.Text()), len(d.Text()), "+a"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	func() {
		defer func() {
			if x := recover(); x != nil && x != "interrupt growth" {
				panic(x)
			}
		}()
		err = interruptible(d, append(opts(cancel), WithContext(ctx))...)
	}()
	if err == nil {
		return true, nil
	}
	interrupted := err
	matchesBatch(t, d, unit)
	matchesBatch(t, d, unit)
	text := d.Text()
	mid := strings.Index(text, "+")
	for _, e := range []struct {
		start, end int
		text       string
	}{{mid, mid + 1, "+a+"}, {0, 1, "a+a"}, {len(text), len(text), "+"}} {
		if err := d.Edit(e.start, e.end, e.text); err != nil {
			t.Fatal(err)
		}
		matchesBatch(t, d, unit)
	}
	return false, interrupted
}

func TestInterruptedGrowth(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, cause := range []string{"cancel", "panic"} {
			grown := 0
			_, err := interrupt(t, unit, func(cancel context.CancelFunc) []ParseOption {
				return []ParseOption{pegort.WithTrace(func(e pegort.TraceEvent) {
					if e.Kind != pegort.TraceExit || !e.Memo || !e.Matched || e.Rule == "main" {
						return
					}
					if grown++; grown == 2 {
						if cause == "panic" {
							panic("interrupt growth")
						}
						cancel()
					}
				})}
			})
			if grown < 2 || (cause == "cancel") != errors.Is(err, context.Canceled) {
				t.Fatalf("%s: growth reached %d provisional results, %v", cause, grown, err)
			}
		}
		limited := 0
		for steps := uint64(1); ; steps++ {
			completed, err := interrupt(t, unit, func(context.CancelFunc) []ParseOption {
				return []ParseOption{WithLimits(Limits{Budget: steps})}
			})
			if completed {
				break
			}
			var limit *LimitError
			if !errors.As(err, &limit) {
				t.Fatalf("%d steps: %v", steps, err)
			}
			limited++
			if steps > 100000 {
				t.Fatal("step limit never sufficed")
			}
		}
		if limited < 10 {
			t.Fatalf("only %d step limits interrupted the parse", limited)
		}
	}
}
`
