//go:build linux || darwin

// Read-only committed-prefix admission binds the signed service uid to the
// fixed protocol namespaces. It never opens a producer ledger or signing key.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const ProductionBootstrapCommittedSchema = "urnetwork-production-bootstrap-committed-prefix-v1"
const productionBootstrapCommittedMaximumBytes = 64 * 1024 * 1024
const productionBootstrapCommittedMaximumControl = 16 * 1024 * 1024
const productionBootstrapCommittedMaximumObjects = 8192

// This is a closed census of committed controls and their real proof tapes.
// The separate unsealed inventory retains liabilities and canonical tail
// boundaries without supplying intent graph authority or worker liveness.
type ProductionBootstrapCommittedObservation struct {
	Schema             string                                  `json:"schema"`
	ConfigHash         string                                  `json:"config_hash"`
	PolicyHash         string                                  `json:"policy_hash"`
	ClientDomainHash   string                                  `json:"client_domain_hash"`
	ServiceUid         uint32                                  `json:"service_uid"`
	Native             ProductionBootstrapNativePoint          `json:"native"`
	EvmBlock           uint64                                  `json:"evm_block"`
	EvmHash            string                                  `json:"evm_hash"`
	ApprovedPrefixHash string                                  `json:"approved_prefix_hash"`
	PreviousHash       string                                  `json:"previous_checkpoint_hash,omitempty"`
	CensusHash         string                                  `json:"source_census_hash"`
	SourceCount        uint64                                  `json:"source_count"`
	Prefixes           []ProductionBootstrapOperatorPrefix     `json:"prefixes"`
	HistoricalSources  bool                                    `json:"historical_sources_authenticated"`
	Unsealed           *ProductionBootstrapUnsealedObservation `json:"unsealed_inventory,omitempty"`
	ContentHash        string                                  `json:"content_hash"`
}

// Borrows the exact approved config and current operator observation. The
// mainnet caller obtains serviceUid from its original signed unit and scratch
// from separate host custody. Fixed history, ledger and intent names are read.
// All source owners and real closes succeed before any projection escapes.
func ObserveProductionBootstrapCommittedPrefix(ctx context.Context, path string, raw []byte, observed ProductionBootstrapObservation, approved ProductionBootstrapPrefixObservation, previous *ProductionBootstrapCommittedObservation, serviceUid uint32, scratch string) (result *ProductionBootstrapCommittedObservation, resultErr error) {
	if ctx == nil || serviceUid == 0 {
		return nil, errors.New("committed prefix requires an explicit non-root service owner")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	cfg, err := loadProductionBootstrapPrefixConfig(ctx, path, raw, observed)
	if err != nil {
		return nil, err
	}
	if scratch == cfg.StateDir || strings.HasPrefix(scratch, cfg.StateDir+string(filepath.Separator)) || strings.HasPrefix(cfg.StateDir, scratch+string(filepath.Separator)) {
		return nil, errors.New("committed prefix scratch overlaps producer state")
	}
	paths := []string{cfg.StateDir}
	for _, operator := range cfg.Operators {
		paths = append(paths, operator.StateDir)
	}
	for _, path := range paths {
		if err := checkProductionBootstrapStatePath(ctx, path, serviceUid); err != nil {
			return nil, err
		}
	}
	// Existing immutable readers retain both namespace descriptors and their
	// complete census through all replay and historical reads. No chmod/chown.
	bounds := cfg.EvidenceV2.Bounds
	bounds.MaxHistoryBytes = min(bounds.MaxHistoryBytes, uint64(productionBootstrapCommittedMaximumControl))
	history, err := readReleaseEvidenceV2HistoryFilesForUid(ctx, cfg.StateDir, bounds, serviceUid, productionBootstrapCommittedMaximumObjects, true, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, history.close())
		for _, path := range paths {
			resultErr = errors.Join(resultErr, checkProductionBootstrapStatePath(context.WithoutCancel(ctx), path, serviceUid))
		}
		if resultErr != nil {
			result = nil
		}
	}()
	options, err := captureProductionBootstrapCommitted(ctx, cfg, observed, history, scratch)
	if err != nil {
		return nil, err
	}
	archive, err := openReleaseEvidenceV2ArchiveHistory(ctx, options)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, archive.Close())
		if resultErr != nil {
			result = nil
		}
	}()
	result, err = projectProductionBootstrapCommitted(ctx, archive, options.Sources, observed, approved, previous, serviceUid)
	if err != nil {
		return nil, err
	}
	unsealed, liabilityOwner, err := readProductionBootstrapUnsealed(ctx, cfg, archive, result, previous, scratch, serviceUid, releaseMeasurementInputV2ReadHooks{})
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, liabilityOwner.close())
		if resultErr != nil {
			result = nil
		}
	}()
	result.Unsealed = unsealed
	// Pure transport retries keep the same completed replay, source bytes and
	// held directory census. No timeout can freshen or discard a checkpoint.
	err = retryProductionBootstrapPrefixRead(ctx, func(attempt context.Context) (attemptErr error) {
		defer func() { attemptErr = errors.Join(attemptErr, history.check(), liabilityOwner.check()) }()
		chain, err := DialReleaseChainContext(attempt, cfg.RPC, common.HexToAddress(cfg.Coordinator))
		if err != nil {
			return err
		}
		defer chain.Close()
		native, err := dialProductionNativeHistory(attempt, cfg)
		if err != nil {
			return err
		}
		defer native.API.Client.Close()
		if err := authenticateProductionBootstrapPrefix(attempt, cfg, archive.inputs, chain, native, releaseRuntimeIdentityV2(cfg)); err != nil {
			return err
		}
		if _, err := archive.ObserveSources(attempt, chain, native); err != nil {
			return err
		}
		result.Unsealed, err = liabilityOwner.authenticateTailBoundaries(attempt, archive, unsealed, chain)
		return err
	}, releaseHttpGetRetryHooks{})
	if err != nil {
		return nil, err
	}
	if err := errors.Join(history.check(), liabilityOwner.check()); err != nil {
		return nil, err
	}
	result.HistoricalSources = true
	result.ContentHash = productionBootstrapPrefixHash(*result)
	return result, nil
}

// Physical ancestors must belong to root or the selected service and exclude
// group/other writes. The protocol root itself is private and service-owned.
// No writable sticky ancestor or symlink is accepted, even for read-only use.
func checkProductionBootstrapStatePath(ctx context.Context, path string, serviceUid uint32) error {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || len(path) > 4096 {
		return errors.New("committed prefix state path is not canonical")
	}
	for directory := path; directory != "/"; directory = filepath.Dir(directory) {
		if err := ctx.Err(); err != nil {
			return err
		}
		owner, err := openAttemptPrivateDirectory(directory, ctx)
		if err != nil {
			return err
		}
		state := owner.anchor
		var admission error
		if state.mode&0o022 != 0 || state.uid != 0 && state.uid != serviceUid || directory == path && (state.uid != serviceUid || state.mode&0o077 != 0) {
			admission = errors.New("committed prefix state has an unprotected or differently owned ancestor")
		}
		if err := errors.Join(admission, owner.check(), owner.close()); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Capture is bounded independently of candidate metadata and deduplicates
// retained content. Every origin still supplies its own complete stream read.
// Signed setup pins and cut headers precede the first content-addressed fetch.
func captureProductionBootstrapCommitted(ctx context.Context, cfg *ReleaseConfig, observed ProductionBootstrapObservation, history *releaseEvidenceV2HistoryFiles, scratch string) (ReleaseEvidenceV2ArchiveOptions, error) {
	options := ReleaseEvidenceV2ArchiveOptions{Config: cfg, Hotkey: observed.Native.Hotkey, ScratchRoot: scratch, MaximumBytes: productionBootstrapCommittedMaximumBytes, MaximumObjects: productionBootstrapCommittedMaximumObjects}
	inputs, err := readProductionBootstrapPrefixInputs(ctx, cfg, observed)
	if err != nil || len(cfg.Operators) != 2 || history == nil {
		return options, errors.Join(errors.New("committed prefix original census is unavailable"), err)
	}
	options.Origins = [2]string{cfg.Operators[0].APIURL, cfg.Operators[1].APIURL}
	capture := ReleaseEvidenceV2CaptureOptions{Hotkey: options.Hotkey, Origins: options.Origins, MaximumBytes: options.MaximumBytes, MaximumObjects: options.MaximumObjects,
		MaximumDataBytes: productionBootstrapCommittedMaximumBytes - productionBootstrapCommittedMaximumControl, MaximumControlBytes: productionBootstrapCommittedMaximumControl, ReuseCapturedStreams: true}
	contents := map[string][]byte{}
	sources := map[ReleaseEvidenceV2CaptureSource]string{}
	budget, err := newReleaseEvidenceCaptureBudgetV2(ctx, capture, func(ctx context.Context, source ReleaseEvidenceV2CaptureSource, encoded []byte) error {
		hash := ReleaseMeasurementContentHash(encoded)
		if _, present := contents[hash]; !present {
			contents[hash] = bytes.Clone(encoded)
		}
		sources[source] = hash
		options.Sources = append(options.Sources, ReleaseEvidenceV2ArchiveSource{Source: source, SizeBytes: uint64(len(encoded)), ContentHash: hash})
		return ctx.Err()
	})
	if err != nil {
		return options, err
	}
	options.ReadSource = func(ctx context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, found := contents[sources[source]]
		if !found {
			return nil, errors.New("committed prefix retained source is absent")
		}
		return raw, nil
	}
	initials := map[uint64]ReleaseEvidenceV2ActivationContext{}
	for _, input := range inputs {
		initials[input.Config.NoID] = input.Context
		for _, field := range []struct {
			name string
			ref  ReleaseEvidenceV2File
		}{
			{name: "activation", ref: input.Config.Activation}, {name: "vpk-signature", ref: input.Config.VPKSignature}, {name: "hotkey-signature", ref: input.Config.HotkeySignature},
			{name: "context", ref: input.Config.Context}, {name: "history", ref: input.Config.History},
		} {
			raw, err := ReadReleaseEvidenceV2File(ctx, field.ref, min(cfg.EvidenceV2.Bounds.MaxControlBytes, uint64(productionBootstrapCommittedMaximumControl)))
			if err != nil {
				return options, err
			}
			if err := budget.emit(ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: fmt.Sprintf("activation/no-%d/%s", input.Config.NoID, field.name)}, raw); err != nil {
				return options, err
			}
		}
	}
	var cuts []*AttemptCutV2
	for _, input := range history.inputs {
		if input.legacy {
			return options, errors.New("committed prefix refuses unsupported legacy mutable inputs")
		}
		if err := budget.emit(ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "measurements/inputs/" + input.name}, input.encoded); err != nil {
			return options, err
		}
		var wire releaseMeasurementInputV2Wire
		if err := decodeAttemptStreamV2JSON(input.encoded, cfg.EvidenceV2.Bounds.MaxInputJournalBytes, &wire); err != nil {
			return options, err
		}
		if wire.Schema != releaseMeasurementInputV2Schema || wire.SubnetEpoch != input.epoch || wire.MeasurementInput.NoID != input.noID || wire.MeasurementInput.AttemptCutV2 == nil ||
			wire.MeasurementInput.CutNativeBlock > observed.Native.Block || wire.SubnetEpoch > observed.Native.Epoch {
			return options, errors.New("committed prefix input routing or current native ceiling differs")
		}
		cuts = append(cuts, wire.MeasurementInput.AttemptCutV2)
	}
	for _, member := range history.closures {
		if err := budget.emit(ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "settlement-closures-v2/" + member.name}, member.encoded); err != nil {
			return options, err
		}
		closure, err := decodeAttemptSettlementClosureV2Bytes(ctx, member.encoded, cfg.EvidenceV2.Bounds.MaxClosureBytes, cfg.EvidenceV2.Bounds.MaxParticipants)
		if err != nil || closure.Epoch != member.epoch || len(closure.Transitions) != 2 {
			return options, errors.Join(errors.New("committed prefix terminal census differs"), err)
		}
		for i, transition := range closure.Transitions {
			if transition.Identity.NoID != inputs[i].Config.NoID || transition.FromBoundary.SettlementEpoch != closure.Epoch {
				return options, errors.New("committed prefix terminal operator routing differs")
			}
			cuts = append(cuts, &transition.Cut)
		}
	}
	// Verify every header/domain before fetching either origin. Full record,
	// trail and prior-state authority still belongs to the existing replay.
	captureBounds := cfg.EvidenceV2.Bounds.Cut
	for _, limits := range []*AttemptStreamV2Bounds{&captureBounds.Records, &captureBounds.Proofs} {
		limits.MaxDataBytes = min(limits.MaxDataBytes, capture.MaximumDataBytes)
		limits.MaxChunkBytes = min(limits.MaxChunkBytes, limits.MaxDataBytes)
		limits.MaxManifestBytes = min(limits.MaxManifestBytes, capture.MaximumControlBytes)
		limits.MaxPageBytes = min(limits.MaxPageBytes, capture.MaximumControlBytes)
		limits.MaxChunks = min(limits.MaxChunks, capture.MaximumObjects)
		limits.MaxPages = min(limits.MaxPages, limits.MaxChunks)
	}
	for _, cut := range cuts {
		initial, found := initials[cut.Context.Identity.NoID]
		if !found || cut.Context.Identity != initial.InitialCut.Identity || cut.Context.Activation != initial.InitialCut.Activation ||
			cut.Context.Boundary.EVMBlock > observed.EvmBlock || cut.Context.Boundary.SettlementEpoch > observed.SettlementEpoch {
			return options, errors.New("committed prefix cut changes its original domain or current ceiling")
		}
		if err := cut.VerifyHeader(cut.Context, captureBounds); err != nil {
			return options, err
		}
	}
	keyConfig := *cfg
	keyConfig.EvidenceV2.Bounds.MaxControlBytes = min(keyConfig.EvidenceV2.Bounds.MaxControlBytes, uint64(productionBootstrapMaximumResponseBytes))
	if _, err := readReleaseServerKeysV2WithCapture(ctx, &keyConfig, budget.emit); err != nil {
		return options, err
	}
	streams := newReleaseCaptureStreamsV2(captureBounds, capture, budget.emit)
	for _, cut := range cuts {
		for _, origin := range options.Origins {
			for _, stream := range []struct {
				kind      string
				reference AttemptStreamV2Reference
				limits    AttemptStreamV2Bounds
			}{
				{kind: AttemptStreamV2Records, reference: cut.Records, limits: captureBounds.Records},
				{kind: AttemptStreamV2Proofs, reference: cut.Proofs, limits: captureBounds.Proofs},
			} {
				if err := retryProductionBootstrapPrefixRead(ctx, func(attempt context.Context) error {
					return streams.capture(attempt, origin, stream.kind, stream.reference, stream.limits)
				}, releaseHttpGetRetryHooks{}); err != nil {
					return options, err
				}
			}
		}
	}
	slices.SortFunc(options.Sources, func(a, b ReleaseEvidenceV2ArchiveSource) int {
		return strings.Compare(a.Source.Kind+"\x00"+a.Source.Origin+"\x00"+a.Source.Name, b.Source.Kind+"\x00"+b.Source.Origin+"\x00"+b.Source.Name)
	})
	return options, history.check()
}

// Projection requires real complete replay and both retained checkpoints as
// anchors. Even validly re-signed shorter history cannot erase a prior prefix.
func projectProductionBootstrapCommitted(ctx context.Context, archive *ReleaseEvidenceV2Archive, sources []ReleaseEvidenceV2ArchiveSource, observed ProductionBootstrapObservation, approved ProductionBootstrapPrefixObservation, previous *ProductionBootstrapCommittedObservation, serviceUid uint32) (*ProductionBootstrapCommittedObservation, error) {
	if archive == nil || archive.closed || archive.history == nil || archive.owner == nil || len(approved.Prefixes) != 2 {
		return nil, errors.New("committed prefix replay owner is absent")
	}
	initial, err := replayProductionBootstrapPrefix(ctx, &archive.owner.cfg, archive.inputs, archive.history.keys, observed)
	if err != nil {
		return nil, err
	}
	hash := approved.ContentHash
	approved.ContentHash = ""
	if hash != productionBootstrapPrefixHash(approved) || approved.Schema != ProductionBootstrapPrefixSchema || !approved.HistoricalSources || approved.CurrentPrefixProven ||
		approved.ConfigHash != initial.ConfigHash || approved.PolicyHash != initial.PolicyHash || approved.ClientDomainHash != initial.ClientDomainHash || approved.Native != observed.Native ||
		approved.EvmBlock != observed.EvmBlock || approved.EvmHash != observed.EvmHash || !reflect.DeepEqual(approved.Prefixes, initial.Prefixes) {
		return nil, errors.New("committed prefix does not extend its exact retained approved checkpoint")
	}
	result := &ProductionBootstrapCommittedObservation{Schema: ProductionBootstrapCommittedSchema, ConfigHash: initial.ConfigHash, PolicyHash: initial.PolicyHash, ClientDomainHash: initial.ClientDomainHash,
		ServiceUid: serviceUid, Native: observed.Native, EvmBlock: observed.EvmBlock, EvmHash: observed.EvmHash, ApprovedPrefixHash: hash, CensusHash: productionBootstrapPrefixHash(sources), SourceCount: uint64(len(sources))}
	for _, prefix := range initial.Prefixes {
		cursor := archive.history.current[prefix.NoId]
		if err := archive.owner.matchPrefix(ctx, prefix.NoId, prefix.LastSequence, prefix.Root); err != nil {
			return nil, err
		}
		if cursor.lastBoundary.EVMBlock > observed.EvmBlock || cursor.epoch > observed.SettlementEpoch {
			return nil, errors.New("committed prefix exceeds the current canonical observation")
		}
		prefix.LastSequence, prefix.Root, prefix.Generation, prefix.Epoch, prefix.PriorEmaHash = cursor.lastSequence, cursor.lastRoot, cursor.generation, cursor.epoch, productionBootstrapPrefixHash(cursor.prior)
		result.Prefixes = append(result.Prefixes, prefix)
	}
	for _, journals := range archive.history.inputByEpoch {
		for _, journal := range journals {
			if journal.SubnetEpoch > observed.Native.Epoch || journal.MeasurementInput.CutNativeBlock > observed.Native.Block {
				return nil, errors.New("committed prefix ordinary source exceeds the current native observation")
			}
		}
	}
	if previous != nil {
		prior := *previous
		result.PreviousHash, prior.ContentHash = prior.ContentHash, ""
		if result.PreviousHash != productionBootstrapPrefixHash(prior) || prior.Schema != result.Schema || !prior.HistoricalSources || prior.ConfigHash != result.ConfigHash || prior.PolicyHash != result.PolicyHash ||
			prior.ServiceUid != serviceUid || prior.Native.Hotkey != observed.Native.Hotkey || prior.Native.Block > observed.Native.Block || prior.Native.Epoch > observed.Native.Epoch || prior.EvmBlock > observed.EvmBlock ||
			prior.Native.Block == observed.Native.Block && prior.Native.Hash != observed.Native.Hash || prior.EvmBlock == observed.EvmBlock && prior.EvmHash != observed.EvmHash || len(prior.Prefixes) != 2 {
			return nil, errors.New("committed prefix previous checkpoint domain regressed")
		}
		for i, prefix := range prior.Prefixes {
			current := result.Prefixes[i]
			if prefix.NoId != current.NoId || prefix.ActivationHash != current.ActivationHash || prefix.HistoryHash != current.HistoryHash || prefix.Epoch > current.Epoch || prefix.Generation > current.Generation || prefix.LastSequence > current.LastSequence ||
				prefix.Epoch == current.Epoch && prefix.PriorEmaHash != current.PriorEmaHash {
				return nil, errors.New("committed prefix replaced its original operator checkpoint")
			}
			if err := archive.owner.matchPrefix(ctx, prefix.NoId, prefix.LastSequence, prefix.Root); err != nil {
				return nil, err
			}
		}
	}
	return result, ctx.Err()
}
