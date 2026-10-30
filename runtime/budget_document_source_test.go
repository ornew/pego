package runtime

import "testing"

// A Document charges the two passes of its source by the size of each character it decodes, so the
// total is two units per byte of the source, as outside a Document. Each character is reserved before
// it is stored, so a limit one unit short stops the second pass before it stores the last character.
func TestSetControlledSourceInDocumentChargesTwoUnitsPerByte(t *testing.T) {
	long := ""
	for range 300 {
		long += "abcdefghij"
	}
	for _, s := range []string{"", "abc", "é", "日本語", "a\xffb", "\xe6\x97", long, long + "é日" + long, long[:100] + "\xff\xfe" + long[:100]} {
		var p *Parser
		spend := func(limit uint64) (used uint64, err error) {
			p = &Parser{Control: newBudgetControl(nil, Limits{Budget: limit}), Document: &documentState{}}
			defer func() {
				if x := recover(); x != nil {
					err = x.(fatal).err
					used = p.Control.budgetUsed()
				}
			}()
			p.setControlledSource(s)
			return p.Control.budgetUsed(), nil
		}
		want := 2 * uint64(len(s))
		used, err := spend(want + 10)
		if err != nil || used != want {
			t.Fatalf("%q: used %d err %v, want %d", s, used, err, want)
		}
		if len(s) == 0 {
			continue
		}
		if _, err := spend(want); err != nil {
			t.Fatalf("%q: limit %d failed: %v", s, want, err)
		}
		if _, err := spend(want - 1); err == nil {
			t.Fatalf("%q: limit %d did not fail", s, want-1)
		}
		if n := len(p.In); n == 0 || p.In[n-1] != 0 || (n > 1 && p.In[n-2] == 0) {
			t.Fatalf("%q: limit %d stored %q, want all but the last character", s, want-1, p.In)
		}
	}
}
