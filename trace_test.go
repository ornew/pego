package pego_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func TestWithTrace(t *testing.T) {
	p, err := pego.CompileSource(`
def main = pair (";" pair)* $$
def pair = key "=" value
def key = @(?a-z)+
def value = @(?0-9)+`, "main")
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	var exits int
	_, err = p.Parse("a=1;b=x", pego.WithTrace(func(e pego.TraceEvent) {
		if e.Kind == pego.TraceEnter {
			line, col := e.LineCol(e.Pos)
			calls = append(calls, fmt.Sprintf("%s%s@%d:%d", strings.Repeat(" ", e.Depth-1), e.Rule, line, col))
			return
		}
		if !e.Matched && e.Rule == "value" {
			calls = append(calls, "value failed: "+e.Failure().Error())
		}
	}), pego.WithTrace(func(e pego.TraceEvent) {
		if e.Kind == pego.TraceExit {
			exits++
		}
	}))
	if err == nil || err.Error() != "1:7: syntax error: expected (?0-9)" {
		t.Errorf("error %v", err)
	}
	want := []string{
		"main@1:1",
		" pair@1:1", "  key@1:1", "  value@1:3",
		" pair@1:5", "  key@1:5", "  value@1:7",
		"value failed: 1:7: syntax error: expected (?0-9)",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	if exits != len(want)-1 {
		t.Errorf("%d exits, want %d", exits, len(want)-1)
	}
}
