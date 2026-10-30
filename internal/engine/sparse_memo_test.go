package engine

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
)

func TestSparseMemoRuleAndPageBoundaries(t *testing.T) {
	t.Parallel()
	for _, stride := range []int{63, 64, 65, 128, 175} {
		memo := core.MemoTable{Stride: stride}
		positions := []int{0, 1, 63, 64, 1023, 1024, 1025, 65535}
		for _, pos := range positions {
			for _, rule := range []int{0, stride - 1} {
				if !sparseMemoRecord(&memo, pos, rule) || sparseMemoRecord(&memo, pos, rule) {
					t.Fatalf("stride=%d pos=%d rule=%d: first/repeated mismatch", stride, pos, rule)
				}
			}
		}
		for i := len(positions) - 1; i >= 0; i-- {
			if sparseMemoRecord(&memo, positions[i], stride-1) {
				t.Fatal("returning to an earlier page lost its bit")
			}
		}
		if !memo.Reset() {
			t.Fatal("bounded memo table rejected reuse")
		}
		for _, dir := range memo.Pages {
			if dir.Last != nil {
				t.Fatal("cached page pointer survived reset")
			}
		}
		memo.Stride = stride
		for _, pos := range positions {
			for _, rule := range []int{0, stride - 1} {
				if !sparseMemoRecord(&memo, pos, rule) {
					t.Fatalf("stride=%d pos=%d rule=%d: stale bit survived reset", stride, pos, rule)
				}
			}
		}
		if core.SparseMemo && stride > 64 && len(memo.Seen) != 0 {
			t.Fatal("sparse path allocated the dense bitset")
		}
	}
}

func sparseMemoRecord(memo *core.MemoTable, pos, rule int) bool {
	if core.SparseMemo && memo.Stride > 64 {
		return memo.MarkSparseSeen(pos, rule)
	}
	return memo.MarkSeen(pos, rule)
}

func TestSparseMemoStrideChanges(t *testing.T) {
	t.Parallel()
	memo := core.MemoTable{}
	for _, stride := range []int{175, 65, 64, 128, 175} {
		memo.Stride = stride
		if !sparseMemoRecord(&memo, 1024, stride-1) || sparseMemoRecord(&memo, 1024, stride-1) {
			t.Fatal("stride change reused a stale bit")
		}
		if !memo.Reset() {
			t.Fatal("bounded table rejected reuse")
		}
	}
}

func TestSparseMemoResetBounds(t *testing.T) {
	t.Parallel()
	for _, memo := range []core.MemoTable{
		{Pages: make([]core.SeenRulePages, 0, core.MaxScratch/5+1)},
		{Pages: make([]core.SeenRulePages, 0, core.MaxScratch/5), BitPages: core.Arena[[16]uint64]{Chunks: []*[64][16]uint64{new([64][16]uint64)}}},
		{Pages: []core.SeenRulePages{{Pages: make([]*[16]uint64, 0, core.MaxScratch+1)}}},
	} {
		if memo.Reset() {
			t.Fatal("oversized directory retained")
		}
	}
	memo := core.MemoTable{Stride: 65}
	for i := 0; i <= core.MaxScratch/16; i++ {
		memo.MarkSparseSeen(i*1024, 0)
	}
	if memo.Reset() {
		t.Fatal("oversized pages retained")
	}
}

func TestSparseMemoParseControls(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		t.Fatal(err)
	}
	g, err := parseTest(string(src))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prog.NSeen <= 64 {
		t.Fatal("control no longer exercises sparse rule count")
	}
	for i, input := range []string{"const 日本語 = \"😀\";\nconst v = [1, 2].map(x => x + 1);\n", "function f( { return 1;", strings.Repeat("const 日本語 = 1 + 2;\n", 100), strings.Repeat("const a = 1;\n", 100) + "function f( { return 1;"} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, recognize := range []bool{false, true} {
				checkTrace(t, prog, "main", input, ParseOptions{Unit: unit, Recognize: recognize})
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					work, result := parseSpend(prog, "main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
					t.Logf("semantic=%d/%s/%s/%t result=%x work=%x", i, backend, unit, recognize, sha256.Sum256([]byte(result)), sha256.Sum256([]byte(fmt.Sprintf("%+v", work))))
				}
			}
		}
	}
}

func TestSparseMemoInputSelection(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		t.Fatal(err)
	}
	g, err := parseTest(string(data))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		"/*" + strings.Repeat("a", 1020) + "*/",
		"/*" + strings.Repeat("a", 1021) + "*/",
		"/*" + strings.Repeat("é", 512) + "*/",
		"/*" + strings.Repeat("a", 1020) + "*/",
	} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				p := core.NewParser(&prog.sharedProgram, text, unit, true)
				p.DeferMemo = true
				if _, err := prog.Run(p, backend, "main"); err != nil {
					t.Fatal(err)
				}
				want := len([]rune(text)) > 1024
				if unit == Bytes {
					want = len(text) > 1024
				}
				want = want && core.SparseMemo
				if p.Memo.Sparse != want {
					t.Fatalf("%s/%s: sparse=%t want=%t", backend, unit, p.Memo.Sparse, want)
				}
				if !want && len(p.Memo.Pages) != 0 {
					t.Fatal("short input allocated sparse directory")
				}
			}
		}
	}
}
