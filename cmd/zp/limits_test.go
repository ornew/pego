package main

import (
	"flag"
	"io"
	"testing"

	"github.com/ornew/zeroparse"
)

func TestLimitFlagParsing(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want zeroparse.Limits
		err  bool
	}{
		{nil, zeroparse.Limits{}, false},
		{[]string{"-budget", "0"}, zeroparse.Limits{}, false},
		{[]string{"-budget", "5000", "-max-input", "7"}, zeroparse.Limits{Budget: 5000, MaxInput: 7}, false},
		{[]string{"-budget", "unlimited"}, zeroparse.Limits{Budget: zeroparse.Unlimited}, false},
		{[]string{"-max-input", "Unlimited"}, zeroparse.Limits{MaxInput: zeroparse.Unlimited}, false},
		{[]string{"-budget", "UNLIMITED", "-max-input", "unlimited"}, zeroparse.Limits{Budget: zeroparse.Unlimited, MaxInput: zeroparse.Unlimited}, false},
		{[]string{"-budget", "18446744073709551615"}, zeroparse.Limits{Budget: zeroparse.Unlimited}, false},
		{[]string{"-budget", "-1"}, zeroparse.Limits{}, true},
		{[]string{"-budget", "1.5"}, zeroparse.Limits{}, true},
		{[]string{"-max-input", "many"}, zeroparse.Limits{}, true},
		{[]string{"-budget", "18446744073709551616"}, zeroparse.Limits{}, true},
	} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		var lim limitFlags
		lim.register(fs)
		err := fs.Parse(tc.args)
		if (err != nil) != tc.err {
			t.Errorf("%v: error %v", tc.args, err)
			continue
		}
		if err == nil && lim.limits() != tc.want {
			t.Errorf("%v: limits %+v, want %+v", tc.args, lim.limits(), tc.want)
		}
	}
	var v limitValue
	if v.String() != "0" {
		t.Errorf("zero prints %q", v.String())
	}
	v = limitValue(zeroparse.Unlimited)
	if v.String() != "unlimited" {
		t.Errorf("Unlimited prints %q", v.String())
	}
	v = 42
	if v.String() != "42" {
		t.Errorf("42 prints %q", v.String())
	}
}
