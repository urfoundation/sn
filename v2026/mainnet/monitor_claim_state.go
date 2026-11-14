package main

import (
	"errors"
	"math"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Prior exact assertions survive a bounded source census omitting old entries.
// Their receipt time is historical; only current observation can refresh reads.
type monitorClaimEpochState struct {
	Epoch       int64                      `json:"epoch"`
	Archived    bool                       `json:"archived,omitempty"`
	Observation *protocol.ClaimObservation `json:"observation,omitempty"`
	Proof       *protocol.ClaimObservation `json:"proof,omitempty"`
	FirstSeenAt time.Time                  `json:"first_seen_at"`
	ProgressAt  time.Time                  `json:"progress_at"`
}

type monitorClaimState struct {
	SampleAt           time.Time                `json:"sample_at"`
	HighWaterAt        time.Time                `json:"high_water_at"`
	LastAcceptedAt     time.Time                `json:"last_accepted_at"`
	SemanticProgressAt time.Time                `json:"semantic_progress_at"`
	OutageSince        time.Time                `json:"outage_since"`
	LastIssueAt        time.Time                `json:"last_issue_at"`
	LastIssue          string                   `json:"last_issue"`
	Incidents          uint64                   `json:"incidents"`
	Restarts           uint64                   `json:"restarts"`
	Status             string                   `json:"status"`
	Record             *protocol.ClaimProgress  `json:"record,omitempty"`
	Epochs             []monitorClaimEpochState `json:"epochs"`
	current            bool
}

func newMonitorClaimState(policy monitorClaimPolicy) *monitorClaimState {
	value := &monitorClaimState{Status: "starting"}
	for _, expected := range policy.Epochs {
		value.Epochs = append(value.Epochs, monitorClaimEpochState{Epoch: expected.Epoch})
	}
	return value
}

func monitorClaimAccepted(observation *protocol.ClaimObservation) bool {
	return observation != nil && (observation.EvidenceKind == "signed-receipt" || observation.EvidenceKind == "finalized-leaf" && observation.LeafClaimed != nil && *observation.LeafClaimed)
}

func cloneMonitorClaimObservation(value *protocol.ClaimObservation) *protocol.ClaimObservation {
	if value == nil {
		return nil
	}
	copyValue := *value
	if value.LeafClaimed != nil {
		claimed := *value.LeafClaimed
		copyValue.LeafClaimed = &claimed
	}
	return &copyValue
}

func cloneMonitorClaimProgress(value *protocol.ClaimProgress) *protocol.ClaimProgress {
	copyValue := *value
	if value.DeclaredPool != nil {
		pool := *value.DeclaredPool
		copyValue.DeclaredPool = &pool
	}
	if value.OldestUnresolvedEpoch != nil {
		epoch := *value.OldestUnresolvedEpoch
		copyValue.OldestUnresolvedEpoch = &epoch
	}
	copyValue.Entries = append([]protocol.ClaimProgressEntry(nil), value.Entries...)
	for index := range copyValue.Entries {
		copyValue.Entries[index].Observation = cloneMonitorClaimObservation(value.Entries[index].Observation)
	}
	return &copyValue
}

// A contradiction to retained acceptance is visible and stops only this role.
// Missing optional detail cannot erase earlier exact asserted evidence.
func (self *monitorClaimState) observationCode(policy monitorClaimPolicy, value *protocol.ClaimProgress, now time.Time) string {
	if value == nil || value.Validate() != nil {
		return "invalid"
	}
	if code := monitorClaimIdentity(policy, value); code != "ok" {
		return code
	}
	if value.Status != "active" || value.Sequence == 0 {
		return "unavailable"
	}
	published, _ := time.Parse(time.RFC3339Nano, value.PublishedAt)
	started, _ := time.Parse(time.RFC3339Nano, value.StartedAt)
	if published.After(now) || now.Sub(published) > time.Duration(policy.FreshnessSeconds)*time.Second {
		return "stale"
	}
	if prior := self.Record; prior != nil {
		priorPublished, _ := time.Parse(time.RFC3339Nano, prior.PublishedAt)
		priorStarted, _ := time.Parse(time.RFC3339Nano, prior.StartedAt)
		if value.InstanceId == prior.InstanceId {
			if value.StartedAt != prior.StartedAt {
				return "identity"
			}
			if value.Sequence <= prior.Sequence || published.Before(priorPublished) {
				return "stale"
			}
		} else if !started.After(priorStarted) || published.Before(priorPublished) {
			return "stale"
		}
	}
	for _, prior := range self.Epochs {
		for _, entry := range value.Entries {
			policy.observeWork("retained-observation-comparison", 1)
			if entry.Epoch != prior.Epoch || entry.Observation == nil {
				continue
			}
			before, after := prior.Observation, entry.Observation
			if prior.Proof != nil && after.EvidenceKind == "finalized-leaf" && (prior.Proof.PayoutRoot != after.PayoutRoot || prior.Proof.ArtifactHash != "" && after.ArtifactHash != "" && prior.Proof.ArtifactHash != after.ArtifactHash) {
				return "contradiction"
			}
			if before == nil {
				continue
			}
			if monitorClaimAccepted(before) && !monitorClaimAccepted(after) || before.EvidenceKind == "signed-receipt" && after.EvidenceKind == "signed-receipt" && (before.TransactionHash != after.TransactionHash || before.AcceptedAmountRao != after.AcceptedAmountRao || before.BlockHash != after.BlockHash) {
				return "contradiction"
			}
			if before.EvidenceKind == "signed-receipt" && after.EvidenceKind == "signed-receipt" && monitorClaimPaymentKnown(before) && monitorClaimPaymentKnown(after) && (before.PaymentStatus != after.PaymentStatus || before.UnpaidCreditRao != after.UnpaidCreditRao || before.AggregatePaidRao != after.AggregatePaidRao) {
				return "contradiction"
			}
		}
	}
	return "ok"
}

func (self *monitorClaimState) observe(policy monitorClaimPolicy, value *protocol.ClaimProgress, code string, now time.Time) {
	self.current = false
	if !self.HighWaterAt.IsZero() && now.Before(self.HighWaterAt) && !monitorClaimTerminal(code) {
		code = "clock"
	}
	self.SampleAt = now
	if now.After(self.HighWaterAt) {
		self.HighWaterAt = now
	}
	if code == "ok" {
		code = self.observationCode(policy, value, now)
	}
	if code == "ok" {
		if self.Record != nil && self.Record.InstanceId != value.InstanceId && self.Restarts < math.MaxUint64 {
			self.Restarts++
		}
		for index := range self.Epochs {
			retained := &self.Epochs[index]
			for _, entry := range value.Entries {
				policy.observeWork("current-observation-comparison", 1)
				if entry.Epoch != retained.Epoch || entry.Observation == nil {
					continue
				}
				observation := entry.Observation
				progressed := false
				if retained.Proof == nil && observation.EvidenceKind == "finalized-leaf" && observation.ProofStatus == "merkle-verified" {
					retained.Proof = cloneMonitorClaimObservation(observation)
					progressed = true
				}
				// Do not replace a receipt with weaker leaf/API evidence.
				if retained.Observation != nil && retained.Observation.EvidenceKind == "signed-receipt" && (observation.EvidenceKind != "signed-receipt" || monitorClaimPaymentKnown(retained.Observation) && !monitorClaimPaymentKnown(observation)) {
					if progressed {
						retained.ProgressAt, self.SemanticProgressAt = now, now
					}
					continue
				}
				if retained.FirstSeenAt.IsZero() {
					retained.FirstSeenAt = now
				}
				progressed = progressed || !monitorClaimAccepted(retained.Observation) && monitorClaimAccepted(observation) || observation.EvidenceKind == "signed-receipt" && (retained.Observation == nil || retained.Observation.EvidenceKind != "signed-receipt") || !monitorClaimPaymentKnown(retained.Observation) && monitorClaimPaymentKnown(observation)
				if progressed {
					retained.ProgressAt, self.SemanticProgressAt = now, now
				}
				retained.Observation = cloneMonitorClaimObservation(entry.Observation)
			}
		}
		self.Record = cloneMonitorClaimProgress(value)
		self.LastAcceptedAt, self.current = now, true
		if self.summary(policy, now).Overdue > 0 {
			code = "overdue"
		}
	}
	if code == "ok" {
		self.OutageSince = time.Time{}
	} else {
		if self.OutageSince.IsZero() {
			self.OutageSince = now
		}
		if code != self.Status && self.Incidents < math.MaxUint64 {
			self.Incidents++
		}
		self.LastIssue, self.LastIssueAt = code, now
	}
	self.Status = code
}

// This fixed-size summary fits the shared diagnostic sink. Exact decimal
// amounts remain in retained typed evidence, never lossy floats or labels.
type monitorClaimSummary struct {
	Expected         int       `json:"expected"`
	Unknown          int       `json:"unknown"`
	AcceptedReceipts int       `json:"accepted_receipts"`
	ClaimedLeaves    int       `json:"claimed_leaves"`
	MerkleProofs     int       `json:"merkle_proofs"`
	ApiNoClaim       int       `json:"api_no_claim"`
	Deferred         int       `json:"deferred"`
	AggregatePaid    int       `json:"aggregate_paid"`
	InvalidPayment   int       `json:"invalid_payment"`
	Overdue          int       `json:"overdue"`
	ProgressAt       time.Time `json:"semantic_progress_at"`
}

func (self *monitorClaimState) summary(policy monitorClaimPolicy, now time.Time) monitorClaimSummary {
	result := monitorClaimSummary{Expected: len(policy.Epochs), ProgressAt: self.SemanticProgressAt}
	for index, expected := range policy.Epochs {
		policy.observeWork("summary-epoch", 1)
		observation := self.Epochs[index].Observation
		if self.Epochs[index].Proof != nil {
			result.MerkleProofs++
		}
		deadline, _ := time.Parse(time.RFC3339Nano, expected.AcceptBy)
		if !now.Before(deadline) && !monitorClaimAccepted(observation) {
			result.Overdue++
		}
		if observation == nil {
			result.Unknown++
			continue
		}
		switch observation.EvidenceKind {
		case "api-no-claim":
			result.ApiNoClaim++
		case "finalized-leaf":
			if observation.LeafClaimed != nil && *observation.LeafClaimed {
				result.ClaimedLeaves++
			}
		case "signed-receipt":
			result.AcceptedReceipts++
		}
		switch observation.PaymentStatus {
		case "deferred":
			result.Deferred++
		case "aggregate-paid":
			result.AggregatePaid++
		}
		// Current malformed payment reporting stays visible even while an
		// earlier exact assertion remains in retained evidence.
		if self.Record != nil {
			for _, entry := range self.Record.Entries {
				policy.observeWork("summary-payment-comparison", 1)
				if entry.Epoch == expected.Epoch && entry.Observation != nil && entry.Observation.PaymentStatus == "invalid" {
					result.InvalidPayment++
				}
			}
		}
	}
	return result
}

var monitorClaimCodes = map[string]int{"starting": 0, "ok": 1, "unknown": 2, "overdue": 3, "unavailable": 4, "invalid": 5, "stale": 6, "clock": 7, "identity": 8, "authentication": 9, "contradiction": 10}

func monitorClaimTerminal(code string) bool {
	return code == "identity" || code == "authentication" || code == "contradiction"
}

func monitorClaimPaymentKnown(observation *protocol.ClaimObservation) bool {
	return observation != nil && (observation.PaymentStatus == "deferred" || observation.PaymentStatus == "aggregate-paid")
}

func validateMonitorClaimState(policy monitorClaimPolicy, state monitorClaimState) error {
	if state.SampleAt.IsZero() || state.HighWaterAt.IsZero() || state.HighWaterAt.Before(state.SampleAt) || len(state.Epochs) != len(policy.Epochs) {
		return errors.New("claim checkpoint lacks a bounded observation and exact epoch census")
	}
	if _, ok := monitorClaimCodes[state.Status]; !ok || state.Status == "starting" || (state.Record == nil) != state.LastAcceptedAt.IsZero() || state.Status != "ok" && state.OutageSince.IsZero() || state.Status == "ok" && !state.OutageSince.IsZero() {
		return errors.New("claim checkpoint status or observation provenance differs")
	}
	if state.Record != nil && (state.Record.Validate() != nil || state.Record.Status != "active" || monitorClaimIdentity(policy, state.Record) != "ok") {
		return errors.New("claim checkpoint retained a foreign or incomplete publication")
	}
	for index, epoch := range state.Epochs {
		policy.observeWork("validated-state-epoch", 1)
		if epoch.Archived || epoch.Epoch != policy.Epochs[index].Epoch || epoch.FirstSeenAt.After(state.HighWaterAt) || epoch.ProgressAt.After(state.HighWaterAt) {
			return errors.New("claim checkpoint expected epoch or times differ")
		}
		if epoch.Observation == nil {
			if !epoch.FirstSeenAt.IsZero() || !epoch.ProgressAt.IsZero() || epoch.Proof != nil {
				return errors.New("claim checkpoint invented absent evidence history")
			}
			continue
		}
		observation := epoch.Observation
		if observation.Validate() != nil || observation.Epoch != epoch.Epoch || epoch.FirstSeenAt.IsZero() || !epoch.ProgressAt.IsZero() && epoch.ProgressAt.Before(epoch.FirstSeenAt) || state.Record == nil {
			return errors.New("claim checkpoint lost retained evidence identity")
		}
		if observation.EvidenceKind != "api-no-claim" && (observation.Pool != policy.ExpectedPool || observation.ShareBps != policy.Epochs[index].ShareBps) {
			return errors.New("claim checkpoint evidence differs from independent pool")
		}
		observed, _ := time.Parse(time.RFC3339Nano, observation.ObservedAt)
		if observed.After(state.HighWaterAt) || epoch.ProgressAt.After(state.SemanticProgressAt) {
			return errors.New("claim checkpoint evidence time differs")
		}
		if proof := epoch.Proof; proof != nil {
			if proof.Validate() != nil || proof.Epoch != epoch.Epoch || proof.EvidenceKind != "finalized-leaf" || proof.ProofStatus != "merkle-verified" || proof.Pool != policy.ExpectedPool || proof.ShareBps != policy.Epochs[index].ShareBps || policy.Epochs[index].PayoutRoot != "" && proof.PayoutRoot != policy.Epochs[index].PayoutRoot || policy.Epochs[index].ArtifactHash != "" && proof.ArtifactHash != policy.Epochs[index].ArtifactHash {
				return errors.New("claim checkpoint proof differs from independent expectation")
			}
			observed, _ := time.Parse(time.RFC3339Nano, proof.ObservedAt)
			if observed.After(state.HighWaterAt) {
				return errors.New("claim checkpoint proof time differs")
			}
		}
	}
	return nil
}
