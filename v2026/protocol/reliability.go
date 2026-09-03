package protocol

import "math/big"

const reliabilityScalePPM = uint32(1_000_000)

// ReliabilityPPM returns the exact floor, in parts per million, of the
// 95-percent Wilson lower bound for confirmations out of assignments. The
// exposure denominator is floored at aMin and confirmations are defensively
// capped at the observed assignment count. The implementation uses only
// integer comparisons, so every architecture produces identical payout input.
func ReliabilityPPM(confirmations, assignments, aMin uint64) uint32 {
	if confirmations == 0 || assignments == 0 {
		return 0
	}
	if confirmations > assignments {
		confirmations = assignments
	}
	n := assignments
	if n < aMin {
		n = aMin
	}
	if n == 0 {
		return 0
	}

	// Wilson z = 1.96 = Z/D. For a candidate q/1e6, q <= lower iff:
	//   L = B*1e6 - q*2*(n*D^2+Z^2) >= 0
	//   L^2*n >= Z^2*(4*D^2*c*(n-c)+Z^2*n)*1e12
	// This removes the irrational square root without approximation.
	const z, d, scale = uint64(196), uint64(100), uint64(reliabilityScalePPM)
	Z := new(big.Int).SetUint64(z)
	D2 := new(big.Int).SetUint64(d * d)
	Z2 := new(big.Int).Mul(Z, Z)
	N := new(big.Int).SetUint64(n)
	C := new(big.Int).SetUint64(confirmations)

	b := new(big.Int).Add(
		new(big.Int).Mul(new(big.Int).Mul(big.NewInt(2), C), D2),
		Z2,
	)
	denom2 := new(big.Int).Mul(
		big.NewInt(2),
		new(big.Int).Add(new(big.Int).Mul(N, D2), Z2),
	)
	nMinusC := new(big.Int).Sub(N, C)
	a := new(big.Int).Add(
		new(big.Int).Mul(new(big.Int).Mul(new(big.Int).Mul(big.NewInt(4), D2), C), nMinusC),
		new(big.Int).Mul(Z2, N),
	)
	right := new(big.Int).Mul(new(big.Int).Mul(Z2, a), new(big.Int).SetUint64(scale*scale))

	valid := func(q uint32) bool {
		leftTerm := new(big.Int).Mul(b, new(big.Int).SetUint64(scale))
		leftTerm.Sub(leftTerm, new(big.Int).Mul(new(big.Int).SetUint64(uint64(q)), denom2))
		if leftTerm.Sign() < 0 {
			return false
		}
		left := new(big.Int).Mul(new(big.Int).Mul(leftTerm, leftTerm), N)
		return left.Cmp(right) >= 0
	}
	lo, hi := uint32(0), reliabilityScalePPM
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if valid(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}
