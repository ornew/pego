package python_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// cpythonScript reads paths (or, with -s, Python string literals) from standard input, one per
// line, and writes a JSON object per input: whether ast.parse accepts it, the SHA-256 of
// ast.dump of the tree (or the full dump with -d), the error, and the source text as CPython
// decodes it (files may be in other encodings).
const cpythonScript = `
import ast, hashlib, json, sys, tokenize, io, warnings
warnings.simplefilter("ignore")
full = "-d" in sys.argv
snippets = "-s" in sys.argv
for line in sys.stdin:
    line = line.rstrip("\n")
    if snippets:
        src = ast.literal_eval(line)
        data = src
    else:
        data = open(line, "rb").read()
        src = None
    r = {}
    try:
        tree = ast.parse(data)
        d = ast.dump(tree)
        r["ok"] = True
        if full:
            r["dump"] = d
        else:
            r["hash"] = hashlib.sha256(d.encode("utf-8", "surrogatepass")).hexdigest()
    except (SyntaxError, ValueError, MemoryError, RecursionError) as e:
        r["ok"] = False
        r["error"] = type(e).__name__ + ": " + str(e)
    if not snippets:
        try:
            enc, _ = tokenize.detect_encoding(io.BytesIO(data).readline)
            src = data.decode(enc)
            if src.startswith(chr(0xfeff)):
                src = src[1:]
            r["source"] = src
        except Exception as e:
            r["decode_error"] = str(e)
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
	if len(results) != len(inputs) {
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
