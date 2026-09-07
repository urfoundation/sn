package main

// Owns the exact-byte plan authentication boundary for one closed archive.

import "bytes"

// Keeps one owned byte snapshot for each successfully authenticated plan path.
// Decoded plans and their nested fields are borrowed read-only by builders.
type finalSemanticPlanSnapshot struct {
	data []byte
	plan *SetupPlan
}

// Reuses full authentication only for identical bytes in this reconstruction.
// Callers still resolve the path and check current lineage on every lookup.
// Changed bytes undergo the complete decoder; failures never enter the cache.
func (self *finalSemanticArchive) decodeSetupPlan(path string, data []byte) (*SetupPlan, error) {
	if prior, found := self.planPathSnapshots[path]; found && bytes.Equal(prior.data, data) {
		return prior.plan, nil
	}
	decode := self.planDecoder
	if decode == nil {
		decode = decodePersistedPlanBytes
	}
	plan, err := decode(data)
	if err != nil {
		return nil, err
	}
	if self.planPathSnapshots == nil {
		self.planPathSnapshots = make(map[string]finalSemanticPlanSnapshot)
	}
	self.planPathSnapshots[path] = finalSemanticPlanSnapshot{data: bytes.Clone(data), plan: plan}
	return plan, nil
}
