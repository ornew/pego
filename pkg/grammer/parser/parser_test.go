package parser

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/ornew/pego/pkg/grammer"
	bootparser "github.com/ornew/pego/pkg/grammer/parser/boot"
	"github.com/ornew/pego/pkg/parser"
	"github.com/ornew/pego/pkg/printer"
	"github.com/stretchr/testify/require"
)

//go:embed boot/grammer.json
var grammerJSON []byte

//go:embed grammer.pego
var grammerPEGO []byte

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		in string
	}{
		{
			in: `package test
sequence <- a b c
choice <- a / b / c
group <- (a b) c
not <- !a b c
and <- &a b c
optional <- a? b c
one_or_more <- a+ b c
zero_or_more <- a* b c
terminal <- "a" "b c"
terminal_range <- [a-z]
any <- .
complex <- a (b / c? !(d &(e f))) [0-9]+ .*
`,
		},
	} {
		p, err := parser.NewParser(strings.NewReader(tt.in))
		if err != nil {
			t.Fatal(err)
		}
		root, err := Parse(p)
		if err != nil {
			t.Fatal(err)
		}
		pp := printer.Printer{
			GetText:  p.GetText,
			Demangle: Demangle,
		}
		fmt.Println(pp.Print(root))
		e := Extractor{
			GetText: p.GetText,
		}
		require.Len(t, root.Children, 1)
		g := e.GetFile(root.Children[0])
		b, _ := json.MarshalIndent(g, "", "  ")
		fmt.Println(string(b))
		fmt.Println(g.String())
	}
}

func TestParseGrammerPEGO(t *testing.T) {
	fmt.Println(string(grammerPEGO))
	var gj grammer.Grammer
	err := json.Unmarshal(grammerJSON, &gj)
	require.NoError(t, err)
	p, err := parser.NewParser(bytes.NewReader(grammerPEGO))
	if err != nil {
		t.Fatal(err)
	}
	root, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	pp := printer.Printer{
		GetText:  p.GetText,
		Demangle: Demangle,
	}
	fmt.Println(pp.Print(root))
	e := Extractor{
		GetText: p.GetText,
	}
	require.Len(t, root.Children, 1)
	g := e.GetFile(root.Children[0])
	b, _ := json.MarshalIndent(g, "", "  ")
	fmt.Println(string(b))
	fmt.Println(g.String())
	if diff := cmp.Diff(&gj, g); diff != "" {
		t.Fatal(diff)
	}
}

func TestBootParseGrammerPEGO(t *testing.T) {
	fmt.Println(string(grammerPEGO))
	var gj grammer.Grammer
	err := json.Unmarshal(grammerJSON, &gj)
	require.NoError(t, err)
	p, err := parser.NewParser(bytes.NewReader(grammerPEGO))
	if err != nil {
		t.Fatal(err)
	}
	root, err := bootparser.Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	pp := printer.Printer{
		GetText:  p.GetText,
		Demangle: Demangle,
	}
	fmt.Println(pp.Print(root))
	e := Extractor{
		GetText: p.GetText,
	}
	require.Len(t, root.Children, 1)
	g := e.GetFile(root.Children[0])
	b, _ := json.MarshalIndent(g, "", "  ")
	t.Log("get grammer JSON")
	fmt.Println(string(b))
	t.Log("get grammer")
	fmt.Println(g.String())
	t.Log("boot grammer")
	fmt.Println(gj.String())
	if diff := cmp.Diff(&gj, g); diff != "" {
		t.Fatal(diff)
	}
}
