package main

// Starting transaction-capable children must not turn an unknown broadcast
// into a fresh nonce. Local plan-only adoption itself dispatches nothing.
import (
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Call after authenticating retained postconditions. Finalized failures are
// known outcomes; a timeout/failed marker alone never resolves a signed hash.
func unresolvedRetainedProvisionalTransactions(plan *SetupPlan, entries []JournalEntry) ([]JournalEntry, error) {
	if plan == nil {
		return nil, errors.New("retained startup transaction approval is missing")
	}
	type identity struct{ plan, action, intent string }
	type transaction struct {
		owner identity
		hash  string
	}
	broadcastKVs := map[transaction]JournalEntry{}
	ownerTransactionKVs := map[identity]map[transaction]bool{}
	pendingKVs := map[transaction]JournalEntry{}
	resolvedKVs := map[transaction]bool{}
	finalizedNonceKVs := map[string]uint64{}
	allowed := plan.allowedPlanHashes()
	for _, entry := range entries {
		if !allowed[entry.PlanHash] {
			continue
		}
		owner := identity{plan: entry.PlanHash, action: entry.ActionID, intent: entry.IntentHash}
		tx := transaction{owner: owner, hash: entry.TransactionHash}
		switch entry.Stage {
		case StageBroadcast:
			if !validCanonicalHashHex(entry.TransactionHash) {
				return nil, errors.New("retained broadcast has no exact transaction hash")
			}
			if prior, exists := broadcastKVs[tx]; exists && (retainedProvisionalSignerKey(prior.Signer) != retainedProvisionalSignerKey(entry.Signer) || prior.Nonce != entry.Nonce) {
				return nil, errors.New("retained broadcast changed its exact signer or nonce")
			}
			broadcastKVs[tx] = entry
			if ownerTransactionKVs[owner] == nil {
				ownerTransactionKVs[owner] = map[transaction]bool{}
			}
			ownerTransactionKVs[owner][tx] = true
			if !resolvedKVs[tx] {
				pendingKVs[tx] = entry
			}
		case StageFinalized:
			broadcast, exists := broadcastKVs[tx]
			if !exists || !validCanonicalHashHex(entry.TransactionHash) || entry.BlockNumber == 0 || !validCanonicalHashHex(entry.BlockHash) {
				continue
			}
			if (entry.Signer != "" && retainedProvisionalSignerKey(entry.Signer) != retainedProvisionalSignerKey(broadcast.Signer)) || (entry.Nonce != "" && entry.Nonce != broadcast.Nonce) {
				return nil, errors.New("retained finalized outcome changed its signed slot")
			}
			resolvedKVs[tx] = true
			delete(pendingKVs, tx)
			// Finalization rows may omit the slot; its exact broadcast supplies it.
			// A consumed slot says nothing about the old action's effects or fees.
			if signer, nonce, valid := retainedProvisionalTransactionSlot(broadcast); valid {
				prior, seen := finalizedNonceKVs[signer]
				if !seen || nonce > prior {
					finalizedNonceKVs[signer] = nonce
				}
			}
		case StageVerified:
			if !validCanonicalHashHex(entry.PostconditionHash) || entry.PostconditionPath == "" {
				continue
			}
			// A receipt resolves only a preceding exact transaction, never a later
			// broadcast under the same action. Legacy hashless receipts must be unique.
			for prior := range ownerTransactionKVs[owner] {
				if entry.TransactionHash == prior.hash || (entry.TransactionHash == "" && len(ownerTransactionKVs[owner]) == 1) {
					resolvedKVs[prior] = true
					delete(pendingKVs, prior)
				}
			}
		}
	}
	var unresolved []JournalEntry
	for _, broadcast := range pendingKVs {
		if signer, nonce, valid := retainedProvisionalTransactionSlot(broadcast); valid {
			if finalizedNonce, exists := finalizedNonceKVs[signer]; exists && nonce <= finalizedNonce {
				continue
			}
		}
		unresolved = append(unresolved, broadcast)
	}
	sort.Slice(unresolved, func(i, j int) bool { return unresolved[i].Sequence < unresolved[j].Sequence })
	return unresolved, nil
}

// Validation remains strict; a separate outcome-only reconciler may first
// persist exact already-finalized EVM outcomes without executing any action.
func validateRetainedProvisionalTransactionOutcomes(plan *SetupPlan, entries []JournalEntry) error {
	pending, err := unresolvedRetainedProvisionalTransactions(plan, entries)
	if err != nil {
		return err
	}
	var unresolved []string
	for _, broadcast := range pending {
		unresolved = append(unresolved, broadcast.ActionID+":"+broadcast.TransactionHash)
	}
	if len(unresolved) == 0 {
		return nil
	}
	sort.Strings(unresolved)
	return fmt.Errorf("retained startup requires exact outcome reconciliation for unresolved broadcasts: %v", unresolved)
}

// EVM checksum spelling does not create another account; native SS58 case does.
// The prefix keeps those nonce domains distinct within the authenticated chain.
func retainedProvisionalSignerKey(signer string) string {
	if len(signer) == 42 && strings.HasPrefix(signer, "0x") {
		if _, err := hex.DecodeString(signer[2:]); err == nil {
			return "evm:" + strings.ToLower(signer)
		}
	}
	return "native:" + signer
}

// Missing or noncanonical legacy slots cannot supply consumption evidence.
func retainedProvisionalTransactionSlot(entry JournalEntry) (string, uint64, bool) {
	nonce, err := strconv.ParseUint(entry.Nonce, 10, 64)
	if entry.Signer == "" || err != nil || strconv.FormatUint(nonce, 10) != entry.Nonce {
		return "", 0, false
	}
	return retainedProvisionalSignerKey(entry.Signer), nonce, true
}
