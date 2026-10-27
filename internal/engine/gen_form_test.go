package engine

import (
	"strings"
	"testing"
)

func TestSelectForm(t *testing.T) {
	g := &generator{pairs: true}
	inner := g.form("c", "o")
	outer := g.formRest("A"+inner+"B", "a"+inner+"b", "r"+inner)
	text := "x" + outer + "y" + g.form("same", "same") + "z"
	for _, tc := range []struct {
		form textForm
		want string
	}{
		{formControlled, "xAcBysamez"},
		{formOrdinaryText, "xaobysamez"},
		{formRestText, "xrcysamez"},
	} {
		if got := selectForm(text, tc.form); got != tc.want {
			t.Errorf("form %d: got %q, want %q", tc.form, got, tc.want)
		}
	}
	if got := (&generator{}).form("c", "o"); got != "c" {
		t.Errorf("without pairs: got %q, want the controlled text", got)
	}
	if got := (&generator{ordinary: true}).form("c", "o"); got != "o" {
		t.Errorf("ordinary: got %q, want the ordinary text", got)
	}
}

func TestSelectFormRejectsMalformedFragments(t *testing.T) {
	for name, text := range map[string]string{
		"no end":       "a" + formStart + "c" + formOrdinary + "o",
		"no separator": "a" + formStart + "c" + formEnd,
		"nested open":  "a" + formStart + "c" + formOrdinary + formStart + "x" + formOrdinary + "y" + formEnd,
	} {
		func() {
			defer func() {
				msg, _ := recover().(string)
				if !strings.HasPrefix(msg, "internal error: malformed text fragment") {
					t.Errorf("%s: panic %q, want the malformed fragment error", name, msg)
				}
			}()
			selectForm(text, formControlled)
		}()
	}
}
