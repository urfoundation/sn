// A workload envelope is original signed source authority, not an observation of
// live subnet capacity. The complete captured census is checked independently.
package main

import (
	"errors"
	"fmt"
)

const nativeYumaWorkloadSchema = "urnetwork-original-yuma-populated-workload-v1"
const nativeYumaPopulatedMaximumOperations = 1024000000
const nativeYumaPopulatedMaximumEdges = 2 * 1024 * 1024

// Counts describe one complete planned epoch. Twofold reserves cover the
// aggregate weights, bonds and both directions of stake inheritance together.
// They do not authorize truncating an actual row or assume only validators can
// retain old bonds. Host memory/time admission remains a separate measurement.
type nativeYumaWorkload struct {
	Schema            string `json:"schema"`
	MaximumUids       uint64 `json:"maximum_uids"`
	MaximumValidators uint64 `json:"maximum_permitted_rows_including_owner"`
	MaximumWeights    uint64 `json:"maximum_original_weight_edges"`
	MaximumBonds      uint64 `json:"maximum_original_bond_edges"`
	MaximumParents    uint64 `json:"maximum_original_parent_shares"`
	MaximumChildren   uint64 `json:"maximum_original_child_shares"`
}

type nativeYumaWorkloadForecast struct {
	Edges                uint64 `json:"one_epoch_edges_and_shares"`
	ReservedEdges        uint64 `json:"twofold_edges_and_shares"`
	PairOperations       uint64 `json:"one_fixed_and_rational_pair_operations"`
	ReservedOperations   uint64 `json:"twofold_pair_operations"`
	MinimumWitnessBytes  uint64 `json:"minimum_complete_wire_bytes"`
	ReservedWitnessBytes uint64 `json:"twofold_admitted_witness_bytes"`
	Authority            string `json:"authority"`
}

// Every multiplication below follows bounded dimensions, before arithmetic.
// The per-lane bound charges 128 operations per uid, 16 per inherited share,
// 64 per weight/bond, and 512 per weight for the full original finite sigmoid
// (without assuming cache hits). A midpoint median may discard only one row
// per iteration: 3*V*(V+1)/2 + 16*V + 16 per column bounds that worst case.
// Both precision lanes and the complete final loss accumulator are included.
func (self nativeYumaWorkload) forecast(witnessBytes uint64) (nativeYumaWorkloadForecast, error) {
	result := nativeYumaWorkloadForecast{Authority: "signed-logical-envelope; actual-host-capacity-unmeasured"}
	n, v := self.MaximumUids, self.MaximumValidators
	if self.Schema != nativeYumaWorkloadSchema || n == 0 || n > rootCensusLimit || v == 0 || v > 128 || v > n || self.MaximumWeights == 0 || self.MaximumBonds == 0 || self.MaximumWeights > n*n || self.MaximumBonds > n*n || self.MaximumParents > n*n || self.MaximumChildren > n*n || witnessBytes > 32*1024*1024 {
		return result, errors.New("native Yuma populated workload has no bounded complete census")
	}
	shares := self.MaximumParents + self.MaximumChildren
	result.Edges = self.MaximumWeights + self.MaximumBonds + shares
	result.ReservedEdges = 2 * result.Edges
	lane := 4096 + 128*n + 16*shares + 64*(self.MaximumWeights+self.MaximumBonds) + 512*self.MaximumWeights + n*(3*v*(v+1)/2+16*v+16)
	result.PairOperations = 2*lane + 2*n
	result.ReservedOperations = 2 * result.PairOperations
	// This is a feasibility floor for the original envelopes and hex vectors,
	// not an upper bound on variable stack or rational companion encodings.
	// The whole actual serialized witness is independently measured and held
	// when it exceeds MaximumWitnessBytes; it is never sliced to fit this floor.
	result.MinimumWitnessBytes = 65536 + 4096*n + 8*(self.MaximumWeights+self.MaximumBonds) + 224*self.MaximumParents + 160*self.MaximumChildren
	result.ReservedWitnessBytes = 2 * witnessBytes
	if result.ReservedEdges > nativeYumaPopulatedMaximumEdges || result.ReservedOperations > nativeYumaPopulatedMaximumOperations {
		return result, errors.Join(errMonitorEconomicCapacity, errors.New("native Yuma complete twofold workload exceeds supported finite profile"))
	}
	return result, nil
}

func (self nativeYumaPolicy) validateWorkload() error {
	if self.Workload == nil {
		return nil
	}
	forecast, err := self.Workload.forecast(self.MaximumWitnessBytes)
	if err != nil {
		return err
	}
	if self.MaximumEdges < forecast.ReservedEdges || self.MaximumOperations < forecast.ReservedOperations || self.MaximumWitnessBytes < forecast.MinimumWitnessBytes || self.HotBlockReserve < 2 {
		return errors.Join(errMonitorEconomicCapacity, fmt.Errorf("native Yuma populated authority lacks complete twofold reserves: need %d edges, %d operations, at least %d witness bytes and two hot epochs", forecast.ReservedEdges, forecast.ReservedOperations, forecast.MinimumWitnessBytes))
	}
	return nil
}

// Inspect all raw rows, including inactive/zero rows and old-only bond owners,
// before any arithmetic. Signed forecast counts never replace original inputs.
func (self nativeYumaPolicy) admitWorkload(input nativeYumaInput) error {
	if self.Workload == nil {
		return nil
	}
	w := self.Workload
	var validators, weights, bonds, parents, children uint64
	for index, node := range input.Nodes {
		if node.Permit || node.Uid == input.OwnerUid {
			validators++
		}
		weights += uint64(len(input.Weights[index]))
		bonds += uint64(len(input.Bonds[index]))
		parents += uint64(len(node.Parents))
		children += uint64(len(node.Children))
	}
	if uint64(input.Count) > w.MaximumUids || validators > w.MaximumValidators || weights > w.MaximumWeights || bonds > w.MaximumBonds || parents > w.MaximumParents || children > w.MaximumChildren {
		return errors.Join(errMonitorEconomicCapacity, errors.New("native Yuma original populated census exceeds its signed workload; preserve every row and admit a larger original profile"))
	}
	return nil
}
