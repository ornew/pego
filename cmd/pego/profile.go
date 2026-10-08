package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ornew/pego"
)

// profileKeys are the columns profile can sort by, with how to compare two rules (descending,
// except for the name).
var profileKeys = map[string]func(a, b *pego.RuleProfile) int{
	"self":     func(a, b *pego.RuleProfile) int { return cmp.Compare(b.SelfTime, a.SelfTime) },
	"time":     func(a, b *pego.RuleProfile) int { return cmp.Compare(b.Time, a.Time) },
	"calls":    func(a, b *pego.RuleProfile) int { return cmp.Compare(b.Calls, a.Calls) },
	"evals":    func(a, b *pego.RuleProfile) int { return cmp.Compare(b.Evals, a.Evals) },
	"memo":     func(a, b *pego.RuleProfile) int { return cmp.Compare(b.MemoHits, a.MemoHits) },
	"failed":   func(a, b *pego.RuleProfile) int { return cmp.Compare(b.Failed, a.Failed) },
	"repeats":  func(a, b *pego.RuleProfile) int { return cmp.Compare(b.Repeats, a.Repeats) },
	"consumed": func(a, b *pego.RuleProfile) int { return cmp.Compare(b.Consumed, a.Consumed) },
	"wasted":   func(a, b *pego.RuleProfile) int { return cmp.Compare(b.Wasted, a.Wasted) },
	"name":     func(a, b *pego.RuleProfile) int { return cmp.Compare(a.Rule, b.Rule) },
}

// profileCmd parses the input once with profiling and prints the cost of each rule.
func profileCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	f := newDebugFlags("profile")
	sortKey := f.fs.String("sort", "self", "column to sort by: self, time, calls, evals, memo, failed, repeats, consumed, wasted, or name")
	rows := f.fs.Int("n", 30, "number of rules to show (0 shows all)")
	format := f.fs.String("f", "text", "output format: text or json")
	p, src, opts, err := f.load(args, stdin)
	if err != nil {
		return err
	}
	less, ok := profileKeys[*sortKey]
	if !ok {
		return fmt.Errorf("unknown sort column %q", *sortKey)
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown format %q", *format)
	}
	var prof pego.Profile
	_, perr := p.Parse(src, append(opts, pego.WithProfile(&prof))...)
	rules := slices.Clone(prof.Rules)
	slices.SortStableFunc(rules, func(a, b *pego.RuleProfile) int {
		return cmp.Or(less(a, b), cmp.Compare(a.Rule, b.Rule))
	})
	if *rows > 0 && len(rules) > *rows {
		rules = rules[:*rows]
	}
	if *format == "json" {
		// The profile with the rules shown, in their order, and the hints
		out := struct {
			Result string `json:"result"`
			pego.Profile
			Hints []string `json:"hints"`
		}{"ok", prof, prof.Hints()}
		out.Rules = rules
		if perr != nil {
			out.Result = perr.Error()
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	return printProfile(stdout, &prof, rules, perr, len(prof.Rules))
}

func printProfile(w io.Writer, prof *pego.Profile, rules []*pego.RuleProfile, perr error, all int) error {
	result := "ok"
	if perr != nil {
		result = strings.ReplaceAll(perr.Error(), "\n", "; ")
	}
	fmt.Fprintf(w, "result: %s\n", result)
	fmt.Fprintf(w, "examined %d positions in %s (profiling included)\n", prof.Examined, dur(prof.Time))
	fmt.Fprintf(w, "%d calls, %d body evaluations, %d memo hits (%.0f%% of calls)\n\n", prof.Calls, prof.Evals, prof.MemoHits, pct(prof.MemoHits, prof.Calls))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "rule\tcalls\tevals\tmemo\tmatched\tfailed\trepeats\tconsumed\twasted\ttime\tself\tself%\t")
	for _, r := range rules {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\t%.1f\t\n", r.Rule, r.Calls, r.Evals, r.MemoHits, r.Matched, r.Failed, r.Repeats, r.Consumed, r.Wasted, dur(r.Time), dur(r.SelfTime), pct(int(r.SelfTime), int(prof.Time)))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(rules) < all {
		fmt.Fprintf(w, "(%d more rules; -n 0 shows all)\n", all-len(rules))
	}
	fmt.Fprintln(w, "\nWhat to look at:")
	for _, h := range prof.Hints() {
		fmt.Fprintf(w, "- %s\n", h)
	}
	return nil
}

func dur(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.1fµs", float64(d)/float64(time.Microsecond))
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}
