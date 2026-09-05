package validator

// A verification operation owns its injected cut primitive; public entry
// points always supply the full existing validator with its explicit key mode.

import (
	"bytes"
	"crypto/ed25519"

	"github.com/urnetwork/connect"
)

// Keeps all cut identity, lifecycle, hash and signature validation observable.
type attemptLedgerCutVerifier func(*AttemptLedgerCut, ed25519.PublicKey, map[byte]ed25519.PublicKey, bool) error

// Bounds retained authentication independently of the cut's record count.
// A valid maximum-depth assignment occupies 359 message bytes in this version.
const (
	attemptAssignVerificationCapacity = 64
	attemptAssignMessagePrefixBytes   = len(connect.VerifyCtx) + 2 + 16 + connect.VerifyNonceSize + ed25519.PublicKeySize + 2
	attemptAssignMessageMaxBytes      = attemptAssignMessagePrefixBytes + 16*connect.VerifyMMax
)

// The algorithm is fixed to Ed25519 assignment signatures. Exact owned bytes,
// not a key ID, trail ID or message digest, identify one successful check.
type attemptAssignVerificationTuple struct {
	publicKey   [ed25519.PublicKeySize]byte
	signature   [ed25519.SignatureSize]byte
	message     [attemptAssignMessageMaxBytes]byte
	messageSize uint16
}

// One sequential cut verification owns this bounded FIFO. The ring and map
// each retain at most 64 tuples; eviction only causes ordinary reverification.
// No entries cross cut/public calls, and every use receives freshly resolved
// server-key bytes from the full record verifier.
type attemptAssignVerificationCache struct {
	verifySignature    func([]byte, []byte, []byte) bool
	verifiedKVs        map[attemptAssignVerificationTuple]struct{}
	verificationTuples [attemptAssignVerificationCapacity]attemptAssignVerificationTuple
	nextIndex          int
}

// Retains successes only within the current canonical assignment wire shape.
// Other widths/domains pass through the primitive without acquiring storage;
// record verification remains responsible for all canonical/context checks.
func (self *attemptAssignVerificationCache) verify(publicKey, message, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || len(message) < attemptAssignMessagePrefixBytes+2*16 || len(message) > attemptAssignMessageMaxBytes {
		return self.verifySignature(publicKey, message, signature)
	}
	m, depth := int(message[attemptAssignMessagePrefixBytes-2]), int(message[attemptAssignMessagePrefixBytes-1])
	if !bytes.Equal(message[:len(connect.VerifyCtx)], []byte(connect.VerifyCtx)) || message[len(connect.VerifyCtx)] != connect.VerifyMsgTypeAssign || m < connect.VerifyMMin || m > connect.VerifyMMax || depth < 2 || depth > m || len(message) != attemptAssignMessagePrefixBytes+16*depth {
		return self.verifySignature(publicKey, message, signature)
	}
	// Copy before invoking the primitive: even an injected synchronous key or
	// input mutation cannot attach its success to a different tuple.
	tuple := attemptAssignVerificationTuple{messageSize: uint16(len(message))}
	copy(tuple.publicKey[:], publicKey)
	copy(tuple.signature[:], signature)
	copy(tuple.message[:], message)
	if _, found := self.verifiedKVs[tuple]; found {
		return true
	}
	if !self.verifySignature(publicKey, message, signature) {
		return false
	}
	if self.verifiedKVs == nil {
		self.verifiedKVs = make(map[attemptAssignVerificationTuple]struct{}, attemptAssignVerificationCapacity)
	}
	if len(self.verifiedKVs) == attemptAssignVerificationCapacity {
		delete(self.verifiedKVs, self.verificationTuples[self.nextIndex])
	}
	self.verificationTuples[self.nextIndex] = tuple
	self.verifiedKVs[tuple] = struct{}{}
	self.nextIndex = (self.nextIndex + 1) % attemptAssignVerificationCapacity
	return true
}
