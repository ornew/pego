package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"

	"github.com/ornew/pego/pkg/generator"
	"github.com/ornew/pego/pkg/grammer"
)

func run() error {
	var g grammer.Grammer
	peg, err := os.ReadFile("examples/calc/grammer.json")
	if err != nil {
		return err
	}
	err = json.Unmarshal(peg, &g)
	if err != nil {
		return err
	}
	f, err := os.OpenFile("examples/calc/parser.go", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	err = generator.Generate(&b, &g)
	if err != nil {
		return err
	}
	o, err := format.Source(b.Bytes())
	if err != nil {
		return err
	}
	_, err = io.Copy(f, bytes.NewReader(o))
	if err != nil {
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
