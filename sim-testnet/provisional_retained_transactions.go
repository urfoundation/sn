package main

// Starting transaction-capable children must not turn an unknown broadcast
// into a fresh nonce. Local plan-only adoption itself dispatches nothing.
import (
	"errors"
	"fmt"
	"sort"
)

// Call after authenticating retained postconditions. Finalized failures are
// known outcomes; a timeout/failed marker alone never resolves a signed hash.
func validateRetainedProvisionalTransactionOutcomes(plan *SetupPlan, entries []JournalEntry) error {
	if plan == nil {
		return errors.New("retained startup transaction approval is missing")
	}
	type identity struct{ plan, action, intent string }
	type transaction struct {
		owner identity
		hash  string
	}
	broadcasts := map[transaction]bool{}
	finalized := map[transaction]bool{}
	verified := map[identity]bool{}
	allowed := plan.allowedPlanHashes()
	for _, entry := range entries {
		if !allowed[entry.PlanHash] {
			continue
		}
		owner := identity{entry.PlanHash, entry.ActionID, entry.IntentHash}
		tx := transaction{owner, entry.TransactionHash}
		switch entry.Stage {
		case StageBroadcast:
			if !validCanonicalHashHex(entry.TransactionHash) {
				return errors.New("retained broadcast has no exact transaction hash")
			}
			broadcasts[tx] = true
		case StageFinalized:
			if validCanonicalHashHex(entry.TransactionHash) && entry.BlockNumber != 0 && validCanonicalHashHex(entry.BlockHash) {
				finalized[tx] = true
			}
		case StageVerified:
			if validCanonicalHashHex(entry.PostconditionHash) && entry.PostconditionPath != "" {
				verified[owner] = true
			}
		}
	}
	var unresolved []string
	for tx := range broadcasts {
		if !finalized[tx] && !verified[tx.owner] {
			unresolved = append(unresolved, tx.owner.action+":"+tx.hash)
		}
	}
	if len(unresolved) == 0 {
		return nil
	}
	sort.Strings(unresolved)
	return fmt.Errorf("retained startup requires exact outcome reconciliation for unresolved broadcasts: %v", unresolved)
}
