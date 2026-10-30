package main

import (
	"flag"
	"io"
	"testing"

	"github.com/ornew/pego"
)

func TestLimitFlagParsing(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want pego.Limits
		err  bool
	}{
		{nil, pego.Limits{}, false},
		{[]string{"-budget", "0"}, pego.Limits{}, false},
		{[]string{"-budget", "5000", "-max-input", "7"}, pego.Limits{Budget: 5000, MaxInput: 7}, false},
		{[]string{"-budget", "unlimited"}, pego.Limits{Budget: pego.Unlimited}, false},
		{[]string{"-max-input", "Unlimited"}, pego.Limits{MaxInput: pego.Unlimited}, false},
		{[]string{"-budget", "UNLIMITED", "-max-input", "unlimited"}, pego.Limits{Budget: pego.Unlimited, MaxInput: pego.Unlimited}, false},
		{[]string{"-budget", "18446744073709551615"}, pego.Limits{Budget: pego.Unlimited}, false},
		{[]string{"-budget", "-1"}, pego.Limits{}, true},
		{[]string{"-budget", "1.5"}, pego.Limits{}, true},
		{[]string{"-max-input", "many"}, pego.Limits{}, true},
		{[]string{"-budget", "18446744073709551616"}, pego.Limits{}, true},
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
	v = limitValue(pego.Unlimited)
	if v.String() != "unlimited" {
		t.Errorf("Unlimited prints %q", v.String())
	}
	v = 42
	if v.String() != "42" {
		t.Errorf("42 prints %q", v.String())
	}
}
