package main

// A reconciliation resolves terminal receipts from one detached append-only
// snapshot. It does not change ordinary execution's fresh journal lookup.

type carriedPreparationIntent struct {
	actionId   string
	intentHash string
}

// Slice order, rather than claimed sequence, preserves the ordinary reverse
// reader's choice across current and explicitly accepted ancestor intents.
type carriedPreparationEntry struct {
	entry    JournalEntry
	position int
}

type carriedPreparationIndex struct {
	verifiedKVs       map[carriedPreparationIntent]carriedPreparationEntry
	activeTopologyKVs map[carriedPreparationIntent]carriedPreparationEntry
}

// Unapproved plans and nonterminal rows cannot replace verified authority.
// Current topology remains distinct from a stopped ancestor's history.
func newCarriedPreparationIndex(plan *SetupPlan, entries []JournalEntry) *carriedPreparationIndex {
	self := &carriedPreparationIndex{
		verifiedKVs:       map[carriedPreparationIntent]carriedPreparationEntry{},
		activeTopologyKVs: map[carriedPreparationIntent]carriedPreparationEntry{},
	}
	allowed := plan.allowedPlanHashes()
	for position, entry := range entries {
		if entry.Stage != StageVerified || !allowed[entry.PlanHash] {
			continue
		}
		key := carriedPreparationIntent{actionId: entry.ActionID, intentHash: entry.IntentHash}
		value := carriedPreparationEntry{entry: entry, position: position}
		self.verifiedKVs[key] = value
		if entry.ActionID == "topology.launch" && entry.PlanHash == plan.PlanHash {
			self.activeTopologyKVs[key] = value
		}
	}
	return self
}

// Each accepted intent competes by its original position in the one snapshot;
// neither the active intent nor ancestor-list order receives preference.
func (self *carriedPreparationIndex) find(action Action, includeAncestorTopology bool) (JournalEntry, bool) {
	entries := self.verifiedKVs
	if action.ID == "topology.launch" && !includeAncestorTopology {
		entries = self.activeTopologyKVs
	}
	latest, found := entries[carriedPreparationIntent{actionId: action.ID, intentHash: action.IntentHash}]
	for _, intent := range action.AcceptedPriorIntentHashes {
		candidate, ok := entries[carriedPreparationIntent{actionId: action.ID, intentHash: intent}]
		if ok && (!found || candidate.position > latest.position) {
			latest, found = candidate, true
		}
	}
	return latest.entry, found
}
