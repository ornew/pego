package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
	"testing"
)

func wideRepetitionCases() []genCase {
	if strconv.IntSize < 64 {
		return nil // Source bounds follow the implementation's int range.
	}
	var cases []genCase
	for _, bound := range []string{"2147483648", "4294967296", "9223372036854775807"} {
		for _, c := range []struct{ name, body string }{
			{"minimum", `"é"{%s} $$`},
			{"unbounded minimum", `"é"{%s,} $$`},
			{"scan minimum", `@(?é){%s} $$`},
			{"maximum", `"é"{0,%s} $$`},
			{"class scan", `@(?é){0,%s} $$`},
			{"any scan", `@.{0,%s} $$`},
			{"projection", `xs:(v:"é"){0,%s} $$ -> map($xs, (x) => $x.v)`},
		} {
			cases = append(cases, genCase{"wide " + c.name + " " + bound, "def main=" + fmt.Sprintf(c.body, bound), []string{"", "é", "éé", "x"}})
		}
	}
	return cases
}

func TestWideRepetitionModuleValidation(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("bounds exceed a 32-bit implementation's source integer range")
	}
	for _, scan := range []bool{false, true} {
		for _, defect := range []string{"missing minimum", "missing maximum", "noninteger", "negative minimum", "negative maximum", "reversed bounds"} {
			t.Run(fmt.Sprintf("scan=%v/%s", scan, defect), func(t *testing.T) {
				body := `"a"{0,4294967296}`
				if scan {
					body = `@(?a){0,4294967296}`
				}
				prog := compile(t, "def main="+body+" $$")
				m := prog.Module()
				found := false
				for i := range m.Code {
					x := &m.Code[i]
					if x.Op != OpRepeatWide && x.Op != OpScanWide {
						continue
					}
					found = true
					min, max := &x.A, &x.B
					if scan {
						min, max = &x.B, &x.C
					}
					switch defect {
					case "missing minimum":
						*min = -1
					case "missing maximum":
						*max = int32(len(m.Exprs))
					case "noninteger":
						m.Exprs[*min].Op = ENil
					case "negative minimum":
						m.Exprs[*min] = Instr{Op: EInt, A: -1}
					case "negative maximum":
						m.Exprs[*max] = Instr{Op: EInt, A: -2}
					case "reversed bounds":
						*min, *max = *max, *min
					}
					break
				}
				if !found {
					t.Fatal("wide instruction not emitted")
				}
				data, err := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := LoadProgram(data, Options{}); err == nil {
					t.Fatal("invalid wide operands loaded successfully")
				}
			})
		}
	}
}

func TestWideRepetitionInstructionSet(t *testing.T) {
	ordinary := compile(t, `def main="a"* $$`)
	data, err := ordinary.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if data[len(compiledMagic)+1] != 3 {
		t.Fatal("ordinary modules must retain instruction-set-3 compatibility")
	}
	if strconv.IntSize < 64 {
		return
	}
	wide := compile(t, `def main="a"{0,4294967296} $$`)
	data, err = wide.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	if data[len(compiledMagic)+1] != 4 {
		t.Fatal("wide modules must require instruction set 4")
	}
	if !strings.Contains(wide.Module().Disassemble(), "max=4294967296") {
		t.Fatal("disassembly must display the complete bound")
	}
	// A valid checksum does not make a false instruction-set declaration valid.
	data[len(compiledMagic)+1] = 3
	data = binary.LittleEndian.AppendUint32(data[:len(data)-4], crc32.ChecksumIEEE(data[:len(data)-4]))
	if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "require instruction set version 4") {
		t.Fatalf("wide instruction in old ISA: %v", err)
	}
}

func TestWideRepetitionBounds(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("bounds exceed a 32-bit implementation's source integer range")
	}
	for _, c := range wideRepetitionCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, options := range []Options{{}, {DisableMemo: true}} {
				prog := compile(t, c.src, options)
				for _, input := range c.inputs {
					checkBackends(t, prog, "main", input)
				}
				for _, marshal := range []MarshalOptions{{}, {OmitAST: true}} {
					data, err := prog.MarshalBinaryWith("main", marshal)
					if err != nil {
						t.Fatal(err)
					}
					loaded, _, err := LoadProgram(data, Options{})
					if err != nil {
						t.Errorf("load: %v", err)
						continue
					}
					for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
						if marshal.OmitAST && backend == Closure {
							continue
						}
						for _, unit := range []Unit{CodePoints, Bytes} {
							for _, input := range c.inputs {
								want, wantErr := prog.ParseWith("main", input, ParseOptions{Unit: unit})
								got, gotErr := loaded.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit})
								if resultJSON(got, gotErr) != resultJSON(want, wantErr) {
									t.Errorf("loaded %s/%s on %q: got %s, want %s", backend, unit, input, resultJSON(got, gotErr), resultJSON(want, wantErr))
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestWideRepetitionDocumentsAndStreams(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("bounds exceed a 32-bit implementation's source integer range")
	}
	for _, bound := range []string{"2147483648", "4294967296", "9223372036854775807"} {
		prog := compile(t, fmt.Sprintf(`def main="é"{0,%s} #stream $$`, bound))
		data, err := prog.MarshalBinary("main")
		if err != nil {
			t.Fatal(err)
		}
		saved, _, err := LoadProgram(data, Options{})
		if err != nil {
			t.Errorf("%s: load: %v", bound, err)
			continue
		}
		for _, p := range []*Program{prog, saved} {
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []Unit{CodePoints, Bytes} {
					opts := ParseOptions{Backend: backend, Unit: unit}
					count := 0
					err := p.ParseStreamWith("main", strings.NewReader("éé"), func(n *Node) error {
						if n.Text != "é" {
							t.Errorf("unexpected stream element: %s", resultJSON(n, nil))
						}
						count++
						return nil
					}, opts)
					if err != nil || count != 2 {
						t.Errorf("%s %s/%s: emitted %d, error %v", bound, backend, unit, count, err)
					}
					// Document repetitions use run-site metadata as well as the VM operands.
					d, err := p.NewDocumentWith("main", "é", opts)
					if err != nil {
						t.Fatal(err)
					}
					for range 2 {
						n, err := d.Parse()
						want, wantErr := prog.ParseWith("main", d.Text(), ParseOptions{Unit: unit})
						if resultJSON(n, err) != resultJSON(want, wantErr) {
							t.Errorf("Document %s/%s: got %s, want %s", backend, unit, resultJSON(n, err), resultJSON(want, wantErr))
						}
						if err := d.Edit(0, 0, "é"); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		}
	}
}
