// Portable treasury signing uses one exact approved action and public metadata.
// Device custody shares the proven one-issuance owner-local Ledger primitive.
package main

import (
	"context"
	"encoding/hex"
	"errors"
)

const treasuryRequestSchema = "urnetwork-native-treasury-signing-request-v1"

// Plans are unsigned and require independently checked original birth state.
type treasuryPlanInput struct {
	Action         treasuryAction       `json:"action"`
	Observation    treasuryObservation  `json:"observation"`
	Metadata       string               `json:"runtime_metadata_scale"`
	LedgerMetadata string               `json:"ledger_metadata_scale"`
	Route          ownedSubmissionRoute `json:"owned_route"`
	MaximumPosts   uint8                `json:"maximum_posts"`
	AuthorityHash  string               `json:"independent_production_authority_hash"`
}

// Reconstruct the only supported codecs before accepting a planning checksum.
func prepareTreasuryAction(action treasuryAction, metadataHex string) (treasuryAction, error) {
	metadata, _, err := nativePinnedMetadata(metadataHex, action.Policy.RuntimeMetadataHash)
	if err != nil {
		return treasuryAction{}, err
	}
	if _, err := treasuryMetadataProfile(metadata, action); err != nil {
		return treasuryAction{}, err
	}
	call, payload, err := action.encoding()
	if err != nil {
		return treasuryAction{}, err
	}
	action.Call, action.Payload = "0x"+hex.EncodeToString(call), "0x"+hex.EncodeToString(payload)
	action.RequestHash = ""
	action.RequestHash = rootObjectHash(action)
	return action, nil
}

// The birth nonce, capacity, effective coldkey and pending operation are original
// native observations. No generated plan supplies its own economic approval.
func prepareTreasuryPlan(ctx context.Context, input treasuryPlanInput) (treasuryConfig, error) {
	action, err := prepareTreasuryAction(input.Action, input.Metadata)
	if err != nil {
		return treasuryConfig{}, err
	}
	metadata, _, err := nativePinnedMetadata(input.Metadata, action.Policy.RuntimeMetadataHash)
	if err != nil {
		return treasuryConfig{}, err
	}
	facts, err := input.Observation.facts(ctx, action, metadata)
	if err != nil {
		return treasuryConfig{}, err
	}
	if input.Observation.ContentHash != action.ObservationHash || input.Observation.FinalizedNumber != action.BirthBlock || input.Observation.FinalizedHash != action.BirthHash {
		return treasuryConfig{}, errors.New("treasury plan changes the independently reviewed birth observation")
	}
	if err := facts.admits(action, true); err != nil {
		return treasuryConfig{}, err
	}
	if err := treasuryMetadataMode(action, input.LedgerMetadata); err != nil {
		return treasuryConfig{}, err
	}
	if err := input.Route.validate(); err != nil {
		return treasuryConfig{}, err
	}
	if input.Route.ReadRetrySeconds < 60 || input.Route.ReadRetrySeconds > 900 || input.Route.SendTimeoutSeconds == 0 || input.Route.SendTimeoutSeconds > 60 || input.MaximumPosts == 0 || input.MaximumPosts > 8 || !planSha256(input.AuthorityHash) {
		return treasuryConfig{}, errors.New("treasury plan lacks bounded route/posts and independent production authority")
	}
	return treasuryConfig{Schema: treasuryConfigSchema, Action: action, Route: input.Route, MaximumPosts: input.MaximumPosts, AuthorityHash: input.AuthorityHash}, nil
}

// RFC78 digest, metadata14 pin and public metadata15 artifact have distinct jobs.
func treasuryMetadataMode(action treasuryAction, encoded string) error {
	hash, err := ownerLedgerMetadataHash(encoded)
	if err != nil || hash != action.LedgerMetadataHash {
		return errors.Join(errors.New("treasury Ledger metadata15 differs from approved original"), err)
	}
	return nil
}

// Export includes no key material. Embedded host paths are inert on the owner.
type treasurySigningRequest struct {
	Schema         string         `json:"schema"`
	Config         treasuryConfig `json:"approved_config"`
	Metadata       string         `json:"runtime_metadata_scale"`
	LedgerMetadata string         `json:"ledger_metadata_scale"`
	SigningBytes   string         `json:"signing_bytes"`
	ContentHash    string         `json:"content_hash"`
}

// Large multisig payloads are hashed by the same rule as native signature verify.
func newTreasurySigningRequest(config treasuryConfig, key, metadata, ledger string) (treasurySigningRequest, error) {
	if err := config.validate(key); err != nil {
		return treasurySigningRequest{}, err
	}
	r := treasurySigningRequest{Schema: treasuryRequestSchema, Config: config, Metadata: metadata, LedgerMetadata: ledger, SigningBytes: "0x" + hex.EncodeToString(config.Action.signingBytes())}
	r.ContentHash = rootObjectHash(r)
	return r, r.validate(ownerSigningTrust{RequestHash: r.ContentHash, ApprovalKey: key, Owner: config.Action.Owner, Genesis: config.Action.Policy.GenesisHash})
}

// The owner independently pins account, genesis, approval and complete request.
func (self treasurySigningRequest) validate(trust ownerSigningTrust) error {
	if self.Schema != treasuryRequestSchema || !planSha256(trust.RequestHash) || self.Config.Action.Owner != trust.Owner || self.Config.Action.Policy.GenesisHash != trust.Genesis {
		return errors.New("treasury request differs from independent owner/genesis/domain")
	}
	if err := self.Config.validate(trust.ApprovalKey); err != nil {
		return err
	}
	if err := treasuryMetadataMode(self.Config.Action, self.LedgerMetadata); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if claimed != trust.RequestHash || claimed != rootObjectHash(self) {
		return errors.New("treasury request checksum changed")
	}
	a, err := prepareTreasuryAction(self.Config.Action, self.Metadata)
	if err != nil || rootObjectHash(a) != rootObjectHash(self.Config.Action) || self.SigningBytes != "0x"+hex.EncodeToString(a.signingBytes()) {
		return errors.Join(errors.New("treasury request metadata or signed payload changed"), err)
	}
	return nil
}

// The device receives full payload and proof even when native signing hashes it.
func (self treasurySigningRequest) ledgerTranscript(trust ownerSigningTrust, proof []byte) (ownerLedgerTranscript, error) {
	if err := self.validate(trust); err != nil {
		return ownerLedgerTranscript{}, err
	}
	a := self.Config.Action
	payload, _ := hex.DecodeString(a.Payload[2:])
	return ownerLedgerTranscriptForPayload(self.ContentHash, a.Owner, a.DerivationPath, a.MetadataDigest, payload, proof)
}
