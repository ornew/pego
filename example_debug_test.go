package pego_test

import (
	"fmt"
	"strings"

	"github.com/ornew/pego"
)

func ExampleWithTrace() {
	p, err := pego.CompileSource(`
def main = pair (";" pair)* $$
def pair = key "=" value
def key = @(?a-z)+
def value = @(?0-9)+`, "main")
	if err != nil {
		panic(err)
	}
	_, err = p.Parse("a=1;b=x", pego.WithTrace(func(e pego.TraceEvent) {
		indent := strings.Repeat("  ", e.Depth-1)
		line, col := e.LineCol(e.Pos)
		switch {
		case e.Kind == pego.TraceEnter:
			fmt.Printf("%s%s at %d:%d\n", indent, e.Rule, line, col)
		case e.Matched:
			fmt.Printf("%s%s matched %q\n", indent, e.Rule, e.Text())
		default:
			fmt.Printf("%s%s failed: %v\n", indent, e.Rule, e.Failure())
		}
	}))
	fmt.Println(err)
	// Output:
	// main at 1:1
	//   pair at 1:1
	//     key at 1:1
	//     key matched "a"
	//     value at 1:3
	//     value matched "1"
	//   pair matched "a=1"
	//   pair at 1:5
	//     key at 1:5
	//     key matched "b"
	//     value at 1:7
	//     value failed: 1:7: syntax error: expected (?0-9)
	//   pair failed: 1:7: syntax error: expected (?0-9)
	// main failed: 1:7: syntax error: expected (?0-9)
	// 1:7: syntax error: expected (?0-9)
}

func ExampleWithProfile() {
	p, err := pego.CompileSource(`
def main = (stmt ";")* $$
def stmt = name "=" num / name "+=" num
def name = @(?a-z)+
def num = @(?0-9)+`, "main")
	if err != nil {
		panic(err)
	}
	var prof pego.Profile
	for _, input := range []string{"a=1;b+=2;", "c+=3;"} {
		if _, err := p.Parse(input, pego.WithProfile(&prof)); err != nil {
			panic(err)
		}
	}
	fmt.Printf("%d parses, %d calls\n", prof.Parses, prof.Calls)
	for _, r := range prof.Rules {
		fmt.Printf("%-5s calls=%d matched=%d failed=%d repeats=%d consumed=%d\n", r.Rule, r.Calls, r.Matched, r.Failed, r.Repeats, r.Consumed)
	}
	// Output:
	// 2 parses, 19 calls
	// main  calls=2 matched=2 failed=0 repeats=0 consumed=14
	// stmt  calls=5 matched=3 failed=2 repeats=0 consumed=11
	// name  calls=9 matched=5 failed=4 repeats=4 consumed=5
	// num   calls=3 matched=3 failed=0 repeats=0 consumed=3
}
