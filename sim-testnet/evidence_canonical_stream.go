// Incoming evidence is validated afresh, then its original RawMessage spelling
// is compacted directly into the hash. No payload-sized canonical copy survives
// between stages, and no decoded-token rewrite changes the signed wire.
package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"

	"github.com/urnetwork/server/v2026/startifact"
)

// Admission belongs to the caller's fresh grammar check; the same bounded
// encoder is exercised by real server publication and simulator verification.
func writeValidatedEvidencePayload(writer io.Writer, payload []byte) error {
	return startifact.WriteValidatedEvidencePayload(writer, payload)
}

// The verifier calls this only after fresh whole-value grammar admission.
// Framing still marshals the actual envelope type, not a second hand-copied
// schema. Every verification computes its own hash and recovers its signature.
func evidenceDigestFromValidatedPayload(envelope *ReleaseEvidenceEnvelope, payload []byte) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	if envelope == nil || len(payload) == 0 {
		return digest, errors.New("release evidence digest owner is missing")
	}
	unsigned := *envelope
	unsigned.ContentHash, unsigned.Signature = "", ""
	prefix, suffix, err := evidenceWireFraming(&unsigned)
	if err != nil {
		return digest, err
	}
	hash := sha256.New()
	_, _ = hash.Write(prefix)
	if err := writeValidatedEvidencePayload(hash, payload); err != nil {
		return digest, err
	}
	_, _ = hash.Write(suffix)
	copy(digest[:], hash.Sum(digest[:0]))
	return digest, nil
}

// Comparing against an existing immutable wire needs only a cursor, not an
// additional canonical envelope. No Write retains or changes borrowed bytes.
type evidenceCanonicalWireComparator struct {
	remaining []byte
}

// Any mismatch ends the comparison; a short original is never a prefix match.
func (self *evidenceCanonicalWireComparator) Write(value []byte) (int, error) {
	if len(value) > len(self.remaining) || !bytes.Equal(value, self.remaining[:len(value)]) {
		return 0, errors.New("canonical evidence wire bytes differ")
	}
	self.remaining = self.remaining[len(value):]
	return len(value), nil
}

// Call only after this invocation has validated its private decoded payload.
// The prior reader has already verified its signature and typed original body;
// every original prefix, payload and suffix byte still must match exactly.
func verifyValidatedEvidenceWire(envelope *ReleaseEvidenceEnvelope, wire []byte) error {
	prefix, suffix, err := evidenceWireFraming(envelope)
	if err != nil {
		return err
	}
	comparator := &evidenceCanonicalWireComparator{remaining: wire}
	if _, err := comparator.Write(prefix); err != nil {
		return err
	}
	if err := writeValidatedEvidencePayload(comparator, envelope.Payload); err != nil {
		return err
	}
	if _, err := comparator.Write(suffix); err != nil {
		return err
	}
	if len(comparator.remaining) != 0 {
		return errors.New("canonical evidence wire has trailing bytes")
	}
	return nil
}
