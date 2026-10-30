package tsgen_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/tsgen"
	"github.com/ornew/pego/internal/gentest"
)

// literalTextUnits computes JS text units from the documented byte-preserving
// surrogate convention, independently of the TypeScript matcher.
func literalTextUnits(input string, unit engine.PositionUnit) []rune {
	var units []rune
	for i := 0; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])
		if unit == engine.Bytes && r == utf8.RuneError && size == 1 {
			units = append(units, 0xdc00+rune(input[i]))
		} else if r > 0xffff {
			r -= 0x10000
			units = append(units, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		} else {
			units = append(units, r)
		}
		i += size
	}
	return units
}

func TestGeneratedTSInvalidUTF8Literals(t *testing.T) {
	t.Parallel()
	for _, tc := range gentest.InvalidLiteralCorpus() {
		t.Run(tc.Name, func(t *testing.T) {
			g := gentest.LiteralCorpusGrammar(t, tc)
			prog, err := engine.Compile(g, compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			code, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", Recognize: true})
			if err != nil {
				t.Fatal(err)
			}
			var script, want strings.Builder
			script.WriteString(`import {parse,recognize,marshal,Node,CodePoints,Bytes} from "./parser.ts";` + "\n")
			for _, input := range tc.Inputs {
				fmt.Fprintf(&script, `for (const unit of [CodePoints,Bytes]){const input=new Uint8Array(Buffer.from(%q,"base64"));const r=parse(input,unit);const rec=recognize(input,unit);if((rec?.message??null)!==(r.error?.message??null))throw new Error("recognition differs");const child=r.node?.field("T");const expected=unit===Bytes?String.fromCharCode(...%s):String.fromCharCode(...%s);if(!r.error && (!(child instanceof Node)||child.text!==expected))throw new Error("matched bytes differ");const out:Record<string,unknown>={};if(r.error)out["err"]=r.error.message;out["node"]=JSON.parse(marshal(r.node));console.log(JSON.stringify(out));}`+"\n", base64.StdEncoding.EncodeToString([]byte(input)), tsgen.TsInts(literalTextUnits(input, engine.Bytes)), tsgen.TsInts(literalTextUnits(input, engine.CodePoints)))
				for _, unit := range []engine.PositionUnit{engine.CodePoints, engine.Bytes} {
					n, err := prog.ParseWith("main", input, engine.ParseOptions{Expectations: true, Unit: unit})
					want.WriteString(gentest.ResultJSON(n, err) + "\n")
				}
			}
			dir := t.TempDir()
			for name, data := range map[string][]byte{"package.json": []byte(`{"type":"module"}`), "parser.ts": code, "main.ts": []byte(script.String())} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			node, err := exec.LookPath("node")
			if err != nil {
				t.Skip("node not found")
			}
			cmd := exec.Command(node, "main.ts")
			cmd.Dir = dir
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil || string(out) != want.String() {
				t.Fatalf("generated TS: %v\n%s\ngot %s\nwant %s", err, stderr.String(), out, want.String())
			}
			if tsc, err := exec.LookPath("tsc"); err == nil {
				cmd := exec.Command(tsc, "--strict", "--target", "ES2022", "--module", "esnext", "--noEmit", "--skipLibCheck", "parser.ts")
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("strict tsc: %v\n%s", err, out)
				}
			}
		})
	}
}
