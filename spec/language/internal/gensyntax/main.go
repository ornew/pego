// Command gensyntax generates the simplified grammar reference.
package main

import (
	"bytes"
	"fmt"
	"os"
	"text/template"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/grammar"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	full, err := os.ReadFile("../../zeroparse.zp")
	if err != nil {
		return err
	}
	g, err := zp.ParseGrammar(string(full))
	if err != nil {
		return err
	}
	t, err := template.ParseFiles("syntax.md.tmpl")
	if err != nil {
		return err
	}
	data := struct{ Simple string }{grammar.FormatSyntax(g)}
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return err
	}
	if existing, err := os.ReadFile("syntax.md"); err == nil && bytes.Equal(existing, out.Bytes()) {
		return nil
	}
	return os.WriteFile("syntax.md", out.Bytes(), 0644)
}
