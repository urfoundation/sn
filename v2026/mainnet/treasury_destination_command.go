// Public receiving observations carry their own scope and schema; optional
// native multisig execution never supplies their input requirements.
package main

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/validator"
)

const treasuryDestinationInputSchema = "urnetwork-native-treasury-destination-input-v1"
const treasuryDestinationObservationSchema = "urnetwork-native-treasury-destination-observation-v1"

// Current runtime and route are independently approved read inputs. Observation
// and metadata are needed only when replaying a retained policy-plan input.
type treasuryDestinationInput struct {
	Schema      string               `json:"schema"`
	Policy      treasuryChainPolicy  `json:"policy"`
	Destination treasuryDestination  `json:"destination"`
	Observation treasuryObservation  `json:"observation"`
	Metadata    string               `json:"runtime_metadata_scale"`
	Route       ownedSubmissionRoute `json:"owned_route"`
}

// Distinct public scope prevents relabeling an execution observation as routing.
func treasuryDestinationScope(policy treasuryChainPolicy, destination treasuryDestination) string {
	return rootObjectHash(struct {
		Schema      string
		Policy      treasuryChainPolicy
		Destination treasuryDestination
	}{Schema: treasuryDestinationObservationSchema, Policy: policy, Destination: destination})
}

// A complete native roster is required for observation; describing an address
// alone never claims that recipients exist or that routing has been admitted.
func (self treasuryDestinationInput) validate() error {
	if self.Schema != treasuryDestinationInputSchema || self.Policy.GenesisHash != self.Destination.GenesisHash || len(self.Destination.RecipientHotkeys) < 2 {
		return errors.New("treasury receiving observation requires one network and at least two declared recipient hotkeys")
	}
	return errors.Join(self.Policy.validate(), self.Destination.validate())
}

// The original rows are decoded again through the same receiving-only reader.
func (self treasuryDestinationInput) facts(ctx context.Context) (treasuryFacts, error) {
	if err := self.validate(); err != nil {
		return treasuryFacts{}, err
	}
	metadata, _, err := nativePinnedMetadata(self.Metadata, self.Policy.RuntimeMetadataHash)
	if err != nil {
		return treasuryFacts{}, err
	}
	return self.Observation.replay(ctx, treasuryDestinationObservationSchema, treasuryDestinationScope(self.Policy, self.Destination), func(read treasuryStorageReader) (treasuryFacts, error) {
		return treasuryReadRecipientFacts(ctx, self.Destination, metadata, self.Observation.FinalizedNumber, read, false)
	})
}

// Read-only capture excludes native nonce, deposit, fee and multisig state.
func (self treasuryDestinationInput) observe(ctx context.Context) (any, error) {
	return treasuryObserve(ctx, self.Policy, self.Route, func(ctx context.Context, chain *rootCanonicalChain, hash string, number uint64) (treasuryObservation, error) {
		return chain.captureTreasuryObservation(ctx, treasuryDestinationObservationSchema, treasuryDestinationScope(self.Policy, self.Destination), hash, number, chain.authenticatedRuntimeAt, func(metadata *types.Metadata, read treasuryStorageReader) (treasuryFacts, error) {
			return treasuryReadRecipientFacts(ctx, self.Destination, metadata, number, read, false)
		})
	})
}

// An unsigned receiving policy contains no reserve sending authority.
func treasuryDestinationCommand(ctx context.Context, mode string, raw []byte, path, pin string) (any, error) {
	var input treasuryDestinationInput
	if err := decodePlanJson(raw, &input); err != nil {
		return nil, err
	}
	if path != "" || pin != "" {
		destination, err := readTreasuryDestination(ctx, path, pin)
		if err != nil {
			return nil, err
		}
		if input.Destination.Schema != "" && rootObjectHash(input.Destination) != rootObjectHash(destination) {
			return nil, errors.New("treasury input and independently pinned destination disagree")
		}
		input.Destination = destination
	}
	if err := input.validate(); err != nil {
		return nil, err
	}
	if mode == "observe" {
		return input.observe(ctx)
	}
	if mode != "policy-plan" {
		return nil, errors.New("treasury destination supports observation and unsigned policy planning only")
	}
	facts, err := input.facts(ctx)
	if err != nil {
		return nil, err
	}
	policy, err := input.Destination.policy(facts)
	if err != nil {
		return nil, err
	}
	hash, err := policy.Hash()
	if err != nil {
		return nil, err
	}
	return struct {
		Policy      validator.TreasuryPolicy `json:"treasury_policy"`
		Hash        string                   `json:"treasury_policy_hash"`
		Observation string                   `json:"observation_hash"`
		Approved    bool                     `json:"approved"`
	}{Policy: policy, Hash: "0x" + hex.EncodeToString(hash[:]), Observation: input.Observation.ContentHash}, nil
}
