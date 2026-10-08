package yaml_test

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/yaml"
)

// suiteCase is a test of the yaml-test-suite: an input, and the events and JSON values it must give or
// whether it must be rejected.
type suiteCase struct {
	id     string
	in     string
	events string // the expected events
	json   []byte // the expected values, one JSON value per document; nil if the test has none
	fail   bool   // the input must be rejected
}

// dataCases reads the tests of a data release of the suite: a directory per test (some with numbered
// subdirectories) with in.yaml and test.event, and in.json or error.
func dataCases(t *testing.T, root string) []suiteCase {
	t.Helper()
	var cases []suiteCase
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.Name() != "in.yaml" {
			return err
		}
		dir := filepath.Dir(path)
		id, _ := filepath.Rel(root, dir)
		read := func(name string) []byte {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			return b
		}
		_, err = os.Stat(filepath.Join(dir, "error"))
		cases = append(cases, suiteCase{id: filepath.ToSlash(id), in: string(read("in.yaml")),
			events: string(read("test.event")), json: read("in.json"), fail: err == nil})
		return nil
	})
	if err != nil || len(cases) == 0 {
		t.Fatalf("no test cases in %s: %v", root, err)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].id < cases[j].id })
	return cases
}

// TestSuite runs every test of the yaml-test-suite (testdata/yaml-test-suite, data release
// 2022-01-17): a valid input must give the events of test.event and, where in.json exists, the values
// it holds; an input with an error file must be rejected by Events and LoadAll. Recognize and ParseAST
// must agree on every input.
func TestSuite(t *testing.T) {
	runSuite(t, dataCases(t, filepath.Join("testdata", "yaml-test-suite")))
}

func runSuite(t *testing.T, cases []suiteCase) {
	var passed, failed []string
	var st suiteStats
	for _, c := range cases {
		if err := runSuiteCase(c, &st); err != nil {
			failed = append(failed, c.id)
			t.Errorf("%s: %v", c.id, err)
		} else {
			passed = append(passed, c.id)
		}
	}
	t.Logf("yaml-test-suite: %d passed, %d failed (%d valid, %d with JSON; %d errors, %d rejected by the parser)",
		len(passed), len(failed), st.valid, st.json, st.errors, st.syntax)
	if len(failed) > 0 {
		t.Logf("failed: %s", strings.Join(failed, " "))
	}
}

// suiteStats counts the kinds of tests that passed.
type suiteStats struct{ valid, json, errors, syntax int }

func runSuiteCase(c suiteCase, st *suiteStats) error {
	_, perr := yaml.ParseAST(c.in)
	if rerr := yaml.Recognize(c.in); (perr == nil) != (rerr == nil) {
		return errorf("ParseAST: %v, Recognize: %v", perr, rerr)
	}
	events, eerr := yaml.Events(c.in)
	_, lerr := yaml.LoadAll(c.in)
	if c.fail {
		if eerr == nil {
			return errorf("accepted; events:\n%s", events)
		}
		if lerr == nil {
			return errorf("LoadAll accepted (Events: %v)", eerr)
		}
		st.errors++
		if perr != nil {
			st.syntax++
		}
		return nil
	}
	if eerr != nil {
		return errorf("rejected: %v", eerr)
	}
	if events != c.events {
		return errorf("events differ\n--- input\n%s--- got\n%s--- want\n%s", c.in, events, c.events)
	}
	if c.json != nil {
		if err := checkJSON(c.in, c.json); err != nil {
			return err
		}
		st.json++
	}
	st.valid++
	return nil
}

// TestSuiteSource runs the tests of the source definitions of the yaml-test-suite (the src directory of
// its main branch, newer than the data release in testdata), if YAML_TEST_SUITE_SRC names that
// directory. The definitions are YAML, read with this package, and are converted as the suite's
// bin/suite-to-data.pl does.
func TestSuiteSource(t *testing.T) {
	dir := os.Getenv("YAML_TEST_SUITE_SRC")
	if dir == "" {
		t.Skip("YAML_TEST_SUITE_SRC is not set")
	}
	runSuite(t, sourceCases(t, dir))
}

// sourceCases reads the test definitions of the suite.
func sourceCases(t *testing.T, dir string) []suiteCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no test definitions in %s: %v", dir, err)
	}
	var cases []suiteCase
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		v, err := yaml.Load(string(data))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		tests, _ := v.([]any)
		id := strings.TrimSuffix(filepath.Base(f), ".yaml")
		if len(tests) == 0 {
			t.Fatalf("%s: no tests", f)
		}
		if first, _ := tests[0].(map[string]any); first["skip"] == true {
			continue
		}
		prev := map[string]any{}
		for i, x := range tests {
			def, _ := x.(map[string]any)
			for _, k := range []string{"yaml", "tree", "json"} { // inherited from the test before
				if _, ok := def[k]; !ok && prev[k] != nil {
					def[k] = prev[k]
				}
			}
			prev = def
			c := suiteCase{id: id, fail: def["fail"] != nil}
			if len(tests) > 1 {
				c.id = fmt.Sprintf("%s/%0*d", id, len(fmt.Sprint(len(tests)-1))+1, i) // as the data release numbers them
			}
			c.in = unescapeSource(str(def["yaml"]))
			var tree strings.Builder
			for _, l := range strings.SplitAfter(unescapeSource(str(def["tree"])), "\n") {
				tree.WriteString(strings.TrimLeft(l, " \t"))
			}
			c.events = strings.TrimRight(tree.String(), "\n") + "\n"
			if j, ok := def["json"].(string); ok {
				c.json = []byte(unescapeSource(j))
			}
			cases = append(cases, c)
		}
	}
	return cases
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// unescapeSource replaces the visible characters of the suite's definitions with what they stand for.
func unescapeSource(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "␣"):
			b.WriteByte(' ')
			i += len("␣")
		case strings.HasPrefix(s[i:], "—") || strings.HasPrefix(s[i:], "»"):
			for strings.HasPrefix(s[i:], "—") {
				i += len("—")
			}
			if strings.HasPrefix(s[i:], "»") {
				i += len("»")
				b.WriteByte('\t')
			}
		case strings.HasPrefix(s[i:], "←"):
			b.WriteByte('\r')
			i += len("←")
		case strings.HasPrefix(s[i:], "⇔"):
			b.WriteString("\uFEFF")
			i += len("⇔")
		case strings.HasPrefix(s[i:], "↵"):
			i += len("↵")
		case s[i:] == "∎\n":
			i = len(s)
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// checkJSON compares the values of LoadAll with the JSON values of in.json, one per document.
func checkJSON(src string, data []byte) error {
	got, err := yaml.LoadAll(src)
	if err != nil {
		return errorf("LoadAll: %v", err)
	}
	var want []any
	dec := stdjson.NewDecoder(bytes.NewReader(data))
	for {
		var v any
		if err := dec.Decode(&v); err == io.EOF {
			break
		} else if err != nil {
			return errorf("in.json: %v", err)
		}
		want = append(want, v)
	}
	if len(got) != len(want) {
		return errorf("LoadAll: %d documents, in.json: %d", len(got), len(want))
	}
	for i := range got {
		g, err := toJSON(got[i])
		if err != nil {
			return errorf("document %d: %v", i, err)
		}
		if !reflect.DeepEqual(g, want[i]) {
			return errorf("document %d: LoadAll %#v, in.json %#v", i, g, want[i])
		}
	}
	return nil
}

// toJSON converts a value of LoadAll to what encoding/json decodes the same value into.
func toJSON(v any) (any, error) {
	switch v := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, x := range v {
			y, err := toJSON(x)
			if err != nil {
				return nil, err
			}
			m[k] = y
		}
		return m, nil
	case []any:
		a := make([]any, len(v))
		for i, x := range v {
			y, err := toJSON(x)
			if err != nil {
				return nil, err
			}
			a[i] = y
		}
		return a, nil
	case int64:
		return float64(v), nil
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, errorf("%v has no JSON form", v)
		}
		return v, nil
	case string, bool, nil:
		return v, nil
	}
	return nil, errorf("%T has no JSON form", v)
}

func errorf(format string, args ...any) error { return fmt.Errorf(format, args...) }
