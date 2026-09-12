//go:build linux || darwin

package validator

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urnetwork/connect"
)

// This pre-Prepared commitment has no signed extrinsic, envelope or intent
// fields. Exact canonical measurement bytes already include every source
// observation/cut hash; length framing and a fixed domain prevent ambiguity.
func releaseNativeSourceHashV2(measurement []byte) [32]byte {
	data := []byte("urnetwork-validator-native-source-commitment-v2\x00")
	data = binary.LittleEndian.AppendUint64(data, uint64(len(measurement)))
	return sha256.Sum256(append(data, measurement...))
}

func releaseRuntimeIdentityV2(cfg *ReleaseConfig) crv4.RuntimeArtifactIdentity {
	return crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
}

// Semantic startup checks the reverse source link before exposing any live
// workers. Exact head math is subsequently replayed by the V2 intent store;
// this boundary proves native byte identity, finality and real signer authority.
func authenticateReleaseNativeSourceReferenceV2(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, intent *SteeringIntent, artifact *ReleaseMeasurementArtifact) error {
	if ctx == nil || native == nil || cfg == nil || intent == nil || intent.Prepared == nil || artifact == nil || artifact.Schema != ReleaseMeasurementSchemaV2 {
		return errors.New("V2 native source reference owner is incomplete")
	}
	encoded, err := canonicalReleaseMeasurementBytes(artifact)
	if err != nil {
		return err
	}
	prepared := intent.Prepared
	if prepared.SourceCommitment == nil || prepared.SourceCommitment.Hash != releaseHex32(releaseNativeSourceHashV2(encoded)) || !releaseBlockAtOrBefore(artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash, prepared.PreparedAtBlock, prepared.PreparedAtBlockHash) {
		return errors.New("V2 native source does not commit its exact pre-Prepared artifact")
	}
	own := *native
	hash, err := types.NewHashFromHexString(prepared.PreparedAtBlockHash)
	if err != nil {
		return err
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, &own, cfg, hash); err != nil {
		return err
	}
	if err := own.ValidatePreparedSource(prepared); err != nil {
		return err
	}
	hotkey, err := canonicalAttemptHex32("V2 source signer", prepared.HotkeyHex, false)
	if err != nil {
		return err
	}
	observed, err := crv4.ReadValidatorScheduleAtContext(ctx, &own, crv4.ValidatorScheduleQuery{GenesisHash: own.GenesisHash, BlockHash: hash, BlockNumber: prepared.PreparedAtBlock, Netuid: cfg.Netuid, Hotkey: hotkey, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, releaseRuntimeIdentityV2(cfg))
	if err != nil || !observed.Stake.MeetsNonSelfStakeAndPermit() || observed.SubnetEpochIndex != intent.SubnetEpoch || observed.Stake.Identity.UID != intent.SelfUID {
		return errors.Join(errors.New("V2 prepared signer lacks actual canonical native schedule/eligibility"), err)
	}
	if intent.FinalizedBlock == 0 {
		return ctx.Err()
	}
	blockHash, err := types.NewHashFromHexString(intent.FinalizedBlockHash)
	if err != nil {
		return err
	}
	txHash, err := types.NewHashFromHexString(prepared.ExtrinsicHash)
	if err != nil {
		return err
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, &own, cfg, blockHash); err != nil {
		return err
	}
	return own.VerifyFinalizedSourceContext(ctx, prepared, &crv4.FinalizedExtrinsic{ExtrinsicHash: txHash, BlockHash: blockHash, BlockNumber: intent.FinalizedBlock})
}

func newReleaseSteererV2(cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, hotkey *crv4.Keypair, contexts []*ReleaseMeasurementContext, runtime *releaseRuntimeV2) (*ReleaseSteerer, error) {
	if cfg == nil || chain == nil || !chain.release || native == nil || hotkey == nil || runtime == nil || runtime.ctx == nil || runtime.hotkey == nil || runtime.native != native || runtime.chain != chain || runtime.hotkey.PublicKey() != hotkey.PublicKey() {
		return nil, errors.New("V2 steerer requires its actual authenticated production root")
	}
	if !reflect.DeepEqual(*cfg, runtime.cfg) {
		return nil, errors.New("V2 steerer configuration differs from its authenticated startup owner")
	}
	ownedCfg := runtime.cfg
	byNo := make(map[uint64]*ReleaseMeasurementContext, len(contexts))
	for _, measurement := range contexts {
		if measurement == nil || measurement.NoID == 0 || measurement.Stats == nil || measurement.ClientKey == nil || measurement.ClientKeyHistory == nil {
			return nil, errors.New("V2 measurement context lacks actual key-history/Stats ownership")
		}
		if _, ok := measurement.Artifacts.(*HTTPArtifactReader); !ok {
			return nil, errors.New("V2 measurement requires the concrete bounded operator HTTP reader")
		}
		if byNo[measurement.NoID] != nil {
			return nil, errors.New("V2 measurement operator is duplicated")
		}
		copy := *measurement
		byNo[measurement.NoID] = &copy
	}
	operators := make(map[uint64]OperatorConfig, len(cfg.Operators))
	for _, operator := range ownedCfg.Operators {
		if byNo[operator.NoID] == nil {
			return nil, errors.New("V2 measurement configured census differs")
		}
		operators[operator.NoID] = operator
	}
	if len(byNo) != len(operators) {
		return nil, errors.New("V2 measurement context census differs")
	}
	intents, err := newReleaseIntentStoreV2(runtime)
	if err != nil {
		return nil, err
	}
	if _, err := intents.currentV2(runtime.ctx); err != nil {
		return nil, err
	}
	ema, err := NewHeadEMAStoreV2(runtime.ctx, ownedCfg.StateDir, ownedCfg.EvidenceV2.Bounds.HeadEMA)
	if err != nil {
		return nil, err
	}
	ema.v2.provisionalEpochGaps = runtime.history != nil && runtime.history.retainedStartup && provisionalClosedNativeInputEnabled(&ownedCfg)
	ema.v2.historyAdoption = runtime.history.historyAdoption
	self := &ReleaseSteerer{cfg: &ownedCfg, chain: chain, native: native, hotkey: runtime.hotkey, contexts: byNo, operators: operators, intents: intents, headEMA: ema, runtimeV2: runtime}
	if err := requireReleaseEvidenceV2Runtime(self); err != nil {
		return nil, err
	}
	return self, nil
}

// Startup and submission require the same concrete V2 disk, source, session
// and intent owners. A lost V2 owner cannot select the legacy submission path.
func requireReleaseEvidenceV2Runtime(self *ReleaseSteerer) error {
	if self == nil || self.cfg == nil || self.runtimeV2 == nil || self.intents == nil || self.intents.v2 == nil || self.intents.v2.runtime != self.runtimeV2 || self.headEMA == nil {
		return errors.New("V2 production startup and submission ownership is incomplete; legacy fallback is forbidden")
	}
	runtime := self.runtimeV2
	if runtime.ctx == nil || runtime.history == nil || runtime.disk == nil || runtime.gate == nil || runtime.hotkey == nil || runtime.native == nil || runtime.chain == nil || !runtime.chain.release || self.hotkey != runtime.hotkey || self.native != runtime.native || self.chain != runtime.chain {
		return errors.New("V2 production chain or semantic startup owner differs")
	}
	if err := runtime.ctx.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(*self.cfg, runtime.cfg) {
		return errors.New("V2 production configuration differs from its authenticated startup owner")
	}
	if err := self.cfg.Validate(); err != nil {
		return err
	}
	if len(self.contexts) != len(runtime.cfg.Operators) || len(self.operators) != len(runtime.cfg.Operators) || len(runtime.runtimes) != len(runtime.cfg.Operators) || len(runtime.sources) != len(runtime.cfg.Operators) {
		return errors.New("V2 production operator ownership census differs")
	}
	for _, operator := range runtime.cfg.Operators {
		measurement, state := self.contexts[operator.NoID], runtime.disk.states[operator.NoID]
		if measurement == nil || measurement.NoID != operator.NoID || state == nil || measurement.Stats != state.stats || measurement.ClientKey == nil || measurement.ClientKeyHistory == nil || self.operators[operator.NoID] != operator {
			return errors.New("V2 production measurement differs from its actual disk or client-key owner")
		}
		reader, ok := measurement.Artifacts.(*HTTPArtifactReader)
		if !ok || reader == nil || reader.baseURL == nil || reader.baseURL.String() != operator.APIURL || reader.deploymentID != self.cfg.DeploymentID || reader.netuid != self.cfg.Netuid {
			return errors.New("V2 production artifact reader differs from its configured source")
		}
	}
	_, err := releaseReservedAttemptCensusReplicasV2(self.cfg, runtime.origins, runtime.runtimes)
	return err
}

// Each fixed provider key comes from the same independently observed binding
// projection used by head assembly; inactive/stale/local-key mismatches never
// acquire a current key merely because an attempt carries one.
func installReleaseBindingKeysV2(ctx context.Context, artifact *ReleaseMeasurementArtifact, options *ReleaseMeasurementV2Options) error {
	providers := make(map[uint64]map[connect.Id]bool, len(artifact.Inputs))
	for _, input := range artifact.Inputs {
		known, err := admitReleaseMeasurementV2ProviderCensus(ctx, input, options.Operators[input.NoID])
		if err != nil {
			return err
		}
		providers[input.NoID] = known
	}
	_, bound, _, _, active, err := releaseMeasurementBindingObservations(artifact, providers)
	if err != nil {
		return err
	}
	for _, input := range artifact.Inputs {
		operator := options.Operators[input.NoID]
		operator.Measurement.CurrentBindingKVs = make(map[connect.Id]FleetScoreKey, len(bound[input.NoID]))
		for id := range bound[input.NoID] {
			operator.Measurement.CurrentBindingKVs[id] = active[fmt.Sprintf("%020d:%s", input.NoID, id)]
		}
		options.Operators[input.NoID] = operator
	}
	return ctx.Err()
}

func (self *ReleaseSteerer) restoreHeadEmaV2(ctx context.Context, intent *SteeringIntent) error {
	if intent == nil {
		return nil
	}
	artifact, _, err := self.intents.measurementArtifactV2(ctx, intent)
	if err != nil {
		return err
	}
	if err := self.runtimeV2.publishDepositAuditV2(ctx, artifact); err != nil {
		return err
	}
	return self.headEMA.CommitForEpochV2(ctx, artifact.SubnetEpoch, artifact.HeadEMA, artifact.Policy.Steering.HeadScoreEMA)
}

// The metadata slot is not shared with a fleet signing identity. Any actual
// coordinator mirror rejects the role; an occupied native slot is allowed only
// when it equals a separately authenticated finalized validator source intent.
func (self *ReleaseSteerer) checkSourceRoleV2(ctx context.Context, snapshot *ReleaseSnapshot, nativeHash types.Hash, previous *SteeringIntent) error {
	data := self.chain.coordinator.PackMirroredCommitments(self.hotkey.PublicKey())
	raw, err := self.chain.ethCallAtHashContext(ctx, common.HexToAddress(self.cfg.Coordinator), data, snapshot.BlockNumber, snapshot.BlockHash)
	if err != nil {
		return err
	}
	if len(raw) != 96 || !slices.Equal(raw, make([]byte, 96)) {
		return errors.New("validator source hotkey has an existing fleet commitment mirror or malformed role response")
	}
	observed, err := self.native.SourceCommitmentSlotAtContext(ctx, self.cfg.Netuid, self.hotkey.PublicKey(), nativeHash)
	if err != nil {
		return err
	}
	if observed == nil {
		return nil
	}
	matches := func(intent *SteeringIntent) bool {
		return intent != nil && intent.Prepared != nil && intent.Prepared.SourceCommitment != nil && intent.FinalizedBlock != 0 && intent.Prepared.SourceCommitment.Hash == releaseHex32(observed.Hash) && intent.FinalizedBlock == observed.CommitmentBlock
	}
	if matches(previous) {
		return ctx.Err()
	}
	history, err := self.intents.authenticatedIntentsV2(ctx)
	if err != nil {
		return err
	}
	for index := range history {
		if matches(&history[index]) {
			return ctx.Err()
		}
	}
	return errors.New("validator source native slot belongs to another role or unretained write")
}

func (self *ReleaseSteerer) submitOnceV2(ctx context.Context) error {
	// Runtime authentication mutates a signing view, never the process-wide
	// metadata pointer used concurrently by independent native observers.
	owned := *self
	native := *self.native
	owned.native = &native
	self = &owned
	allowWeightRejection := self.runtimeV2.history.retainedStartup && provisionalClosedNativeInputEnabled(self.cfg)
	nativeHash, err := authenticatePinnedNativeRuntimeContext(ctx, self.native, self.cfg)
	if err != nil {
		return fmt.Errorf("authenticate native runtime before steering snapshot: %w", err)
	}
	nativeState, err := self.native.EpochScheduleStateAtContext(ctx, self.cfg.Netuid, nativeHash)
	if err != nil {
		return err
	}
	snapshot, err := self.chain.ReleaseSnapshotContext(ctx)
	if err != nil {
		return err
	}
	if err := self.validatePinnedChains(ctx, snapshot, nativeState, nativeHash); err != nil {
		return err
	}
	var hotkeyUids map[[32]byte]uint16
	loadHotkeyUids := func() error {
		if hotkeyUids != nil {
			return nil
		}
		var err error
		hotkeyUids, err = self.chain.MetagraphHotkeysAtHashContext(ctx, snapshot.BlockNumber, snapshot.BlockHash, self.cfg.Netuid)
		if err != nil {
			return fmt.Errorf("snapshot release metagraph: %w", err)
		}
		return nil
	}
	current, err := self.intents.currentV2(ctx)
	if err != nil {
		return err
	}
	if err := self.intents.v2.historyAdoption.requireFirstEpoch(current, nativeState.SubnetEpochIndex); err != nil {
		return err
	}
	if err := self.restoreHeadEmaV2(ctx, current); err != nil {
		return err
	}
	if current != nil && current.Status == "finalized" {
		if err := loadHotkeyUids(); err != nil {
			return err
		}
		if err := self.checkApplicationV2(ctx, snapshot, hotkeyUids); err != nil {
			return err
		}
	}
	if current, err = self.intents.currentV2(ctx); err != nil {
		return err
	} else if current != nil {
		if current.Status == "pending" {
			resolved, err := self.reconcilePendingV2(ctx, current, nativeState)
			if err != nil {
				return err
			}
			if resolved {
				return ErrSteeringAlreadyFinal
			}
			current, err = self.intents.currentV2(ctx)
			if err != nil {
				return err
			}
		}
		if current.SubnetEpoch == nativeState.SubnetEpochIndex {
			if current.Status == "finalized" || current.Status == "applied" {
				return ErrSteeringAlreadyFinal
			}
		}
		if current.SubnetEpoch < nativeState.SubnetEpochIndex && current.Status != "applied" && current.Status != "failed" {
			return fmt.Errorf("prior subnet epoch %d intent is %s; refusing a new commit", current.SubnetEpoch, current.Status)
		}
	}
	if err := loadHotkeyUids(); err != nil {
		return err
	}
	runtimeIdentity := releaseRuntimeIdentityV2(self.cfg)
	observed, err := crv4.ReadValidatorScheduleAtContext(ctx, self.native, crv4.ValidatorScheduleQuery{GenesisHash: self.native.GenesisHash, BlockHash: nativeHash, BlockNumber: nativeState.CurrentBlock, Netuid: self.cfg.Netuid, Hotkey: self.hotkey.PublicKey(), MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, runtimeIdentity)
	if err != nil || !observed.Stake.MeetsNonSelfStakeAndPermit() || observed.SubnetEpochIndex != nativeState.SubnetEpochIndex || hotkeyUids[self.hotkey.PublicKey()] != observed.Stake.Identity.UID {
		return errors.Join(errors.New("V2 current native validator schedule/stake/permit differs from independent EVM registration"), err)
	}
	inputs, options, err := self.runtimeV2.collect(ctx, self, current, snapshot, nativeState.SubnetEpochIndex, nativeState.CurrentBlock, nativeHash.Hex(), hotkeyUids)
	if err != nil {
		return err
	}
	if current != nil {
		options.Expected.PreviousArtifactHash = current.MeasurementArtifactHash
	}
	settlement := options.Settlement
	options.Settlement = nil
	head, err := self.gatherHeadV2(ctx, snapshot, nativeState.SubnetEpochIndex, nativeState.CurrentBlock, nativeHash.Hex(), hotkeyUids, inputs, options)
	if err != nil {
		return err
	}
	selfUid, found := hotkeyUids[self.hotkey.PublicKey()]
	if !found {
		return fmt.Errorf("self-mask: validator hotkey has no live UID on netuid %d", self.cfg.Netuid)
	}
	previousArtifactHash := ""
	if current, currentErr := self.intents.currentV2(ctx); currentErr != nil {
		return currentErr
	} else if current != nil {
		previousArtifactHash = current.MeasurementArtifactHash
	}
	controlledNoIds := append(make([]uint64, 0, len(self.cfg.ControlledNOIDs)), self.cfg.ControlledNOIDs...)
	sort.Slice(controlledNoIds, func(i, j int) bool { return controlledNoIds[i] < controlledNoIds[j] })
	measurementArtifact := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchemaV2, DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID,
		GenesisHash: strings.ToLower(self.cfg.GenesisHash), Coordinator: self.cfg.Coordinator,
		SettlementVault: self.cfg.SettlementVault, ValidatorID: self.cfg.ValidatorID, Netuid: self.cfg.Netuid,
		SubnetEpoch: nativeState.SubnetEpochIndex, NativeSnapshotBlock: nativeState.CurrentBlock,
		NativeSnapshotHash: strings.ToLower(nativeHash.Hex()), EVMSnapshotBlock: snapshot.BlockNumber,
		EVMSnapshotHash: releaseHex32(snapshot.BlockHash), SettlementEpoch: snapshot.Epoch.Uint64(),
		PolicyHash: strings.ToLower(self.cfg.PolicyHash), Policy: self.cfg.Policy,
		PreviousArtifactHash: previousArtifactHash, ControlledNOIDs: controlledNoIds,
		Inputs: head.Inputs, Bindings: head.Bindings, HeadEMA: head.HeadEMA,
		SelfUID: selfUid,
	}
	closure, err := self.runtimeV2.closureForDecision(ctx, snapshot.Epoch.Uint64())
	if err != nil {
		return err
	}
	measurementArtifact.SettlementClosureV2 = closure
	options.Settlement = settlement
	self.headInputsByNO = make(map[uint64]ReleaseMeasurementInput, len(head.Inputs))
	for _, input := range head.Inputs {
		self.headInputsByNO[input.NoID] = input
	}
	poolObservations, depositAudits, err := self.gatherPoolsV2(ctx, snapshot, head.Bound, hotkeyUids, measurementArtifact)
	if err != nil {
		return err
	}
	measurementArtifact.Pools, measurementArtifact.DepositAudits = poolObservations, depositAudits
	options.Bindings, options.Pools, options.DepositAudits = head.Bindings, poolObservations, depositAudits
	if err := installReleaseBindingKeysV2(ctx, measurementArtifact, &options); err != nil {
		return err
	}
	options, err = self.runtimeV2.measurementReplayOptionsV2(ctx, options, "submit-artifact")
	if err != nil {
		return err
	}
	measurementBytes, measurementHash, submissionReplay, err := self.runtimeV2.sealSubmissionArtifactV2(ctx, measurementArtifact, options)
	if err != nil {
		return classifyProvisionalNativeWeights(ctx, allowWeightRejection, nativeState.SubnetEpochIndex, snapshot.Epoch.Uint64(), err)
	}
	defer submissionReplay.close()
	measurementPath, measurementSize, err := persistReleaseMeasurementArtifact(self.cfg.StateDir, measurementBytes, measurementHash)
	if err != nil {
		return err
	}
	var verified VerifiedReleaseMeasurementV2
	if submissionReplay != nil {
		verified, err = submissionReplay.verify(ctx, measurementBytes)
	} else {
		options, err = self.runtimeV2.measurementReplayOptionsV2(ctx, options, "submit-decision")
		if err == nil {
			_, verified, err = DecodeReleaseMeasurementArtifactV2(ctx, measurementBytes, options)
		}
	}
	if err != nil {
		return err
	}
	verifiedMeasurement := verified.Decision
	uids := verifiedMeasurement.UIDs
	scores := verifiedMeasurement.Scores
	encodedScores, err := rationalJSON(scores)
	if err != nil {
		return err
	}
	eligibleScores := make([]*big.Rat, len(verifiedMeasurement.EligibleHead))
	for index := range verifiedMeasurement.EligibleHead {
		eligibleScores[index] = verifiedMeasurement.EligibleHead[index].Score
	}
	encodedEligibleScores, err := rationalJSON(eligibleScores)
	if err != nil {
		return err
	}
	preparedRuntimeHash, err := authenticatePinnedNativeRuntimeContext(ctx, self.native, self.cfg)
	if err != nil {
		return fmt.Errorf("authenticate native runtime before preparing steering: %w", err)
	}
	if err := self.checkSourceRoleV2(ctx, snapshot, preparedRuntimeHash, current); err != nil {
		return err
	}
	submitOptions := releaseSubmitOptions(self.cfg)
	submitOptions.SourceHash = releaseNativeSourceHashV2(measurementBytes)
	prepared, err := crv4.PrepareWeightsCRv4ExactAtContext(ctx, self.native, self.hotkey, self.cfg.Netuid, uids, scores, submitOptions, preparedRuntimeHash)
	if err != nil {
		return classifyProvisionalNativeWeights(ctx, allowWeightRejection, nativeState.SubnetEpochIndex, snapshot.Epoch.Uint64(), err)
	}
	preparedHash, err := types.NewHashFromHexString(prepared.PreparedAtBlockHash)
	if err != nil {
		return fmt.Errorf("decode prepared steering runtime hash: %w", err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, self.native, self.cfg, preparedHash); err != nil {
		return fmt.Errorf("authenticate prepared steering runtime at %s: %w", preparedHash.Hex(), err)
	}
	if prepared.SubnetEpoch != nativeState.SubnetEpochIndex {
		return fmt.Errorf("native epoch crossed during steering snapshot: started %d prepared %d", nativeState.SubnetEpochIndex, prepared.SubnetEpoch)
	}
	var envelopeBytes []byte
	var envelopeHash string
	if submissionReplay != nil {
		envelopeBytes, envelopeHash, _, err = submissionReplay.sealEnvelope(ctx, measurementBytes, selfUid, self.hotkey, strings.ToLower(prepared.ExtrinsicHash), time.Now().UTC())
	} else {
		options, err = self.runtimeV2.measurementReplayOptionsV2(ctx, options, "submit-envelope")
		if err == nil {
			envelopeBytes, envelopeHash, _, err = SealReleaseMeasurementEnvelopeV2(ctx, measurementBytes, selfUid, self.hotkey, strings.ToLower(prepared.ExtrinsicHash), time.Now().UTC(), options)
		}
	}
	if err != nil {
		return err
	}
	envelopePath, envelopeSize, err := persistReleaseMeasurementEnvelope(self.cfg.StateDir, envelopeBytes, envelopeHash)
	if err != nil {
		return err
	}
	intent, err := self.intents.beginV2(ctx, SteeringIntent{
		ValidatorID:             self.cfg.ValidatorID,
		Netuid:                  self.cfg.Netuid,
		SubnetEpoch:             nativeState.SubnetEpochIndex,
		NativeSnapshotBlock:     nativeState.CurrentBlock,
		NativeSnapshotHash:      nativeHash.Hex(),
		EVMSnapshotBlock:        snapshot.BlockNumber,
		EVMSnapshotHash:         fmt.Sprintf("0x%x", snapshot.BlockHash),
		SettlementEpoch:         snapshot.Epoch.Uint64(),
		PolicyHash:              self.cfg.PolicyHash,
		MeasurementArtifactPath: measurementPath,
		MeasurementArtifactHash: measurementHash,
		MeasurementArtifactSize: measurementSize,
		MeasurementEnvelopePath: envelopePath,
		MeasurementEnvelopeHash: envelopeHash,
		MeasurementEnvelopeSize: envelopeSize,
		SelfUID:                 selfUid,
		MaskedUIDs:              verifiedMeasurement.MaskedUIDs,
		EligibleHeadUIDs:        headSelectionUIDs(verifiedMeasurement.EligibleHead),
		EligibleHeadScores:      encodedEligibleScores,
		SelectedHeadUIDs:        headSelectionUIDs(verifiedMeasurement.SelectedHead),
		RejectedHeadUIDs:        headSelectionUIDs(verifiedMeasurement.RejectedHead),
		StaleHeadBindings:       verifiedMeasurement.StaleBindings,
		DepositAudits:           depositAudits,
		UIDs:                    uids,
		Scores:                  encodedScores,
		Prepared:                prepared,
	})
	if err != nil {
		return err
	}
	if err := self.runtimeV2.publishDepositAuditV2(ctx, measurementArtifact); err != nil {
		return err
	}
	if err := self.headEMA.CommitForEpochV2(ctx, measurementArtifact.SubnetEpoch, measurementArtifact.HeadEMA, measurementArtifact.Policy.Steering.HeadScoreEMA); err != nil {
		return fmt.Errorf("commit head EMA after steering intent: %w", err)
	}
	if _, err := authenticatePinnedNativeRuntimeContext(ctx, self.native, self.cfg); err != nil {
		return fmt.Errorf("authenticate native runtime before steering broadcast: %w", err)
	}
	result, err := crv4.SubmitPrepared(ctx, self.native, prepared)
	if err != nil {
		// The error can occur after broadcast but before finality was observed.
		// Preserve an uncertain pending state so a restart cannot double-submit.
		return self.recordReleasePendingError(intent.VectorHash, err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, self.native, self.cfg, result.FinalizedBlockHash); err != nil {
		return fmt.Errorf("authenticate steering finality at %s: %w", result.FinalizedBlockHash.Hex(), self.recordReleasePendingError(intent.VectorHash, err))
	}
	return self.intents.markFinalizedV2(ctx, intent.VectorHash, result.TxHash.Hex(), result.FinalizedBlock, result.FinalizedBlockHash.Hex(), result.RevealBlock, result.Values)
}

func (self *ReleaseSteerer) reconcilePendingV2(ctx context.Context, current *SteeringIntent, nativeState *crv4.EpochScheduleState) (bool, error) {
	if current == nil || current.Status != "pending" || current.Prepared == nil {
		return false, errors.New("cannot reconcile a non-pending steering intent")
	}
	if _, err := current.Prepared.Validate(); err != nil {
		return false, fmt.Errorf("validate pending steering submission: %w", err)
	}
	preparedRuntimeHash, err := types.NewHashFromHexString(current.Prepared.PreparedAtBlockHash)
	if err != nil {
		return false, fmt.Errorf("pending steering preparation hash: %w", err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, self.native, self.cfg, preparedRuntimeHash); err != nil {
		return false, fmt.Errorf("authenticate pending steering preparation runtime at %s: %w", preparedRuntimeHash.Hex(), err)
	}
	hash, err := types.NewHashFromHexString(current.Prepared.ExtrinsicHash)
	if err != nil {
		return false, fmt.Errorf("pending steering extrinsic hash: %w", err)
	}
	receipt, found, err := self.native.LocateFinalizedExtrinsic(ctx, hash, current.Prepared.PreparedAtBlock)
	if err != nil {
		return false, fmt.Errorf("reconcile pending steering finality: %w", err)
	}
	if found {
		if err := authenticatePinnedNativeRuntimeAtContext(ctx, self.native, self.cfg, receipt.BlockHash); err != nil {
			return false, fmt.Errorf("authenticate recovered steering finality at %s: %w", receipt.BlockHash.Hex(), err)
		}
		if sourceErr := self.native.VerifyFinalizedSourceContext(ctx, current.Prepared, receipt); sourceErr != nil {
			var dispatch *crv4.FinalizedDispatchError
			if errors.As(sourceErr, &dispatch) {
				return false, self.intents.markFailedV2(ctx, current.VectorHash, sourceErr)
			}
			return false, sourceErr
		}
		if err := self.intents.markFinalizedV2(ctx, current.VectorHash, receipt.ExtrinsicHash.Hex(), receipt.BlockNumber, receipt.BlockHash.Hex(), current.Prepared.RevealBlock, current.Prepared.Values); err != nil {
			return false, err
		}
		return true, nil
	}
	if current.SubnetEpoch < nativeState.SubnetEpochIndex {
		err := fmt.Errorf("unfinalized steering submission expired at subnet epoch %d", current.SubnetEpoch)
		if markErr := self.intents.markFailedV2(ctx, current.VectorHash, err); markErr != nil {
			return false, markErr
		}
		return false, nil
	}
	if current.SubnetEpoch > nativeState.SubnetEpochIndex {
		return false, fmt.Errorf("pending steering epoch %d is ahead of finalized epoch %d", current.SubnetEpoch, nativeState.SubnetEpochIndex)
	}
	nonceHash, err := authenticatePinnedNativeRuntimeContext(ctx, self.native, self.cfg)
	if err != nil {
		return false, fmt.Errorf("authenticate steering nonce runtime: %w", err)
	}
	finalizedNonce, err := self.native.AccountNonceAtContext(ctx, self.hotkey.PublicKey(), nonceHash)
	if err != nil {
		return false, err
	}
	if finalizedNonce > current.Prepared.AccountNonce {
		err := fmt.Errorf("steering nonce %d was consumed by a different finalized extrinsic", current.Prepared.AccountNonce)
		if markErr := self.intents.markFailedV2(ctx, current.VectorHash, err); markErr != nil {
			return false, markErr
		}
		return false, nil
	}
	if finalizedNonce < current.Prepared.AccountNonce {
		return false, fmt.Errorf("steering nonce gap: finalized %d, prepared %d", finalizedNonce, current.Prepared.AccountNonce)
	}
	if _, err := authenticatePinnedNativeRuntimeContext(ctx, self.native, self.cfg); err != nil {
		return false, fmt.Errorf("authenticate native runtime before pending replay: %w", err)
	}
	result, err := crv4.SubmitPrepared(ctx, self.native, current.Prepared)
	if err != nil {
		return false, self.recordReleasePendingError(current.VectorHash, err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, self.native, self.cfg, result.FinalizedBlockHash); err != nil {
		return false, fmt.Errorf("authenticate replayed steering finality at %s: %w", result.FinalizedBlockHash.Hex(), self.recordReleasePendingError(current.VectorHash, err))
	}
	if err := self.intents.markFinalizedV2(ctx, current.VectorHash, result.TxHash.Hex(), result.FinalizedBlock, result.FinalizedBlockHash.Hex(), result.RevealBlock, result.Values); err != nil {
		return false, err
	}
	return true, nil
}

func (self *ReleaseSteerer) checkApplicationV2(ctx context.Context, snapshot *ReleaseSnapshot, hotkeyUids map[[32]byte]uint16) error {
	current, err := self.intents.currentV2(ctx)
	if err != nil || current == nil || current.Status != "finalized" {
		return err
	}
	if hotkeyUids == nil {
		return errors.New("cannot track applied weights without a metagraph snapshot")
	}
	uid, found := hotkeyUids[self.hotkey.PublicKey()]
	if !found {
		return errors.New("cannot track applied weights without live validator UID")
	}
	hash, err := authenticatePinnedNativeRuntimeContext(ctx, self.native, self.cfg)
	if err != nil {
		return err
	}
	header, err := self.native.HeaderAtContext(ctx, hash)
	if err != nil {
		return fmt.Errorf("read applied-weight finalized header at %s: %w", hash.Hex(), err)
	}
	if header == nil {
		return fmt.Errorf("applied-weight finalized header at %s is unavailable", hash.Hex())
	}
	row, err := self.native.WeightsAtContext(ctx, self.cfg.Netuid, uid, hash)
	if err != nil {
		return err
	}
	block := uint64(header.Number)
	if block < current.RevealBlock {
		return nil
	}
	want := map[uint16]uint16{}
	for i, targetUid := range current.UIDs {
		if i < len(current.Values) {
			want[targetUid] = current.Values[i]
		}
	}
	got := map[uint16]uint16{}
	for _, pair := range row {
		got[uint16(pair.UID)] = uint16(pair.Value)
	}
	if len(want) != len(got) {
		return nil
	}
	for targetUid, value := range want {
		if got[targetUid] != value {
			return nil
		}
	}
	return self.intents.markAppliedV2(ctx, current.VectorHash, block, hash.Hex())
}
