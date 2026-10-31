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
		want zp.Limits
		err  bool
	}{
		{nil, zp.Limits{}, false},
		{[]string{"-budget", "0"}, zp.Limits{}, false},
		{[]string{"-budget", "5000", "-max-input", "7"}, zp.Limits{Budget: 5000, MaxInput: 7}, false},
		{[]string{"-budget", "unlimited"}, zp.Limits{Budget: zp.Unlimited}, false},
		{[]string{"-max-input", "Unlimited"}, zp.Limits{MaxInput: zp.Unlimited}, false},
		{[]string{"-budget", "UNLIMITED", "-max-input", "unlimited"}, zp.Limits{Budget: zp.Unlimited, MaxInput: zp.Unlimited}, false},
		{[]string{"-budget", "18446744073709551615"}, zp.Limits{Budget: zp.Unlimited}, false},
		{[]string{"-budget", "-1"}, zp.Limits{}, true},
		{[]string{"-budget", "1.5"}, zp.Limits{}, true},
		{[]string{"-max-input", "many"}, zp.Limits{}, true},
		{[]string{"-budget", "18446744073709551616"}, zp.Limits{}, true},
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
	v = limitValue(zp.Unlimited)
	if v.String() != "unlimited" {
		t.Errorf("Unlimited prints %q", v.String())
	}
	v = 42
	if v.String() != "42" {
		t.Errorf("42 prints %q", v.String())
	}
}
