// Both live wire versions share exact scoring and exclusion without depending
// on the platform-specific compact stream or storage implementation.
package validator

import (
	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
	"math/big"
	"slices"
	"sort"
)

// Both live wire versions use exactly the same shared-prefix splitting, EMA,
// deterministic top-head selection and pool-exclusion rules. Preview is not a
// durable EMA commit; submission still commits only after the owned intent.
func finishReleaseHead(headEMA *HeadEMAStore, subnetEpoch uint64, policy protocol.Policy, fleets map[FleetScoreKey]map[[32]byte]bool, bound map[uint64]map[connect.Id]bool, controlledNO map[uint64]bool, membersByUID map[uint16][]releaseHeadMember, staleBindings []StaleHeadBinding, inputs []ReleaseMeasurementInput, bindings []ReleaseBindingMeasurement) (releaseHeadResult, error) {
	ema, head, err := headEMA.PreviewForEpoch(subnetEpoch, releaseRawHeadScores(fleets), policy.Steering.HeadScoreEMA)
	if err != nil {
		return releaseHeadResult{}, err
	}
	return assembleReleaseHead(policy, ema, head, bound, controlledNO, membersByUID, staleBindings, inputs, bindings)
}

// Exact selection consumes a completed real EMA preview. It neither verifies
// input evidence nor advances durable state; callers must own those boundaries.
func assembleReleaseHead(policy protocol.Policy, ema map[uint16]*big.Rat, head []HeadEMAMeasurement, bound map[uint64]map[connect.Id]bool, controlledNO map[uint64]bool, membersByUID map[uint16][]releaseHeadMember, staleBindings []StaleHeadBinding, inputs []ReleaseMeasurementInput, bindings []ReleaseBindingMeasurement) (releaseHeadResult, error) {
	uids := make([]uint16, 0, len(ema))
	for uid := range ema {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	eligible := make([]ExactWeightInput, 0, len(uids))
	for _, uid := range uids {
		eligible = append(eligible, ExactWeightInput{UID: uid, Score: ema[uid]})
	}
	selection, err := selectHeadFleets(eligible, policy.Steering.MaximumHeadFleets)
	if err != nil {
		return releaseHeadResult{}, err
	}
	controlledHead := excludeLiveHeadMembers(bound, controlledNO, membersByUID)
	sort.Slice(staleBindings, func(i, j int) bool {
		if staleBindings[i].NoID != staleBindings[j].NoID {
			return staleBindings[i].NoID < staleBindings[j].NoID
		}
		return staleBindings[i].ClientID < staleBindings[j].ClientID
	})
	return releaseHeadResult{
		Weights: selection.Selected, Eligible: append(append([]ExactWeightInput(nil), selection.Selected...), selection.Rejected...),
		Bound: bound, Controlled: controlledHead, EligibleUIDs: append(headSelectionUIDs(selection.Selected), headSelectionUIDs(selection.Rejected)...),
		SelectedUIDs: headSelectionUIDs(selection.Selected), RejectedUIDs: headSelectionUIDs(selection.Rejected),
		StaleBindings: staleBindings, Inputs: inputs, Bindings: bindings, HeadEMA: head,
	}, nil
}
