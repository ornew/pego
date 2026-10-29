package engine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

// failOnRead lets decode read buffered bytes but reports every attempt to
// obtain unrelated data. The parser must not use read failures as EOF.
type failOnRead struct {
	calls int
	err   error
}

func (r *failOnRead) Read([]byte) (int, error) { r.calls++; return 0, r.err }

func TestByteDecodeDoesNotReadPastCompleteRune(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"a", "é", "漢", "😀", string([]byte{0xff}), string([]byte{0xc3, '('})} {
		t.Run(fmt.Sprintf("%x", s), func(t *testing.T) {
			stop := errors.New("unrelated read")
			r := &failOnRead{err: stop}
			reader := bufio.NewReader(io.MultiReader(strings.NewReader(s), r))
			// Preload only this rune (or invalid prefix) without synthesizing EOF.
			if _, err := reader.Peek(len(s)); err != nil {
				t.Fatal(err)
			}
			in := core.Input{Unit: Bytes, Reader: reader}
			defer func() {
				if x := recover(); x != nil {
					t.Errorf("decode attempted unrelated read: %v", x)
				}
			}()
			got, size, examined, ok := in.Decode(0)
			want, wantSize := utf8.DecodeRuneInString(s)
			wantExamined := wantSize
			if want == utf8.RuneError && wantSize == 1 {
				wantExamined = len(s)
			}
			if !ok || size != wantSize || got != want || examined != wantExamined || r.calls != 0 || in.Eof {
				t.Fatalf("rune=%U size=%d ok=%v reads=%d eof=%v", got, size, ok, r.calls, in.Eof)
			}
		})
	}
}

func TestOpenPipeStreamEmitsCompleteItem(t *testing.T) {
	t.Parallel()
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, s := range []string{"a", "é", "漢", "😀", string([]byte{0xff})} {
				for split := 0; split < len(s); split++ {
					t.Run(fmt.Sprintf("%s/%s/%x/split%d", b, unit, s, split), func(t *testing.T) {
						p := mustCompile(t, "rule main = item* #stream $$\nrule item = . \"\\n\"")
						pr, pw := io.Pipe()
						emitted := make(chan string, 2)
						done := make(chan error, 1)
						go func() {
							defer pr.Close() // Unblock a writer if parsing fails before all chunks arrive.
							done <- p.ParseStreamWith("main", pr, func(n *value.Node) error { emitted <- gentest.ResultJSON(n, nil); return nil }, ParseOptions{Backend: b, Unit: unit})
						}()
						t.Cleanup(func() {
							pw.Close()
							if t.Failed() {
								pr.Close()
							}
							select {
							case err := <-done:
								if !t.Failed() && err != nil {
									t.Errorf("stream completion: %v", err)
								}
							case <-time.After(2 * time.Second):
								pr.Close()
								t.Error("stream goroutine did not stop")
							}
							pr.Close()
						})
						// Each partial UTF-8 prefix arrives separately. Comparing the
						// final tree and completion detects premature replacement decoding.
						if split > 0 {
							if _, err := io.WriteString(pw, s[:split]); err != nil {
								t.Fatal(err)
							}
						}
						if _, err := io.WriteString(pw, s[split:]+"\n"); err != nil {
							t.Fatal(err)
						}
						want := ""
						refErr := p.ParseStreamWith("main", strings.NewReader(s+"\n"), func(n *value.Node) error { want = gentest.ResultJSON(n, nil); return nil }, ParseOptions{Unit: unit})
						if refErr != nil {
							t.Fatal(refErr)
						}
						select {
						case got := <-emitted:
							if got != want {
								t.Fatalf("got %s want %s", got, want)
							}
						case <-time.After(time.Second):
							t.Fatal("complete item waited for future bytes or EOF")
						}
						if err := pw.Close(); err != nil {
							t.Fatal(err)
						}
						// Cleanup owns the done receive, including failure paths.
					})
				}
			}
		}
	}
}

func TestByteDecodeIncompletePrefixAtEOF(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "\xc3", "\xe6\xbc", "\xf0\x9f\x98"} {
		t.Run(fmt.Sprintf("%x", s), func(t *testing.T) {
			in := core.Input{Unit: Bytes, Reader: bufio.NewReader(strings.NewReader(s))}
			got, size, examined, ok := in.Decode(0)
			if s == "" {
				if ok || size != 0 || examined != 1 || !in.Eof {
					t.Fatalf("empty EOF: %U %d %d %v", got, size, examined, ok)
				}
			} else if !ok || got != utf8.RuneError || size != 1 || examined != len(s)+1 || !in.Eof {
				t.Fatalf("incomplete EOF: %U %d %d %v eof=%v", got, size, examined, ok, in.Eof)
			}
		})
	}
}

func TestStreamIncompletePrefixPropagatesReaderError(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, "rule main = item* #stream $$\nrule item = . \"\\n\"")
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(backend.String()+"/"+unit.String(), func(t *testing.T) {
				stop := errors.New("reader stopped")
				r := io.MultiReader(strings.NewReader("\xc3"), &failOnRead{err: stop})
				count := 0
				err := p.ParseStreamWith("main", r, func(*value.Node) error { count++; return nil }, ParseOptions{Backend: backend, Unit: unit})
				if !errors.Is(err, stop) || count != 0 {
					t.Fatalf("error=%v emitted=%d", err, count)
				}
			})
		}
	}
}

// BenchmarkRuneInputDelivery exercises the affected stream decoder and compares
// whole-input parsing, whose buffer already has a known end, in both units.
func BenchmarkRuneInputDelivery(b *testing.B) {
	g, err := parseTest("rule main = item* #stream $$\nrule item = . \"\\n\"")
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, compile.Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct{ name, item string }{{"ASCII", "a\n"}, {"Unicode", "😀\n"}} {
		text := strings.Repeat(tc.item, 1000)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if _, err := prog.rule(backend, "main"); err != nil {
				b.Fatal(err)
			}
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, stream := range []bool{true, false} {
					mode := "Batch"
					if stream {
						mode = "Stream"
					}
					b.Run(tc.name+"/"+backend.String()+"/"+unit.String()+"/"+mode, func(b *testing.B) {
						b.SetBytes(int64(len(text)))
						b.ReportAllocs()
						opts := ParseOptions{Backend: backend, Unit: unit}
						for b.Loop() {
							if stream {
								count := 0
								err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { count++; return nil }, opts)
								if err != nil || count != 1000 {
									b.Fatalf("emitted %d: %v", count, err)
								}
							} else {
								if _, err := prog.ParseWith("main", text, opts); err != nil {
									b.Fatal(err)
								}
							}
						}
					})
				}
			}
		}
	}
}
