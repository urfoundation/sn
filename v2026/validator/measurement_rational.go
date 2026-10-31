package validator

// measurement_rational.go centralizes canonical non-negative rational JSON
// encoding so persisted validator decisions cannot have multiple byte forms.
// The encoding itself is protocol/headscore's, which the operator shares.

import (
	"math/big"

	"github.com/urfoundation/sn/v2026/protocol/headscore"
)

// encodeRationalJSON returns the unique reduced decimal representation.
func encodeRationalJSON(value *big.Rat) (RationalJSON, error) {
	return headscore.EncodeRational(value)
}

// decodeRationalJSON rejects non-reduced, negative and malformed values.
func decodeRationalJSON(encoded RationalJSON) (*big.Rat, error) {
	return headscore.DecodeRational(encoded)
}
