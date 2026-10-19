// Fixed-width VPK consent binds staging to one immutable activation, replica,
// current authenticated API session, and exact typed object. It is not a
// validator registry, historical verdict, or permission to publish evidence.
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
	ValidatorAttemptUploadHeader       = "X-Urnetwork-Validator-Upload"
	ValidatorAttemptUploadAction       = "urnetwork/validator-attempt-upload/v1:POST:/sn/attempt-artifact"
	ValidatorAttemptUploadPayloadSize  = len(ValidatorAttemptUploadAction) + 1 + 32 + 32 + 8 + 32 + 1 + 32 + 8 + 8
	ValidatorAttemptUploadEnvelopeSize = ValidatorAttemptUploadPayloadSize + ed25519.SignatureSize
)

// ActivationHash already commits the complete chain/deployment/operator/hotkey
// domain. ReplicaNoID names the destination, not the activation's operator:
// another independent operator must be able to host this same signed object.
type ValidatorAttemptUploadIntent struct {
	ActivationHash [32]byte
	VPK            [32]byte
	ReplicaNoID    uint64
	SessionHash    [32]byte
	Kind           byte
	ContentHash    [32]byte
	Size           uint64
	NotAfter       uint64
}

// No zero owner, unsupported object type, lossy counter, or unlimited expiry
// reaches hashing. The receiver separately enforces its trusted time window.
func (self ValidatorAttemptUploadIntent) Validate() error {
	const maximumExactInteger = 9007199254740991
	if self.ActivationHash == ([32]byte{}) || self.VPK == ([32]byte{}) ||
		self.ReplicaNoID == 0 || self.SessionHash == ([32]byte{}) ||
		self.Kind < 1 || self.Kind > 3 || self.ContentHash == ([32]byte{}) ||
		self.Size == 0 || self.Size > maximumExactInteger || self.NotAfter == 0 || self.NotAfter > maximumExactInteger {
		return errors.New("validator upload intent is incomplete or exceeds fixed bounds")
	}
	return nil
}

// The representation owns its bytes and has no optional/trailing fields.
func (self ValidatorAttemptUploadIntent) Payload() ([]byte, error) {
	if err := self.Validate(); err != nil {
		return nil, err
	}
	data := make([]byte, 0, ValidatorAttemptUploadPayloadSize)
	data = append(data, ValidatorAttemptUploadAction...)
	data = append(data, 0)
	data = append(data, self.ActivationHash[:]...)
	data = append(data, self.VPK[:]...)
	data = binary.BigEndian.AppendUint64(data, self.ReplicaNoID)
	data = append(data, self.SessionHash[:]...)
	data = append(data, self.Kind)
	data = append(data, self.ContentHash[:]...)
	data = binary.BigEndian.AppendUint64(data, self.Size)
	return binary.BigEndian.AppendUint64(data, self.NotAfter), nil
}

// Exact framing is checked before any indexing/allocation. Fixed array copies
// retain no caller-owned memory and decoding alone never authorizes a request.
func DecodeValidatorAttemptUploadIntent(data []byte) (ValidatorAttemptUploadIntent, error) {
	var result ValidatorAttemptUploadIntent
	if len(data) != ValidatorAttemptUploadPayloadSize ||
		string(data[:len(ValidatorAttemptUploadAction)]) != ValidatorAttemptUploadAction ||
		data[len(ValidatorAttemptUploadAction)] != 0 {
		return result, errors.New("validator upload intent framing differs")
	}
	offset := len(ValidatorAttemptUploadAction) + 1
	take := func(size int) []byte { part := data[offset : offset+size]; offset += size; return part }
	copy(result.ActivationHash[:], take(32))
	copy(result.VPK[:], take(32))
	result.ReplicaNoID = binary.BigEndian.Uint64(take(8))
	copy(result.SessionHash[:], take(32))
	result.Kind = take(1)[0]
	copy(result.ContentHash[:], take(32))
	result.Size = binary.BigEndian.Uint64(take(8))
	result.NotAfter = binary.BigEndian.Uint64(take(8))
	if err := result.Validate(); err != nil {
		return ValidatorAttemptUploadIntent{}, err
	}
	return result, nil
}

// Both directions hash the exact compact JWT accepted by the existing API
// client-auth wrapper. Refresh signs a fresh intent; an old account's observed
// intent cannot be replayed with another live account or refreshed credential.
func ValidatorAttemptUploadSessionHash(credential string) ([32]byte, error) {
	if len(credential) == 0 || len(credential) > 16*1024 {
		return [32]byte{}, errors.New("validator upload session is unbounded or absent")
	}
	for _, value := range []byte(credential) {
		if value < 0x21 || value > 0x7e {
			return [32]byte{}, errors.New("validator upload session is not an exact compact credential")
		}
	}
	return sha256.Sum256([]byte(credential)), nil
}

// Sign only a consistent private key for this operator-scoped VPK. The caller
// has already authenticated its activation; possession cannot establish that.
func (self ValidatorAttemptUploadIntent) Sign(privateKey ed25519.PrivateKey) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", errors.New("validator upload signing owner differs")
	}
	owned := ed25519.NewKeyFromSeed(privateKey[:ed25519.SeedSize])
	if subtle.ConstantTimeCompare(privateKey, owned) != 1 || subtle.ConstantTimeCompare(owned[ed25519.SeedSize:], self.VPK[:]) != 1 {
		return "", errors.New("validator upload signing owner differs")
	}
	payload, err := self.Payload()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	envelope := append(payload, ed25519.Sign(owned, digest[:])...)
	return base64.RawURLEncoding.EncodeToString(envelope), nil
}

// Header length and canonical encoding are admitted before decoding. Expected
// activation/VPK/session/route/object/freshness remain independent caller checks.
func VerifyValidatorAttemptUploadHeader(header string) (ValidatorAttemptUploadIntent, error) {
	var zero ValidatorAttemptUploadIntent
	if len(header) != base64.RawURLEncoding.EncodedLen(ValidatorAttemptUploadEnvelopeSize) {
		return zero, errors.New("validator upload envelope length differs")
	}
	envelope, err := base64.RawURLEncoding.Strict().DecodeString(header)
	if err != nil || len(envelope) != ValidatorAttemptUploadEnvelopeSize ||
		base64.RawURLEncoding.EncodeToString(envelope) != header {
		return zero, errors.New("validator upload envelope is noncanonical")
	}
	intent, err := DecodeValidatorAttemptUploadIntent(envelope[:ValidatorAttemptUploadPayloadSize])
	if err != nil {
		return zero, err
	}
	digest := sha256.Sum256(envelope[:ValidatorAttemptUploadPayloadSize])
	if !ed25519.Verify(ed25519.PublicKey(intent.VPK[:]), digest[:], envelope[ValidatorAttemptUploadPayloadSize:]) {
		return zero, errors.New("validator upload VPK consent is invalid")
	}
	return intent, nil
}

// A refreshed JWT, nonce-free signature, or retry expiry cannot mint another
// object reservation. Each retry still spends the separate finite attempt
// counter and must complete real immutable write/readback before success.
func (self ValidatorAttemptUploadIntent) ObjectReservationHash() ([32]byte, error) {
	if err := self.Validate(); err != nil {
		return [32]byte{}, err
	}
	data := append([]byte("urnetwork/validator-upload-object/v1\x00"), self.ActivationHash[:]...)
	data = append(data, self.Kind)
	data = append(data, self.ContentHash[:]...)
	data = binary.BigEndian.AppendUint64(data, self.Size)
	return sha256.Sum256(data), nil
}
