//go:build linux || darwin

// Native preparation refuses an already-known history gap before publishing
// another signed input. This shares the durable intent owner's existing policy.
package validator

import "errors"

// The current intent is authenticated by the submission owner before this
// negative preflight. Success grants no measurement, source or write authority;
// the durable intent reader still replays every actual successor and its history.
func (self *releaseRuntimeV2) admitNativeIntentSuccessorV2(current *SteeringIntent, epoch, settlementEpoch uint64) error {
	if current == nil || epoch <= current.SubnetEpoch {
		return nil
	}
	if self == nil || self.history == nil {
		return errors.New("native intent preflight has no authenticated startup owner")
	}
	next := &SteeringIntent{SubnetEpoch: epoch, SettlementEpoch: settlementEpoch}
	allowGap := self.history.retainedStartup && provisionalClosedNativeInputEnabled(&self.cfg) || self.history.historyAdoption.allowsIntentEdge(current, next)
	return validateSteeringIntentSuccessorWithGapsV2(current, next, allowGap)
}
