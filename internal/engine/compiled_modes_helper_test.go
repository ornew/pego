package engine

import (
	"strings"
	"testing"
)

// valueModes are the modes of compiled grammars that build values: parse mode serves plain parses
// and document mode traced ones (servedBy).
var valueModes = []CompiledMode{CompiledParse, CompiledDocument}

// allModes are the modes of compiled grammars.
var allModes = []CompiledMode{CompiledParse, CompiledRecognize, CompiledDocument}

// servedBy returns o as a parse that a program loaded in mode serves: a recognize-mode program
// serves recognition, and a document-mode program traced parses, so o gets a trace that does
// nothing unless it has one.
func servedBy(mode CompiledMode, o ParseOptions) ParseOptions {
	switch mode {
	case CompiledRecognize:
		o.Recognize = true
	case CompiledDocument:
		if o.Trace == nil {
			o.Trace = func(TraceEvent) {}
		}
	}
	return o
}

// saveLoad saves prog in mode with the start rule start and loads it again.
func saveLoad(t *testing.T, prog *Program, start string, mode CompiledMode) *Program {
	t.Helper()
	data, err := prog.Marshal(start, mode)
	if err != nil {
		t.Fatalf("save in %s mode: %v", mode, err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatalf("load in %s mode: %v", mode, err)
	}
	return loaded
}

// wantClosureRefused fails unless err is the error of the closure backend on a loaded program.
func wantClosureRefused(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "closure backend needs the grammar source") {
		t.Errorf("closure backend on a loaded program: got %v, want the closure backend refused", err)
	}
}

// isaOffset is the offset of the instruction set version in a compiled grammar, after the magic,
// the container version and the mode; versions below 128 take one byte.
const isaOffset = len(compiledMagic) + 2
