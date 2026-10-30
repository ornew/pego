// Package editfuzz checks incremental parsing with a grammar against fresh parses by applying sequences of
// edits to a pego.Document, and saves a failing sequence as a file that replays it exactly.
//
// A Sequence is a versioned event log: the initial text, then edit, undo, redo and checkpoint operations.
// At the start and at each checkpoint, the Document's parse result is compared with a fresh parse of the
// Document's current text: the values with their spans, and the errors with their positions, expectations
// and messages. The log stores the text and the replacements as exact bytes, including invalid UTF-8, so
// replaying it does not depend on the random generator that produced it.
//
// Fuzz generates sequences from a seed and an initial-text corpus, Replay runs a saved sequence and
// Minimize shortens a failing one while it fails the same way. The design is described in
// docs/design/records/037-replayable-edit-fuzzing.md.
package editfuzz

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"

	"github.com/ornew/pego"
)

// Version is the version of the Sequence format that this package reads and writes.
const Version = 1

// Kind is the kind of an Operation.
type Kind string

// The kinds of operations.
const (
	// Edit replaces [Start, End) of the current text, in the sequence's unit, with Text.
	Edit Kind = "edit"
	// Undo restores the text before the latest edit that is not undone.
	Undo Kind = "undo"
	// Redo reapplies the latest undone edit. A new edit discards the edits that can be redone.
	Redo Kind = "redo"
	// Checkpoint parses the Document and compares the result with a fresh parse.
	Checkpoint Kind = "checkpoint"
)

// An Operation is one event of a Sequence. Start, End and Text are used only by edits; the positions
// refer to the text immediately before the operation.
type Operation struct {
	Kind  Kind
	Start int
	End   int
	Text  []byte
}

// MarshalJSON writes an edit with its range and base64 replacement and other operations as their kind.
func (o Operation) MarshalJSON() ([]byte, error) {
	switch o.Kind {
	case Edit:
		if o.Start < 0 || o.End < o.Start {
			return nil, fmt.Errorf("invalid edit range [%d,%d)", o.Start, o.End)
		}
		text := o.Text
		if text == nil {
			text = []byte{}
		}
		return json.Marshal(struct {
			Kind  Kind   `json:"kind"`
			Start int    `json:"start"`
			End   int    `json:"end"`
			Text  []byte `json:"text_base64"`
		}{o.Kind, o.Start, o.End, text})
	case Undo, Redo, Checkpoint:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
		}{o.Kind})
	}
	return nil, fmt.Errorf("unknown operation kind %q", o.Kind)
}

// UnmarshalJSON reads an operation, requiring exactly the fields of its kind.
func (o *Operation) UnmarshalJSON(data []byte) error {
	fields, err := object(data)
	if err != nil {
		return err
	}
	var kind Kind
	if err := field(fields, "kind", &kind); err != nil {
		return err
	}
	switch kind {
	case Edit:
		if err := only(fields, "kind", "start", "end", "text_base64"); err != nil {
			return err
		}
		op := Operation{Kind: Edit}
		if err := errors.Join(field(fields, "start", &op.Start), field(fields, "end", &op.End),
			field(fields, "text_base64", &op.Text)); err != nil {
			return err
		}
		if op.Start < 0 || op.End < op.Start {
			return fmt.Errorf("invalid edit range [%d,%d)", op.Start, op.End)
		}
		if op.Text == nil {
			op.Text = []byte{}
		}
		*o = op
	case Undo, Redo, Checkpoint:
		if err := only(fields, "kind"); err != nil {
			return fmt.Errorf("%s takes no range or text: %w", kind, err)
		}
		*o = Operation{Kind: kind}
	default:
		return fmt.Errorf("unknown operation kind %q", kind)
	}
	return nil
}

// A Sequence is a replayable edit history for one grammar, start rule, backend and unit.
type Sequence struct {
	// GrammarSHA256 is the hex SHA-256 hash of the exact grammar source bytes; replay refuses other
	// sources.
	GrammarSHA256 string
	Start         string
	Backend       pego.Backend
	Unit          pego.PositionUnit
	// Limits bound the Documents and the fresh parses of a replay. A Document that reuses earlier work
	// spends less than a fresh parse, so a check in which either parse exhausts its budget compares
	// nothing. The saved format does not record the limits, so a replay supplies them again.
	Limits pego.Limits
	// Seed is the seed that generated the sequence, kept only as provenance: replay does not use it.
	Seed       uint64
	Initial    []byte
	Operations []Operation
}

type sequenceJSON struct {
	Version              int         `json:"version"`
	GrammarSHA256        string      `json:"grammar_sha256"`
	GrammarNormalization string      `json:"grammar_normalization"`
	Start                string      `json:"start"`
	Backend              string      `json:"backend"`
	Unit                 string      `json:"unit"`
	Seed                 uint64      `json:"seed"`
	TextEncoding         string      `json:"text_encoding"`
	Initial              []byte      `json:"initial_base64"`
	Operations           []Operation `json:"operations"`
}

// GrammarHash returns the hash that a Sequence records for the grammar source src.
func GrammarHash(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}

// MarshalJSON writes the sequence in the versioned format, with text as base64 bytes.
func (s Sequence) MarshalJSON() ([]byte, error) {
	if err := checkHash(s.GrammarSHA256); err != nil {
		return nil, err
	}
	if s.Start == "" {
		return nil, errors.New("missing start rule")
	}
	backend, err := backendName(s.Backend)
	if err != nil {
		return nil, err
	}
	unit, err := unitName(s.Unit)
	if err != nil {
		return nil, err
	}
	initial, ops := s.Initial, s.Operations
	if initial == nil {
		initial = []byte{}
	}
	if ops == nil {
		ops = []Operation{}
	}
	return json.Marshal(sequenceJSON{
		Version:              Version,
		GrammarSHA256:        s.GrammarSHA256,
		GrammarNormalization: "none",
		Start:                s.Start,
		Backend:              backend,
		Unit:                 unit,
		Seed:                 s.Seed,
		TextEncoding:         "base64",
		Initial:              initial,
		Operations:           ops,
	})
}

// UnmarshalJSON reads a sequence, requiring exactly the fields of the format and rejecting unknown
// versions, backends and units.
func (s *Sequence) UnmarshalJSON(data []byte) error {
	fields, err := object(data)
	if err != nil {
		return err
	}
	if err := only(fields, sequenceFields...); err != nil {
		return err
	}
	var j sequenceJSON
	if err := errors.Join(field(fields, "version", &j.Version), field(fields, "grammar_sha256", &j.GrammarSHA256),
		field(fields, "grammar_normalization", &j.GrammarNormalization), field(fields, "start", &j.Start),
		field(fields, "backend", &j.Backend), field(fields, "unit", &j.Unit), field(fields, "seed", &j.Seed),
		field(fields, "text_encoding", &j.TextEncoding), field(fields, "initial_base64", &j.Initial),
		field(fields, "operations", &j.Operations)); err != nil {
		return err
	}
	if j.Version != Version {
		return fmt.Errorf("unsupported sequence version %d", j.Version)
	}
	if j.GrammarNormalization != "none" {
		return fmt.Errorf("unsupported grammar normalization %q", j.GrammarNormalization)
	}
	if j.TextEncoding != "base64" {
		return fmt.Errorf("unsupported text encoding %q", j.TextEncoding)
	}
	if err := checkHash(j.GrammarSHA256); err != nil {
		return err
	}
	if j.Start == "" {
		return errors.New("missing start rule")
	}
	backend, err := ParseBackend(j.Backend)
	if err != nil {
		return err
	}
	unit, err := ParseUnit(j.Unit)
	if err != nil {
		return err
	}
	if j.Initial == nil {
		j.Initial = []byte{}
	}
	if j.Operations == nil {
		j.Operations = []Operation{}
	}
	*s = Sequence{GrammarSHA256: j.GrammarSHA256, Start: j.Start, Backend: backend, Unit: unit, Seed: j.Seed,
		Initial: j.Initial, Operations: j.Operations}
	return nil
}

var sequenceFields = []string{"version", "grammar_sha256", "grammar_normalization", "start", "backend", "unit", "seed",
	"text_encoding", "initial_base64", "operations"}

// ParseBackend returns the backend named closure, bytecode or bytecode-iterative.
func ParseBackend(name string) (pego.Backend, error) {
	for _, b := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
		if b.String() == name {
			return b, nil
		}
	}
	return 0, fmt.Errorf("unknown backend %q", name)
}

// ParseUnit returns the unit named codepoints or bytes.
func ParseUnit(name string) (pego.PositionUnit, error) {
	for _, u := range []pego.PositionUnit{pego.CodePoints, pego.Bytes} {
		if u.String() == name {
			return u, nil
		}
	}
	return 0, fmt.Errorf("unknown unit %q", name)
}

func backendName(b pego.Backend) (string, error) {
	if b == pego.DefaultBackend {
		return "", errors.New("a sequence needs an explicit backend")
	}
	if _, err := ParseBackend(b.String()); err != nil {
		return "", err
	}
	return b.String(), nil
}

func checkHash(h string) error {
	if b, err := hex.DecodeString(h); err != nil || len(b) != sha256.Size || hex.EncodeToString(b) != h {
		return fmt.Errorf("invalid grammar hash %q: want %d lowercase hex digits", h, sha256.Size*2)
	}
	return nil
}

func unitName(u pego.PositionUnit) (string, error) {
	if u != pego.CodePoints && u != pego.Bytes {
		return "", fmt.Errorf("unknown unit %d", u)
	}
	return u.String(), nil
}

// object reads a JSON object into its members, rejecting duplicate keys, which encoding/json accepts.
func object(data []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	if t, err := d.Token(); err != nil {
		return nil, err
	} else if t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		key := t.(string)
		if _, ok := fields[key]; ok {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		fields[key] = v
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("trailing data after JSON value")
	}
	return fields, nil
}

// only reports a field of fields that is not one of names, which are case-sensitive, unlike in
// encoding/json.
func only(fields map[string]json.RawMessage, names ...string) error {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !slices.Contains(names, k) {
			return fmt.Errorf("unknown field %q", k)
		}
	}
	return nil
}

// field decodes the required field name into v.
func field(fields map[string]json.RawMessage, name string, v any) error {
	data, ok := fields[name]
	if !ok {
		return fmt.Errorf("missing field %q", name)
	}
	if string(data) == "null" {
		return fmt.Errorf("field %q is null", name)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("field %q: %w", name, err)
	}
	return nil
}
