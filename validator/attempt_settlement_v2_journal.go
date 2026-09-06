//go:build linux || darwin

package validator

// One canonical transaction records original disk, pre-operation live and
// post-operation images. A later shutdown Save may legitimately write the live
// preimage; no arbitrary same-epoch snapshot is accepted as that image.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

const attemptSettlementTransactionV2Schema = "urnetwork-validator-settlement-transaction-v2"

// OriginalJSON is absent only when no original disk checkpoint existed. The
// hashes bind exact bytes, including reporting state and carried v1 history.
type attemptSettlementV2Snapshot struct {
	NoID uint64 `json:"no_id"`
	StatsPath string `json:"stats_path"`
	OriginalJSON []byte `json:"original_json,omitempty"`
	OriginalHash string `json:"original_hash,omitempty"`
	PreImageJSON []byte `json:"pre_image_json"`
	PreImageHash string `json:"pre_image_hash"`
	StatsJSON []byte `json:"stats_json"`
	StatsHash string `json:"stats_hash"`
}

// Activation has no closed epoch. Advance carries the exact immutable closure
// that was sealed before this metadata journal became durable.
type attemptSettlementTransactionV2 struct {
	Schema string `json:"schema"`
	Kind string `json:"kind"`
	Epoch uint64 `json:"epoch"`
	Snapshots []attemptSettlementV2Snapshot `json:"snapshots"`
	ClosureJSON []byte `json:"closure_json,omitempty"`
}

// The separate namespace cannot consume a legacy transaction.
func attemptSettlementTransactionV2Path(coordinator string) string {
	return filepath.Join(coordinator, "settlement-transaction-v2.json")
}

// Used for both stored and freshly read images; absence has no digest.
func attemptSettlementV2ImageHash(encoded []byte) string {
	if len(encoded) == 0 { return "" }
	digest := sha256.Sum256(encoded)
	return attemptHex32(digest)
}

// Roots and file closes are joined before absence can be treated as benign.
func readAttemptSettlementV2Path(path string, limit uint64, physical attemptSettlementV2IO) ([]byte, bool, error) {
	root, release, err := physical.openRoot(filepath.Dir(path))
	if err != nil { return nil, false, err }
	data, exists, readErr := readAttemptSettlementV2File(root, filepath.Base(path), limit, physical)
	closeErr := release()
	if closeErr != nil { return nil, false, errors.Join(readErr, closeErr) }
	if readErr != nil { return nil, false, readErr }
	return data, exists, nil
}

// Exact serialization prevents an unknown/duplicate-field alternate journal.
func canonicalAttemptSettlementTransactionV2(transaction *attemptSettlementTransactionV2) ([]byte, error) {
	encoded, err := json.Marshal(transaction)
	if err != nil { return nil, err }
	return append(encoded, '\n'), nil
}

// A fresh candidate decodes the existing snapshot codec, then proves exact
// canonical bytes. Loading does not attach a ledger or activate admission.
func decodeAttemptSettlementV2Stats(encoded []byte, config StatsConfig) (*StatsEngine, error) {
	stats := NewStatsEngine(config)
	if err := stats.loadStatsSnapshotOwned(encoded); err != nil { return nil, err }
	canonical, err := encodeStatsSnapshot(stats.snapshotStats())
	if err != nil || !bytes.Equal(canonical, encoded) {
		return nil, errors.Join(errors.New("compact runtime statistics image is not canonical"), err)
	}
	return stats, nil
}

// Preimages retain the same exact epoch, cursors, generation and activation;
// an older on-disk checkpoint may lag only its applied observation sequence.
func validateAttemptSettlementV2Original(original, live *StatsEngine) error {
	if original.settlementEpochKnown != live.settlementEpochKnown || original.settlementEpoch != live.settlementEpoch || original.egressGeneration != live.egressGeneration || original.attemptSettlementFirstSequence != live.attemptSettlementFirstSequence || original.attemptEgressFirstSequence != live.attemptEgressFirstSequence || original.attemptLastAppliedSequence > live.attemptLastAppliedSequence {
		return errors.New("compact transaction original checkpoint has an unrelated generation")
	}
	left, err := json.Marshal(original.attemptV2)
	if err != nil { return err }
	right, err := json.Marshal(live.attemptV2)
	if err != nil || !bytes.Equal(left, right) {
		return errors.Join(errors.New("compact transaction original activation/history differs"), err)
	}
	left, err = json.Marshal(original.settlementTransition)
	if err != nil { return err }
	right, err = json.Marshal(live.settlementTransition)
	if err != nil || !bytes.Equal(left, right) {
		return errors.Join(errors.New("compact transaction original legacy history differs"), err)
	}
	return nil
}

// Captures all three images before the first journal write. The raw window
// postimage is independently checked against the signed closure by admission.
func newAttemptSettlementTransactionV2(ctx context.Context, kind string, epoch uint64, batch *attemptSettlementV2Owners, postimages []*StatsEngine, closure *AttemptSettlementClosureV2, authority AttemptSettlementV2Options, physical attemptSettlementV2IO) (*attemptSettlementTransactionV2, error) {
	transaction := &attemptSettlementTransactionV2{Schema: attemptSettlementTransactionV2Schema, Kind: kind, Epoch: epoch, Snapshots: make([]attemptSettlementV2Snapshot, len(batch.ordered))}
	if closure != nil {
		encoded, err := json.Marshal(closure)
		if err != nil { return nil, err }
		transaction.ClosureJSON = append(encoded, '\n')
	}
	for index, participant := range batch.ordered {
		entry := attemptSettlementV2Snapshot{NoID: participant.NoID, StatsPath: filepath.Join(participant.StateDir, "stats.json")}
		original, exists, err := readAttemptSettlementV2Path(entry.StatsPath, 0, physical)
		if err != nil { return nil, err }
		if exists {
			originalStats, err := decodeAttemptSettlementV2Stats(original, batch.candidates[index].cfg)
			if err != nil { return nil, err }
			if err := validateAttemptSettlementV2Original(originalStats, batch.candidates[index]); err != nil { return nil, err }
			if kind == "advance" {
				if err := validateAttemptSettlementV2LedgerState(ctx, participant, originalStats, authority.Operators[participant.NoID]); err != nil { return nil, err }
			}
			entry.OriginalJSON, entry.OriginalHash = original, attemptSettlementV2ImageHash(original)
		}
		entry.PreImageJSON, err = encodeStatsSnapshot(batch.candidates[index].snapshotStats())
		if err != nil { return nil, err }
		if kind == "activate" && exists && !bytes.Equal(entry.OriginalJSON, entry.PreImageJSON) { return nil, errors.New("compact activation original checkpoint differs from its drained live image") }
		entry.StatsJSON, err = encodeStatsSnapshot(postimages[index].snapshotStats())
		if err != nil { return nil, err }
		entry.PreImageHash, entry.StatsHash = attemptSettlementV2ImageHash(entry.PreImageJSON), attemptSettlementV2ImageHash(entry.StatsJSON)
		transaction.Snapshots[index] = entry
	}
	return transaction, nil
}

// Canonical shape and every exact path/hash are checked before any stream is
// read. No journal-selected participant or output path can enter recovery.
func decodeAttemptSettlementTransactionV2(encoded []byte, participants []AttemptSettlementRuntimeV2Participant) (*attemptSettlementTransactionV2, []*StatsEngine, []*StatsEngine, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var transaction attemptSettlementTransactionV2
	if err := decoder.Decode(&transaction); err != nil { return nil, nil, nil, err }
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) { return nil, nil, nil, errors.New("compact transaction has trailing JSON") }
	canonical, err := canonicalAttemptSettlementTransactionV2(&transaction)
	if err != nil || !bytes.Equal(canonical, encoded) || transaction.Schema != attemptSettlementTransactionV2Schema || len(transaction.Snapshots) != len(participants) || transaction.Kind != "activate" && transaction.Kind != "advance" || transaction.Kind == "activate" && len(transaction.ClosureJSON) != 0 || transaction.Kind == "advance" && len(transaction.ClosureJSON) == 0 {
		return nil, nil, nil, errors.Join(errors.New("compact transaction shape or canonical encoding differs"), err)
	}
	preimages, postimages := make([]*StatsEngine, len(participants)), make([]*StatsEngine, len(participants))
	for index, entry := range transaction.Snapshots {
		participant := participants[index]
		if entry.NoID != participant.NoID || entry.StatsPath != filepath.Join(participant.StateDir, "stats.json") || len(entry.PreImageJSON) == 0 || len(entry.StatsJSON) == 0 || entry.PreImageHash != attemptSettlementV2ImageHash(entry.PreImageJSON) || entry.StatsHash != attemptSettlementV2ImageHash(entry.StatsJSON) || entry.OriginalHash != attemptSettlementV2ImageHash(entry.OriginalJSON) {
			return nil, nil, nil, errors.New("compact transaction participant path or exact image hash differs")
		}
		pre, err := decodeAttemptSettlementV2Stats(entry.PreImageJSON, participant.Stats.cfg)
		if err != nil { return nil, nil, nil, err }
		post, err := decodeAttemptSettlementV2Stats(entry.StatsJSON, participant.Stats.cfg)
		if err != nil { return nil, nil, nil, err }
		if post.attemptV2 == nil || !post.settlementEpochKnown || post.settlementEpoch != transaction.Epoch || len(post.window) != 0 || len(post.egress) != 0 || post.attemptLastAppliedSequence != pre.attemptLastAppliedSequence {
			return nil, nil, nil, errors.New("compact transaction postimage is not one empty exact successor")
		}
		if len(entry.OriginalJSON) != 0 {
			original, err := decodeAttemptSettlementV2Stats(entry.OriginalJSON, participant.Stats.cfg)
			if err != nil { return nil, nil, nil, err }
			if err := validateAttemptSettlementV2Original(original, pre); err != nil { return nil, nil, nil, err }
		}
		preimages[index], postimages[index] = pre, post
	}
	return &transaction, preimages, postimages, nil
}

// Neither a later native generation nor an arbitrary same-epoch Save is a
// recovery preimage. Test all participants before replacing even the first.
func checkAttemptSettlementV2DiskImages(transaction *attemptSettlementTransactionV2, participants []AttemptSettlementRuntimeV2Participant, physical attemptSettlementV2IO) error {
	for index, entry := range transaction.Snapshots {
		encoded, exists, err := readAttemptSettlementV2Path(entry.StatsPath, 0, physical)
		if err != nil { return err }
		if !exists {
			if len(entry.OriginalJSON) != 0 { return errors.New("compact transaction lost an existing statistics checkpoint") }
			continue
		}
		if _, err := decodeAttemptSettlementV2Stats(encoded, participants[index].Stats.cfg); err != nil { return err }
		digest := attemptSettlementV2ImageHash(encoded)
		if digest != entry.OriginalHash && digest != entry.PreImageHash && digest != entry.StatsHash {
			return fmt.Errorf("compact transaction no_id %d statistics are not an exact known image", entry.NoID)
		}
	}
	return nil
}

// Writes journal-owned snapshots, then the immutable closure, then coherently
// publishes the complete closed-gate generation before the removal callback.
// Any error leaves admission reserved; no rollback overwrites durable evidence.
func finishAttemptSettlementTransactionV2(ctx context.Context, coordinator string, transaction *attemptSettlementTransactionV2, batch *attemptSettlementV2Owners, postimages []*StatsEngine, authority AttemptSettlementV2Options, physical attemptSettlementV2IO, publish bool) error {
	if err := physical.checkRoots(); err != nil { return err }
	if err := checkAttemptSettlementV2DiskImages(transaction, batch.ordered, physical); err != nil { return err }
	if err := checkAttemptSettlementV2TransactionHeads(transaction, batch.ordered, postimages); err != nil { return err }
	for index, entry := range transaction.Snapshots {
		if err := ctx.Err(); err != nil { return err }
		if physical.step != nil {
			if err := physical.step(fmt.Sprintf("before-snapshot-%d", entry.NoID)); err != nil { return err }
		}
		if err := ctx.Err(); err != nil { return err }
		if err := physical.checkRoots(); err != nil { return err }
		if err := physical.writeSnapshot(physical.roots[filepath.Dir(entry.StatsPath)], filepath.Base(entry.StatsPath), entry.StatsJSON); err != nil { return err }
		if err := physical.checkRoots(); err != nil { return err }
		if err := checkAttemptSettlementV2TransactionHeads(transaction, batch.ordered, postimages); err != nil { return err }
		postimages[index].attemptLedger = batch.ordered[index].Ledger
		postimages[index].attemptCutPending, postimages[index].attemptSettlementCutPending, postimages[index].attemptSettlementCutEpoch = true, true, transaction.Epoch
	}
	if err := ctx.Err(); err != nil { return err }
	if transaction.Kind == "advance" {
		if err := publishAttemptSettlementClosureV2(coordinator, transaction.Epoch-1, transaction.ClosureJSON, authority.MaxClosureBytes, physical); err != nil { return err }
	}
	if err := checkAttemptSettlementV2TransactionHeads(transaction, batch.ordered, postimages); err != nil { return err }
	if err := physical.checkRoots(); err != nil { return err }
	if err := ctx.Err(); err != nil { return err }
	if publish {
		batch.candidates = postimages
		batch.publish()
	}
	if physical.step != nil {
		if err := physical.step("before-journal-removal"); err != nil { return err }
	}
	if err := ctx.Err(); err != nil { return err }
	if err := physical.checkRoots(); err != nil { return err }
	if err := physical.removeJournal(physical.roots[coordinator], filepath.Base(attemptSettlementTransactionV2Path(coordinator))); err != nil { return err }
	if err := physical.checkRoots(); err != nil { return err }
	return ctx.Err()
}
