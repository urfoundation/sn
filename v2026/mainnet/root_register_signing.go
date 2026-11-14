// Offline root registration handoff carries public bytes only. The signing computer pins
// request, owner, genesis and approval key independently; embedded paths are inert.
package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
)

const rootRegisterRequestSchema = "urnetwork-mainnet-root-register-signing-request-v1"
const rootRegisterSigningRequestLimit = 24 * 1024 * 1024

// Planning retains the exact observed birth facts and metadata artifact.
type rootRegisterPlanInput struct {
	Action         rootRegisterAction      `json:"action"`
	Observation    rootRegisterObservation `json:"observation"`
	Metadata       string                  `json:"runtime_metadata_scale"`
	LedgerMetadata string                  `json:"ledger_metadata_scale,omitempty"`
	Route          ownedSubmissionRoute    `json:"owned_route"`
}

// Snapshot eligibility and quote limits are preflight only. The separately
// signed action discloses the uncapped execution-time balance and fee exposure.
func prepareRootRegisterPlan(input rootRegisterPlanInput) (rootRegisterConfig, error) {
	action := input.Action
	if err := action.Policy.validate(); err != nil {
		return rootRegisterConfig{}, err
	}
	metadata, _, err := nativePinnedMetadata(input.Metadata, action.Policy.RuntimeMetadataHash)
	if err != nil {
		return rootRegisterConfig{}, err
	}
	window, err := input.Observation.eligibility(action.Policy, metadata)
	if err != nil {
		return rootRegisterConfig{}, err
	}
	action.QuotedBurnRao, action.ObservedFreeRao, action.ObservedReducibleRao = window.BurnRao, window.FreeRao, window.ConservativeReducibleRao
	action, err = prepareRootRegisterAction(action, input.Metadata)
	if err != nil {
		return rootRegisterConfig{}, err
	}
	if action.ObservationHash != input.Observation.ContentHash || action.Nonce != window.Nonce || action.BirthBlock != input.Observation.FinalizedNumber || action.BirthHash != input.Observation.FinalizedHash || window.ExistingSeat != nil || !window.Eligible {
		return rootRegisterConfig{}, errors.New("root registration plan differs from its original absent-seat eligibility, nonce or finalized birth")
	}
	if err := rootRegisterPreflightExposure(action, window); err != nil {
		return rootRegisterConfig{}, err
	}
	if err := rootRegisterMetadataMode(action, input.LedgerMetadata); err != nil {
		return rootRegisterConfig{}, err
	}
	if err := input.Route.validate(); err != nil {
		return rootRegisterConfig{}, err
	}
	if input.Route.ReadRetrySeconds < 60 || input.Route.ReadRetrySeconds > 900 || input.Route.SendTimeoutSeconds == 0 || input.Route.SendTimeoutSeconds > 60 {
		return rootRegisterConfig{}, errors.New("root registration route bounds invalid")
	}
	return rootRegisterConfig{Schema: rootRegisterConfigSchema, Action: action, Route: input.Route}, nil
}

// Raw metadata15 is a separate pin from the RFC78 digest and metadata14.
func rootRegisterMetadataMode(action rootRegisterAction, ledgerMetadata string) error {
	if action.SigningProfile == rootRegisterLedgerHardware {
		hash, err := ownerLedgerMetadataHash(ledgerMetadata)
		if err != nil || hash != action.LedgerMetadataHash {
			return errors.Join(errors.New("root registration Ledger metadata15 differs from approval"), err)
		}
	} else if ledgerMetadata != "" {
		return errors.New("root registration portable hardware cannot acquire Ledger metadata")
	}
	return nil
}

// One portable request includes the signed configuration and public artifacts.
// It cannot select an account, route or local file outside the independent pins.
type rootRegisterSigningRequest struct {
	Schema         string             `json:"schema"`
	Config         rootRegisterConfig `json:"approved_config"`
	Metadata       string             `json:"runtime_metadata_scale"`
	LedgerMetadata string             `json:"ledger_metadata_scale,omitempty"`
	SigningBytes   string             `json:"signing_bytes"`
	ContentHash    string             `json:"content_hash"`
}

// Native payload is below256 bytes; validation still binds its exact encoding.
func newRootRegisterSigningRequest(config rootRegisterConfig, key, metadata, ledgerMetadata string) (rootRegisterSigningRequest, error) {
	request := rootRegisterSigningRequest{Schema: rootRegisterRequestSchema, Config: config, Metadata: metadata, LedgerMetadata: ledgerMetadata, SigningBytes: config.Action.Payload}
	request.ContentHash = rootObjectHash(request)
	return request, request.validate(ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: key, Owner: config.Action.Policy.Operator, Genesis: config.Action.Policy.GenesisHash})
}

// Rebuild the action from authenticated metadata before any owner-side use.
func (self rootRegisterSigningRequest) validate(trust ownerSigningTrust) error {
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > rootRegisterSigningRequestLimit {
		return errors.New("root registration signing request exceeds its physical custody bound")
	}
	if self.Schema != rootRegisterRequestSchema || !planSha256(trust.RequestHash) || !rootCanonicalHash(trust.Owner) || !rootCanonicalHash(trust.Genesis) || self.Config.Action.Policy.Operator != trust.Owner || self.Config.Action.Policy.GenesisHash != trust.Genesis {
		return errors.New("root registration request differs from independently pinned owner, chain or domain")
	}
	if err := self.Config.validate(trust.ApprovalKey); err != nil {
		return err
	}
	if _, err := rootReceiptHex(self.Metadata, maxMetadataRpcReplyBytes); err != nil {
		return err
	}
	if err := rootRegisterMetadataMode(self.Config.Action, self.LedgerMetadata); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if claimed != trust.RequestHash || claimed != rootObjectHash(self) {
		return errors.New("root registration signing request checksum changed")
	}
	action, err := prepareRootRegisterAction(self.Config.Action, self.Metadata)
	if err != nil || rootObjectHash(action) != rootObjectHash(self.Config.Action) || self.SigningBytes != action.Payload {
		return errors.Join(errors.New("root registration request differs from exact native payload/metadata"), err)
	}
	return nil
}

// APDU planning is offline and makes no physical-device or proof-verification
// claim. The actual approved app must authenticate its shortened RFC78 proof.
func (self rootRegisterSigningRequest) ledgerTranscript(trust ownerSigningTrust, proof []byte) (ownerLedgerTranscript, error) {
	if err := self.validate(trust); err != nil {
		return ownerLedgerTranscript{}, err
	}
	action := self.Config.Action
	if action.SignatureScheme != "ed25519" || action.SigningProfile != rootRegisterLedgerHardware {
		return ownerLedgerTranscript{}, errors.New("root registration Ledger transcript requires the Ed25519/RFC78 profile")
	}
	payload, _ := hex.DecodeString(action.Payload[2:])
	return ownerLedgerTranscriptForPayload(self.ContentHash, action.Policy.Operator, action.DerivationPath, action.MetadataDigest, payload, proof)
}

// A current quote can refuse a send but cannot constrain a later native debit.
// Subtraction after comparison avoids overflow when a caller supplies large fees.
func rootRegisterPreflightExposure(action rootRegisterAction, current rootRegisterEligibility) error {
	if current.BurnRao > action.QuotedBurnLimitRao || current.BurnRao > current.ConservativeReducibleRao || action.FeeReserveRao > current.ConservativeReducibleRao-current.BurnRao {
		return errors.New("root registration current quote or conservative spendable balance exceeds preflight allowance; no burn cap is encoded")
	}
	return nil
}
