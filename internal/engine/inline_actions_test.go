package engine

import (
	"encoding/binary"
	"hash/crc32"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func inlineActionCases() []genCase {
	return []genCase{
		{"sequence reduction", `rule main = ("a" "b" => $2) "c" => $1`, []string{"abc", "ab", "xbc"}},
		{"choice reduction", `rule main = ("a" / "b" => $0)`, []string{"a", "b", "c"}},
		{"nested reduction", `rule main = (("a" => $1) "b" => $1)`, []string{"ab", "a", "b"}},
		{"explicit inline default", `rule main = ("a" => ...) "b" => $1`, []string{"ab", "a"}},
		{"inline capture", `rule main = (x:"a" => $x) "b" => $1`, []string{"ab", "a"}},
		{"capture before inline", `rule main = x:"a" ("b" => $x)`, []string{"ab", "a"}},
		{"capture before inline sequence", `rule main = x:"a" ("b" "c" => $x)`, []string{"abc", "ab"}},
		{"capture before inline choice", `rule main = x:"a" (("b" => $x) / "c")`, []string{"ab", "ac", "a"}},
		{"capture before inline optional", `rule main = x:"a" ("b" => $x)?`, []string{"ab", "a", "ac"}},
		{"nil positional inline", `rule main = ($$ => $1)`, []string{"", "a"}},
		{"capture rebound after inline", `rule main = (x:"a" => $x) x:"b" => $x`, []string{"ab", "a"}},
		{"typed inline action", `type T terminal
rule main T = ("a" => T{"z"})`, []string{"a", "b"}},
		{"inline left recursion", `rule main = e $$
rule e = (e "+" "a" => $1) / "a"`, []string{"a", "a+a", "a+a+a", "a+"}},
	}
}

func TestInlineActionsAcrossBackends(t *testing.T) {
	for _, tc := range inlineActionCases() {
		t.Run(tc.name, func(t *testing.T) {
			prog := compile(t, tc.src)
			for _, input := range tc.inputs {
				var want string
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					got := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend}))
					if want == "" {
						want = got
					} else if got != want {
						t.Fatalf("%q: %s differs: %s / %s", input, backend, got, want)
					}
				}
			}
		})
	}
}

func TestInlineActionsCaptureBoundary(t *testing.T) {
	compileErr := compileError(t, `rule main = (x:"a" => $x) "b" => $x`)
	if !strings.Contains(compileErr, "not visible after an inline action") {
		t.Fatal(compileErr)
	}
	g, err := syntax.Parse(`rule main = (x:"a" => $x) "b" => $x`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, Options{NoTypeCheck: true}); err == nil || !strings.Contains(err.Error(), "not visible after an inline action") {
		t.Fatalf("NoTypeCheck resolved a closed capture: %v", err)
	}
	// A new capture of the same name after the boundary is visible again.
	compile(t, `rule main = (x:"a" => $x) x:"b" => $x`, Options{NoTypeCheck: true})
	// A capture absent from the type checker's outer scope has not necessarily
	// crossed an inline boundary. A lookahead capture in a repetition remains
	// available to the whole-rule action under the existing capture contract.
	compile(t, `rule main = ("[" (&p:"a" "a")* "]")? => $p`)
	for _, src := range []string{
		`rule main = x:"a" (("b" => $x) / ("c" => $x)) => $x`,
		`rule main = x:"a" ("b" => $x)? => $x`,
	} {
		if err := compileError(t, src); !strings.Contains(err, "not visible after an inline action") {
			t.Fatalf("branch did not close its preceding capture: %v", err)
		}
	}
}

func TestInlineActionsFatalError(t *testing.T) {
	prog := compile(t, `rule main = ("a" => 1) / "a"`, Options{NoTypeCheck: true})
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		_, err := prog.ParseWith("main", "a", ParseOptions{Backend: backend})
		if err == nil || !strings.Contains(err.Error(), "action in main: result must be a node") {
			t.Fatalf("%s: %v", backend, err)
		}
	}
}

func TestInlineActionsCompiledRoundTrip(t *testing.T) {
	for _, tc := range inlineActionCases() {
		prog := compile(t, tc.src)
		for _, omit := range []bool{false, true} {
			data, err := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range tc.inputs {
				want := resultJSON(prog.Parse("main", input))
				got := resultJSON(loaded.Parse("main", input))
				if got != want {
					t.Fatalf("%s/%q (omit AST %v): %s / %s", tc.name, input, omit, got, want)
				}
			}
		}
	}
}

func TestInlineActionsInstructionSet(t *testing.T) {
	ordinary := compile(t, `rule main = "a"`)
	if got := ordinary.Module().instructionSetVersion(); got != 3 {
		t.Fatalf("ordinary module requires instruction set %d, want 3", got)
	}
	inline := compile(t, `rule main = ("a" => $1)`)
	if got := inline.Module().instructionSetVersion(); got != 7 {
		t.Fatalf("inline action requires instruction set %d, want 7", got)
	}
	for _, omit := range []bool{false, true} {
		data, err := inline.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
		if err != nil {
			t.Fatal(err)
		}
		if data[len(compiledMagic)+1] != 7 {
			t.Fatal("saved inline action has the wrong instruction set")
		}
		data[len(compiledMagic)+1] = 6
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "inline actions require instruction set version 7") {
			t.Fatalf("under-declared inline action accepted (omit AST %v): %v", omit, err)
		}
	}
	// The recognizer can elide an inline action whose value is unused. Its
	// module still uses the old instruction set, but the optional AST cannot.
	elided := compile(t, `rule main = ("a" => $1) "b" => nil`)
	if got := elided.Module().instructionSetVersion(); got != 3 {
		t.Fatalf("elided inline action module requires instruction set %d, want 3", got)
	}
	for _, omit := range []bool{false, true} {
		data, err := elided.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
		if err != nil {
			t.Fatal(err)
		}
		wantISA := byte(7)
		if omit {
			wantISA = 3
		}
		if got := data[len(compiledMagic)+1]; got != wantISA {
			t.Fatalf("elided inline action, omit AST %v: instruction set %d, want %d", omit, got, wantISA)
		}
		if _, _, err := LoadProgram(data, Options{}); err != nil {
			t.Fatalf("elided inline action, omit AST %v: %v", omit, err)
		}
		if !omit {
			data[len(compiledMagic)+1] = 3
			binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
			if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "inline actions require instruction set version 7") {
				t.Fatalf("under-declared AST-only inline action accepted: %v", err)
			}
		}
	}
}

func TestGeneratedInlineActions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated parsers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, inlineActionCases(), GenOptions{})
	testGeneratedTypesCorpus(t, goBin, inlineActionCases(), GenOptions{convertTypes: true})
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, inlineActionCases(), GenOptions{})
	}
}
