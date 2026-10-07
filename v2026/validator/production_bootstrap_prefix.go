//go:build linux || darwin

// Bootstrap proof admission replays the independently pinned activation
// prefix. Live mutable history and worker progress remain separate domains.
package validator

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
)

const ProductionBootstrapPrefixSchema = "urnetwork-production-bootstrap-approved-prefix-v1"
const productionBootstrapPrefixMaximumBytes = 16 * 1024 * 1024
const productionBootstrapPrefixMaximumClosures = 256

// This is a verified historical checkpoint, never a claim that a running
// worker has no later records, pending trails or unobserved steering liability.
type ProductionBootstrapOperatorPrefix struct {
	NoId           uint64 `json:"no_id"`
	ActivationHash string `json:"activation_hash"`
	HistoryHash    string `json:"history_hash"`
	LastSequence   uint64 `json:"last_sequence"`
	Root           string `json:"root"`
	Generation     uint64 `json:"generation"`
	Epoch          uint64 `json:"epoch"`
	PriorEmaHash   string `json:"prior_ema_hash"`
}

// ClientDomainHash commits the independently authenticated current operator
// response census. It does not turn those current keys into historical keys.
type ProductionBootstrapPrefixObservation struct {
	Schema              string                              `json:"schema"`
	ConfigHash          string                              `json:"config_hash"`
	PolicyHash          string                              `json:"policy_hash"`
	ClientDomainHash    string                              `json:"client_domain_hash"`
	Native              ProductionBootstrapNativePoint      `json:"native"`
	EvmBlock            uint64                              `json:"evm_block"`
	EvmHash             string                              `json:"evm_hash"`
	Prefixes            []ProductionBootstrapOperatorPrefix `json:"prefixes"`
	HistoricalSources   bool                                `json:"historical_sources_authenticated"`
	CurrentPrefixProven bool                                `json:"current_mutable_prefix_proven"`
	ContentHash         string                              `json:"content_hash"`
}

// Borrows exact original configuration and the caller's current authenticated
// operator projection. Only public pinned inputs and read-only RPC/API methods
// are consumed. No mutable validator state, signing key or producer is opened.
// The fixed total budget owns 60 second remote attempts and 300 second retries.
func ObserveProductionBootstrapPrefix(ctx context.Context, path string, raw []byte, observed ProductionBootstrapObservation) (result *ProductionBootstrapPrefixObservation, resultErr error) {
	if ctx == nil {
		return nil, errors.New("bootstrap prefix context is absent")
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
	inputs, err := readProductionBootstrapPrefixInputs(ctx, cfg, observed)
	if err != nil {
		return nil, err
	}
	keyConfig := *cfg
	keyConfig.EvidenceV2.Bounds.MaxControlBytes = min(keyConfig.EvidenceV2.Bounds.MaxControlBytes, uint64(productionBootstrapMaximumResponseBytes))
	keys, err := readReleaseServerKeysV2(ctx, &keyConfig)
	if err != nil {
		return nil, err
	}
	result, err = replayProductionBootstrapPrefix(ctx, cfg, inputs, keys, observed)
	if err != nil {
		return nil, err
	}
	// Mathematical replay completes once. Transient historical reads retry
	// against the same retained inputs; they never restart or freshen that work.
	err = retryProductionBootstrapPrefixRead(ctx, func(attempt context.Context) error {
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
		return authenticateProductionBootstrapPrefix(attempt, cfg, inputs, chain, native, releaseRuntimeIdentityV2(cfg))
	}, releaseHttpGetRetryHooks{})
	if err != nil {
		return nil, err
	}
	result.HistoricalSources = true
	result.ContentHash = productionBootstrapPrefixHash(*result)
	return result, nil
}

// Both approved and committed prefix readers load exactly the independently
// approved public configuration. This never calls the producer/key loader.
func loadProductionBootstrapPrefixConfig(ctx context.Context, path string, raw []byte, observed ProductionBootstrapObservation) (*ReleaseConfig, error) {
	inspection, err := InspectProductionBootstrapConfig(ctx, path, raw)
	if err != nil {
		return nil, err
	}
	approval := inspection.Approval
	if observed.ConfigHash != fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) || observed.DeploymentId != inspection.DeploymentId || observed.ValidatorId != inspection.ValidatorId ||
		approval.Production == nil || !slices.Contains(approval.Production.ValidatorHotkeys, observed.Native.Hotkey) || observed.Native.Hash == ([32]byte{}) ||
		observed.Native.Block < approval.ValidFromNativeBlock || observed.Native.Block > approval.ValidThroughNativeBlock ||
		observed.Native.Epoch < approval.FirstNativeEpoch || observed.Native.Epoch > approval.Production.ValidThroughNativeEpoch || observed.EvmBlock == 0 {
		return nil, errors.New("bootstrap prefix differs from its original config or current native scope")
	}
	if _, err := canonicalAttemptHex32("prefix current EVM hash", observed.EvmHash, false); err != nil {
		return nil, err
	}
	cfg, err := decodeReleaseConfigDocument(path, raw)
	if err != nil {
		return nil, err
	}
	cfg.Coordinator, cfg.SettlementVault = strings.ToLower(cfg.Coordinator), strings.ToLower(cfg.SettlementVault)
	approvalRaw, err := ReadReleaseEvidenceV2File(ctx, inspection.ApprovalReference, maximumOwnerRecycleApprovalBytes)
	if err != nil {
		return nil, err
	}
	if err := loadOwnerRecycleProductionConfigBytes(cfg, approvalRaw); err != nil {
		return nil, err
	}
	if err := loadReleaseProductionRuntimeHistoryBytes(cfg, nil); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Both complete operator histories and every signature are pinned before the
// first network read. Extra, reordered, omitted or substituted domains refuse.
func readProductionBootstrapPrefixInputs(ctx context.Context, cfg *ReleaseConfig, observed ProductionBootstrapObservation) ([]releaseEvidenceV2ActivationInput, error) {
	public, err := readProductionBootstrapPublicInputs(ctx, cfg, observed.Native.Hotkey)
	if err != nil {
		return nil, err
	}
	if len(public) != 2 || len(observed.Operators) != len(public) {
		return nil, errors.New("bootstrap prefix requires the exact two-operator census")
	}
	remaining := uint64(productionBootstrapPrefixMaximumBytes)
	inputs := make([]releaseEvidenceV2ActivationInput, 0, len(public))
	for i, item := range public {
		files := cfg.EvidenceV2.Operators[i]
		digest, err := item.context.Activation.Digest()
		if err != nil {
			return nil, err
		}
		operator := observed.Operators[i]
		if operator.NoId != files.NoID || operator.ActivationHash != attemptHex32(digest) || operator.ClientId == "" || operator.ClientKeyGeneration == 0 || operator.ClientKey != attemptHex32(item.context.Activation.VPK) ||
			item.context.Activation.NativeBlock > observed.Native.Block || item.context.InitialCut.Boundary.EVMBlock > observed.EvmBlock {
			return nil, errors.New("bootstrap prefix activation or current client-key domain differs")
		}
		for _, value := range []string{operator.ClientKey, operator.ClientKeyRegistrationHash, operator.ClientKeyResponseHash, operator.ObservationNonce} {
			if _, err := canonicalAttemptHex32("prefix client-key scope", value, false); err != nil {
				return nil, err
			}
		}
		history, err := ReadReleaseEvidenceV2File(ctx, files.History, min(cfg.EvidenceV2.Bounds.MaxHistoryBytes, remaining))
		if err != nil {
			return nil, err
		}
		remaining -= uint64(len(history))
		var census ReleaseEvidenceV2ActivationHistory
		if err := decodeAttemptStreamV2JSON(history, productionBootstrapPrefixMaximumBytes, &census); err != nil {
			return nil, err
		}
		if len(census.LegacyClosures) > productionBootstrapPrefixMaximumClosures {
			return nil, errors.New("bootstrap prefix exceeds the bounded historical closure census")
		}
		vpkSignature, err := ReadReleaseEvidenceV2File(ctx, files.VPKSignature, ed25519.SignatureSize)
		if err != nil {
			return nil, err
		}
		hotkeySignature, err := ReadReleaseEvidenceV2File(ctx, files.HotkeySignature, 64)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, releaseEvidenceV2ActivationInput{Config: files, Context: item.context, Candidate: item.context.Activation, VPKSignature: vpkSignature, HotkeySignature: hotkeySignature, HistoryBytes: history})
	}
	return inputs, ctx.Err()
}

// The existing real signed-record, lifetime-trail and EMA replay determines
// every prefix. Missing history is never interpreted as an empty origin.
func replayProductionBootstrapPrefix(ctx context.Context, cfg *ReleaseConfig, inputs []releaseEvidenceV2ActivationInput, keys map[uint64]map[byte]ed25519.PublicKey, observed ProductionBootstrapObservation) (*ProductionBootstrapPrefixObservation, error) {
	last, err := replayReleaseEvidenceV2ActivationHistoriesForArchive(ctx, cfg, inputs, keys, true)
	if err != nil {
		return nil, err
	}
	result := &ProductionBootstrapPrefixObservation{Schema: ProductionBootstrapPrefixSchema, ConfigHash: observed.ConfigHash, PolicyHash: cfg.PolicyHash,
		ClientDomainHash: productionBootstrapPrefixHash(observed.Operators), Native: observed.Native, EvmBlock: observed.EvmBlock, EvmHash: observed.EvmHash}
	for i, input := range inputs {
		initial := input.Context.InitialCut
		digest, err := input.Candidate.Digest()
		if err != nil {
			return nil, err
		}
		var prior []AttemptSettlementQuality
		if last != nil {
			prior = last.Transitions[i].PostFold
		}
		result.Prefixes = append(result.Prefixes, ProductionBootstrapOperatorPrefix{NoId: input.Config.NoID, ActivationHash: attemptHex32(digest), HistoryHash: ReleaseMeasurementContentHash(input.HistoryBytes),
			LastSequence: initial.FirstSequence - 1, Root: initial.PriorRoot, Generation: initial.EgressGeneration, Epoch: initial.Boundary.SettlementEpoch, PriorEmaHash: productionBootstrapPrefixHash(prior)})
	}
	return result, ctx.Err()
}

// Original historical boundaries use actual finalized native/EVM readers.
// Initial activation and every earlier terminal are independently observed.
func authenticateProductionBootstrapPrefix(ctx context.Context, cfg *ReleaseConfig, inputs []releaseEvidenceV2ActivationInput, chain *ChainClient, native *crv4.Chain, runtime crv4.RuntimeArtifactIdentity) error {
	for _, input := range inputs {
		initial := input.Context
		err := func() error {
			operatorCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			_, err := chain.AuthenticateReleaseActivationV2Context(operatorCtx, native, ReleaseActivationV2Authority{Expected: initial.Activation, Journal: initial.Journal, RuntimeHash: initial.RuntimeHash, ValidatorUID: initial.ValidatorUID, NativeRuntime: runtime, productionRuntimeConfig: cfg}, input.Candidate, input.VPKSignature, input.HotkeySignature, initial.ObservedEVMBlock, initial.ObservedEVMHash)
			if err != nil {
				return err
			}
			return chain.authenticateReleaseInitialBoundaryV2Context(operatorCtx, initial, cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
		}()
		if err != nil {
			return err
		}
	}
	var history ReleaseEvidenceV2ActivationHistory
	if err := decodeAttemptStreamV2JSON(inputs[0].HistoryBytes, productionBootstrapPrefixMaximumBytes, &history); err != nil {
		return err
	}
	for _, raw := range history.LegacyClosures {
		var closure AttemptSettlementClosure
		if err := decodeAttemptStreamV2JSON(raw, productionBootstrapPrefixMaximumBytes, &closure); err != nil {
			return err
		}
		for i, transition := range closure.Transitions {
			if err := chain.authenticateReleaseStartupBoundaryV2WithPolicy(ctx, inputs[i].Context.InitialCut.Activation.Domain, transition.Identity.NoID, transition.FromBoundary, true, false, cfg, ""); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

// A pure transport failure may retry; a joined integrity, file or cancellation
// cause cannot be hidden by another timeout. All per-attempt clients are closed.
// Elapsed deadlines refuse admission while timer cancellation is still queued.
func retryProductionBootstrapPrefixRead(ctx context.Context, read func(context.Context) error, hooks releaseHttpGetRetryHooks) error {
	if ctx == nil || read == nil {
		return errors.New("bootstrap prefix retry owner is absent")
	}
	withTimeout, wait := hooks.withTimeout, hooks.wait
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	if wait == nil {
		wait = waitReleaseSnapshotRetry
	}
	operation, cancel := withTimeout(ctx, 300*time.Second)
	defer cancel()
	var last error
	for {
		if err := errors.Join(evidenceReadContextError(ctx), evidenceReadContextError(operation)); err != nil {
			return errors.Join(last, err)
		}
		attempt, closeAttempt := withTimeout(operation, 60*time.Second)
		if err := errors.Join(evidenceReadContextError(ctx), evidenceReadContextError(operation)); err != nil {
			closeAttempt()
			return errors.Join(last, err)
		}
		last = evidenceReadContextError(attempt)
		if last == nil {
			last = errors.Join(read(attempt), evidenceReadContextError(attempt))
		}
		closeAttempt()
		ownerErr := errors.Join(evidenceReadContextError(ctx), evidenceReadContextError(operation))
		if last == nil || ownerErr != nil || !retryableProductionSteeringRead(last) {
			return errors.Join(last, ownerErr)
		}
		if err := wait(operation, releaseSnapshotStartupRetryDelay); err != nil {
			return errors.Join(last, err, evidenceReadContextError(ctx), evidenceReadContextError(operation))
		}
	}
}

// Digests commit bounded projections; callers must still validate all domains.
func productionBootstrapPrefixHash(value any) string {
	raw, _ := json.Marshal(value)
	return ReleaseMeasurementContentHash(raw)
}
