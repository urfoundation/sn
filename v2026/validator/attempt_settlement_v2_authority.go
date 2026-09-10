//go:build linux || darwin

package validator

// Local snapshot validation cannot establish activation authority. Runtime
// checks compare independently pinned contexts to the retained physical ledger
// prefix and replay signed observations before recovery can write or attach.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/urnetwork/connect/v2026"
)

// Reads only one actual cursor row, never a candidate-selected replacement
// root. The real disk Walk authenticates the retained root-owned namespace.
func attemptSettlementV2LedgerRoot(ctx context.Context, ledger *AttemptLedger, before uint64) (string, error) {
	if before == 0 {
		return zeroAttemptHash(), nil
	}
	var root string
	if err := ledger.Walk(ctx, before, before, func(record AttemptRecord) error {
		if record.Sequence != before {
			return errors.New("compact runtime cursor row differs")
		}
		root = record.RecordHash
		return nil
	}); err != nil {
		return "", err
	}
	if root == "" {
		return "", errors.New("compact runtime cursor row is missing")
	}
	return root, nil
}

// Holds actual ledger lifetime through the pending census without turning a
// refused activation into a synthetic terminal outcome.
func requireAttemptSettlementV2NoPending(ctx context.Context, ledger *AttemptLedger) error {
	if err := ledger.begin(ctx); err != nil {
		return err
	}
	defer ledger.active.Done()
	return ledger.disk.Pending(ctx, func(AttemptRecord) error { return errors.New("compact activation has a real unfinished attempt") })
}

// Authenticated activation is pinned to the actual fully applied prefix. A
// pending attempt cannot be silently excluded even when counters are empty.
func validateAttemptSettlementV2Activation(ctx context.Context, participants []AttemptSettlementRuntimeV2Participant, statsTs []*StatsEngine, epoch uint64, authority AttemptSettlementV2Options, persistence AttemptSettlementRuntimeV2PersistenceBounds) error {
	legacy := &AttemptSettlementClosure{Schema: AttemptSettlementClosureSchema}
	serverKeys := map[uint64]map[byte]ed25519.PublicKey{}
	legacyCount := 0
	for index, participant := range participants {
		stats, operator := statsTs[index], authority.Operators[participant.NoID]
		expected := operator.Expected
		head, err := participant.Ledger.checkedHead()
		if err != nil {
			return err
		}
		if head.LastSequence == ^uint64(0) || !stats.settlementEpochKnown || stats.settlementEpoch != epoch || stats.activeAttemptCount != 0 || len(stats.window) != 0 || len(stats.egress) != 0 || stats.attemptLastAppliedSequence != head.LastSequence || stats.attemptSettlementFirstSequence != head.LastSequence+1 || stats.attemptEgressFirstSequence != head.LastSequence+1 || stats.egressGeneration != expected.EgressGeneration || expected.Boundary.SettlementEpoch != epoch || expected.Activation.Domain.ActivationEpoch != epoch || expected.Activation.FirstSequence != head.LastSequence+1 || expected.Activation.PriorRoot != head.Root || expected.FirstSequence != head.LastSequence+1 || expected.EgressFirstSequence != expected.FirstSequence || expected.PriorRoot != head.Root {
			return errors.New("compact activation requires the exact drained empty applied prefix")
		}
		if stats.attemptV2 != nil && (stats.attemptV2.Activation != expected.Activation || stats.attemptV2.SettlementPriorRoot != head.Root || stats.attemptV2.Terminal != nil) {
			return errors.New("compact activation would replace retained activation or terminal history")
		}
		if _, err := attemptSettlementV2RawMeasurement(stats, operator); err != nil {
			return err
		}
		if err := requireAttemptSettlementV2NoPending(ctx, participant.Ledger); err != nil {
			return err
		}
		if stats.settlementTransition == nil {
			if len(stats.ema) != 0 || len(stats.emaPPM) != 0 || head.LastSequence != 0 {
				return errors.New("compact activation requires explicit signed migration of existing history")
			}
			continue
		}
		transition := stats.settlementTransition
		if transition.Identity != expected.Identity || transition.ToEpoch != epoch || transition.PreFold.AttemptCut == nil || transition.PreFold.Config != operator.Measurement.ExpectedConfig || transition.PreFold.AttemptCut.LastSequence != head.LastSequence || transition.PreFold.AttemptCut.Root != head.Root || !slices.Equal(transition.PostFold, sortedAttemptEMAQualities(stats.emaPPM)) || len(stats.ema) != len(stats.emaPPM) {
			return errors.New("compact activation legacy exact prior or terminal prefix differs")
		}
		if legacyCount == 0 {
			legacy.Epoch = transition.FromBoundary.SettlementEpoch
		}
		legacy.Transitions = append(legacy.Transitions, transition)
		serverKeys[participant.NoID] = operator.Measurement.Replay.ServerKeys
		legacyCount++
	}
	if legacyCount != 0 {
		if legacyCount != len(participants) {
			return errors.New("compact activation legacy history omits a configured operator")
		}
		encoded, err := marshalAttemptSettlementV2JSON(ctx, legacy, persistence.MaxJournalBytes, false, true)
		if err != nil {
			return err
		}
		if _, err := DecodeAttemptSettlementClosureWithServerKeys(encoded, serverKeys); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Current raw counters are reconstructed from actual signed local rows. Only
// the existing explicit record/provider/hash bounds are used. Prior EMA is
// preserved from the authenticated retained terminal, never refolded here.
func validateAttemptSettlementV2LedgerState(ctx context.Context, participant AttemptSettlementRuntimeV2Participant, stats *StatsEngine, operator AttemptSettlementV2OperatorOptions, maxBytes uint64) error {
	state := stats.attemptV2
	if state == nil || state.Activation != operator.Expected.Activation || !stats.settlementEpochKnown {
		return errors.New("compact runtime retained activation differs from independent authority")
	}
	head, err := participant.Ledger.checkedHead()
	if err != nil {
		return err
	}
	if head.LastSequence == ^uint64(0) || stats.attemptLastAppliedSequence > head.LastSequence || stats.attemptSettlementFirstSequence == 0 || stats.attemptLastAppliedSequence+1 < stats.attemptSettlementFirstSequence || stats.attemptLastAppliedSequence+1-stats.attemptSettlementFirstSequence > operator.Bounds.Records.MaxItems {
		return errors.New("compact runtime snapshot range exceeds its actual ledger or bound")
	}
	for _, cursor := range []struct {
		first uint64
		root  string
	}{
		{first: state.Activation.FirstSequence, root: state.Activation.PriorRoot},
		{first: stats.attemptSettlementFirstSequence, root: state.SettlementPriorRoot},
	} {
		if cursor.first == 0 || cursor.first > head.LastSequence+1 {
			return errors.New("compact runtime retained prefix is unavailable")
		}
		root, err := attemptSettlementV2LedgerRoot(ctx, participant.Ledger, cursor.first-1)
		if err != nil {
			return err
		}
		if root != cursor.root {
			return errors.New("compact runtime retained root differs from actual ledger prefix")
		}
	}
	measurement, err := attemptSettlementV2RawMeasurement(stats, operator)
	if err != nil {
		return err
	}
	replayed := NewStatsEngine(stats.cfg)
	replayed.ema, replayed.emaPPM = maps.Clone(stats.ema), maps.Clone(stats.emaPPM)
	depth, err := attemptCutV2PolicyDepth(operator.Expected, operator.Policy)
	if err != nil {
		return err
	}
	vpk, err := canonicalAttemptHex32("compact runtime replay validator", operator.Expected.Identity.ValidatorVPK, false)
	if err != nil {
		return err
	}
	if stats.attemptSettlementFirstSequence <= stats.attemptLastAppliedSequence {
		if err := participant.Ledger.Walk(ctx, stats.attemptSettlementFirstSequence, stats.attemptLastAppliedSequence, func(record AttemptRecord) error {
			if record.M != depth || record.Boundary.SettlementEpoch != stats.settlementEpoch {
				return errors.New("compact runtime replay policy or epoch differs")
			}
			if err := VerifyAttemptRecord(&record, operator.Expected.Identity, vpk[:], operator.Measurement.Replay.ServerKeys); err != nil {
				return err
			}
			if record.Disposition == AttemptDispositionPending {
				return nil
			}
			if err := replayed.validateAttemptStatsWithLock(&record); err != nil {
				return err
			}
			replayed.applyAttemptStatsWithLock(&record)
			if record.Sequence < stats.attemptEgressFirstSequence {
				replayed.egress = map[connect.Id]map[[32]byte]bool{}
			}
			_, err := attemptSettlementV2RawMeasurement(replayed, operator)
			return err
		}); err != nil {
			return err
		}
	}
	actual, err := attemptSettlementV2RawMeasurement(replayed, operator)
	if err != nil {
		return err
	}
	wantJSON, err := marshalAttemptSettlementV2JSON(ctx, measurement, maxBytes, false, false)
	if err != nil {
		return err
	}
	actualJSON, err := marshalAttemptSettlementV2JSON(ctx, actual, maxBytes, false, false)
	if err != nil || !bytes.Equal(wantJSON, actualJSON) {
		return errors.Join(errors.New("compact runtime saved counters differ from actual signed rows"), err)
	}
	return ctx.Err()
}

// Before sealing, the authenticated expected terminal must describe every
// retained mutable cursor, not a context copied from the signed candidate.
func validateAttemptSettlementV2Terminal(ctx context.Context, participant AttemptSettlementRuntimeV2Participant, stats *StatsEngine, epoch uint64, boundary AttemptBoundary, operator AttemptSettlementV2OperatorOptions, maxBytes uint64) (AttemptLedgerHead, error) {
	var zero AttemptLedgerHead
	expected := operator.Expected
	if stats.attemptV2 == nil || stats.attemptV2.Activation != expected.Activation || !stats.settlementEpochKnown || stats.settlementEpoch == ^uint64(0) || stats.settlementEpoch+1 != epoch || stats.settlementEpoch != boundary.SettlementEpoch || expected.Boundary != boundary || expected.FirstSequence != stats.attemptSettlementFirstSequence || expected.EgressFirstSequence != stats.attemptEgressFirstSequence || expected.EgressGeneration != stats.egressGeneration || expected.PriorRoot != stats.attemptV2.SettlementPriorRoot {
		return zero, errors.New("compact runtime terminal authority differs from owned generation")
	}
	if _, err := countAttemptSettlementV2Engine(ctx, stats, maxBytes); err != nil {
		return zero, err
	}
	if err := validateAttemptStatsV2Snapshot(stats.snapshotStats()); err != nil {
		return zero, err
	}
	if err := validateAttemptSettlementV2LedgerState(ctx, participant, stats, operator, maxBytes); err != nil {
		return zero, err
	}
	head, err := participant.Ledger.checkedHead()
	if err != nil || head.LastSequence != stats.attemptLastAppliedSequence {
		return zero, errors.Join(errors.New("compact runtime terminal has unapplied ledger rows"), err)
	}
	return head, nil
}

// Binds every canonical postimage to the independently replayed full closure
// and verifies the exact journal-owned postimage without folding a retry.
func validateAttemptSettlementTransactionV2(ctx context.Context, transaction *attemptSettlementTransactionV2, participants []AttemptSettlementRuntimeV2Participant, preimages, postimages []*StatsEngine, authority AttemptSettlementV2Options, persistence AttemptSettlementRuntimeV2PersistenceBounds) (*AttemptSettlementClosureV2, error) {
	if transaction.Kind == "activate" {
		if err := validateAttemptSettlementV2Activation(ctx, participants, preimages, transaction.Epoch, authority, persistence); err != nil {
			return nil, err
		}
		for index, pre := range preimages {
			entry := transaction.Snapshots[index]
			if len(entry.OriginalJSON) != 0 && !bytes.Equal(entry.OriginalJSON, entry.PreImageJSON) {
				return nil, errors.New("compact activation original checkpoint differs from its drained live image")
			}
			if pre.attemptV2 != nil {
				return nil, errors.New("compact activation journal replaces already active state")
			}
			pre.attemptV2 = &attemptStatsV2State{Activation: authority.Operators[participants[index].NoID].Expected.Activation, SettlementPriorRoot: authority.Operators[participants[index].NoID].Expected.Activation.PriorRoot}
			encoded, err := encodeAttemptSettlementV2Engine(ctx, pre, persistence.MaxSnapshotBytes)
			if err != nil || !bytes.Equal(encoded, transaction.Snapshots[index].StatsJSON) {
				return nil, errors.Join(errors.New("compact activation postimage differs from exact original state"), err)
			}
		}
		return nil, nil
	}
	closure, _, err := DecodeAttemptSettlementClosureV2(ctx, transaction.ClosureJSON, authority)
	if err != nil {
		return nil, err
	}
	if closure.Epoch == ^uint64(0) || closure.Epoch+1 != transaction.Epoch || len(closure.Transitions) != len(participants) {
		return nil, errors.New("compact transaction closed epoch differs")
	}
	for index, participant := range participants {
		pre, post, terminal := preimages[index], postimages[index], closure.Transitions[index]
		if encoded := transaction.Snapshots[index].OriginalJSON; len(encoded) != 0 {
			original, err := decodeAttemptSettlementV2Stats(ctx, encoded, participant.Stats.cfg, persistence.MaxSnapshotBytes)
			if err != nil {
				return nil, err
			}
			if err := validateAttemptSettlementV2LedgerState(ctx, participant, original, authority.Operators[participant.NoID], persistence.MaxSnapshotBytes); err != nil {
				return nil, err
			}
		}
		if _, err := validateAttemptSettlementV2Terminal(ctx, participant, pre, transaction.Epoch, terminal.FromBoundary, authority.Operators[participant.NoID], persistence.MaxSnapshotBytes); err != nil {
			return nil, err
		}
		raw, err := attemptSettlementV2RawMeasurement(pre, authority.Operators[participant.NoID])
		if err != nil {
			return nil, err
		}
		left, err := marshalAttemptSettlementV2JSON(ctx, raw, persistence.MaxSnapshotBytes, false, false)
		if err != nil {
			return nil, err
		}
		right, err := marshalAttemptSettlementV2JSON(ctx, terminal.PreFold, persistence.MaxSnapshotBytes, false, false)
		if err != nil || !bytes.Equal(left, right) || terminal.Cut.LastSequence != pre.attemptLastAppliedSequence {
			return nil, errors.Join(errors.New("compact transaction live preimage differs from full replay"), err)
		}
		candidate, err := attemptSettlementV2Postimage(ctx, pre, terminal, false, persistence.MaxSnapshotBytes)
		if err != nil {
			return nil, err
		}
		// Reporting floats use the legacy Wilson calculation and deliberately
		// differ from integer policy EMA. They remain the exact journal-owned
		// postimage; their canonical numeric/census validation is not a refold.
		candidate.ema = maps.Clone(post.ema)
		encoded, err := encodeAttemptSettlementV2Engine(ctx, candidate, persistence.MaxSnapshotBytes)
		if err != nil || !bytes.Equal(encoded, transaction.Snapshots[index].StatsJSON) {
			return nil, errors.Join(fmt.Errorf("compact transaction no_id %d postimage differs from real fold", participant.NoID), err)
		}
		if post.attemptV2.Activation != authority.Operators[participant.NoID].Expected.Activation {
			return nil, errors.New("compact transaction successor activation differs")
		}
	}
	return closure, nil
}

// Pure candidate folding is checked against the just-replayed signed result;
// carried legacy history survives unchanged and cannot recursively grow.
func attemptSettlementV2Postimage(ctx context.Context, pre *StatsEngine, terminal *AttemptSettlementTransitionV2, fold bool, maxBytes uint64) (*StatsEngine, error) {
	if _, err := countAttemptSettlementV2Engine(ctx, pre, maxBytes); err != nil {
		return nil, err
	}
	// The public closure result must not share a mutable slice or pointer with
	// the immutable-owned Stats carry state after this operation returns.
	encodedTerminal, err := marshalAttemptSettlementV2JSON(ctx, terminal, maxBytes, false, false)
	if err != nil {
		return nil, err
	}
	var retained AttemptSettlementTransitionV2
	if err := json.Unmarshal(encodedTerminal, &retained); err != nil {
		return nil, err
	}
	pre.mu.Lock()
	post := pre.cloneStatsWithLock()
	pre.mu.Unlock()
	if pre.attemptV2 == nil || pre.egressGeneration == ^uint64(0) || terminal.Cut.LastSequence == ^uint64(0) || terminal.ToEpoch != pre.settlementEpoch+1 {
		return nil, errors.New("compact successor generation overflows or differs")
	}
	if fold {
		post.foldStatsOwned()
		if !slices.Equal(sortedAttemptEMAQualities(post.emaPPM), terminal.PostFold) {
			return nil, errors.New("compact signed post-fold differs from actual Stats fold")
		}
	} else {
		// Retry validation reconstructs no quality fold. Full closure replay has
		// already proved these exact values from the journal's signed preimage.
		post.window, post.ema, post.emaPPM = map[connect.Id]*ProviderWindow{}, map[connect.Id]float64{}, map[connect.Id]uint32{}
		for _, quality := range terminal.PostFold {
			id, err := connect.ParseId(quality.ClientID)
			if err != nil {
				return nil, err
			}
			post.ema[id], post.emaPPM[id] = float64(quality.QualityPPM)/1_000_000, quality.QualityPPM
		}
	}
	post.egress = map[connect.Id]map[[32]byte]bool{}
	post.egressGeneration++
	post.settlementEpoch, post.settlementEpochKnown = terminal.ToEpoch, true
	post.attemptSettlementFirstSequence, post.attemptEgressFirstSequence = terminal.Cut.LastSequence+1, terminal.Cut.LastSequence+1
	post.attemptV2 = &attemptStatsV2State{Activation: pre.attemptV2.Activation, SettlementPriorRoot: terminal.Cut.Root, Terminal: &retained}
	post.attemptCutPending, post.attemptSettlementCutPending, post.attemptSettlementCutEpoch = true, true, terminal.ToEpoch
	return post, nil
}
