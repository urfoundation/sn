// Contract identity is the validated address bytes. Configuration spelling is
// independent of the canonical representation retained in signed evidence.
package validator

import "github.com/ethereum/go-ethereum/common"

// Validate both widths before decoding so truncation or malformed input cannot
// collapse onto the same address. Original strings and signed bytes stay owned.
func releaseAddressIdentityMatches(left, right string) bool {
	return common.IsHexAddress(left) && common.IsHexAddress(right) && common.HexToAddress(left) == common.HexToAddress(right)
}
