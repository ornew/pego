package parsers_test

import (
	"os"
	"testing"

	"github.com/ornew/zeroparse"
)

func TestYAMLDirectiveBackends(t *testing.T) {
	src, err := os.ReadFile("yaml/yaml.zp")
	if err != nil {
		t.Fatal(err)
	}
	p, err := zp.CompileSource(string(src), "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		directive string
		valid     bool
	}{
		{"%TAG", false}, {"%YAML", false}, {"%TAG !e!", false},
		{"%YAML 1.foo", false}, {"%TAG notahandle tag:x", false},
		{"%YAML 1.2 extra", false}, {"%TAG !e! tag:x extra", false},
		{"%YAMLfoo", true}, {"%TAGx ignored", true}, {"%YAML#name", true},
		{"%YAML 0001.02", true}, {"%TAG !e! notahandle", true},
	} {
		t.Run(tc.directive, func(t *testing.T) {
			for _, backend := range []zp.Backend{zp.Closure, zp.Bytecode, zp.BytecodeIterative} {
				for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
					for _, recognize := range []bool{false, true} {
						opts := []zp.ParseOption{zp.WithBackend(backend), zp.WithUnit(unit)}
						if recognize {
							opts = append(opts, zp.RecognizeOnly())
						}
						_, err := p.Parse(tc.directive+"\n--- x\n", opts...)
						if (err == nil) != tc.valid {
							t.Errorf("backend %v unit %v recognize %v: %v", backend, unit, recognize, err)
						}
					}
				}
			}
		})
	}
}
