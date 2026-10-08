package python_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// cpythonScript reads paths of files (or, with -s, JSON strings of source text) from standard
// input, one per line, or with -x ROOT takes the snippets of Python code in the files under ROOT:
// every string constant (they include the code that tests compile, valid or not) and every
// doctest example. It writes a JSON object for each: whether ast.parse accepts it, the SHA-256 of
// ast.dump of the tree (or the full dump with -d), the error, and the source text (as CPython
// decodes it, for files, which may be in other encodings).
const cpythonScript = `
import ast, doctest, hashlib, io, json, os, sys, tokenize, warnings
warnings.simplefilter("ignore")
full = "-d" in sys.argv

def snippets(root):
    found = set()
    parser = doctest.DocTestParser()
    for dp, dn, fn in os.walk(root):
        for f in sorted(fn):
            if not f.endswith(".py"):
                continue
            try:
                tree = ast.parse(open(os.path.join(dp, f), "rb").read())
            except Exception:
                continue
            for n in ast.walk(tree):
                if not isinstance(n, ast.Constant) or not isinstance(n.value, (str, bytes)):
                    continue
                v = n.value
                if isinstance(v, bytes):
                    try:
                        v = v.decode("utf-8")
                    except UnicodeDecodeError:
                        continue
                if not 0 < len(v) <= 5000:
                    continue
                found.add(v)
                if ">>>" in v:
                    try:
                        found.update(ex.source for ex in parser.get_examples(v))
                    except Exception:
                        pass
    out = []
    for s in sorted(found):
        try:
            s.encode("utf-8")
        except UnicodeEncodeError:
            continue
        out.append(s)
    return out

if "-x" in sys.argv:
    inputs = [(s, s) for s in snippets(sys.argv[sys.argv.index("-x") + 1])]
elif "-s" in sys.argv:
    inputs = [(json.loads(line), None) for line in sys.stdin]
    inputs = [(s, s) for s, _ in inputs]
else:
    inputs = [(open(p, "rb").read(), None) for p in (line.rstrip("\n") for line in sys.stdin)]
for data, src in inputs:
    r = {}
    try:
        tree = ast.parse(data)
        d = ast.dump(tree, include_attributes="-a" in sys.argv)
        r["ok"] = True
        if full:
            r["dump"] = d
        else:
            r["hash"] = hashlib.sha256(d.encode("utf-8", "surrogatepass")).hexdigest()
    except (SyntaxError, ValueError, MemoryError, RecursionError) as e:
        r["ok"] = False
        r["error"] = type(e).__name__ + ": " + str(e)
    if src is None:
        try:
            enc, _ = tokenize.detect_encoding(io.BytesIO(data).readline)
            src = data.decode(enc)
            if src.startswith(chr(0xfeff)):
                src = src[1:]
        except Exception as e:
            r["decode_error"] = str(e)
    r["source"] = src
    print(json.dumps(r, ensure_ascii=True))
    sys.stdout.flush()
`

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
