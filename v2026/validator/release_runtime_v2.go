//go:build linux || darwin

// The production root joins authenticated startup, real live disk cuts,
// complete durable terminal closure and dual-origin public census publication.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"maps"
	"math/big"
	"path/filepath"
	"slices"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Only one cut/terminal mutation uses these current cursors at a time. They
// were reconstructed before Stats attachment; no snapshot/header supplies them.
// The root still owns every runtime and ledger Close after all workers join.
type releaseRuntimeV2 struct {
	ctx                  context.Context
	cfg                  ReleaseConfig
	chain                *ChainClient
	native               *crv4.Chain
	hotkey               *crv4.Keypair
	disk                 *releaseEvidenceV2DiskState
	history              *releaseEvidenceV2StartupHistory
	origins              [2]string
	runtimes             []*releaseOperatorRuntime
	sources              map[uint64]*releaseAttemptUploadSourceV2
	gate                 chan struct{}
	publications         map[uint64]*AttemptSettlementClosureV2
	publicationContexts  map[uint64]map[uint64]AttemptCutV2Context
	retainedStartupEpoch uint64
	publishEpoch         func(uint64)
}

// Semantic startup is called while the complete disk census is still dormant.
// Retained private cursor inputs escape only after actual recovery, reference
// authentication, proof projection, coherent publication and every Close.
func newReleaseRuntimeV2(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, hotkey *crv4.Keypair, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey, origins [2]string, disk *releaseEvidenceV2DiskState) (*releaseRuntimeV2, error) {
	if cfg == nil {
		return nil, errors.New("release V2 root configuration is absent")
	}
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
	return newReleaseRuntimeV2WithRuntime(ctx, cfg, chain, native, hotkey, inputs, serverKeys, origins, disk, runtime)
}

// The same private explicit-artifact boundary as semantic startup permits
// genuine fixture metadata without changing configured production pins. It
// still runs the complete real native, disk, reference and public replay.
func newReleaseRuntimeV2WithRuntime(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, hotkey *crv4.Keypair, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey, origins [2]string, disk *releaseEvidenceV2DiskState, runtime crv4.RuntimeArtifactIdentity) (*releaseRuntimeV2, error) {
	if ctx == nil || cfg == nil || chain == nil || native == nil || hotkey == nil || disk == nil {
		return nil, errors.New("release V2 root ownership is incomplete")
	}
	ownHotkey, _, err := ownReleaseMeasurementEnvelopeV2Hotkey(hotkey)
	if err != nil {
		return nil, err
	}
	sources := make(map[uint64]*releaseAttemptUploadSourceV2, len(inputs))
	for _, input := range inputs {
		source, err := newReleaseAttemptUploadSourceV2(cfg, input)
		if err != nil {
			return nil, err
		}
		if source.activation.Hotkey != hotkey.PublicKey() || sources[source.activation.NoID] != nil {
			return nil, errors.New("release V2 source census differs from the actual hotkey")
		}
		sources[source.activation.NoID] = source
	}
	var history *releaseEvidenceV2StartupHistory
	if err := startReleaseEvidenceV2DiskStateOwned(ctx, cfg, chain, native, inputs, serverKeys, origins, disk, runtime, attemptSettlementV2PhysicalIO(), &history); err != nil {
		return nil, err
	}
	if history == nil || len(sources) != len(history.participants) {
		return nil, errors.New("release V2 semantic startup omitted an owner")
	}
	for _, participant := range history.participants {
		disk.states[participant.NoID].uploadSource = sources[participant.NoID]
	}
	self := &releaseRuntimeV2{ctx: ctx, cfg: history.cfg, chain: chain, native: native, hotkey: ownHotkey, disk: disk, history: history, origins: origins, sources: sources,
		gate: make(chan struct{}, 1), publications: maps.Clone(history.terminals), publicationContexts: maps.Clone(history.terminalContexts)}
	if history.retainedStartup {
		self.retainedStartupEpoch = history.current[history.participants[0].NoID].epoch
	}
	if self.publicationContexts == nil {
		self.publicationContexts = make(map[uint64]map[uint64]AttemptCutV2Context)
	}
	// File/image history is already fully consumed and closed. Keep only the
	// bounded current/pre-terminal cursors and exact pending terminal objects.
	history.files, history.images = nil, nil
	history.journal, history.journalPreimages, history.journalPostimages = nil, nil, nil
	history.scratchPaths = nil
	return self, nil
}

func (self *releaseRuntimeV2) acquire(ctx context.Context) (func(), error) {
	if ctx == nil || self == nil || self.gate == nil {
		return nil, errors.New("release V2 operation owner is absent")
	}
	select {
	case self.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-self.gate
			return nil, err
		}
		return func() { <-self.gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Runtime constructors, not a supplied callback map, own all destination JWTs.
func (self *releaseRuntimeV2) attach(runtimes []*releaseOperatorRuntime) error {
	if self == nil || self.runtimes != nil {
		return errors.New("release V2 destinations were already attached")
	}
	if _, err := releaseReservedAttemptCensusReplicasV2(&self.cfg, self.origins, runtimes); err != nil {
		return err
	}
	for _, runtime := range runtimes {
		state := self.disk.states[runtime.measurement.NoID]
		if state == nil || runtime.measurement.Stats != state.stats || runtime.attemptLedger != state.ledger || runtime.attemptSource != self.sources[runtime.measurement.NoID] {
			return errors.New("release V2 destination runtime differs from the root's actual state")
		}
	}
	self.runtimes = slices.Clone(runtimes)
	return nil
}

// Fresh child names use the existing independently configured root, with no
// cleanup, implicit storage increase or reuse of completed replay scratch.
func (self *releaseRuntimeV2) scratch(ctx context.Context, noId uint64, seal bool, purpose string) (string, error) {
	var root string
	for _, operator := range self.cfg.EvidenceV2.Operators {
		if operator.NoID == noId {
			root = operator.ReplayScratchRoot
			if seal {
				root = operator.SealScratchRoot
			}
			break
		}
	}
	if root == "" || !validAttemptPrivateLeaf(purpose) {
		return "", errors.New("release V2 scratch owner is missing")
	}
	parent, err := openReleaseMeasurementInputV2Parents(ctx, root)
	if err != nil {
		return "", err
	}
	if err := errors.Join(parent.check(), parent.close(), ctx.Err()); err != nil {
		return "", err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return filepath.Join(root, purpose+"-"+hex.EncodeToString(nonce[:])), nil
}

func (self *releaseRuntimeV2) context(noId uint64, cursor releaseEvidenceV2StartupCursor, boundary AttemptBoundary) (AttemptCutV2Context, error) {
	initial, found := self.history.initial[noId]
	if !found || cursor.epoch != boundary.SettlementEpoch {
		return AttemptCutV2Context{}, errors.New("release V2 independent cursor epoch differs")
	}
	value := initial.InitialCut
	value.Boundary = boundary
	value.FirstSequence, value.EgressFirstSequence, value.EgressGeneration, value.PriorRoot = cursor.first, cursor.egressFirst, cursor.generation, cursor.priorRoot
	return value, value.Validate()
}

func (self *releaseRuntimeV2) operator(ctx context.Context, expected AttemptCutV2Context, purpose string) (AttemptSettlementV2OperatorOptions, error) {
	path, err := self.scratch(ctx, expected.Identity.NoID, false, purpose)
	if err != nil {
		return AttemptSettlementV2OperatorOptions{}, err
	}
	bounds, reader := self.cfg.EvidenceV2.Bounds, self.history.readers[0]
	return AttemptSettlementV2OperatorOptions{Expected: expected, Policy: self.cfg.Policy, Bounds: bounds.Cut, Measurement: AttemptCutV2MeasurementOptions{
		ExpectedConfig: ReleaseStatsConfig{AMin: self.cfg.Policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis},
		MaxProviders:   bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes, MaxFleetPrefixes: bounds.MaxFleetPrefixes,
		Replay: AttemptCutV2ReplayOptions{Bounds: bounds.Replay, ScratchDirectory: path, ServerKeys: self.history.keys[expected.Identity.NoID], ReadMetadata: reader.ReadMetadata, OpenData: reader.OpenData}}}, nil
}

func (self *releaseRuntimeV2) authority(ctx context.Context, contexts map[uint64]AttemptCutV2Context, purpose string) (AttemptSettlementV2Options, error) {
	bounds := self.cfg.EvidenceV2.Bounds
	options := AttemptSettlementV2Options{Operators: make(map[uint64]AttemptSettlementV2OperatorOptions, len(self.history.participants)), MaxParticipants: bounds.MaxParticipants, MaxTransitionBytes: bounds.MaxTransitionBytes, MaxClosureBytes: bounds.MaxClosureBytes}
	if len(contexts) != len(self.history.participants) {
		return options, errors.New("release V2 independent terminal contexts are incomplete")
	}
	for _, participant := range self.history.participants {
		expected, found := contexts[participant.NoID]
		if !found || expected.Identity.NoID != participant.NoID {
			return options, errors.New("release V2 independent terminal owner is missing")
		}
		operator, err := self.operator(ctx, expected, purpose)
		if err != nil {
			return options, err
		}
		options.Operators[participant.NoID] = operator
	}
	return ownAttemptSettlementV2Options(ctx, options)
}

func (self *releaseRuntimeV2) seal(ctx context.Context, noId uint64, replicas [2]AttemptCutV2Replica, purpose string) (AttemptCutV2SealOptions, error) {
	paths, err := self.scratch(ctx, noId, true, purpose)
	if err != nil {
		return AttemptCutV2SealOptions{}, err
	}
	publisher, err := newAttemptCutV2Replicas(self.cfg.EvidenceV2.Bounds.Cut, replicas)
	if err != nil {
		return AttemptCutV2SealOptions{}, err
	}
	return AttemptCutV2SealOptions{ReplayBounds: self.cfg.EvidenceV2.Bounds.Replay, ScratchDirectory: paths, ServerKeys: self.history.keys[noId],
		WriteRecords: publisher.writer(AttemptStreamV2Records), WriteProofs: publisher.writer(AttemptStreamV2Proofs), WriteMetadata: publisher.writer("metadata"),
		ReadMetadata: publisher.readers[0].ReadMetadata, OpenData: publisher.readers[0].OpenData}, nil
}

// Both geometric endpoints are read at the authenticated finalized observer.
// The terminal hash is then independently checked at its actual historical epoch.
func (self *releaseRuntimeV2) window(ctx context.Context, snapshot *ReleaseSnapshot, epoch uint64) (protocol.ValidatorEvidenceWindow, AttemptBoundary, error) {
	var zero protocol.ValidatorEvidenceWindow
	if snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() || epoch == ^uint64(0) || epoch >= snapshot.Epoch.Uint64() {
		return zero, AttemptBoundary{}, errors.New("release V2 closed epoch is not finalized")
	}
	start, err := self.chain.ReleaseEpochStartBlockAtHashContext(ctx, snapshot.BlockNumber, snapshot.BlockHash, new(big.Int).SetUint64(epoch))
	if err != nil {
		return zero, AttemptBoundary{}, err
	}
	end, err := self.chain.ReleaseEpochStartBlockAtHashContext(ctx, snapshot.BlockNumber, snapshot.BlockHash, new(big.Int).SetUint64(epoch+1))
	if err != nil || start == 0 || end <= start || end > snapshot.BlockNumber {
		return zero, AttemptBoundary{}, errors.Join(errors.New("release V2 terminal geometry differs"), err)
	}
	hash, err := self.chain.BlockHashContext(ctx, end-1)
	if err != nil {
		return zero, AttemptBoundary{}, err
	}
	boundary := AttemptBoundary{SettlementEpoch: epoch, EVMBlock: end - 1, EVMBlockHash: attemptHex32(hash)}
	for _, participant := range self.history.participants {
		if err := self.chain.authenticateReleaseStartupBoundaryV2Context(ctx, self.history.initial[participant.NoID].InitialCut.Activation.Domain, participant.NoID, boundary, true); err != nil {
			return zero, AttemptBoundary{}, err
		}
	}
	return protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: start, EndBlock: end, FinalizedBlock: snapshot.BlockNumber}, boundary, nil
}

// Publish all retained terminal slots before allowing another cut/advance.
// Exact retries retain the same closure, public hashes and immutable locator.
func (self *releaseRuntimeV2) publish(ctx context.Context, snapshot *ReleaseSnapshot) error {
	return self.publishWithReadHooks(ctx, snapshot, releaseMeasurementInputV2ReadHooks{})
}

// Publication never replaces real custody reads with a successful callback.
// Fault observers run only around the existing descriptor-owned locator read.
func (self *releaseRuntimeV2) publishWithReadHooks(ctx context.Context, snapshot *ReleaseSnapshot, hooks releaseMeasurementInputV2ReadHooks) error {
	replicas, err := releaseReservedAttemptCensusReplicasV2(&self.cfg, self.origins, self.runtimes)
	if err != nil {
		return err
	}
	epochs := make([]uint64, 0, len(self.publications))
	for epoch := range self.publications {
		epochs = append(epochs, epoch)
	}
	slices.Sort(epochs)
	for _, epoch := range epochs {
		closure := self.publications[epoch]
		if self.history.retainedStartup && epoch < self.retainedStartupEpoch {
			retained, err := self.resumeProvisionalRetainedPublication(ctx, epoch, closure, hooks)
			if err != nil {
				return err
			}
			if retained {
				delete(self.publications, epoch)
				delete(self.publicationContexts, epoch)
				continue
			}
		}
		window, boundary, err := self.window(ctx, snapshot, epoch)
		if err != nil {
			return err
		}
		if closure == nil || len(closure.Transitions) == 0 || closure.Transitions[0].FromBoundary != boundary {
			return errors.New("release V2 retained closure differs from the actual terminal")
		}
		authority, err := self.authority(ctx, self.publicationContexts[epoch], "census-replay")
		if err != nil {
			return err
		}
		options := ValidatorEvidenceCensusV2Options{Settlement: authority, Window: window, PrivateKeys: make(map[uint64]ed25519.PrivateKey), Hotkey: self.hotkey, ReplicasByOperator: replicas, SecondReplicaScratchDirectories: make(map[uint64]string)}
		noIds := make([]uint64, 0, len(self.history.participants))
		readOptions := ValidatorEvidencePublicationV2ReadOptions{Window: window, Origins: self.origins, Bounds: self.cfg.EvidenceV2.Bounds}
		for _, participant := range self.history.participants {
			noId := participant.NoID
			options.PrivateKeys[noId] = ed25519.PrivateKey(self.sources[noId].privateKey[:])
			options.SecondReplicaScratchDirectories[noId], err = self.scratch(ctx, noId, false, "census-replica-two")
			if err != nil {
				return err
			}
			noIds = append(noIds, noId)
			readOptions.Activations = append(readOptions.Activations, self.sources[noId].activation)
		}
		manifestPath, err := ValidatorEvidencePublicationV2ManifestPath(self.cfg.StateDir, epoch)
		if err != nil {
			return err
		}
		retained, err := readValidatorEvidencePublicationV2Manifest(ctx, manifestPath, self.cfg.EvidenceV2.Bounds.MaxClosureBytes, self.cfg.EvidenceV2.Bounds.MaxParticipants, hooks)
		if err != nil && !releaseMeasurementInputV2InitialAbsence(err) {
			return err
		}
		publication, err := publishValidatorEvidenceClosedCensusV2(ctx, closure, options, retained, readOptions)
		if err != nil {
			return err
		}
		if _, err := WriteValidatorEvidencePublicationV2Manifest(ctx, self.cfg.StateDir, publication, noIds, self.cfg.EvidenceV2.Bounds.MaxClosureBytes, self.cfg.EvidenceV2.Bounds.MaxParticipants); err != nil {
			return err
		}
		delete(self.publications, epoch)
		delete(self.publicationContexts, epoch)
	}
	return ctx.Err()
}

func (self *releaseRuntimeV2) advance(ctx context.Context, snapshot *ReleaseSnapshot) error {
	release, err := self.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	return self.advanceOwned(ctx, snapshot)
}

func (self *releaseRuntimeV2) advanceOwned(ctx context.Context, snapshot *ReleaseSnapshot) error {
	if snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() || len(self.runtimes) == 0 {
		return errors.New("release V2 live census or snapshot is unavailable")
	}
	if err := self.publish(ctx, snapshot); err != nil {
		return err
	}
	current := self.history.current[self.history.participants[0].NoID].epoch
	target := snapshot.Epoch.Uint64()
	if current > target {
		return errAttemptSettlementSnapshotStale
	}
	if current == target {
		return nil
	}
	for current < target {
		if err := ctx.Err(); err != nil {
			return err
		}
		next := current + 1
		if err := self.refreshServerKeys(ctx); err != nil {
			return err
		}
		_, boundary, err := self.window(ctx, snapshot, current)
		if err != nil {
			return err
		}
		contexts := make(map[uint64]AttemptCutV2Context, len(self.history.participants))
		for _, participant := range self.history.participants {
			contexts[participant.NoID], err = self.context(participant.NoID, self.history.current[participant.NoID], boundary)
			if err != nil {
				return err
			}
		}
		authority, err := self.authority(ctx, contexts, "terminal-replay")
		if err != nil {
			return err
		}
		replicas, err := releaseReservedAttemptCensusReplicasV2(&self.cfg, self.origins, self.runtimes)
		if err != nil {
			return err
		}
		options := AttemptSettlementRuntimeV2Options{Authority: authority, Persistence: self.cfg.EvidenceV2.Bounds.Persistence, Seal: make(map[uint64]AttemptCutV2SealOptions), PrivateKeys: make(map[uint64]ed25519.PrivateKey)}
		for _, participant := range self.history.participants {
			noId := participant.NoID
			options.Seal[noId], err = self.seal(ctx, noId, replicas[noId], "terminal-seal")
			if err != nil {
				return err
			}
			options.PrivateKeys[noId] = ed25519.PrivateKey(self.sources[noId].privateKey[:])
		}
		closure, err := AdvanceAttemptSettlementEpochV2(ctx, self.cfg.StateDir, self.history.participants, next, boundary, options)
		if err != nil {
			return err
		}
		self.history.terminal, self.history.terminalBefore = closure, maps.Clone(self.history.current)
		if self.history.terminalContexts == nil {
			self.history.terminalContexts = make(map[uint64]map[uint64]AttemptCutV2Context)
		}
		self.history.terminalContexts[current] = contexts
		self.history.terminals[current] = closure
		for _, transition := range closure.Transitions {
			noId := transition.Identity.NoID
			cursor := self.history.current[noId]
			cursor.epoch, cursor.first, cursor.egressFirst, cursor.generation, cursor.priorRoot = next, transition.Cut.LastSequence+1, transition.Cut.LastSequence+1, cursor.generation+1, transition.Cut.Root
			cursor.lastSequence, cursor.lastRoot, cursor.lastBoundary, cursor.prior, cursor.terminal = transition.Cut.LastSequence, transition.Cut.Root, boundary, slices.Clone(transition.PostFold), transition
			self.history.current[noId] = cursor
			delete(self.history.lastOrdinary, noId)
			delete(self.history.ordinaryContexts, noId)
		}
		self.publications[current], self.publicationContexts[current] = closure, contexts
		if err := self.publish(ctx, snapshot); err != nil {
			return err
		}
		if self.publishEpoch != nil {
			self.publishEpoch(next)
		}
		current = next
	}
	return nil
}

// A decision receives a detached exact closure for its own settlement epoch.
// A concurrent later refresh is a retry condition, never a different closure.
func (self *releaseRuntimeV2) closureForDecision(ctx context.Context, epoch uint64) (*AttemptSettlementClosureV2, error) {
	release, err := self.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	closure := self.history.terminal
	if closure == nil {
		return nil, nil
	}
	if closure.Epoch == ^uint64(0) || closure.Epoch+1 != epoch {
		return nil, errAttemptSettlementSnapshotStale
	}
	encoded, err := marshalAttemptSettlementV2JSON(ctx, closure, self.cfg.EvidenceV2.Bounds.MaxClosureBytes, false, true)
	if err != nil {
		return nil, err
	}
	return decodeAttemptSettlementClosureV2Bytes(ctx, encoded, self.cfg.EvidenceV2.Bounds.MaxClosureBytes, self.cfg.EvidenceV2.Bounds.MaxParticipants)
}

// Real native detach owns drain, signed streams, immutable input publication
// and durable egress rotation. Head selection and prepared native intent are
// separate actual consumers, supplied independent contexts rather than verdicts.
func (self *releaseRuntimeV2) collect(ctx context.Context, steerer *ReleaseSteerer, current *SteeringIntent, snapshot *ReleaseSnapshot, subnetEpoch, nativeBlock uint64, nativeHash string, hotkeys map[[32]byte]uint16) ([]ReleaseMeasurementInput, ReleaseMeasurementV2Options, error) {
	var zero ReleaseMeasurementV2Options
	release, err := self.acquire(ctx)
	if err != nil {
		return nil, zero, err
	}
	defer release()
	if steerer == nil || steerer.runtimeV2 != self || nativeBlock == 0 || hotkeys == nil {
		return nil, zero, errors.New("release V2 native collector owner differs")
	}
	if err := self.advanceOwned(ctx, snapshot); err != nil {
		return nil, zero, err
	}
	if err := self.history.provisionalClosedInputDeferral(ctx, current, subnetEpoch, nativeBlock, nativeHash, snapshot); err != nil {
		return nil, zero, err
	}
	if err := self.refreshServerKeys(ctx); err != nil {
		return nil, zero, err
	}
	uid, found := hotkeys[self.hotkey.PublicKey()]
	if !found {
		return nil, zero, errors.New("release V2 native decision omits its actual hotkey")
	}
	bounds := self.cfg.EvidenceV2.Bounds
	options := ReleaseMeasurementV2Options{Expected: ReleaseMeasurementV2Decision{DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: self.cfg.GenesisHash,
		Coordinator: self.cfg.Coordinator, SettlementVault: self.cfg.SettlementVault, ValidatorID: self.cfg.ValidatorID, Netuid: self.cfg.Netuid, SubnetEpoch: subnetEpoch,
		NativeSnapshotBlock: nativeBlock, NativeSnapshotHash: nativeHash, EVMSnapshotBlock: snapshot.BlockNumber, EVMSnapshotHash: attemptHex32(snapshot.BlockHash),
		SettlementEpoch: snapshot.Epoch.Uint64(), PolicyHash: self.cfg.PolicyHash, SelfUID: uid}, Policy: self.cfg.Policy, ControlledNOIDs: slices.Clone(self.cfg.ControlledNOIDs),
		Operators: make(map[uint64]ReleaseMeasurementV2OperatorOptions, len(self.history.participants)), MaxOperators: bounds.MaxOperators, MaxHeadEntries: bounds.MaxHeadEntries, MaxArtifactBytes: bounds.MaxArtifactBytes, MaxControlBytes: bounds.MaxControlBytes}
	replicas, err := releaseReservedAttemptCensusReplicasV2(&self.cfg, self.origins, self.runtimes)
	if err != nil {
		return nil, zero, err
	}
	inputs := make([]ReleaseMeasurementInput, 0, len(self.history.participants))
	for _, participant := range self.history.participants {
		noId, cursor := participant.NoID, self.history.current[participant.NoID]
		boundary := AttemptBoundary{SettlementEpoch: snapshot.Epoch.Uint64(), EVMBlock: snapshot.BlockNumber, EVMBlockHash: attemptHex32(snapshot.BlockHash)}
		expected, err := self.context(noId, cursor, boundary)
		if err != nil {
			return nil, zero, err
		}
		prior := self.history.lastOrdinary[noId]
		retry := prior != nil && prior.SubnetEpoch == subnetEpoch
		if prior != nil && prior.SubnetEpoch > subnetEpoch {
			return nil, zero, errors.New("release V2 native input epoch regressed")
		}
		if retry {
			var found bool
			expected, found = self.history.ordinaryContexts[noId]
			if !found {
				return nil, zero, errors.New("release V2 native retry lost independently replayed context")
			}
		}
		operator, err := self.operator(ctx, expected, "ordinary-stats")
		if err != nil {
			return nil, zero, err
		}
		seal, err := self.seal(ctx, noId, replicas[noId], "ordinary-seal")
		if err != nil {
			return nil, zero, err
		}
		inputOptions := releaseMeasurementInputV2Options{MaxJournalBytes: bounds.MaxInputJournalBytes,
			Stats: releaseStatsV2Options{Activation: expected.Activation, Policy: self.cfg.Policy, Bounds: bounds.Cut, Seal: seal,
				Stats: AttemptCutV2StatsOptions{ExpectedConfig: operator.Measurement.ExpectedConfig, MaxProviders: bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes, Replay: operator.Measurement.Replay}}}
		input, err := steerer.loadOrDetachReleaseMeasurementInputV2(ctx, noId, subnetEpoch, nativeBlock, nativeHash, snapshot, inputOptions)
		if err != nil {
			return nil, zero, err
		}
		if input.AttemptCutV2 == nil {
			return nil, zero, errors.New("release V2 native detach omitted its actual signed cut")
		}
		if err := input.AttemptCutV2.VerifyHeader(expected, bounds.Cut); err != nil {
			return nil, zero, err
		}
		// Retain an independently owned copy of the actual journal bytes for
		// native intent recovery; the returned candidate cannot mutate it.
		journalBytes, err := readReleaseMeasurementInputV2Context(ctx, releaseMeasurementInputV2Path(self.cfg.StateDir, subnetEpoch, noId), bounds.MaxInputJournalBytes, releaseMeasurementInputV2ReadHooks{})
		if err != nil {
			return nil, zero, err
		}
		journal, err := decodeReleaseMeasurementInputV2(ctx, journalBytes, inputOptions)
		if err != nil {
			return nil, zero, err
		}
		actual, err := marshalAttemptSettlementV2JSON(ctx, input, bounds.MaxInputJournalBytes, false, true)
		if err != nil {
			return nil, zero, err
		}
		retained, err := marshalAttemptSettlementV2JSON(ctx, journal.MeasurementInput, bounds.MaxInputJournalBytes, false, true)
		if err != nil || !bytes.Equal(actual, retained) {
			return nil, zero, errors.Join(errors.New("release V2 native journal changed after actual detach"), err)
		}
		if self.history.inputByEpoch[subnetEpoch] == nil {
			self.history.inputByEpoch[subnetEpoch] = make(map[uint64]*releaseMeasurementInputJournal)
		}
		self.history.inputByEpoch[subnetEpoch][noId] = journal
		if self.history.inputContextsByEpoch == nil {
			self.history.inputContextsByEpoch = make(map[uint64]map[uint64]AttemptCutV2Context)
		}
		if self.history.inputContextsByEpoch[subnetEpoch] == nil {
			self.history.inputContextsByEpoch[subnetEpoch] = make(map[uint64]AttemptCutV2Context)
		}
		self.history.inputContextsByEpoch[subnetEpoch][noId] = expected
		if !retry {
			cursor.egressFirst, cursor.generation = input.AttemptCutV2.LastSequence+1, cursor.generation+1
			cursor.lastSequence, cursor.lastRoot, cursor.lastBoundary = input.AttemptCutV2.LastSequence, input.AttemptCutV2.Root, expected.Boundary
			self.history.current[noId] = cursor
			self.history.lastOrdinary[noId] = journal
			if self.history.ordinaryContexts == nil {
				self.history.ordinaryContexts = make(map[uint64]AttemptCutV2Context)
			}
			self.history.ordinaryContexts[noId] = expected
		}
		// The following consumer needs a fresh physical replay name: the
		// ordinary statistics reconciliation already consumed its own one.
		operator, err = self.operator(ctx, expected, "ordinary-head")
		if err != nil {
			return nil, zero, err
		}
		options.Operators[noId] = ReleaseMeasurementV2OperatorOptions{Expected: expected, CutNativeBlock: input.CutNativeBlock, CutNativeBlockHash: input.CutNativeBlockHash, Bounds: bounds.Cut, Measurement: operator.Measurement}
		inputs = append(inputs, input)
	}
	if closure := self.history.terminal; closure != nil {
		if closure.Epoch+1 != snapshot.Epoch.Uint64() {
			return nil, zero, errors.New("release V2 prior terminal epoch differs")
		}
		authority, err := self.authority(ctx, self.history.terminalContexts[closure.Epoch], "measurement-terminal")
		if err != nil {
			return nil, zero, err
		}
		options.Settlement = &authority
	}
	return inputs, options, ctx.Err()
}

// Runtime refresh may add a new server-key version but never reinterpret an
// already observed version used by historical M8 signatures.
func (self *releaseRuntimeV2) refreshServerKeys(ctx context.Context) error {
	keys, err := readReleaseServerKeysV2(ctx, &self.cfg)
	if err != nil {
		return err
	}
	for noId, values := range keys {
		for version, key := range values {
			prior := self.history.keys[noId][version]
			if prior != nil && !bytes.Equal(prior, key) {
				return errors.New("release V2 server-key version was reinterpreted")
			}
		}
	}
	for noId, values := range keys {
		for version, key := range values {
			self.history.keys[noId][version] = bytes.Clone(key)
		}
	}
	return nil
}
