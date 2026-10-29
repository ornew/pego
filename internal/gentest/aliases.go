package gentest

var AliasCases = []Case{
	{"terminal singleton union", `type Num = node[string]
rule main Num | Num = ("é"+ $$) #atomic`, []string{"éé", "", "x"}},
	{"alias terminal", `type Num = node[string]
type Alias ~ Num
type Deep ~ Alias
rule main Deep = ("é"+ $$) #atomic`, []string{"éé", "", "x"}},
	{"alias terminals in structs", `type Num = node[string]
type Alias ~ Num
type Deep ~ Alias
type Box = { Value Num }
rule main Box = v:num $$ => Box{Value: $v}
rule num Deep = ("é"+) #atomic`, []string{"éé", "", "x"}},
	{"alias constructors", `type Num = node[string]
type NumAlias ~ Num
type P = { Value Num; N int }
type Alias ~ P
type Deep ~ Alias
rule main Deep = v:num $$ => Deep{Value: $v, N: 7}
rule num NumAlias = ("é"+) #atomic`, []string{"éé", "", "x"}},
	{"alias singleton unions", `type Num = node[string]
type Alias ~ Num | Num
type P = { Value Alias }
type Deep ~ P | P
rule main Deep = v:num $$ => Deep{Value: $v}
rule num Alias = ("é"+) #atomic`, []string{"éé", "", "x"}},
	{"alias terminal action", `type Num = node[string]
type Alias ~ Num
rule main Alias = v:num $$ => $v
rule num Num = ("é"+) #atomic`, []string{"éé", "", "x"}},
}
var aliasTypedGoldens = map[string]string{
	"terminal singleton union":   `{"Start":0,"End":2,"Value":"éé"}`,
	"alias terminal":             `{"Start":0,"End":2,"Value":"éé"}`,
	"alias terminals in structs": `{"Start":0,"End":2,"Value":{"Start":0,"End":2,"Value":"éé"}}`,
	"alias constructors":         `{"Start":0,"End":2,"Value":{"Start":0,"End":2,"Value":"éé"},"N":7}`,
	"alias singleton unions":     `{"Start":0,"End":2,"Value":{"Start":0,"End":2,"Value":"éé"}}`,
	"alias terminal action":      `{"Start":0,"End":2,"Value":"éé"}`,
}
