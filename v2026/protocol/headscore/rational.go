package headscore

import (
	"errors"
	"math/big"
)

// Rational is the exact JSON form of a non-negative rational: the reduced
// numerator and denominator in decimal, with no sign or leading zeros. Every
// value has exactly one form, so equal values have equal bytes.
type Rational struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
}

var (
	errRationalNegative   = errors.New("rational is nil or negative")
	errRationalMalformed  = errors.New("rational is malformed or negative")
	errRationalNotReduced = errors.New("rational is not canonically reduced")
)

// EncodeRational returns the unique reduced form of a non-negative value.
func EncodeRational(value *big.Rat) (Rational, error) {
	if value == nil || value.Sign() < 0 {
		return Rational{}, errRationalNegative
	}
	return Rational{Numerator: value.Num().String(), Denominator: value.Denom().String()}, nil
}

// DecodeRational accepts only the unique reduced form of a non-negative value
// and refuses anything else, so a decoded value re-encodes to the same bytes.
func DecodeRational(encoded Rational) (*big.Rat, error) {
	numerator, numeratorOK := new(big.Int).SetString(encoded.Numerator, 10)
	denominator, denominatorOK := new(big.Int).SetString(encoded.Denominator, 10)
	if !numeratorOK || !denominatorOK || numerator.Sign() < 0 || denominator.Sign() <= 0 {
		return nil, errRationalMalformed
	}
	value := new(big.Rat).SetFrac(numerator, denominator)
	if encoded.Numerator != value.Num().String() || encoded.Denominator != value.Denom().String() {
		return nil, errRationalNotReduced
	}
	return value, nil
}

// Float64 is the float64 nearest an exact value, for display only. Ranking,
// selection and every comparison use the exact value.
func Float64(value *big.Rat) float64 {
	if value == nil {
		return 0
	}
	float, _ := value.Float64()
	return float
}

// Decimal renders an exact value with the given number of digits after the
// decimal point, rounding the last digit to nearest with halves away from
// zero, for display only.
func Decimal(value *big.Rat, digits int) string {
	if value == nil {
		value = new(big.Rat)
	}
	return value.FloatString(max(digits, 0))
}
