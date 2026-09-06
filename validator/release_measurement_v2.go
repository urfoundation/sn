//go:build linux || darwin

package validator

// Compact artifacts keep the source observations and exact scoring math of
// release measurements, but authenticate attempts through bounded typed streams.
// This is a public wire verifier, not a producer activation or a chain observer.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// This version cannot be decoded or verified through a legacy entrypoint.
const ReleaseMeasurementSchemaV2 = "urnetwork-validator-release-measurement-v2"

// The compact wire and its live control owners share the existing signed
// envelope ceiling; this alias is not a new default or an independent budget.
const maxReleaseMeasurementArtifactBytes = releaseMeasurementEnvelopeMaxArtifactSize

// These pins come from the caller's authenticated release decision, never from
// candidate headers. Earlier activation and current/historical native authority
// must already be established by that caller; a signature alone is not a chain
// anchor. PreviousArtifactHash names the exact prior transcript, when present.
type ReleaseMeasurementV2Decision struct {
	DeploymentID         string
	ChainID              uint64
	GenesisHash          string
	Coordinator          string
	SettlementVault      string
	ValidatorID          uint64
	Netuid               uint16
	SubnetEpoch          uint64
	NativeSnapshotBlock  uint64
	NativeSnapshotHash   string
	EVMSnapshotBlock     uint64
	EVMSnapshotHash      string
	SettlementEpoch      uint64
	PolicyHash           string
	PreviousArtifactHash string
	SelfUID              uint16
}

// The native cut labels are separate from the signed egress cursor. Both are
// pinned independently. Replay scratch is private to this one invocation.
type ReleaseMeasurementV2OperatorOptions struct {
	Expected           AttemptCutV2Context
	CutNativeBlock     uint64
	CutNativeBlockHash string
	Bounds             AttemptCutV2Bounds
	Measurement        AttemptCutV2MeasurementOptions
}

// Observations are the exact independently authenticated coordinator/native
// and deposit facts. CurrentBindingKVs must also match their common eligibility
// projection. Complete operator membership and finite bounds are caller policy.
// MaxControlBytes bounds detached object storage before copying; MaxArtifactBytes
// bounds the canonical wire. Neither may exceed the existing envelope ceiling.
// Settlement supplies the independent previous-window authority whenever the
// artifact carries a closure; its scratch owners must differ from current ones.
type ReleaseMeasurementV2Options struct {
	Expected         ReleaseMeasurementV2Decision
	Policy           protocol.Policy
	ControlledNOIDs  []uint64
	Bindings         []ReleaseBindingMeasurement
	Pools            []ReleasePoolMeasurement
	DepositAudits    []DepositAudit
	Operators        map[uint64]ReleaseMeasurementV2OperatorOptions
	Settlement       *AttemptSettlementV2Options
	MaxOperators     uint64
	MaxHeadEntries   uint64
	MaxArtifactBytes uint64
	MaxControlBytes  uint64
}

// Decision and replay census are published atomically. Prior EMA migration,
// complete chain ancestry and cross-settlement terminal-ID persistence remain
// outer obligations, just as they do for a standalone legacy artifact.
type VerifiedReleaseMeasurementV2 struct {
	Decision             *VerifiedReleaseMeasurement
	ReplayByNO           map[uint64]AttemptCutV2ReplayResult
	SettlementReplayByNO map[uint64]AttemptCutV2ReplayResult
}

// This value projection deliberately contains no candidate-derived authority.
func releaseMeasurementV2Decision(artifact *ReleaseMeasurementArtifact) ReleaseMeasurementV2Decision {
	return ReleaseMeasurementV2Decision{
		DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID,
		GenesisHash: artifact.GenesisHash, Coordinator: artifact.Coordinator,
		SettlementVault: artifact.SettlementVault, ValidatorID: artifact.ValidatorID,
		Netuid: artifact.Netuid, SubnetEpoch: artifact.SubnetEpoch,
		NativeSnapshotBlock: artifact.NativeSnapshotBlock, NativeSnapshotHash: artifact.NativeSnapshotHash,
		EVMSnapshotBlock: artifact.EVMSnapshotBlock, EVMSnapshotHash: artifact.EVMSnapshotHash,
		SettlementEpoch: artifact.SettlementEpoch, PolicyHash: artifact.PolicyHash,
		PreviousArtifactHash: artifact.PreviousArtifactHash, SelfUID: artifact.SelfUID,
	}
}

// Count owned control storage without marshaling, normalizing strings or
// following an unbounded candidate graph. Only the acyclic admitted artifact
// shape reaches this function (all ordinary/terminal legacy pointers nil).
// Strings are charged even when shared; no history map or pointer cache exists.
func releaseMeasurementV2ControlStorage(ctx context.Context, value reflect.Value, remaining *uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	charge := func(count, size uint64) error {
		if size != 0 && count > *remaining/size {
			return errors.New("compact measurement control storage exceeds its bound")
		}
		*remaining -= count * size
		return nil
	}
	switch value.Kind() {
	case reflect.String:
		return charge(uint64(value.Len()), 1)
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		if err := charge(1, uint64(value.Type().Elem().Size())); err != nil {
			return err
		}
		return releaseMeasurementV2ControlStorage(ctx, value.Elem(), remaining)
	case reflect.Slice:
		if err := charge(uint64(value.Len()), uint64(value.Type().Elem().Size())); err != nil {
			return err
		}
		for index := 0; index < value.Len(); index++ {
			if err := releaseMeasurementV2ControlStorage(ctx, value.Index(index), remaining); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if err := releaseMeasurementV2ControlStorage(ctx, value.Field(index), remaining); err != nil {
				return err
			}
		}
	case reflect.Array:
		// All array fields in this wire shape are fixed byte identities. Array
		// storage is already charged in its containing struct or slice element.
		if value.Type().Elem().Kind() != reflect.Uint8 {
			return errors.New("compact measurement has an unsupported control array")
		}
	case reflect.Bool, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Int:
	default:
		return errors.New("compact measurement has an unsupported control field")
	}
	return nil
}

// Fixed-width raw input admission precedes copying and generic normalization.
// Actual semantic counters, histogram totals and quality are checked once by
// the joint projection, followed by the complete signed record/proof replay.
func admitReleaseMeasurementV2ProviderCensus(ctx context.Context, input ReleaseMeasurementInput, options ReleaseMeasurementV2OperatorOptions) (map[connect.Id]bool, error) {
	measurement := options.Measurement
	if input.AttemptCutV2 == nil || input.Stats.AttemptCut != nil || input.Stats.SettlementTransition != nil || measurement.Replay.VisitRecord != nil {
		return nil, errors.New("compact measurement contains missing or competing attempt authority")
	}
	if measurement.MaxProviders == 0 || measurement.MaxEgressHashes == 0 || measurement.MaxFleetPrefixes == 0 || uint64(len(input.Stats.Providers)) > measurement.MaxProviders || len(measurement.CurrentBindingKVs) > len(input.Stats.Providers) {
		return nil, errors.New("compact measurement provider or head census exceeds its bound")
	}
	providerKVs := make(map[connect.Id]bool, len(input.Stats.Providers))
	var hashes uint64
	previous := ""
	for _, provider := range input.Stats.Providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(provider.ClientID) != 36 || len(provider.LatencyBuckets) != statsLatencyBuckets || provider.ClientID <= previous {
			return nil, errors.New("compact measurement provider shape or order is invalid")
		}
		clientID, err := connect.ParseId(provider.ClientID)
		if err != nil || clientID.String() != provider.ClientID {
			return nil, errors.New("compact measurement provider identity is not canonical")
		}
		count := uint64(len(provider.EgressIPHashHexes))
		if count > measurement.MaxEgressHashes-hashes {
			return nil, errors.New("compact measurement egress census exceeds its bound")
		}
		hashes += count
		for _, encoded := range provider.EgressIPHashHexes {
			if len(encoded) != 66 {
				return nil, errors.New("compact measurement egress width is invalid")
			}
		}
		providerKVs[clientID] = true
		previous = provider.ClientID
	}
	return providerKVs, nil
}

// Admission precedes every reader/scratch mutation. Detach all operators at
// once: a first operator's synchronous callback must not change a later one's
// keys, raw counters, signed header, binding observations or scoring policy.
func ownReleaseMeasurementV2(ctx context.Context, artifact *ReleaseMeasurementArtifact, options ReleaseMeasurementV2Options) (*ReleaseMeasurementArtifact, ReleaseMeasurementV2Options, error) {
	if ctx == nil {
		return nil, options, errors.New("compact release measurement context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, options, err
	}
	if artifact == nil || artifact.Schema != ReleaseMeasurementSchemaV2 {
		return nil, options, errors.New("compact release measurement schema is unsupported")
	}
	if (artifact.SettlementClosureV2 == nil) != (options.Settlement == nil) {
		return nil, options, errors.New("compact terminal closure and independent authority must both be present")
	}
	if options.MaxArtifactBytes == 0 || options.MaxArtifactBytes > maxReleaseMeasurementArtifactBytes || options.MaxControlBytes == 0 || options.MaxControlBytes > maxReleaseMeasurementArtifactBytes || options.MaxOperators == 0 || options.MaxHeadEntries == 0 || uint64(len(options.Operators)) > options.MaxOperators || len(artifact.Inputs) != len(options.Operators) || uint64(len(artifact.HeadEMA)) > options.MaxHeadEntries {
		return nil, options, errors.New("compact release measurement bounds or complete operator census are invalid")
	}
	// Refuse recursive legacy authority before the bounded storage walk.
	for _, input := range artifact.Inputs {
		if input.Stats.AttemptCut != nil || input.Stats.SettlementTransition != nil {
			return nil, options, errors.New("compact measurement contains competing legacy authority")
		}
	}
	if closure := artifact.SettlementClosureV2; closure != nil {
		if closure.Epoch == ^uint64(0) || closure.Epoch+1 != artifact.SettlementEpoch || len(closure.Transitions) != len(artifact.Inputs) || options.Settlement.MaxClosureBytes > options.MaxArtifactBytes {
			return nil, options, errors.New("compact terminal closure epoch, census or containing byte bound differs")
		}
		for _, transition := range closure.Transitions {
			if transition == nil || transition.PreFold.AttemptCut != nil || transition.PreFold.SettlementTransition != nil {
				return nil, options, errors.New("compact terminal closure contains missing or competing legacy authority")
			}
		}
	}
	remaining := options.MaxControlBytes
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(artifact), &remaining); err != nil {
		return nil, options, err
	}
	if releaseMeasurementV2Decision(artifact) != options.Expected || !reflect.DeepEqual(artifact.Policy, options.Policy) || !slices.Equal(artifact.ControlledNOIDs, options.ControlledNOIDs) || !slices.Equal(artifact.Bindings, options.Bindings) || !slices.Equal(artifact.Pools, options.Pools) || !slices.Equal(artifact.DepositAudits, options.DepositAudits) {
		return nil, options, errors.New("compact release measurement differs from independently authenticated observations")
	}
	if err := verifyReleaseMeasurementCommonIdentity(artifact); err != nil {
		return nil, options, err
	}
	if len(artifact.Inputs) < options.Policy.Safety.MinimumHealthyNOCount {
		return nil, options, errors.New("compact measurement has too few independently measured operators")
	}
	config := ReleaseStatsConfig{AMin: options.Policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis}
	scratchKVs := make(map[string]bool, len(artifact.Inputs))
	for index, input := range artifact.Inputs {
		operator, exists := options.Operators[input.NoID]
		if !exists || input.NoID == 0 || index > 0 && input.NoID <= artifact.Inputs[index-1].NoID {
			return nil, options, errors.New("compact measurement operator inputs are not the exact ordered census")
		}
		// Admit every configured replay owner before the first operator reads.
		// Actual descriptor-anchored filesystem custody still belongs to replay;
		// this operation never creates, erases or reuses those private indexes.
		if err := operator.Measurement.Replay.Bounds.validate(operator.Bounds); err != nil {
			return nil, options, err
		}
		scratch := operator.Measurement.Replay.ScratchDirectory
		if !filepath.IsAbs(scratch) || filepath.Clean(scratch) != scratch || filepath.Dir(scratch) == scratch || scratchKVs[scratch] || operator.Measurement.Replay.ReadMetadata == nil || operator.Measurement.Replay.OpenData == nil {
			return nil, options, errors.New("compact measurement requires distinct owned replay namespaces and readers")
		}
		scratchKVs[scratch] = true
		if _, err := admitReleaseMeasurementV2ProviderCensus(ctx, input, operator); err != nil {
			return nil, options, err
		}
		if input.CutNativeBlock == 0 || input.CutNativeBlock != operator.CutNativeBlock || input.CutNativeBlockHash != operator.CutNativeBlockHash || !releaseBlockAtOrBefore(input.CutNativeBlock, input.CutNativeBlockHash, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash) {
			return nil, options, errors.New("compact measurement native cut differs from its expected decision")
		}
		if _, err := parseReleaseHex32("compact native cut hash", input.CutNativeBlockHash, false); err != nil {
			return nil, options, err
		}
		expected := operator.Expected
		identity := expected.Identity
		domain := expected.Activation.Domain
		firstActivation := options.Operators[artifact.Inputs[0].NoID].Expected.Activation
		if !equalAttemptCutV2CommonDomain(domain, firstActivation.Domain) || expected.Activation.Hotkey != firstActivation.Hotkey {
			return nil, options, errors.New("compact operators do not share one authenticated activation and native hotkey")
		}
		if identity.NoID != input.NoID || identity.DeploymentID != artifact.DeploymentID || identity.ChainID != artifact.ChainID || identity.GenesisHash != artifact.GenesisHash || identity.Netuid != artifact.Netuid || identity.ValidatorID != artifact.ValidatorID || identity.ValidatorUID != artifact.SelfUID || common.Address(domain.Coordinator) != common.HexToAddress(artifact.Coordinator) || common.Address(domain.SettlementVault) != common.HexToAddress(artifact.SettlementVault) {
			return nil, options, errors.New("compact operator authority differs from the expected decision namespace")
		}
		if _, err := attemptCutV2PolicyDepth(expected, options.Policy); err != nil {
			return nil, options, err
		}
		if input.SettlementEpoch != artifact.SettlementEpoch || input.SettlementEpoch != expected.Boundary.SettlementEpoch || input.CutEVMSnapshotBlock != expected.Boundary.EVMBlock || input.CutEVMSnapshotHash != expected.Boundary.EVMBlockHash || input.EgressGeneration != expected.EgressGeneration || input.AttemptCutV2.Context != expected || !releaseBlockAtOrBefore(input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash, artifact.EVMSnapshotBlock, artifact.EVMSnapshotHash) {
			return nil, options, errors.New("compact input boundary or signed native cursor differs from expected authority")
		}
		if input.Stats.Config != config || operator.Measurement.ExpectedConfig != config {
			return nil, options, errors.New("compact measurement scoring configuration differs from release policy")
		}
		if err := input.AttemptCutV2.Validate(operator.Bounds); err != nil {
			return nil, options, err
		}
	}
	var settlement *attemptSettlementV2Operation
	if artifact.SettlementClosureV2 != nil {
		var err error
		settlement, err = admitAttemptSettlementV2(ctx, artifact.SettlementClosureV2, *options.Settlement, true)
		if err != nil {
			return nil, options, err
		}
		for index, transition := range settlement.closure.Transitions {
			input := artifact.Inputs[index]
			operator := settlement.options.Operators[transition.Identity.NoID]
			if transition.Identity.NoID != input.NoID || !reflect.DeepEqual(operator.Policy, options.Policy) || scratchKVs[operator.Measurement.Replay.ScratchDirectory] {
				return nil, options, errors.New("compact terminal operator, policy or replay namespace differs")
			}
			if err := verifyAttemptSettlementV2Successor(transition, input.Stats, *input.AttemptCutV2, false); err != nil {
				return nil, options, err
			}
		}
	}
	encoded, err := canonicalReleaseMeasurementBytes(artifact)
	if err != nil || uint64(len(encoded)) > options.MaxArtifactBytes {
		return nil, options, errors.Join(errors.New("compact measurement canonical wire exceeds its bound"), err)
	}
	// Canonical serialization is now bounded; decoding creates one fully owned
	// control object, not history or a validation cache. No external I/O occurred.
	var owned ReleaseMeasurementArtifact
	if err := json.Unmarshal(encoded, &owned); err != nil {
		return nil, options, err
	}
	options.Policy = owned.Policy
	options.ControlledNOIDs = slices.Clone(owned.ControlledNOIDs)
	options.Bindings = slices.Clone(owned.Bindings)
	options.Pools = slices.Clone(owned.Pools)
	options.DepositAudits = slices.Clone(owned.DepositAudits)
	operators := make(map[uint64]ReleaseMeasurementV2OperatorOptions, len(options.Operators))
	for noID, operator := range options.Operators {
		operator.Measurement.CurrentBindingKVs = maps.Clone(operator.Measurement.CurrentBindingKVs)
		keys := make(map[byte]ed25519.PublicKey, len(operator.Measurement.Replay.ServerKeys))
		for keyID, key := range operator.Measurement.Replay.ServerKeys {
			if len(key) != ed25519.PublicKeySize {
				return nil, options, errors.New("compact operator server key has invalid width")
			}
			keys[keyID] = slices.Clone(key)
		}
		operator.Measurement.Replay.ServerKeys = keys
		operators[noID] = operator
	}
	options.Operators = operators
	if settlement != nil {
		owned.SettlementClosureV2 = settlement.closure
		options.Settlement = &settlement.options
	}
	return &owned, options, nil
}

// The only per-history lineage state is one expected sequence/hash per
// operator. Empty prior cuts are established by the unchanged first/prior pair.
type releaseMeasurementV2Checkpoint struct {
	sequence uint64
	root     string
	seen     bool
}

func (self *releaseMeasurementV2Checkpoint) visit(record AttemptRecord) error {
	if record.Sequence != self.sequence {
		return nil
	}
	if self.seen || record.RecordHash != self.root {
		return errors.New("compact measurement rewrites its authenticated prior prefix")
	}
	self.seen = true
	return nil
}

// Owned observations use the common eligibility rules. Each operator then
// executes exactly one full joint replay, retaining only bounded projections.
func verifyOwnedReleaseMeasurementV2(ctx context.Context, artifact *ReleaseMeasurementArtifact, options ReleaseMeasurementV2Options, checkpoints, terminalCheckpoints map[uint64]*releaseMeasurementV2Checkpoint) (VerifiedReleaseMeasurementV2, error) {
	providerKVs := make(map[uint64]map[connect.Id]bool, len(artifact.Inputs))
	for _, input := range artifact.Inputs {
		providers, err := admitReleaseMeasurementV2ProviderCensus(ctx, input, options.Operators[input.NoID])
		if err != nil {
			return VerifiedReleaseMeasurementV2{}, err
		}
		providerKVs[input.NoID] = providers
	}
	fleets, bound, membersByUID, stale, activeKVs, err := releaseMeasurementBindingObservations(artifact, providerKVs)
	if err != nil {
		return VerifiedReleaseMeasurementV2{}, err
	}
	// Bind every operator's supplied fixed-key map before the first stream read.
	for _, input := range artifact.Inputs {
		currentKVs := make(map[connect.Id]FleetScoreKey)
		for clientID := range bound[input.NoID] {
			currentKVs[clientID] = activeKVs[fmt.Sprintf("%020d:%s", input.NoID, clientID)]
		}
		if !maps.Equal(currentKVs, options.Operators[input.NoID].Measurement.CurrentBindingKVs) {
			return VerifiedReleaseMeasurementV2{}, errors.New("compact current bindings differ from authenticated eligibility")
		}
	}
	controlled := make(map[uint64]bool, len(artifact.ControlledNOIDs))
	for index, noID := range artifact.ControlledNOIDs {
		if noID == 0 || index > 0 && noID <= artifact.ControlledNOIDs[index-1] || providerKVs[noID] == nil {
			return VerifiedReleaseMeasurementV2{}, errors.New("compact controlled operator census is invalid")
		}
		controlled[noID] = true
	}
	var settlementReplayKVs map[uint64]AttemptCutV2ReplayResult
	if artifact.SettlementClosureV2 != nil {
		operation := &attemptSettlementV2Operation{closure: artifact.SettlementClosureV2, options: *options.Settlement}
		terminal, err := operation.replay(ctx, func(noID uint64, record AttemptRecord) error {
			if checkpoint := terminalCheckpoints[noID]; checkpoint != nil {
				return checkpoint.visit(record)
			}
			return nil
		}, false)
		if err != nil {
			return VerifiedReleaseMeasurementV2{}, fmt.Errorf("compact terminal closure: %w", err)
		}
		for _, checkpoint := range terminalCheckpoints {
			if !checkpoint.seen {
				return VerifiedReleaseMeasurementV2{}, errors.New("compact terminal closure omits its authenticated prior checkpoint")
			}
		}
		settlementReplayKVs = make(map[uint64]AttemptCutV2ReplayResult, len(terminal.Operators))
		for noID, projection := range terminal.Operators {
			settlementReplayKVs[noID] = projection.Replay
		}
	} else if len(terminalCheckpoints) != 0 {
		return VerifiedReleaseMeasurementV2{}, errors.New("compact lineage omitted its terminal replay")
	}
	statsKVs := make(map[uint64]VerifiedReleaseStats, len(artifact.Inputs))
	replayedKVs := make(map[uint64]AttemptCutV2ReplayResult, len(artifact.Inputs))
	for _, input := range artifact.Inputs {
		operator := options.Operators[input.NoID]
		var visit func(AttemptRecord) error
		if checkpoint := checkpoints[input.NoID]; checkpoint != nil {
			visit = checkpoint.visit
		}
		projection, err := verifyReleaseStatsAndHeadWithAttemptCutV2(ctx, input.Stats, *input.AttemptCutV2, operator.Expected, options.Policy, operator.Bounds, operator.Measurement, visit)
		if err != nil {
			return VerifiedReleaseMeasurementV2{}, fmt.Errorf("compact operator %d: %w", input.NoID, err)
		}
		if checkpoint := checkpoints[input.NoID]; checkpoint != nil && !checkpoint.seen {
			return VerifiedReleaseMeasurementV2{}, errors.New("compact measurement omits its authenticated prior checkpoint")
		}
		for key, prefixes := range projection.HeadPrefixes {
			destination := fleets[key]
			if destination == nil {
				return VerifiedReleaseMeasurementV2{}, errors.New("compact replay returned an ineligible fleet")
			}
			for hash := range prefixes {
				destination[hash] = true
			}
		}
		statsKVs[input.NoID], replayedKVs[input.NoID] = projection.Stats, projection.Replay
	}
	decision, err := assembleReleaseMeasurement(artifact, statsKVs, fleets, bound, membersByUID, stale, controlled)
	if err != nil {
		return VerifiedReleaseMeasurementV2{}, err
	}
	if err := ctx.Err(); err != nil {
		return VerifiedReleaseMeasurementV2{}, err
	}
	return VerifiedReleaseMeasurementV2{Decision: decision, ReplayByNO: replayedKVs, SettlementReplayByNO: settlementReplayKVs}, nil
}

// No caller can supply a verified flag or a fabricated legacy cut. Full
// verification, close and cancellation checks happen on every invocation.
func VerifyReleaseMeasurementArtifactV2(ctx context.Context, artifact *ReleaseMeasurementArtifact, options ReleaseMeasurementV2Options) (result VerifiedReleaseMeasurementV2, resultErr error) {
	if ctx == nil {
		return result, errors.New("compact release measurement context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = VerifiedReleaseMeasurementV2{}
		}
	}()
	owned, ownedOptions, err := ownReleaseMeasurementV2(ctx, artifact, options)
	if err != nil {
		return result, err
	}
	return verifyOwnedReleaseMeasurementV2(ctx, owned, ownedOptions, nil, nil)
}

// The explicit raw wire has no legacy cut/transition decoding destination.
// Unknown legacy fields are refused without materializing their record history.
type releaseMeasurementV2WireStats struct {
	Config    ReleaseStatsConfig           `json:"config"`
	Providers []ReleaseProviderMeasurement `json:"providers"`
}

type releaseMeasurementV2WireInput struct {
	NoID                uint64                        `json:"no_id"`
	SettlementEpoch     uint64                        `json:"settlement_epoch"`
	CutNativeBlock      uint64                        `json:"cut_native_block"`
	CutNativeBlockHash  string                        `json:"cut_native_block_hash"`
	CutEVMSnapshotBlock uint64                        `json:"cut_evm_snapshot_block"`
	CutEVMSnapshotHash  string                        `json:"cut_evm_snapshot_hash"`
	EgressGeneration    uint64                        `json:"egress_generation"`
	Stats               releaseMeasurementV2WireStats `json:"stats"`
	AttemptCutV2        *AttemptCutV2                 `json:"attempt_cut_v2"`
}

// The outer common metadata is shared; these explicit fields override the
// materialized legacy inputs and defer terminal parsing to its separate API.
type releaseMeasurementV2DecodeWire struct {
	ReleaseMeasurementArtifact
	Inputs              []releaseMeasurementV2WireInput `json:"inputs"`
	SettlementClosureV2 json.RawMessage                 `json:"settlement_closure_v2"`
}

// Strict decoding has the same canonical newline/field-order contract as v1,
// but no v1 evidence verifier is used. The private decoder proves bytes only.
func decodeReleaseMeasurementV2Bytes(ctx context.Context, encoded []byte, maxBytes, maxOperators uint64) (*ReleaseMeasurementArtifact, error) {
	if maxBytes == 0 || maxBytes > maxReleaseMeasurementArtifactBytes || maxOperators == 0 || uint64(len(encoded)) > maxBytes {
		return nil, errors.New("compact release measurement bytes exceed their bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var wire releaseMeasurementV2DecodeWire
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	if wire.Schema != ReleaseMeasurementSchemaV2 || uint64(len(wire.Inputs)) > maxOperators {
		return nil, errors.New("compact measurement schema or operator census is unsupported")
	}
	artifact := wire.ReleaseMeasurementArtifact
	if len(wire.SettlementClosureV2) != 0 {
		closure, err := decodeAttemptSettlementClosureV2Bytes(ctx, append(wire.SettlementClosureV2, '\n'), maxBytes, maxOperators)
		if err != nil {
			return nil, err
		}
		artifact.SettlementClosureV2 = closure
	}
	if wire.Inputs != nil {
		artifact.Inputs = make([]ReleaseMeasurementInput, len(wire.Inputs))
	}
	for index, input := range wire.Inputs {
		artifact.Inputs[index] = ReleaseMeasurementInput{
			NoID: input.NoID, SettlementEpoch: input.SettlementEpoch,
			CutNativeBlock: input.CutNativeBlock, CutNativeBlockHash: input.CutNativeBlockHash,
			CutEVMSnapshotBlock: input.CutEVMSnapshotBlock, CutEVMSnapshotHash: input.CutEVMSnapshotHash,
			EgressGeneration: input.EgressGeneration, AttemptCutV2: input.AttemptCutV2,
			Stats: ReleaseStatsMeasurement{Config: input.Stats.Config, Providers: input.Stats.Providers},
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.Join(errors.New("compact release measurement has trailing JSON"), err)
	}
	canonical, err := canonicalReleaseMeasurementBytes(&artifact)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return nil, errors.Join(errors.New("compact release measurement bytes are not canonical"), err)
	}
	return &artifact, nil
}

// Every standalone public decode performs the complete policy-aware replay.
func DecodeReleaseMeasurementArtifactV2(ctx context.Context, encoded []byte, options ReleaseMeasurementV2Options) (*ReleaseMeasurementArtifact, VerifiedReleaseMeasurementV2, error) {
	if ctx == nil {
		return nil, VerifiedReleaseMeasurementV2{}, errors.New("compact release measurement context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, VerifiedReleaseMeasurementV2{}, err
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(ctx, encoded, options.MaxArtifactBytes, options.MaxOperators)
	if err != nil {
		return nil, VerifiedReleaseMeasurementV2{}, err
	}
	result, err := VerifyReleaseMeasurementArtifactV2(ctx, artifact, options)
	if err != nil {
		return nil, VerifiedReleaseMeasurementV2{}, err
	}
	return artifact, result, nil
}

// The emitted bytes describe the same owned control object that was replayed.
// A synchronous transport cannot mutate the caller's artifact after acceptance
// and thereby substitute different bytes into the returned content address.
func SealReleaseMeasurementArtifactV2(ctx context.Context, artifact *ReleaseMeasurementArtifact, options ReleaseMeasurementV2Options) (encoded []byte, contentHash string, resultErr error) {
	if ctx == nil {
		return nil, "", errors.New("compact release measurement context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			encoded, contentHash = nil, ""
		}
	}()
	owned, ownedOptions, err := ownReleaseMeasurementV2(ctx, artifact, options)
	if err != nil {
		return nil, "", err
	}
	if _, err := verifyOwnedReleaseMeasurementV2(ctx, owned, ownedOptions, nil, nil); err != nil {
		return nil, "", err
	}
	encoded, err = canonicalReleaseMeasurementBytes(owned)
	if err != nil {
		return nil, "", err
	}
	return encoded, ReleaseMeasurementContentHash(encoded), nil
}

// The public compact intent join does not trust a caller-built scoring result.
// Like the v1 measurement join, this checks the intent's content/decision, not
// its transaction signature. Existing legacy intent-store/envelope methods
// still refuse this wire rather than replaying remotely under their mutexes.
func VerifyReleaseMeasurementIntentV2(ctx context.Context, encoded []byte, options ReleaseMeasurementV2Options, intent *SteeringIntent) error {
	if ctx == nil {
		return errors.New("compact release measurement context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if options.MaxArtifactBytes == 0 || options.MaxArtifactBytes > maxReleaseMeasurementArtifactBytes || uint64(len(encoded)) > options.MaxArtifactBytes {
		return errors.New("compact release measurement bytes exceed their bound")
	}
	// Own exactly the compared decision fields before callbacks. Private
	// transaction/submission state remains outside this measurement join.
	ownIntent := func() (*SteeringIntent, error) {
		maxBytes := options.MaxControlBytes
		if intent == nil || maxBytes == 0 || maxBytes > maxReleaseMeasurementArtifactBytes {
			return nil, errors.New("compact measurement intent or control bound is absent")
		}
		owned := &SteeringIntent{
			ValidatorID: intent.ValidatorID, Netuid: intent.Netuid, SubnetEpoch: intent.SubnetEpoch,
			NativeSnapshotBlock: intent.NativeSnapshotBlock, NativeSnapshotHash: intent.NativeSnapshotHash,
			EVMSnapshotBlock: intent.EVMSnapshotBlock, EVMSnapshotHash: intent.EVMSnapshotHash,
			SettlementEpoch: intent.SettlementEpoch, PolicyHash: intent.PolicyHash, SelfUID: intent.SelfUID,
			MeasurementArtifactHash: intent.MeasurementArtifactHash, MeasurementArtifactSize: intent.MeasurementArtifactSize,
			MaskedUIDs: intent.MaskedUIDs, EligibleHeadUIDs: intent.EligibleHeadUIDs, EligibleHeadScores: intent.EligibleHeadScores,
			SelectedHeadUIDs: intent.SelectedHeadUIDs, RejectedHeadUIDs: intent.RejectedHeadUIDs,
			StaleHeadBindings: intent.StaleHeadBindings, DepositAudits: intent.DepositAudits,
			UIDs: intent.UIDs, Scores: intent.Scores,
		}
		remaining := maxBytes
		if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(owned), &remaining); err != nil {
			return nil, err
		}
		owned.MaskedUIDs = slices.Clone(owned.MaskedUIDs)
		owned.EligibleHeadUIDs = slices.Clone(owned.EligibleHeadUIDs)
		owned.EligibleHeadScores = slices.Clone(owned.EligibleHeadScores)
		owned.SelectedHeadUIDs = slices.Clone(owned.SelectedHeadUIDs)
		owned.RejectedHeadUIDs = slices.Clone(owned.RejectedHeadUIDs)
		owned.StaleHeadBindings = slices.Clone(owned.StaleHeadBindings)
		owned.DepositAudits = slices.Clone(owned.DepositAudits)
		owned.UIDs = slices.Clone(owned.UIDs)
		owned.Scores = slices.Clone(owned.Scores)
		return owned, nil
	}
	ownedIntent, err := ownIntent()
	if err != nil {
		return err
	}
	if ownedIntent.MeasurementArtifactSize != uint64(len(encoded)) || ownedIntent.MeasurementArtifactHash != ReleaseMeasurementContentHash(encoded) {
		return errors.New("compact measurement intent names different artifact bytes")
	}
	artifact, result, err := DecodeReleaseMeasurementArtifactV2(ctx, encoded, options)
	if err != nil {
		return err
	}
	if err := VerifyReleaseMeasurementIntent(ownedIntent, artifact, result.Decision); err != nil {
		return err
	}
	return ctx.Err()
}

// Both artifacts and any containing terminal closure are authenticated in one
// owned operation. Within a window the current cut must contain the previous
// prefix; across settlement the full terminal cut proves that prefix and its
// complete post-fold quality becomes the new window's exact prior census.
func VerifyReleaseMeasurementLineageV2(ctx context.Context, previousEncoded []byte, previousOptions ReleaseMeasurementV2Options, current *ReleaseMeasurementArtifact, currentOptions ReleaseMeasurementV2Options) (result VerifiedReleaseMeasurementV2, resultErr error) {
	if ctx == nil {
		return result, errors.New("compact release lineage context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = VerifiedReleaseMeasurementV2{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	previous, err := decodeReleaseMeasurementV2Bytes(ctx, previousEncoded, previousOptions.MaxArtifactBytes, previousOptions.MaxOperators)
	if err != nil {
		return result, err
	}
	previous, previousOptions, err = ownReleaseMeasurementV2(ctx, previous, previousOptions)
	if err != nil {
		return result, err
	}
	current, currentOptions, err = ownReleaseMeasurementV2(ctx, current, currentOptions)
	if err != nil {
		return result, err
	}
	// Every traversal in this invocation owns a distinct scratch namespace.
	// Refuse aliases before the previous artifact can create its first index.
	scratchKVs := map[string]bool{}
	for _, options := range []ReleaseMeasurementV2Options{previousOptions, currentOptions} {
		for _, operator := range options.Operators {
			path := operator.Measurement.Replay.ScratchDirectory
			if scratchKVs[path] {
				return result, errors.New("compact lineage reuses a replay namespace")
			}
			scratchKVs[path] = true
		}
		if options.Settlement != nil {
			for _, operator := range options.Settlement.Operators {
				path := operator.Measurement.Replay.ScratchDirectory
				if scratchKVs[path] {
					return result, errors.New("compact lineage reuses a terminal replay namespace")
				}
				scratchKVs[path] = true
			}
		}
	}
	if current.PreviousArtifactHash != ReleaseMeasurementContentHash(previousEncoded) || current.DeploymentID != previous.DeploymentID || current.ChainID != previous.ChainID || current.GenesisHash != previous.GenesisHash || current.Coordinator != previous.Coordinator || current.SettlementVault != previous.SettlementVault || current.ValidatorID != previous.ValidatorID || current.Netuid != previous.Netuid || current.SelfUID != previous.SelfUID || current.PolicyHash != previous.PolicyHash {
		return result, errors.New("compact release measurement lineage identity differs")
	}
	crossSettlement := current.SettlementEpoch != previous.SettlementEpoch
	if crossSettlement && (previous.SettlementEpoch == ^uint64(0) || current.SettlementEpoch != previous.SettlementEpoch+1 || current.SettlementClosureV2 == nil) {
		return result, errors.New("compact cross-settlement lineage requires its consecutive complete terminal closure")
	}
	if current.SubnetEpoch != previous.SubnetEpoch && (previous.SubnetEpoch == ^uint64(0) || current.SubnetEpoch != previous.SubnetEpoch+1) {
		return result, errors.New("compact measurement lineage is not consecutive by native epoch")
	}
	if len(current.Inputs) != len(previous.Inputs) {
		return result, errors.New("compact measurement lineage changes operator coverage")
	}
	checkpoints := make(map[uint64]*releaseMeasurementV2Checkpoint, len(current.Inputs))
	terminalCheckpoints := make(map[uint64]*releaseMeasurementV2Checkpoint)
	for index, input := range current.Inputs {
		prior := previous.Inputs[index]
		nextStats, nextCut := input.Stats, input.AttemptCutV2
		if crossSettlement {
			terminal := current.SettlementClosureV2.Transitions[index]
			nextStats, nextCut = terminal.PreFold, &terminal.Cut
		}
		if prior.NoID != input.NoID || !maps.Equal(releasePriorQualityState(prior.Stats), releasePriorQualityState(nextStats)) {
			return result, errors.New("compact measurement lineage changes operator or prior pool EMA")
		}
		oldCut, newCut := prior.AttemptCutV2, nextCut
		if oldCut.Context.Identity != newCut.Context.Identity || oldCut.Context.Activation != newCut.Context.Activation || oldCut.Context.FirstSequence != newCut.Context.FirstSequence || oldCut.Context.PriorRoot != newCut.Context.PriorRoot || oldCut.LastSequence > newCut.LastSequence || !releaseBlockAtOrBefore(prior.CutEVMSnapshotBlock, prior.CutEVMSnapshotHash, newCut.Context.Boundary.EVMBlock, newCut.Context.Boundary.EVMBlockHash) || !releaseBlockAtOrBefore(prior.CutNativeBlock, prior.CutNativeBlockHash, input.CutNativeBlock, input.CutNativeBlockHash) || oldCut.Context.EgressGeneration > newCut.Context.EgressGeneration || oldCut.Context.EgressFirstSequence > newCut.Context.EgressFirstSequence || oldCut.Context.EgressGeneration == newCut.Context.EgressGeneration && oldCut.Context.EgressFirstSequence != newCut.Context.EgressFirstSequence {
			return result, errors.New("compact measurement lineage changes or regresses its cumulative prefix")
		}
		checkpoint := &releaseMeasurementV2Checkpoint{sequence: oldCut.LastSequence, root: oldCut.Root, seen: oldCut.RecordCount == 0}
		if crossSettlement {
			terminalCheckpoints[input.NoID] = checkpoint
		} else {
			checkpoints[input.NoID] = checkpoint
		}
	}
	if _, err := verifyOwnedReleaseMeasurementV2(ctx, previous, previousOptions, nil, nil); err != nil {
		return result, fmt.Errorf("previous compact measurement: %w", err)
	}
	if err := verifyReleaseMeasurementHeadLineage(previous, current); err != nil {
		return result, err
	}
	return verifyOwnedReleaseMeasurementV2(ctx, current, currentOptions, checkpoints, terminalCheckpoints)
}
