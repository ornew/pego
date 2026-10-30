package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
)

func TestDiagnosticAttribute(t *testing.T) {
	t.Parallel()
	check(t, `rule main = ([0-9]+ "." [0-9]+) @diagnostic(name="number")`,
		fails("12x", `1:3: syntax error: expected number`),
		fails("12.", `1:4: syntax error: expected number`),
	)
	check(t, `rule main = "a" @diagnostic(name="letter")`,
		ok("a", `"a"@main`),
		fails("b", `1:1: syntax error: expected letter`),
	)
	check(t, `rule main = _|_ @diagnostic(name="impossible")`,
		fails("", `1:1: syntax error`),
	)
	check(t, `rule main = "a"+ @diagnostic(name="letter") "b"`,
		fails("aac", `1:3: syntax error: expected "a", "b"`),
	)
	check(t, `rule main = ("a" @diagnostic(name="first") / "b")`,
		fails("c", `1:1: syntax error: expected "b", first`),
	)
	check(t, `rule main = ("a" @error(message="explicit") @diagnostic(name="ordinary"))`,
		fails("b", `1:1: explicit`),
	)
	check(t, `rule main = ("a" @diagnostic(name="ordinary") @error(message="explicit"))`,
		fails("b", `1:1: explicit`),
	)
	check(t, `rule main = "a" @diagnostic(name="文字")`,
		fails("b", `1:1: syntax error: expected 文字`),
	)
}

func TestDiagnosticAttributeStructuredErrors(t *testing.T) {
	t.Parallel()
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			mixed := mustCompile(t, `rule main = ("a" @error(message="explicit") @diagnostic(name="ordinary") / "b" @diagnostic(name="bee"))`)
			_, err := mixed.ParseWith("main", "c", ParseOptions{Backend: backend})
			var syntax *SyntaxError
			if !errors.As(err, &syntax) || !slices.Equal(syntax.Expected, []string{"bee"}) || !slices.Equal(syntax.Messages, []string{"explicit"}) {
				t.Fatalf("mixed diagnostic = %#v, err %v", syntax, err)
			}

			farther := mustCompile(t, `rule main = "a" "b" / "c" @diagnostic(name="cee")`)
			_, err = farther.ParseWith("main", "ax", ParseOptions{Backend: backend})
			if !errors.As(err, &syntax) || !slices.Equal(syntax.Expected, []string{`"b"`}) || len(syntax.Messages) != 0 || syntax.Col != 2 {
				t.Fatalf("farther diagnostic = %#v, err %v", syntax, err)
			}

			recovered := mustCompile(t, `rule main = ("a" @diagnostic(name="letter") #recover(skip=[^!]* "!") "b")`)
			_, err = recovered.ParseWith("main", "x!b", ParseOptions{Backend: backend})
			var recoveries SyntaxErrors
			if !errors.As(err, &recoveries) || len(recoveries) != 1 || !slices.Equal(recoveries[0].Expected, []string{"letter"}) || len(recoveries[0].Messages) != 0 {
				t.Fatalf("recovered diagnostic = %#v, err %v", recoveries, err)
			}
		})
	}
}

func TestDiagnosticAttributeInstructionSetVersion(t *testing.T) {
	t.Parallel()
	ordinary, err := mustCompile(t, `rule main = "a"`).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if ordinary[len(compiledMagic)] != compiledVersion {
		t.Fatalf("ordinary module version: %d", ordinary[len(compiledMagic)])
	}
	if ordinary[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("ordinary module ISA: %d", ordinary[isaOffset])
	}
	prog := mustCompile(t, `rule main = "a" @diagnostic(name="letter")`)
	for _, mode := range allModes {
		data, err := prog.Marshal("main", mode)
		if err != nil {
			t.Fatal(err)
		}
		if data[len(compiledMagic)] != compiledVersion {
			t.Fatalf("diagnostic module version: %d, want %d", data[len(compiledMagic)], compiledVersion)
		}
		if data[isaOffset] != bytecode.ISAVersion {
			t.Fatalf("diagnostic module ISA: %d", data[isaOffset])
		}
		loaded, _, err := LoadProgram(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := resultWith(loaded, "b", servedBy(mode, ParseOptions{Backend: Bytecode})); got != "error: 1:1: syntax error: expected letter" {
			t.Errorf("loaded diagnostic in %s mode: %s", mode, got)
		}
		oldISA := append([]byte(nil), data...)
		oldISA[isaOffset] = 6
		binary.LittleEndian.PutUint32(oldISA[len(oldISA)-4:], crc32.ChecksumIEEE(oldISA[:len(oldISA)-4]))
		if _, _, err := LoadProgram(oldISA); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
			t.Errorf("old ISA accepted diagnostic module: %v", err)
		}
	}
	reserved := append([]byte(nil), ordinary...)
	reserved[isaOffset] = bytecode.ISAVersion + 1
	binary.LittleEndian.PutUint32(reserved[len(reserved)-4:], crc32.ChecksumIEEE(reserved[:len(reserved)-4]))
	if _, _, err := LoadProgram(reserved); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unsupported instruction set version %d", bytecode.ISAVersion+1)) {
		t.Errorf("reserved ISA accepted: %v", err)
	}
	combined := mustCompile(t, `rule main = v1:"a" @diagnostic(name="letter") $$ => filter(list($v1), (item) => true)`)
	data, err := combined.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("filter and diagnostic ISA: %d, want %d", data[isaOffset], bytecode.ISAVersion)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := resultWith(loaded, "b", ParseOptions{Backend: Bytecode}); got != "error: 1:1: syntax error: expected letter" {
		t.Errorf("loaded filter and diagnostic: %s", got)
	}
}
