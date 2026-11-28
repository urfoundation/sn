// Missing RPC evidence preserves an unknown receipt outcome. Complete evidence
// that contradicts a canonical commitment remains a separate hard error.
package crv4

import (
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Issued only at the physical receipt reader when required wire data is absent.
// It does not authorize absence, advance a scan, or classify RPC error strings.
type ReceiptEvidenceUnavailableError struct {
	BlockHash types.Hash
	Field     string
}

func (self *ReceiptEvidenceUnavailableError) Error() string {
	return fmt.Sprintf("crv4: receipt evidence unavailable at %s: %s is missing", self.BlockHash.Hex(), self.Field)
}
