package engine

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

// decodedBoundaries is an independent oracle for edit validation: invalid
// bytes each decode as a one-byte RuneError, just as they do when parsing.
func decodedBoundaries(s string) []int {
	positions := []int{0}
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		positions = append(positions, i)
	}
	return positions
}

func TestInvalidUTF8EditBoundaries(t *testing.T) {
	corpus := []string{"", "a", "é", "日本", "😀", "\x80", "\xc3", "\xc3\x80", "\xc3x\x80", "\xe0\x80\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xf0\x9f\x98", "a\x80é\xff日"}
	rng := rand.New(rand.NewSource(15))
	for i := 0; i < 1000; i++ {
		bs := make([]byte, rng.Intn(24))
		rng.Read(bs)
		corpus = append(corpus, string(bs))
	}
	for _, s := range corpus {
		bounds := decodedBoundaries(s)
		want := make(map[int]bool, len(bounds))
		for _, pos := range bounds {
			want[pos] = true
		}
		for pos := 0; pos <= len(s); pos++ {
			if got := validBoundary(s, Bytes, pos) == nil; got != want[pos] {
				t.Fatalf("input %x position %d: valid=%v, want %v", s, pos, got, want[pos])
			}
		}
	}
}

func TestDocumentCompletesUTF8Prefix(t *testing.T) {
	p := compile(t, "def main=x $$\ndef x=.")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, s := range []string{"\xc3", "\xe6\xbc", "\xf0\x9f\x98"} {
			t.Run(backend.String()+fmt.Sprintf("/%x", s), func(t *testing.T) {
				d, err := p.NewDocumentWith("main", s, ParseOptions{Backend: backend, Unit: Bytes})
				if err != nil {
					t.Fatal(err)
				}
				d.Parse() // Both successful and failed prefix results must be invalidated.
				suffix := map[string]string{"\xc3": "\xa9", "\xe6\xbc": "\xa2", "\xf0\x9f\x98": "\x80"}[s]
				if err := d.Edit(len(s), len(s), suffix); err != nil {
					t.Fatal(err)
				}
				got, err := d.Parse()
				want, werr := p.ParseWith("main", s+suffix, ParseOptions{Backend: backend, Unit: Bytes})
				if err != nil || werr != nil || resultJSON(got, err) != resultJSON(want, werr) {
					t.Fatalf("completed prefix: got %s, want %s", resultJSON(got, err), resultJSON(want, werr))
				}
			})
		}
	}
}

func TestDocumentInvalidUTF8EditEquivalence(t *testing.T) {
	pieces := []string{"", "a", "\n", "é", "日", "😀", "\x80", "\xa9", "\xc3", "\xe6\xbc", "\xf0\x9f\x98", "\xff", "\xed\xa0\x80"}
	grammars := []string{"def main=item* $$\ndef item=.", "def main=item* $$\ndef item=\"é\" / \"日\" / \"😀\" / \"\\uFFFD\"", "def main=item* $$\ndef item=(?é日😀\\uFFFD)", "def main=x $$\ndef x=."}
	for gi, grammar := range grammars {
		p := compile(t, grammar)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{Bytes, CodePoints} {
				t.Run(fmt.Sprintf("%d/%s/%s", gi, backend, unit), func(t *testing.T) {
					d, err := p.NewDocumentWith("main", "\x80\xc3", ParseOptions{Backend: backend, Unit: unit})
					if err != nil {
						t.Fatal(err)
					}
					rng := rand.New(rand.NewSource(15))
					for i := 0; i < 500; i++ {
						got, err := d.Parse()
						want, werr := p.ParseWith("main", d.Text(), ParseOptions{Backend: backend, Unit: unit})
						if resultJSON(got, err) != resultJSON(want, werr) || fmt.Sprint(got) != fmt.Sprint(want) {
							t.Fatalf("edit %d text %x: got %s, want %s", i, d.Text(), resultJSON(got, err), resultJSON(want, werr))
						}
						bounds := decodedBoundaries(d.Text())
						if unit == CodePoints {
							for j := range bounds {
								bounds[j] = j
							}
						}
						start := rng.Intn(len(bounds))
						end := start + rng.Intn(len(bounds)-start)
						if err := d.Edit(bounds[start], bounds[end], pieces[rng.Intn(len(pieces))]); err != nil {
							t.Fatalf("edit %d [%d,%d): %v", i, bounds[start], bounds[end], err)
						}
					}
				})
			}
		}
	}
}

func TestDocumentResumesCompletedUTF8Prefix(t *testing.T) {
	p := compile(t, "def main=item* $$\ndef item=.")
	prefix := strings.Repeat("a", 1000)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			d, err := p.NewDocumentWith("main", prefix+"\xc3", ParseOptions{Backend: backend, Unit: Bytes})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Parse(); err != nil {
				t.Fatal(err)
			}
			if len(d.runs) == 0 {
				t.Fatal("repetition was not recorded")
			}
			if err := d.Edit(len(prefix)+1, len(prefix)+1, "\xa9"); err != nil {
				t.Fatal(err)
			}
			got, err := d.Parse()
			want, werr := p.ParseWith("main", prefix+"é", ParseOptions{Backend: backend, Unit: Bytes})
			if err != nil || resultJSON(got, err) != resultJSON(want, werr) {
				t.Fatalf("resumed parse differs from fresh parse (errors %v / %v)", err, werr)
			}
			if d.resumed < len(prefix)-10 {
				t.Fatalf("unaffected prefix was not resumed: %d elements", d.resumed)
			}
		})
	}
}

func BenchmarkDecodedEditBoundary(b *testing.B) {
	for _, s := range []string{strings.Repeat("a", 100_000), strings.Repeat("é", 50_000), strings.Repeat("😀", 25_000)} {
		b.Run(fmt.Sprintf("%x", s[:min(len(s), 4)]), func(b *testing.B) {
			pos := len(s) - len(string([]rune(s)[0]))
			b.ReportAllocs()
			for b.Loop() {
				if err := validBoundary(s, Bytes, pos); err != nil {
					b.Fatal(err)
				}
			}
		})
		if len(string([]rune(s)[0])) > 1 {
			b.Run(fmt.Sprintf("%x/interior", s[:min(len(s), 4)]), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if err := validBoundary(s, Bytes, len(s)-1); err == nil {
						b.Fatal("accepted interior position")
					}
				}
			})
		}
	}
}

func BenchmarkByteDecodeDependency(b *testing.B) {
	for _, s := range []string{"a", "é", "日", "😀", "\x80", "\xc3", "\xe6\xbc", "\xf0\x9f\x98"} {
		b.Run(fmt.Sprintf("%x", s), func(b *testing.B) {
			in := newInput(s, Bytes)
			b.ReportAllocs()
			for b.Loop() {
				if _, size, _, ok := in.decode(0); !ok || size < 1 {
					b.Fatal("failed decoding")
				}
			}
		})
	}
}
