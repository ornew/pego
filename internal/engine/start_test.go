package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// compileStarts compiles source as written, without marking start rules for the test.
func compileStarts(t *testing.T, src string) (*Program, error) {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	return Compile(g, Options{})
}

func TestStartRules(t *testing.T) {
	for _, tc := range []struct {
		src    string
		starts []string
		def    string
	}{
		{"@start\nrule a = b\nrule b = \"x\"\n", []string{"a"}, "a"},
		{"rule a = \"x\"\n@start\nrule b = a\n", []string{"b"}, "b"},
		{"@start(default)\nrule a = b\nrule b = \"x\"\n", []string{"a"}, "a"},
		{"@start\nrule a = b\n@start(default)\nrule b = \"x\"\n", []string{"a", "b"}, "b"},
		{"@start(default)\nrule a = b c\n@start\nrule b = \"x\"\n@start\nrule c = \"y\"\n", []string{"a", "b", "c"}, "a"},
		// Comments may stand between the attribute and the rule.
		{"@start // the entry\n// The rule.\nrule a = \"x\"\n", []string{"a"}, "a"},
	} {
		prog, err := compileStarts(t, tc.src)
		if err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		if !slices.Equal(prog.Starts(), tc.starts) || prog.DefaultStart() != tc.def {
			t.Errorf("%q: starts %v default %q, want %v %q", tc.src, prog.Starts(), prog.DefaultStart(), tc.starts, tc.def)
		}
	}
}

func TestStartRuleErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"rule a = b\nrule b = \"x\"\n", "1:1: the grammar has no start rule: mark each rule where parsing may begin with @start"},
		{"package p\n", "the grammar has no start rule: mark each rule where parsing may begin with @start"},
		{"@start\nrule a = b\n@start\nrule b = \"x\"\n", "3:1: several start rules (a, b) and no default: mark one with @start(default)"},
		{"@start\nrule a = b c\n@start\nrule b = \"x\"\n@start\nrule c = \"y\"\n", "3:1: several start rules (a, b, c) and no default: mark one with @start(default)"},
		{"@start(default)\nrule a = b\n@start(default)\nrule b = \"x\"\n", "3:1: more than one default start rule: a and b"},
		{"@start @start\nrule a = \"x\"\n", "1:8: duplicate @start on rule a"},
		{"@start\n@start(default)\nrule a = \"x\"\n", "2:1: duplicate @start on rule a"},
		{"@start(main)\nrule a = \"x\"\n", "1:1: @start: the only argument is default"},
		{"@start(\"default\")\nrule a = \"x\"\n", "1:1: @start: the only argument is default"},
		{"@start(default, default)\nrule a = \"x\"\n", "1:1: @start: the only argument is default"},
		{"@start(default=default)\nrule a = \"x\"\n", "1:1: @start: the only argument is default"},
		{"@start\n@entry\nrule a = \"x\"\n", "2:1: unknown declaration attribute @entry"},
		// Other errors come first, and the start rules are reported only
		// once the grammar is otherwise valid.
		{"rule a = b\n", "1:10: undefined rule b"},
	} {
		_, err := compileStarts(t, tc.src)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%q\n got %v\nwant %s", tc.src, err, tc.want)
		}
	}
}

// A hidden rule cannot be a start rule: @start on it is an error at the
// attribute, whether the program is compiled or generated, and the rules
// behind inline actions never become start rules.
func TestStartHiddenRule(t *testing.T) {
	const hidden = "rule _x is hidden: a hidden rule cannot be a start rule"
	for _, tc := range []struct{ src, want string }{
		{"@start\nrule _x = \"x\"\n", "1:1: " + hidden},
		{"@start(default)\nrule _x = \"x\"\n", "1:1: " + hidden},
		{"@start(default)\nrule a = _x\n\n@start\nrule _x = \"x\"\n", "4:1: " + hidden},
	} {
		_, err := compileStarts(t, tc.src)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%q\n got %v\nwant %s", tc.src, err, tc.want)
		}
		g, perr := syntax.Parse(tc.src)
		if perr != nil {
			t.Fatal(perr)
		}
		if _, err := generateCode(g, GenOptions{Package: "p"}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Generate %q\n got %v\nwant %s", tc.src, err, tc.want)
		}
		if _, err := GenerateTS(g, GenOptions{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("GenerateTS %q\n got %v\nwant %s", tc.src, err, tc.want)
		}
	}
	// Negative control: the hidden rule compiles when it is not marked.
	prog, err := compileStarts(t, "@start\nrule a = _x\nrule _x = \"x\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(prog.Starts(), []string{"a"}) {
		t.Errorf("starts %v", prog.Starts())
	}
	_, err = prog.ParseWith("_x", "x", ParseOptions{})
	if want := "rule _x is hidden: a rule whose name begins with an underscore is not an entry point"; err == nil || err.Error() != want {
		t.Errorf("Parse(_x): got %v, want %s", err, want)
	}
	// The hidden rules behind inline actions are not start rules.
	prog, err = compileStarts(t, "@start\nrule a = (\"x\" => 1) (\"y\" => 2)\n")
	if err != nil {
		t.Fatal(err)
	}
	var helpers []string
	for _, r := range prog.rules {
		if hiddenRule(r.name) {
			helpers = append(helpers, r.name)
		}
	}
	if len(helpers) != 2 || !slices.Equal(prog.Starts(), []string{"a"}) || prog.DefaultStart() != "a" {
		t.Errorf("hidden rules %v starts %v default %q", helpers, prog.Starts(), prog.DefaultStart())
	}
}

const startRestrictionSource = `@start(default)
rule main = item* #stream $$
@start
rule item = "a"
rule other = "b" _h?
rule _h = "c"
`

// checkStartRestriction checks that every way into prog parses only from start rules. Streams
// and documents do not recognize, so they are left out with o.Recognize.
// streams says whether prog can stream on its backend: a program compiled with a recognition
// bundle does not stream on the bytecode backends yet, whatever its start rules.
func checkStartRestriction(t *testing.T, prog *Program, o ParseOptions, streams bool) {
	t.Helper()
	notStart := "rule other is not a start rule; parsing can begin only at a rule marked @start"
	for _, tc := range []struct{ start, want string }{
		{"other", notStart},
		{"nope", "rule nope is not defined"},
	} {
		surfaces := map[string]error{}
		_, surfaces["Parse"] = prog.ParseWith(tc.start, "a", o)
		_, surfaces["ParsePrefix"] = prog.ParsePrefixWith(tc.start, "a", o)
		if !o.Recognize {
			surfaces["ParseStream"] = prog.ParseStreamWith(tc.start, strings.NewReader("a"), func(*Node) error { return nil }, o)
			_, surfaces["NewDocument"] = prog.NewDocumentWith(tc.start, "a", o)
			_, surfaces["Iterate"] = prog.Iterate(tc.start, strings.NewReader("a"), o)
		}
		if tc.start != "" {
			_, surfaces["MarshalBinary"] = prog.MarshalBinary(tc.start)
		}
		for name, err := range surfaces {
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s(%q): got %v, want %s", name, tc.start, err, tc.want)
			}
		}
	}
	// An empty name selects the default start rule, main, which consumes "aa"
	// where item consumes only "a".
	if _, err := prog.ParseWith("", "aa", o); err != nil {
		t.Errorf("Parse(\"\"): %v", err)
	}
	if r, err := prog.ParsePrefixWith("", "aa", o); err != nil || r.Consumed != 2 {
		t.Errorf("ParsePrefix(\"\"): consumed %d, %v", r.Consumed, err)
	}
	if r, err := prog.ParsePrefixWith("item", "aa", o); err != nil || r.Consumed != 1 {
		t.Errorf("ParsePrefix(item): consumed %d, %v", r.Consumed, err)
	}
	if err := prog.CheckStart(""); err != nil {
		t.Errorf("CheckStart(\"\"): %v", err)
	}
	if !o.Recognize {
		if _, err := prog.NewDocumentWith("", "aa", o); err != nil {
			t.Errorf("NewDocument(\"\"): %v", err)
		}
		if streams {
			if err := prog.ParseStreamWith("", strings.NewReader("aa"), func(*Node) error { return nil }, o); err != nil {
				t.Errorf("ParseStream(\"\"): %v", err)
			}
			if it, err := prog.Iterate("", strings.NewReader("aa"), o); err != nil {
				t.Errorf("Iterate(\"\"): %v", err)
			} else {
				it.Close()
			}
		}
	}
	// Negative control: the start rules are accepted.
	for _, start := range []string{"main", "item"} {
		if _, err := prog.ParseWith(start, "a", o); err != nil {
			t.Errorf("Parse(%q): %v", start, err)
		}
		if _, err := prog.ParsePrefixWith(start, "a", o); err != nil {
			t.Errorf("ParsePrefix(%q): %v", start, err)
		}
		if o.Recognize {
			continue
		}
		if _, err := prog.NewDocumentWith(start, "a", o); err != nil {
			t.Errorf("NewDocument(%q): %v", start, err)
		}
	}
	if o.Recognize || !streams {
		return
	}
	if err := prog.ParseStreamWith("main", strings.NewReader("aa"), func(*Node) error { return nil }, o); err != nil {
		t.Errorf("ParseStream: %v", err)
	}
	it, err := prog.Iterate("main", strings.NewReader("aa"), o)
	if err != nil {
		t.Errorf("Iterate: %v", err)
	} else {
		it.Close()
	}
}

func TestStartRestriction(t *testing.T) {
	for _, recognition := range []bool{false, true} {
		g, err := syntax.Parse(startRestrictionSource)
		if err != nil {
			t.Fatal(err)
		}
		prog, err := Compile(g, Options{recognize: recognition})
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range []Backend{Default, Closure, Bytecode, BytecodeIterative} {
			for _, recognize := range []bool{false, true} {
				t.Run(b.String(), func(t *testing.T) {
					checkStartRestriction(t, prog, ParseOptions{Backend: b, Recognize: recognize}, !recognition || prog.backend(b) == Closure)
				})
			}
		}
	}
}

func TestStartRoundTrip(t *testing.T) {
	for _, recognition := range []bool{false, true} {
		for _, omitAST := range []bool{false, true} {
			g, err := syntax.Parse(startRestrictionSource)
			if err != nil {
				t.Fatal(err)
			}
			prog, err := Compile(g, Options{recognize: recognition})
			if err != nil {
				t.Fatal(err)
			}
			for _, start := range []string{"", "item"} {
				data, err := prog.MarshalBinaryWith(start, MarshalOptions{OmitAST: omitAST})
				if err != nil {
					t.Fatal(err)
				}
				loaded, gotStart, err := LoadProgram(data, Options{})
				if err != nil {
					t.Fatalf("recognition=%t omitAST=%t start=%q: %v", recognition, omitAST, start, err)
				}
				if gotStart != start {
					t.Errorf("start %q, want %q", gotStart, start)
				}
				if !slices.Equal(loaded.Starts(), []string{"main", "item"}) || loaded.DefaultStart() != "main" {
					t.Errorf("recognition=%t omitAST=%t: starts %v default %q", recognition, omitAST, loaded.Starts(), loaded.DefaultStart())
				}
				backends := []Backend{Bytecode, BytecodeIterative}
				if !omitAST {
					backends = append(backends, Closure)
				}
				for _, b := range backends {
					checkStartRestriction(t, loaded, ParseOptions{Backend: b}, !recognition || b == Closure)
				}
			}
		}
	}
	// A single start rule is the default without (default), and stays so.
	prog, err := compileStarts(t, "@start\nrule a = b\nrule b = \"x\"\n")
	if err != nil {
		t.Fatal(err)
	}
	data, err := prog.MarshalBinary("")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(loaded.Starts(), []string{"a"}) || loaded.DefaultStart() != "a" {
		t.Errorf("starts %v default %q", loaded.Starts(), loaded.DefaultStart())
	}
}

func TestStartModuleValidation(t *testing.T) {
	fresh := func() *Module {
		prog, err := compileStarts(t, startRestrictionSource)
		if err != nil {
			t.Fatal(err)
		}
		m := *prog.Module()
		m.Starts = slices.Clone(m.Starts)
		return &m
	}
	m := fresh()
	if err := validateModule(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Starts) != 2 || m.DefaultStart != m.Starts[0] {
		t.Fatalf("starts %v default %d", m.Starts, m.DefaultStart)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Module)
		want   string
	}{
		{"no start rule", func(m *Module) { m.Starts = nil }, "no start rule"},
		{"out of range", func(m *Module) { m.Starts[1] = len(m.Rules) }, "start rule"},
		{"negative", func(m *Module) { m.Starts[0] = -1 }, "start rule"},
		{"repeated", func(m *Module) { m.Starts[1] = m.Starts[0] }, "invalid start rule"},
		{"out of order", func(m *Module) { m.Starts[0], m.Starts[1] = m.Starts[1], m.Starts[0] }, "invalid start rule"},
		{"default not a start rule", func(m *Module) {
			for i := range m.Rules {
				if !slices.Contains(m.Starts, i) {
					m.DefaultStart = i
					return
				}
			}
		}, "is not a start rule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fresh()
			tc.mutate(m)
			if err := validateModule(m); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}
