package validator

// An uncertain submission is still pending: only its existing prepared bytes
// authorize reconciliation. Return the cause without writing a pending.Error
// image that strict lifecycle replay would reject on restart.

func (self *ReleaseSteerer) recordReleasePendingError(vectorHash string, cause error) error {
	return cause
}
