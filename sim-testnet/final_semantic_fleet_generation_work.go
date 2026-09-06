// Per-source candidate positions retain every authenticated journal row. A
// separate per-install bounded cache retains only exact complete signature
// tuples; neither helper supplies plan, receipt or publication authority.
package main

import (
	"crypto/ed25519"

	"github.com/urfoundation/sn/protocol"
)

// Only action and stage choose candidates. Plan, intent, transaction,
// postcondition and caller predicates are still checked on every use.
type finalFleetGenerationJournalKey struct {
	actionID string
	stage    JournalStage
}

// Preserve original positions and duplicates after the full journal decoder
// succeeds. The source and any joined private fork borrow this immutable map.
func indexFinalFleetGenerationJournal(entries []JournalEntry) map[finalFleetGenerationJournalKey][]int {
	indices := make(map[finalFleetGenerationJournalKey][]int)
	for index, entry := range entries {
		key := finalFleetGenerationJournalKey{actionID: entry.ActionID, stage: entry.Stage}
		indices[key] = append(indices[key], index)
	}
	return indices
}

const finalFleetGenerationBindingCheckLimit = finalFleetGenerationBatchSize * finalFleetGenerationMembersPerFleet

// Fixed arrays own the complete message, key and signature tuple before an
// observation callback can run. Algorithms are always Ed25519 and sr25519.
type finalFleetGenerationBindingTuple struct {
	digest          [32]byte
	clientKey       [ed25519.PublicKeySize]byte
	hotkey          [32]byte
	clientSignature [ed25519.SignatureSize]byte
	hotkeySignature [64]byte
}

// One synchronous install owns this cache; it is never stored on the source,
// shared with another call, or used concurrently. FIFO eviction reverifies.
type finalFleetGenerationBindingChecks struct {
	checked map[finalFleetGenerationBindingTuple]struct{}
	order   [finalFleetGenerationBindingCheckLimit]finalFleetGenerationBindingTuple
	next    int
}

// A nil owner preserves standalone full verification. Every call validates
// the current message; only successful exact tuples avoid repeated primitives.
func (self *finalFleetGenerationBindingChecks) verify(binding protocol.FleetBinding, clientSignature, hotkeySignature []byte, observed func()) bool {
	if len(clientSignature) != ed25519.SignatureSize || len(hotkeySignature) != 64 {
		return false
	}
	digest, err := binding.Digest()
	if err != nil {
		return false
	}
	tuple := finalFleetGenerationBindingTuple{digest: digest, clientKey: binding.ClientKey, hotkey: binding.Hotkey}
	copy(tuple.clientSignature[:], clientSignature)
	copy(tuple.hotkeySignature[:], hotkeySignature)
	if self != nil {
		if _, found := self.checked[tuple]; found {
			return true
		}
	}
	if observed != nil {
		observed()
	}
	if !binding.VerifyClient(tuple.clientSignature[:]) || !binding.VerifyHotkey(tuple.hotkeySignature[:]) {
		return false
	}
	if self == nil {
		return true
	}
	if self.checked == nil {
		self.checked = make(map[finalFleetGenerationBindingTuple]struct{}, finalFleetGenerationBindingCheckLimit)
	}
	if len(self.checked) < int(finalFleetGenerationBindingCheckLimit) {
		self.order[len(self.checked)] = tuple
	} else {
		delete(self.checked, self.order[self.next])
		self.order[self.next] = tuple
		self.next = (self.next + 1) % len(self.order)
	}
	self.checked[tuple] = struct{}{}
	return true
}
