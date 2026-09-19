package main

import (
	"errors"
	"fmt"
)

const reserveRepairPreSignFailureFormat = "reserve transfer stopped before signing: planned minimum finalized stake %d+(%d-%d) does not retain %d bps of registered alpha %d"

// Only the first complete intent/failure pair can prove no signer was reached.
// A later retry cannot erase an earlier ambiguous signing/crash window. The
// exact diagnostic is emitted before native call construction, signing, raw
// artifact persistence and broadcast, and its arithmetic is checked here.
func reserveRepairPreSignFailure(prior *SetupPlan, action Action, entries []JournalEntry) (*JournalEntry, error) {
	if prior == nil {
		return nil, errors.New("reserve repair source plan is absent")
	}
	var related []JournalEntry
	for _, entry := range entries {
		if entry.ActionID != action.ID && entry.IntentHash != action.IntentHash {
			continue
		}
		if entry.PlanHash != prior.PlanHash || entry.DeploymentID != prior.DeploymentID || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash ||
			entry.TransactionHash != "" || entry.Signer != "" || entry.Nonce != "" || entry.BlockNumber != 0 || entry.BlockHash != "" ||
			entry.RecoveryBlock != 0 || entry.RecoveryBlockHash != "" || entry.PostconditionHash != "" || entry.PostconditionPath != "" {
			return nil, errors.New("reserve repair has transaction, signature or conflicting lineage evidence")
		}
		related = append(related, entry)
	}
	if len(related) == 0 {
		return nil, nil
	}
	if len(related) != 2 || related[0].Stage != StageIntent || related[0].Error != "" || related[1].Stage != StageFailed ||
		related[0].Sequence == 0 || related[1].Sequence != related[0].Sequence+1 || related[1].PreviousHash != related[0].EntryHash ||
		!validCanonicalHashHex(related[0].EntryHash) || !validCanonicalHashHex(related[1].EntryHash) {
		return nil, errors.New("reserve repair does not have one exact first-attempt pre-sign failure")
	}
	for _, entry := range related {
		want := entry.EntryHash
		entry.EntryHash = ""
		hash, err := canonicalHashHex(entry)
		if err != nil || hash != want {
			return nil, errors.New("reserve repair pre-sign journal entry hash differs")
		}
	}
	failure := related[1]
	var reserve, exact, shortfall, target, total uint64
	count, err := fmt.Sscanf(failure.Error, reserveRepairPreSignFailureFormat, &reserve, &exact, &shortfall, &target, &total)
	if err != nil || count != 5 || fmt.Sprintf(reserveRepairPreSignFailureFormat, reserve, exact, shortfall, target, total) != failure.Error {
		return nil, errors.New("reserve repair failure does not prove the exact pre-sign share rejection")
	}
	wantTarget, _, shareRepair, termsErr := reserveShareRepairTerms(action)
	wantShortfall, roundingErr := alphaTransferRoundingShortfall(action)
	if termsErr != nil || roundingErr != nil || !shareRepair || exact != action.Spend.AlphaRao || exact <= shortfall ||
		shortfall != wantShortfall || target != uint64(wantTarget) || total == 0 || reserve > total {
		return nil, errors.New("reserve repair pre-sign failure differs from its approved transfer bounds")
	}
	after, valid := checkedAdd(reserve, exact-shortfall)
	if !valid || after > total || alphaShareMeets(total, after, wantTarget) {
		return nil, errors.New("reserve repair pre-sign failure has no reproducible target shortfall")
	}
	return &failure, nil
}
