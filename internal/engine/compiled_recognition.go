package engine

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// recognitionEntry separates public start names from internal value-free twins.
// NoValue is execution metadata and does not determine start eligibility.
type recognitionEntry struct {
	name  string
	index int
}

func (prog *Program) vmRule(vm *vmProgram, name string) *rule {
	if prog.recognitionStarts != nil {
		for _, entry := range prog.recognitionStarts {
			if entry.name == name {
				return vm.rules[entry.index]
			}
		}
		return nil
	}
	return vm.byName[name]
}

func moduleEntries(m *Module) []recognitionEntry {
	entries := []recognitionEntry{}
	seen := map[string]bool{}
	for i, r := range m.Rules {
		name := m.Strings[r.Name]
		if !seen[name] {
			seen[name] = true
			entries = append(entries, recognitionEntry{name, i})
		}
	}
	return entries
}

func encodeRecognition(prog *Program) []byte {
	m := prog.Module()
	w := &encoder{strs: map[string]int{}}
	for i, s := range m.Strings {
		w.strs[s] = i
		w.list = append(w.list, s)
	}
	w.module(m)
	entries := prog.recognitionStarts
	if entries == nil {
		entries = moduleEntries(m)
	}
	w.uint(len(entries))
	for _, entry := range entries {
		w.str(entry.name)
		w.uint(entry.index)
	}
	var out bytes.Buffer
	out.Write(binary.AppendUvarint(nil, uint64(m.instructionSetVersion())))
	w.finish(&out)
	// The containing file checksum already protects the entire bundle.
	return out.Bytes()[:out.Len()-4]
}

// decodeRecognition requires a validated main module. LoadProgram validates it
// before calling this function so entry comparisons can index its strings.
func decodeRecognition(data []byte, main *Module) (*Module, []recognitionEntry, error) {
	r := &decoder{data: data}
	isa := r.uint()
	if r.err == nil && isa != isaVersion {
		r.fail("unsupported recognition instruction set version %d", isa)
	}
	r.strings()
	m := r.module()
	n := r.count()
	entries := make([]recognitionEntry, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		entries = append(entries, recognitionEntry{r.str(), r.uint()})
	}
	if r.err == nil && r.pos != len(data) {
		r.fail("recognition bundle has %d trailing bytes", len(data)-r.pos)
	}
	if r.err != nil {
		return nil, nil, r.err
	}
	if err := validateModule(m); err != nil {
		return nil, nil, fmt.Errorf("invalid compiled grammar recognition bundle: %w", err)
	}
	if err := sameVars(moduleVarTable(main), moduleVarTable(m)); err != nil {
		return nil, nil, fmt.Errorf("invalid compiled grammar: the variables of the recognition bundle differ from the main module: %w", err)
	}
	want, available := moduleEntries(main), moduleEntries(m)
	if len(entries) != len(want) || len(entries) != len(available) {
		return nil, nil, fmt.Errorf("invalid compiled grammar: recognition start names do not match main module")
	}
	for i, entry := range entries {
		if entry.name != want[i].name || entry.name != available[i].name || entry.index != available[i].index {
			return nil, nil, fmt.Errorf("invalid compiled grammar: invalid recognition entry %d", i)
		}
	}
	return m, entries, nil
}
