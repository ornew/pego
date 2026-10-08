package typescript_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ornew/pego/parsers/typescript"
)

// The differential tests run the TypeScript compiler (with node) on the same sources and compare.
//
//	PEGO_TYPESCRIPT        the directory of the typescript package (node_modules/typescript), version 5.9.3
//	PEGO_TYPESCRIPT_TESTS  the tests/cases directory of the TypeScript repository at tag v5.9.3
//
// The tests are skipped when the variables are not set.
var (
	tscVerbose = flag.Bool("tsc.v", false, "log every mismatch with the TypeScript compiler")
	tscFilter  = flag.String("tsc.run", "", "only the test cases whose path matches this regular expression")
	tscUpdate  = flag.Bool("tsc.update", false, "rewrite testdata/tsc-known-failures.txt")
)

// tscResult is the compiler's result for a source.
type tscResult struct {
	Diags []struct {
		Pos  int    `json:"pos"`
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	} `json:"diags"`
	Nodes []flatNode `json:"nodes"`
}

// flatNode is a node of a tree listed in preorder: its kind, its range in UTF-16 offsets and the number of
// its children.
type flatNode struct {
	Kind       string
	Start, End int
	Children   int
}

func (n *flatNode) UnmarshalJSON(data []byte) error {
	var a []any
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if len(a) != 4 {
		return fmt.Errorf("malformed node %s", data)
	}
	n.Kind, _ = a[0].(string)
	n.Start, n.End, n.Children = toInt(a[1]), toInt(a[2]), toInt(a[3])
	return nil
}

// tscProcess is a node process running testdata/tsc.js.
type tscProcess struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func startTSC(t testing.TB) *tscProcess {
	t.Helper()
	pkg := os.Getenv("PEGO_TYPESCRIPT")
	if pkg == "" {
		t.Skip("PEGO_TYPESCRIPT is not set (the directory of the typescript package)")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	pkg, err := filepath.Abs(pkg)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "testdata/tsc.js", pkg)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &tscProcess{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20)}
	t.Cleanup(func() {
		p.in.Close()
		p.cmd.Wait()
	})
	return p
}

func (p *tscProcess) parse(name, text string) (*tscResult, error) {
	req, err := json.Marshal(struct {
		Name string `json:"name"`
		Text string `json:"text"`
		TSX  bool   `json:"tsx"`
	}{name, text, typescript.IsTSX(name)})
	if err != nil {
		return nil, err
	}
	if _, err := p.in.Write(append(req, '\n')); err != nil {
		return nil, err
	}
	line, err := p.out.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var r tscResult
	return &r, json.Unmarshal(line, &r)
}

// flatten lists the tree of n in preorder, as testdata/tsc.js does, with UTF-16 offsets.
func flatten(n typescript.ASTNode, u16 []int) []flatNode {
	var out []flatNode
	var walk func(n typescript.ASTNode)
	walk = func(n typescript.ASTNode) {
		s, e := n.Range()
		i := len(out)
		out = append(out, flatNode{Kind: typescript.Kind(n), Start: u16[s], End: u16[e]})
		typescript.ForEachChild(n, func(c typescript.ASTNode) bool {
			out[i].Children++
			walk(c)
			return true
		})
	}
	walk(n)
	return out
}

// utf16Offsets maps each byte offset of text to its offset in UTF-16 code units.
func utf16Offsets(text string) []int {
	u := make([]int, len(text)+1)
	n := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		for j := range size {
			u[i+j] = n
		}
		n += utf16.RuneLen(r) // an invalid byte is U+FFFD, here and in the JSON of the request
		i += size
	}
	u[len(text)] = n
	return u
}

// diffTrees returns the first difference between the compiler's tree and ours, with the path to it: the
// kinds of the ancestors and the index of each among its parent's children.
func diffTrees(want, got []flatNode) string {
	type frame struct {
		path        string
		left, index int // children left to visit in the compiler's tree, index of the next one
	}
	var stack []frame
	for i := 0; i < len(want) || i < len(got); i++ {
		path := ""
		if len(stack) > 0 {
			top := &stack[len(stack)-1]
			path = fmt.Sprintf("%s/%d", top.path, top.index)
		}
		if i >= len(want) || i >= len(got) {
			return path + ": the trees have different sizes"
		}
		w, g := want[i], got[i]
		here := path + "/" + w.Kind
		switch {
		case w.Kind != g.Kind:
			return fmt.Sprintf("%s: kind %s, got %s at %d-%d", here, w.Kind, g.Kind, g.Start, g.End)
		case w.Start != g.Start || w.End != g.End:
			return fmt.Sprintf("%s: range %d-%d, got %d-%d", here, w.Start, w.End, g.Start, g.End)
		case w.Children != g.Children:
			return fmt.Sprintf("%s at %d-%d: %d children, got %d", here, w.Start, w.End, w.Children, g.Children)
		}
		if len(stack) > 0 {
			stack[len(stack)-1].left--
			stack[len(stack)-1].index++
		}
		if w.Children > 0 {
			stack = append(stack, frame{path: here, left: w.Children})
		}
		for len(stack) > 0 && stack[len(stack)-1].left == 0 {
			stack = stack[:len(stack)-1]
		}
	}
	return ""
}

func toInt(v any) int {
	switch v := v.(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return -1
}

// compareWithTSC parses the source of the file name with the compiler and with ParseFile, and returns a
// description of the first difference: in acceptance (the compiler reports parse diagnostics) or in the tree.
// rejected reports that the compiler reports parse diagnostics.
func compareWithTSC(p *tscProcess, name, text string) (diff string, rejected bool, err error) {
	want, err := p.parse(name, text)
	if err != nil {
		return "", false, err
	}
	rejected = len(want.Diags) > 0
	f, perr := typescript.ParseFile(name, text, typescript.Bytes)
	switch {
	case len(want.Diags) > 0 && perr == nil:
		d := want.Diags[0]
		return fmt.Sprintf("accepted, but tsc reports at %d: %s (TS%d)", d.Pos, d.Msg, d.Code), rejected, nil
	case len(want.Diags) == 0 && perr != nil:
		var se *typescript.SyntaxError
		if errors.As(perr, &se) {
			lines := strings.Split(text, "\n")
			if se.Line-1 < len(lines) {
				line := lines[se.Line-1]
				col := min(max(se.Col-1, 0), len(line))
				return fmt.Sprintf("rejected at %d:%d: %q ⟨here⟩ %q", se.Line, se.Col, line[:col], line[col:]), rejected, nil
			}
		}
		return fmt.Sprintf("rejected: %v", perr), rejected, nil
	case perr != nil:
		return "", rejected, nil // both reject
	}
	if d := diffTrees(want.Nodes, flatten(f, utf16Offsets(text))); d != "" {
		return "tree: " + d, rejected, nil
	}
	return "", rejected, nil
}

// TestTSCSnippets compares small sources with the compiler: the parser must accept and reject what the
// compiler does, and build the same tree.
func TestTSCSnippets(t *testing.T) {
	p := startTSC(t)
	for _, src := range tscSnippets {
		name := "test.ts"
		if strings.HasPrefix(src, "//tsx\n") {
			name = "test.tsx"
		}
		d, _, err := compareWithTSC(p, name, src)
		if err != nil {
			t.Fatal(err)
		}
		if d != "" {
			t.Errorf("%q: %s", src, d)
		}
	}
}

// tscSnippets are sources for TestTSCSnippets; those starting with "//tsx\n" are .tsx files.
var tscSnippets = []string{
	`let x = 1;`,
	"var a = 1\nvar b = 2",
}

// --- The TypeScript test suite ---

// testUnit is one file of a test case: test cases may hold several files, each after a // @filename
// directive (makeUnitsFromTest in the TypeScript test harness).
type testUnit struct {
	name, content string
}

var optionRegex = regexp.MustCompile(`^//\s*@(\w+)\s*:\s*([^\r\n]*)`)

// splitTestCase splits a test case into its files, as the TypeScript test harness does: directive lines
// (// @name: value) are removed, and each // @filename directive starts a file.
func splitTestCase(fileName, code string) []testUnit {
	var lines []string
	switch {
	case strings.Contains(code, "\r\n"):
		lines = strings.Split(code, "\r\n")
	case strings.Contains(code, "\n"):
		lines = strings.Split(code, "\n")
	default:
		lines = strings.Split(code, "\r")
	}
	var units []testUnit
	var content *string
	var name string
	for _, line := range lines {
		if m := optionRegex.FindStringSubmatch(line); m != nil {
			if strings.ToLower(m[1]) != "filename" {
				continue
			}
			if name != "" {
				c := ""
				if content != nil {
					c = *content
				}
				units = append(units, testUnit{name, c})
			}
			name = strings.TrimSpace(m[2])
			content = nil
			continue
		}
		if content == nil {
			s := ""
			content = &s
		} else if *content != "" {
			*content += "\n"
		}
		*content += line
	}
	if len(units) == 0 && name == "" {
		name = filepath.Base(fileName)
	}
	c := ""
	if content != nil {
		c = *content
	}
	return append(units, testUnit{name, c})
}

// decodeTestFile decodes a test file: UTF-8, with or without a byte order mark, or UTF-16 with one.
func decodeTestFile(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return string(data[3:])
	case len(data) >= 2 && (data[0] == 0xFF && data[1] == 0xFE || data[0] == 0xFE && data[1] == 0xFF):
		be := data[0] == 0xFE
		u := make([]uint16, 0, len(data)/2)
		for i := 2; i+1 < len(data); i += 2 {
			if be {
				u = append(u, uint16(data[i])<<8|uint16(data[i+1]))
			} else {
				u = append(u, uint16(data[i+1])<<8|uint16(data[i]))
			}
		}
		return string(utf16.Decode(u))
	}
	return string(data)
}

// isTypeScriptFile reports whether the unit is a TypeScript file (.ts, .tsx, .mts, .cts, .d.ts, ...).
func isTypeScriptFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ts", ".mts", ".cts", ".tsx":
		return true
	}
	return false
}

// TestTypeScriptSuite compares the parser with the compiler on the test cases of the TypeScript repository
// (tests/cases/conformance and tests/cases/compiler): acceptance, file by file, and the trees of the files
// both accept. The known differences are listed in testdata/tsc-known-failures.txt; the test fails on any
// other difference, and on a listed one that no longer differs (run with -tsc.update to rewrite the list).
func TestTypeScriptSuite(t *testing.T) {
	cases := os.Getenv("PEGO_TYPESCRIPT_TESTS")
	if cases == "" {
		t.Skip("PEGO_TYPESCRIPT_TESTS is not set (the tests/cases directory of the TypeScript repository)")
	}
	p := startTSC(t)
	var filter *regexp.Regexp
	if *tscFilter != "" {
		filter = regexp.MustCompile(*tscFilter)
	}
	var files []string
	for _, dir := range []string{"conformance", "compiler"} {
		filepath.WalkDir(filepath.Join(cases, dir), func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if isTypeScriptFile(path) {
					files = append(files, path)
				}
			}
			return nil
		})
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatalf("no test cases in %s", cases)
	}

	// pass counts the files where the parser agrees with the compiler: both accept with the same tree, or both
	// reject (rejected: the compiler reports parse diagnostics).
	type stats struct{ files, pass, rejected, fail, skip int }
	byDir := map[string]*stats{}
	var failures []string
	start := time.Now()
	for _, path := range files {
		rel, _ := filepath.Rel(cases, path)
		rel = filepath.ToSlash(rel)
		if filter != nil && !filter.MatchString(rel) {
			continue
		}
		dir := rel[:strings.Index(rel, "/")]
		if dir == "conformance" {
			if parts := strings.SplitN(rel, "/", 3); len(parts) == 3 {
				dir += "/" + parts[1]
			}
		}
		st := byDir[dir]
		if st == nil {
			st = &stats{}
			byDir[dir] = st
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range splitTestCase(path, decodeTestFile(data)) {
			if !isTypeScriptFile(u.name) {
				st.skip++
				continue
			}
			st.files++
			d, rejected, err := compareWithTSC(p, u.name, u.content)
			if err != nil {
				t.Fatalf("%s (%s): %v", rel, u.name, err)
			}
			if d == "" {
				st.pass++
				if rejected {
					st.rejected++
				}
				continue
			}
			st.fail++
			key := rel + " " + u.name
			failures = append(failures, key)
			if *tscVerbose {
				t.Logf("%s: %s", key, d)
			}
		}
	}

	var dirs []string
	total := stats{}
	for d, st := range byDir {
		dirs = append(dirs, d)
		total.files += st.files
		total.pass += st.pass
		total.rejected += st.rejected
		total.fail += st.fail
		total.skip += st.skip
	}
	sort.Strings(dirs)
	report := func(name string, st *stats) {
		pct := 0.0
		if st.files > 0 {
			pct = 100 * float64(st.pass) / float64(st.files)
		}
		t.Logf("%-40s %6d files %6d pass (%5d rejected by both) %4d fail %5d skipped (not .ts/.tsx) %6.2f%%", name,
			st.files, st.pass, st.rejected, st.fail, st.skip, pct)
	}
	for _, d := range dirs {
		report(d, byDir[d])
	}
	report("total", &total)
	t.Logf("in %v", time.Since(start).Round(time.Millisecond))

	if filter != nil && !*tscUpdate {
		return
	}
	known, err := readKnownFailures()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if *tscUpdate {
		if err := os.WriteFile(knownFailuresFile, []byte(strings.Join(failures, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, f := range failures {
		if !slices.Contains(known, f) {
			t.Errorf("new difference: %s (run with -tsc.v to see it)", f)
		}
	}
	for _, k := range known {
		if !slices.Contains(failures, k) {
			t.Errorf("fixed: %s (run with -tsc.update to update %s)", k, knownFailuresFile)
		}
	}
}

const knownFailuresFile = "testdata/tsc-known-failures.txt"

func readKnownFailures() ([]string, error) {
	data, err := os.ReadFile(knownFailuresFile)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out, nil
}
