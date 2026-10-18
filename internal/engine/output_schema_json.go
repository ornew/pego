package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Standard decoding validates syntax, value types and maximum nesting before
// the allocation-free common key scan checks exact names and duplicate keys.
// The receiver is published only after both checks and graph validation pass.
func decodeOutputSchemaJSON(data []byte) (OutputSchema, error) {
	type plain OutputSchema
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return OutputSchema{}, err
	}
	keys := outputSchemaJSONKeys{data: data}
	if err := keys.object(schemaJSONEnvelope); err != nil {
		return OutputSchema{}, err
	}
	return OutputSchema(value), nil
}

type schemaJSONObject uint8

const (
	schemaJSONEnvelope schemaJSONObject = iota
	schemaJSONType
	schemaJSONRule
	schemaJSONShape
	schemaJSONField
	schemaJSONCapture
)

var outputSchemaJSONFields = [...][]string{
	{"format", "version", "start", "positionUnitIndependent", "sourceInfo", "types", "rules"},
	{"id", "name", "origin", "shape"},
	{"name", "origin", "result"},
	{"kind", "ref", "scalar", "element", "elements", "alternatives", "fields", "value", "captures"},
	{"name", "shape"},
	{"name", "paths", "side", "shape"},
}

type outputSchemaJSONKeys struct {
	data []byte
	at   int
}

func (s *outputSchemaJSONKeys) space() {
	for s.at < len(s.data) {
		switch s.data[s.at] {
		case ' ', '\t', '\r', '\n':
			s.at++
		default:
			return
		}
	}
}

// The typed decode above has already proved these token and container bounds.
func (s *outputSchemaJSONKeys) stringBytes() ([]byte, error) {
	start := s.at
	s.at++
	escaped := false
	for s.data[s.at] != '"' {
		if s.data[s.at] == '\\' {
			escaped = true
			s.at++
		}
		s.at++
	}
	s.at++
	if !escaped {
		return s.data[start+1 : s.at-1], nil
	}
	var value string
	if err := json.Unmarshal(s.data[start:s.at], &value); err != nil {
		return nil, err
	}
	return []byte(value), nil
}

func (s *outputSchemaJSONKeys) scalar() error {
	s.space()
	if s.data[s.at] == '"' {
		_, err := s.stringBytes()
		return err
	}
	for s.at < len(s.data) {
		switch s.data[s.at] {
		case ',', '}', ']', ' ', '\t', '\r', '\n':
			return nil
		default:
			s.at++
		}
	}
	return nil
}

func (s *outputSchemaJSONKeys) object(kind schemaJSONObject) error {
	s.space()
	if s.data[s.at] == 'n' {
		s.at += len("null")
		return nil
	}
	s.at++ // {
	var seen uint64
	for {
		s.space()
		if s.data[s.at] == '}' {
			s.at++
			return nil
		}
		key, err := s.stringBytes()
		if err != nil {
			return err
		}
		field := -1
		for i, name := range outputSchemaJSONFields[kind] {
			if bytes.Equal(key, []byte(name)) {
				field = i
				break
			}
		}
		if field < 0 {
			return fmt.Errorf("json: unknown field %q in output schema", key)
		}
		bit := uint64(1) << field
		if seen&bit != 0 {
			return fmt.Errorf("duplicate output schema key %q", key)
		}
		seen |= bit
		s.space()
		s.at++ // :
		s.space()
		switch {
		case kind == schemaJSONEnvelope && field == 5:
			err = s.array(schemaJSONType)
		case kind == schemaJSONEnvelope && field == 6:
			err = s.array(schemaJSONRule)
		case kind == schemaJSONType && field == 3,
			kind == schemaJSONRule && field == 2,
			kind == schemaJSONField && field == 1,
			kind == schemaJSONCapture && field == 3,
			kind == schemaJSONShape && (field == 3 || field == 7):
			err = s.object(schemaJSONShape)
		case kind == schemaJSONShape && (field == 4 || field == 5):
			err = s.array(schemaJSONShape)
		case kind == schemaJSONShape && field == 6:
			err = s.array(schemaJSONField)
		case kind == schemaJSONShape && field == 8:
			err = s.array(schemaJSONCapture)
		case kind == schemaJSONCapture && field == 1:
			s.numbers()
		default:
			err = s.scalar()
		}
		if err != nil {
			return err
		}
		s.space()
		if s.data[s.at] == ',' {
			s.at++
		}
	}
}

// numbers skips a value made of arrays and numbers, such as capture paths.
func (s *outputSchemaJSONKeys) numbers() {
	depth := 0
	for s.at < len(s.data) {
		switch s.data[s.at] {
		case '[':
			depth++
		case ']':
			depth--
		case ',', '}':
			if depth == 0 {
				return
			}
		}
		s.at++
		if depth == 0 && s.data[s.at-1] == ']' {
			return
		}
	}
}

func (s *outputSchemaJSONKeys) array(kind schemaJSONObject) error {
	s.space()
	if s.data[s.at] == 'n' {
		s.at += len("null")
		return nil
	}
	s.at++ // [
	for {
		s.space()
		if s.data[s.at] == ']' {
			s.at++
			return nil
		}
		if err := s.object(kind); err != nil {
			return err
		}
		s.space()
		if s.data[s.at] == ',' {
			s.at++
		}
	}
}
