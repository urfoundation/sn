// Original pending authority permits receipt observation, never a new send.
// The typed wait keeps independent workers alive while that observation repeats.
package validator

import "fmt"

// Constructed after original authority authentication and an actual receipt
// scan, either empty or interrupted only by typed transport failure. It neither
// marks an epoch complete nor authorizes an old signature under current approval.
type productionPendingReconciliation struct {
	nativeEpoch   uint64
	extrinsicHash string
	cause         error
}

func (self *productionPendingReconciliation) Error() string {
	message := fmt.Sprintf("original production transaction %s remains pending in native epoch %d; retaining bytes and observing receipts without rebroadcast", self.extrinsicHash, self.nativeEpoch)
	if self.cause != nil {
		message += ": " + self.cause.Error()
	}
	return message
}

// Retains the real transport cause for diagnostics and cancellation accounting.
func (self *productionPendingReconciliation) Unwrap() error { return self.cause }
