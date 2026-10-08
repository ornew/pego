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

func TestWithProfile(t *testing.T) {
	p, err := pego.CompileSource(`
def main = item+ $$
def item = word / num
def word = @(?a-z)+ " "?
def num = @(?0-9)+ " "?`, "main")
	if err != nil {
		t.Fatal(err)
	}
	var prof pego.Profile
	if _, err := p.Parse("ab 12 cd", pego.WithProfile(&prof)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Parse("1 x", pego.WithProfile(&prof), pego.WithBackend(pego.BytecodeIterative)); err != nil {
		t.Fatal(err)
	}
	counts := map[string]string{}
	for _, r := range prof.Rules {
		counts[r.Rule] = fmt.Sprintf("calls=%d matched=%d failed=%d consumed=%d", r.Calls, r.Matched, r.Failed, r.Consumed)
	}
	want := map[string]string{
		"main": "calls=2 matched=2 failed=0 consumed=11",
		"item": "calls=7 matched=5 failed=2 consumed=11",
		"word": "calls=7 matched=3 failed=4 consumed=6",
		"num":  "calls=4 matched=2 failed=2 consumed=5",
	}
	if fmt.Sprint(counts) != fmt.Sprint(want) || prof.Parses != 2 || len(prof.Hints()) == 0 {
		t.Errorf("profile %v (%d parses), want %v", counts, prof.Parses, want)
	}
}

// TestNilTraceOptions checks that WithTrace(nil) and WithProfile(nil) are ignored, alone and
// combined with other trace functions.
func TestNilTraceOptions(t *testing.T) {
	p, err := pego.CompileSource(`def main = @(?a-z)+ $$`, "main")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	count := pego.WithTrace(func(pego.TraceEvent) { calls++ })
	for _, opts := range [][]pego.ParseOption{
		{pego.WithTrace(nil)},
		{pego.WithProfile(nil)},
		{count, pego.WithTrace(nil), pego.WithProfile(nil)},
		{pego.WithProfile(nil), count},
	} {
		if n, err := p.Parse("abc", opts...); err != nil || n == nil {
			t.Errorf("got %v, %v", n, err)
		}
	}
	if calls != 4 {
		t.Errorf("%d events, want 4", calls)
	}
}
