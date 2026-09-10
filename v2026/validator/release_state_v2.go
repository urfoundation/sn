//go:build linux || darwin

// Startup owns the complete disk-ledger census before publishing any operator.
// Snapshot reads are bounded and descriptor-owned; neither loading a snapshot
// nor importing a signed ledger attaches Stats or authorizes native submission.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// The root owns Close after all consumers join. Fields remain private and
// immutable until initialization/recovery takes over the retained ledgers.
// The snapshots are untrusted candidates, not attached live statistics.
type releaseEvidenceV2DiskState struct {
	states          map[uint64]*releaseAttemptState
	participants    []AttemptSettlementRuntimeV2Participant
	snapshots       []*StatsEngine
	snapshotBytes   [][]byte
	snapshotPresent []bool
	journalBytes    []byte
	journalPresent  bool
	initialHistory  *AttemptSettlementClosure
	ledgerOwners    []releaseEvidenceV2DiskLedgerOwner
	closeOnce       sync.Once
	closeErr        error
}

// Cleanup owns acquisition values, not the mutable runtime projection maps.
// A borrowed private Stats/proof wrapper never acquires ledger Close authority.
type releaseEvidenceV2DiskLedgerOwner struct {
	noID   uint64
	ledger *AttemptLedger
}

// Actual acquired ledgers are visible only for deterministic lifecycle tests.
// An observer may inspect/cancel, but cannot replace a ledger or read result.
type releaseEvidenceV2DiskObserver struct {
	opened     func(uint64, *AttemptLedger)
	beforeRead func(uint64)
}

// Only the admitted acquisition fields cross a callback boundary. Keys are
// detached before workers start, and cleanup uses this same fixed census.
type releaseEvidenceV2DiskInput struct {
	noID       uint64
	stateDir   string
	identity   AttemptLedgerIdentity
	privateKey ed25519.PrivateKey
}

// Call after real activation/native/boundary authentication and before worker
// construction. The complete configured history is replayed again at this
// byte-consumer boundary; a prior bootstrap result is not an authority token.
func openReleaseEvidenceV2DiskState(ctx context.Context, cfg *ReleaseConfig, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey) (*releaseEvidenceV2DiskState, error) {
	return openReleaseEvidenceV2DiskStateWithObserver(ctx, cfg, inputs, serverKeys, releaseEvidenceV2DiskObserver{})
}

// Independent operator acquisition is bounded by effective cores. Any failure
// cancels and joins the entire census, then closes every acquired disk owner.
func openReleaseEvidenceV2DiskStateWithObserver(ctx context.Context, cfg *ReleaseConfig, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey, observer releaseEvidenceV2DiskObserver) (result *releaseEvidenceV2DiskState, resultErr error) {
	if ctx == nil || cfg == nil {
		return nil, errors.New("evidence disk startup context or configuration is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			if result != nil {
				resultErr = errors.Join(resultErr, result.close())
			}
			result = nil
		}
	}()
	history, err := replayReleaseEvidenceV2ActivationHistories(ctx, cfg, inputs, serverKeys)
	if err != nil {
		return nil, err
	}
	operators := make(map[uint64]OperatorConfig, len(cfg.Operators))
	for _, operator := range cfg.Operators {
		operators[operator.NoID] = operator
	}
	stateDir, coordinator := cfg.StateDir, strings.ToLower(cfg.Coordinator)
	diskBounds, persistence := cfg.EvidenceV2.Bounds.Disk, cfg.EvidenceV2.Bounds.Persistence
	statsConfig := StatsConfig{AMin: cfg.Policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis}
	diskInputs := make([]releaseEvidenceV2DiskInput, len(inputs))
	// Validate every key before the first directory/import mutation. Preserve
	// the historical ledger UID; current registration is a separate identity.
	for index, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		vpk, err := canonicalAttemptHex32("evidence disk validator", input.Context.InitialCut.Identity.ValidatorVPK, false)
		if err != nil {
			return nil, err
		}
		if err := attemptCutV2PrivateKey(input.PrivateKey, vpk[:]); err != nil {
			return nil, err
		}
		seed, err := loadClientSeed(operators[input.Config.NoID].ClientKeySeedFile)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(ed25519.NewKeyFromSeed(seed), input.PrivateKey) {
			return nil, errors.New("evidence disk signing key differs from configured private input")
		}
		diskInputs[index] = releaseEvidenceV2DiskInput{noID: input.Config.NoID, stateDir: operators[input.Config.NoID].StateDir, identity: input.Context.InitialCut.Identity, privateKey: bytes.Clone(input.PrivateKey)}
	}
	operationCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	states := make([]*releaseAttemptState, len(diskInputs))
	failures := make([]error, len(diskInputs))
	completed := make([]bool, len(diskInputs))
	var next atomic.Uint64
	var joined sync.WaitGroup
	workers := min(len(diskInputs), runtime.GOMAXPROCS(0))
	joined.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer joined.Done()
			for {
				index := next.Add(1) - 1
				if index >= uint64(len(diskInputs)) || operationCtx.Err() != nil {
					return
				}
				input := diskInputs[index]
				ledger, err := NewDiskAttemptLedger(operationCtx, input.stateDir, input.identity, coordinator, input.privateKey, diskBounds)
				if err != nil {
					failures[index] = fmt.Errorf("evidence disk no_id %d: %w", input.noID, err)
					cancel()
					return
				}
				states[index] = &releaseAttemptState{ledger: ledger, stats: NewStatsEngine(statsConfig)}
				if observer.opened != nil {
					observer.opened(input.noID, ledger)
				}
				completed[index] = true
			}
		}()
	}
	joined.Wait()
	for index, input := range diskInputs {
		if !completed[index] && failures[index] == nil {
			failures[index] = fmt.Errorf("evidence disk no_id %d acquisition did not complete", input.noID)
		}
	}
	owned := &releaseEvidenceV2DiskState{states: make(map[uint64]*releaseAttemptState, len(diskInputs)), participants: make([]AttemptSettlementRuntimeV2Participant, len(diskInputs)), snapshots: make([]*StatsEngine, len(diskInputs)), snapshotBytes: make([][]byte, len(diskInputs)), snapshotPresent: make([]bool, len(diskInputs)), initialHistory: history}
	for index, input := range diskInputs {
		if states[index] == nil {
			continue
		}
		owned.ledgerOwners = append(owned.ledgerOwners, releaseEvidenceV2DiskLedgerOwner{noID: input.noID, ledger: states[index].ledger})
		owned.states[input.noID] = states[index]
		owned.participants[index] = AttemptSettlementRuntimeV2Participant{NoID: input.noID, StateDir: input.stateDir, Stats: states[index].stats, Ledger: states[index].ledger}
	}
	transferred := false
	defer func() {
		if !transferred {
			resultErr = errors.Join(resultErr, owned.close())
		}
	}()
	if err := errors.Join(append(failures, operationCtx.Err())...); err != nil {
		return nil, err
	}
	physical := attemptSettlementV2PhysicalIO()
	physical.ctx = operationCtx
	physical, err = retainAttemptSettlementV2Roots(stateDir, owned.participants, physical)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, physical.closeRoots()) }()
	owned.journalBytes, owned.journalPresent, err = readAttemptSettlementV2Path(attemptSettlementTransactionV2Path(stateDir), persistence.MaxJournalBytes, physical)
	if err != nil {
		return nil, err
	}
	for index, participant := range owned.participants {
		if observer.beforeRead != nil {
			observer.beforeRead(participant.NoID)
		}
		encoded, exists, err := readAttemptSettlementV2Path(filepath.Join(participant.StateDir, "stats.json"), persistence.MaxSnapshotBytes, physical)
		if err != nil {
			return nil, err
		}
		owned.snapshotBytes[index], owned.snapshotPresent[index] = encoded, exists
		if !exists {
			continue
		}
		candidate, err := decodeAttemptSettlementV2Stats(operationCtx, encoded, participant.Stats.cfg, persistence.MaxSnapshotBytes)
		if err != nil {
			return nil, err
		}
		owned.snapshots[index] = candidate
		// Journals may contain differing pre/post images; their complete
		// decoder/recovery must check both before a single snapshot write.
		if !owned.journalPresent {
			var expected *AttemptSettlementTransition
			if history != nil {
				expected = history.Transitions[index]
			}
			if err := matchReleaseActivationHistoryV2Transition(expected, candidate.settlementTransition); err != nil {
				return nil, err
			}
		}
	}
	if err := physical.closeRoots(); err != nil {
		return nil, err
	}
	if err := operationCtx.Err(); err != nil {
		return nil, err
	}
	transferred = true
	return owned, nil
}

// Close is idempotent and joins all opened disk backends, including a failed
// partial acquisition. No caller closes the shared configuration/state roots.
func (self *releaseEvidenceV2DiskState) close() error {
	if self == nil {
		return nil
	}
	self.closeOnce.Do(func() {
		for _, owner := range self.ledgerOwners {
			self.closeErr = errors.Join(self.closeErr, releaseStageError(fmt.Sprintf("validator no_id %d attempt ledger shutdown", owner.noID), owner.ledger.Close()))
		}
	})
	return self.closeErr
}
