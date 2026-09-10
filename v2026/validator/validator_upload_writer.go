// Reserved writes retain the ordinary exact HTTP ownership contract and add
// account-bound VPK consent. The server, not this constructor or a YAML file,
// authenticates the existing activation and historical/current eligibility.
package validator

import (
	"context"
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Values are owned and immutable after admission, including the signing key.
// Destination is independent of the original activation operator.
type validatorAttemptUploadSigner struct {
	activation           protocol.ValidatorEvidenceActivation
	digest               [32]byte
	replicaNoID          uint64
	maximumIntentSeconds uint64
	privateKey           [ed25519.PrivateKeySize]byte
}

// No credential callback or HTTP request runs before complete key/domain and
// finite intent-lifetime admission. A proof of possession is not eligibility.
func newValidatorAttemptUploadSigner(activation protocol.ValidatorEvidenceActivation, replicaNoID, maximumIntentSeconds uint64, privateKey ed25519.PrivateKey) (*validatorAttemptUploadSigner, error) {
	if replicaNoID == 0 || maximumIntentSeconds == 0 || maximumIntentSeconds > 3600 || len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("reserved upload signer or intent lifetime is incomplete")
	}
	digest, err := activation.Digest()
	if err != nil {
		return nil, err
	}
	self := &validatorAttemptUploadSigner{activation: activation, digest: digest, replicaNoID: replicaNoID, maximumIntentSeconds: maximumIntentSeconds}
	copy(self.privateKey[:], privateKey)
	// Reuse the protocol's constant-time private/public consistency check.
	if _, err := activation.SignVPK(ed25519.PrivateKey(self.privateKey[:])); err != nil {
		return nil, err
	}
	return self, nil
}

// The production clock cannot be selected by a request or credential getter.
func (self *validatorAttemptUploadSigner) header(credential, kind string, contentHash [32]byte, size uint64) (string, error) {
	return self.headerAt(credential, kind, contentHash, size, time.Now())
}

// Exact current credentials are fetched once by the outer HTTP writer; the
// same bytes are used for Authorization and this signed session hash.
func (self *validatorAttemptUploadSigner) headerAt(credential, kind string, contentHash [32]byte, size uint64, now time.Time) (string, error) {
	if self == nil || now.Unix() <= 0 || uint64(now.Unix()) > 9007199254740991-self.maximumIntentSeconds {
		return "", errors.New("reserved upload signer clock is invalid")
	}
	sessionHash, err := protocol.ValidatorAttemptUploadSessionHash(credential)
	if err != nil {
		return "", err
	}
	objectKind := byte(0)
	switch kind {
	case "metadata":
		objectKind = 1
	case AttemptStreamV2Records:
		objectKind = 2
	case AttemptStreamV2Proofs:
		objectKind = 3
	default:
		return "", errors.New("reserved upload object kind is unknown")
	}
	intent := protocol.ValidatorAttemptUploadIntent{ActivationHash: self.digest, VPK: self.activation.VPK, ReplicaNoID: self.replicaNoID, SessionHash: sessionHash,
		Kind: objectKind, ContentHash: contentHash, Size: size, NotAfter: uint64(now.Unix()) + self.maximumIntentSeconds}
	return intent.Sign(ed25519.PrivateKey(self.privateKey[:]))
}

// Safe for concurrent writes; the base owns the live destination API getter
// and the signer owns the source activation's VPK, never an operator key.
type ValidatorReservedAttemptStreamV2Writer struct {
	base   *HTTPAttemptStreamV2Writer
	signer *validatorAttemptUploadSigner
}

// Intended for an existing SDK API session. Constructor success proves route,
// finite bounds and signing possession, not the server's cached eligibility.
func NewValidatorReservedAttemptStreamV2Writer(origin string, bounds AttemptCutV2Bounds, byJwt func() string, activation protocol.ValidatorEvidenceActivation, replicaNoID, maximumIntentSeconds uint64, privateKey ed25519.PrivateKey) (*ValidatorReservedAttemptStreamV2Writer, error) {
	signer, err := newValidatorAttemptUploadSigner(activation, replicaNoID, maximumIntentSeconds, privateKey)
	if err != nil {
		return nil, err
	}
	base, err := NewHTTPAttemptStreamV2Writer(origin, bounds, byJwt)
	if err != nil {
		return nil, err
	}
	return &ValidatorReservedAttemptStreamV2Writer{base: base, signer: signer}, nil
}

// Every reserved request receives a fresh current-account-bound signature.
// Refresh changes the signature but not the bounded object reservation key.
func (self *ValidatorReservedAttemptStreamV2Writer) Write(ctx context.Context, kind, contentHash string, data []byte) error {
	if self == nil || self.base == nil || self.signer == nil {
		return errors.New("reserved upload writer is unavailable")
	}
	return self.base.write(ctx, kind, contentHash, data, self.signer)
}
