package types

import (
	"strings"
)

// Strings returns the values of the fields of the file's records, leaving out blank records, as Records
// does.
func (f *File) Strings() [][]string {
	out := make([][]string, 0, len(f.Records))
	// One backing array for all the fields, as encoding/csv's ReadAll allocates one per record.
	n := 0
	for _, r := range f.Records {
		n += len(r.Fields)
	}
	vals := make([]string, 0, n)
	for _, r := range f.Records {
		if r.Blank() {
			continue
		}
		start := len(vals)
		for _, fl := range r.Fields {
			vals = append(vals, fl.Value())
		}
		out = append(out, vals[start:len(vals):len(vals)])
	}
	return out
}

// Strings returns the values of the record's fields.
func (r *Record) Strings() []string {
	s := make([]string, len(r.Fields))
	for i, f := range r.Fields {
		s[i] = f.Value()
	}
	return s
}

// Blank reports whether the record is an empty line: a single empty field without quotes. Records and
// Table leave blank records out, as encoding/csv does.
func (r *Record) Blank() bool { return len(r.Fields) == 1 && r.Fields[0].TNode.Value == "" }

// Quoted reports whether the field is enclosed in double quotes.
func (f *Field) Quoted() bool { return f.TNode.Value != "" && f.TNode.Value[0] == '"' }

// Value returns the field's value: the text of an unquoted field, or the text between the quotes of a
// quoted field with each "" replaced by ".
func (f *Field) Value() string {
	t := f.TNode.Value
	if t == "" || t[0] != '"' {
		return t
	}
	t = t[1 : len(t)-1]
	if !strings.Contains(t, `""`) {
		return t
	}
	return strings.ReplaceAll(t, `""`, `"`)
}
