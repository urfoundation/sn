//go:build linux || darwin

package validator

// Compact input journals are immutable bounded private records. Their schema
// and filenames are disjoint from v1 input history. The Stats write owner
// holds publication through durable rotation; restart replays real evidence.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// The prior private input-v2 schema contains legacy materialized cuts.
const releaseMeasurementInputV2Schema = "urnetwork-validator-release-measurement-input-v3"

// The enclosing authenticated runtime supplies this finite journal capacity;
// no default, envelope increase or additional object capacity is introduced.
type releaseMeasurementInputV2Options struct {
	Stats releaseStatsV2Options
	MaxJournalBytes uint64
}

// This decode-only wire cannot recursively allocate a legacy cut/transition.
// Raw providers and the compact header remain bounded by the complete bytes.
type releaseMeasurementInputV2Wire struct {
	Schema string `json:"schema"`
	DeploymentID string `json:"deployment_id"`
	ChainID uint64 `json:"chain_id"`
	GenesisHash string `json:"genesis_hash"`
	Coordinator string `json:"coordinator"`
	ValidatorID uint64 `json:"validator_id"`
	Netuid uint16 `json:"netuid"`
	SubnetEpoch uint64 `json:"subnet_epoch"`
	PolicyHash string `json:"policy_hash"`
	MeasurementInput releaseMeasurementV2WireInput `json:"measurement_input"`
}

// A distinct filename preserves existing v1 same-epoch input history without
// overwriting it or allowing a legacy decoder to reinterpret compact state.
func releaseMeasurementInputV2Path(stateDir string, subnetEpoch, noID uint64) string {
	return filepath.Join(stateDir, "measurements", "inputs", fmt.Sprintf("subnet-%020d-no-%020d-v2.json", subnetEpoch, noID))
}

// Bounds are checked before an open or length conversion. One extra byte
// proves EOF rather than accepting a truncated prefix at the declared limit.
func validateReleaseMeasurementInputV2Limit(limit uint64) error {
	if limit == 0 || limit >= uint64(^uint(0)>>1) || limit >= uint64(1<<63-1) { return errors.New("compact input journal byte bound is invalid") }
	return nil
}

// A private regular inode is checked before and after the bounded read. The
// returned bytes cannot hide a late Close error, replacement, or size change.
func readReleaseMeasurementInputV2(path string, limit uint64) (encoded []byte, resultErr error) {
	if err := validateReleaseMeasurementInputV2Limit(limit); err != nil { return nil, err }
	before, err := os.Lstat(path)
	if err != nil { return nil, err }
	if !before.Mode().IsRegular() || before.Mode().Perm()&0o077 != 0 || before.Size() < 0 || uint64(before.Size()) > limit { return nil, errors.New("compact input journal is not a bounded private regular file") }
	file, err := os.Open(path)
	if err != nil { return nil, err }
	defer func() { resultErr = errors.Join(resultErr, file.Close()); if resultErr != nil { encoded = nil } }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) { return nil, errors.Join(errors.New("compact input journal changed before its read"), err) }
	encoded, err = io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil { return nil, err }
	if uint64(len(encoded)) > limit { return nil, errors.New("compact input journal exceeds its byte bound") }
	after, err := file.Stat()
	if err != nil || after.Size() != before.Size() || int64(len(encoded)) != before.Size() { return nil, errors.Join(errors.New("compact input journal changed during its read"), err) }
	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(before, current) { return nil, errors.Join(errors.New("compact input journal pathname changed during its read"), err) }
	return encoded, nil
}

// Link publishes a fully synced temporary inode without replacing an existing
// journal. Concurrent callers may acknowledge only identical committed bytes;
// parent-directory Sync and every owned Close/cleanup result are retained.
func writeReleaseMeasurementInputV2(path string, encoded []byte, limit uint64) (resultErr error) {
	if err := validateReleaseMeasurementInputV2Limit(limit); err != nil { return err }
	if len(encoded) == 0 || uint64(len(encoded)) > limit { return errors.New("compact input journal exceeds its byte bound") }
	if existing, err := readReleaseMeasurementInputV2(path, limit); err == nil {
		if !bytes.Equal(existing, encoded) { return errors.New("compact input journal already names different immutable bytes") }
		return syncReleaseMeasurementInputV2Directory(path)
	} else if !errors.Is(err, os.ErrNotExist) { return err }
	dir := filepath.Dir(path)
	if err := ensurePrivateStateDir(dir); err != nil { return err }
	file, err := os.CreateTemp(dir, ".compact-input-")
	if err != nil { return err }
	temporary, closed := file.Name(), false
	defer func() {
		if !closed { resultErr = errors.Join(resultErr, file.Close()) }
		if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) { resultErr = errors.Join(resultErr, err) }
	}()
	if err := file.Chmod(0o600); err != nil { return err }
	if _, err := file.Write(encoded); err != nil { return err }
	if err := file.Sync(); err != nil { return err }
	closed = true
	if err := file.Close(); err != nil { return err }
	if err := os.Link(temporary, path); err != nil {
		if !errors.Is(err, os.ErrExist) { return err }
		existing, readErr := readReleaseMeasurementInputV2(path, limit)
		if readErr != nil { return errors.Join(err, readErr) }
		if !bytes.Equal(existing, encoded) { return errors.New("compact input journal publication raced different bytes") }
	}
	if err := os.Remove(temporary); err != nil { return err }
	return syncReleaseMeasurementInputV2Directory(path)
}

// Retry also syncs an already-visible identical journal: a prior link may
// have succeeded before its directory sync failed, so visibility is not a
// durable acknowledgement by itself.
func syncReleaseMeasurementInputV2Directory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil { return err }
	return errors.Join(directory.Sync(), directory.Close())
}

// Config and authenticated activation are independent authorities. Neither
// the outer journal labels nor a valid signature under another local ledger
// may select the deployment, operator, coordinator, vault or policy.
func validateReleaseMeasurementInputV2Context(cfg *ReleaseConfig, noID uint64, expected AttemptCutV2Context) error {
	identity, domain := expected.Identity, expected.Activation.Domain
	if identity.DeploymentID != cfg.DeploymentID || identity.ChainID != cfg.ChainID || !strings.EqualFold(identity.GenesisHash, cfg.GenesisHash) || identity.ValidatorID != cfg.ValidatorID || identity.Netuid != cfg.Netuid || identity.NoID != noID {
		return errors.New("compact input cut differs from configured ledger identity")
	}
	policyHash, err := parseReleaseHex32("compact input configured policy", cfg.PolicyHash, false)
	if err != nil { return err }
	if !common.IsHexAddress(cfg.Coordinator) || !common.IsHexAddress(cfg.SettlementVault) || domain.Coordinator != [20]byte(common.HexToAddress(cfg.Coordinator)) || domain.SettlementVault != [20]byte(common.HexToAddress(cfg.SettlementVault)) || domain.PolicyHash != policyHash {
		return errors.New("compact input cut differs from configured activation domain")
	}
	return nil
}

// Complete canonical decoding uses a nonrecursive compact wire. Structural
// admission is not an accepted cut: the Stats owner still verifies the signed
// context and fully replays the referenced record/proof streams on recovery.
func decodeReleaseMeasurementInputV2(ctx context.Context, encoded []byte, options releaseMeasurementInputV2Options) (*releaseMeasurementInputJournal, error) {
	if ctx == nil { return nil, errors.New("compact input journal context is nil") }
	if err := ctx.Err(); err != nil { return nil, err }
	if err := validateReleaseMeasurementInputV2Limit(options.MaxJournalBytes); err != nil { return nil, err }
	if len(encoded) == 0 || uint64(len(encoded)) > options.MaxJournalBytes { return nil, errors.New("compact input journal exceeds its byte bound") }
	var wire releaseMeasurementInputV2Wire
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil { return nil, err }
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) { return nil, errors.Join(errors.New("compact input journal contains trailing JSON"), err) }
	input := ReleaseMeasurementInput{
		NoID: wire.MeasurementInput.NoID, SettlementEpoch: wire.MeasurementInput.SettlementEpoch,
		CutNativeBlock: wire.MeasurementInput.CutNativeBlock, CutNativeBlockHash: wire.MeasurementInput.CutNativeBlockHash,
		CutEVMSnapshotBlock: wire.MeasurementInput.CutEVMSnapshotBlock, CutEVMSnapshotHash: wire.MeasurementInput.CutEVMSnapshotHash,
		EgressGeneration: wire.MeasurementInput.EgressGeneration, AttemptCutV2: wire.MeasurementInput.AttemptCutV2,
		Stats: ReleaseStatsMeasurement{Config: wire.MeasurementInput.Stats.Config, Providers: wire.MeasurementInput.Stats.Providers},
	}
	journal := &releaseMeasurementInputJournal{
		Schema: wire.Schema, DeploymentID: wire.DeploymentID, ChainID: wire.ChainID, GenesisHash: wire.GenesisHash,
		Coordinator: wire.Coordinator, ValidatorID: wire.ValidatorID, Netuid: wire.Netuid, SubnetEpoch: wire.SubnetEpoch,
		PolicyHash: wire.PolicyHash, MeasurementInput: input,
	}
	if journal.Schema != releaseMeasurementInputV2Schema || input.NoID == 0 || input.CutNativeBlock == 0 || input.CutEVMSnapshotBlock == 0 || input.AttemptCutV2 == nil { return nil, errors.New("compact input journal identity is incomplete") }
	if _, err := parseReleaseHex32("compact input native hash", input.CutNativeBlockHash, false); err != nil { return nil, err }
	if _, err := parseReleaseHex32("compact input EVM hash", input.CutEVMSnapshotHash, false); err != nil { return nil, err }
	if err := input.AttemptCutV2.Validate(options.Stats.Bounds); err != nil { return nil, err }
	if _, err := newAttemptCutV2StatsProjection(ctx, input.Stats, input.AttemptCutV2.Context, options.Stats.Policy, options.Stats.Stats, nil); err != nil { return nil, err }
	canonical, err := canonicalReleaseMeasurementInputBytes(journal)
	if err != nil { return nil, err }
	if !bytes.Equal(encoded, canonical) { return nil, errors.New("compact input journal bytes are not canonical") }
	return journal, nil
}

// Root routing supplies authenticated v2 options. Existing journals bind the
// same decision or an earlier finalized snapshot within it, while their signed
// egress context is checked against real Stats/ledger ownership on replay.
func (self *ReleaseSteerer) loadOrDetachReleaseMeasurementInputV2(ctx context.Context, noID, subnetEpoch, nativeBlock uint64, nativeHash string, snapshot *ReleaseSnapshot, options releaseMeasurementInputV2Options) (ReleaseMeasurementInput, error) {
	if ctx == nil || self == nil || self.cfg == nil || snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() { return ReleaseMeasurementInput{}, errors.New("compact input decision context is incomplete") }
	if err := ctx.Err(); err != nil { return ReleaseMeasurementInput{}, err }
	if err := validateReleaseMeasurementInputV2Limit(options.MaxJournalBytes); err != nil { return ReleaseMeasurementInput{}, err }
	if !filepath.IsAbs(self.cfg.StateDir) || filepath.Clean(self.cfg.StateDir) != self.cfg.StateDir || filepath.Dir(self.cfg.StateDir) == self.cfg.StateDir { return ReleaseMeasurementInput{}, errors.New("compact input state directory is invalid") }
	measurement := self.contexts[noID]
	if noID == 0 || measurement == nil || measurement.Stats == nil || nativeBlock == 0 || snapshot.BlockNumber == 0 { return ReleaseMeasurementInput{}, errors.New("compact input operator context is absent") }
	if _, err := parseReleaseHex32("compact input decision native hash", nativeHash, false); err != nil { return ReleaseMeasurementInput{}, err }
	path := releaseMeasurementInputV2Path(self.cfg.StateDir, subnetEpoch, noID)
	encoded, err := readReleaseMeasurementInputV2(path, options.MaxJournalBytes)
	if err == nil {
		journal, err := decodeReleaseMeasurementInputV2(ctx, encoded, options)
		if err != nil { return ReleaseMeasurementInput{}, err }
		input := journal.MeasurementInput
		if journal.DeploymentID != self.cfg.DeploymentID || journal.ChainID != self.cfg.ChainID || !strings.EqualFold(journal.GenesisHash, self.cfg.GenesisHash) || !strings.EqualFold(journal.Coordinator, self.cfg.Coordinator) || journal.ValidatorID != self.cfg.ValidatorID || journal.Netuid != self.cfg.Netuid || journal.SubnetEpoch != subnetEpoch || !strings.EqualFold(journal.PolicyHash, self.cfg.PolicyHash) || input.NoID != noID || input.SettlementEpoch != snapshot.Epoch.Uint64() || !releaseBlockAtOrBefore(input.CutNativeBlock, input.CutNativeBlockHash, nativeBlock, nativeHash) || !releaseBlockAtOrBefore(input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash, snapshot.BlockNumber, releaseHex32(snapshot.BlockHash)) {
			return ReleaseMeasurementInput{}, errors.New("compact input journal differs from the active decision")
		}
		cut := input.AttemptCutV2
		if cut.Context.Identity.NoID != noID || cut.Context.Boundary.SettlementEpoch != input.SettlementEpoch || cut.Context.Boundary.EVMBlock != input.CutEVMSnapshotBlock || cut.Context.Boundary.EVMBlockHash != input.CutEVMSnapshotHash || cut.Context.EgressGeneration != input.EgressGeneration { return ReleaseMeasurementInput{}, errors.New("compact input labels differ from its signed cut") }
		if err := validateReleaseMeasurementInputV2Context(self.cfg, noID, cut.Context); err != nil { return ReleaseMeasurementInput{}, err }
		if err := syncReleaseMeasurementInputV2Directory(path); err != nil { return ReleaseMeasurementInput{}, err }
		if err := measurement.Stats.reconcileReleaseStatsCutV2(ctx, measurementStateDir(self.cfg, noID), input.Stats, *cut, options.Stats); err != nil { return ReleaseMeasurementInput{}, fmt.Errorf("reconcile compact input: %w", err) }
		return input, nil
	}
	if !errors.Is(err, os.ErrNotExist) { return ReleaseMeasurementInput{}, err }
	input := ReleaseMeasurementInput{NoID: noID, SettlementEpoch: snapshot.Epoch.Uint64(), CutNativeBlock: nativeBlock, CutNativeBlockHash: nativeHash, CutEVMSnapshotBlock: snapshot.BlockNumber, CutEVMSnapshotHash: releaseHex32(snapshot.BlockHash)}
	boundary := AttemptBoundary{SettlementEpoch: input.SettlementEpoch, EVMBlock: snapshot.BlockNumber, EVMBlockHash: input.CutEVMSnapshotHash}
	_, _, err = measurement.Stats.detachReleaseStatsMeasurementV2(ctx, measurementStateDir(self.cfg, noID), boundary, options.Stats, func(stats ReleaseStatsMeasurement, cut AttemptCutV2) error {
		if err := validateReleaseMeasurementInputV2Context(self.cfg, noID, cut.Context); err != nil { return err }
		input.Stats, input.EgressGeneration, input.AttemptCutV2 = stats, cut.Context.EgressGeneration, &cut
		journal := &releaseMeasurementInputJournal{Schema: releaseMeasurementInputV2Schema, DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: strings.ToLower(self.cfg.GenesisHash), Coordinator: self.cfg.Coordinator, ValidatorID: self.cfg.ValidatorID, Netuid: self.cfg.Netuid, SubnetEpoch: subnetEpoch, PolicyHash: strings.ToLower(self.cfg.PolicyHash), MeasurementInput: input}
		encoded, err := canonicalReleaseMeasurementInputBytes(journal)
		if err != nil { return err }
		if uint64(len(encoded)) > options.MaxJournalBytes { return errors.New("compact input journal exceeds its byte bound") }
		return writeReleaseMeasurementInputV2(path, encoded, options.MaxJournalBytes)
	})
	if err != nil { return ReleaseMeasurementInput{}, err }
	return input, nil
}
