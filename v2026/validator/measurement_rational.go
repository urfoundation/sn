package validator

// measurement_rational.go centralizes canonical non-negative rational JSON
// encoding so persisted validator decisions cannot have multiple byte forms.

import (
	"errors"
	"math/big"
)

// encodeRationalJSON returns the unique reduced decimal representation.
func encodeRationalJSON(value *big.Rat) (RationalJSON, error) {
	if value == nil || value.Sign() < 0 {
		return RationalJSON{}, errors.New("rational is nil or negative")
	}
	return RationalJSON{Numerator: value.Num().String(), Denominator: value.Denom().String()}, nil
}

// decodeRationalJSON rejects non-reduced, negative and malformed values.
func decodeRationalJSON(encoded RationalJSON) (*big.Rat, error) {
	numerator, numeratorOK := new(big.Int).SetString(encoded.Numerator, 10)
	denominator, denominatorOK := new(big.Int).SetString(encoded.Denominator, 10)
	if !numeratorOK || !denominatorOK || numerator.Sign() < 0 || denominator.Sign() <= 0 {
		return nil, errors.New("rational is malformed or negative")
	}
	value := new(big.Rat).SetFrac(numerator, denominator)
	if encoded.Numerator != value.Num().String() || encoded.Denominator != value.Denom().String() {
		return nil, errors.New("rational is not canonically reduced")
	}
	return value, nil
}
