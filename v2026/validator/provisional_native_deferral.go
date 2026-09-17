package validator

import (
	"errors"
	"fmt"
)

var errProvisionalClosedNativeInput = errors.New("provisional native input belongs to an already closed settlement")

// Deferral records no native success. It preserves an authenticated immutable
// input while allowing independent trails and settlement publication to run.
type provisionalClosedNativeInput struct {
	nativeEpoch      uint64
	activeSettlement uint64
}

func (self *provisionalClosedNativeInput) Error() string {
	return fmt.Sprintf("provisional native epoch %d deferred: its signed input settlement is already closed; active settlement %d; no native submission", self.nativeEpoch, self.activeSettlement)
}

func (self *provisionalClosedNativeInput) Unwrap() error { return errProvisionalClosedNativeInput }

func provisionalClosedNativeInputEnabled(cfg *ReleaseConfig) bool {
	return cfg != nil && cfg.ProvisionalDeferClosedNativeInput && cfg.ChainID == 945 && cfg.Policy.NetworkProfile == "testnet"
}
