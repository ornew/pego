package complexitytest

import (
	"math"
	"testing"
)

// TestControls measures every control on every backend and checks its growth order. Controls
// that are not capped must finish below Cap: a capped point is a lower bound, and only an Exp
// control may rely on one.
func TestControls(t *testing.T) {
	t.Parallel()
	for _, c := range Controls() {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			p, err := Compile(c.Source)
			if err != nil {
				t.Fatal(err)
			}
			sizes := c.Sizes
			if testing.Short() {
				sizes = c.ShortSizes
			}
			var slopes [][]float64
			for _, b := range Backends {
				s, err := Measure(p, c.Input, sizes, b, Cap)
				if err != nil {
					t.Fatalf("%s: %v", BackendName(b), err)
				}
				if s.CappedPoints() && !c.Capped {
					t.Errorf("%s: %v reached the cap of an uncapped control", BackendName(b), s.Costs)
				}
				if got := Classify(s); got != c.Order {
					t.Errorf("%s: %v (slopes %.2f) classified %v, want %v", BackendName(b), s.Costs, s.Slopes(), got, c.Order)
				}
				slopes = append(slopes, s.Slopes())
				t.Logf("%s: steps %v slopes %.2f", BackendName(b), s.Costs, s.Slopes())
			}
			if c.Order.Kind == KindExp {
				return // exponential slopes grow with the constant of each backend
			}
			for _, sl := range slopes[1:] {
				for i := range sl {
					if math.Abs(sl[i]-slopes[0][i]) > 0.3 {
						t.Errorf("backends disagree on slope %d: %.2f against %.2f", i, sl[i], slopes[0][i])
					}
				}
			}
		})
	}
}

func TestControlsTable(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range Controls() {
		if seen[c.Name] {
			t.Errorf("duplicate control %q", c.Name)
		}
		seen[c.Name] = true
		if c.Capped && c.Order != Exp {
			t.Errorf("%s: only an Exp control may be capped", c.Name)
		}
		if c.Positive && c.Order != Linear {
			t.Errorf("%s: a positive control is linear, got %v", c.Name, c.Order)
		}
		if c.BecomesLinear && c.Positive == (c.Order != Linear) {
			t.Errorf("%s: BecomesLinear and Positive disagree with the order %v", c.Name, c.Order)
		}
		if len(c.Sizes) != 3 || len(c.ShortSizes) != 3 {
			t.Errorf("%s: a series has three sizes", c.Name)
		}
		for _, sz := range [][]int{c.Sizes, c.ShortSizes} {
			if sz[1] != 2*sz[0] || sz[2] != 2*sz[1] {
				t.Errorf("%s: sizes %v do not double", c.Name, sz)
			}
		}
		if c.Class == "" {
			t.Errorf("%s: no expected class", c.Name)
		}
	}
}
