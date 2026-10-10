// Operational progress belongs to the running observation command, separately
// from financial custody. Estimates use acknowledged blocks and local elapsed
// time; a restart, unavailable source or changed identity cannot reuse a rate.
package main

import (
	"math/big"
	"os"
	"time"
)

// Exact measurement inputs remain available in JSON. Scalar projections round
// elapsed time upward and never represent a future boundary or repair promise.
type economicConservationRate struct {
	Blocks             uint64    `json:"blocks"`
	FromAt             time.Time `json:"from_at"`
	ThroughAt          time.Time `json:"through_at"`
	ElapsedNanoseconds int64     `json:"elapsed_nanoseconds"`
}

// The observed target and its original read time are retained evidence. A
// current empty backlog is different from an unavailable or unknown target.
type economicConservationComponentProgress struct {
	Class                    string                    `json:"class"`
	Action                   string                    `json:"action"`
	BlockingDependency       string                    `json:"blocking_dependency"`
	Cursor                   economicEmissionBoundary  `json:"acknowledged_cursor"`
	Finalized                *economicEmissionBoundary `json:"observed_finalized"`
	LastReadAt               time.Time                 `json:"last_successful_read_at"`
	LastProgressAt           time.Time                 `json:"instance_last_progress_at"`
	BacklogBlocks            *uint64                   `json:"backlog_blocks"`
	Preparation              *economicConservationRate `json:"preparation_throughput"`
	FinalizedCadence         *economicConservationRate `json:"local_finalized_observation_cadence"`
	CatchupEtaSeconds        *uint64                   `json:"catchup_eta_seconds"`
	FutureBoundaryEtaSeconds *uint64                   `json:"future_boundary_eta_seconds"`
	RepairEtaSeconds         *uint64                   `json:"repair_eta_seconds"`
}

// This identity is created by the running command after admission. Neither a
// retained checkpoint nor the continued existence of this output proves life.
// Sample attempts count parent cycles, not RPCs, transactions or child retries.
type economicConservationProgressSummary struct {
	Schema                 string                                `json:"schema"`
	ProcessId              int                                   `json:"process_id"`
	StartedAt              time.Time                             `json:"started_at"`
	CompletedAt            time.Time                             `json:"completed_at"`
	PolicyHash             string                                `json:"policy_hash"`
	CheckpointPath         string                                `json:"checkpoint_path"`
	CheckpointHash         string                                `json:"checkpoint_hash"`
	SampleAttempts         uint64                                `json:"sample_attempts"`
	NextSampleAfterSeconds *uint64                               `json:"next_sample_after_seconds"`
	EstimateScope          string                                `json:"estimate_scope"`
	Native                 economicConservationComponentProgress `json:"native"`
	Vault                  economicConservationComponentProgress `json:"vault"`
}

// Only the parent command accesses these fields. Pending polls keep the same
// preparation anchor, including the time spent inside an owned capture/replay.
type economicConservationProgress struct {
	policyHash     string
	checkpointPath string
	startedAt      time.Time
	attempts       uint64
	native         economicConservationProgressTracker
	vault          economicConservationProgressTracker
}

// A finalized observation and a completed preparation advance have separate
// anchors: chain cadence is not preparation throughput.
type economicConservationProgressTracker struct {
	cursor         economicEmissionBoundary
	anchorAt       time.Time
	anchorValid    bool
	lastAt         time.Time
	readHighWater  time.Time
	lastProgressAt time.Time
	finalized      economicEmissionBoundary
	finalizedAt    time.Time
	finalizedValid bool
}

// Initial elapsed time starts with this admitted invocation, never the age of
// its checkpoint. Historical downtime cannot become measured preparation.
func newEconomicConservationProgress(state *economicConservationState, checkpointPath string, now time.Time) *economicConservationProgress {
	return &economicConservationProgress{
		policyHash: state.PolicyHash, checkpointPath: checkpointPath, startedAt: now,
		native: economicConservationProgressTracker{cursor: state.Native.Cursor, anchorAt: now, anchorValid: true, lastAt: now},
		vault:  economicConservationProgressTracker{cursor: state.Vault.Cursor, anchorAt: now, anchorValid: true, lastAt: now},
	}
}

// Called only after financial publication is acknowledged. A metrics outage
// does not roll back these measurements or repeat acknowledged financial work.
func (self *economicConservationProgress) observe(state *economicConservationState, summary economicConservationSummary, now time.Time, nextSample time.Duration) *economicConservationProgressSummary {
	self.attempts++
	identityCurrent := state.PolicyHash == self.policyHash && summary.PolicyHash == self.policyHash && state.ContentHash == summary.CheckpointHash
	result := &economicConservationProgressSummary{
		Schema: "urnetwork-economic-conservation-progress-v1", ProcessId: os.Getpid(), StartedAt: self.startedAt,
		CompletedAt: now, PolicyHash: self.policyHash, CheckpointPath: self.checkpointPath, CheckpointHash: summary.CheckpointHash,
		SampleAttempts: self.attempts, EstimateScope: "catch-up-to-retained-observed-finalized-boundary-only; future-boundary-and-repair-time-unknown",
		Native: self.native.observe(state.Native.Cursor, state.Native.Finalized, state.Native.LastReadAt, now, summary.NativeCurrent, summary.NativePending, summary.NativeHeld, summary.NativeIssue, identityCurrent),
		Vault:  self.vault.observe(state.Vault.Cursor, state.Vault.Finalized, state.Vault.LastReadAt, now, summary.VaultCurrent, false, summary.VaultHeld, summary.VaultIssue, identityCurrent),
	}
	if nextSample > 0 {
		seconds := uint64((nextSample + time.Second - 1) / time.Second)
		result.NextSampleAfterSeconds = &seconds
	}
	return result
}

// Completed unavailability clears measurement continuity. A healthy pending
// worker keeps its original anchor but publishes no new rate, cadence or ETA.
func (self *economicConservationProgressTracker) observe(cursor economicEmissionBoundary, finalized *economicEmissionBoundary, lastReadAt, now time.Time, current, pending, held bool, issue string, admitted bool) economicConservationComponentProgress {
	result := economicConservationComponentProgress{Cursor: cursor, LastReadAt: lastReadAt, LastProgressAt: self.lastProgressAt, Class: "observation-unavailable", Action: "await-original-observation", BlockingDependency: "original-source-read"}
	if finalized != nil && !lastReadAt.IsZero() && !lastReadAt.After(now) && rootCanonicalHash(cursor.Hash) && rootCanonicalHash(finalized.Hash) && finalized.Number >= cursor.Number && (finalized.Number != cursor.Number || finalized.Hash == cursor.Hash) {
		copy := *finalized
		result.Finalized = &copy
		backlog := finalized.Number - cursor.Number
		result.BacklogBlocks = &backlog
	}
	clockCurrent := !now.IsZero() && !now.Before(self.lastAt)
	identityCurrent := admitted && rootCanonicalHash(cursor.Hash) && cursor.Number >= self.cursor.Number && (cursor.Number != self.cursor.Number || cursor.Hash == self.cursor.Hash)
	readCurrent := !lastReadAt.After(now) && (!current || !lastReadAt.IsZero()) && !lastReadAt.Before(self.readHighWater)
	if self.finalizedValid && finalized != nil {
		identityCurrent = identityCurrent && finalized.Number >= self.finalized.Number && (finalized.Number != self.finalized.Number || finalized.Hash == self.finalized.Hash)
		readCurrent = readCurrent && !lastReadAt.Before(self.finalizedAt) && (finalized.Number == self.finalized.Number || lastReadAt.After(self.finalizedAt))
	}
	if !clockCurrent || !identityCurrent || !readCurrent || held || issue != "" || !current && !pending {
		self.anchorValid, self.finalizedValid = false, false
		self.cursor, self.anchorAt = cursor, now
		if now.After(self.lastAt) {
			self.lastAt = now
		}
		switch {
		case held:
			result.Class, result.Action, result.BlockingDependency = "integrity-or-authorization-failure", "retain-original-evidence", "review-original-refusal"
		case !clockCurrent || !readCurrent:
			result.Class, result.Action, result.BlockingDependency = "observation-unavailable", "await-comparable-clock", "local-observation-clock"
		case !identityCurrent:
			result.Class, result.Action, result.BlockingDependency = "observation-unavailable", "await-comparable-boundary", "original-boundary-identity"
		}
		if !identityCurrent || !readCurrent {
			result.Finalized, result.BacklogBlocks = nil, nil
		}
		return result
	}
	self.lastAt = now
	if pending {
		result.Class, result.Action, result.BlockingDependency = "deferred-audit", "await-owned-capture-or-replay", "owned-native-observation"
		return result
	}
	self.readHighWater = lastReadAt
	result.Class, result.Action, result.BlockingDependency = "deferred-audit", "read-or-replay-finalized-blocks", "original-finalized-evidence"
	if !self.anchorValid {
		self.cursor, self.anchorAt, self.anchorValid = cursor, now, true
	} else if cursor.Number > self.cursor.Number {
		if elapsed := now.Sub(self.anchorAt); elapsed > 0 && self.anchorAt.Add(elapsed).Equal(now) {
			result.Preparation = &economicConservationRate{Blocks: cursor.Number - self.cursor.Number, FromAt: self.anchorAt, ThroughAt: now, ElapsedNanoseconds: int64(elapsed)}
		}
		self.cursor, self.anchorAt, self.lastProgressAt = cursor, now, now
		result.LastProgressAt = now
	}
	if result.Finalized != nil {
		if self.finalizedValid && finalized.Number > self.finalized.Number {
			if elapsed := lastReadAt.Sub(self.finalizedAt); elapsed > 0 && self.finalizedAt.Add(elapsed).Equal(lastReadAt) {
				result.FinalizedCadence = &economicConservationRate{Blocks: finalized.Number - self.finalized.Number, FromAt: self.finalizedAt, ThroughAt: lastReadAt, ElapsedNanoseconds: int64(elapsed)}
			}
			self.finalized, self.finalizedAt = *finalized, lastReadAt
		}
		if !self.finalizedValid {
			self.finalized, self.finalizedAt, self.finalizedValid = *finalized, lastReadAt, true
		}
		if *result.BacklogBlocks == 0 {
			zero := uint64(0)
			result.CatchupEtaSeconds = &zero
			result.Class, result.Action, result.BlockingDependency = "pending-finality", "wait-next-finalized-boundary", "chain-finality"
		} else if result.Preparation != nil {
			result.CatchupEtaSeconds = economicConservationRoundedDuration(*result.BacklogBlocks, result.Preparation.ElapsedNanoseconds, result.Preparation.Blocks, int64(time.Second))
		}
	} else {
		self.finalizedValid = false
	}
	return result
}

// Multiply before dividing with exact bounded big integers. Overflow or an
// invalid basis withdraws an estimate instead of stopping financial progress.
func economicConservationRoundedDuration(count uint64, elapsed int64, blocks uint64, unit int64) *uint64 {
	if elapsed <= 0 || blocks == 0 || unit <= 0 {
		return nil
	}
	numerator := new(big.Int).Mul(new(big.Int).SetUint64(count), big.NewInt(elapsed))
	denominator := new(big.Int).Mul(new(big.Int).SetUint64(blocks), big.NewInt(unit))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return nil
	}
	value := quotient.Uint64()
	return &value
}
