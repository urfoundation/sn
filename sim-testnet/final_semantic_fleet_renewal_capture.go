package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

// Retain only the finalized envelopes owned by approved renewal actions. The
// ordinary public and receipt bundles do not contain these signed bytes.
func captureFinalFleetRenewalTransactions(ctx context.Context, stateRoot string, foundation []FinalCollectedFileBundleEntry) ([]FinalCollectedFileBundleEntry, error) {
	if ctx == nil || stateRoot == "" {
		return nil, errors.New("fleet renewal capture owner is absent")
	}
	var planBytes, journalBytes []byte
	for _, file := range foundation {
		switch file.Path {
		case "plan.json":
			if planBytes != nil {
				return nil, errors.New("fleet renewal capture has duplicate current plans")
			}
			planBytes = file.Data
		case "journal.jsonl":
			if journalBytes != nil {
				return nil, errors.New("fleet renewal capture has duplicate journals")
			}
			journalBytes = file.Data
		}
	}
	plan, err := decodePersistedPlanBytes(planBytes)
	if err != nil {
		return nil, err
	}
	if len(plan.FleetRenewals) == 0 {
		return nil, ctx.Err()
	}
	entries, err := decodeFinalSemanticJournalBytes(journalBytes)
	if err != nil {
		return nil, err
	}
	return captureFinalFleetRenewalTransactionEntries(ctx, stateRoot, plan, entries)
}

func captureFinalFleetRenewalTransactionEntries(ctx context.Context, stateRoot string, plan *SetupPlan, entries []JournalEntry) ([]FinalCollectedFileBundleEntry, error) {
	wanted := map[string]Action{}
	for _, action := range plan.Actions {
		if isFleetRenewalAction(action) && action.Kind == "evm-transaction" {
			wanted[action.ID] = action
		}
	}
	allowed := plan.allowedPlanHashes()
	result := []FinalCollectedFileBundleEntry{}
	seen := map[string]bool{}
	completed := map[string]bool{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Stage == StageFinalized && allowed[entry.PlanHash] && completed[entry.ActionID] {
			return nil, fmt.Errorf("fleet renewal action %s has duplicate finalized ownership", entry.ActionID)
		}
		action, found := wanted[entry.ActionID]
		if !found || entry.Stage != StageFinalized || !allowed[entry.PlanHash] {
			continue
		}
		if !actionAcceptsIntent(action, entry.IntentHash) || !validCanonicalHashHex(entry.TransactionHash) || seen[entry.TransactionHash] {
			return nil, fmt.Errorf("fleet renewal action %s has conflicting finalized envelope ownership", entry.ActionID)
		}
		name := filepath.ToSlash(filepath.Join("transactions", stringsTrim0x(entry.TransactionHash)+".rlp"))
		files, err := finalCollectedNamedEntries(stateRoot, []string{name})
		if err != nil || len(files) != 1 {
			return nil, stateMismatchError(err, "fleet renewal signed envelope %s is absent", entry.ActionID)
		}
		if err := verifyFinalFleetRenewalEnvelope(plan, action, entry.TransactionHash, files[0].Data, nil); err != nil {
			return nil, err
		}
		seen[entry.TransactionHash] = true
		completed[entry.ActionID] = true
		result = append(result, files[0])
		delete(wanted, entry.ActionID)
	}
	if len(wanted) != 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("fleet renewal capture is missing finalized action %s (%d total)", missing[0], len(missing))
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Path < result[right].Path })
	return result, ctx.Err()
}

func verifyFinalFleetRenewalEnvelope(plan *SetupPlan, action Action, hash string, raw, calldata []byte) error {
	var tx ethTypes.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return err
	}
	if tx.Hash().Hex() != hash || !tx.Protected() || !tx.ChainId().IsUint64() || tx.ChainId().Uint64() != plan.ChainID {
		return errors.New("fleet renewal source transaction differs from the exact approved chain and hash")
	}
	if len(calldata) != 0 && !bytes.Equal(tx.Data(), calldata) {
		return errors.New("fleet renewal source transaction changed exact calldata")
	}
	signer, err := ethTypes.Sender(ethTypes.LatestSignerForChainID(tx.ChainId()), &tx)
	if err != nil {
		return err
	}
	if err := validateFleetRenewalEVMFields(action, signer, tx.Nonce(), tx.To(), tx.Value(), tx.Data()); err != nil {
		return err
	}
	gas, fee, err := evmActionFeeEnvelope(action)
	if err != nil || tx.Gas() == 0 || tx.Gas() > gas || !tx.GasFeeCap().IsUint64() || tx.GasFeeCap().Uint64() > fee {
		return stateMismatchError(err, "fleet renewal source envelope exceeded its approved fee ceiling")
	}
	return nil
}
