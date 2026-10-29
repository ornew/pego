package document_test

import (
	"strings"
	"testing"

	. "github.com/ornew/pego/internal/engine"
)

func TestDocumentResumesCompletedUTF8Prefix(t *testing.T) {
	p := mustCompile(t, "rule main = item* $$\nrule item = .")
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
			if len(d.Runs()) == 0 {
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
			if d.Resumed() < len(prefix)-10 {
				t.Fatalf("unaffected prefix was not resumed: %d elements", d.Resumed())
			}
		})
	}
}
