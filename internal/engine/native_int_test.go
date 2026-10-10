package engine

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Encoded integers must not become valid values by narrowing on the host.
func TestDecodedNativeIntegers(t *testing.T) {
	for _, n := range []int64{-1 << 63, -1<<31 - 1, -1 << 31, -1, 0, 1<<31 - 1, 1 << 31, 1<<63 - 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			r := decoder{data: binary.AppendVarint(nil, n)}
			got := r.int()
			fits := strconv.IntSize == 64 || n >= -1<<31 && n <= 1<<31-1
			if fits {
				if r.err != nil || int64(got) != n {
					t.Fatalf("decoded %d, %v; want %d", got, r.err, n)
				}
			} else if r.err == nil {
				t.Fatalf("accepted out-of-range value as %d", got)
			}
			r = decoder{data: binary.AppendVarint(nil, n)}
			got32 := r.int32()
			if n >= -1<<31 && n <= 1<<31-1 {
				if r.err != nil || int64(got32) != n {
					t.Fatalf("operand %d, %v; want %d", got32, r.err, n)
				}
			} else if r.err == nil {
				t.Fatalf("accepted out-of-range operand as %d", got32)
			}
		})
	}
	for _, n := range []uint64{0, 1<<31 - 1, 1 << 31, 1<<31 + 1, 1<<64 - 1} {
		r := decoder{data: binary.AppendUvarint(nil, n)}
		got := r.uint()
		fits := n <= 1<<31-1 || strconv.IntSize == 64 && n == 1<<31
		if fits {
			if r.err != nil || uint64(got) != n {
				t.Fatalf("unsigned %d, %v; want %d", got, r.err, n)
			}
		} else if r.err == nil {
			t.Fatalf("accepted unsigned %d as %d", n, got)
		}
	}
}

func TestLoadedNativeIntegerConstants(t *testing.T) {
	for _, n := range []int64{-1 << 63, -1<<31 - 1, -1 << 31, 1<<31 - 1, 1 << 31, 1<<63 - 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			prog := compile(t, `type N struct { V int }
def main = "a" -> new N{V: 1}`)
			found := false
			for i := range prog.Module().Exprs {
				x := &prog.Module().Exprs[i]
				if x.Op == EInt {
					x.A = int32(n)
					x.B = int32((uint64(n) - uint64(int64(x.A))) >> 32)
					found = true
				}
			}
			if !found {
				t.Fatal("missing integer expression")
			}
			data, err := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
			if err != nil {
				t.Fatal(err)
			}
			loaded, start, err := LoadProgram(data, Options{})
			if strconv.IntSize == 32 && (n < -1<<31 || n > 1<<31-1) {
				if err == nil || !strings.Contains(err.Error(), "out of range") {
					t.Fatalf("out-of-range module: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				got, err := loaded.ParseWith(start, "a", ParseOptions{Backend: backend})
				want := fmt.Sprintf("(N V=%d)", n)
				if err != nil || got.String() != want {
					t.Fatalf("%s: %v, %v; want %s", backend, got, err, want)
				}
			}
		})
	}
}

func TestTSNativeIntegerLiterals(t *testing.T) {
	max := int(^uint(0) >> 1)
	for _, n := range []int{-max - 1, -1, 0, max} {
		want := strconv.Itoa(n)
		if int64(n) < -(1<<53-1) || int64(n) > 1<<53-1 {
			want = "BigInt(\"" + want + "\")"
		}
		if got := tsInt(n); got != want {
			t.Fatalf("%d: got %s; want %s", n, got, want)
		}
	}
}

func TestLoadedNativeRepetitionBounds(t *testing.T) {
	for _, scan := range []bool{false, true} {
		for _, n := range []int64{1<<31 - 1, 1 << 31, 1<<32 + 1, 1<<63 - 1} {
			t.Run(fmt.Sprintf("scan=%v/max=%d", scan, n), func(t *testing.T) {
				body := `"a"{0,2} $$`
				if scan {
					body = `@(?a){0,2} $$`
				}
				prog := compile(t, "def main = "+body)
				m := prog.Module()
				lo := int32(n)
				minIndex := int32(len(m.Exprs))
				m.Exprs = append(m.Exprs, Instr{Op: EInt},
					Instr{Op: EInt, A: lo, B: int32((uint64(n) - uint64(int64(lo))) >> 32)})
				found := false
				for i := range m.Code {
					x := &m.Code[i]
					if !scan && x.Op == OpRepeat {
						x.Op, x.A, x.B = OpRepeatWide, minIndex, minIndex+1
						found = true
					} else if scan && x.Op == OpScan {
						x.Op, x.B, x.C = OpScanWide, minIndex, minIndex+1
						found = true
					}
				}
				if !found {
					t.Fatal("missing repetition instruction")
				}
				data, err := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
				if err != nil {
					t.Fatal(err)
				}
				loaded, start, err := LoadProgram(data, Options{})
				if strconv.IntSize == 32 && n > 1<<31-1 {
					if err == nil || !strings.Contains(err.Error(), "implementation int range") {
						t.Fatalf("out-of-range repetition bound: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					for _, text := range []string{"", "a", "aaa"} {
						if _, err := loaded.ParseWith(start, text, ParseOptions{Backend: backend}); err != nil {
							t.Fatalf("%s %q: %v", backend, text, err)
						}
					}
				}
			})
		}
	}
}
