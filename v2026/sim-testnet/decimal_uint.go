// Decimal unsigned integers keep wei-denominated campaign ceilings exact beyond
// uint64 while retaining canonical unquoted JSON numbers for legacy plan hashes.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"strconv"
)

// DecimalUint is a canonical nonnegative base-ten integer with no fixed-width
// ceiling. Its zero value behaves as zero and marshals as the JSON number 0.
type DecimalUint string

// Parse a canonical nonnegative base-ten integer.
func parseDecimalUint(value string) (DecimalUint, error) {
	if value == "" {
		return "", errors.New("decimal unsigned integer is empty")
	}
	integer, ok := new(big.Int).SetString(value, 10)
	if !ok || integer.Sign() < 0 || integer.String() != value {
		return "", fmt.Errorf("%q is not a canonical decimal unsigned integer", value)
	}
	return DecimalUint(value), nil
}

// Convert one machine-sized value without intermediate formatting ambiguity.
func decimalUint64(value uint64) DecimalUint {
	return DecimalUint(strconv.FormatUint(value, 10))
}

// Convert a nonnegative arbitrary-precision value by copy.
func decimalUintBig(value *big.Int) (DecimalUint, error) {
	if value == nil || value.Sign() < 0 {
		return "", errors.New("decimal unsigned integer is nil or negative")
	}
	return DecimalUint(value.String()), nil
}

// Return the canonical zero representation for an uninitialized value.
func (self DecimalUint) String() string {
	if self == "" {
		return "0"
	}
	return string(self)
}

// Convert to an independent arbitrary-precision integer.
func (self DecimalUint) Big() (*big.Int, error) {
	value, ok := new(big.Int).SetString(self.String(), 10)
	if !ok || value.Sign() < 0 || value.String() != self.String() {
		return nil, fmt.Errorf("%q is not a canonical decimal unsigned integer", self)
	}
	return value, nil
}

// Report whether the value is exactly zero, treating its Go zero value as zero.
func (self DecimalUint) IsZero() bool {
	return self == "" || self == "0"
}

// Compare two valid values.
func (self DecimalUint) Cmp(other DecimalUint) (int, error) {
	left, err := self.Big()
	if err != nil {
		return 0, err
	}
	right, err := other.Big()
	if err != nil {
		return 0, err
	}
	return left.Cmp(right), nil
}

// Encode as a JSON number so uint64-era plan hashes remain stable.
func (self DecimalUint) MarshalJSON() ([]byte, error) {
	value, err := self.Big()
	if err != nil {
		return nil, err
	}
	return []byte(value.String()), nil
}

// Accept only canonical JSON numbers; configuration strings are parsed before
// they enter a plan so persisted approvals retain one representation.
func (self *DecimalUint) UnmarshalJSON(data []byte) error {
	if self == nil {
		return errors.New("nil decimal unsigned integer receiver")
	}
	value := string(bytes.TrimSpace(data))
	parsed, err := parseDecimalUint(value)
	if err != nil {
		return err
	}
	*self = parsed
	return nil
}

// Add without a fixed-width ceiling.
func addDecimalUint(left, right DecimalUint) (DecimalUint, error) {
	a, err := left.Big()
	if err != nil {
		return "", err
	}
	b, err := right.Big()
	if err != nil {
		return "", err
	}
	return decimalUintBig(new(big.Int).Add(a, b))
}

// Subtract while rejecting an underflow.
func subtractDecimalUint(left, right DecimalUint) (DecimalUint, error) {
	a, err := left.Big()
	if err != nil {
		return "", err
	}
	b, err := right.Big()
	if err != nil {
		return "", err
	}
	if a.Cmp(b) < 0 {
		return "", errors.New("decimal unsigned integer underflow")
	}
	return decimalUintBig(new(big.Int).Sub(a, b))
}

// Subtract several approved slices in order and identify any aggregate
// underflow at the common caller boundary.
func subtractDecimalUints(value DecimalUint, subtractors ...DecimalUint) (DecimalUint, error) {
	result := value
	var err error
	for _, subtractor := range subtractors {
		result, err = subtractDecimalUint(result, subtractor)
		if err != nil {
			return "", err
		}
	}
	return result, nil
}

// Multiply by one machine-sized factor.
func multiplyDecimalUint64(value DecimalUint, factor uint64) (DecimalUint, error) {
	integer, err := value.Big()
	if err != nil {
		return "", err
	}
	return decimalUintBig(new(big.Int).Mul(integer, new(big.Int).SetUint64(factor)))
}

// Multiply two machine-sized inputs into an arbitrary-size result.
func multiplyUint64Decimal(left, right uint64) DecimalUint {
	return DecimalUint(new(big.Int).Mul(new(big.Int).SetUint64(left), new(big.Int).SetUint64(right)).String())
}

// Floor-multiply by a rational weight without overflowing an intermediate.
func multiplyDivideDecimalUint(value DecimalUint, numerator, denominator uint64) (DecimalUint, error) {
	if denominator == 0 {
		return "", errors.New("decimal unsigned integer division by zero")
	}
	integer, err := value.Big()
	if err != nil {
		return "", err
	}
	integer.Mul(integer, new(big.Int).SetUint64(numerator))
	integer.Div(integer, new(big.Int).SetUint64(denominator))
	return decimalUintBig(integer)
}

// Floor-divide by one machine-sized divisor.
func divideDecimalUint(value DecimalUint, divisor uint64) (DecimalUint, error) {
	return multiplyDivideDecimalUint(value, 1, divisor)
}

// Convert a ceiling division to uint64 for native mirror funding.
func ceilDivideDecimalUintToUint64(value DecimalUint, divisor uint64) (uint64, error) {
	if divisor == 0 {
		return 0, errors.New("decimal unsigned integer division by zero")
	}
	integer, err := value.Big()
	if err != nil {
		return 0, err
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(integer, new(big.Int).SetUint64(divisor), remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return 0, errors.New("decimal unsigned integer quotient exceeds uint64")
	}
	return quotient.Uint64(), nil
}
