package miner

// Production callbacks name the failed authority. A local persistence failure
// is distinct from a rejected credential, even when both stop one provider.

import (
	"errors"
	"fmt"

	"github.com/urfoundation/sn/v2026/clientauth"
)

var errSwarmAuthenticationRejected = errors.New("provider authentication was rejected")

// The retained token may be unchanged or may have lost its durability reply.
// Neither case authorizes another signed wallet request or credential rewrite.
type swarmMemberStorageError struct{ cause error }

// Keep the established diagnostic text while preserving its underlying cause.
func (self *swarmMemberStorageError) Error() string {
	return fmt.Sprintf("persist refreshed client JWT: %v", self.cause)
}

// Callers can still inspect the actual filesystem observation.
func (self *swarmMemberStorageError) Unwrap() error { return self.cause }

// The same listener is used by the real SDK and the forced I/O regression.
// It requests failure handling; it never joins the SDK from its own callback.
func swarmMemberJwtRefreshListener(path string, failed func(error)) clientauth.JwtRefreshListenerFunc {
	return clientauth.JwtRefreshListenerFunc(func(jwt string) {
		if err := clientauth.WriteToken(path, jwt); err != nil {
			failed(&swarmMemberStorageError{cause: err})
		}
	})
}

// All joined causes must belong to the local persistence boundary. A hard
// authentication or unknown cause never becomes local because another cause is.
func swarmMemberStorageFailure(err error) bool {
	if err == nil || errors.Is(err, errSwarmAuthenticationRejected) {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		found := false
		for _, cause := range joined.Unwrap() {
			if cause == nil {
				continue
			}
			found = true
			if !swarmMemberStorageFailure(cause) {
				return false
			}
		}
		return found
	}
	if _, ok := err.(*swarmMemberStorageError); ok {
		return true
	}
	if cause := errors.Unwrap(err); cause != nil {
		return swarmMemberStorageFailure(cause)
	}
	return false
}
