//go:build linux || darwin

package main

// Refresh is a new exact approval over cumulative custody, never an implicit
// restart extension or a replacement of the original monetary allowance.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// Initial fee repartition owns only original debits. A refresh can also retain
// the lower-fee slots already admitted by its authenticated predecessors.
func (self *EvidenceRelayContinuation) debitLimit() uint64 {
	if self != nil && self.Schema == evidenceRelayContinuationExpansionSchema {
		return evidenceRelayContinuationExpandedSlots
	}
	if self != nil && self.Schema == evidenceRelayContinuationRefreshSchema {
		return evidenceRelayContinuationSlots
	}
	return evidenceRelayOriginalSlots
}

// Pure append admission preserves the predecessor's economic and historical
// obligations. Actual prefix bytes and signed records are rechecked by capture.
func validateEvidenceRelayContinuationRefresh(base *SetupPlan, current *EvidenceRelayContinuation) error {
	if base == nil || base.EvidenceRelayContinuation == nil || current == nil || (current.Schema != evidenceRelayContinuationRefreshSchema && current.Schema != evidenceRelayContinuationExpansionSchema) || current.SourcePlanHash != base.PlanHash {
		return errors.New("relay refresh requires its exact adopted continuation predecessor")
	}
	prior := base.EvidenceRelayContinuation
	if prior.Schema != evidenceRelayContinuationSchema && prior.Schema != evidenceRelayContinuationRefreshSchema && prior.Schema != evidenceRelayContinuationExpansionSchema {
		return errors.New("relay refresh cannot change an older approved fee version")
	}
	if err := validateEvidenceRelayContinuationPlan(base); err != nil {
		return err
	}
	if current.ConfigHash != prior.ConfigHash || current.ActivationPlanHash != prior.ActivationPlanHash || current.PreparedSHA256 != prior.PreparedSHA256 || current.CompletedSHA256 != prior.CompletedSHA256 || !reflect.DeepEqual(current.OriginalReserve, prior.OriginalReserve) {
		return errors.New("relay refresh changed original activation or monetary authority")
	}
	if current.EVMHead.Number <= prior.EVMHead.Number || current.NativeHead.Number <= prior.NativeHead.Number || current.SettlementEpoch < prior.SettlementEpoch || current.NativeEpoch < prior.NativeEpoch || current.EndBlock <= prior.EndBlock || current.RequiredWorkBlocks != prior.RequiredWorkBlocks {
		return errors.New("relay refresh requires fresh forward snapshots and unchanged complete work")
	}
	if len(current.Sources) != len(prior.Sources) {
		return errors.New("relay refresh changed the original source census")
	}
	for index, previous := range prior.Sources {
		next := current.Sources[index]
		if next.ValidatorID != previous.ValidatorID || next.NoID != previous.NoID || next.CoordinatorStateDir != previous.CoordinatorStateDir || next.Activation != previous.Activation || next.Capacity.Identity != previous.Capacity.Identity || next.Capacity.Coordinator != previous.Capacity.Coordinator || next.IntentPrefixCount < previous.IntentPrefixCount || next.LastNativeEpoch < previous.LastNativeEpoch || next.Capacity.Head.LastSequence < previous.Capacity.Head.LastSequence || next.Capacity.Head.TrailCount < previous.Capacity.Head.TrailCount || next.Capacity.Head.RecordBytes < previous.Capacity.Head.RecordBytes {
			return errors.New("relay refresh reset or replaced an authenticated source prefix")
		}
		if next.IntentPrefixCount == previous.IntentPrefixCount && (next.IntentPrefixSHA256 != previous.IntentPrefixSHA256 || next.LastNativeEpoch != previous.LastNativeEpoch || next.LastArtifactHash != previous.LastArtifactHash) {
			return errors.New("relay refresh changed unchanged native intent history")
		}
		if next.Capacity.Head.LastSequence == previous.Capacity.Head.LastSequence && next.Capacity.Head != previous.Capacity.Head {
			return errors.New("relay refresh changed unchanged signed ledger history")
		}
	}
	if err := validateEvidenceRelayContinuationRetained(prior, current.Retained); err != nil {
		return err
	}
	debitKVs := map[string]EvidenceRelayContinuationDebit{}
	for _, debit := range current.Debits {
		if _, exists := debitKVs[debit.ActionID]; exists {
			return errors.New("relay refresh duplicated a retained debit")
		}
		debitKVs[debit.ActionID] = debit
	}
	for _, debit := range prior.Debits {
		if retained, exists := debitKVs[debit.ActionID]; !exists || retained != debit {
			return errors.New("relay refresh omitted or changed an original admitted liability")
		}
	}
	remaining, liability, err := current.remainingSlots()
	_, priorSlots, priorErr := prior.feeTerms()
	_, currentSlots, currentErr := current.feeTerms()
	if priorErr != nil || currentErr != nil || currentSlots < priorSlots {
		return errors.New("relay refresh cannot reduce its adopted aggregate capacity")
	}
	maximumRemaining, ok := checkedAdd(prior.NewSlots, currentSlots-priorSlots)
	if err != nil || !ok || current.NewSlots != remaining || current.HistoricalLiabilityWei != liability || current.NewSlots > maximumRemaining {
		return errors.Join(errors.New("relay refresh restored spent slot allowance"), err)
	}
	return nil
}

// The request's current full prefix was captured by the ordinary reader. Its
// nonempty ancestor must also remain byte-identical inside that same history.
func checkEvidenceRelayContinuationHistoryPrefix(ctx context.Context, configPath string, configBytes []byte, request validatorcomponent.ReleaseHistoryAdoptionV2, previous EvidenceRelayContinuationSource) error {
	if request.IntentPrefixCount < previous.IntentPrefixCount || request.CoordinatorStateDir != previous.CoordinatorStateDir {
		return errors.New("relay refresh lost its adopted native history prefix")
	}
	if previous.IntentPrefixCount == 0 {
		return nil
	}
	request.IntentPrefixCount = previous.IntentPrefixCount
	request.IntentPrefixSHA256 = previous.IntentPrefixSHA256
	request.LastNativeEpoch = previous.LastNativeEpoch
	request.LastArtifactHash = previous.LastArtifactHash
	raw, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return validatorcomponent.CheckReleaseHistoryAdoptionV2Source(ctx, configPath, configBytes, raw, bytesSHA256(raw))
}

// Startup must leave the complete approved campaign inside the fixed end,
// including after a long local verification pass consumes preparation time.
func validateEvidenceRelayContinuationRunway(current *EvidenceRelayContinuation, block uint64) error {
	if current == nil || current.RequiredWorkBlocks == 0 || block < current.EVMHead.Number || block >= current.EndBlock || current.RequiredWorkBlocks > current.EndBlock-block {
		if current == nil {
			return errors.New("relay continuation runway owner is absent")
		}
		return fmt.Errorf("relay continuation insufficient full-work runway before startup: observed_block=%d required_work=%d fixed_end=%d", block, current.RequiredWorkBlocks, current.EndBlock)
	}
	return nil
}
