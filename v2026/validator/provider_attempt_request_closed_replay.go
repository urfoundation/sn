// Closed checkpoints admit their original payload and prohibit any subsequent
// sequence in that epoch, including an interrupted unpublished request.
package validator

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"hash"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Full cold replay owns this digest; no cached producer verdict substitutes.
type providerRequestClosedReplay struct {
	closed *ProviderAttemptRequestClosedHead
	digest hash.Hash
}

// Legacy checkpoints have no window proof and retain their original grammar.
func newProviderRequestClosedReplay(closed *ProviderAttemptRequestClosedHead) *providerRequestClosedReplay {
	return &providerRequestClosedReplay{closed: closed, digest: sha256.New()}
}

// A pending original after the close must belong to a strictly later epoch.
func verifyProviderRequestAfterClose(closed *ProviderAttemptRequestClosedHead, record ProviderAttemptRequestRecord) error {
	if closed != nil && record.Sequence > closed.End.Sequence && record.Boundary.SettlementEpoch <= closed.Window.Epoch {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request original follows its closed epoch fence"))
	}
	return nil
}

// The exact signed interval contributes its complete canonical JSONL bytes.
func (self *providerRequestClosedReplay) record(record ProviderAttemptRequestRecord, raw []byte) error {
	if err := verifyProviderRequestAfterClose(self.closed, record); err != nil {
		return err
	}
	if self.closed == nil || record.Sequence <= self.closed.Begin.Sequence || record.Sequence > self.closed.End.Sequence {
		return nil
	}
	window := self.closed.Window
	if record.Boundary.SettlementEpoch != window.Epoch || record.Boundary.EVMBlock < window.StartBlock || record.Boundary.EVMBlock >= window.EndBlock {
		return protocol.ErrProviderAttemptsIntegrity
	}
	_, _ = self.digest.Write(raw)
	_, _ = self.digest.Write([]byte{'\n'})
	return nil
}

// Compare even an empty interval: its original digest is SHA256(empty).
func (self *providerRequestClosedReplay) finish() error {
	if self.closed != nil && !bytes.Equal(self.digest.Sum(nil), self.closed.RecordsHash[:]) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider request closed original payload hash differs"))
	}
	return nil
}
