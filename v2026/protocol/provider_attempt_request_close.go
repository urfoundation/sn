// An owner may permanently close its own original request after transport
// drain. A Server tombstone is useful only when it also fences future execution.
package protocol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/urnetwork/connect/v2026"
)

const (
	ProviderAttemptRequestCloseDomain     = "urnetwork-provider-attempt-request-close-v1\x00"
	ProviderAttemptClosedUnreceivedDomain = "urnetwork-provider-attempt-closed-unreceived-v1\x00"
)

// The request owner signs once after its actual cut fence. A copied request
// signature alone cannot authorize a third party to close the request early.
type ProviderAttemptRequestClosure struct {
	Schema           string                      `json:"schema"`
	Scope            ProviderAttemptReceiptScope `json:"scope"`
	ClientId         connect.Id                  `json:"client_id"`
	Message          []byte                      `json:"message"`
	RequestSignature []byte                      `json:"request_signature"`
	CutHash          [32]byte                    `json:"cut_hash"`
	Epoch            uint64                      `json:"epoch"`
	EndBlock         uint64                      `json:"end_block"`
	Signature        []byte                      `json:"signature"`
}

// The signature commits only an immutable execution fence for this request.
// It makes no assertion about global coverage, eligibility or physical traffic.
type ProviderAttemptClosedUnreceived struct {
	Body      []byte `json:"body"`
	Signature []byte `json:"signature"`
}

// A key identifier is a selector into independently admitted historical keys.
type ProviderAttemptClosedUnreceivedBody struct {
	Schema      string   `json:"schema"`
	ClosureHash [32]byte `json:"closure_hash"`
	ServerKeyId byte     `json:"server_key_id"`
}

// Parse exact existing seed/extend wire before using its embedded signing key.
func VerifyProviderAttemptRequestWire(message, signature []byte) ([32]byte, error) {
	var key [32]byte
	prefix := len(connect.VerifyCtx)
	if !bytes.HasPrefix(message, []byte(connect.VerifyCtx)) || len(message) <= prefix || len(signature) != ed25519.SignatureSize {
		return key, ErrProviderAttemptsIntegrity
	}
	offset := prefix + 1
	switch message[prefix] {
	case connect.VerifyMsgTypeSeed:
		if len(message) != offset+32+connect.VerifyNonceSize+1 {
			return key, ErrProviderAttemptsIntegrity
		}
		copy(key[:], message[offset:offset+32])
	case connect.VerifyMsgTypeExtend:
		if len(message) < offset+16+connect.VerifyNonceSize+32+2 {
			return key, ErrProviderAttemptsIntegrity
		}
		keyOffset := offset + 16 + connect.VerifyNonceSize
		copy(key[:], message[keyOffset:keyOffset+32])
		count := int(message[keyOffset+33])
		if count == 0 || len(message) != keyOffset+34+count*16 {
			return key, ErrProviderAttemptsIntegrity
		}
	default:
		return key, ErrProviderAttemptsIntegrity
	}
	if key == ([32]byte{}) || !ed25519.Verify(ed25519.PublicKey(key[:]), message, signature) {
		return [32]byte{}, ErrProviderAttemptsIntegrity
	}
	return key, nil
}

// Canonical close encoding keeps every request/domain coordinate under consent.
func (self ProviderAttemptRequestClosure) signingBytes() ([]byte, error) {
	self.Signature = nil
	raw, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append([]byte(ProviderAttemptRequestCloseDomain), raw...), nil
}

// The exact signed closure is retained as the immutable tombstone identity.
func (self ProviderAttemptRequestClosure) Hash() ([32]byte, error) {
	raw, err := json.Marshal(self)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// The transport caller supplies its already selected original scope. Registry
// membership and cut inclusion remain separate mandatory consumer checks.
func VerifyProviderAttemptRequestClosure(ctx context.Context, value ProviderAttemptRequestClosure, scope ProviderAttemptReceiptScope) error {
	if ctx == nil {
		return errors.New("provider close owner context absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if value.Schema != ProviderAttemptRequestCloseDomain || value.Scope != scope || scope.Profile == "" || scope.DeploymentId == "" || scope.DeploymentKey == "" || scope.GenesisHash == ([32]byte{}) || scope.PolicyHash == ([32]byte{}) || scope.Netuid == 0 || scope.NoId == 0 || value.ClientId == (connect.Id{}) || value.CutHash == ([32]byte{}) || value.EndBlock == 0 || len(value.Signature) != ed25519.SignatureSize {
		return ErrProviderAttemptsIntegrity
	}
	key, err := VerifyProviderAttemptRequestWire(value.Message, value.RequestSignature)
	if err != nil {
		return err
	}
	raw, err := value.signingBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(key[:]), raw, value.Signature) {
		return ErrProviderAttemptsIntegrity
	}
	return ctx.Err()
}

// Sign only with the original wire key; copying another owner's valid request
// into a new close envelope cannot suppress that owner's future accounting.
func SealProviderAttemptRequestClosure(ctx context.Context, value ProviderAttemptRequestClosure, key ed25519.PrivateKey) (*ProviderAttemptRequestClosure, error) {
	if ctx == nil || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("provider close key or owner absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value.Signature = nil
	raw, err := value.signingBytes()
	if err != nil {
		return nil, err
	}
	value.Signature = ed25519.Sign(key, raw)
	if err := VerifyProviderAttemptRequestClosure(ctx, value, value.Scope); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var owned ProviderAttemptRequestClosure
	if err := json.Unmarshal(encoded, &owned); err != nil {
		return nil, err
	}
	return &owned, ctx.Err()
}

// The Server calls this only after its same-lock immutable fence commits.
func SealProviderAttemptClosedUnreceived(ctx context.Context, closure ProviderAttemptRequestClosure, keyId byte, key ed25519.PrivateKey) (*ProviderAttemptClosedUnreceived, error) {
	if ctx == nil || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("provider tombstone key or owner absent")
	}
	if err := VerifyProviderAttemptRequestClosure(ctx, closure, closure.Scope); err != nil {
		return nil, err
	}
	hash, err := closure.Hash()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ProviderAttemptClosedUnreceivedBody{Schema: ProviderAttemptClosedUnreceivedDomain, ClosureHash: hash, ServerKeyId: keyId})
	if err != nil {
		return nil, err
	}
	return &ProviderAttemptClosedUnreceived{Body: raw, Signature: ed25519.Sign(key, append([]byte(ProviderAttemptClosedUnreceivedDomain), raw...))}, ctx.Err()
}

// No live key lookup, absence response or operator agreement participates here.
func VerifyProviderAttemptClosedUnreceived(ctx context.Context, value ProviderAttemptClosedUnreceived, closure ProviderAttemptRequestClosure, keys map[byte]ed25519.PublicKey) error {
	if ctx == nil {
		return errors.New("provider tombstone context absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := VerifyProviderAttemptRequestClosure(ctx, closure, closure.Scope); err != nil {
		return err
	}
	if len(value.Body) == 0 || len(value.Body) > 4096 || len(value.Signature) != ed25519.SignatureSize {
		return ErrProviderAttemptsIntegrity
	}
	var body ProviderAttemptClosedUnreceivedBody
	if err := json.Unmarshal(value.Body, &body); err != nil {
		return err
	}
	canonical, err := json.Marshal(body)
	if err != nil || !bytes.Equal(canonical, value.Body) {
		return errors.Join(ErrProviderAttemptsIntegrity, err)
	}
	hash, err := closure.Hash()
	if err != nil {
		return err
	}
	key := keys[body.ServerKeyId]
	if body.Schema != ProviderAttemptClosedUnreceivedDomain || body.ClosureHash != hash || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, append([]byte(ProviderAttemptClosedUnreceivedDomain), value.Body...), value.Signature) {
		return ErrProviderAttemptsIntegrity
	}
	return ctx.Err()
}
