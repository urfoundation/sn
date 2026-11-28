// A validator's VPK consent to be charged as a validator: the operator's api
// mints a short-lived rate class token for it while the reserved upload
// admission admits the activation, and the operator's lb charges the
// validator's requests that carry that token to the validator rate class. It
// binds one immutable activation, the destination replica and the current
// authenticated API session, as an upload consent does, under an action of
// its own, so neither consent can stand in for the other.
package protocol

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
)

const (
	// The intent's header on POST /sn/validator-class.
	ValidatorClassIntentHeader = "X-UR-Validator-Class-Intent"
	// The minted class token's header on every request to the api.
	ValidatorClassHeader       = "X-UR-Validator-Class"
	ValidatorClassAction       = "urnetwork/validator-class/v1:POST:/sn/validator-class"
	ValidatorClassPayloadSize  = len(ValidatorClassAction) + 1 + 32 + 32 + 8 + 32 + 8
	ValidatorClassEnvelopeSize = ValidatorClassPayloadSize + ed25519.SignatureSize
)

type ValidatorClassIntent struct {
	ActivationHash [32]byte
	VPK            [32]byte
	ReplicaNoID    uint64
	SessionHash    [32]byte
	NotAfter       uint64
}

// No zero owner, destination or session, and no unlimited expiry, reaches
// hashing. The receiver separately enforces its trusted time window.
func (self ValidatorClassIntent) Validate() error {
	const maximumExactInteger = 9007199254740991
	if self.ActivationHash == ([32]byte{}) || self.VPK == ([32]byte{}) || self.ReplicaNoID == 0 ||
		self.SessionHash == ([32]byte{}) || self.NotAfter == 0 || self.NotAfter > maximumExactInteger {
		return errors.New("validator class intent is incomplete or exceeds fixed bounds")
	}
	return nil
}

// The representation owns its bytes and has no optional or trailing fields.
func (self ValidatorClassIntent) Payload() ([]byte, error) {
	if err := self.Validate(); err != nil {
		return nil, err
	}
	data := make([]byte, 0, ValidatorClassPayloadSize)
	data = append(data, ValidatorClassAction...)
	data = append(data, 0)
	data = append(data, self.ActivationHash[:]...)
	data = append(data, self.VPK[:]...)
	data = binary.BigEndian.AppendUint64(data, self.ReplicaNoID)
	data = append(data, self.SessionHash[:]...)
	return binary.BigEndian.AppendUint64(data, self.NotAfter), nil
}

// Exact framing is checked before any indexing. Decoding alone never
// authorizes anything.
func DecodeValidatorClassIntent(data []byte) (ValidatorClassIntent, error) {
	var result ValidatorClassIntent
	if len(data) != ValidatorClassPayloadSize ||
		string(data[:len(ValidatorClassAction)]) != ValidatorClassAction ||
		data[len(ValidatorClassAction)] != 0 {
		return result, errors.New("validator class intent framing differs")
	}
	offset := len(ValidatorClassAction) + 1
	take := func(size int) []byte { part := data[offset : offset+size]; offset += size; return part }
	copy(result.ActivationHash[:], take(32))
	copy(result.VPK[:], take(32))
	result.ReplicaNoID = binary.BigEndian.Uint64(take(8))
	copy(result.SessionHash[:], take(32))
	result.NotAfter = binary.BigEndian.Uint64(take(8))
	if err := result.Validate(); err != nil {
		return ValidatorClassIntent{}, err
	}
	return result, nil
}

// Signs with the operator-scoped VPK's own private key only.
func (self ValidatorClassIntent) Sign(privateKey ed25519.PrivateKey) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", errors.New("validator class signing owner differs")
	}
	owned := ed25519.NewKeyFromSeed(privateKey[:ed25519.SeedSize])
	if subtle.ConstantTimeCompare(privateKey, owned) != 1 || subtle.ConstantTimeCompare(owned[ed25519.SeedSize:], self.VPK[:]) != 1 {
		return "", errors.New("validator class signing owner differs")
	}
	payload, err := self.Payload()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	envelope := append(payload, ed25519.Sign(owned, digest[:])...)
	return base64.RawURLEncoding.EncodeToString(envelope), nil
}

// Header length and canonical encoding are admitted before decoding. The
// expected activation, replica, session and freshness remain the caller's
// checks.
func VerifyValidatorClassHeader(header string) (ValidatorClassIntent, error) {
	var zero ValidatorClassIntent
	if len(header) != base64.RawURLEncoding.EncodedLen(ValidatorClassEnvelopeSize) {
		return zero, errors.New("validator class envelope length differs")
	}
	envelope, err := base64.RawURLEncoding.Strict().DecodeString(header)
	if err != nil || len(envelope) != ValidatorClassEnvelopeSize ||
		base64.RawURLEncoding.EncodeToString(envelope) != header {
		return zero, errors.New("validator class envelope is noncanonical")
	}
	intent, err := DecodeValidatorClassIntent(envelope[:ValidatorClassPayloadSize])
	if err != nil {
		return zero, err
	}
	digest := sha256.Sum256(envelope[:ValidatorClassPayloadSize])
	if !ed25519.Verify(ed25519.PublicKey(intent.VPK[:]), digest[:], envelope[ValidatorClassPayloadSize:]) {
		return zero, errors.New("validator class VPK consent is invalid")
	}
	return intent, nil
}
