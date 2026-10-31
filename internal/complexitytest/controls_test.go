package complexitytest

import (
	"context"
	"math"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/starttest"
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
		if c.BecomesLinear && becomesLinearLanded != (c.Order == Linear) {
			t.Errorf("%s: BecomesLinear disagrees with the order %v", c.Name, c.Order)
		}
		if len(c.Sizes) != 3 || len(c.ShortSizes) != 3 {
			t.Errorf("%s: a series has three sizes", c.Name)
		}
		for _, sz := range [][]int{c.Sizes, c.ShortSizes} {
			if sz[1] != 2*sz[0] || sz[2] != 2*sz[1] {
				t.Errorf("%s: sizes %v do not double", c.Name, sz)
			}
		}
		// The class of a positive control is a claim of linear cost, and that of a negative one is
		// not.
		if linear := c.Class == ClassLinear || c.Class == ClassLinearEnv; linear != c.Positive {
			t.Errorf("%s: expected class %v for a control with Positive %v", c.Name, c.Class, c.Positive)
		}
	}
}

// TestControlClasses checks the class that the complexity classifier assigns to the start rule of
// every control: a negative control is never classified linear, with or without an environment
// bound, and a positive control is.
func TestControlClasses(t *testing.T) {
	t.Parallel()
	for _, c := range Controls() {
		t.Run(c.Name, func(t *testing.T) {
			g, err := pego.ParseGrammar(c.Source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.ClassifyComplexity(context.Background(), starttest.MarkAll(g), "main")
			if err != nil {
				t.Fatal(err)
			}
			if got.Class != c.Class {
				t.Errorf("classified %v (degree %d), want %v\ncauses %v\nreasons %v", got.Class, got.Degree, c.Class, got.Causes, got.Reasons)
			}
			if c.Order.Kind == KindPoly && got.Class == ClassPoly && got.Degree != c.Order.Degree {
				t.Errorf("degree %d, measured order %v", got.Degree, c.Order)
			}
		})
	}
}
