// Offline recycle handoff carries public bytes only. The signing computer pins
// request, owner, genesis and approval key independently; embedded paths are inert.
package main

import (
	"encoding/hex"
	"errors"
)

const ownerRecycleRequestSchema = "urnetwork-mainnet-owner-recycle-signing-request-v1"

// Planning retains the exact observed birth facts and metadata artifact.
type ownerRecyclePlanInput struct {
	Action         ownerRecycleAction      `json:"action"`
	Observation    ownerRecycleObservation `json:"observation"`
	Metadata       string                  `json:"runtime_metadata_scale"`
	LedgerMetadata string                  `json:"ledger_metadata_scale,omitempty"`
	Route          ownedSubmissionRoute    `json:"owned_route"`
}

// A full-period window check is deliberately conservative. It cannot guarantee
// future governance/owner changes, global nonce custody or native fee exposure.
func prepareOwnerRecyclePlan(input ownerRecyclePlanInput) (ownerRecycleConfig, error) {
	action, err := prepareOwnerRecycleAction(input.Action, input.Metadata)
	if err != nil {
		return ownerRecycleConfig{}, err
	}
	metadata, _, err := nativePinnedMetadata(input.Metadata, action.Policy.RuntimeMetadataHash)
	if err != nil {
		return ownerRecycleConfig{}, err
	}
	window, err := input.Observation.window(action.Policy, metadata)
	if err != nil {
		return ownerRecycleConfig{}, err
	}
	if action.ObservationHash != input.Observation.ContentHash || action.Owner != window.Owner || action.Nonce != window.Nonce || action.BirthBlock != input.Observation.FinalizedNumber || action.BirthHash != input.Observation.FinalizedHash || action.SubnetRegistrationBlock != window.RegistrationBlock || window.Mode != 0 || window.FreeRao < action.FeeReserveRao {
		return ownerRecycleConfig{}, errors.New("recycle plan differs from original Burn observation, owner, nonce, generation or fee reserve")
	}
	for block := action.BirthBlock + 1; block < action.BirthBlock+action.Period; block++ {
		if !window.permits(block) {
			return ownerRecycleConfig{}, errors.New("recycle rate limit/admin window does not admit every possible mortal inclusion block")
		}
	}
	if err := ownerRecycleMetadataMode(action, input.LedgerMetadata); err != nil {
		return ownerRecycleConfig{}, err
	}
	if err := input.Route.validate(); err != nil {
		return ownerRecycleConfig{}, err
	}
	if input.Route.ReadRetrySeconds < 60 || input.Route.ReadRetrySeconds > 900 || input.Route.SendTimeoutSeconds == 0 || input.Route.SendTimeoutSeconds > 60 {
		return ownerRecycleConfig{}, errors.New("recycle route bounds invalid")
	}
	return ownerRecycleConfig{Schema: ownerRecycleConfigSchema, Action: action, Route: input.Route}, nil
}

// Raw metadata15 is a separate pin from the RFC78 digest and metadata14.
func ownerRecycleMetadataMode(action ownerRecycleAction, ledgerMetadata string) error {
	if action.SignatureScheme == "ed25519" {
		hash, err := ownerLedgerMetadataHash(ledgerMetadata)
		if err != nil || hash != action.LedgerMetadataHash {
			return errors.Join(errors.New("recycle Ledger metadata15 differs from approval"), err)
		}
	} else if ledgerMetadata != "" {
		return errors.New("recycle sr25519 action cannot acquire Ledger metadata")
	}
	return nil
}

// One portable request includes the signed configuration and public artifacts.
// It cannot select an account, route or local file outside the independent pins.
type ownerRecycleSigningRequest struct {
	Schema         string             `json:"schema"`
	Config         ownerRecycleConfig `json:"approved_config"`
	Metadata       string             `json:"runtime_metadata_scale"`
	LedgerMetadata string             `json:"ledger_metadata_scale,omitempty"`
	SigningBytes   string             `json:"signing_bytes"`
	ContentHash    string             `json:"content_hash"`
}

// Native payload is below256 bytes; validation still binds its exact encoding.
func newOwnerRecycleSigningRequest(config ownerRecycleConfig, key, metadata, ledgerMetadata string) (ownerRecycleSigningRequest, error) {
	request := ownerRecycleSigningRequest{Schema: ownerRecycleRequestSchema, Config: config, Metadata: metadata, LedgerMetadata: ledgerMetadata, SigningBytes: config.Action.Payload}
	request.ContentHash = rootObjectHash(request)
	return request, request.validate(ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: key, Owner: config.Action.Owner, Genesis: config.Action.Policy.GenesisHash})
}

// Rebuild the action from authenticated metadata before any owner-side use.
func (self ownerRecycleSigningRequest) validate(trust ownerSigningTrust) error {
	if self.Schema != ownerRecycleRequestSchema || !planSha256(trust.RequestHash) || !rootCanonicalHash(trust.Owner) || !rootCanonicalHash(trust.Genesis) || self.Config.Action.Owner != trust.Owner || self.Config.Action.Policy.GenesisHash != trust.Genesis {
		return errors.New("recycle request differs from independently pinned owner, chain or domain")
	}
	if err := self.Config.validate(trust.ApprovalKey); err != nil {
		return err
	}
	if _, err := rootReceiptHex(self.Metadata, maxMetadataRpcReplyBytes); err != nil {
		return err
	}
	if err := ownerRecycleMetadataMode(self.Config.Action, self.LedgerMetadata); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if claimed != trust.RequestHash || claimed != rootObjectHash(self) {
		return errors.New("recycle signing request checksum changed")
	}
	action, err := prepareOwnerRecycleAction(self.Config.Action, self.Metadata)
	if err != nil || rootObjectHash(action) != rootObjectHash(self.Config.Action) || self.SigningBytes != action.Payload {
		return errors.Join(errors.New("recycle request differs from exact native payload/metadata"), err)
	}
	return nil
}

// APDU planning is offline and makes no physical-device or proof-verification
// claim. The actual approved app must authenticate its shortened RFC78 proof.
func (self ownerRecycleSigningRequest) ledgerTranscript(trust ownerSigningTrust, proof []byte) (ownerLedgerTranscript, error) {
	if err := self.validate(trust); err != nil {
		return ownerLedgerTranscript{}, err
	}
	action := self.Config.Action
	if action.SignatureScheme != "ed25519" {
		return ownerLedgerTranscript{}, errors.New("recycle Ledger transcript requires the Ed25519/RFC78 profile")
	}
	payload, _ := hex.DecodeString(action.Payload[2:])
	return ownerLedgerTranscriptForPayload(self.ContentHash, action.Owner, action.DerivationPath, action.MetadataDigest, payload, proof)
}
