//go:build linux || darwin

// Audit discovery is separate from terminal-census grammar. The real native
// submission path publishes these consents before its source commitment;
// relaying preserves each actual observation/native-cycle logical slot.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Only immutable subject coordinates select a completed audit publication.
// Its complete locator hash is the value, so changing content cannot mint
// another slot or hide a changed destination behind an already-seen census.
type evidenceRelayAuditKey struct {
	validatorId      uint64
	epoch            uint64
	observationEpoch uint64
	nativeEpoch      uint64
}

// Called only by the existing relay worker. Completed entries are bounded by
// its approved slot allowance and retain conflicting-locator detection.
func (self *evidenceRelayRuntime) advanceDepositAudits(completed map[evidenceRelayAuditKey][32]byte) error {
	if completed == nil {
		return errors.New("evidence audit relay has no bounded completion owner")
	}
	block, hash, err := self.chain.FinalizedBlockContext(self.ctx)
	if err != nil {
		return err
	}
	if err := self.checkHorizonBlock(block); err != nil {
		return err
	}
	for index := range self.sources {
		source := &self.sources[index]
		manifests, err := validatorcomponent.DiscoverValidatorEvidenceDepositAuditV2Manifests(self.ctx, source.stateDir, source.bounds)
		if err != nil {
			return err
		}
		for _, manifest := range manifests {
			if manifest.Decision.ValidatorID != source.validatorId {
				return errors.New("evidence audit decision differs from its original configured validator")
			}
			key := evidenceRelayAuditKey{validatorId: source.validatorId, epoch: manifest.Epoch, observationEpoch: manifest.Subject.ObservationEpoch, nativeEpoch: manifest.Subject.NativeEpoch}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				return err
			}
			identity := sha256.Sum256(encoded)
			if previous, found := completed[key]; found {
				if previous != identity {
					return errors.New("completed evidence audit locator changes its immutable publication")
				}
				continue
			}
			if uint64(len(completed)) >= self.horizon.maximum {
				return errors.New("evidence audit completion owner exceeds its approved slot bound")
			}
			requests, err := self.readAuditPublication(self.ctx, source, &manifest, block, hash)
			if err != nil {
				return err
			}
			for _, expected := range requests {
				if err := self.horizon.admit(expected.Evidence.Header, block); err != nil {
					return err
				}
				action, ownerPlanHash, err := self.executor.admitOwnedEvidenceRelayAction(self.ctx, expected)
				if err != nil {
					return err
				}
				result, err := self.executor.keeper.relayValidatorEvidenceTransaction(self.ctx, self.chain, ownerPlanHash, action, expected)
				if err != nil {
					return err
				}
				if result == nil || result.Winner == nil {
					return errors.New("evidence audit relay returned no canonical winner")
				}
				if err := self.retainOwnedResult(ownerPlanHash, action, result); err != nil {
					return err
				}
			}
			completed[key] = identity
		}
	}
	return self.ctx.Err()
}

// The final happy-path observation precedes this request. Require a NEW full
// discovery/publication pass after it, even if closed epochs were already done.
// This prevents a cached closed-window success from bypassing later audits.
func (self *evidenceRelayRuntime) WaitAuditPass(ctx context.Context) error {
	if ctx == nil {
		return errors.New("evidence audit drain context is absent")
	}
	minimum, err := func() (uint64, error) {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.startedAuditPasses == ^uint64(0) {
			return 0, errors.New("evidence audit completion sequence overflows")
		}
		return self.startedAuditPasses + 1, self.resultErr
	}()
	if err != nil {
		return err
	}
	for {
		passed, changed, outcome := func() (bool, <-chan struct{}, error) {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			changed := self.changed
			return self.completedAuditPasses >= minimum, changed, self.resultErr
		}()
		if outcome != nil {
			return outcome
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if passed {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		case <-self.done:
			return errors.New("evidence relay stopped before its final audit discovery pass")
		}
	}
}
