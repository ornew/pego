package python_test

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/python"
)

// The tests in this file compare the parser with CPython 3.14, which they run as a subprocess:
// the interpreter named by PEGO_PYTHON, or python3.14 on PATH. They are skipped without it.
// PEGO_CPYTHON_SRC names a checkout of CPython 3.14.0 whose Lib/test is used as a corpus too.

// python returns the path of CPython 3.14, or skips the test.
func findPython(t testing.TB) string {
	t.Helper()
	py := os.Getenv("PEGO_PYTHON")
	if py == "" {
		var err error
		if py, err = exec.LookPath("python3.14"); err != nil {
			t.Skip("CPython 3.14 not found (set PEGO_PYTHON)")
		}
	}
	out, err := exec.Command(py, "-I", "-c", "import sys; print(sys.version_info[:2] == (3, 14))").Output()
	if err != nil || strings.TrimSpace(string(out)) != "True" {
		t.Skipf("%s is not CPython 3.14 (%v)", py, err)
	}
	return py
}

// cpythonScript is the program that runs CPython (see its header).
//
//go:embed internal/refgen/refgen.py
var cpythonScript string

type cpythonResult struct {
	OK          bool   `json:"ok"`
	Hash        string `json:"hash"`
	Dump        string `json:"dump"`
	Error       string `json:"error"`
	Source      string `json:"source"`
	DecodeError string `json:"decode_error"`
}

// runCPython runs cpythonScript on the inputs.
func runCPython(t testing.TB, py string, inputs []string, args ...string) []cpythonResult {
	t.Helper()
	cmd := exec.Command(py, append([]string{"-I", "-c", cpythonScript}, args...)...)
	cmd.Stdin = strings.NewReader(strings.Join(inputs, "\n") + "\n")
	if len(inputs) == 0 {
		cmd.Stdin = nil
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var results []cpythonResult
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 1<<30)
	for sc.Scan() {
		var r cpythonResult
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		results = append(results, r)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if len(inputs) > 0 && len(results) != len(inputs) {
		t.Fatalf("CPython returned %d results for %d inputs", len(results), len(inputs))
	}
	return results
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// corpus returns the .py files of the CPython standard library and of CPython's Lib/test.
func corpus(t *testing.T, py string) []string {
	out, err := exec.Command(py, "-I", "-c", "import sysconfig; print(sysconfig.get_paths()['stdlib'])").Output()
	if err != nil {
		t.Fatal(err)
	}
	dirs := []string{strings.TrimSpace(string(out))}
	if src := os.Getenv("PEGO_CPYTHON_SRC"); src != "" {
		dirs = append(dirs, filepath.Join(src, "Lib", "test"))
	}
	var files []string
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".py") {
				files = append(files, path)
			}
			return nil
		})
	}
	sort.Strings(files)
	return files
}

// TestCPythonCorpus parses every file of the corpus and compares with CPython: the same files
// must be accepted, with the same ast.dump.
func TestCPythonCorpus(t *testing.T) {
	py := findPython(t)
	files := corpus(t, py)
	if only := os.Getenv("PEGO_PYTHON_ONLY"); only != "" {
		var sel []string
		for _, f := range files {
			if strings.Contains(f, only) {
				sel = append(sel, f)
			}
		}
		files = sel
	}
	results := runCPython(t, py, files)
	var accepted, rejected, same, wrongAccept, wrongReject, diff int
	var diffs []string
	for i, path := range files {
		r := results[i]
		if r.DecodeError != "" {
			if r.OK {
				t.Errorf("%s: CPython accepts it but cannot decode it: %s", path, r.DecodeError)
			}
			rejected++
			continue
		}
		m, err := python.ParseModule(r.Source)
		switch {
		case r.OK && err != nil:
			wrongReject++
			t.Errorf("%s: rejected: %v", path, firstLine(err))
			continue
		case !r.OK && err == nil:
			wrongAccept++
			t.Errorf("%s: accepted, CPython: %s", path, r.Error)
			continue
		case !r.OK:
			rejected++
			continue
		}
		accepted++
		if sha(python.Dump(m)) != r.Hash {
			diff++
			diffs = append(diffs, path)
			continue
		}
		same++
	}
	for _, path := range diffs[:min(len(diffs), 10)] {
		reportDiff(t, py, path)
	}
	t.Logf("%d files: %d accepted by both (%d with the same ast.dump, %d different), %d rejected by both, %d accepted only by CPython, %d accepted only here",
		len(files), accepted, same, diff, rejected, wrongReject, wrongAccept)
	if diff > 0 {
		t.Errorf("%d files have a different ast.dump", diff)
	}
}

// TestCPythonSnippets compares the parser with CPython on the snippets of code in the strings and
// doctests of CPython's Lib/test (PEGO_CPYTHON_SRC): tens of thousands of programs, most of them
// small and many of them invalid, written to test the compiler. The same snippets must be
// accepted, with the same ast.dump.
func TestCPythonSnippets(t *testing.T) {
	py := findPython(t)
	src := os.Getenv("PEGO_CPYTHON_SRC")
	if src == "" {
		t.Skip("set PEGO_CPYTHON_SRC to a checkout of CPython 3.14")
	}
	results := runCPython(t, py, nil, "-x", filepath.Join(src, "Lib", "test"))
	var same, rejected, byCheck int
	var bad []string
	for _, r := range results {
		m, err := python.ParseModule(r.Source)
		_, aerr := python.ParseAST(r.Source)
		if rerr := python.Recognize(r.Source); (rerr == nil) != (aerr == nil) {
			bad = append(bad, fmt.Sprintf("%q: ParseAST: %v, Recognize: %v", r.Source, aerr, rerr))
		}
		if aerr == nil && err != nil {
			byCheck++ // rejected only by Check
		}
		switch {
		case r.OK && err != nil:
			bad = append(bad, fmt.Sprintf("rejected %q: %v", r.Source, firstLine(err)))
		case !r.OK && err == nil:
			bad = append(bad, fmt.Sprintf("accepted %q; CPython: %s", r.Source, r.Error))
		case !r.OK:
			rejected++
		case sha(python.Dump(m)) != r.Hash:
			bad = append(bad, fmt.Sprintf("different ast.dump for %q", r.Source))
		default:
			same++
		}
	}
	sort.Strings(bad)
	for _, b := range bad[:min(len(bad), 40)] {
		t.Error(b)
	}
	t.Logf("%d snippets: %d accepted by both with the same ast.dump, %d rejected by both (%d of them only by Check), %d differ",
		len(results), same, rejected, byCheck, len(bad))
}

// reportDiff shows where the dump of a file differs from CPython's.
func reportDiff(t *testing.T, py, path string) {
	r := runCPython(t, py, []string{path}, "-d")[0]
	m, err := python.ParseModule(r.Source)
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return
	}
	got := python.Dump(m)
	t.Errorf("%s: ast.dump differs:\n%s", path, firstDiff(got, r.Dump))
}

// TestCPythonPositions compares the positions of the nodes (ast.dump with include_attributes=True)
// with CPython's, on the files of the corpus and on the snippets of Lib/test.
func TestCPythonPositions(t *testing.T) {
	py := findPython(t)
	files := corpus(t, py)
	results := runCPython(t, py, files, "-a")
	if src := os.Getenv("PEGO_CPYTHON_SRC"); src != "" {
		results = append(results, runCPython(t, py, nil, "-a", "-x", filepath.Join(src, "Lib", "test"))...)
	}
	var same, diff int
	var bad []string
	for i, r := range results {
		if !r.OK {
			continue
		}
		m, err := python.ParseModule(r.Source)
		if err != nil {
			continue // reported by the other tests
		}
		if sha(python.DumpWithPositions(m, r.Source)) == r.Hash {
			same++
			continue
		}
		diff++
		name := fmt.Sprintf("snippet %.60q", r.Source)
		if i < len(files) {
			name = files[i]
		}
		if len(bad) < 20 {
			var rr cpythonResult
			if i < len(files) {
				rr = runCPython(t, py, []string{files[i]}, "-a", "-d")[0]
			} else {
				js, _ := json.Marshal(r.Source)
				rr = runCPython(t, py, []string{string(js)}, "-a", "-d", "-s")[0]
			}
			bad = append(bad, name+":\n"+firstDiff(python.DumpWithPositions(m, r.Source), rr.Dump))
		}
	}
	for _, b := range bad {
		t.Error(b)
	}
	t.Logf("%d sources accepted by both: %d with the same positions, %d different", same+diff, same, diff)
}

func firstDiff(got, want string) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	lo := max(0, i-150)
	return "got:  ..." + got[lo:min(len(got), i+150)] + "\nwant: ..." + want[lo:min(len(want), i+150)]
}

func firstLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	var se *python.SyntaxError
	if errors.As(err, &se) {
		return s
	}
	return s
}

// TestCPythonMutations compares the parser with CPython on random mutations (deleted, inserted,
// replaced and swapped characters and tokens) of the snippets of the vendored reference data that
// both accept: inputs near the border of the language, where grammars differ. The same mutants
// must be accepted, with the same ast.dump.
func TestCPythonMutations(t *testing.T) {
	py := findPython(t)
	var seeds []string
	for _, r := range readReferences(t, "testdata/cpython-snippets.jsonl.gz") {
		if r.OK && len(r.Source) <= 400 {
			seeds = append(seeds, r.Source)
		}
	}
	n := 60000
	if testing.Short() {
		n = 6000
	}
	if v, err := strconv.Atoi(os.Getenv("PEGO_MUTANTS")); err == nil {
		n = v
	}
	pieces := []string{"(", ")", "[", "]", "{", "}", ":", ":=", ",", ";", ".", "...", "=", "==", "*", "**", "->", "@", "\n", "\n    ", "\t",
		" ", "\\\n", "#", "'", "\"", "'''", "f'", "f\"{", "}", "{", "!r", "lambda ", "async ", "await ", "yield ", "not ", "in ", "if ",
		"else ", "for ", "def ", "class ", "match ", "case ", "type ", "except ", "*", "0x", "1_", "e5", "j", "u", "b'", "r'", "ä", "\x0c"}
	seed, _ := strconv.Atoi(os.Getenv("PEGO_MUTANTS_SEED"))
	rng := rand.New(rand.NewPCG(7, uint64(11+seed)))
	mutants := make([]string, 0, n)
	for len(mutants) < n {
		rs := []rune(seeds[rng.IntN(len(seeds))])
		for k := 1 + rng.IntN(2); k > 0 && len(rs) > 0; k-- {
			i := rng.IntN(len(rs))
			switch rng.IntN(4) {
			case 0:
				rs = append(rs[:i], rs[i+1:]...)
			case 1:
				rs = slices.Insert(rs, i, []rune(pieces[rng.IntN(len(pieces))])...)
			case 2:
				rs[i] = []rune(pieces[rng.IntN(len(pieces))])[0]
			default:
				j := rng.IntN(len(rs))
				rs[i], rs[j] = rs[j], rs[i]
			}
		}
		mutants = append(mutants, string(rs))
	}
	var inputs []string
	for _, m := range mutants {
		js, _ := json.Marshal(m)
		inputs = append(inputs, string(js))
	}
	results := runCPython(t, py, inputs, "-s")
	var same, rejected, bad int
	for i, r := range results {
		m, err := python.ParseModule(mutants[i])
		var msg string
		switch {
		case r.OK && err != nil:
			msg = fmt.Sprintf("rejected %q: %v", mutants[i], firstLine(err))
		case !r.OK && err == nil:
			msg = fmt.Sprintf("accepted %q; CPython: %s", mutants[i], r.Error)
		case !r.OK:
			rejected++
		case sha(python.Dump(m)) != r.Hash:
			msg = fmt.Sprintf("different ast.dump for %q", mutants[i])
		default:
			same++
		}
		if msg != "" {
			if bad++; bad <= 30 {
				t.Error(msg)
			}
		}
	}
	t.Logf("%d mutants: %d accepted by both with the same ast.dump, %d rejected by both, %d differ", len(results), same, rejected, bad)
}

// TestCPythonLines compares the parser with CPython on random sequences of the pieces that make up
// the lines of a program: names, brackets, colons, comments, tabs, form feeds, backslash
// continuations and line ends of every kind. They exercise the tokenizer's rules about
// indentation, blank lines and the end of the input, which a grammar without a tokenizer
// reproduces with rules of its own.
func TestCPythonLines(t *testing.T) {
	py := findPython(t)
	pieces := []string{"a", "b = 1", "if x:", "else:", "pass", "def f():", "class C:", "(", ")", "[", "]", ":", ";", ",", "#c", "'s'", "'''", "\"\"\"",
		"\n", "\n", "\n", "\r\n", "\r", "\\\n", "\\", " ", " ", "  ", "    ", "\t", "\x0c", "\\\r\n", "lambda", "x if y else z", "f'{", "}", "async", "match x:", "case 1:"}
	n := 100000
	if testing.Short() {
		n = 10000
	}
	if v, err := strconv.Atoi(os.Getenv("PEGO_MUTANTS")); err == nil {
		n = v
	}
	seed, _ := strconv.Atoi(os.Getenv("PEGO_MUTANTS_SEED"))
	rng := rand.New(rand.NewPCG(13, uint64(17+seed)))
	var progs, inputs []string
	for len(progs) < n {
		var b strings.Builder
		for k := 2 + rng.IntN(9); k > 0; k-- {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		progs = append(progs, b.String())
		js, _ := json.Marshal(b.String())
		inputs = append(inputs, string(js))
	}
	results := runCPython(t, py, inputs, "-s")
	var same, rejected, bad int
	for i, r := range results {
		m, err := python.ParseModule(progs[i])
		var msg string
		switch {
		case r.OK && err != nil:
			msg = fmt.Sprintf("rejected %q: %v", progs[i], firstLine(err))
		case !r.OK && err == nil:
			msg = fmt.Sprintf("accepted %q; CPython: %s", progs[i], r.Error)
		case !r.OK:
			rejected++
		case sha(python.Dump(m)) != r.Hash:
			msg = fmt.Sprintf("different ast.dump for %q", progs[i])
		default:
			same++
		}
		if msg != "" {
			if bad++; bad <= 30 {
				t.Error(msg)
			}
		}
	}
	t.Logf("%d programs: %d accepted by both with the same ast.dump, %d rejected by both, %d differ", len(results), same, rejected, bad)
}
