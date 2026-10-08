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

// suiteCase is a test of the yaml-test-suite: a directory with in.yaml and test.event, and in.json
// or error.
type suiteCase struct {
	id, dir string
}

func suiteCases(t *testing.T) []suiteCase {
	t.Helper()
	root := filepath.Join("testdata", "yaml-test-suite")
	var cases []suiteCase
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "in.yaml" {
			dir := filepath.Dir(path)
			id, _ := filepath.Rel(root, dir)
			cases = append(cases, suiteCase{filepath.ToSlash(id), dir})
		}
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
	var passed, failed []string
	var st suiteStats
	for _, c := range suiteCases(t) {
		if err := runSuiteCase(c, &st); err != nil {
			failed = append(failed, c.id)
			t.Errorf("%s: %v", c.id, err)
		} else {
			passed = append(passed, c.id)
		}
	}
	t.Logf("yaml-test-suite: %d passed, %d failed (%d valid, %d with in.json; %d errors, %d rejected by the parser)",
		len(passed), len(failed), st.valid, st.json, st.errors, st.syntax)
	if len(failed) > 0 {
		t.Logf("failed: %s", strings.Join(failed, " "))
	}
}

// suiteStats counts the kinds of tests that passed.
type suiteStats struct{ valid, json, errors, syntax int }

func runSuiteCase(c suiteCase, st *suiteStats) error {
	in, err := os.ReadFile(filepath.Join(c.dir, "in.yaml"))
	if err != nil {
		return err
	}
	src := string(in)
	_, perr := yaml.ParseAST(src)
	if rerr := yaml.Recognize(src); (perr == nil) != (rerr == nil) {
		return errorf("ParseAST: %v, Recognize: %v", perr, rerr)
	}
	events, eerr := yaml.Events(src)
	_, lerr := yaml.LoadAll(src)
	if _, err := os.Stat(filepath.Join(c.dir, "error")); err == nil {
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
	want, err := os.ReadFile(filepath.Join(c.dir, "test.event"))
	if err != nil {
		return err
	}
	if events != string(want) {
		return errorf("events differ\n--- input\n%s--- got\n%s--- want\n%s", in, events, want)
	}
	data, err := os.ReadFile(filepath.Join(c.dir, "in.json"))
	if err != nil {
		st.valid++
		return nil // no JSON for this test
	}
	if err := checkJSON(src, data); err != nil {
		return err
	}
	st.valid++
	st.json++
	return nil
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
