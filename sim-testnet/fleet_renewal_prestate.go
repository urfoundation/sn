// A revoked predecessor keeps its original dual-signed binding. The separate
// client-authorized, finalized revocation explains the shortened chain lease.
package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Carries the exact earlier approved mutation without rewriting signed bytes.
type FleetRenewalPriorRevocation struct {
	PlanHash        string `json:"plan_hash"`
	ActionId        string `json:"action_id"`
	IntentHash      string `json:"intent_hash"`
	EffectiveEpoch  uint64 `json:"effective_epoch"`
	ClientSignature string `json:"client_signature"`
	TransactionHash string `json:"transaction_hash"`
	BlockNumber     uint64 `json:"block_number"`
	BlockHash       string `json:"block_hash"`
}

// A cutoff is optional for old plans and can only shorten their signed lease.
func (self FleetRenewalMember) priorEffectiveValidTo() (uint64, error) {
	if self.PriorRevocation == nil {
		return self.Prior.ValidToEpoch, nil
	}
	if self.PriorRevocation.EffectiveEpoch == 0 || self.PriorRevocation.EffectiveEpoch-1 >= self.Prior.ValidToEpoch {
		return 0, errors.New("renewal predecessor revocation does not shorten its signed lease")
	}
	return self.PriorRevocation.EffectiveEpoch - 1, nil
}

// Validate consent and approved calldata before using a shorter effective end.
// Receipt canonicality is independently rechecked before any successor write.
func fleetRenewalPriorValidTo(plan *SetupPlan, manifest protocol.FleetManifest, client protocol.FleetMember, member FleetRenewalMember) (uint64, error) {
	validTo, err := member.priorEffectiveValidTo()
	if err != nil {
		return 0, err
	}
	proof := member.PriorRevocation
	if proof == nil {
		return validTo, nil
	}
	if plan == nil || proof.EffectiveEpoch == 0 || proof.EffectiveEpoch-1 >= member.Prior.ValidToEpoch ||
		!plan.allowedPlanHashes()[proof.PlanHash] || !validCanonicalHashHex(proof.IntentHash) ||
		!validCanonicalHashHex(proof.TransactionHash) || !validCanonicalHashHex(proof.BlockHash) || proof.BlockNumber == 0 {
		return 0, errors.New("renewal predecessor revocation lacks an approved finalized checkpoint")
	}
	revoke := protocol.FleetRevoke{ChainID: manifest.ChainID, Netuid: manifest.Netuid, Coordinator: manifest.Coordinator, ClientID: client.ClientID, Generation: member.Prior.Generation, EffectiveEpoch: proof.EffectiveEpoch}
	signature, ok := evidenceFixedHex(proof.ClientSignature, 64)
	if !ok || !revoke.VerifyClient(client.ClientKey[:], signature) {
		return 0, errors.New("renewal predecessor revocation lacks exact client consent")
	}
	action, err := exactPlanActionByID(plan, proof.ActionId)
	if err != nil || !isFleetRenewalAction(action) || action.Parameters["operation"] != "revoke" || action.IntentHash != proof.IntentHash ||
		action.Target != common.BytesToAddress(manifest.Coordinator[:]).Hex() {
		return 0, errors.New("renewal predecessor revocation differs from its approved action")
	}
	data, err := stabi.NewSTCoordinator().TryPackRevokeFleetBinding(client.ClientID, member.Prior.Generation, proof.EffectiveEpoch, signature)
	if err != nil || action.Parameters["renewal_calldata"] != "0x"+hex.EncodeToString(data) {
		return 0, errors.New("renewal predecessor revocation changed its approved calldata")
	}
	return validTo, nil
}

// Resolve a changed expiry only through an exact finalized revoke in the
// admitted source lineage. A matching shorter value alone is insufficient.
func fleetRenewalObservedPriorRevocation(base *SetupPlan, entries []JournalEntry, fleet int, client protocol.FleetMember, prior FleetBindingEvidence, observedValidTo, observedBlock uint64) (*FleetRenewalPriorRevocation, error) {
	if observedValidTo == prior.ValidToEpoch {
		return nil, nil
	}
	if observedValidTo > prior.ValidToEpoch || observedValidTo == math.MaxUint64 || base == nil {
		return nil, errors.New("renewal predecessor validity increased without signed consent")
	}
	effective := observedValidTo + 1
	allowedPlanHashKVs := base.allowedPlanHashes()
	for index := len(base.FleetRenewals) - 1; index >= 0; index-- {
		renewal := base.FleetRenewals[index]
		if renewal.ValidFromEpoch != effective || fleet < 1 || fleet > len(renewal.Fleets) {
			continue
		}
		planned := renewal.Fleets[fleet-1]
		manifest, err := protocol.ParseFleetManifest(planned.Manifest)
		if err != nil {
			return nil, err
		}
		for memberIndex, member := range planned.Members {
			if member.Prior.ClientID != prior.ClientID || member.Prior.BindingDigest != prior.BindingDigest || member.Prior.TransactionHash != prior.TransactionHash || member.RevokeSignature == "" {
				continue
			}
			action, err := exactPlanActionByID(base, fleetRenewalActionID(renewal.Round, fleet, "revoke", memberIndex+1))
			if err != nil {
				return nil, err
			}
			var finalized *JournalEntry
			for _, entry := range entries {
				if entry.Stage != StageFinalized || !allowedPlanHashKVs[entry.PlanHash] || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash {
					continue
				}
				if finalized != nil && finalized.TransactionHash != entry.TransactionHash {
					return nil, errors.New("renewal predecessor revocation has conflicting finalized receipts")
				}
				copy := entry
				finalized = &copy
			}
			if finalized == nil || finalized.BlockNumber > observedBlock {
				continue
			}
			proof := &FleetRenewalPriorRevocation{PlanHash: finalized.PlanHash, ActionId: action.ID, IntentHash: action.IntentHash, EffectiveEpoch: effective, ClientSignature: member.RevokeSignature, TransactionHash: finalized.TransactionHash, BlockNumber: finalized.BlockNumber, BlockHash: finalized.BlockHash}
			if _, err := fleetRenewalPriorValidTo(base, *manifest, client, FleetRenewalMember{Prior: prior, PriorRevocation: proof}); err != nil {
				return nil, err
			}
			return proof, nil
		}
	}
	return nil, fmt.Errorf("fleet %d predecessor %s shortened to epoch %d without an exact finalized approved revocation", fleet, prior.ClientID, observedValidTo)
}
