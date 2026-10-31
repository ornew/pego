package gogen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// The default budget of a generated stream follows the furthest position the parser demands, so that
// the same input gives the same result, or the same limit, spend and request, whatever the chunks of
// the reader.
func TestGeneratedStreamDefaultBudgetIgnoresChunking(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds a generated standalone stream consumer")
	}
	g, err := parseTest(`rule main = ^^ header items:record* #stream $$
rule header = "#records\n"
rule record = ^ ([a-z] &mid)* "\n"
rule mid = ([a-z] &[a-z]*)*`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "fixture", Start: "main", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":         gentest.GoMod("fixture"),
		"parser.go":      string(code),
		"parser_test.go": generatedStreamDefaultBudgetConsumer,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

const generatedStreamDefaultBudgetConsumer = `package fixture

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type chunkReader struct {
	s    string
	size int
}

func (r *chunkReader) Read(b []byte) (int, error) {
	if r.s == "" {
		return 0, errEOF
	}
	n := min(len(r.s), r.size, len(b))
	copy(b, r.s[:n])
	r.s = r.s[n:]
	return n, nil
}

var errEOF = fmt.Errorf("EOF")

// cleanReader ends the stream with io.EOF, so that a completed parse returns nil.
type cleanReader struct{ chunkReader }

func (r *cleanReader) Read(b []byte) (int, error) {
	n, err := r.chunkReader.Read(b)
	if err == errEOF {
		err = io.EOF
	}
	return n, err
}

func TestDefaultBudgetIgnoresChunking(t *testing.T) {
	cases := []struct {
		name, text string
		limits     Limits
		limit      uint64
	}{
		{"completes", "#records\n" + strings.Repeat("abc\n", 500), Limits{}, 0},
		{"exceeds the floor", "#records\n" + strings.Repeat("a", 300) + "\n", Limits{}, 1 << 20},
		{"exceeds a grown default", "#records\n" + strings.Repeat("a", 3000) + "\n", Limits{}, 3010 * 1024},
		{"exceeds an explicit limit", "#records\nabc\n", Limits{Budget: 20}, 20},
	}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, c := range cases {
			var want string
			for _, size := range []int{1, 7, len(c.text)} {
				n := 0
				r := &chunkReader{s: c.text, size: size}
				err := ParseStream(r, func(*Node) error { n++; return nil }, WithUnit(unit), WithLimits(c.limits))
				var le *LimitError
				isLimit := errors.As(err, &le)
				if isLimit != (c.limit != 0) {
					t.Fatalf("%s/%d/%d: error %v", c.name, unit, size, err)
				}
				got := fmt.Sprint(n, " ", err)
				if want == "" {
					want = got
				} else if got != want {
					t.Errorf("%s/%d: chunk size %d gives %q, chunk size 1 gives %q", c.name, unit, size, got, want)
				}
				if isLimit && le.Limit != c.limit {
					t.Errorf("%s/%d: limit %d, want %d", c.name, unit, le.Limit, c.limit)
				}
			}
		}
	}
}

// A stream fails when the parser needs a unit beyond MaxInput and the stream has one, whatever the
// chunks of the reader; a stream of exactly MaxInput units completes.
func TestMaxInputIgnoresChunking(t *testing.T) {
	text := "#records\n" + strings.Repeat("abc\n", 50)
	n := uint64(len(text))
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, max := range []uint64{0, Unlimited, n, n + 1} {
			for _, size := range []int{1, 7, len(text)} {
				err := ParseStream(&cleanReader{chunkReader{s: text, size: size}}, func(*Node) error { return nil }, WithUnit(unit), WithLimits(Limits{MaxInput: max}))
				if err != nil {
					t.Errorf("%d/MaxInput %d/%d: %v", unit, max, size, err)
				}
			}
		}
		var want string
		for _, size := range []int{1, 7, len(text)} {
			count := 0
			err := ParseStream(&cleanReader{chunkReader{s: text, size: size}}, func(*Node) error { count++; return nil }, WithUnit(unit), WithLimits(Limits{MaxInput: n - 1}))
			var le *LimitError
			if !errors.As(err, &le) || le.Kind != LimitInput || le.Limit != n-1 || le.Requested != n {
				t.Fatalf("%d/%d: error %v", unit, size, err)
			}
			got := fmt.Sprint(count, " ", err)
			if want == "" {
				want = got
			} else if got != want {
				t.Errorf("%d: chunk size %d gives %q, chunk size 1 gives %q", unit, size, got, want)
			}
		}
	}
}
`
