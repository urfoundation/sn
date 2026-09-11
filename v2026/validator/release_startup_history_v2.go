//go:build linux || darwin

// Startup reconstructs every current cursor from independently pinned initial
// history and the complete immutable ordinary/terminal census. Snapshot fields
// never select a replay prefix, current generation or predecessor EMA.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
)

// This private value is derived in order during one complete replay, not
// loaded from a snapshot. LastSequence/Root bind the actual retained ledger.
type releaseEvidenceV2StartupCursor struct {
	epoch        uint64
	first        uint64
	egressFirst  uint64
	generation   uint64
	priorRoot    string
	lastSequence uint64
	lastRoot     string
	lastBoundary AttemptBoundary
	prior        []AttemptSettlementQuality
	terminal     *AttemptSettlementTransitionV2
}

// The reader owns configuration, keys, input bytes and cursor maps before any
// transport callback. Results remain private startup data until every exact
// image is checked and the real all-operator mutation owner has completed.
type releaseEvidenceV2StartupHistory struct {
	cfg                ReleaseConfig
	retainedStartup    bool
	initial            map[uint64]ReleaseEvidenceV2ActivationContext
	keys               map[uint64]map[byte]ed25519.PublicKey
	participants       []AttemptSettlementRuntimeV2Participant
	states             map[uint64]*releaseAttemptState
	readers            [2]*HTTPAttemptStreamV2Reader
	files              *releaseEvidenceV2HistoryFiles
	images             *attemptSettlementV2StartupImages
	activationHistory  *AttemptSettlementClosure
	current            map[uint64]releaseEvidenceV2StartupCursor
	lastOrdinary       map[uint64]*releaseMeasurementInputJournal
	lastNative         map[uint64]*releaseMeasurementInputJournal
	lastOrdinaryBefore map[uint64]releaseEvidenceV2StartupCursor
	terminal           *AttemptSettlementClosureV2
	terminals          map[uint64]*AttemptSettlementClosureV2
	terminalBefore     map[uint64]releaseEvidenceV2StartupCursor
	// These are the independent inputs passed to successful real replays,
	// not contexts copied later from candidate header fields.
	terminalContexts     map[uint64]map[uint64]AttemptCutV2Context
	ordinaryContexts     map[uint64]AttemptCutV2Context
	inputContextsByEpoch map[uint64]map[uint64]AttemptCutV2Context
	journal              *attemptSettlementTransactionV2
	journalPreimages     []*StatsEngine
	journalPostimages    []*StatsEngine
	inputByEpoch         map[uint64]map[uint64]*releaseMeasurementInputJournal
	scratchPaths         []string
}

// Owns independent HTTP origins without installing publication callbacks in a
// read-only workflow. This retains the replicated sealer's same-origin grammar.
func newReleaseEvidenceV2StartupReaders(origins [2]string, bounds AttemptCutV2Bounds) ([2]*HTTPAttemptStreamV2Reader, error) {
	return newReleaseEvidenceV2ReadersWithMetadataLimit(origins, bounds, attemptStreamV2MetadataBytes(bounds))
}

// Complete typed payloads may exceed stream pages. Their caller supplies the
// independent finite allowance while both origins retain identical admission.
func newReleaseEvidenceV2ReadersWithMetadataLimit(origins [2]string, bounds AttemptCutV2Bounds, metadataBytes uint64) ([2]*HTTPAttemptStreamV2Reader, error) {
	var readers [2]*HTTPAttemptStreamV2Reader
	var canonical [2]string
	for index, origin := range origins {
		reader, err := newHttpAttemptStreamV2Reader(origin, bounds, metadataBytes)
		if err != nil {
			return [2]*HTTPAttemptStreamV2Reader{}, err
		}
		host := strings.ToLower(reader.endpoint.Hostname())
		if ip := net.ParseIP(host); ip != nil {
			host = ip.String()
		}
		port := reader.endpoint.Port()
		if port == "" {
			port = "443"
			if reader.endpoint.Scheme == "http" {
				port = "80"
			}
		}
		canonical[index] = reader.endpoint.Scheme + "://" + net.JoinHostPort(host, port)
		readers[index] = reader
	}
	if canonical[0] == canonical[1] {
		return [2]*HTTPAttemptStreamV2Reader{}, errors.New("startup history public origins are not distinct")
	}
	return readers, nil
}

// Every operation names a fresh child, never the configured scratch root.
// Native private parent acquisition refuses aliases and joins actual closes.
// Successful/failed scratch remains root-owned evidence for explicit cleanup.
func (self *releaseEvidenceV2StartupHistory) scratch(ctx context.Context, noID uint64, purpose string) (string, error) {
	var path string
	for _, operator := range self.cfg.EvidenceV2.Operators {
		if operator.NoID == noID {
			path = operator.ReplayScratchRoot
			break
		}
	}
	if path == "" || !validAttemptPrivateLeaf(purpose) {
		return "", errors.New("startup replay scratch ownership is absent")
	}
	parent, err := openReleaseMeasurementInputV2Parents(ctx, path)
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
	path = filepath.Join(path, "startup-"+purpose+"-"+hex.EncodeToString(nonce[:]))
	self.scratchPaths = append(self.scratchPaths, path)
	return path, ctx.Err()
}

// Statistics recovery needs complete signatures/proofs and exact EMA, not a
// miner-head eligibility decision. The head projection is deliberately empty
// and is never returned as head authority; root publication supplies its real
// independently observed current bindings in its separate full head workflow.
func (self *releaseEvidenceV2StartupHistory) operator(ctx context.Context, noID uint64, cursor releaseEvidenceV2StartupCursor, boundary AttemptBoundary, replica int, purpose string) (AttemptSettlementV2OperatorOptions, error) {
	initial, exists := self.initial[noID]
	if !exists || replica < 0 || replica >= len(self.readers) {
		return AttemptSettlementV2OperatorOptions{}, errors.New("startup replay operator or public origin is absent")
	}
	path, err := self.scratch(ctx, noID, purpose)
	if err != nil {
		return AttemptSettlementV2OperatorOptions{}, err
	}
	expected := initial.InitialCut
	expected.Boundary = boundary
	expected.FirstSequence, expected.EgressFirstSequence, expected.EgressGeneration, expected.PriorRoot = cursor.first, cursor.egressFirst, cursor.generation, cursor.priorRoot
	bounds := self.cfg.EvidenceV2.Bounds
	reader := self.readers[replica]
	return AttemptSettlementV2OperatorOptions{Expected: expected, Policy: self.cfg.Policy, Bounds: bounds.Cut, Measurement: AttemptCutV2MeasurementOptions{
		ExpectedConfig: ReleaseStatsConfig{AMin: self.cfg.Policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis},
		MaxProviders:   bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes, MaxFleetPrefixes: bounds.MaxFleetPrefixes,
		Replay: AttemptCutV2ReplayOptions{Bounds: bounds.Replay, ScratchDirectory: path, ServerKeys: self.keys[noID], ReadMetadata: reader.ReadMetadata, OpenData: reader.OpenData},
	}}, nil
}

// The complete configured census uses one independently authenticated boundary
// and separate scratch per participant/replica. Current cursor maps may not be
// populated from candidate snapshots or a caller-selected history prefix.
func (self *releaseEvidenceV2StartupHistory) authority(ctx context.Context, cursors map[uint64]releaseEvidenceV2StartupCursor, boundary AttemptBoundary, replica int, purpose string) (AttemptSettlementV2Options, error) {
	bounds := self.cfg.EvidenceV2.Bounds
	options := AttemptSettlementV2Options{Operators: make(map[uint64]AttemptSettlementV2OperatorOptions, len(self.participants)), MaxParticipants: bounds.MaxParticipants, MaxTransitionBytes: bounds.MaxTransitionBytes, MaxClosureBytes: bounds.MaxClosureBytes, retainedStartup: self.retainedStartup}
	if len(cursors) != len(self.participants) {
		return options, errors.New("startup authority cursor census is incomplete")
	}
	for _, participant := range self.participants {
		cursor, exists := cursors[participant.NoID]
		if !exists || cursor.epoch != boundary.SettlementEpoch {
			return AttemptSettlementV2Options{}, errors.New("startup authority cursor epoch or member differs")
		}
		operator, err := self.operator(ctx, participant.NoID, cursor, boundary, replica, purpose)
		if err != nil {
			return AttemptSettlementV2Options{}, err
		}
		options.Operators[participant.NoID] = operator
	}
	return ownAttemptSettlementV2Options(ctx, options)
}

// Independently known priors must all survive, including valid zero EMA. New
// providers cannot gain a self-declared prior simply by signing another cut.
func matchReleaseEvidenceV2StartupPrior(measurement ReleaseStatsMeasurement, prior []AttemptSettlementQuality) error {
	want := make(map[string]uint32, len(prior))
	for _, value := range prior {
		want[value.ClientID] = value.QualityPPM
	}
	seen := map[string]bool{}
	for _, provider := range measurement.Providers {
		value, exists := want[provider.ClientID]
		if provider.HasPriorQuality != exists || exists && provider.PriorQualityPPM != value || !exists && provider.PriorQualityPPM != 0 || seen[provider.ClientID] {
			return errors.New("startup cut prior EMA differs from its complete authenticated predecessor")
		}
		if exists {
			seen[provider.ClientID] = true
		}
	}
	if len(seen) != len(want) {
		return errors.New("startup cut omits an authenticated prior EMA provider")
	}
	return nil
}

// Matching the actual retained disk prefix joins every overlapping ordinary
// cut and terminal to the same globally lifecycle-checked ledger. No map grows
// with historical trail IDs and no later cut may replace an earlier hash chain.
func (self *releaseEvidenceV2StartupHistory) matchLedgerCut(ctx context.Context, noID, last uint64, root string) error {
	for _, participant := range self.participants {
		if participant.NoID != noID {
			continue
		}
		head, err := participant.Ledger.checkedHead()
		if err != nil || last > head.LastSequence {
			return errors.Join(errors.New("startup signed cut exceeds the retained actual ledger"), err)
		}
		actual, err := releaseStatsV2PrefixRoot(ctx, participant.Ledger, last)
		if err != nil || actual != root {
			return errors.Join(errors.New("startup signed cut differs from the retained actual ledger prefix"), err)
		}
		return nil
	}
	return errors.New("startup signed cut belongs to an unconfigured disk ledger")
}

// Full dual-origin replay precedes one deterministic egress rotation. Settlement
// counters and EMA do not fold at a native cut, including a journal written
// before any steering intent and before its snapshot rename succeeded.
func (self *releaseEvidenceV2StartupHistory) replayOrdinary(ctx context.Context, journal *releaseMeasurementInputJournal) error {
	input := journal.MeasurementInput
	cursor, exists := self.current[input.NoID]
	cut := input.AttemptCutV2
	if !exists || cut == nil || input.SettlementEpoch != cursor.epoch || cursor.generation == ^uint64(0) || cut.LastSequence == ^uint64(0) || cut.LastSequence < cursor.lastSequence || !releaseBlockAtOrBefore(cursor.lastBoundary.EVMBlock, cursor.lastBoundary.EVMBlockHash, input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash) || cursor.lastBoundary.SettlementEpoch < cursor.epoch && cursor.lastBoundary.EVMBlock >= input.CutEVMSnapshotBlock {
		return errors.New("startup ordinary history has an epoch, prefix or boundary gap")
	}
	if prior := self.lastNative[input.NoID]; prior != nil {
		if journal.SubnetEpoch <= prior.SubnetEpoch || !releaseBlockAtOrBefore(prior.MeasurementInput.CutNativeBlock, prior.MeasurementInput.CutNativeBlockHash, input.CutNativeBlock, input.CutNativeBlockHash) {
			return errors.New("startup ordinary native history is not strictly ordered")
		}
	}
	if input.EgressGeneration != cursor.generation || cut.Context.EgressGeneration != input.EgressGeneration || cut.Context.Boundary.SettlementEpoch != input.SettlementEpoch || cut.Context.Boundary.EVMBlock != input.CutEVMSnapshotBlock || cut.Context.Boundary.EVMBlockHash != input.CutEVMSnapshotHash {
		return errors.New("startup ordinary labels differ from the independent current cursor")
	}
	if err := matchReleaseEvidenceV2StartupPrior(input.Stats, cursor.prior); err != nil {
		return err
	}
	replicas := len(self.readers)
	if self.retainedStartup {
		replicas = 1
	}
	for replica := 0; replica < replicas; replica++ {
		operator, err := self.operator(ctx, input.NoID, cursor, cut.Context.Boundary, replica, "ordinary")
		if err != nil {
			return err
		}
		if self.retainedStartup {
			err = admitProvisionalRetainedStatsV2(ctx, input.Stats, *cut, operator)
		} else {
			_, _, err = VerifyReleaseStatsMeasurementWithAttemptCutV2(ctx, input.Stats, *cut, operator.Expected, operator.Policy, operator.Bounds, AttemptCutV2StatsOptions{ExpectedConfig: operator.Measurement.ExpectedConfig, MaxProviders: operator.Measurement.MaxProviders, MaxEgressHashes: operator.Measurement.MaxEgressHashes, Replay: operator.Measurement.Replay})
		}
		if err != nil {
			return fmt.Errorf("startup no_id %d origin %d ordinary replay: %w", input.NoID, replica, err)
		}
		if replica == 0 {
			if self.ordinaryContexts == nil {
				self.ordinaryContexts = make(map[uint64]AttemptCutV2Context)
			}
			self.ordinaryContexts[input.NoID] = operator.Expected
			if self.inputContextsByEpoch == nil {
				self.inputContextsByEpoch = make(map[uint64]map[uint64]AttemptCutV2Context)
			}
			if self.inputContextsByEpoch[journal.SubnetEpoch] == nil {
				self.inputContextsByEpoch[journal.SubnetEpoch] = make(map[uint64]AttemptCutV2Context)
			}
			self.inputContextsByEpoch[journal.SubnetEpoch][input.NoID] = operator.Expected
		}
	}
	if err := self.matchLedgerCut(ctx, input.NoID, cut.LastSequence, cut.Root); err != nil {
		return err
	}
	self.lastOrdinaryBefore[input.NoID] = cursor
	self.lastOrdinary[input.NoID] = journal
	self.lastNative[input.NoID] = journal
	cursor.egressFirst, cursor.generation = cut.LastSequence+1, cursor.generation+1
	cursor.lastSequence, cursor.lastRoot, cursor.lastBoundary = cut.LastSequence, cut.Root, cut.Context.Boundary
	self.current[input.NoID] = cursor
	return ctx.Err()
}

// A terminal requires every configured member and an exact consecutive epoch.
// The existing complete signed batch verifier independently reconstructs each
// post-fold EMA at both public origins before any current cursor is advanced.
func (self *releaseEvidenceV2StartupHistory) replayTerminal(ctx context.Context, closure *AttemptSettlementClosureV2) error {
	if closure == nil || len(closure.Transitions) != len(self.participants) || len(closure.Transitions) == 0 || closure.Transitions[0] == nil || closure.Epoch == ^uint64(0) {
		return errors.New("startup terminal history omits the configured census")
	}
	boundary := closure.Transitions[0].FromBoundary
	for index, participant := range self.participants {
		transition, cursor := closure.Transitions[index], self.current[participant.NoID]
		if transition == nil || transition.Identity.NoID != participant.NoID || transition.FromBoundary != boundary || cursor.epoch != closure.Epoch || transition.ToEpoch != cursor.epoch+1 || transition.Cut.LastSequence == ^uint64(0) || transition.Cut.LastSequence < cursor.lastSequence || cursor.generation == ^uint64(0) || !releaseBlockAtOrBefore(cursor.lastBoundary.EVMBlock, cursor.lastBoundary.EVMBlockHash, boundary.EVMBlock, boundary.EVMBlockHash) || cursor.lastBoundary.SettlementEpoch < cursor.epoch && cursor.lastBoundary.EVMBlock >= boundary.EVMBlock {
			return errors.New("startup terminal chronology, boundary or complete membership differs")
		}
		if err := matchReleaseEvidenceV2StartupPrior(transition.PreFold, cursor.prior); err != nil {
			return err
		}
	}
	var contexts map[uint64]AttemptCutV2Context
	replicas := len(self.readers)
	if self.retainedStartup {
		replicas = 1
	}
	for replica := 0; replica < replicas; replica++ {
		options, err := self.authority(ctx, self.current, boundary, replica, "terminal")
		if err != nil {
			return err
		}
		if self.retainedStartup {
			_, err = admitProvisionalRetainedSettlementV2(ctx, closure, options)
		} else {
			_, err = VerifyAttemptSettlementClosureV2(ctx, closure, options)
		}
		if err != nil {
			return fmt.Errorf("startup origin %d complete terminal replay: %w", replica, err)
		}
		if replica == 0 {
			contexts = make(map[uint64]AttemptCutV2Context, len(options.Operators))
			for noID, operator := range options.Operators {
				contexts[noID] = operator.Expected
			}
		}
	}
	for _, transition := range closure.Transitions {
		if err := self.matchLedgerCut(ctx, transition.Identity.NoID, transition.Cut.LastSequence, transition.Cut.Root); err != nil {
			return err
		}
	}
	self.terminalBefore = maps.Clone(self.current)
	self.terminal = closure
	self.terminals[closure.Epoch] = closure
	if self.terminalContexts == nil {
		self.terminalContexts = make(map[uint64]map[uint64]AttemptCutV2Context)
	}
	self.terminalContexts[closure.Epoch] = contexts
	for _, transition := range closure.Transitions {
		noID := transition.Identity.NoID
		cursor := self.current[noID]
		cursor.epoch = transition.ToEpoch
		cursor.first, cursor.egressFirst, cursor.generation, cursor.priorRoot = transition.Cut.LastSequence+1, transition.Cut.LastSequence+1, cursor.generation+1, transition.Cut.Root
		cursor.lastSequence, cursor.lastRoot, cursor.lastBoundary = transition.Cut.LastSequence, transition.Cut.Root, boundary
		cursor.prior = slices.Clone(transition.PostFold)
		cursor.terminal = transition
		self.current[noID] = cursor
		delete(self.lastOrdinary, noID)
		delete(self.lastOrdinaryBefore, noID)
		delete(self.ordinaryContexts, noID)
	}
	return ctx.Err()
}

// Production always supplies the configured reviewed native artifact; the
// private explicit-artifact path lets actual SDK/RPC fixtures test this same
// complete reader without fabricating an already-authenticated observation.
func readReleaseEvidenceV2StartupHistory(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey, origins [2]string, disk *releaseEvidenceV2DiskState) (*releaseEvidenceV2StartupHistory, error) {
	if cfg == nil {
		return nil, errors.New("startup history configuration is absent")
	}
	runtime := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
	return readReleaseEvidenceV2StartupHistoryWithRuntime(ctx, cfg, chain, native, inputs, serverKeys, origins, disk, runtime)
}

// Complete admission owns every borrowed configuration/key/byte input before
// the first historical RPC or public stream callback. There are no live Stats
// writes in this reader, even when a durable transaction is partly complete.
func readReleaseEvidenceV2StartupHistoryWithRuntime(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey, origins [2]string, disk *releaseEvidenceV2DiskState, runtime crv4.RuntimeArtifactIdentity) (result *releaseEvidenceV2StartupHistory, resultErr error) {
	if ctx == nil || cfg == nil || chain == nil || native == nil || disk == nil {
		return nil, errors.New("startup history root, chain or configuration owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(inputs) != len(cfg.Operators) || len(disk.participants) != len(inputs) || len(disk.states) != len(inputs) || len(inputs) == 0 || uint64(len(serverKeys)) > cfg.EvidenceV2.Bounds.MaxOperators {
		return nil, errors.New("startup history configured input or disk census differs")
	}
	owned := &releaseEvidenceV2StartupHistory{cfg: *cfg, initial: map[uint64]ReleaseEvidenceV2ActivationContext{}, keys: map[uint64]map[byte]ed25519.PublicKey{}, participants: slices.Clone(disk.participants), current: map[uint64]releaseEvidenceV2StartupCursor{}, lastOrdinary: map[uint64]*releaseMeasurementInputJournal{}, lastNative: map[uint64]*releaseMeasurementInputJournal{}, lastOrdinaryBefore: map[uint64]releaseEvidenceV2StartupCursor{}, inputByEpoch: map[uint64]map[uint64]*releaseMeasurementInputJournal{}}
	owned.states = maps.Clone(disk.states)
	owned.terminals = map[uint64]*AttemptSettlementClosureV2{}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owned.files.close())
			result = nil
		}
	}()
	owned.cfg.Operators = slices.Clone(cfg.Operators)
	owned.cfg.EvidenceV2.Operators = slices.Clone(cfg.EvidenceV2.Operators)
	owned.cfg.ControlledNOIDs = slices.Clone(cfg.ControlledNOIDs)
	owned.cfg.RPC, owned.cfg.Substrate = slices.Clone(cfg.RPC), slices.Clone(cfg.Substrate)
	owned.cfg.Policy.Deposit.Tiers = slices.Clone(cfg.Policy.Deposit.Tiers)
	inputs = slices.Clone(inputs)
	for index := range inputs {
		input := &inputs[index]
		if uint64(len(input.HistoryBytes)) > owned.cfg.EvidenceV2.Bounds.MaxHistoryBytes || len(input.PrivateKey) != ed25519.PrivateKeySize || len(input.VPKSignature) != ed25519.SignatureSize || len(input.HotkeySignature) != 64 || input.Config.NoID != owned.cfg.EvidenceV2.Operators[index].NoID || input.Config.NoID != owned.participants[index].NoID {
			return nil, errors.New("startup history activation/cursor member census differs")
		}
		input.HistoryBytes = bytes.Clone(input.HistoryBytes)
		input.PrivateKey, input.VPKSignature, input.HotkeySignature = bytes.Clone(input.PrivateKey), bytes.Clone(input.VPKSignature), bytes.Clone(input.HotkeySignature)
		owned.initial[input.Config.NoID] = input.Context
		keys := make(map[byte]ed25519.PublicKey, len(serverKeys[input.Config.NoID]))
		for version, key := range serverKeys[input.Config.NoID] {
			if len(key) != ed25519.PublicKeySize {
				return nil, errors.New("startup history server-key input has invalid width")
			}
			keys[version] = bytes.Clone(key)
		}
		owned.keys[input.Config.NoID] = keys
		participant := owned.participants[index]
		state := owned.states[participant.NoID]
		if state == nil || state.stats != participant.Stats || state.ledger != participant.Ledger || state.store != nil {
			return nil, errors.New("startup history disk destinations differ from the dormant acquisition plan")
		}
		var configured *OperatorConfig
		for operatorIndex := range owned.cfg.Operators {
			if owned.cfg.Operators[operatorIndex].NoID == participant.NoID {
				configured = &owned.cfg.Operators[operatorIndex]
				break
			}
		}
		if configured == nil || configured.StateDir != participant.StateDir || participant.Ledger == nil || participant.Ledger.identity != input.Context.InitialCut.Identity || participant.Stats == nil || participant.Stats.attemptLedger != nil || participant.Stats.settlementEpochKnown {
			return nil, errors.New("startup history does not own the complete dormant configured disk census")
		}
		if index > 0 && participant.NoID <= owned.participants[index-1].NoID {
			return nil, errors.New("startup history disk census is not canonically ordered")
		}
	}
	for noID := range serverKeys {
		if _, exists := owned.initial[noID]; !exists {
			return nil, errors.New("startup history server-key census contains an unconfigured operator")
		}
	}
	owned.retainedStartup = provisionalRetainedStartupHistory(inputs)
	var err error
	owned.images, err = newAttemptSettlementV2StartupImages(ctx, owned.cfg.StateDir, owned.participants, disk.snapshotBytes, disk.snapshotPresent, disk.journalBytes, disk.journalPresent, owned.cfg.EvidenceV2.Bounds.Persistence)
	if err != nil {
		return nil, err
	}
	owned.readers, err = newReleaseEvidenceV2StartupReaders(origins, owned.cfg.EvidenceV2.Bounds.Cut)
	if err != nil {
		return nil, err
	}
	owned.files, err = readReleaseEvidenceV2HistoryFiles(ctx, owned.cfg.StateDir, owned.cfg.EvidenceV2.Bounds, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		return nil, err
	}
	owned.activationHistory, err = authenticateReleaseEvidenceV2InitialHistory(ctx, &owned.cfg, chain, inputs, owned.keys)
	if err != nil {
		return nil, err
	}
	retainedHistoricalRPC := provisionalRetainedStartupHistory(inputs)
	for index, input := range inputs {
		initial := input.Context.InitialCut
		cursor := releaseEvidenceV2StartupCursor{epoch: initial.Boundary.SettlementEpoch, first: initial.FirstSequence, egressFirst: initial.EgressFirstSequence, generation: initial.EgressGeneration, priorRoot: initial.PriorRoot, lastSequence: initial.FirstSequence - 1, lastRoot: initial.PriorRoot, lastBoundary: initial.Boundary}
		if owned.activationHistory != nil {
			cursor.prior = slices.Clone(owned.activationHistory.Transitions[index].PostFold)
		}
		owned.current[input.Config.NoID] = cursor
		if err := owned.matchLedgerCut(ctx, input.Config.NoID, cursor.lastSequence, cursor.lastRoot); err != nil {
			return nil, err
		}
	}
	var ordinary []*releaseMeasurementInputJournal
	for _, member := range owned.files.inputs {
		initial, exists := owned.initial[member.noID]
		if !exists {
			return nil, errors.New("startup input history contains an unconfigured operator")
		}
		journal, err := owned.decodeInput(ctx, member)
		if err != nil {
			return nil, err
		}
		if owned.inputByEpoch[journal.SubnetEpoch] == nil {
			owned.inputByEpoch[journal.SubnetEpoch] = map[uint64]*releaseMeasurementInputJournal{}
		}
		if owned.inputByEpoch[journal.SubnetEpoch][member.noID] != nil {
			return nil, errors.New("startup ordinary history repeats a native-epoch member across versions")
		}
		owned.inputByEpoch[journal.SubnetEpoch][member.noID] = journal
		input := journal.MeasurementInput
		observationCtx, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(&owned.cfg))
		err = authenticateReleaseStartupNativeV2ContextWithRetainedHistory(observationCtx, native, initial, journal, runtime, member.legacy, retainedHistoricalRPC)
		cancel()
		if err != nil {
			return nil, err
		}
		boundary := AttemptBoundary{SettlementEpoch: input.SettlementEpoch, EVMBlock: input.CutEVMSnapshotBlock, EVMBlockHash: input.CutEVMSnapshotHash}
		if err := chain.authenticateReleaseStartupBoundaryV2ContextWithRetainedHistory(ctx, initial.InitialCut.Activation.Domain, member.noID, boundary, false, retainedHistoricalRPC); err != nil {
			return nil, err
		}
		if member.legacy {
			if err := owned.replayLegacyInput(ctx, journal); err != nil {
				return nil, err
			}
		} else {
			ordinary = append(ordinary, journal)
		}
	}
	closures := make([]*AttemptSettlementClosureV2, 0, len(owned.files.closures)+1)
	closureBytes := map[uint64][]byte{}
	for _, member := range owned.files.closures {
		closure, err := decodeAttemptSettlementClosureV2Bytes(ctx, member.encoded, owned.cfg.EvidenceV2.Bounds.MaxClosureBytes, owned.cfg.EvidenceV2.Bounds.MaxParticipants)
		if err != nil || closure.Epoch != member.epoch {
			return nil, errors.Join(errors.New("startup terminal filename differs from its canonical closure"), err)
		}
		closures = append(closures, closure)
		closureBytes[member.epoch] = member.encoded
		if err := owned.images.addClosure(ctx, owned.cfg.StateDir, member.epoch, member.encoded, owned.cfg.EvidenceV2.Bounds.MaxClosureBytes); err != nil {
			return nil, err
		}
	}
	if image := owned.images.paths[attemptSettlementTransactionV2Path(owned.cfg.StateDir)]; image.present {
		owned.journal, owned.journalPreimages, owned.journalPostimages, err = decodeAttemptSettlementTransactionV2(ctx, image.encoded, owned.participants, owned.cfg.EvidenceV2.Bounds.Persistence)
		if err != nil {
			return nil, err
		}
		if owned.journal.Kind == "advance" {
			closure, err := decodeAttemptSettlementClosureV2Bytes(ctx, owned.journal.ClosureJSON, owned.cfg.EvidenceV2.Bounds.MaxClosureBytes, owned.cfg.EvidenceV2.Bounds.MaxParticipants)
			if err != nil || closure.Epoch == ^uint64(0) || closure.Epoch+1 != owned.journal.Epoch {
				return nil, errors.Join(errors.New("startup pending terminal journal epoch differs"), err)
			}
			if prior, exists := closureBytes[closure.Epoch]; exists {
				if !bytes.Equal(prior, owned.journal.ClosureJSON) {
					return nil, errors.New("startup pending journal differs from its immutable terminal")
				}
			} else {
				closures = append(closures, closure)
			}
		} else if len(ordinary) != 0 || len(closures) != 0 {
			return nil, errors.New("startup activation journal cannot precede already advanced history")
		}
	}
	slices.SortFunc(ordinary, func(left, right *releaseMeasurementInputJournal) int {
		for _, pair := range [][2]uint64{{left.MeasurementInput.SettlementEpoch, right.MeasurementInput.SettlementEpoch}, {left.SubnetEpoch, right.SubnetEpoch}, {left.MeasurementInput.NoID, right.MeasurementInput.NoID}} {
			if pair[0] < pair[1] {
				return -1
			}
			if pair[0] > pair[1] {
				return 1
			}
		}
		return 0
	})
	slices.SortFunc(closures, func(left, right *AttemptSettlementClosureV2) int {
		if left.Epoch < right.Epoch {
			return -1
		}
		if left.Epoch > right.Epoch {
			return 1
		}
		return 0
	})
	nextInput := 0
	for _, closure := range closures {
		if closure.Epoch != owned.current[owned.participants[0].NoID].epoch || len(closure.Transitions) != len(owned.participants) {
			return nil, errors.New("startup terminal history has a gap or a partial operator census")
		}
		for nextInput < len(ordinary) && ordinary[nextInput].MeasurementInput.SettlementEpoch <= closure.Epoch {
			if err := owned.replayOrdinary(ctx, ordinary[nextInput]); err != nil {
				return nil, err
			}
			nextInput++
		}
		for index, participant := range owned.participants {
			transition := closure.Transitions[index]
			if transition == nil || transition.Identity.NoID != participant.NoID {
				return nil, errors.New("startup terminal operator order differs from configured history")
			}
			if err := chain.authenticateReleaseStartupBoundaryV2ContextWithRetainedHistory(ctx, owned.initial[participant.NoID].InitialCut.Activation.Domain, participant.NoID, transition.FromBoundary, true, retainedHistoricalRPC); err != nil {
				return nil, err
			}
		}
		if err := owned.replayTerminal(ctx, closure); err != nil {
			return nil, err
		}
	}
	for ; nextInput < len(ordinary); nextInput++ {
		if err := owned.replayOrdinary(ctx, ordinary[nextInput]); err != nil {
			return nil, err
		}
	}
	if owned.journal != nil && owned.journal.Kind == "advance" {
		if owned.terminal == nil || owned.terminal.Epoch+1 != owned.journal.Epoch || len(owned.lastOrdinary) != 0 {
			return nil, errors.New("startup pending journal is not the independently replayed latest terminal")
		}
	}
	if err := owned.files.check(); err != nil {
		return nil, err
	}
	return owned, ctx.Err()
}

// Both private journal schemas are decoded explicitly from their bounded
// namespace. Canonical outer labels must match configured deployment authority,
// never a context copied from the candidate cut itself.
func (self *releaseEvidenceV2StartupHistory) decodeInput(ctx context.Context, member releaseEvidenceV2HistoryFile) (*releaseMeasurementInputJournal, error) {
	var journal *releaseMeasurementInputJournal
	var err error
	if member.legacy {
		journal, err = decodeReleaseMeasurementInput(member.encoded)
	} else {
		bounds := self.cfg.EvidenceV2.Bounds
		journal, err = decodeReleaseMeasurementInputV2(ctx, member.encoded, releaseMeasurementInputV2Options{MaxJournalBytes: bounds.MaxInputJournalBytes, Stats: releaseStatsV2Options{Policy: self.cfg.Policy, Bounds: bounds.Cut, Stats: AttemptCutV2StatsOptions{ExpectedConfig: ReleaseStatsConfig{AMin: self.cfg.Policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis}, MaxProviders: bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes}}})
	}
	if err != nil {
		return nil, err
	}
	if journal.DeploymentID != self.cfg.DeploymentID || journal.ChainID != self.cfg.ChainID || !strings.EqualFold(journal.GenesisHash, self.cfg.GenesisHash) || !strings.EqualFold(journal.Coordinator, self.cfg.Coordinator) || journal.ValidatorID != self.cfg.ValidatorID || journal.Netuid != self.cfg.Netuid || !strings.EqualFold(journal.PolicyHash, self.cfg.PolicyHash) || journal.SubnetEpoch != member.epoch || journal.MeasurementInput.NoID != member.noID {
		return nil, errors.New("startup input history differs from configured identity or canonical filename")
	}
	return journal, ctx.Err()
}

// Old ordinary records are replayed explicitly but never select V2's initial
// generation: that exact cut remains independently pinned by configuration and
// the fully replayed complete legacy closure history, including nonzero EMA.
func (self *releaseEvidenceV2StartupHistory) replayLegacyInput(ctx context.Context, journal *releaseMeasurementInputJournal) error {
	input := journal.MeasurementInput
	initial := self.initial[input.NoID].InitialCut
	cut := input.Stats.AttemptCut
	bounds := self.cfg.EvidenceV2.Bounds
	if journal.Schema != releaseMeasurementInputSchema || input.AttemptCutV2 != nil || cut == nil || cut.Identity != initial.Identity || cut.LastSequence >= initial.FirstSequence || input.SettlementEpoch >= initial.Boundary.SettlementEpoch || input.EgressGeneration >= initial.EgressGeneration || uint64(len(cut.Records)) > bounds.Disk.MaxRecordCount || uint64(len(input.Stats.Providers)) > bounds.MaxProviders || cut.Boundary.SettlementEpoch != input.SettlementEpoch || cut.Boundary.EVMBlock != input.CutEVMSnapshotBlock || cut.Boundary.EVMBlockHash != input.CutEVMSnapshotHash {
		return errors.New("startup legacy input is not an explicit predecessor of the pinned activation")
	}
	vpk, err := canonicalAttemptHex32("startup legacy validator", initial.Identity.ValidatorVPK, false)
	if err != nil {
		return err
	}
	if err := VerifyAttemptLedgerCut(cut, vpk[:], self.keys[input.NoID]); err != nil {
		return err
	}
	if _, err := VerifyReleaseStatsMeasurement(input.Stats); err != nil {
		return err
	}
	return self.matchLedgerCut(ctx, input.NoID, cut.LastSequence, cut.Root)
}
