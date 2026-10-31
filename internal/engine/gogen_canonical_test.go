package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gentest"
)

// TestCanonicalValues checks the canonical forms on hand-written values: a pair of values that
// agree, and pairs that differ in a nil field inside a struct, in a tuple slot, between null and
// an empty list, and in the range of a struct.
func TestCanonicalValues(t *testing.T) {
	t.Parallel()
	engine := `{"type":"R","start":0,"end":3,"fields":{"P":[1,"s"],"Start":0,"Q":{"type":"T","start":0,"end":1,"fields":{"Text":"a"}},"N":null,"L":[]}}`
	agree := `{"struct":"R","span":true,"start":0,"end":3,"fields":{"P":{"struct":"Tuple2[int,string]","fields":{"E0":1,"E1":"s"}},"Start_":0,"Q":{"struct":"T","span":true,"start":0,"end":1,"fields":{"Text":"a"}},"N":null,"L":[]}}`
	cases := []struct {
		name, typed string
		equal       bool
	}{
		{"agree", agree, true},
		{"nil field inside a struct", strings.Replace(agree, `"Q":{"struct":"T","span":true,"start":0,"end":1,"fields":{"Text":"a"}}`, `"Q":null`, 1), false},
		{"wrong value in a tuple slot", strings.Replace(agree, `"E1":"s"`, `"E1":"t"`, 1), false},
		{"null against an empty list", strings.Replace(agree, `"N":null`, `"N":[]`, 1), false},
		{"struct range differs", strings.Replace(agree, `"start":0,"end":1,"fields":{"Text"`, `"start":0,"end":2,"fields":{"Text"`, 1), false},
	}
	var want any
	if err := json.Unmarshal([]byte(engine), &want); err != nil {
		t.Fatal(err)
	}
	canon, err := gentest.CanonicalEngine(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		err := gentest.CanonicalTypedMatches(json.RawMessage(c.typed), canon)
		if (err == nil) != c.equal {
			t.Errorf("%s: equal = %v, want %v (%v)", c.name, err == nil, c.equal, err)
		}
	}
	// An engine struct that leaves fields unset: the typed zero values of those fields match, but a
	// non-zero value or an empty list does not.
	unset := `{"type":"R","start":0,"end":3,"fields":{"Start":0}}`
	if err := json.Unmarshal([]byte(unset), &want); err != nil {
		t.Fatal(err)
	}
	canon, err = gentest.CanonicalEngine(want)
	if err != nil {
		t.Fatal(err)
	}
	unsetCases := []struct {
		name, typed string
		equal       bool
	}{
		{"unset fields at their zero values", `{"struct":"R","span":true,"start":0,"end":3,"fields":{"Start_":0,"N":null,"S":"","C":0,"B":false}}`, true},
		{"unset field with a non-zero value", `{"struct":"R","span":true,"start":0,"end":3,"fields":{"Start_":0,"C":2}}`, false},
		{"unset field with an empty list", `{"struct":"R","span":true,"start":0,"end":3,"fields":{"Start_":0,"L":[]}}`, false},
	}
	for _, c := range unsetCases {
		err := gentest.CanonicalTypedMatches(json.RawMessage(c.typed), canon)
		if (err == nil) != c.equal {
			t.Errorf("%s: equal = %v, want %v (%v)", c.name, err == nil, c.equal, err)
		}
	}
}

// TestEngineStructKeepsExplicitZeroFields pins that the engine writes the key of a struct field set
// to a zero value: nil, "", 0, false and an empty list. gentest.CanonicalDifferences treats an absent key of
// a struct as an unset field, which is sound only while this holds.
func TestEngineStructKeepsExplicitZeroFields(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type Z = { P *node[string]; S string; I int; B bool; L []node[string] }
rule main Z = x:"a" => Z{P: nil, S: "", I: 0, B: false, L: []node[string]{}}`)
	n, err := prog.Parse("main", "a")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"P": "null", "S": `""`, "I": "0", "B": "false", "L": "[]"}
	for name, text := range want {
		got, ok := v.Fields[name]
		if !ok {
			t.Errorf("field %s is absent from %s", name, data)
			continue
		}
		if string(got) != text {
			t.Errorf("field %s = %s, want %s", name, got, text)
		}
	}
	if len(v.Fields) != len(want) {
		t.Errorf("fields = %s, want exactly %d keys", data, len(want))
	}
}

// TestCanonicalUnsetFieldsAreDeclaredOnly checks that the exemption for an absent key applies only to
// the fields of a struct: a zero-valued key that the engine omits elsewhere is still a difference.
func TestCanonicalUnsetFieldsAreDeclaredOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		typed, eng map[string]any
		diffs      int
	}{
		{"unset declared field", map[string]any{"type": "R", "fields": map[string]any{"N": nil, "S": ""}}, map[string]any{"type": "R", "fields": map[string]any{}}, 0},
		{"zero key outside fields", map[string]any{"type": "R", "fields": map[string]any{}, "Extra": 0.0}, map[string]any{"type": "R", "fields": map[string]any{}}, 1},
		{"non-zero unset field", map[string]any{"type": "R", "fields": map[string]any{"C": 2.0}}, map[string]any{"type": "R", "fields": map[string]any{}}, 1},
	}
	for _, c := range cases {
		var diffs []string
		gentest.CanonicalDifferences(c.typed, c.eng, "$", false, &diffs)
		if len(diffs) != c.diffs {
			t.Errorf("%s: %d differences %v, want %d", c.name, len(diffs), diffs, c.diffs)
		}
	}
}

// TestCanonicalGenericNodeType checks that a generic typed node matches an engine node with or
// without a named type, and that a named typed node still differs from another named type or none.
func TestCanonicalGenericNodeType(t *testing.T) {
	t.Parallel()
	node := func(typ any) map[string]any {
		n := map[string]any{"start": 0.0, "end": 1.0, "value": "a"}
		if typ != nil {
			n["type"] = typ
		}
		return n
	}
	cases := []struct {
		name       string
		typed, eng map[string]any
		diffs      int
	}{
		{"generic and named", node(gentest.TypedGenericNode), node("Num"), 0},
		{"generic and plain", node(gentest.TypedGenericNode), node(nil), 0},
		{"named and other named", node("Num"), node("Word"), 1},
		{"named and plain", node("Num"), node(nil), 1},
		{"generic with another value", map[string]any{"start": 0.0, "end": 1.0, "value": "b", "type": gentest.TypedGenericNode}, node("Num"), 1},
	}
	for _, c := range cases {
		var diffs []string
		gentest.CanonicalDifferences(c.typed, c.eng, "$", false, &diffs)
		if len(diffs) != c.diffs {
			t.Errorf("%s: %d differences %v, want %d", c.name, len(diffs), diffs, c.diffs)
		}
	}
}
