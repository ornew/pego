package engine

import (
	"testing"
)

var aliasCases = []genCase{
	{"terminal singleton union", `
type Num terminal
def main: Num | Num = "é"+ $$`, []string{"éé", "", "x"}},
	{"alias terminal", `
type Num terminal
type Alias = Num
type Deep = Alias
def main: Deep = "é"+ $$`, []string{"éé", "", "x"}},
	{"alias terminals in structs", `
type Num terminal
type Alias = Num
type Deep = Alias
type Box struct { Value Num }
def main: Box = v:num $$ -> new Box{Value: $v}
def num: Deep = "é"+`, []string{"éé", "", "x"}},
	{"alias constructors", `
type Num terminal
type NumAlias = Num
type P struct { Value Num, N int }
type Alias = P
type Deep = Alias
def main: Deep = v:num $$ -> new Deep{Value: $v, N: 7}
def num: NumAlias = "é"+`, []string{"éé", "", "x"}},
	{"alias singleton unions", `
type Num terminal
type Alias = Num | Num
type P struct { Value Alias }
type Deep = P | P
def main: Deep = v:num $$ -> new Deep{Value: $v}
def num: Alias = "é"+`, []string{"éé", "", "x"}},
	{"alias terminal action", `
type Num terminal
type Alias = Num
def main: Alias = v:num $$ -> $v
def num: Num = "é"+`, []string{"éé", "", "x"}},
}

var aliasTypedGoldens = map[string]string{
	"terminal singleton union":   `{"Start":0,"End":2,"Text":"éé"}`,
	"alias terminal":             `{"Start":0,"End":2,"Text":"éé"}`,
	"alias terminals in structs": `{"Start":0,"End":2,"Value":{"Start":0,"End":2,"Text":"éé"}}`,
	"alias constructors":         `{"Start":0,"End":2,"Value":{"Start":0,"End":2,"Text":"éé"},"N":7}`,
	"alias singleton unions":     `{"Start":0,"End":2,"Value":{"Start":0,"End":2,"Text":"éé"}}`,
	"alias terminal action":      `{"Start":0,"End":2,"Text":"éé"}`,
}

func TestAliasNodeIdentity(t *testing.T) {
	for _, c := range aliasCases {
		t.Run(c.name, func(t *testing.T) {
			for _, options := range []Options{{}, {DisableMemo: true}, {NoTypeCheck: true}} {
				prog := compile(t, c.src, options)
				for _, unit := range []Unit{CodePoints, Bytes} {
					n, err := prog.ParseWith("main", "éé", ParseOptions{Unit: unit})
					if err != nil {
						t.Fatal(err)
					}
					wantType := "Num"
					switch c.name {
					case "alias terminals in structs":
						wantType = "Box"
					case "alias constructors", "alias singleton unions":
						wantType = "P"
					}
					if n.Type() != wantType {
						t.Errorf("%s: got type %s, want %s", unit, n.Type(), wantType)
					}
					terminal := n
					if v, ok := n.Fields.Get("Value"); ok {
						terminal, _ = v.(*Node)
					}
					if terminal == nil || terminal.Type() != "Num" || terminal.Text != "éé" {
						t.Errorf("%s: expected a Num terminal, got %s", unit, resultJSON(terminal, nil))
					}
				}
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
						t.Fatal(err)
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
