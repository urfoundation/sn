package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/crv4"
)

// NativeTransactionFeeResponse retains the provider's integer token so parsing
// never passes through float64.
type NativeTransactionFeeResponse struct {
	PartialFee json.RawMessage `json:"partialFee"`
}

// ParseNativeTransactionFee parses the standard payment_queryInfo fee without
// accepting JSON floats, signs, overflow, or provider-specific loss of integer
// precision.
func ParseNativeTransactionFee(raw json.RawMessage) (uint64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return 0, fmt.Errorf("decode native transaction fee: %w", err)
		}
		value = strings.TrimSpace(decoded)
	}
	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		value = value[2:]
	}
	if value == "" {
		return 0, errors.New("native transaction fee is empty")
	}
	fee, err := strconv.ParseUint(value, base, 64)
	if err != nil {
		return 0, fmt.Errorf("native transaction fee %q is not an unsigned uint64: %w", value, err)
	}
	return fee, nil
}

// ValidateNativeTransactionFee enforces the approval-bound per-extrinsic
// ceiling. A zero limit fails closed.
func ValidateNativeTransactionFee(estimated, limit uint64) error {
	if limit == 0 {
		return errors.New("native transaction fee limit is zero")
	}
	if estimated > limit {
		return fmt.Errorf("estimated native transaction fee %d rao exceeds approved limit %d rao", estimated, limit)
	}
	return nil
}

// QuoteNativeTransactionFee quotes the exact signed bytes through
// payment_queryInfo. Callers quote immediately before broadcast; the funded
// signer balance remains the hard loss boundary if the runtime multiplier
// changes between the quote and inclusion.
func QuoteNativeTransactionFee(ctx context.Context, chain *crv4.Chain, raw []byte) (uint64, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || len(raw) == 0 {
		return 0, errors.New("native transaction fee quote dependencies are unavailable")
	}
	var response NativeTransactionFeeResponse
	if err := chain.API.Client.CallContext(ctx, &response, "payment_queryInfo", codec.HexEncodeToString(raw)); err != nil {
		return 0, fmt.Errorf("quote native transaction fee: %w", err)
	}
	return ParseNativeTransactionFee(response.PartialFee)
}

// ApproveNativeTransactionFee quotes the signed bytes and enforces the limit.
// The estimate is returned even when it exceeds the limit so callers can
// report it.
func ApproveNativeTransactionFee(ctx context.Context, chain *crv4.Chain, raw []byte, limit uint64) (uint64, error) {
	estimated, err := QuoteNativeTransactionFee(ctx, chain, raw)
	if err != nil {
		return 0, err
	}
	if err := ValidateNativeTransactionFee(estimated, limit); err != nil {
		return estimated, err
	}
	return estimated, nil
}
