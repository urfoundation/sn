// Native fee tests pin parsing and fail-closed thresholds discovered during
// the first netuid-521 registration campaign.
package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestNativeTransactionFeeParsingAcceptsExactIntegerEncodings(t *testing.T) {
	for input, want := range map[string]uint64{
		`2131733`:    2_131_733,
		`"2131733"`:  2_131_733,
		`"0x208715"`: 2_131_733,
		`0`:          0,
	} {
		got, err := parseNativeTransactionFee(json.RawMessage(input))
		if err != nil || got != want {
			t.Fatalf("fee %s = %d, %v; want %d", input, got, err, want)
		}
	}
}

func TestNativeTransactionFeeParsingRejectsMalformedAndLossyValues(t *testing.T) {
	for _, input := range []string{"", `""`, `-1`, `1.5`, `"0x"`, `18446744073709551616`, `null`} {
		if fee, err := parseNativeTransactionFee(json.RawMessage(input)); err == nil {
			t.Fatalf("malformed fee %q was accepted as %d", input, fee)
		}
	}
	if got, err := parseNativeTransactionFee(json.RawMessage(`18446744073709551615`)); err != nil || got != math.MaxUint64 {
		t.Fatalf("maximum uint64 fee = %d, %v", got, err)
	}
}

func TestNativeTransactionFeeLimitCoversObservedRegistrationAndFailsClosed(t *testing.T) {
	const observed = uint64(2_131_733)
	if err := validateNativeTransactionFee(observed, 3_000_000); err != nil {
		t.Fatalf("observed netuid-521 fee was rejected: %v", err)
	}
	for _, limit := range []uint64{0, 2_000_000, observed - 1} {
		if err := validateNativeTransactionFee(observed, limit); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("unsafe fee limit %d was accepted: %v", limit, err)
		}
	}
	if err := validateNativeTransactionFee(observed, observed); err != nil {
		t.Fatalf("exact fee ceiling was rejected: %v", err)
	}
}
