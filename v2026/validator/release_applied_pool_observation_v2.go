//go:build linux || darwin

// Project an independently selected applied receipt and public vector through
// the normal retained-history custody reader. This is not chain acceptance.
package validator

import (
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"time"
)

// Every selection is independently named; a newer current intent does not
// change which retained application is being observed.
type AppliedPoolObservationV2Query struct {
	NativeEpoch            uint64
	MinimumSettlementEpoch uint64
	MeasurementHash        string
	MeasurementBytes       uint64
	VectorHash             string
	PoolNoId               uint64
	PoolHotkey             [32]byte
}

// Discovery starts from independently selected public progress identity. Its
// result must be pinned before the strict vector projection is requested.
type AppliedPoolReferenceV2Query struct {
	NativeEpoch            uint64
	MinimumSettlementEpoch uint64
	VectorHash             string
	PoolNoId               uint64
	PoolHotkey             [32]byte
}

// The first phase exports an authenticated content reference, not the vector
// or a finality verdict. No operator manually reads or hashes private state.
type AppliedPoolReferenceV2Observation struct {
	Schema                             string                     `json:"schema"`
	Scope                              string                     `json:"scope"`
	SourceStartedAt                    time.Time                  `json:"source_started_at_utc"`
	SourceCompletedAt                  time.Time                  `json:"source_completed_at_utc"`
	ConfigSha256                       string                     `json:"config_sha256"`
	HandoffSha256                      string                     `json:"handoff_sha256"`
	DeploymentId                       string                     `json:"deployment_id"`
	ChainId                            uint64                     `json:"chain_id"`
	GenesisHash                        string                     `json:"genesis_hash"`
	ValidatorId                        uint64                     `json:"validator_id"`
	ValidatorHotkey                    string                     `json:"validator_hotkey"`
	Netuid                             uint16                     `json:"netuid"`
	Receipt                            ProvisionalIntentReceiptV2 `json:"receipt"`
	MeasurementHash                    string                     `json:"measurement_hash"`
	MeasurementBytes                   uint64                     `json:"measurement_bytes"`
	PoolNoId                           uint64                     `json:"pool_no_id"`
	PoolHotkey                         string                     `json:"pool_hotkey"`
	ReferenceMustBeIndependentlyPinned bool                       `json:"reference_must_be_independently_pinned"`
	NativeChainAcceptance              bool                       `json:"native_chain_acceptance"`
}

// The same normal custody path performs discovery; the reference-only return
// type prevents a missing independent measurement pin from becoming proof.
func DiscoverConfiguredAppliedPoolReferenceV2(ctx context.Context, options ProvisionalConfiguredIntentObservationV2Options, query AppliedPoolReferenceV2Query) (*AppliedPoolReferenceV2Observation, error) {
	selected, err := readConfiguredAppliedPoolV2(ctx, options, AppliedPoolObservationV2Query{NativeEpoch: query.NativeEpoch,
		MinimumSettlementEpoch: query.MinimumSettlementEpoch, VectorHash: query.VectorHash, PoolNoId: query.PoolNoId, PoolHotkey: query.PoolHotkey}, releaseMeasurementInputV2ReadHooks{}, true)
	if err != nil {
		return nil, err
	}
	return &AppliedPoolReferenceV2Observation{Schema: "urnetwork-public-applied-pool-reference-discovery-v1", Scope: "retained-local-reference-discovery",
		SourceStartedAt: selected.SourceStartedAt, SourceCompletedAt: selected.SourceCompletedAt, ConfigSha256: selected.ConfigSha256, HandoffSha256: selected.HandoffSha256,
		DeploymentId: selected.DeploymentId, ChainId: selected.ChainId, GenesisHash: selected.GenesisHash, ValidatorId: selected.ValidatorId,
		ValidatorHotkey: selected.ValidatorHotkey, Netuid: selected.Netuid, Receipt: selected.Receipt, MeasurementHash: selected.MeasurementHash,
		MeasurementBytes: selected.MeasurementBytes, PoolNoId: selected.PoolNoId, PoolHotkey: selected.PoolHotkey, ReferenceMustBeIndependentlyPinned: true}, nil
}

// Only public receipt/vector/measurement fields leave custody. In particular,
// no prepared transaction, encrypted payload, signing key or raw journal does.
type AppliedPoolObservationV2 struct {
	Schema                          string                     `json:"schema"`
	Scope                           string                     `json:"scope"`
	SourceStartedAt                 time.Time                  `json:"source_started_at_utc"`
	SourceCompletedAt               time.Time                  `json:"source_completed_at_utc"`
	ConfigSha256                    string                     `json:"config_sha256"`
	HandoffSha256                   string                     `json:"handoff_sha256"`
	DeploymentId                    string                     `json:"deployment_id"`
	ChainId                         uint64                     `json:"chain_id"`
	GenesisHash                     string                     `json:"genesis_hash"`
	ValidatorId                     uint64                     `json:"validator_id"`
	ValidatorHotkey                 string                     `json:"validator_hotkey"`
	Netuid                          uint16                     `json:"netuid"`
	ValidatorUid                    uint16                     `json:"validator_uid"`
	Receipt                         ProvisionalIntentReceiptV2 `json:"receipt"`
	MeasurementHash                 string                     `json:"measurement_hash"`
	MeasurementBytes                uint64                     `json:"measurement_bytes"`
	PolicyHash                      string                     `json:"policy_hash"`
	NativeSnapshotBlock             uint64                     `json:"native_snapshot_block"`
	NativeSnapshotHash              string                     `json:"native_snapshot_hash"`
	EvmSnapshotBlock                uint64                     `json:"evm_snapshot_block"`
	EvmSnapshotHash                 string                     `json:"evm_snapshot_hash"`
	PoolNoId                        uint64                     `json:"pool_no_id"`
	PoolHotkey                      string                     `json:"pool_hotkey"`
	DecisionPoolUid                 uint16                     `json:"decision_pool_uid"`
	Uids                            []uint16                   `json:"uids"`
	Values                          []uint16                   `json:"values"`
	NormalHistoryVerified           bool                       `json:"normal_history_verified"`
	MeasurementContextVerified      bool                       `json:"measurement_context_verified"`
	FullMathematicalReplayPerformed bool                       `json:"full_mathematical_replay_performed"`
	NativeChainAcceptance           bool                       `json:"native_chain_acceptance"`
	PrivateFieldsEmitted            bool                       `json:"private_fields_emitted"`
	SignersConstructed              bool                       `json:"signers_constructed"`
	StateWrites                     bool                       `json:"state_writes"`
}

// The existing managed observer authenticates the complete configured history.
// A second descriptor-owned read projects only the exact selected public data;
// both reads must agree on the public receipt before any result is returned.
func ObserveConfiguredAppliedPoolV2(ctx context.Context, options ProvisionalConfiguredIntentObservationV2Options, query AppliedPoolObservationV2Query) (*AppliedPoolObservationV2, error) {
	return observeConfiguredAppliedPoolV2(ctx, options, query, releaseMeasurementInputV2ReadHooks{})
}

// Descriptor fault controls remain private; the public path always uses the
// normal custody implementation without an injectable source or validator.
func observeConfiguredAppliedPoolV2(ctx context.Context, options ProvisionalConfiguredIntentObservationV2Options, query AppliedPoolObservationV2Query, hooks releaseMeasurementInputV2ReadHooks) (result *AppliedPoolObservationV2, resultErr error) {
	return readConfiguredAppliedPoolV2(ctx, options, query, hooks, false)
}

// Discovery is private to the reference-only entry point. Every public vector
// projection still requires independently named measurement hash and length.
func readConfiguredAppliedPoolV2(ctx context.Context, options ProvisionalConfiguredIntentObservationV2Options, query AppliedPoolObservationV2Query, hooks releaseMeasurementInputV2ReadHooks, discovery bool) (result *AppliedPoolObservationV2, resultErr error) {
	started := time.Now().UTC()
	if ctx == nil || query.NativeEpoch == 0 || query.MinimumSettlementEpoch == 0 || !discovery && query.MeasurementBytes == 0 || query.PoolNoId == 0 || query.PoolHotkey == ([32]byte{}) {
		return nil, errors.New("applied pool observation has no independent finite selection")
	}
	if !discovery {
		if _, err := parseReleaseContentHash(query.MeasurementHash); err != nil {
			return nil, err
		}
	}
	if _, err := canonicalAttemptHex32("applied pool vector hash", query.VectorHash, false); err != nil {
		return nil, err
	}
	observed, err := ObserveProvisionalConfiguredIntentsV2(ctx, options)
	if err != nil || observed == nil || observed.State != "observed" || observed.FinalAcceptance {
		return nil, errors.Join(errors.New("normal configured intent observation did not complete"), err)
	}
	var receipt *ProvisionalIntentReceiptV2
	for index := range observed.Receipts {
		candidate := &observed.Receipts[index]
		if candidate.SubnetEpoch == query.NativeEpoch {
			if receipt != nil {
				return nil, errors.New("selected applied native epoch repeats")
			}
			receipt = candidate
		}
	}
	if receipt == nil || receipt.Status != "applied" || receipt.SettlementEpoch < query.MinimumSettlementEpoch || receipt.VectorHash != query.VectorHash {
		return nil, errors.New("selected receipt is not the named applied settlement vector")
	}
	configBytes, err := ReadReleaseEvidenceV2File(ctx, options.Config, ProvisionalActivationSetupV2MaximumBytes)
	if err != nil {
		return nil, err
	}
	cfg, err := decodeReleaseConfigBytes(options.Config.Path, configBytes)
	if err != nil {
		return nil, err
	}
	if cfg.DeploymentID != options.DeploymentID || cfg.ValidatorID != options.ValidatorID || cfg.Netuid != options.Netuid || cfg.StateDir != options.StateDir || query.MeasurementBytes > cfg.EvidenceV2.Bounds.MaxArtifactBytes {
		return nil, errors.New("selected applied pool source differs from its managed configuration")
	}
	limit := cfg.EvidenceV2.Bounds.IntentFileLimit()
	custody := &releaseEvidenceV2StartupReferences{remaining: cfg.EvidenceV2.Bounds.MaxHistoryBytes, readHooks: hooks}
	defer func() {
		resultErr = errors.Join(resultErr, custody.check(), custody.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		} else {
			result.SourceCompletedAt = time.Now().UTC()
		}
	}()
	encoded, err := custody.read(ctx, filepath.Join(cfg.StateDir, "steering-intents.json"), limit, false)
	if err != nil {
		return nil, err
	}
	checkPublication := func() error {
		for _, name := range []string{releaseIntentV2Marker, releaseIntentV2Candidate} {
			_, err := custody.owners[0].directory.stat(name)
			if !releaseMeasurementInputV2OnlyMissing(err) {
				return errors.Join(errors.New("applied pool intent publication is unresolved"), err)
			}
		}
		return nil
	}
	if err := checkPublication(); err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, checkPublication()) }()
	all, _, retained, err := ReadSteeringIntentHistory(ctx, encoded, limit, func(name string, maximum uint64) ([]byte, error) {
		return custody.read(ctx, filepath.Join(cfg.StateDir, filepath.FromSlash(name)), maximum, false)
	})
	if err != nil {
		return nil, err
	}
	var selected *SteeringIntent
	selectedIndex := 0
	for index := range all {
		if all[index].SubnetEpoch == query.NativeEpoch {
			if selected != nil {
				return nil, errors.New("selected applied intent repeats in retained custody")
			}
			selected = &all[index]
			selectedIndex = index
		}
	}
	if selected == nil || selected.Status != "applied" || selected.VectorHash != query.VectorHash || !discovery && (selected.MeasurementArtifactHash != query.MeasurementHash || selected.MeasurementArtifactSize != query.MeasurementBytes) {
		return nil, errors.New("retained applied intent differs from its independent selection")
	}
	if discovery {
		query.MeasurementHash, query.MeasurementBytes = selected.MeasurementArtifactHash, selected.MeasurementArtifactSize
		if query.MeasurementBytes == 0 || query.MeasurementBytes > cfg.EvidenceV2.Bounds.MaxArtifactBytes {
			return nil, errors.New("discovered measurement reference exceeds its configured bound")
		}
	}
	if err := validateSteeringIntentLifecycle(selected, selectedIndex < retained); err != nil {
		return nil, err
	}
	if err := selected.VerifyVectorHash(); err != nil {
		return nil, err
	}
	actualReceipt := ProvisionalIntentReceiptV2{Status: selected.Status, SubnetEpoch: selected.SubnetEpoch, SettlementEpoch: selected.SettlementEpoch,
		VectorHash: selected.VectorHash, PreparedExtrinsicHash: selected.Prepared.ExtrinsicHash, PreparedAtBlock: selected.Prepared.PreparedAtBlock, PreparedAtBlockHash: selected.Prepared.PreparedAtBlockHash,
		UpdatedAt: selected.UpdatedAt, ExtrinsicHash: selected.ExtrinsicHash, FinalizedBlock: selected.FinalizedBlock, FinalizedBlockHash: selected.FinalizedBlockHash,
		RevealBlock: selected.RevealBlock, ApplicationBlock: selected.ApplicationBlock, ApplicationBlockHash: selected.ApplicationBlockHash}
	if !reflect.DeepEqual(actualReceipt, *receipt) || selected.ValidatorID != cfg.ValidatorID || selected.Netuid != cfg.Netuid || selected.Prepared.HotkeyHex != releaseHex32(options.Hotkey) || len(selected.UIDs) == 0 || len(selected.UIDs) != len(selected.Values) {
		return nil, errors.New("managed applied receipt changed or lost its complete public vector")
	}
	hash, err := parseReleaseContentHash(query.MeasurementHash)
	if err != nil {
		return nil, err
	}
	name := filepath.ToSlash(filepath.Join("measurements", hex.EncodeToString(hash[:])+".json"))
	if selected.MeasurementArtifactPath != name {
		return nil, errors.New("applied pool measurement path is not its exact content address")
	}
	measurement, err := custody.read(ctx, filepath.Join(cfg.StateDir, filepath.FromSlash(name)), query.MeasurementBytes, false)
	if err != nil || uint64(len(measurement)) != query.MeasurementBytes || ReleaseMeasurementContentHash(measurement) != query.MeasurementHash {
		return nil, errors.Join(errors.New("applied pool public measurement differs from its pin"), err)
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, cfg.EvidenceV2.Bounds.MaxArtifactBytes, cfg.EvidenceV2.Bounds.MaxOperators)
	if err != nil {
		return nil, err
	}
	if artifact.DeploymentID != cfg.DeploymentID || artifact.ChainID != cfg.ChainID || artifact.GenesisHash != cfg.GenesisHash || !releaseAddressIdentityMatches(artifact.Coordinator, cfg.Coordinator) || !releaseAddressIdentityMatches(artifact.SettlementVault, cfg.SettlementVault) || artifact.ValidatorID != cfg.ValidatorID || artifact.Netuid != cfg.Netuid || artifact.SelfUID != selected.SelfUID || artifact.SubnetEpoch != selected.SubnetEpoch || artifact.SettlementEpoch != selected.SettlementEpoch || artifact.PolicyHash != selected.PolicyHash || artifact.NativeSnapshotBlock != selected.NativeSnapshotBlock || artifact.NativeSnapshotHash != selected.NativeSnapshotHash || artifact.EVMSnapshotBlock != selected.EVMSnapshotBlock || artifact.EVMSnapshotHash != selected.EVMSnapshotHash {
		return nil, errors.New("applied pool typed measurement context differs from its retained intent")
	}
	var pool *ReleasePoolMeasurement
	for index := range artifact.Pools {
		candidate := &artifact.Pools[index]
		if candidate.NoID == query.PoolNoId {
			if pool != nil {
				return nil, errors.New("applied pool measurement repeats the selected operator")
			}
			pool = candidate
		}
	}
	if pool == nil || pool.PoolHotkey != releaseHex32(query.PoolHotkey) {
		return nil, errors.New("applied pool decision does not name the immutable selected hotkey")
	}
	return &AppliedPoolObservationV2{Schema: "urnetwork-public-applied-pool-vector-observation-v1", Scope: "retained-local-receipt-and-vector", SourceStartedAt: started,
		ConfigSha256: options.Config.SHA256, HandoffSha256: options.HandoffSHA256, DeploymentId: cfg.DeploymentID, ChainId: cfg.ChainID, GenesisHash: cfg.GenesisHash,
		ValidatorId: cfg.ValidatorID, ValidatorHotkey: releaseHex32(options.Hotkey), Netuid: cfg.Netuid, ValidatorUid: selected.SelfUID, Receipt: actualReceipt,
		MeasurementHash: query.MeasurementHash, MeasurementBytes: query.MeasurementBytes, PolicyHash: selected.PolicyHash,
		NativeSnapshotBlock: selected.NativeSnapshotBlock, NativeSnapshotHash: selected.NativeSnapshotHash, EvmSnapshotBlock: selected.EVMSnapshotBlock, EvmSnapshotHash: selected.EVMSnapshotHash,
		PoolNoId: query.PoolNoId, PoolHotkey: pool.PoolHotkey, DecisionPoolUid: pool.UID, Uids: slices.Clone(selected.UIDs), Values: slices.Clone(selected.Values),
		NormalHistoryVerified: true, MeasurementContextVerified: true}, nil
}
