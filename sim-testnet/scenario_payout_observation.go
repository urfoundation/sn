// Payout membership follows the signed settlement window, independently of a
// later artifact published while terminal faults or claims are reconciling.
package main

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// Each small row is derived only after complete artifact signature, deployment
// identity and pinned chain-root checks. Full original artifacts remain public
// and are independently captured/replayed by the final verifier.
type OperatorPayoutTierArtifactObservation struct {
	Epoch                 uint64 `json:"epoch"`
	NoId                  int    `json:"no_id"`
	ContentHash           string `json:"content_hash"`
	PayoutRoot            string `json:"payout_root"`
	CandidateProviders    int    `json:"candidate_providers"`
	CandidateHeadExcluded int    `json:"candidate_head_excluded"`
	CandidateLeaves       int    `json:"candidate_leaves"`
	PoolTailProviders     int    `json:"pool_tail_providers"`
	PoolTailHeadExcluded  int    `json:"pool_tail_head_excluded"`
	PoolTailLeaves        int    `json:"pool_tail_leaves"`
	TierMembershipValid   bool   `json:"tier_membership_valid"`
	Error                 string `json:"error,omitempty"`
}

// Retain a failed cohort summary too: a later valid epoch cannot erase it.
func observeOperatorPayoutTierArtifact(cfg *ResolvedConfig, noId int, artifact *payoutArtifact, clients map[[16]byte]int, lifecycle *FleetLifecycleEvidence) OperatorPayoutTierArtifactObservation {
	membership, err := summarizePayoutTierMembershipForCandidates(cfg, noId, artifact, clients, fleetLifecycleCandidateMinerSet(cfg, lifecycle, artifact.Epoch))
	row := OperatorPayoutTierArtifactObservation{
		Epoch: artifact.Epoch, NoId: noId, ContentHash: artifact.ContentHash, PayoutRoot: fleetLifecycleHex(artifact.PayoutRoot),
		CandidateProviders: membership.CandidateProviders, CandidateHeadExcluded: membership.CandidateHeadExcluded, CandidateLeaves: membership.CandidateLeaves,
		PoolTailProviders: membership.PoolTailProviders, PoolTailHeadExcluded: membership.PoolTailHeadExcluded, PoolTailLeaves: membership.PoolTailLeaves,
		TierMembershipValid: err == nil,
	}
	if err != nil {
		row.Error = err.Error()
	}
	return row
}

// Non-window inspection retains its original latest-artifact semantics.
// Release and production acceptance require exact per-epoch authority and never
// borrow a good or bad cohort from outside their signed settlement window.
func scenarioPayoutTiersForAcceptance(e *scenarioEvaluation) (bool, string) {
	if e == nil || e.Cfg == nil || e.Cfg.Config == nil || e.Current == nil || len(e.Current.Operators) != e.Cfg.Config.Topology.Operators {
		return false, "payout acceptance operator census or configuration is unavailable"
	}
	if e.Window == nil && (e.Definition.Name == "release-1.0" || e.Definition.Name == "production-soak") {
		return false, "release payout acceptance requires its signed epoch window"
	}
	end := uint64(0)
	if e.Window != nil {
		var ok bool
		end, ok = checkedAdd(e.Window.FirstEpoch, e.Window.EpochCount)
		if !ok || e.Window.EpochCount == 0 || e.Current.Status == nil || e.Current.Status.Contracts == nil {
			return false, "payout acceptance window or pinned contract epochs are unavailable"
		}
	}
	seen := map[int]bool{}
	for _, operator := range e.Current.Operators {
		if operator.NoID < 1 || operator.NoID > e.Cfg.Config.Topology.Operators || seen[operator.NoID] {
			return false, "payout acceptance has a duplicate or foreign operator identity"
		}
		seen[operator.NoID] = true
		if e.Window == nil {
			row := OperatorPayoutTierArtifactObservation{Epoch: operator.LatestArtifactEpoch, NoId: operator.NoID,
				CandidateProviders: operator.CandidateProviders, CandidateHeadExcluded: operator.CandidateHeadExcluded, CandidateLeaves: operator.CandidateLeaves,
				PoolTailProviders: operator.PoolTailProviders, PoolTailHeadExcluded: operator.PoolTailHeadExcluded, PoolTailLeaves: operator.PoolTailLeaves, TierMembershipValid: operator.TierMembershipValid}
			if !scenarioPayoutTierValid(row) {
				return false, scenarioPayoutTierDetail(row)
			}
			continue
		}
		if len(operator.PayoutTierArtifacts) == 0 {
			return false, fmt.Sprintf("payout acceptance operator %d scoped epoch evidence is unavailable in the retained observation; latest artifact epoch %d does not prove [%d,%d)", operator.NoID, operator.LatestArtifactEpoch, e.Window.FirstEpoch, end)
		}
		rows := map[uint64]OperatorPayoutTierArtifactObservation{}
		for _, row := range operator.PayoutTierArtifacts {
			if row.Epoch < e.Window.FirstEpoch || row.Epoch >= end {
				continue
			}
			if _, duplicate := rows[row.Epoch]; duplicate || row.NoId != operator.NoID {
				return false, fmt.Sprintf("payout acceptance operator %d epoch %d is duplicated or has a foreign identity", operator.NoID, row.Epoch)
			}
			rows[row.Epoch] = row
		}
		for epoch := e.Window.FirstEpoch; epoch < end; epoch++ {
			row, found := rows[epoch]
			if !found {
				return false, fmt.Sprintf("payout acceptance operator %d epoch %d has no authenticated tier observation", operator.NoID, epoch)
			}
			if !scenarioPayoutTierMatchesChain(row, e.Current.Status.Contracts) {
				return false, fmt.Sprintf("payout acceptance operator %d epoch %d hash or root differs from its exact finalized contract row", operator.NoID, epoch)
			}
			if !scenarioPayoutTierValid(row) {
				return false, scenarioPayoutTierDetail(row)
			}
		}
	}
	return true, "every accepted operator epoch excludes its observed head cohort and retains pool-tail leaves"
}

// Cohort counts describe actual completed usage; inactive configured providers
// need not have a row. An empty tier or mixed payout remains a strict failure.
func scenarioPayoutTierValid(row OperatorPayoutTierArtifactObservation) bool {
	return row.TierMembershipValid && row.Error == "" && row.CandidateProviders > 0 && row.CandidateHeadExcluded == row.CandidateProviders && row.CandidateLeaves == 0 && row.PoolTailProviders > 0 && row.PoolTailHeadExcluded == 0 && row.PoolTailLeaves > 0 && row.PoolTailLeaves <= row.PoolTailProviders
}

func scenarioPayoutTierDetail(row OperatorPayoutTierArtifactObservation) string {
	return fmt.Sprintf("no=%d epoch=%d candidates=%d excluded=%d leaves=%d tail=%d tail_excluded=%d tail_leaves=%d error=%s", row.NoId, row.Epoch, row.CandidateProviders, row.CandidateHeadExcluded, row.CandidateLeaves, row.PoolTailProviders, row.PoolTailHeadExcluded, row.PoolTailLeaves, row.Error)
}

// Recheck the observation's hash/root binding at evaluation, including duplicate
// contract rows. A boolean summary alone never establishes the artifact source.
func scenarioPayoutTierMatchesChain(row OperatorPayoutTierArtifactObservation, contracts *ContractView) bool {
	hash, err := hex.DecodeString(strings.TrimPrefix(row.ContentHash, "sha256:"))
	root, rootOk := evidenceFixedHex(row.PayoutRoot, 32)
	if contracts == nil || err != nil || len(hash) != 32 || row.ContentHash != "sha256:"+hex.EncodeToString(hash) || !rootOk || row.PayoutRoot != "0x"+hex.EncodeToString(root) {
		return false
	}
	matches := 0
	for _, epoch := range contracts.Epochs {
		if epoch.Epoch != row.Epoch {
			continue
		}
		for _, operator := range epoch.Operators {
			if operator.NoID != uint64(row.NoId) {
				continue
			}
			if operator.Status != 2 || operator.CommitBlock == 0 || operator.CommitBlock > contracts.FinalizedHead.Number || !strings.EqualFold(operator.ArtifactHash, "0x"+hex.EncodeToString(hash)) || !strings.EqualFold(operator.PayoutRoot, row.PayoutRoot) {
				return false
			}
			matches++
		}
	}
	return matches == 1
}
