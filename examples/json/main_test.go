package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/examples/json/generated"
)

// TestMatchesEncodingJSON compares the results of json.pego with encoding/json.
func TestDecodeMatchesEncodingJSON(t *testing.T) {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{
		`null`, `true`, `false`, `0`, `-12.5e+3`, `"a\"b\\c\/dé\n"`,
		`[]`, `{}`, `[1, [2, [3]], {"k": []}]`,
		`{"name": "pego", "tags": ["peg", "pratt"], "ok": true, "n": null, "v": 1.5}`,
		" \n\t{ \"a\" : { \"b\" : [ 1 , 2 ] } } \n",
		`"日本語"`,
	} {
		var want any
		if err := json.Unmarshal([]byte(src), &want); err != nil {
			t.Fatalf("encoding/json rejected %q: %v", src, err)
		}
		got, err := decode(p, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %#v, want %#v", src, got, want)
		}
	}
}

func TestRejectsInvalidJSON(t *testing.T) {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{
		``, `{`, `[1,]`, `{"a" 1}`, `01`, `1.`, `"\x"`, "\"a\nb\"", `tru`, `[1] [2]`, `{a: 1}`,
	} {
		var v any
		if json.Unmarshal([]byte(src), &v) == nil {
			t.Fatalf("encoding/json accepted %q", src)
		}
		if _, err := decode(p, src); err == nil {
			t.Errorf("%q: expected an error", src)
		}
	}
}

// The generated parser (generated/parser.go) returns the same results as the engine.
func TestGeneratedParser(t *testing.T) {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{`{"a": [1, true, null, "x\"y"]}`, `[1,]`, ` -0.5e10 `} {
		want, werr := p.Parse(src)
		got, gerr := generated.Parse(src)
		wj, _ := json.Marshal(want)
		gj, _ := json.Marshal(got)
		if string(wj) != string(gj) || fmt.Sprint(werr) != fmt.Sprint(gerr) {
			t.Errorf("%q:\n engine    %s %v\n generated %s %v", src, wj, werr, gj, gerr)
		}
	}
}

// generated/parser.go matches a fresh generation from json.pego (go generate was not forgotten).
func TestGeneratedParserIsUpToDate(t *testing.T) {
	g, err := pego.ParseGrammar(grammar)
	if err != nil {
		t.Fatal(err)
	}
	want, err := pego.GenerateGo(g, "generated", "main")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("generated/parser.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("generated/parser.go is out of date; run go generate ./examples/json")
	}
}
