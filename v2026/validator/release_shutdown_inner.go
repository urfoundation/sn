package validator

import "errors"

// An uncertain submission is still pending: only its existing prepared bytes
// authorize reconciliation. Return the cause without writing a pending.Error
// image that strict lifecycle replay would reject on restart.

func (self *ReleaseSteerer) recordReleasePendingError(vectorHash string, cause error) error {
	if isOwnerRecycleProductionConfig(self.cfg) && retryableProductionSteeringRead(cause) {
		// Pending bytes were already committed before submission. A transport
		// acknowledgement cannot resolve them or spend service failure budget.
		var wait *productionSteeringReadWait
		if errors.As(cause, &wait) {
			return cause
		}
		return &productionSteeringReadWait{phase: productionReadReceipt, cause: cause}
	}
	return cause
}
