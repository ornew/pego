package engine

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/schema"
)

// schemaJSON exports the schema of p for start, or every rule with allRules, as canonical JSON.
func schemaJSON(t *testing.T, p *Program, start string, allRules bool) string {
	t.Helper()
	s, err := outputSchemaPrototype(p, start, allRules)
	if err != nil {
		t.Fatalf("schema of %s (all rules %t): %v", start, allRules, err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// schemaCorpus returns the grammars of the schema agreement tests: the backend corpus and the
// grammars of the parsers directory, which are the examples of the generated parsers.
func schemaCorpus(t *testing.T) []genCase {
	cases := genCorpus(t)
	paths, err := filepath.Glob(filepath.Join("..", "..", "parsers", "*", "*.pego"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if testing.Short() && len(src) > 20000 {
			continue
		}
		cases = append(cases, genCase{name: "parsers/" + filepath.Base(filepath.Dir(path)), src: string(src)})
	}
	return cases
}

// wantSchemaNeedsSource fails unless err is the schema error of a program loaded from a compiled
// grammar.
func wantSchemaNeedsSource(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, schema.ErrUnavailable) || !strings.Contains(err.Error(), "export the schema from the grammar source") {
		t.Fatalf("schema of a loaded program: %v", err)
	}
}

// TestCompiledSchemaMatchesSource checks that a compiled grammar holds no schema in any mode: a
// loaded parser refuses every start rule and all rules, the source still exports them, and saving
// the loaded file again reproduces it.
func TestCompiledSchemaMatchesSource(t *testing.T) {
	for _, c := range schemaCorpus(t) {
		for _, options := range []compile.Options{{SourceInfo: true}, {DefaultValue: true}} {
			t.Run(c.name, func(t *testing.T) {
				src := compileBare(t, c.src, options)
				if len(src.StartRules) == 0 {
					t.Fatal("no start rule")
				}
				def := src.DefaultStart()
				for _, mode := range allModes {
					data, err := src.Marshal(def, mode)
					if err != nil {
						t.Fatal(err)
					}
					if data[len(compiledMagic)] != compiledVersion || CompiledMode(data[len(compiledMagic)+1]) != mode {
						t.Fatal("saves are not the current format")
					}
					loaded, _, err := LoadProgram(data)
					if err != nil {
						t.Fatal(err)
					}
					again, err := loaded.Marshal(def, mode)
					if err != nil || !bytes.Equal(data, again) {
						t.Fatalf("%s mode file changed on save: %v", mode, err)
					}
					for _, start := range append(append([]string{}, src.StartRules...), "") {
						name := start
						if name == "" {
							name = def
						}
						for _, all := range []bool{false, true} {
							schemaJSON(t, src, name, all)
							_, err := outputSchemaPrototype(loaded, name, all)
							wantSchemaNeedsSource(t, err)
						}
					}
				}
			})
		}
	}
}

// TestCompiledSchemaBackendAgreement parses the inputs of each grammar with every backend and checks
// that the schema exported afterwards is the same. The closure backend runs on the source program;
// the two bytecode backends also run on the file in parse mode, which has no schema before or after
// parsing.
func TestCompiledSchemaBackendAgreement(t *testing.T) {
	for _, c := range genCorpus(t) {
		t.Run(c.name, func(t *testing.T) {
			src := mustCompile(t, c.src)
			def := src.DefaultStart()
			want := schemaJSON(t, src, def, true)
			loaded := saveLoad(t, src, def, CompiledParse)
			for _, input := range c.inputs {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					src.ParseWith(def, input, ParseOptions{Backend: backend})
				}
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					loaded.ParseWith(def, input, ParseOptions{Backend: backend})
				}
				if got := schemaJSON(t, src, def, true); got != want {
					t.Fatalf("source schema changed after parsing %q", input)
				}
				_, err := outputSchemaPrototype(loaded, def, true)
				wantSchemaNeedsSource(t, err)
				_, err = outputSchemaPrototype(loaded, def, false)
				wantSchemaNeedsSource(t, err)
			}
		})
	}
}

// marshalWithTrailing writes a compiled grammar in parse mode with section after the module, where
// the format has nothing more, for the tests of data after the module.
func marshalWithTrailing(prog *Program, start string, section []byte) []byte {
	m := prog.Module()
	w := &bytecode.Encoder{Strs: map[string]int{}}
	for i, s := range m.Strings {
		w.Strs[s] = i
		w.List = append(w.List, s)
	}
	w.Str(start)
	w.WriteModule(m)
	w.Body.Write(section)
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(compiledVersion)
	out.WriteByte(byte(CompiledParse))
	out.Write(binary.AppendUvarint(nil, uint64(m.InstructionSetVersion())))
	w.Finish(&out)
	return out.Bytes()
}

// TestCompiledSchemaSectionErrors checks that a compiled grammar has no place for a schema: a schema
// document after the module, valid or not, is data the format does not have, and the file is
// rejected.
func TestCompiledSchemaSectionErrors(t *testing.T) {
	p := mustCompile(t, `@start(default)
rule main = "a"
@start
rule other = "b"`)
	if _, _, err := LoadProgram(marshalWithTrailing(p, "main", nil)); err != nil {
		t.Fatalf("file without trailing data: %v", err)
	}
	s, err := outputSchemaPrototype(p, "main", true)
	if err != nil {
		t.Fatal(err)
	}
	section, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, section string
	}{
		{"valid schema", string(section)},
		{"not JSON", "x"},
		{"unknown member", strings.Replace(string(section), `"rules"`, `"extra":1,"rules"`, 1)},
		{"trailing data", string(section) + " {}"},
		{"missing start rule", strings.Replace(string(section), `"start":"main"`, `"start":"absent"`, 1)},
		{"unknown rule", strings.Replace(string(section), `"main"`, `"absent"`, 1)},
		{"start count", dropLastStart.ReplaceAllString(string(section), "]}")},
		{"default start", strings.Replace(string(section), `"start":"main"`, `"start":"other"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := LoadProgram(marshalWithTrailing(p, "main", []byte(tc.section)))
			if err == nil || !strings.Contains(err.Error(), "trailing bytes") {
				t.Fatalf("error %v, want trailing bytes", err)
			}
			if !strings.HasPrefix(err.Error(), "invalid compiled grammar") {
				t.Fatalf("error %q lacks the compiled grammar prefix", err)
			}
		})
	}
}

// dropLastStart matches the end of the last rule entry of the schema of the two-start grammar of
// TestCompiledSchemaSectionErrors.
var dropLastStart = regexp.MustCompile(`,\{"name":"other".*\]\}$`)

// TestCompiledSchemaUnavailable checks that a loaded file in each mode reports that its schema
// needs the grammar source, while the source exports it.
func TestCompiledSchemaUnavailable(t *testing.T) {
	p := mustCompile(t, `rule main = "a"`)
	if _, err := outputSchemaPrototype(p, "main", false); err != nil {
		t.Fatalf("source schema: %v", err)
	}
	for _, mode := range allModes {
		loaded := saveLoad(t, p, "main", mode)
		_, err := outputSchemaPrototype(loaded, "main", false)
		wantSchemaNeedsSource(t, err)
		// A loaded file is saved again only in its own mode, never with a schema.
		for _, other := range allModes {
			if _, err := loaded.Marshal("main", other); (err == nil) != (other == mode) {
				t.Fatalf("%s mode file saved in %s mode: %v", mode, other, err)
			}
		}
	}
}

// TestCompiledSchemaWithAST checks that a file of another container version, such as the earlier
// one that held the grammar and a schema, is rejected and needs to be compiled again.
func TestCompiledSchemaWithAST(t *testing.T) {
	p := mustCompile(t, `rule main = "a"`)
	for _, version := range []byte{compiledVersion - 1, compiledVersion + 1} {
		data, err := p.MarshalBinary("main")
		if err != nil {
			t.Fatal(err)
		}
		data[len(compiledMagic)] = version
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		_, _, err = LoadProgram(data)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unsupported compiled grammar version %d", version)) || !strings.Contains(err.Error(), "compile the grammar again") {
			t.Fatalf("container version %d: %v", version, err)
		}
	}
}

// TestCompiledSchemaStartNames checks that the source accepts any rule as the start of the complete
// schema and only start rules otherwise, and that a loaded file refuses every name the same way,
// because it holds no schema.
func TestCompiledSchemaStartNames(t *testing.T) {
	p := mustCompile(t, `@start
rule main = a:"x"
rule helper = "y"`)
	loaded := saveLoad(t, p, "main", CompiledParse)
	for _, start := range []string{"helper", "nosuch"} {
		_, wantErr := outputSchemaPrototype(p, start, true)
		if (wantErr == nil) != (start == "helper") {
			t.Fatalf("start %q: source error %v", start, wantErr)
		}
		if wantErr != nil && wantErr.Error() != `unknown schema start rule "nosuch"` {
			t.Fatalf("start %q: source error %v", start, wantErr)
		}
		_, gotErr := outputSchemaPrototype(loaded, start, true)
		wantSchemaNeedsSource(t, gotErr)
	}
	// Without all rules only start rules export: a rule that is not a start rule is an unknown start.
	for _, start := range []string{"helper", "nosuch"} {
		_, wantErr := outputSchemaPrototype(p, start, false)
		if wantErr == nil || wantErr.Error() != fmt.Sprintf("unknown schema start rule %q", start) {
			t.Fatalf("start %q without all rules: source error %v", start, wantErr)
		}
		_, gotErr := outputSchemaPrototype(loaded, start, false)
		wantSchemaNeedsSource(t, gotErr)
	}
	if _, err := outputSchemaPrototype(p, "main", false); err != nil {
		t.Fatalf("start rule main: %v", err)
	}
}
