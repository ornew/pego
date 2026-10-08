package golang_test

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// snippets are inserted into source files by the mutation tests: tokens, keywords, comments,
// malformed literals and characters that go/scanner rejects.
var snippets = []string{
	"(", ")", "{", "}", "[", "]", ",", ";", "\n", ":", ":=", "=", "==", ".", "...", "*", "&", "<-", "~", "|",
	"+", "-", "++", "--", "!", "^", "&^", "<<", "->", "=>",
	"chan", "func", "type", "struct", "interface", "map", "go", "defer", "range", "case", "default", "if",
	"else", "for", "switch", "select", "return", "break", "goto", "fallthrough", "var", "const", "import",
	"package", "x", "_", "T[int]", "x.(type)", "[]int{}", "chan<-", "<-chan", "func()", "(x)",
	"0", "1.5", "0x1p-2", "1e3i", "'a'", "'\\n'", `"s"`, "`r`", "/*c*/", "//c\n", "/*\n*/",
	"0x", "1e", "0b2", "08", "1__0", "'ab'", `"\q"`, "'\\400'", `"\uD800"`, "`", `"`, "'",
	"@", "#", "$", "\x00", "\xff", "\xef\xbb\xbf", "\xc3\xa9", "\xd9\xa3", "\xc2\xb7", "\xef\xbf\xbd", "\r", "\t", "\v", "\f",
	"//line x:0\n", "/*line :1:0*/", "//line x:7\n",
}

// mutate applies one random change to src.
func mutate(r *rand.Rand, src string) string {
	if len(src) == 0 {
		return snippets[r.IntN(len(snippets))]
	}
	i := r.IntN(len(src) + 1)
	switch r.IntN(6) {
	case 0, 1: // insert a snippet
		return src[:i] + snippets[r.IntN(len(snippets))] + src[i:]
	case 2: // delete a few bytes
		j := min(len(src), i+1+r.IntN(8))
		return src[:i] + src[j:]
	case 3: // replace a few bytes with a snippet
		j := min(len(src), i+1+r.IntN(4))
		return src[:i] + snippets[r.IntN(len(snippets))] + src[j:]
	case 4: // truncate
		return src[:i]
	default: // duplicate a line
		start := strings.LastIndexByte(src[:i], '\n') + 1
		end := strings.IndexByte(src[i:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += i + 1
		}
		return src[:end] + src[start:end] + src[end:]
	}
}

// TestMutations compares the package with go/parser on mutated copies of files of GOROOT/src:
// most are rejected, which checks that the grammar rejects what go/parser rejects.
func TestMutations(t *testing.T) {
	files := corpus(t)
	n, per := 600, 12
	if testing.Short() {
		n = 150
	}
	if v := os.Getenv("PEGO_GO_MUTATIONS"); v != "" {
		fmt.Sscan(v, &n)
	}
	r := rand.New(rand.NewPCG(1, 2))
	type job struct {
		name string
		src  string
	}
	var jobs []job
	for k := range n {
		path := files[r.IntN(len(files))]
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 64<<10 {
			continue
		}
		src := string(data)
		for m := range per {
			s := src
			for range 1 + r.IntN(2) {
				s = mutate(r, s)
			}
			jobs = append(jobs, job{fmt.Sprintf("%s#%d.%d", path, k, m), s})
		}
	}
	var mu sync.Mutex
	var failures []string
	var accepted int
	work := make(chan job)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				d := compare(j.name, []byte(j.src), 0)
				ok := goparserAccepts(j.src)
				mu.Lock()
				if d != "" {
					failures = append(failures, fmt.Sprintf("%s: %s\n%s", j.name, d, excerpt(j.src, d)))
				}
				if ok {
					accepted++
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		work <- j
	}
	close(work)
	wg.Wait()
	sort.Strings(failures)
	for i, f := range failures {
		if i == 20 {
			t.Errorf("... and %d more", len(failures)-20)
			break
		}
		t.Error(f)
	}
	t.Logf("%d mutated files (%d accepted by go/parser), %d differ", len(jobs), accepted, len(failures))
}

// excerpt writes the input of a failure to the directory $PEGO_GO_DUMP, if set, and returns where.
func excerpt(src, d string) string {
	dir := os.Getenv("PEGO_GO_DUMP")
	if dir == "" {
		return ""
	}
	f, err := os.CreateTemp(dir, "case*.go")
	if err != nil {
		return err.Error()
	}
	defer f.Close()
	f.WriteString(src)
	return "\tinput: " + filepath.Base(f.Name())
}

func goparserAccepts(src string) bool {
	_, err := goparser.ParseFile(token.NewFileSet(), "", src, goparser.SkipObjectResolution)
	return err == nil
}
