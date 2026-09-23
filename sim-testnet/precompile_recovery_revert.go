// A bounded recovery funding operation requires an actual execution revert,
// never a transport failure or unrelated diagnostic containing the same words.
package main

import (
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Geth identifies revert data with code 3. The owned node uses -32603 and the
// VM-exception message instead; -32000 is the other execution-error envelope.
// Every cause in a joined error must qualify before it can authorize funding.
func precompileRecoveryCallReverted(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !precompileRecoveryCallReverted(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return precompileRecoveryCallReverted(cause)
		}
	}
	rpcError, ok := err.(rpc.Error)
	if !ok {
		return false
	}
	switch rpcError.ErrorCode() {
	case 3, -32000, -32603:
	default:
		return false
	}
	if _, ok := ethclient.RevertErrorData(err); ok {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(rpcError.Error()))
	vmRevert := "vm exception while processing transaction: revert"
	if message != "execution reverted" && !strings.HasPrefix(message, "execution reverted:") && message != vmRevert && !strings.HasPrefix(message, vmRevert+" ") && !strings.HasPrefix(message, vmRevert+":") {
		return false
	}
	// Empty bytes are a valid revert without a reason. If the provider supplied
	// data, malformed or non-byte data cannot establish a revert.
	if dataError, ok := err.(rpc.DataError); ok && dataError.ErrorData() != nil {
		encoded, ok := dataError.ErrorData().(string)
		if !ok {
			return false
		}
		_, err := hexutil.Decode(encoded)
		return err == nil
	}
	return true
}
