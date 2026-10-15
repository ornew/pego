package pego_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func TestParsePrefixRejectsMissingTokenOptions(t *testing.T) {
	parser, err := pego.CompileSource(`rule main = "x"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		for _, recognize := range []bool{false, true} {
			traceCalls := 0
			opts := []pego.ParseOption{pego.WithBackend(backend), pego.WithTrace(func(pego.TraceEvent) { traceCalls++ })}
			if recognize {
				opts = append(opts, pego.RecognizeOnly())
			}
			// A positive option is invalid independently of grammar annotations and
			// whether the rule could otherwise succeed. A later positive value wins.
			for _, limits := range [][]pego.ParseOption{
				{pego.WithMissingTokens(1)},
				{pego.WithMissingTokens(0), pego.WithMissingTokens(1)},
			} {
				options := append(append([]pego.ParseOption{}, opts...), limits...)
				result, err := parser.ParsePrefix("main", "xtail", options...)
				if err == nil || !strings.Contains(err.Error(), "prefix parsing") || result != (pego.PrefixResult{}) || traceCalls != 0 {
					t.Fatalf("%v/recognize=%v: positive limit returned %+v, %v; trace calls=%d", backend, recognize, result, err, traceCalls)
				}
			}
			// Disabling insertion last restores the ordinary prefix API.
			options := append(append([]pego.ParseOption{}, opts...), pego.WithMissingTokens(1), pego.WithMissingTokens(0))
			result, err := parser.ParsePrefix("main", "xtail", options...)
			if err != nil || result.Consumed != 1 || result.ConsumedBytes != 1 || recognize && result.Node != nil {
				t.Fatalf("%v/recognize=%v: disabled insertion returned %+v, %v", backend, recognize, result, err)
			}
			// Negative limits retain the existing shared configuration validation.
			options = append(append([]pego.ParseOption{}, opts...), pego.WithMissingTokens(-1))
			_, referenceErr := parser.Parse("x", options...)
			result, err = parser.ParsePrefix("main", "xtail", options...)
			if err == nil || referenceErr == nil || err.Error() != referenceErr.Error() || result != (pego.PrefixResult{}) {
				t.Fatalf("negative limit: prefix=%+v/%v, Parse error=%v", result, err, referenceErr)
			}
		}
	}
}

func TestParsePrefixMissingGrammarKeepsWholeInputInsertionScope(t *testing.T) {
	parser, err := pego.CompileSource(`rule main = token $$
rule token = "x" ";" #missing`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, recognize := range []bool{false, true} {
		opts := []pego.ParseOption{pego.WithBackend(pego.Closure), pego.WithMissingTokens(1)}
		if recognize {
			opts = append(opts, pego.RecognizeOnly())
		}
		// Whole-input parsing continues to synthesize the missing semicolon.
		node, err := parser.Parse("x", opts...)
		var recovered pego.SyntaxErrors
		if !errors.As(err, &recovered) || len(recovered) != 1 || recognize && node != nil || !recognize && node == nil {
			t.Fatalf("whole-input insertion recognize=%v: node=%v error=%v", recognize, node, err)
		}
		// Prefix parsing rejects the option both when insertion would be needed
		// and when the literal is already present; it must not silently ignore it.
		for _, input := range []string{"x", "x;tail"} {
			result, err := parser.ParsePrefix("token", input, opts...)
			if err == nil || !strings.Contains(err.Error(), "prefix parsing") || result != (pego.PrefixResult{}) {
				t.Fatalf("insertion prefix input=%q recognize=%v: %+v %v", input, recognize, result, err)
			}
		}
		// Annotated grammars remain unsupported when insertion is disabled,
		// including omission, an explicit zero and a zero overriding a positive.
		for _, limits := range [][]pego.ParseOption{
			nil,
			{pego.WithMissingTokens(0)},
			{pego.WithMissingTokens(1), pego.WithMissingTokens(0)},
		} {
			options := []pego.ParseOption{pego.WithBackend(pego.Closure)}
			if recognize {
				options = append(options, pego.RecognizeOnly())
			}
			options = append(options, limits...)
			for _, input := range []string{"x", "x;tail"} {
				result, err := parser.ParsePrefix("token", input, options...)
				if err == nil || !strings.Contains(err.Error(), "prefix parsing") || result != (pego.PrefixResult{}) {
					t.Fatalf("disabled insertion on annotated grammar input=%q recognize=%v: %+v %v", input, recognize, result, err)
				}
			}
		}
	}
}
