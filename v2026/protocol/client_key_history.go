// Operator signatures preserve accountable client-key API statements. They
// do not establish global availability or physical wall-clock existence; the
// validator's separate native source commitment anchors the bytes it used.
package protocol

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	ClientKeyRegistrationSchema = "urnetwork-operator-client-key-registration-v1"
	ClientKeyObservationSchema  = "urnetwork-operator-client-key-observation-v1"
	MaxClientKeyStatementBytes  = 8 * 1024
)

// Registration must work before validator activation. This immutable domain
// deliberately excludes activation, a closed census and mutable operator UID.
type ClientKeyHistoryDomain struct {
	ChainID          uint64         `json:"chain_id"`
	GenesisHash      [32]byte       `json:"genesis_hash"`
	Netuid           uint16         `json:"netuid"`
	Coordinator      common.Address `json:"coordinator"`
	SettlementVault  common.Address `json:"settlement_vault"`
	DeploymentIDHash [32]byte       `json:"deployment_id_hash"`
	PolicyHash       [32]byte       `json:"policy_hash"`
	NoID             uint64         `json:"no_id"`
}

// A client or operator cannot migrate an existing generation across domains.
func (self ClientKeyHistoryDomain) Validate() error {
	if self.ChainID == 0 || self.GenesisHash == ([32]byte{}) || self.Netuid == 0 || self.Coordinator == (common.Address{}) || self.SettlementVault == (common.Address{}) || self.DeploymentIDHash == ([32]byte{}) || self.PolicyHash == ([32]byte{}) || self.NoID == 0 {
		return errors.New("client-key history deployment or operator domain is incomplete")
	}
	return nil
}

// Uses fixed-width tagged signing fields; JSON spellings are not an authority.
func (self ClientKeyHistoryDomain) payload() []byte {
	data := binary.BigEndian.AppendUint64(nil, self.ChainID)
	data = append(data, self.GenesisHash[:]...)
	data = binary.BigEndian.AppendUint16(data, self.Netuid)
	data = append(data, self.Coordinator[:]...)
	data = append(data, self.SettlementVault[:]...)
	data = append(data, self.DeploymentIDHash[:]...)
	data = append(data, self.PolicyHash[:]...)
	return binary.BigEndian.AppendUint64(data, self.NoID)
}

// Names one immutable database/public-history namespace without relying on a
// display deployment name or a mutable current operator configuration.
func (self ClientKeyHistoryDomain) Digest() ([32]byte, error) {
	if err := self.Validate(); err != nil {
		return [32]byte{}, err
	}
	data := append([]byte("urnetwork-operator-client-key-domain-v1"), 0)
	return sha256.Sum256(append(data, self.payload()...)), nil
}

// The operator observed this canonical finalized EVM identity while handling
// the API operation. It is not a claim that the API call occurred in that block.
type ClientKeyEffectiveBoundary struct {
	Epoch uint64   `json:"epoch"`
	Block uint64   `json:"block"`
	Hash  [32]byte `json:"hash"`
}

// Epoch zero is valid at first startup; a missing block identity is not.
func (self ClientKeyEffectiveBoundary) Validate() error {
	if self.Block == 0 || self.Hash == ([32]byte{}) {
		return errors.New("client-key effective boundary is incomplete")
	}
	return nil
}

// Encodes only the explicit finalized identity, never a local clock field.
func (self ClientKeyEffectiveBoundary) payload() []byte {
	data := binary.BigEndian.AppendUint64(nil, self.Epoch)
	data = binary.BigEndian.AppendUint64(data, self.Block)
	return append(data, self.Hash[:]...)
}

// Every actual key transition advances one durable generation. Empty keys
// are signed tombstones, while identical retries reuse the original record.
type ClientKeyRegistration struct {
	Schema            string                     `json:"schema"`
	Domain            ClientKeyHistoryDomain     `json:"domain"`
	ClientID          [16]byte                   `json:"client_id"`
	NetworkID         [16]byte                   `json:"network_id"`
	Generation        uint64                     `json:"generation"`
	Present           bool                       `json:"present"`
	PublicKey         [32]byte                   `json:"public_key"`
	PreviousHash      [32]byte                   `json:"previous_hash"`
	EffectiveBoundary ClientKeyEffectiveBoundary `json:"effective_boundary"`
	Signer            common.Address             `json:"signer"`
	Signature         [65]byte                   `json:"signature"`
}

// A zero public key never means a present Ed25519 identity. The predecessor
// and exact contiguous generation are checked separately against owned history.
func (self ClientKeyRegistration) Digest() ([32]byte, error) {
	if err := errors.Join(self.Domain.Validate(), self.EffectiveBoundary.Validate()); err != nil {
		return [32]byte{}, err
	}
	if self.Schema != ClientKeyRegistrationSchema || self.ClientID == ([16]byte{}) || self.NetworkID == ([16]byte{}) || self.Generation == 0 || self.Signer == (common.Address{}) || self.Present == (self.PublicKey == ([32]byte{})) || (self.Generation == 1) != (self.PreviousHash == ([32]byte{})) {
		return [32]byte{}, errors.New("client-key registration identity, generation or key is invalid")
	}
	data := append([]byte(ClientKeyRegistrationSchema), 0)
	data = append(data, self.Domain.payload()...)
	data = append(data, self.ClientID[:]...)
	data = append(data, self.NetworkID[:]...)
	data = binary.BigEndian.AppendUint64(data, self.Generation)
	if self.Present {
		data = append(data, 1)
	} else {
		data = append(data, 0)
	}
	data = append(data, self.PublicKey[:]...)
	data = append(data, self.PreviousHash[:]...)
	data = append(data, self.EffectiveBoundary.payload()...)
	data = append(data, self.Signer[:]...)
	return sha256.Sum256(data), nil
}

// Signers are supplied only by the real operator-owned API implementation,
// not a public request or validator artifact. Failure leaves the input intact.
func SignClientKeyRegistration(registration *ClientKeyRegistration, key *ecdsa.PrivateKey) error {
	if registration == nil || !validClientKeyStatementSigner(key) {
		return errors.New("client-key registration signing owner is unavailable")
	}
	owned := *registration
	owned.Schema, owned.Signer = ClientKeyRegistrationSchema, crypto.PubkeyToAddress(key.PublicKey)
	digest, err := owned.Digest()
	if err != nil {
		return err
	}
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		return err
	}
	copy(owned.Signature[:], signature)
	*registration = owned
	return nil
}

// Refuse malformed local key owners without panicking or publishing a partial
// signature. No public API may supply this value.
func validClientKeyStatementSigner(key *ecdsa.PrivateKey) bool {
	if key == nil || key.D == nil || key.Curve != crypto.S256() || key.X == nil || key.Y == nil || key.D.Sign() <= 0 || key.D.Cmp(crypto.S256().Params().N) >= 0 || !crypto.S256().IsOnCurve(key.X, key.Y) {
		return false
	}
	x, y := crypto.S256().ScalarBaseMult(key.D.Bytes())
	return x.Cmp(key.X) == 0 && y.Cmp(key.Y) == 0
}

// Require canonical low-s recoverable signatures. The caller must separately
// authenticate the expected signer in the operator's pinned chain version.
func verifyClientKeyStatementSignature(digest [32]byte, signature [65]byte, signer common.Address) error {
	if signer == (common.Address{}) || !crypto.ValidateSignatureValues(signature[64], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:64]), true) {
		return errors.New("client-key statement signature is not canonical")
	}
	publicKey, err := crypto.SigToPub(digest[:], signature[:])
	if err != nil || crypto.PubkeyToAddress(*publicKey) != signer {
		return errors.Join(errors.New("client-key statement operator signature differs"), err)
	}
	return nil
}

// This verifies a statement, not an onchain signer or history verdict.
func (self ClientKeyRegistration) VerifySignature() error {
	digest, err := self.Digest()
	if err != nil {
		return err
	}
	return verifyClientKeyStatementSignature(digest, self.Signature, self.Signer)
}

// Content custody covers the complete canonical signed bytes, not the signing
// digest. The two hashes must never be interchanged in predecessor links.
func (self ClientKeyRegistration) Bytes() ([]byte, error) {
	if err := self.VerifySignature(); err != nil {
		return nil, err
	}
	return json.Marshal(self)
}

// Returns the exact immutable byte identity used by history and observations.
func (self ClientKeyRegistration) ContentHash() ([32]byte, error) {
	encoded, err := self.Bytes()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// An append cannot skip history, change a client's deployment/network, repeat
// its current value as a new generation, or roll back the observed boundary.
func (self ClientKeyRegistration) Follows(prior *ClientKeyRegistration) error {
	if err := self.VerifySignature(); err != nil {
		return err
	}
	if prior == nil {
		if self.Generation != 1 || self.PreviousHash != ([32]byte{}) {
			return errors.New("client-key registration has no exact initial predecessor")
		}
		return nil
	}
	priorHash, err := prior.ContentHash()
	if err != nil {
		return err
	}
	if prior.Generation == math.MaxUint64 || self.Generation != prior.Generation+1 || self.PreviousHash != priorHash || self.Domain != prior.Domain || self.ClientID != prior.ClientID || self.NetworkID != prior.NetworkID || self.Present == prior.Present && self.PublicKey == prior.PublicKey {
		return errors.New("client-key registration does not extend its exact generation history")
	}
	if self.EffectiveBoundary.Epoch < prior.EffectiveBoundary.Epoch || self.EffectiveBoundary.Block < prior.EffectiveBoundary.Block || self.EffectiveBoundary.Block == prior.EffectiveBoundary.Block && self.EffectiveBoundary != prior.EffectiveBoundary {
		return errors.New("client-key registration rolls back its effective boundary")
	}
	return nil
}

// A fresh request binds the API read to one validator decision and prevents a
// transport from substituting an earlier signed response after key rotation.
type ClientKeyObservationRequest struct {
	ClientID         [16]byte                   `json:"client_id"`
	ValidatorHotkey  [32]byte                   `json:"validator_hotkey"`
	NativeBlock      uint64                     `json:"native_block"`
	NativeHash       [32]byte                   `json:"native_hash"`
	NativeEpoch      uint64                     `json:"native_epoch"`
	DecisionBoundary ClientKeyEffectiveBoundary `json:"decision_boundary"`
	Nonce            [32]byte                   `json:"nonce"`
}

// The native and EVM hashes remain different explicit clocks.
func (self ClientKeyObservationRequest) Validate() error {
	if self.ClientID == ([16]byte{}) || self.ValidatorHotkey == ([32]byte{}) || self.NativeBlock == 0 || self.NativeHash == ([32]byte{}) || self.Nonce == ([32]byte{}) {
		return errors.New("client-key observation request identity is incomplete")
	}
	return self.DecisionBoundary.Validate()
}

// API captures may share a decision, but never another request's nonce.
func (self ClientKeyObservationRequest) payload() []byte {
	data := append([]byte(nil), self.ClientID[:]...)
	data = append(data, self.ValidatorHotkey[:]...)
	data = binary.BigEndian.AppendUint64(data, self.NativeBlock)
	data = append(data, self.NativeHash[:]...)
	data = binary.BigEndian.AppendUint64(data, self.NativeEpoch)
	data = append(data, self.DecisionBoundary.payload()...)
	return append(data, self.Nonce[:]...)
}

// The operator signs the exact durable registration that its current API read
// observed. PublicKey is intentionally not a request parameter or repeated here.
type ClientKeyObservation struct {
	Schema           string                      `json:"schema"`
	Domain           ClientKeyHistoryDomain      `json:"domain"`
	ClientID         [16]byte                    `json:"client_id"`
	Generation       uint64                      `json:"generation"`
	RegistrationHash [32]byte                    `json:"registration_hash"`
	Request          ClientKeyObservationRequest `json:"request"`
	Signer           common.Address              `json:"signer"`
	Signature        [65]byte                    `json:"signature"`
}

// Signs a request-bound head, not an arbitrary caller-supplied message.
func (self ClientKeyObservation) Digest() ([32]byte, error) {
	if err := errors.Join(self.Domain.Validate(), self.Request.Validate()); err != nil {
		return [32]byte{}, err
	}
	if self.Schema != ClientKeyObservationSchema || self.ClientID == ([16]byte{}) || self.ClientID != self.Request.ClientID || self.Generation == 0 || self.RegistrationHash == ([32]byte{}) || self.Signer == (common.Address{}) {
		return [32]byte{}, errors.New("client-key observation current registration is incomplete")
	}
	data := append([]byte(ClientKeyObservationSchema), 0)
	data = append(data, self.Domain.payload()...)
	data = append(data, self.ClientID[:]...)
	data = binary.BigEndian.AppendUint64(data, self.Generation)
	data = append(data, self.RegistrationHash[:]...)
	data = append(data, self.Request.payload()...)
	data = append(data, self.Signer[:]...)
	return sha256.Sum256(data), nil
}

// Registration selection belongs to the actual model reader before this call.
func SignClientKeyObservation(observation *ClientKeyObservation, key *ecdsa.PrivateKey) error {
	if observation == nil || !validClientKeyStatementSigner(key) {
		return errors.New("client-key observation signing owner is unavailable")
	}
	owned := *observation
	owned.Schema, owned.Signer = ClientKeyObservationSchema, crypto.PubkeyToAddress(key.PublicKey)
	digest, err := owned.Digest()
	if err != nil {
		return err
	}
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		return err
	}
	copy(owned.Signature[:], signature)
	*observation = owned
	return nil
}

// Transport decoding does not confer independently pinned signer authority.
func (self ClientKeyObservation) VerifySignature() error {
	digest, err := self.Digest()
	if err != nil {
		return err
	}
	return verifyClientKeyStatementSignature(digest, self.Signature, self.Signer)
}

// Makes the exact signed response suitable for bounded immutable custody.
func (self ClientKeyObservation) Bytes() ([]byte, error) {
	if err := self.VerifySignature(); err != nil {
		return nil, err
	}
	return json.Marshal(self)
}

// Both operator signer arguments must come from independent pinned versions:
// registration at its effective boundary, observation at the decision boundary.
func (self ClientKeyObservation) VerifyRegistration(registration ClientKeyRegistration, domain ClientKeyHistoryDomain, request ClientKeyObservationRequest, registrationSigner, observationSigner common.Address) error {
	if err := errors.Join(domain.Validate(), request.Validate(), self.VerifySignature(), registration.VerifySignature()); err != nil {
		return err
	}
	registrationHash, err := registration.ContentHash()
	if err != nil {
		return err
	}
	if self.Domain != domain || registration.Domain != domain || self.Request != request || self.ClientID != registration.ClientID || self.Generation != registration.Generation || self.RegistrationHash != registrationHash || self.Signer != observationSigner || registration.Signer != registrationSigner {
		return errors.New("client-key observation differs from the independent request, registration or operator authority")
	}
	if registration.EffectiveBoundary.Epoch > request.DecisionBoundary.Epoch || registration.EffectiveBoundary.Block > request.DecisionBoundary.Block || registration.EffectiveBoundary.Block == request.DecisionBoundary.Block && registration.EffectiveBoundary != request.DecisionBoundary {
		return errors.New("client-key registration effective boundary follows the decision")
	}
	return nil
}

// Fixed bounded canonical decoding rejects unknown fields, alternate JSON,
// duplicate fields, trailing values and truncated statements before publication.
func decodeClientKeyStatement(encoded []byte, statement any) error {
	if len(encoded) == 0 || len(encoded) > MaxClientKeyStatementBytes {
		return errors.New("client-key statement exceeds its fixed byte bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(statement); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("client-key statement has trailing JSON")
	}
	canonical, err := json.Marshal(statement)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return errors.Join(errors.New("client-key statement JSON is not canonical"), err)
	}
	return nil
}

// Failure never returns a partially decoded, valid-looking registration.
func DecodeClientKeyRegistration(encoded []byte) (ClientKeyRegistration, error) {
	var registration ClientKeyRegistration
	if err := decodeClientKeyStatement(encoded, &registration); err != nil {
		return ClientKeyRegistration{}, err
	}
	if err := registration.VerifySignature(); err != nil {
		return ClientKeyRegistration{}, err
	}
	return registration, nil
}

// The decoded response still requires the exact independent request comparison.
func DecodeClientKeyObservation(encoded []byte) (ClientKeyObservation, error) {
	var observation ClientKeyObservation
	if err := decodeClientKeyStatement(encoded, &observation); err != nil {
		return ClientKeyObservation{}, err
	}
	if err := observation.VerifySignature(); err != nil {
		return ClientKeyObservation{}, err
	}
	return observation, nil
}
