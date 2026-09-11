//go:build linux || darwin

// Read the complete configured operator census before starting historical RPC
// authentication. These inputs still require prefix/EMA/history replay before
// a producer can acquire settlement ownership or submit native weights.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

const ReleaseEvidenceV2ActivationContextSchema = "urnetwork-validator-activation-context-v2"

// The separately pinned context is supplied by the deployment/history owner,
// not constructed by decoding the candidate activation. It records both
// distinct chain clocks and the exact initial cut; a checksum is not replay.
type ReleaseEvidenceV2ActivationContext struct {
	Schema           string                               `json:"schema"`
	Activation       protocol.ValidatorEvidenceActivation `json:"activation"`
	InitialCut       AttemptCutV2Context                  `json:"initial_cut"`
	ValidatorUID     uint16                               `json:"validator_uid"`
	Journal          [20]byte                             `json:"journal"`
	RuntimeHash      [32]byte                             `json:"runtime_hash"`
	ObservedEVMBlock uint64                               `json:"observed_evm_block"`
	ObservedEVMHash  [32]byte                             `json:"observed_evm_hash"`
}

// Canonical output rejects incomplete or internally competing identities.
// This method provides wire syntax only, never migration or chain authority.
func (self ReleaseEvidenceV2ActivationContext) CanonicalJSON(maxBytes uint64) ([]byte, error) {
	if maxBytes == 0 || maxBytes >= uint64(^uint(0)>>1) || uint64(len(self.InitialCut.Identity.DeploymentID)) > maxBytes {
		return nil, errors.New("activation context byte bound is absent or exceeded")
	}
	if err := self.InitialCut.Validate(); err != nil {
		return nil, err
	}
	domain, err := self.Activation.EvidenceDomain()
	if err != nil {
		return nil, err
	}
	cut := self.InitialCut
	if self.Schema != ReleaseEvidenceV2ActivationContextSchema || cut.Activation.Domain != domain ||
		cut.Identity.NoID != self.Activation.NoID || cut.Identity.ValidatorVPK != attemptHex32(self.Activation.VPK) ||
		cut.Identity.ValidatorUID != self.ValidatorUID || uint32(self.ValidatorUID) >= releaseNativeValidatorMaximumUIDs ||
		cut.Activation.Hotkey != self.Activation.Hotkey || cut.Activation.FirstSequence != self.Activation.FirstSequence ||
		cut.Activation.PriorRoot != attemptHex32(self.Activation.PriorRoot) ||
		cut.FirstSequence != self.Activation.FirstSequence || cut.EgressFirstSequence != cut.FirstSequence ||
		cut.PriorRoot != cut.Activation.PriorRoot || cut.Boundary.SettlementEpoch != self.Activation.Domain.Epoch ||
		self.Journal == ([20]byte{}) || self.Journal == domain.Coordinator || self.Journal == domain.SettlementVault ||
		self.RuntimeHash == ([32]byte{}) || self.ObservedEVMBlock <= self.Activation.EVMBlock || self.ObservedEVMHash == ([32]byte{}) {
		return nil, errors.New("activation context has inconsistent deployment, prefix or observation identity")
	}
	if cut.Boundary.EVMBlock <= self.Activation.EVMBlock || cut.Boundary.EVMBlock > self.ObservedEVMBlock ||
		cut.Boundary.EVMBlock == self.ObservedEVMBlock && cut.Boundary.EVMBlockHash != attemptHex32(self.ObservedEVMHash) {
		return nil, errors.New("activation context contains conflicting EVM boundary clocks")
	}
	encoded, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	if uint64(len(encoded)) >= maxBytes {
		return nil, errors.New("activation context exceeds its canonical byte bound")
	}
	return append(encoded, '\n'), nil
}

// Fixed, nonrecursive decoding is followed by canonical byte comparison, so
// duplicate fields, null fixed arrays, reordered keys and trailing data fail.
func decodeReleaseEvidenceV2ActivationContext(encoded []byte, maxBytes uint64) (ReleaseEvidenceV2ActivationContext, error) {
	var result ReleaseEvidenceV2ActivationContext
	if err := decodeAttemptStreamV2JSON(encoded, maxBytes, &result); err != nil {
		return ReleaseEvidenceV2ActivationContext{}, err
	}
	canonical, err := result.CanonicalJSON(maxBytes)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return ReleaseEvidenceV2ActivationContext{}, errors.Join(errors.New("activation context is not canonical"), err)
	}
	return result, nil
}

// Owned startup data is private to the root. HistoryBytes is checksum-checked
// input, not a verified history or prior-EMA verdict; callers must replay it.
// The signing key is never serialized or placed in a public evidence artifact.
type releaseEvidenceV2ActivationInput struct {
	Config          ReleaseEvidenceV2OperatorConfig
	Context         ReleaseEvidenceV2ActivationContext
	Candidate       protocol.ValidatorEvidenceActivation
	VPKSignature    []byte
	HotkeySignature []byte
	PrivateKey      ed25519.PrivateKey
	HistoryBytes    []byte
	Observation     VerifiedReleaseActivationV2
	retainedSetup   *provisionalReleaseActivationV2Admission
}

// Complete byte/census admission precedes any file read or chain request.
// Callers retain immutable configuration ownership until this method returns.
// No durable directory, ledger, Stats engine or network worker is created here.
func loadReleaseEvidenceV2ActivationInputs(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, hotkey [32]byte) (result []releaseEvidenceV2ActivationInput, resultErr error) {
	return loadReleaseEvidenceV2ActivationInputsWithRetainedSetup(ctx, cfg, chain, native, hotkey, nil)
}

func loadReleaseEvidenceV2ActivationInputsWithRetainedSetup(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, hotkey [32]byte, retained *ProvisionalActivationSetupV2) (result []releaseEvidenceV2ActivationInput, resultErr error) {
	if cfg == nil {
		return nil, errors.New("activation bootstrap configuration is absent")
	}
	nativeRuntime := crv4.RuntimeArtifactIdentity{
		Version:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion},
		CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash,
	}
	return readReleaseEvidenceV2ActivationInputsWithRetainedSetup(ctx, cfg, chain, native, hotkey, nativeRuntime, retained)
}

// The private reader receives an explicit historical runtime identity, just
// like the underlying real CRV4 reader. Production above always supplies the
// reviewed config pins; tests can supply real fixture metadata, not a verdict.
// A UID belongs to its historical snapshot, not the stable signing hotkey.
// Each pinned context supplies its own historical mapping. Current native
// eligibility remains a separate startup prerequisite after re-registration.
func readReleaseEvidenceV2ActivationInputs(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, hotkey [32]byte, nativeRuntime crv4.RuntimeArtifactIdentity) (result []releaseEvidenceV2ActivationInput, resultErr error) {
	return readReleaseEvidenceV2ActivationInputsWithRetainedSetup(ctx, cfg, chain, native, hotkey, nativeRuntime, nil)
}

func readReleaseEvidenceV2ActivationInputsWithRetainedSetup(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, hotkey [32]byte, nativeRuntime crv4.RuntimeArtifactIdentity, retained *ProvisionalActivationSetupV2) (result []releaseEvidenceV2ActivationInput, resultErr error) {
	if ctx == nil || cfg == nil {
		return nil, errors.New("activation bootstrap context or configuration is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if chain == nil || native == nil || hotkey == ([32]byte{}) {
		return nil, errors.New("activation bootstrap chain or signing identity is absent")
	}
	remaining := cfg.EvidenceV2.Bounds.MaxControlBytes
	for _, operator := range cfg.EvidenceV2.Operators {
		for _, reference := range operator.Files() {
			if reference.Bytes > remaining {
				return nil, errors.New("activation bootstrap aggregate input bytes exceed the control bound")
			}
			remaining -= reference.Bytes
		}
	}
	operators := make(map[uint64]OperatorConfig, len(cfg.Operators))
	for _, operator := range cfg.Operators {
		operators[operator.NoID] = operator
	}
	inputs := make([]releaseEvidenceV2ActivationInput, len(cfg.EvidenceV2.Operators))
	for index, operator := range cfg.EvidenceV2.Operators {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		seed, err := loadClientSeed(operators[operator.NoID].ClientKeySeedFile)
		if err != nil {
			return nil, fmt.Errorf("activation bootstrap no_id %d VPK: %w", operator.NoID, err)
		}
		privateKey := ed25519.NewKeyFromSeed(seed)
		encodedContext, err := ReadReleaseEvidenceV2File(ctx, operator.Context, cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
		if err != nil {
			return nil, err
		}
		configured, err := decodeReleaseEvidenceV2ActivationContext(encodedContext, cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
		if err != nil {
			return nil, err
		}
		if err := validateReleaseMeasurementInputV2Context(cfg, operator.NoID, configured.InitialCut); err != nil {
			return nil, err
		}
		if configured.Activation.Hotkey != hotkey || !bytes.Equal(configured.Activation.VPK[:], privateKey[ed25519.SeedSize:]) {
			return nil, errors.New("activation bootstrap authority differs from the actual signing identities")
		}
		activationBytes, err := ReadReleaseEvidenceV2File(ctx, operator.Activation, uint64(protocol.ValidatorEvidenceActivationPayloadSize))
		if err != nil {
			return nil, err
		}
		candidate, err := protocol.DecodeValidatorEvidenceActivationPayload(activationBytes)
		if err != nil {
			return nil, err
		}
		vpkSignature, err := ReadReleaseEvidenceV2File(ctx, operator.VPKSignature, ed25519.SignatureSize)
		if err != nil {
			return nil, err
		}
		hotkeySignature, err := ReadReleaseEvidenceV2File(ctx, operator.HotkeySignature, 64)
		if err != nil {
			return nil, err
		}
		if err := candidate.Verify(configured.Activation, vpkSignature, hotkeySignature); err != nil {
			return nil, err
		}
		history, err := ReadReleaseEvidenceV2File(ctx, operator.History, cfg.EvidenceV2.Bounds.MaxHistoryBytes)
		if err != nil {
			return nil, err
		}
		inputs[index] = releaseEvidenceV2ActivationInput{Config: operator, Context: configured, Candidate: candidate, VPKSignature: vpkSignature, HotkeySignature: hotkeySignature, PrivateKey: privateKey, HistoryBytes: history}
	}
	if retained != nil {
		for index := range inputs {
			if err := retained.admit(&inputs[index]); err != nil {
				return nil, err
			}
		}
		return inputs, nil
	}
	// All configured operators passed local admission before the first RPC.
	// Parallelism follows effective host capacity and the complete finite census.
	operationCtx, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(cfg))
	defer cancel()
	errorsByOperator := make([]error, len(inputs))
	var next atomic.Uint64
	var joined sync.WaitGroup
	workers := min(len(inputs), runtime.GOMAXPROCS(0))
	joined.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer joined.Done()
			for {
				index := next.Add(1) - 1
				if index >= uint64(len(inputs)) || operationCtx.Err() != nil {
					return
				}
				input := &inputs[index]
				configured := input.Context
				input.Observation, errorsByOperator[index] = chain.AuthenticateReleaseActivationV2Context(operationCtx, native, ReleaseActivationV2Authority{
					Expected: configured.Activation, Journal: common.Address(configured.Journal), RuntimeHash: configured.RuntimeHash, ValidatorUID: configured.ValidatorUID, NativeRuntime: nativeRuntime,
				}, input.Candidate, input.VPKSignature, input.HotkeySignature, configured.ObservedEVMBlock, configured.ObservedEVMHash)
				if errorsByOperator[index] == nil && input.Observation.Publication.PublishedBlock > configured.InitialCut.Boundary.EVMBlock {
					errorsByOperator[index] = errors.New("activation publication follows the configured initial cut boundary")
				}
				if errorsByOperator[index] != nil {
					cancel()
					return
				}
			}
		}()
	}
	joined.Wait()
	if err := errors.Join(append(errorsByOperator, operationCtx.Err())...); err != nil {
		return nil, err
	}
	return inputs, nil
}
