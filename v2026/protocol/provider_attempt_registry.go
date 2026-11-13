// An independent deployment registry fixes the complete validator/operator
// owner set before closed attempt evidence is discovered. Successors retain the
// original signing authority and exact predecessor; cuts cannot choose a roster.
package protocol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
)

const ProviderAttemptRegistrySchema = "urnetwork-provider-attempt-registry-v1"

var (
	ErrProviderAttemptsUnavailable = errors.New("original provider attempt evidence is unavailable")
	ErrProviderAttemptsIntegrity   = errors.New("original provider attempt evidence conflicts")
	ErrProviderAttemptsCapacity    = errors.New("original provider attempt evidence exceeds its admitted capacity")
)

// No activation, numeric native uid, candidate root or operator row count can
// replace these independently selected deployment coordinates.
type ProviderAttemptDomain struct {
	ChainId          uint64   `json:"chain_id"`
	GenesisHash      [32]byte `json:"genesis_hash"`
	Netuid           uint16   `json:"netuid"`
	Coordinator      [20]byte `json:"coordinator"`
	SettlementVault  [20]byte `json:"settlement_vault"`
	DeploymentIdHash [32]byte `json:"deployment_id_hash"`
	PolicyHash       [32]byte `json:"policy_hash"`
}

// Stable native hotkeys own a complete sorted set of operator lanes. Vpk
// rotation is authenticated separately at the original activation boundary.
type ProviderAttemptOwner struct {
	Hotkey [32]byte `json:"hotkey"`
	NoIds  []uint64 `json:"no_ids"`
}

// Revision zero is pinned by the consumer's original policy. A later signed
// revision takes effect at an explicit future epoch; omission never shrinks a
// candidate's independently selected active roster.
type ProviderAttemptRegistry struct {
	Schema         string                 `json:"schema"`
	Domain         ProviderAttemptDomain  `json:"domain"`
	Revision       uint64                 `json:"revision"`
	EffectiveEpoch uint64                 `json:"effective_epoch"`
	PreviousHash   [32]byte               `json:"previous_hash"`
	Owners         []ProviderAttemptOwner `json:"owners"`
	Signature      []byte                 `json:"signature"`
}

// Bounds are supplied by the original consumer resource policy. A registry
// signature authenticates deployment ownership, not physical traffic or stake.
type ProviderAttemptRegistryExpectation struct {
	Domain           ProviderAttemptDomain
	RootHash         [32]byte
	RequiredHeadHash [32]byte
	Signer           [32]byte
	MaxRevisions     uint64
	MaxOwners        uint64
	MaxOperatorLanes uint64
	MaxBytes         uint64
}

// Reject incomplete authority before any signing or public discovery.
func (self ProviderAttemptDomain) Validate() error {
	if self.ChainId == 0 || self.GenesisHash == ([32]byte{}) || self.Netuid == 0 || self.Coordinator == ([20]byte{}) || self.SettlementVault == ([20]byte{}) || self.Coordinator == self.SettlementVault || self.DeploymentIdHash == ([32]byte{}) || self.PolicyHash == ([32]byte{}) {
		return errors.Join(ErrProviderAttemptsIntegrity, errors.New("provider attempt deployment domain is incomplete"))
	}
	return nil
}

// Compares the stable deployment domain without importing a candidate's
// activation epoch/hash into the independent registry authority.
func (self ProviderAttemptDomain) MatchesEvidence(value ValidatorEvidenceDomain) bool {
	return self.ChainId == value.ChainID && self.GenesisHash == value.GenesisHash && self.Netuid == value.Netuid && self.Coordinator == value.Coordinator && self.SettlementVault == value.SettlementVault && self.DeploymentIdHash == value.DeploymentIDHash && self.PolicyHash == value.PolicyHash
}

// Own and bound the census before canonical encoding or signature operations.
func ownProviderAttemptRegistry(ctx context.Context, value ProviderAttemptRegistry, expected ProviderAttemptRegistryExpectation) (ProviderAttemptRegistry, []byte, error) {
	if ctx == nil {
		return ProviderAttemptRegistry{}, nil, errors.New("provider registry owner context is absent")
	}
	if err := ctx.Err(); err != nil {
		return ProviderAttemptRegistry{}, nil, err
	}
	if err := expected.Domain.Validate(); err != nil {
		return ProviderAttemptRegistry{}, nil, err
	}
	if expected.MaxOwners == 0 || expected.MaxOperatorLanes == 0 || expected.MaxRevisions == 0 || expected.MaxBytes == 0 || uint64(len(value.Owners)) > expected.MaxOwners {
		return ProviderAttemptRegistry{}, nil, ErrProviderAttemptsCapacity
	}
	if value.Schema != ProviderAttemptRegistrySchema || value.Domain != expected.Domain || len(value.Owners) == 0 || (value.Revision == 0) != (value.PreviousHash == ([32]byte{})) {
		return ProviderAttemptRegistry{}, nil, errors.Join(ErrProviderAttemptsIntegrity, errors.New("provider registry identity or original predecessor differs"))
	}
	owned := value
	owned.Signature = nil
	owned.Owners = make([]ProviderAttemptOwner, len(value.Owners))
	var lanes uint64
	for index, owner := range value.Owners {
		if err := ctx.Err(); err != nil {
			return ProviderAttemptRegistry{}, nil, err
		}
		if uint64(len(owner.NoIds)) > expected.MaxOperatorLanes-lanes {
			return ProviderAttemptRegistry{}, nil, ErrProviderAttemptsCapacity
		}
		lanes += uint64(len(owner.NoIds))
		if owner.Hotkey == ([32]byte{}) || len(owner.NoIds) == 0 || index > 0 && bytes.Compare(value.Owners[index-1].Hotkey[:], owner.Hotkey[:]) >= 0 {
			return ProviderAttemptRegistry{}, nil, errors.Join(ErrProviderAttemptsIntegrity, errors.New("provider registry owners are not a unique ordered census"))
		}
		for position, noId := range owner.NoIds {
			if noId == 0 || position > 0 && owner.NoIds[position-1] >= noId {
				return ProviderAttemptRegistry{}, nil, errors.Join(ErrProviderAttemptsIntegrity, errors.New("provider registry operator lanes are not unique and ordered"))
			}
		}
		owned.Owners[index] = ProviderAttemptOwner{Hotkey: owner.Hotkey, NoIds: slices.Clone(owner.NoIds)}
	}
	encoded, err := json.Marshal(owned)
	if err != nil {
		return ProviderAttemptRegistry{}, nil, err
	}
	if uint64(len(encoded))+128 > expected.MaxBytes {
		return ProviderAttemptRegistry{}, nil, ErrProviderAttemptsCapacity
	}
	return owned, encoded, ctx.Err()
}

// The offline deployment producer signs the exact canonical roster once.
// Retries retain the returned bytes; no private key is accepted by the reader.
func SealProviderAttemptRegistry(ctx context.Context, value ProviderAttemptRegistry, expected ProviderAttemptRegistryExpectation, privateKey ed25519.PrivateKey) (*ProviderAttemptRegistry, error) {
	owned, encoded, err := ownProviderAttemptRegistry(ctx, value, expected)
	if err != nil {
		return nil, err
	}
	if len(privateKey) != ed25519.PrivateKeySize || !bytes.Equal(privateKey, ed25519.NewKeyFromSeed(privateKey[:ed25519.SeedSize])) || !bytes.Equal(privateKey[ed25519.SeedSize:], expected.Signer[:]) {
		return nil, errors.Join(ErrProviderAttemptsIntegrity, errors.New("provider registry original signing key differs"))
	}
	message := append([]byte(ProviderAttemptRegistrySchema+"\x00"), encoded...)
	owned.Signature = ed25519.Sign(privateKey, message)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &owned, nil
}

// The content hash includes the original signature. It is a commitment, not a
// trust decision; VerifyProviderAttemptRegistry requires the independent pin.
func (self ProviderAttemptRegistry) Hash() ([32]byte, error) {
	encoded, err := json.Marshal(self)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// Replay the full retained registry chain, then select the last effective
// revision. Future valid revisions do not change earlier window ownership.
func VerifyProviderAttemptRegistry(ctx context.Context, history []ProviderAttemptRegistry, epoch uint64, expected ProviderAttemptRegistryExpectation) (result *ProviderAttemptRegistry, resultErr error) {
	if ctx == nil {
		return nil, errors.New("provider registry owner context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if expected.RootHash == ([32]byte{}) || expected.RequiredHeadHash == ([32]byte{}) || expected.Signer == ([32]byte{}) || len(history) == 0 {
		return nil, ErrProviderAttemptsUnavailable
	}
	if uint64(len(history)) > expected.MaxRevisions {
		return nil, ErrProviderAttemptsCapacity
	}
	var priorHash [32]byte
	var priorEpoch, total uint64
	for index, value := range history {
		owned, encoded, err := ownProviderAttemptRegistry(ctx, value, expected)
		if err != nil {
			return nil, err
		}
		if uint64(len(encoded))+128 > expected.MaxBytes-total {
			return nil, ErrProviderAttemptsCapacity
		}
		total += uint64(len(encoded)) + 128
		if value.Revision != uint64(index) || value.PreviousHash != priorHash || index > 0 && value.EffectiveEpoch <= priorEpoch || len(value.Signature) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(expected.Signer[:]), append([]byte(ProviderAttemptRegistrySchema+"\x00"), encoded...), value.Signature) {
			return nil, errors.Join(ErrProviderAttemptsIntegrity, errors.New("provider registry signature, predecessor or effective epoch differs"))
		}
		owned.Signature = slices.Clone(value.Signature)
		hash, err := owned.Hash()
		if err != nil {
			return nil, err
		}
		if index == 0 && hash != expected.RootHash {
			return nil, errors.Join(ErrProviderAttemptsIntegrity, errors.New("provider registry differs from original policy commitment"))
		}
		if owned.EffectiveEpoch <= epoch {
			selected := owned
			result = &selected
		}
		priorHash, priorEpoch = hash, value.EffectiveEpoch
	}
	if result == nil {
		return nil, ErrProviderAttemptsUnavailable
	}
	if priorHash != expected.RequiredHeadHash {
		return nil, errors.Join(ErrProviderAttemptsIntegrity, errors.New("provider registry omits or changes the retained admitted head"))
	}
	return result, nil
}
