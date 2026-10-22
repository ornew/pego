package engine

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// schemaJSON exports the schema of p for start, or every rule with allRules, as canonical JSON.
func schemaJSON(t *testing.T, p *Program, start string, allRules bool) string {
	t.Helper()
	s, err := p.OutputSchemaPrototype(start, allRules)
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

// TestCompiledSchemaMatchesSource checks that a loaded parser exports the schema of its source for
// every start rule and for all rules, with and without the AST, and that the AST-less file keeps
// its schema section when it is saved again.
func TestCompiledSchemaMatchesSource(t *testing.T) {
	for _, c := range schemaCorpus(t) {
		for _, options := range []Options{{SourceInfo: true}, {DefaultValue: true}} {
			t.Run(c.name, func(t *testing.T) {
				src := compileBare(t, c.src, options)
				if len(src.starts) == 0 {
					t.Fatal("no start rule")
				}
				def := src.DefaultStart()
				withAST, err := src.MarshalBinary(def)
				if err != nil {
					t.Fatal(err)
				}
				bare, err := src.MarshalBinaryWith(def, MarshalOptions{OmitAST: true, Schema: true})
				if err != nil {
					t.Fatal(err)
				}
				if bare[len(compiledMagic)] != compiledVersion || withAST[len(compiledMagic)] != compiledVersion {
					t.Fatal("saves are not the current format")
				}
				loadedAST, _, err := LoadProgram(withAST, Options{})
				if err != nil {
					t.Fatal(err)
				}
				loadedBare, _, err := LoadProgram(bare, Options{})
				if err != nil {
					t.Fatal(err)
				}
				again, err := loadedBare.MarshalBinaryWith(def, MarshalOptions{OmitAST: true})
				if err != nil || !bytes.Equal(bare, again) {
					t.Fatalf("AST-less file did not keep its schema section on save: %v", err)
				}
				for _, start := range append(append([]string{}, src.starts...), "") {
					name := start
					if name == "" {
						name = def
					}
					for _, all := range []bool{false, true} {
						want := schemaJSON(t, src, name, all)
						if got := schemaJSON(t, loadedAST, name, all); got != want {
							t.Fatalf("with AST, start %s all %t:\n%s\nwant\n%s", name, all, got, want)
						}
						if got := schemaJSON(t, loadedBare, name, all); got != want {
							t.Fatalf("without AST, start %s all %t:\n%s\nwant\n%s", name, all, got, want)
						}
					}
				}
			})
		}
	}
}

// TestCompiledSchemaBackendAgreement parses the inputs of each grammar with every backend and checks
// that the schema exported afterwards is the same. The closure backend runs on the source program;
// the two bytecode backends run on the file without the AST, which has no other backend.
func TestCompiledSchemaBackendAgreement(t *testing.T) {
	for _, c := range genCorpus(t) {
		t.Run(c.name, func(t *testing.T) {
			src := compile(t, c.src)
			def := src.DefaultStart()
			want := schemaJSON(t, src, def, true)
			wantStart := schemaJSON(t, src, def, false)
			bare, err := src.MarshalBinaryWith(def, MarshalOptions{OmitAST: true, Schema: true})
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(bare, Options{})
			if err != nil {
				t.Fatal(err)
			}
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
				if got := schemaJSON(t, loaded, def, true); got != want {
					t.Fatalf("loaded schema differs after parsing %q", input)
				}
				if got := schemaJSON(t, loaded, def, false); got != wantStart {
					t.Fatalf("loaded start schema differs after parsing %q", input)
				}
			}
		})
	}
}

// marshalWithSchemaSection writes a file with the given schema section, with the AST when withAST is
// set, for the tests of malformed sections and of the section with the AST.
func marshalWithSchemaSection(prog *Program, start string, section []byte, withAST bool) []byte {
	m := prog.Module()
	w := &encoder{strs: map[string]int{}}
	for i, s := range m.Strings {
		w.strs[s] = i
		w.list = append(w.list, s)
	}
	w.str(start)
	w.str(prog.pkg)
	w.module(m)
	w.bool(false) // no recognition bundle
	w.bool(true)
	w.uint(len(section))
	w.body.Write(section)
	w.bool(withAST)
	if withAST {
		w.statements(prog)
	}
	var out bytes.Buffer
	out.WriteString(compiledMagic)
	out.WriteByte(compiledVersion)
	out.Write(binary.AppendUvarint(nil, uint64(m.instructionSetVersion())))
	w.finish(&out)
	return out.Bytes()
}

func TestCompiledSchemaSectionErrors(t *testing.T) {
	p := compile(t, `@start(default)
rule main = "a"
@start
rule other = "b"`)
	section, err := p.encodeSchemaSection()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(marshalWithSchemaSection(p, "main", section, false), Options{}); err != nil {
		t.Fatalf("valid section: %v", err)
	}
	for _, tc := range []struct {
		name, section, want string
	}{
		{"not JSON", "x", "invalid schema section"},
		{"unknown member", strings.Replace(string(section), `"starts"`, `"extra":1,"starts"`, 1), "unknown field"},
		{"trailing data", string(section) + " {}", "trailing data"},
		{"missing start rule", strings.Replace(string(section), `"start":"other"`, `"start":"absent"`, 1), "where \"other\" is expected"},
		{"unknown rule", strings.Replace(string(section), `"rules":["main"]`, `"rules":["absent"]`, 1), "unknown rule"},
		{"unknown type", strings.Replace(string(section), `"types":["`, `"types":["declared/Absent","`, 1), "unknown type"},
		{"start count", dropLastStart.ReplaceAllString(string(section), "]}"), "start rules in the schema for"},
		// The schema's own start rule is the first "start" member of the section.
		{"default start", strings.Replace(string(section), `"start":"main"`, `"start":"other"`, 1), "the schema starts at \"other\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := LoadProgram(marshalWithSchemaSection(p, "main", []byte(tc.section), false), Options{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
			if !strings.HasPrefix(err.Error(), "invalid compiled grammar: invalid schema section: ") {
				t.Fatalf("error %q lacks the compiled grammar prefix", err)
			}
		})
	}
}

// dropLastStart matches the last start rule entry of a schema section of the two-start grammar of
// TestCompiledSchemaSectionErrors.
var dropLastStart = regexp.MustCompile(`,\{"start":"other".*\]\}$`)

// TestCompiledSchemaUnavailable checks that an AST-less file without a schema section reports the
// option that adds one, and that the schema is not stored with the AST.
func TestCompiledSchemaUnavailable(t *testing.T) {
	p := compile(t, `rule main = "a"`)
	bare, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(bare, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.OutputSchemaPrototype("main", false); !errors.Is(err, ErrSchemaUnavailable) || !strings.Contains(err.Error(), "-schema") {
		t.Fatalf("AST-less file without a schema: %v", err)
	}
	if _, err := p.MarshalBinaryWith("main", MarshalOptions{Schema: true}); err == nil {
		t.Fatal("schema stored with the AST")
	}
	// Saving a file without a schema section with the schema option derives it from nothing.
	if _, err := loaded.MarshalBinaryWith("main", MarshalOptions{OmitAST: true, Schema: true}); !errors.Is(err, ErrSchemaUnavailable) {
		t.Fatalf("schema added to a file that has none: %v", err)
	}
}

// TestCompiledSchemaWithAST checks that a file with the AST refuses a schema section: its schema is
// derived from the AST on load, so a stored one would be a second source of the same schema.
func TestCompiledSchemaWithAST(t *testing.T) {
	p := compile(t, `rule main = "a"`)
	section, err := p.encodeSchemaSection()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = LoadProgram(marshalWithSchemaSection(p, "main", section, true), Options{})
	if err == nil || !strings.Contains(err.Error(), "a schema section with the AST") {
		t.Fatalf("file with the AST and a schema section: %v", err)
	}
}

// TestCompiledSchemaStartNames checks that the complete schema of an AST-less file accepts the same
// start names as the source it was saved from: any rule, and only unknown names are rejected with
// the same error.
func TestCompiledSchemaStartNames(t *testing.T) {
	p := compile(t, `@start
rule main = a:"x"
rule helper = "y"`)
	bare, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true, Schema: true})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(bare, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []string{"helper", "nosuch"} {
		want, wantErr := p.OutputSchemaPrototype(start, true)
		got, gotErr := loaded.OutputSchemaPrototype(start, true)
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("start %q: source error %v, stored error %v", start, wantErr, gotErr)
		}
		if gotErr != nil {
			if gotErr.Error() != wantErr.Error() || gotErr.Error() != `unknown schema start rule "nosuch"` {
				t.Fatalf("start %q: source error %v, stored error %v", start, wantErr, gotErr)
			}
			continue
		}
		wj, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		gj, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(wj, gj) {
			t.Fatalf("start %q: stored all-rules schema differs from the source", start)
		}
	}
}
