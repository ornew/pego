package sample

import "github.com/ornew/pego"

// F is the part of *testing.F that Seed uses. (Taking an interface keeps the testing package out of
// programs that import sample without seeding fuzz tests.)
type F interface {
	Helper()
	Add(args ...any)
	Fatalf(format string, args ...any)
}

// Seed adds up to n distinct inputs that p accepts to the seed corpus of the fuzz test f, each as a
// single string argument, and returns them; the fuzz target must therefore take one string. It
// verifies again that p accepts every input, and fails the test if it cannot generate any.
//
//	func FuzzParse(f *testing.F) {
//		p := ... // the parser under test
//		sample.Seed(f, p, 50)
//		f.Fuzz(func(t *testing.T, in string) {
//			p.Parse(in) // must not panic
//		})
//	}
func Seed(f F, p *pego.Parser, n int, opts ...Option) []string {
	f.Helper()
	inputs, err := Generate(p, n, opts...)
	if err != nil {
		f.Fatalf("sample.Seed: %v", err)
		return nil
	}
	for _, in := range inputs {
		if _, err := p.Parse(in); err != nil {
			f.Fatalf("sample.Seed: generated input %q does not parse: %v", in, err)
			return nil
		}
		f.Add(in)
	}
	return inputs
}
