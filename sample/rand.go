package sample

import "math/bits"

// pcg draws the generator's random numbers. It is PCG-DXSM with 128 bits of
// state, and bounded numbers use Lemire's multiply-and-reject method: the
// algorithms of math/rand/v2's PCG, IntN, Int64N and Shuffle in Go 1.27, which
// the package used before. It keeps its own copy because Go does not promise
// to keep those algorithms, and a seed must give the same inputs with every Go
// release.
type pcg struct{ hi, lo uint64 }

func newPCG(seed1, seed2 uint64) *pcg { return &pcg{seed1, seed2} }

func (p *pcg) Uint64() uint64 {
	const (
		mulHi = 2549297995355413924
		mulLo = 4865540595714422341
		incHi = 6364136223846793005
		incLo = 1442695040888963407
	)
	// state = state * mul + inc
	hi, lo := bits.Mul64(p.lo, mulLo)
	hi += p.hi*mulLo + p.lo*mulHi
	lo, c := bits.Add64(lo, incLo, 0)
	hi, _ = bits.Add64(hi, incHi, c)
	p.lo, p.hi = lo, hi

	// DXSM output ("double xorshift multiply")
	const cheapMul = 0xda942042e4dd58b5
	hi ^= hi >> 32
	hi *= cheapMul
	hi ^= hi >> 48
	hi *= lo | 1
	return hi
}

// uint64n returns a uniform number in [0,n) for n > 0.
func (p *pcg) uint64n(n uint64) uint64 {
	if n&(n-1) == 0 {
		return p.Uint64() & (n - 1)
	}
	hi, lo := bits.Mul64(p.Uint64(), n)
	if lo < n {
		thresh := -n % n
		for lo < thresh {
			hi, lo = bits.Mul64(p.Uint64(), n)
		}
	}
	return hi
}

// IntN returns a uniform number in [0,n). It panics if n <= 0.
func (p *pcg) IntN(n int) int {
	if n <= 0 {
		panic("invalid argument to IntN")
	}
	return int(p.uint64n(uint64(n)))
}

// Int64N returns a uniform number in [0,n). It panics if n <= 0.
func (p *pcg) Int64N(n int64) int64 {
	if n <= 0 {
		panic("invalid argument to Int64N")
	}
	return int64(p.uint64n(uint64(n)))
}

// Shuffle permutes n elements with the Fisher-Yates shuffle.
func (p *pcg) Shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		swap(i, int(p.uint64n(uint64(i+1))))
	}
}
