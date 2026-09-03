package main

// final_semantic_collect.go is the sole live-input boundary for FINAL.md.
// It runs before the terminal supervised-log scan, verifies every supervised
// response immediately, and persists only public evidence (never client seeds
// or credentials). The later source builder is offline.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
)

const (
	finalSemanticCollectedInputsSchema = "urnetwork-final-semantic-collected-inputs-v1"
	finalCollectedProofRecordSchema    = "urnetwork-final-validator-path-proof-record-v1"
	finalCollectedAttemptRecordsSchema = "urnetwork-final-validator-attempt-records-v1"
	finalSemanticCaptureStatusSchema   = "urnetwork-final-semantic-capture-status-v1"
	finalSemanticCaptureStatusFilename = "final-semantic-capture.json"
)

var finalSemanticCaptureRequiredClasses = []string{
	"claim-routing-topology",
	"contract-and-chain-receipts",
	"scenario-result-and-observation-history",
	"signed-attempt-records-and-path-proofs",
	"signed-validator-intents-measurements-and-envelopes",
	"signed-payout-artifacts",
}

const finalSemanticPriorPhaseRequiredClass = "authenticated-prior-phase-lineage"

type FinalCollectedProofRecord struct {
	Schema string                   `json:"schema"`
	Record validatorpkg.ProofRecord `json:"record"`
}

type FinalCollectedPayoutArtifact struct {
	NoID     uint64               `json:"no_id"`
	Epoch    uint64               `json:"epoch"`
	Artifact FinalArtifactLocator `json:"artifact"`
}

type FinalCollectedValidatorInputs struct {
	ValidatorID            uint64                             `json:"validator_id"`
	PathVPK                string                             `json:"path_vpk"`
	IntentStore            FinalArtifactLocator               `json:"intent_store"`
	DishonestDepositIntent *FinalCollectedValidatorIntent     `json:"dishonest_deposit_intent,omitempty"`
	Intents                []FinalCollectedValidatorIntent    `json:"intents"`
	LifecycleIntents       []FinalCollectedValidatorIntent    `json:"lifecycle_intents,omitempty"`
	Attempts               []FinalCollectedValidatorAttempts  `json:"attempts"`
	PathProofs             []FinalCollectedValidatorPathProof `json:"path_proofs"`
}

type FinalCollectedValidatorAttempts struct {
	NoID                  uint64               `json:"no_id"`
	RecordCount           uint64               `json:"record_count"`
	CheckpointCount       uint64               `json:"checkpoint_count"`
	CompleteCount         uint64               `json:"complete_count"`
	FailedCount           uint64               `json:"failed_count"`
	PendingCount          uint64               `json:"pending_count"`
	PendingRecoveredCount uint64               `json:"pending_recovered_count"`
	Artifact              FinalArtifactLocator `json:"artifact"`
}

type FinalCollectedAttemptRecords struct {
	Schema                string                       `json:"schema"`
	ValidatorID           uint64                       `json:"validator_id"`
	NoID                  uint64                       `json:"no_id"`
	FirstSequence         uint64                       `json:"first_sequence"`
	LastSequence          uint64                       `json:"last_sequence"`
	DispositionCounts     map[string]uint64            `json:"disposition_counts"`
	PendingRecoveredCount uint64                       `json:"pending_recovered_count"`
	Records               []validatorpkg.AttemptRecord `json:"records"`
}

type FinalCollectedValidatorIntent struct {
	Sequence        uint64               `json:"sequence"`
	SettlementEpoch uint64               `json:"settlement_epoch"`
	SubnetEpoch     uint64               `json:"subnet_epoch"`
	Status          string               `json:"status"`
	VectorHash      string               `json:"vector_hash"`
	Artifact        FinalArtifactLocator `json:"artifact"`
	Measurement     FinalArtifactLocator `json:"measurement_artifact"`
	Envelope        FinalArtifactLocator `json:"measurement_envelope"`
}

type FinalCollectedValidatorPathProof struct {
	NoID       uint64               `json:"no_id"`
	FirstEpoch uint64               `json:"first_epoch"`
	LastEpoch  uint64               `json:"last_epoch"`
	ProofCount uint64               `json:"proof_count"`
	Artifact   FinalArtifactLocator `json:"artifact"`
}

type FinalCollectedPriorPhaseInputs struct {
	Phase                   string                   `json:"phase"`
	RunID                   string                   `json:"run_id"`
	ResultHash              string                   `json:"result_hash"`
	Window                  ScenarioAcceptanceWindow `json:"acceptance_window"`
	ScenarioResult          FinalArtifactLocator     `json:"scenario_result"`
	OwnerCompletion         FinalArtifactLocator     `json:"owner_completion"`
	EvidenceManifest        FinalArtifactLocator     `json:"evidence_manifest"`
	CaptureStatus           FinalArtifactLocator     `json:"capture_status"`
	CollectedInputsManifest FinalArtifactLocator     `json:"collected_inputs_manifest"`
	LiveChainBundles        []FinalArtifactLocator   `json:"live_chain_bundles"`
	SemanticSupplement      FinalArtifactLocator     `json:"semantic_verified_supplement"`
	SemanticFileEnvelopes   []FinalArtifactLocator   `json:"semantic_file_envelopes"`
}

type FinalSemanticCollectedInputs struct {
	Schema              string                          `json:"schema"`
	Phase               string                          `json:"phase"`
	RunID               string                          `json:"run_id"`
	ResultHash          string                          `json:"result_hash"`
	Window              ScenarioAcceptanceWindow        `json:"acceptance_window"`
	Policy              FinalArtifactLocator            `json:"policy"`
	ScenarioResult      FinalArtifactLocator            `json:"scenario_result"`
	TerminalObservation FinalArtifactLocator            `json:"terminal_observation"`
	ObservationHistory  FinalArtifactLocator            `json:"observation_history"`
	PriorPhase          *FinalCollectedPriorPhaseInputs `json:"prior_phase,omitempty"`
	ClosedInputBundles  []FinalArtifactLocator          `json:"closed_input_bundles"`
	Payouts             []FinalCollectedPayoutArtifact  `json:"payouts"`
	LifecyclePayouts    []FinalCollectedPayoutArtifact  `json:"lifecycle_payouts,omitempty"`
	Validators          []FinalCollectedValidatorInputs `json:"validators"`
	EvidenceHash        string                          `json:"evidence_hash"`
}

// FinalSemanticCaptureStatus is the immutable live/offline handoff. A signed
// scenario completion may advance the next phase only after this exact file is
// in its closed archive. Semantic reconstruction remains pending and cannot be
// mistaken for final acceptance.
type FinalSemanticCaptureStatus struct {
	Schema                  string               `json:"schema"`
	Status                  string               `json:"status"`
	SemanticStatus          string               `json:"semantic_status"`
	Phase                   string               `json:"phase"`
	RunID                   string               `json:"run_id"`
	ResultHash              string               `json:"result_hash"`
	ClosedAt                string               `json:"closed_at"`
	CollectedInputsHash     string               `json:"collected_inputs_hash"`
	CollectedInputsManifest FinalArtifactLocator `json:"collected_inputs_manifest"`
	RequiredClasses         []string             `json:"required_classes"`
	ClosedBundleCount       uint64               `json:"closed_bundle_count"`
	PayoutArtifactCount     uint64               `json:"payout_artifact_count"`
	ValidatorIntentCount    uint64               `json:"validator_intent_count"`
	AttemptRecordCount      uint64               `json:"attempt_record_count"`
	PathProofCount          uint64               `json:"path_proof_count"`
	EvidenceHash            string               `json:"evidence_hash"`
}

// CollectFinalSemanticInputs must be invoked while supervised operator APIs
// and validator runtime state are still live, before the terminal process-log
// scan. It performs no chain signing and writes no FINAL output.
func CollectFinalSemanticInputs(ctx context.Context, cfg *ResolvedConfig, stateDir, runDir string, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation) (*FinalSemanticCollectedInputs, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil || cfg.Policy == nil || result == nil || result.AcceptanceWindow == nil || terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil || stateDir == "" || runDir == "" || len(history) == 0 {
		return nil, errors.New("final semantic live collection inputs are incomplete")
	}
	if result.Name != "release-1.0" && result.Name != "production-soak" {
		return nil, fmt.Errorf("scenario %q has no final semantic collection", result.Name)
	}
	if result.Result != "pass" || result.EvidenceHash == "" || terminal.ObservationHash == "" {
		return nil, errors.New("final semantic collection requires a passing hashed candidate")
	}
	definition, err := scenarioDefinitionFor(cfg, result.Name)
	if err != nil {
		return nil, err
	}
	if err := validateScenarioAcceptanceResult(cfg, definition, result); err != nil {
		return nil, fmt.Errorf("final semantic collection acceptance window: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runRoot, err := filepath.Abs(runDir)
	if err != nil {
		return nil, err
	}
	stateRoot, err := filepath.Abs(stateDir)
	if err != nil || !pathWithinRoot(stateRoot, runRoot) {
		return nil, errors.New("final semantic collection run directory is outside state")
	}
	// Production live execution intentionally overlaps offline release analysis.
	// Once the live phase is complete, wait here—before creating final-inputs or
	// any other capture output—until the predecessor's owner-authenticated
	// semantic_verified marker is atomically visible.
	if err := awaitFinalPriorSemanticReady(ctx, cfg, stateRoot, result); err != nil {
		return nil, err
	}
	policyBytes, err := cfg.Policy.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	collected := &FinalSemanticCollectedInputs{
		Schema: finalSemanticCollectedInputsSchema, Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash, Window: *result.AcceptanceWindow,
	}
	collected.Policy, err = persistFinalCollectedArtifact(runRoot, "policy", "final-inputs/policy.json", policyBytes)
	if err != nil {
		return nil, err
	}
	collected.ClosedInputBundles, collected.ScenarioResult, collected.TerminalObservation, collected.ObservationHistory, err = captureFinalSemanticClosedInputs(stateRoot, runRoot, result, terminal, history, cfg.Config.Topology.Miners, cfg.Config.Topology.MinerSwarmProcesses, cfg.Config.Topology.Operators)
	if err != nil {
		return nil, err
	}
	liveChainBundles, err := captureFinalSemanticLiveChain(ctx, cfg, stateRoot, runRoot, result, terminal, history)
	if err != nil {
		return nil, err
	}
	collected.ClosedInputBundles = append(collected.ClosedInputBundles, liveChainBundles...)
	sort.Slice(collected.ClosedInputBundles, func(i, j int) bool {
		return collected.ClosedInputBundles[i].URI < collected.ClosedInputBundles[j].URI
	})
	collected.PriorPhase, err = collectFinalPriorPhaseInputs(cfg, stateRoot, runRoot, result)
	if err != nil {
		return nil, err
	}
	collected.Payouts, collected.LifecyclePayouts, err = collectFinalPayoutArtifacts(ctx, cfg, runRoot, terminal, result.AcceptanceWindow)
	if err != nil {
		return nil, err
	}
	startedAt, err := time.Parse(time.RFC3339Nano, result.StartedAt)
	if err != nil || result.StartedAt != startedAt.UTC().Format(time.RFC3339Nano) {
		return nil, errors.New("final semantic campaign start time is not canonical UTC")
	}
	completedAt, err := time.Parse(time.RFC3339Nano, result.CompletedAt)
	if err != nil || result.CompletedAt != completedAt.UTC().Format(time.RFC3339Nano) || completedAt.Before(startedAt) {
		return nil, errors.New("final semantic campaign completion time is not canonical UTC")
	}
	collected.Validators, err = collectFinalValidatorInputs(cfg, stateRoot, runRoot, terminal, result.Name, result.AcceptanceWindow, startedAt, completedAt)
	if err != nil {
		return nil, err
	}
	collected.EvidenceHash, err = finalSemanticCollectedInputsHash(collected)
	if err != nil {
		return nil, err
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, collected); err != nil {
		return nil, err
	}
	wire, err := json.MarshalIndent(collected, "", "  ")
	if err != nil {
		return nil, err
	}
	wire = append(wire, '\n')
	manifestLocator, err := persistFinalCollectedArtifact(runRoot, "final-semantic-input-manifest", "final-inputs/manifest.json", wire)
	if err != nil {
		return nil, err
	}
	if err := verifyFinalCollectedClosedGraph(ctx, cfg, stateRoot, runRoot, collected); err != nil {
		return nil, err
	}
	status := finalSemanticCaptureStatus(result, collected, manifestLocator)
	status.EvidenceHash, err = finalSemanticCaptureStatusHash(status)
	if err != nil {
		return nil, err
	}
	if err := verifyFinalSemanticCaptureStatus(status, collected); err != nil {
		return nil, err
	}
	statusWire, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return nil, err
	}
	statusWire = append(statusWire, '\n')
	if _, err := persistFinalCollectedArtifact(runRoot, "final-semantic-capture-status", finalSemanticCaptureStatusFilename, statusWire); err != nil {
		return nil, err
	}
	var reread FinalSemanticCaptureStatus
	if err := decodeStrictJSONFile(filepath.Join(runRoot, finalSemanticCaptureStatusFilename), &reread); err != nil {
		return nil, fmt.Errorf("read back final semantic capture status: %w", err)
	}
	if err := verifyFinalSemanticCaptureStatus(&reread, collected); err != nil {
		return nil, fmt.Errorf("read back final semantic capture status: %w", err)
	}
	return collected, nil
}

const finalPriorSemanticReadyPollInterval = 25 * time.Millisecond

func validateFinalPriorProductionIdentity(prior, current *ScenarioResult) error {
	if prior == nil || current == nil {
		return errors.New("prior/current production lineage is absent")
	}
	priorCompleted, completedErr := time.Parse(time.RFC3339Nano, prior.CompletedAt)
	currentStarted, startedErr := time.Parse(time.RFC3339Nano, current.StartedAt)
	if completedErr != nil || startedErr != nil || !priorCompleted.Before(currentStarted) || prior.DeploymentID != current.DeploymentID || prior.ConfigHash != current.ConfigHash || !strings.EqualFold(prior.PolicyHash, current.PolicyHash) || prior.ChainID != current.ChainID || !strings.EqualFold(prior.GenesisHash, current.GenesisHash) || prior.Netuid != current.Netuid {
		return errors.New("authenticated prior release phase does not precede or identify the production phase")
	}
	return nil
}

func awaitFinalPriorSemanticReady(ctx context.Context, cfg *ResolvedConfig, stateRoot string, result *ScenarioResult) error {
	return awaitFinalPriorSemanticReadyObserved(ctx, cfg, stateRoot, result, nil)
}

func awaitFinalPriorSemanticReadyObserved(ctx context.Context, cfg *ResolvedConfig, stateRoot string, result *ScenarioResult, waiting func()) error {
	if result == nil || result.Name == "release-1.0" {
		return nil
	}
	if ctx == nil || cfg == nil || result.Name != "production-soak" {
		return errors.New("prior semantic readiness context is invalid")
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return fmt.Errorf("resolve prior phase evidence owner: %w", err)
	}
	prior, _, err := loadCompletedScenarioCampaign(cfg, stateRoot, roles, "release-1.0")
	if err != nil {
		return fmt.Errorf("authenticate prior release phase before semantic wait: %w", err)
	}
	if err := validateFinalPriorProductionIdentity(prior, result); err != nil {
		return err
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return errors.New("prior semantic evidence owner is absent")
	}
	ownerKey, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		return fmt.Errorf("decode prior semantic evidence owner: %w", err)
	}
	relative := filepath.ToSlash(filepath.Join("runs", prior.RunID, finalSemanticSupplementFilename))
	absolute := filepath.Join(stateRoot, filepath.FromSlash(relative))
	ticker := time.NewTicker(finalPriorSemanticReadyPollInterval)
	defer ticker.Stop()
	notified := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := os.Lstat(absolute); err == nil {
			entry, err := finalCollectedFileEntry(stateRoot, relative)
			if err != nil {
				return fmt.Errorf("read prior semantic_verified readiness marker: %w", err)
			}
			var envelope ReleaseEvidenceEnvelope
			if err := decodeStrictJSONBytes(entry.Data, &envelope); err != nil {
				return fmt.Errorf("decode prior semantic_verified readiness marker: %w", err)
			}
			if err := verifyFinalSemanticOwnerEnvelope(cfg, &envelope, &ownerKey.PublicKey, finalSemanticSupplementKind, prior.RunID); err != nil {
				return fmt.Errorf("authenticate prior semantic_verified readiness marker: %w", err)
			}
			var payload FinalSemanticSupplementPayload
			if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
				return fmt.Errorf("decode prior semantic_verified readiness payload: %w", err)
			}
			if payload.Schema != finalSemanticSupplementSchema || payload.Status != finalSemanticSupplementStatus || payload.Phase != "release-1.0" || payload.RunID != prior.RunID || payload.ResultHash != prior.EvidenceHash || !validSHA256ContentHash(payload.ScenarioCompleteHash) || !validSHA256ContentHash(payload.ScenarioEvidenceManifestHash) || !validSHA256ContentHash(payload.CaptureStatusHash) || !validSHA256ContentHash(payload.CollectedInputsHash) || !validCanonicalHashHex(payload.SemanticEvidenceHash) || !validCanonicalHashHex(payload.PublicTranscriptHash) || len(payload.Files) < 2 || len(payload.Files) > maximumCampaignEvidenceObjects {
				return errors.New("prior semantic_verified readiness marker does not bind the release closure")
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect prior semantic_verified readiness marker: %w", err)
		}
		if !notified && waiting != nil {
			waiting()
			notified = true
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func finalSemanticCaptureStatus(result *ScenarioResult, collected *FinalSemanticCollectedInputs, manifest FinalArtifactLocator) *FinalSemanticCaptureStatus {
	requiredClasses := append([]string(nil), finalSemanticCaptureRequiredClasses...)
	if collected.PriorPhase != nil {
		requiredClasses = append(requiredClasses, finalSemanticPriorPhaseRequiredClass)
	}
	status := &FinalSemanticCaptureStatus{
		Schema: finalSemanticCaptureStatusSchema, Status: "capture_closed", SemanticStatus: "pending_offline_verification",
		Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash, ClosedAt: result.CompletedAt,
		CollectedInputsHash: collected.EvidenceHash, CollectedInputsManifest: manifest,
		RequiredClasses: requiredClasses, ClosedBundleCount: uint64(len(collected.ClosedInputBundles)),
		PayoutArtifactCount: uint64(len(collected.Payouts)),
	}
	for _, validator := range collected.Validators {
		status.ValidatorIntentCount += uint64(len(validator.Intents))
		if validator.DishonestDepositIntent != nil {
			status.ValidatorIntentCount++
		}
		for _, attempts := range validator.Attempts {
			status.AttemptRecordCount += attempts.RecordCount
		}
		for _, proofs := range validator.PathProofs {
			status.PathProofCount += proofs.ProofCount
		}
	}
	return status
}

func verifyFinalSemanticCaptureStatus(status *FinalSemanticCaptureStatus, collected *FinalSemanticCollectedInputs) error {
	wantClasses := append([]string(nil), finalSemanticCaptureRequiredClasses...)
	if collected != nil && collected.PriorPhase != nil {
		wantClasses = append(wantClasses, finalSemanticPriorPhaseRequiredClass)
	}
	if status == nil || collected == nil || status.Schema != finalSemanticCaptureStatusSchema || status.Status != "capture_closed" || status.SemanticStatus != "pending_offline_verification" || status.Phase != collected.Phase || status.RunID != collected.RunID || status.ResultHash != collected.ResultHash || status.CollectedInputsHash != collected.EvidenceHash || status.ClosedBundleCount != uint64(len(collected.ClosedInputBundles)) || status.PayoutArtifactCount != uint64(len(collected.Payouts)) || !slices.Equal(status.RequiredClasses, wantClasses) {
		return errors.New("final semantic capture status is incomplete or differs from the closed graph")
	}
	closedAt, err := time.Parse(time.RFC3339Nano, status.ClosedAt)
	if err != nil || status.ClosedAt != closedAt.UTC().Format(time.RFC3339Nano) {
		return errors.New("final semantic capture status time is not canonical UTC")
	}
	if err := verifyFinalArtifact("collected input manifest", status.CollectedInputsManifest, "final-semantic-input-manifest"); err != nil {
		return err
	}
	wantIntents, wantAttempts, wantProofs := uint64(0), uint64(0), uint64(0)
	for _, validator := range collected.Validators {
		wantIntents += uint64(len(validator.Intents))
		if validator.DishonestDepositIntent != nil {
			wantIntents++
		}
		for _, attempts := range validator.Attempts {
			wantAttempts += attempts.RecordCount
		}
		for _, proofs := range validator.PathProofs {
			wantProofs += proofs.ProofCount
		}
	}
	if status.ValidatorIntentCount != wantIntents || status.AttemptRecordCount != wantAttempts || status.PathProofCount != wantProofs || wantIntents == 0 || wantAttempts == 0 || wantProofs == 0 {
		return errors.New("final semantic capture status signed-input counts differ from the closed graph")
	}
	wantHash, err := finalSemanticCaptureStatusHash(status)
	if err != nil {
		return err
	}
	if status.EvidenceHash == "" || status.EvidenceHash != wantHash {
		return errors.New("final semantic capture status hash differs")
	}
	return nil
}

func finalSemanticCaptureStatusHash(status *FinalSemanticCaptureStatus) (string, error) {
	copy := *status
	copy.EvidenceHash = ""
	return canonicalHashHex(copy)
}

func validateFinalSemanticCaptureClosure(cfg *ResolvedConfig, runDir string, result *ScenarioResult) error {
	if cfg == nil || result == nil || runDir == "" {
		return errors.New("final semantic capture closure context is incomplete")
	}
	var collected FinalSemanticCollectedInputs
	manifestPath := filepath.Join(runDir, "final-inputs", "manifest.json")
	if err := decodeStrictJSONFile(manifestPath, &collected); err != nil {
		return fmt.Errorf("decode final semantic collected-input manifest: %w", err)
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, &collected); err != nil {
		return err
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var status FinalSemanticCaptureStatus
	if err := decodeStrictJSONFile(filepath.Join(runDir, finalSemanticCaptureStatusFilename), &status); err != nil {
		return fmt.Errorf("decode final semantic capture status: %w", err)
	}
	if err := verifyFinalSemanticCaptureStatus(&status, &collected); err != nil {
		return err
	}
	if status.Phase != result.Name || status.RunID != result.RunID || status.ResultHash != result.EvidenceHash || status.ClosedAt != result.CompletedAt || status.CollectedInputsManifest.URI != "final-inputs/manifest.json" || status.CollectedInputsManifest.SizeBytes != uint64(len(manifestBytes)) || status.CollectedInputsManifest.ContentHash != bytesSHA256(manifestBytes) {
		return errors.New("final semantic capture status does not bind the completed scenario and exact manifest bytes")
	}
	return nil
}

func collectFinalPriorPhaseInputs(cfg *ResolvedConfig, stateRoot, runRoot string, result *ScenarioResult) (*FinalCollectedPriorPhaseInputs, error) {
	if result == nil || result.Name == "release-1.0" {
		return nil, nil
	}
	if result.Name != "production-soak" {
		return nil, fmt.Errorf("scenario %q cannot bind final semantic phase lineage", result.Name)
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve prior phase evidence owner: %w", err)
	}
	prior, _, err := loadCompletedScenarioCampaign(cfg, stateRoot, roles, "release-1.0")
	if err != nil {
		return nil, fmt.Errorf("authenticate prior release phase: %w", err)
	}
	if err := validateFinalPriorProductionIdentity(prior, result); err != nil {
		return nil, err
	}
	copyExact := func(kind, sourceName, destinationName string) (FinalArtifactLocator, error) {
		entry, err := finalCollectedFileEntry(stateRoot, filepath.ToSlash(filepath.Join("runs", prior.RunID, filepath.FromSlash(sourceName))))
		if err != nil {
			return FinalArtifactLocator{}, err
		}
		return persistFinalCollectedArtifact(runRoot, kind, filepath.ToSlash(filepath.Join("final-inputs", "prior-release", destinationName)), entry.Data)
	}
	// Keep raw authenticated JSON in opaque files. This preserves byte identity
	// without making the current archive walker follow the prior manifest's
	// already-closed relative locator graph a second time.
	resultLocator, err := copyExact("prior-scenario-result", "result.json", "result.json.bin")
	if err != nil {
		return nil, err
	}
	completionLocator, err := copyExact("prior-owner-completion-envelope", "complete.json", "complete.json.bin")
	if err != nil {
		return nil, err
	}
	manifestLocator, err := copyExact("prior-evidence-manifest-envelope", campaignEvidenceManifestFilename, "campaign-evidence-manifest.json.bin")
	if err != nil {
		return nil, err
	}
	captureLocator, err := copyExact("prior-capture-status", finalSemanticCaptureStatusFilename, "final-semantic-capture.json.bin")
	if err != nil {
		return nil, err
	}
	inputsLocator, err := copyExact("prior-collected-input-manifest", "final-inputs/manifest.json", "collected-inputs-manifest.json.bin")
	if err != nil {
		return nil, err
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return nil, errors.New("prior semantic evidence owner is absent")
	}
	ownerKey, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("decode prior semantic evidence owner: %w", err)
	}
	readPrior := func(relative string) ([]byte, error) {
		entry, err := finalCollectedFileEntry(stateRoot, filepath.ToSlash(filepath.Join("runs", prior.RunID, filepath.FromSlash(relative))))
		if err != nil {
			return nil, err
		}
		return entry.Data, nil
	}
	completionData, err := readPrior("complete.json")
	if err != nil {
		return nil, err
	}
	manifestData, err := readPrior(campaignEvidenceManifestFilename)
	if err != nil {
		return nil, err
	}
	captureData, err := readPrior(finalSemanticCaptureStatusFilename)
	if err != nil {
		return nil, err
	}
	inputsData, err := readPrior("final-inputs/manifest.json")
	if err != nil {
		return nil, err
	}
	var completion, manifestEnvelope ReleaseEvidenceEnvelope
	var capture FinalSemanticCaptureStatus
	var priorInputs FinalSemanticCollectedInputs
	if err := decodeStrictJSONBytes(completionData, &completion); err != nil {
		return nil, fmt.Errorf("decode prior completion for semantic lineage: %w", err)
	}
	if err := decodeStrictJSONBytes(manifestData, &manifestEnvelope); err != nil {
		return nil, fmt.Errorf("decode prior evidence manifest for semantic lineage: %w", err)
	}
	if err := decodeStrictJSONBytes(captureData, &capture); err != nil {
		return nil, fmt.Errorf("decode prior capture status for semantic lineage: %w", err)
	}
	if err := decodeStrictJSONBytes(inputsData, &priorInputs); err != nil {
		return nil, fmt.Errorf("decode prior collected inputs for semantic lineage: %w", err)
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, &priorInputs); err != nil || priorInputs.Phase != "release-1.0" || priorInputs.RunID != prior.RunID || priorInputs.ResultHash != prior.EvidenceHash || priorInputs.Window != *prior.AcceptanceWindow {
		return nil, stateMismatchError(err, "prior collected-input manifest does not bind the authenticated release")
	}
	// Carry the exact prior live-chain bundle into the production closure. The
	// source builder must never reach back into the mutable prior run directory
	// to recover the native terminal checkpoint.
	liveChainBundles := make([]FinalArtifactLocator, 0, 1)
	for index, locator := range priorInputs.ClosedInputBundles {
		data, err := readPrior(locator.URI)
		if err != nil {
			return nil, fmt.Errorf("read prior closed bundle %s: %w", locator.URI, err)
		}
		if uint64(len(data)) != locator.SizeBytes || bytesSHA256(data) != locator.ContentHash {
			return nil, fmt.Errorf("prior closed bundle %s differs from its manifest", locator.URI)
		}
		bundle, err := decodeFinalCollectedFileBundle(data)
		if err != nil {
			return nil, fmt.Errorf("decode prior closed bundle %s: %w", locator.URI, err)
		}
		if finalSemanticBundleClass(bundle.Name) != "live-chain" {
			continue
		}
		destination := filepath.ToSlash(filepath.Join("final-inputs", "prior-release", "live-chain", fmt.Sprintf("bundle-%03d.json", index)))
		copied, err := persistFinalCollectedArtifact(runRoot, "prior-live-chain-bundle", destination, data)
		if err != nil {
			return nil, err
		}
		liveChainBundles = append(liveChainBundles, copied)
	}
	if len(liveChainBundles) == 0 {
		return nil, errors.New("prior collected-input graph lacks a live-chain bundle")
	}
	sort.Slice(liveChainBundles, func(i, j int) bool { return liveChainBundles[i].URI < liveChainBundles[j].URI })
	supplementData, err := readPrior(finalSemanticSupplementFilename)
	if err != nil {
		return nil, fmt.Errorf("read prior semantic_verified supplement: %w", err)
	}
	var supplement ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(supplementData, &supplement); err != nil {
		return nil, fmt.Errorf("decode prior semantic_verified supplement: %w", err)
	}
	if err := verifyFinalSemanticOwnerEnvelope(cfg, &supplement, &ownerKey.PublicKey, finalSemanticSupplementKind, prior.RunID); err != nil {
		return nil, fmt.Errorf("authenticate prior semantic_verified supplement: %w", err)
	}
	var supplementPayload FinalSemanticSupplementPayload
	if err := decodeStrictJSONBytes(supplement.Payload, &supplementPayload); err != nil {
		return nil, fmt.Errorf("decode prior semantic_verified payload: %w", err)
	}
	if supplementPayload.Schema != finalSemanticSupplementSchema || supplementPayload.Status != finalSemanticSupplementStatus || supplementPayload.Phase != "release-1.0" || supplementPayload.RunID != prior.RunID || supplementPayload.ResultHash != prior.EvidenceHash || supplementPayload.ScenarioCompleteHash != completion.ContentHash || supplementPayload.ScenarioEvidenceManifestHash != manifestEnvelope.ContentHash || supplementPayload.CaptureStatusHash != capture.EvidenceHash || supplementPayload.CollectedInputsHash != priorInputs.EvidenceHash || !validCanonicalHashHex(supplementPayload.SemanticEvidenceHash) || !validCanonicalHashHex(supplementPayload.PublicTranscriptHash) {
		return nil, errors.New("prior semantic_verified supplement does not bind the closed release")
	}
	if len(supplementPayload.Files) < 2 || len(supplementPayload.Files) > maximumCampaignEvidenceObjects {
		return nil, errors.New("prior semantic_verified file census is incomplete")
	}
	semanticFiles := make([]FinalArtifactLocator, 0, len(supplementPayload.Files))
	seenSemantic, seenMarkdown := false, false
	previousPath := ""
	for _, item := range supplementPayload.Files {
		if err := validateFinalSemanticPostCapturePath(item.Path); err != nil || item.Path <= previousPath || item.Size == 0 || !validSHA256ContentHash(item.ContentHash) || !validSHA256ContentHash(item.EnvelopeHash) {
			return nil, stateMismatchError(err, "prior semantic_verified file manifest is invalid at %q", item.Path)
		}
		previousPath = item.Path
		pathHash := sha256.Sum256([]byte(item.Path))
		envelopeRelative := filepath.ToSlash(filepath.Join("public", finalSemanticSupplementArchiveDir, prior.RunID, "files", hex.EncodeToString(pathHash[:])+".evidence.json"))
		envelopeEntry, err := finalCollectedFileEntry(stateRoot, envelopeRelative)
		if err != nil {
			return nil, fmt.Errorf("read prior semantic file envelope %s: %w", item.Path, err)
		}
		var fileEnvelope ReleaseEvidenceEnvelope
		if err := decodeStrictJSONBytes(envelopeEntry.Data, &fileEnvelope); err != nil {
			return nil, fmt.Errorf("decode prior semantic file envelope %s: %w", item.Path, err)
		}
		if err := verifyFinalSemanticOwnerEnvelope(cfg, &fileEnvelope, &ownerKey.PublicKey, finalSemanticSupplementFileKind, prior.RunID); err != nil || fileEnvelope.ContentHash != item.EnvelopeHash {
			return nil, stateMismatchError(err, "authenticate prior semantic file envelope %s", item.Path)
		}
		var filePayload finalSemanticSupplementFilePayload
		if err := decodeStrictJSONBytes(fileEnvelope.Payload, &filePayload); err != nil || filePayload.Schema != finalSemanticSupplementFileSchema || filePayload.RunID != prior.RunID || filePayload.Path != item.Path || filePayload.ContentHash != item.ContentHash || filePayload.Size != item.Size || uint64(len(filePayload.Data)) != item.Size || bytesSHA256(filePayload.Data) != item.ContentHash {
			return nil, stateMismatchError(err, "prior semantic file payload differs for %s", item.Path)
		}
		if item.Path == finalSemanticEvidenceFilename {
			var semantic FinalSemanticEvidence
			if err := decodeStrictJSONBytes(filePayload.Data, &semantic); err != nil || VerifyFinalSemanticEvidence(&semantic) != nil || semantic.PublicVerification == nil || semantic.Phase != "release-1.0" || semantic.RunID != prior.RunID || semantic.ResultHash != prior.EvidenceHash || semantic.EvidenceHash != supplementPayload.SemanticEvidenceHash || semantic.PublicVerification.TranscriptHash != supplementPayload.PublicTranscriptHash {
				return nil, stateMismatchError(err, "prior signed semantic evidence is invalid")
			}
			seenSemantic = true
		}
		seenMarkdown = seenMarkdown || item.Path == finalSemanticMarkdownFilename
		destination := filepath.ToSlash(filepath.Join("final-inputs", "prior-release", "semantic-files", hex.EncodeToString(pathHash[:])+".evidence.json"))
		locator, err := persistFinalCollectedArtifact(runRoot, "prior-semantic-file-envelope", destination, envelopeEntry.Data)
		if err != nil {
			return nil, err
		}
		semanticFiles = append(semanticFiles, locator)
	}
	if !seenSemantic || !seenMarkdown {
		return nil, errors.New("prior semantic_verified supplement lacks FINAL.md or semantic evidence")
	}
	sort.Slice(semanticFiles, func(i, j int) bool { return semanticFiles[i].URI < semanticFiles[j].URI })
	supplementLocator, err := persistFinalCollectedArtifact(runRoot, "prior-semantic-supplement-envelope", filepath.ToSlash(filepath.Join("final-inputs", "prior-release", "semantic-verified.evidence.json.bin")), supplementData)
	if err != nil {
		return nil, err
	}
	return &FinalCollectedPriorPhaseInputs{
		Phase: "release-1.0", RunID: prior.RunID, ResultHash: prior.EvidenceHash, Window: *prior.AcceptanceWindow,
		ScenarioResult: resultLocator, OwnerCompletion: completionLocator, EvidenceManifest: manifestLocator,
		CaptureStatus: captureLocator, CollectedInputsManifest: inputsLocator, LiveChainBundles: liveChainBundles, SemanticSupplement: supplementLocator, SemanticFileEnvelopes: semanticFiles,
	}, nil
}

func verifyFinalCollectedPriorPhase(prior *FinalCollectedPriorPhaseInputs, current *FinalSemanticCollectedInputs) error {
	if prior == nil || current == nil || prior.Phase != "release-1.0" || prior.RunID == "" || prior.RunID == current.RunID || !validCanonicalHashHex(prior.ResultHash) || prior.Window.EpochCount != finalReleaseEpochCount || prior.Window.EpochBlocks != finalReleaseEpochBlocks || prior.Window.FinalizeOffsetBlocks != finalReleaseFinalizeOffsetBlocks || prior.Window.TerminalBlock == 0 || prior.Window.TerminalBlock >= current.Window.StartBlock {
		return errors.New("collected production inputs have incomplete or unordered prior release lineage")
	}
	for label, item := range map[string]struct {
		locator FinalArtifactLocator
		kind    string
	}{
		"scenario result":          {locator: prior.ScenarioResult, kind: "prior-scenario-result"},
		"owner completion":         {locator: prior.OwnerCompletion, kind: "prior-owner-completion-envelope"},
		"evidence manifest":        {locator: prior.EvidenceManifest, kind: "prior-evidence-manifest-envelope"},
		"capture status":           {locator: prior.CaptureStatus, kind: "prior-capture-status"},
		"collected input manifest": {locator: prior.CollectedInputsManifest, kind: "prior-collected-input-manifest"},
		"semantic supplement":      {locator: prior.SemanticSupplement, kind: "prior-semantic-supplement-envelope"},
	} {
		if err := verifyFinalArtifact("collected prior "+label, item.locator, item.kind); err != nil {
			return err
		}
	}
	if len(prior.SemanticFileEnvelopes) < 2 {
		return errors.New("collected prior semantic_verified file census is incomplete")
	}
	if len(prior.LiveChainBundles) == 0 {
		return errors.New("collected prior live-chain bundle census is incomplete")
	}
	for index, locator := range prior.LiveChainBundles {
		if index > 0 && locator.URI <= prior.LiveChainBundles[index-1].URI {
			return errors.New("collected prior live-chain bundles are not canonical")
		}
		if err := verifyFinalArtifact("collected prior live-chain bundle", locator, "prior-live-chain-bundle"); err != nil {
			return err
		}
	}
	for index, locator := range prior.SemanticFileEnvelopes {
		if index > 0 && locator.URI <= prior.SemanticFileEnvelopes[index-1].URI {
			return errors.New("collected prior semantic file envelopes are not canonical")
		}
		if err := verifyFinalArtifact("collected prior semantic file envelope", locator, "prior-semantic-file-envelope"); err != nil {
			return err
		}
	}
	return nil
}

type finalLifecyclePayoutRequirement struct {
	contentHash string
	payoutRoot  string
	records     []FleetLifecyclePayoutEvidence
	observation *OperatorLifecyclePayoutArtifactObservation
}

func finalLifecyclePayoutRequirements(terminal *ScenarioObservation) (map[string]finalLifecyclePayoutRequirement, error) {
	result := map[string]finalLifecyclePayoutRequirement{}
	if terminal == nil || terminal.FleetLifecycle == nil {
		return result, nil
	}
	clientsByOperator := map[int]map[string]bool{}
	clientOperator := map[string]int{}
	clientsByArtifact := map[string]bool{}
	for _, payout := range terminal.FleetLifecycle.Payouts {
		if payout.Epoch == 0 || payout.NoID < 1 || !validSHA256ContentHash(payout.ContentHash) || requireFinalHex32("fleet lifecycle payout root", strings.ToLower(payout.PayoutRoot)) != nil || len(payout.ClientIDs) == 0 || payout.Disposition == "" {
			return nil, errors.New("terminal fleet lifecycle payout index is incomplete")
		}
		key := fmt.Sprintf("%d/%d", payout.Epoch, payout.NoID)
		requirement := result[key]
		if requirement.contentHash != "" && (requirement.contentHash != payout.ContentHash || !strings.EqualFold(requirement.payoutRoot, payout.PayoutRoot)) {
			return nil, fmt.Errorf("fleet lifecycle payout epoch/operator %s names conflicting artifacts", key)
		}
		for _, prior := range requirement.records {
			if prior.Disposition == payout.Disposition {
				return nil, fmt.Errorf("fleet lifecycle payout epoch/operator %s duplicates disposition %s", key, payout.Disposition)
			}
		}
		requirement.contentHash, requirement.payoutRoot = payout.ContentHash, strings.ToLower(payout.PayoutRoot)
		requirement.records = append(requirement.records, payout)
		result[key] = requirement
		for _, clientID := range payout.ClientIDs {
			id, err := finalPayoutClientID(clientID)
			if err != nil {
				return nil, fmt.Errorf("fleet lifecycle payout %s client: %w", payout.Disposition, err)
			}
			encoded := strings.ToLower(id.String())
			if owner, exists := clientOperator[encoded]; exists && owner != payout.NoID {
				return nil, fmt.Errorf("fleet lifecycle payout client %s crosses operators %d/%d", encoded, owner, payout.NoID)
			}
			artifactClient := key + "/" + encoded
			if clientsByArtifact[artifactClient] {
				return nil, fmt.Errorf("fleet lifecycle payout artifact %s assigns client %s to multiple dispositions", key, encoded)
			}
			clientsByArtifact[artifactClient] = true
			clientOperator[encoded] = payout.NoID
			if clientsByOperator[payout.NoID] == nil {
				clientsByOperator[payout.NoID] = map[string]bool{}
			}
			clientsByOperator[payout.NoID][encoded] = true
		}
	}
	if len(result) == 0 {
		return nil, errors.New("terminal fleet lifecycle has no payout artifact index")
	}
	for operatorIndex := range terminal.Operators {
		operator := &terminal.Operators[operatorIndex]
		for observationIndex := range operator.LifecyclePayoutArtifacts {
			observation := &operator.LifecyclePayoutArtifacts[observationIndex]
			key := fmt.Sprintf("%d/%d", observation.Epoch, observation.NoID)
			requirement, required := result[key]
			if !required {
				continue
			}
			wantClients := clientsByOperator[operator.NoID]
			if requirement.observation != nil || observation.NoID != uint64(operator.NoID) || observation.ContentHash != requirement.contentHash || !strings.EqualFold(observation.PayoutRoot, requirement.payoutRoot) || len(observation.Clients) != len(wantClients) {
				return nil, fmt.Errorf("fleet lifecycle payout observation %s is duplicate or differs from its terminal index", key)
			}
			seenClients := make(map[string]bool, len(observation.Clients))
			for _, client := range observation.Clients {
				id, idErr := finalPayoutClientID(client.ClientID)
				encoded := strings.ToLower(id.String())
				if idErr != nil || !wantClients[encoded] || seenClients[encoded] || client.Leaf == client.HeadExcluded {
					return nil, fmt.Errorf("fleet lifecycle payout observation %s has a foreign, duplicate, or ambiguous client", key)
				}
				seenClients[encoded] = true
			}
			copy := *observation
			requirement.observation = &copy
			result[key] = requirement
		}
	}
	for key, requirement := range result {
		if requirement.observation == nil {
			return nil, fmt.Errorf("fleet lifecycle payout observation %s is absent", key)
		}
	}
	return result, nil
}

func verifyFinalLifecyclePayoutArtifact(requirement finalLifecyclePayoutRequirement, artifact *payoutartifact.Artifact) error {
	if artifact == nil || requirement.observation == nil || artifact.ContentHash != requirement.contentHash || !strings.EqualFold("0x"+hex.EncodeToString(artifact.PayoutRoot[:]), requirement.payoutRoot) || artifact.Epoch != requirement.observation.Epoch || artifact.NoID != requirement.observation.NoID {
		return errors.New("fleet lifecycle payout artifact differs from its authenticated compact index")
	}
	providers := make(map[string]payoutartifact.ProviderInput, len(artifact.Providers))
	for _, provider := range artifact.Providers {
		id := strings.ToLower(connect.Id(provider.ClientID).String())
		if _, duplicate := providers[id]; duplicate {
			return fmt.Errorf("fleet lifecycle payout artifact duplicates provider %s", id)
		}
		providers[id] = provider
	}
	leaves := make(map[string]bool, len(artifact.Leaves))
	for _, leaf := range artifact.Leaves {
		id := strings.ToLower(connect.Id(leaf.ClientID).String())
		if leaves[id] {
			return fmt.Errorf("fleet lifecycle payout artifact duplicates leaf %s", id)
		}
		leaves[id] = true
	}
	compact := make(map[string]OperatorPayoutClientTierObservation, len(requirement.observation.Clients))
	for _, client := range requirement.observation.Clients {
		id, err := finalPayoutClientID(client.ClientID)
		if err != nil {
			return err
		}
		key := strings.ToLower(id.String())
		provider, ok := providers[key]
		if !ok || client.Leaf != leaves[key] || client.HeadExcluded != provider.HeadExcluded || client.Leaf == client.HeadExcluded {
			return fmt.Errorf("fleet lifecycle payout compact client %s differs from the full artifact", key)
		}
		compact[key] = client
	}
	for _, payout := range requirement.records {
		_, tier, err := finalLifecyclePayoutVariant(payout.Disposition)
		if err != nil {
			return err
		}
		for _, encoded := range payout.ClientIDs {
			id, err := finalPayoutClientID(encoded)
			if err != nil {
				return err
			}
			client, ok := compact[strings.ToLower(id.String())]
			if !ok || tier == "head-candidate" && (!client.HeadExcluded || client.Leaf) || tier == "pool-tail" && (!client.Leaf || client.HeadExcluded) {
				return fmt.Errorf("fleet lifecycle payout %s client %s is not exclusively in its expected tier", payout.Disposition, id.String())
			}
		}
	}
	return nil
}

func collectFinalPayoutArtifacts(ctx context.Context, cfg *ResolvedConfig, runRoot string, terminal *ScenarioObservation, window *ScenarioAcceptanceWindow) ([]FinalCollectedPayoutArtifact, []FinalCollectedPayoutArtifact, error) {
	requiredLifecycle, err := finalLifecyclePayoutRequirements(terminal)
	if err != nil {
		return nil, nil, err
	}
	operatorView := map[uint64]OperatorView{}
	for _, operator := range terminal.Status.Contracts.Operators {
		operatorView[operator.NoID] = operator
	}
	operatorObservation := map[int]OperatorObservation{}
	for _, operator := range terminal.Operators {
		operatorObservation[operator.NoID] = operator
	}
	client := &http.Client{Timeout: time.Duration(cfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds) * time.Millisecond}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	lastEpoch := window.FirstEpoch + window.EpochCount - 1
	wantFirst := window.FirstEpoch - 1
	if terminal.DishonestDeposit != nil && terminal.DishonestDepositValid {
		if window.FirstEpoch < 2 || terminal.DishonestDeposit.Transaction.Epoch+1 != window.FirstEpoch {
			return nil, nil, errors.New("dishonest-deposit penalty epoch does not immediately precede production acceptance")
		}
		wantFirst = window.FirstEpoch - 2
	}
	seen := map[string]bool{}
	seenLifecycle := map[string]bool{}
	result := make([]FinalCollectedPayoutArtifact, 0, cfg.Config.Topology.Operators*int(lastEpoch-wantFirst+1))
	lifecycle := make([]FinalCollectedPayoutArtifact, 0, len(requiredLifecycle))
	for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
		observation, ok := operatorObservation[noID]
		view, viewOK := operatorView[uint64(noID)]
		wantBase := fmt.Sprintf("http://127.0.0.1:%d", 18080+noID)
		if !ok || !viewOK || observation.APIURL != wantBase || observation.Error != "" || len(observation.ArtifactHashes) == 0 || !common.IsHexAddress(view.RootSigner) {
			return nil, nil, fmt.Errorf("operator %d payout collection identity is incomplete", noID)
		}
		for _, contentHash := range observation.ArtifactHashes {
			if !validSHA256ContentHash(contentHash) {
				return nil, nil, fmt.Errorf("operator %d payout content hash is invalid", noID)
			}
			requestURL := wantBase + "/sn/artifact?hash=" + url.QueryEscape(contentHash)
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
			if err != nil {
				return nil, nil, err
			}
			response, err := client.Do(request)
			if err != nil {
				return nil, nil, fmt.Errorf("operator %d payout %s: %w", noID, contentHash, err)
			}
			data, readErr := readBoundedResponse(response, 32*1024*1024)
			if readErr != nil {
				return nil, nil, fmt.Errorf("operator %d payout %s: %w", noID, contentHash, readErr)
			}
			decoded, err := payoutartifact.Decode(data)
			if err != nil {
				return nil, nil, fmt.Errorf("operator %d payout %s: %w", noID, contentHash, err)
			}
			key := fmt.Sprintf("%d/%d", decoded.Epoch, noID)
			requirement, lifecycleRequired := requiredLifecycle[key]
			inAcceptanceSource := decoded.Epoch >= wantFirst && decoded.Epoch <= lastEpoch
			if decoded.NoID != uint64(noID) || decoded.DeploymentID != cfg.Config.Deployment.DeploymentID || decoded.ChainID != cfg.ChainID || decoded.Netuid != cfg.Netuid || !strings.EqualFold(decoded.GenesisHash, cfg.Public.Chain.GenesisHash) || !strings.EqualFold(decoded.PolicyHash, cfg.PolicyHash) || !strings.EqualFold(decoded.Signer.Hex(), view.RootSigner) || decoded.ContentHash != contentHash || !inAcceptanceSource && (!lifecycleRequired || contentHash != requirement.contentHash) {
				continue
			}
			if inAcceptanceSource && seen[key] {
				return nil, nil, fmt.Errorf("operator %d epoch %d has duplicate accepted payout artifacts", noID, decoded.Epoch)
			}
			if lifecycleRequired {
				if contentHash != requirement.contentHash {
					if inAcceptanceSource {
						return nil, nil, fmt.Errorf("operator %d epoch %d accepted artifact differs from the lifecycle index", noID, decoded.Epoch)
					}
					continue
				}
				if seenLifecycle[key] {
					return nil, nil, fmt.Errorf("operator %d epoch %d duplicates the exact lifecycle payout artifact", noID, decoded.Epoch)
				}
				if err := verifyFinalLifecyclePayoutArtifact(requirement, decoded); err != nil {
					return nil, nil, fmt.Errorf("operator %d epoch %d lifecycle payout: %w", noID, decoded.Epoch, err)
				}
			}
			name := fmt.Sprintf("final-inputs/payouts/no-%d-epoch-%d.json", noID, decoded.Epoch)
			locator, err := persistFinalCollectedArtifact(runRoot, "payout-artifact", name, data)
			if err != nil {
				return nil, nil, err
			}
			if inAcceptanceSource {
				seen[key] = true
				result = append(result, FinalCollectedPayoutArtifact{NoID: uint64(noID), Epoch: decoded.Epoch, Artifact: locator})
			}
			if lifecycleRequired {
				seenLifecycle[key] = true
				lifecycle = append(lifecycle, FinalCollectedPayoutArtifact{NoID: uint64(noID), Epoch: decoded.Epoch, Artifact: locator})
			}
		}
		for epoch := wantFirst; epoch <= lastEpoch; epoch++ {
			if !seen[fmt.Sprintf("%d/%d", epoch, noID)] {
				return nil, nil, fmt.Errorf("operator %d accepted/source epoch %d payout artifact is missing", noID, epoch)
			}
		}
	}
	for key := range requiredLifecycle {
		if !seenLifecycle[key] {
			return nil, nil, fmt.Errorf("exact lifecycle payout artifact %s is missing", key)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Epoch != result[j].Epoch {
			return result[i].Epoch < result[j].Epoch
		}
		return result[i].NoID < result[j].NoID
	})
	sort.Slice(lifecycle, func(i, j int) bool {
		if lifecycle[i].Epoch != lifecycle[j].Epoch {
			return lifecycle[i].Epoch < lifecycle[j].Epoch
		}
		return lifecycle[i].NoID < lifecycle[j].NoID
	})
	return result, lifecycle, nil
}

func finalLifecycleIntentRequirements(terminal *ScenarioObservation, phase string) (map[int][]FleetLifecycleValidatorCensus, error) {
	result := map[int][]FleetLifecycleValidatorCensus{}
	if terminal == nil || terminal.FleetLifecycle == nil {
		return result, nil
	}
	seen := map[string]bool{}
	for _, census := range terminal.FleetLifecycle.CandidateCensuses {
		if census.Phase != phase {
			continue
		}
		if err := validateFleetLifecyclePersistedCensus(census); err != nil {
			return nil, err
		}
		if err := validateFleetLifecycleCensusBlockRange(terminal.FleetLifecycle, census); err != nil {
			return nil, err
		}
		for _, validator := range census.Validators {
			key := fmt.Sprintf("%d/%d/%d/%s", validator.ValidatorID, validator.SettlementEpoch, validator.SubnetEpoch, strings.ToLower(validator.VectorHash))
			if seen[key] {
				return nil, fmt.Errorf("fleet lifecycle phase %s duplicates validator intent %s", phase, key)
			}
			seen[key] = true
			result[validator.ValidatorID] = append(result[validator.ValidatorID], validator)
		}
	}
	return result, nil
}

func finalLifecycleIntentMatches(intent *validatorpkg.SteeringIntent, expected FleetLifecycleValidatorCensus) bool {
	return finalLifecycleIntentMismatch(intent, expected) == ""
}

func finalLifecycleIntentMismatch(intent *validatorpkg.SteeringIntent, expected FleetLifecycleValidatorCensus) string {
	if intent == nil {
		return "intent is nil"
	}
	checks := []struct {
		matches bool
		field   string
	}{
		{intent.ValidatorID == uint64(expected.ValidatorID), "validator_id"},
		{intent.Status == "applied", "status"},
		{intent.SettlementEpoch == expected.SettlementEpoch, "settlement_epoch"},
		{intent.SubnetEpoch == expected.SubnetEpoch, "subnet_epoch"},
		{intent.NativeSnapshotBlock == expected.NativeSnapshot.Number, "native_snapshot_block"},
		{strings.EqualFold(intent.NativeSnapshotHash, expected.NativeSnapshot.Hash), "native_snapshot_hash"},
		{intent.EVMSnapshotBlock == expected.EVMSnapshot.Number, "evm_snapshot_block"},
		{strings.EqualFold(intent.EVMSnapshotHash, expected.EVMSnapshot.Hash), "evm_snapshot_hash"},
		{intent.MeasurementArtifactHash == expected.MeasurementArtifactHash, "measurement_artifact_hash"},
		{strings.EqualFold(intent.VectorHash, expected.VectorHash), "vector_hash"},
		{strings.EqualFold(intent.ExtrinsicHash, expected.ExtrinsicHash), "extrinsic_hash"},
		{intent.FinalizedBlock == expected.Commit.Number, "finalized_block"},
		{strings.EqualFold(intent.FinalizedBlockHash, expected.Commit.Hash), "finalized_block_hash"},
		{intent.RevealBlock == expected.RevealBlock, "reveal_block"},
		{intent.ApplicationBlock == expected.Application.Number, "application_block"},
		{strings.EqualFold(intent.ApplicationBlockHash, expected.Application.Hash), "application_block_hash"},
		{slices.Equal(intent.EligibleHeadUIDs, expected.EligibleUIDs), "eligible_head_uids"},
		{slices.Equal(intent.SelectedHeadUIDs, expected.SelectedUIDs), "selected_head_uids"},
		{slices.Equal(intent.RejectedHeadUIDs, expected.RejectedUIDs), "rejected_head_uids"},
		{len(intent.UIDs) == len(intent.Values), "uid_value_lengths"},
	}
	for _, check := range checks {
		if !check.matches {
			return check.field
		}
	}
	weights := make(map[uint16]uint16, len(intent.UIDs))
	for index, uid := range intent.UIDs {
		if _, duplicate := weights[uid]; duplicate {
			return fmt.Sprintf("duplicate_uid_%d", uid)
		}
		weights[uid] = intent.Values[index]
	}
	for _, weight := range expected.AppliedWeights {
		value, ok := weights[weight.UID]
		if value != weight.Value || !ok && weight.Value != 0 {
			return fmt.Sprintf("applied_weight_uid_%d", weight.UID)
		}
	}
	return ""
}

func collectFinalValidatorInputs(cfg *ResolvedConfig, stateRoot, runRoot string, terminal *ScenarioObservation, phase string, window *ScenarioAcceptanceWindow, startedAt, completedAt time.Time) ([]FinalCollectedValidatorInputs, error) {
	lifecycleRequired, err := finalLifecycleIntentRequirements(terminal, phase)
	if err != nil {
		return nil, err
	}
	serverKeys := make(map[uint64]map[byte]ed25519.PublicKey, len(terminal.Operators))
	for _, operator := range terminal.Operators {
		keys := map[byte]ed25519.PublicKey{}
		for _, key := range operator.VerifyKeys {
			if len(key.PublicKey) != ed25519.PublicKeySize || keys[key.ServerKeyID] != nil {
				return nil, fmt.Errorf("operator %d collected server key history is invalid", operator.NoID)
			}
			keys[key.ServerKeyID] = append(ed25519.PublicKey(nil), key.PublicKey...)
		}
		if operator.NoID < 1 || len(keys) == 0 || serverKeys[uint64(operator.NoID)] != nil {
			return nil, errors.New("collected server key domains are incomplete")
		}
		serverKeys[uint64(operator.NoID)] = keys
	}
	lastEpoch := window.FirstEpoch + window.EpochCount - 1
	result := make([]FinalCollectedValidatorInputs, 0, cfg.Config.Topology.Validators)
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		root := filepath.Join(stateRoot, "runtime", fmt.Sprintf("validator-%d", validatorID), "state")
		storeEntry, err := finalCollectedFileEntry(stateRoot, filepath.ToSlash(filepath.Join("runtime", fmt.Sprintf("validator-%d", validatorID), "state", "steering-intents.json")))
		if err != nil {
			return nil, err
		}
		intents, err := readValidatorIntentFile(stateRoot, validatorID)
		if err != nil {
			return nil, err
		}
		seedPath := filepath.Join(root, "operators", "no-1", "client.key")
		seed, err := os.ReadFile(seedPath)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("validator %d path identity seed is unavailable", validatorID)
		}
		privateKey := ed25519.NewKeyFromSeed(seed)
		vpk := privateKey.Public().(ed25519.PublicKey)
		attemptRecords := make(map[uint64]map[uint64]validatorpkg.AttemptRecord, cfg.Config.Topology.Operators)
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			attemptRecords[uint64(noID)] = map[uint64]validatorpkg.AttemptRecord{}
		}
		storeLocator, err := persistFinalCollectedArtifact(runRoot, "validator-steering-intent-store", fmt.Sprintf("final-inputs/validators/validator-%d-steering-intents.json", validatorID), storeEntry.Data)
		if err != nil {
			return nil, err
		}
		collected := FinalCollectedValidatorInputs{ValidatorID: uint64(validatorID), PathVPK: "0x" + hex.EncodeToString(vpk), IntentStore: storeLocator}
		captureIntent := func(sequence int, intent *validatorpkg.SteeringIntent, collectAttempts bool) (FinalCollectedValidatorIntent, error) {
			measurement, measurementData, err := collectFinalValidatorMeasurement(cfg, stateRoot, root, runRoot, validatorID, intent, terminal)
			if err != nil {
				return FinalCollectedValidatorIntent{}, err
			}
			if collectAttempts {
				if err := collectFinalAttemptCuts(validatorID, measurementData, vpk, serverKeys, attemptRecords); err != nil {
					return FinalCollectedValidatorIntent{}, err
				}
			}
			envelope, err := collectFinalValidatorMeasurementEnvelope(stateRoot, root, runRoot, validatorID, intent, measurementData, startedAt, completedAt)
			if err != nil {
				return FinalCollectedValidatorIntent{}, err
			}
			data, err := json.Marshal(intent)
			if err != nil {
				return FinalCollectedValidatorIntent{}, err
			}
			locator, err := persistFinalCollectedArtifact(runRoot, "steering-intent", fmt.Sprintf("final-inputs/validators/validator-%d-intent-%06d-%s.json", validatorID, sequence+1, strings.TrimPrefix(intent.VectorHash, "0x")), data)
			if err != nil {
				return FinalCollectedValidatorIntent{}, err
			}
			return FinalCollectedValidatorIntent{Sequence: uint64(sequence + 1), SettlementEpoch: intent.SettlementEpoch, SubnetEpoch: intent.SubnetEpoch, Status: intent.Status, VectorHash: intent.VectorHash, Artifact: locator, Measurement: measurement, Envelope: envelope}, nil
		}
		if terminal.DishonestDeposit != nil && terminal.DishonestDepositValid {
			var expected *DishonestDepositValidatorEvidence
			for index := range terminal.DishonestDeposit.Validators {
				candidate := &terminal.DishonestDeposit.Validators[index]
				if candidate.ValidatorID == validatorID {
					expected = candidate
				}
			}
			matches := 0
			for sequence := range intents {
				intent := &intents[sequence]
				if expected == nil || intent.Status != "applied" || intent.SettlementEpoch != terminal.DishonestDeposit.Transaction.Epoch || intent.SubnetEpoch != expected.SubnetEpoch || intent.VectorHash != expected.VectorHash || intent.ApplicationBlock != expected.ApplicationBlock || !strings.EqualFold(intent.ApplicationBlockHash, expected.ApplicationBlockHash) {
					continue
				}
				for auditIndex := range intent.DepositAudits {
					if finalJSONEqual(intent.DepositAudits[auditIndex], expected.Audit) {
						matches++
						captured, err := captureIntent(sequence, intent, false)
						if err != nil {
							return nil, err
						}
						collected.DishonestDepositIntent = &captured
					}
				}
			}
			if expected == nil || matches != 1 {
				return nil, fmt.Errorf("validator %d exact dishonest-deposit applied intent census=%d, want 1", validatorID, matches)
			}
		}
		seenEpoch := map[uint64]bool{}
		requirements := lifecycleRequired[validatorID]
		matchedRequirements := make([]bool, len(requirements))
		for sequence, intent := range intents {
			inAcceptance := intent.SettlementEpoch >= window.FirstEpoch && intent.SettlementEpoch <= lastEpoch
			matchedLifecycle := -1
			for requirementIndex, expected := range requirements {
				if finalLifecycleIntentMatches(&intent, expected) {
					if matchedLifecycle >= 0 {
						return nil, fmt.Errorf("validator %d intent sequence %d matches duplicate lifecycle censes", validatorID, sequence+1)
					}
					matchedLifecycle = requirementIndex
				}
			}
			if !inAcceptance && matchedLifecycle < 0 {
				continue
			}
			if inAcceptance && intent.Status == "applied" {
				if seenEpoch[intent.SettlementEpoch] {
					return nil, fmt.Errorf("validator %d settlement epoch %d has duplicate applied intents", validatorID, intent.SettlementEpoch)
				}
				seenEpoch[intent.SettlementEpoch] = true
			}
			captured, err := captureIntent(sequence, &intent, true)
			if err != nil {
				return nil, err
			}
			if inAcceptance {
				collected.Intents = append(collected.Intents, captured)
			}
			if matchedLifecycle >= 0 {
				if matchedRequirements[matchedLifecycle] {
					return nil, fmt.Errorf("validator %d lifecycle census %d matches multiple intents", validatorID, matchedLifecycle+1)
				}
				matchedRequirements[matchedLifecycle] = true
				collected.LifecycleIntents = append(collected.LifecycleIntents, captured)
			}
		}
		for epoch := window.FirstEpoch; epoch <= lastEpoch; epoch++ {
			if !seenEpoch[epoch] {
				return nil, fmt.Errorf("validator %d accepted epoch %d applied intent is missing", validatorID, epoch)
			}
		}
		for requirementIndex, matched := range matchedRequirements {
			if !matched {
				expected := requirements[requirementIndex]
				return nil, fmt.Errorf("validator %d lifecycle settlement/subnet epoch %d/%d exact applied intent is missing", validatorID, expected.SettlementEpoch, expected.SubnetEpoch)
			}
		}
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			records, summary, err := persistFinalAttemptRecords(runRoot, validatorID, noID, attemptRecords[uint64(noID)])
			if err != nil {
				return nil, err
			}
			collected.Attempts = append(collected.Attempts, summary)
			proofPath := filepath.Join(root, "operators", fmt.Sprintf("no-%d", noID), "proofs.jsonl")
			lines, err := completedReleaseProofLines(proofPath)
			if err != nil {
				return nil, err
			}
			authoritative := map[connect.Id]validatorpkg.ProofRecord{}
			for _, record := range records {
				if record.Disposition == validatorpkg.AttemptDispositionComplete && record.Proof != nil {
					if prior, exists := authoritative[record.Proof.TrailId]; exists {
						if !finalJSONEqual(prior, *record.Proof) {
							return nil, fmt.Errorf("validator %d operator %d trail %s has conflicting authoritative proofs", validatorID, noID, record.Proof.TrailId)
						}
						continue
					}
					authoritative[record.Proof.TrailId] = *record.Proof
				}
			}
			projected := map[connect.Id]bool{}
			for lineIndex, line := range lines {
				var record validatorpkg.ProofRecord
				if err := json.Unmarshal(line, &record); err != nil {
					return nil, err
				}
				if record.Epoch < window.FirstEpoch || record.Epoch > lastEpoch {
					continue
				}
				if err := validatorpkg.VerifyProofRecord(&record, vpk, serverKeys[uint64(noID)], cfg.Policy.Verify.TrailDepth); err != nil {
					return nil, fmt.Errorf("validator %d operator %d proof projection line %d: %w", validatorID, noID, lineIndex+1, err)
				}
				want, ok := authoritative[record.TrailId]
				if !ok || !finalJSONEqual(want, record) {
					return nil, fmt.Errorf("validator %d operator %d proof projection contains orphan or non-authoritative trail %s", validatorID, noID, record.TrailId)
				}
				if projected[record.TrailId] {
					return nil, fmt.Errorf("validator %d operator %d proof projection duplicates trail %s", validatorID, noID, record.TrailId)
				}
				projected[record.TrailId] = true
			}
			var data []byte
			covered := map[uint64]bool{}
			count := uint64(0)
			for _, record := range records {
				if record.Disposition != validatorpkg.AttemptDispositionComplete || record.Proof == nil {
					continue
				}
				if !projected[record.Proof.TrailId] {
					return nil, fmt.Errorf("validator %d operator %d authoritative trail %s is absent from the proof projection", validatorID, noID, record.Proof.TrailId)
				}
				canonical, err := json.Marshal(FinalCollectedProofRecord{Schema: finalCollectedProofRecordSchema, Record: *record.Proof})
				if err != nil {
					return nil, err
				}
				data = append(data, canonical...)
				data = append(data, '\n')
				covered[record.Proof.Epoch] = true
				count++
			}
			for epoch := window.FirstEpoch; epoch <= lastEpoch; epoch++ {
				if !covered[epoch] {
					return nil, fmt.Errorf("validator %d operator %d accepted epoch %d path proof is missing", validatorID, noID, epoch)
				}
			}
			locator, err := persistFinalCollectedArtifact(runRoot, "validator-path-proofs", fmt.Sprintf("final-inputs/validators/validator-%d-no-%d-proofs.jsonl", validatorID, noID), data)
			if err != nil {
				return nil, err
			}
			collected.PathProofs = append(collected.PathProofs, FinalCollectedValidatorPathProof{NoID: uint64(noID), FirstEpoch: window.FirstEpoch, LastEpoch: lastEpoch, ProofCount: count, Artifact: locator})
		}
		result = append(result, collected)
	}
	return result, nil
}

func collectFinalAttemptCuts(validatorID int, measurementData []byte, validatorVPK ed25519.PublicKey, serverKeys map[uint64]map[byte]ed25519.PublicKey, recordsByNO map[uint64]map[uint64]validatorpkg.AttemptRecord) error {
	artifact, _, err := validatorpkg.DecodeReleaseMeasurementArtifact(measurementData)
	if err != nil {
		return fmt.Errorf("validator %d decode attempt-backed measurement: %w", validatorID, err)
	}
	for _, input := range artifact.Inputs {
		cut := input.Stats.AttemptCut
		records, ok := recordsByNO[input.NoID]
		keys := serverKeys[input.NoID]
		if !ok || len(keys) == 0 || cut == nil {
			return fmt.Errorf("validator %d operator %d attempt authority is incomplete", validatorID, input.NoID)
		}
		if err := validatorpkg.VerifyAttemptLedgerCut(cut, validatorVPK, keys); err != nil {
			return fmt.Errorf("validator %d operator %d attempt cut: %w", validatorID, input.NoID, err)
		}
		for _, record := range cut.Records {
			if previous, exists := records[record.Sequence]; exists {
				if !finalJSONEqual(previous, record) {
					return fmt.Errorf("validator %d operator %d attempt sequence %d differs across signed cuts", validatorID, input.NoID, record.Sequence)
				}
				continue
			}
			records[record.Sequence] = record
		}
	}
	return nil
}

func persistFinalAttemptRecords(runRoot string, validatorID, noID int, recordsBySequence map[uint64]validatorpkg.AttemptRecord) ([]validatorpkg.AttemptRecord, FinalCollectedValidatorAttempts, error) {
	sequences := make([]uint64, 0, len(recordsBySequence))
	for sequence := range recordsBySequence {
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	if len(sequences) == 0 {
		return nil, FinalCollectedValidatorAttempts{}, fmt.Errorf("validator %d operator %d signed attempt records are empty", validatorID, noID)
	}
	records := make([]validatorpkg.AttemptRecord, len(sequences))
	dispositions := map[string]uint64{}
	pending := map[connect.Id]bool{}
	var recovered uint64
	for index, sequence := range sequences {
		if index > 0 && sequence != sequences[index-1]+1 {
			return nil, FinalCollectedValidatorAttempts{}, fmt.Errorf("validator %d operator %d signed attempt records have a gap before sequence %d", validatorID, noID, sequence)
		}
		record := recordsBySequence[sequence]
		records[index] = record
		dispositions[record.Disposition]++
		if record.Disposition == validatorpkg.AttemptDispositionPending {
			pending[record.TrailID] = true
		} else {
			if record.Disposition == validatorpkg.AttemptDispositionValidatorError && pending[record.TrailID] {
				recovered++
			}
			delete(pending, record.TrailID)
		}
	}
	if len(pending) != 0 {
		return nil, FinalCollectedValidatorAttempts{}, fmt.Errorf("validator %d operator %d signed attempt records contain unfinished trails", validatorID, noID)
	}
	payload := FinalCollectedAttemptRecords{
		Schema: finalCollectedAttemptRecordsSchema, ValidatorID: uint64(validatorID), NoID: uint64(noID),
		FirstSequence: sequences[0], LastSequence: sequences[len(sequences)-1], DispositionCounts: dispositions,
		PendingRecoveredCount: recovered, Records: records,
	}
	encoded, err := json.Marshal(&payload)
	if err != nil {
		return nil, FinalCollectedValidatorAttempts{}, err
	}
	locator, err := persistFinalCollectedArtifact(runRoot, "validator-attempt-records", fmt.Sprintf("final-inputs/validators/validator-%d-no-%d-attempt-records.json", validatorID, noID), encoded)
	if err != nil {
		return nil, FinalCollectedValidatorAttempts{}, err
	}
	failed := uint64(0)
	for disposition, count := range dispositions {
		if disposition != validatorpkg.AttemptDispositionPending && disposition != validatorpkg.AttemptDispositionComplete {
			failed += count
		}
	}
	summary := FinalCollectedValidatorAttempts{
		NoID: uint64(noID), RecordCount: uint64(len(records)), CheckpointCount: dispositions[validatorpkg.AttemptDispositionPending],
		CompleteCount: dispositions[validatorpkg.AttemptDispositionComplete], FailedCount: failed, PendingCount: uint64(len(pending)), PendingRecoveredCount: recovered, Artifact: locator,
	}
	return records, summary, nil
}

func finalJSONEqual(left, right any) bool {
	leftEncoded, leftErr := json.Marshal(left)
	rightEncoded, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftEncoded, rightEncoded)
}

func collectFinalValidatorMeasurement(cfg *ResolvedConfig, stateRoot, validatorRoot, runRoot string, validatorID int, intent *validatorpkg.SteeringIntent, terminal *ScenarioObservation) (FinalArtifactLocator, []byte, error) {
	if intent == nil || intent.Schema != validatorpkg.SteeringIntentSchema || intent.MeasurementArtifactSize == 0 || intent.MeasurementArtifactSize > 64*1024*1024 || !validSHA256ContentHash(intent.MeasurementArtifactHash) {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement locator is invalid", validatorID)
	}
	wantRelative := filepath.ToSlash(filepath.Join("measurements", strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:")+".json"))
	if intent.MeasurementArtifactPath != wantRelative || filepath.IsAbs(intent.MeasurementArtifactPath) || filepath.Clean(filepath.FromSlash(intent.MeasurementArtifactPath)) != filepath.FromSlash(intent.MeasurementArtifactPath) {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement path is not canonical", validatorID)
	}
	absolute := filepath.Join(validatorRoot, filepath.FromSlash(intent.MeasurementArtifactPath))
	if !pathWithinRoot(stateRoot, absolute) || !pathWithinRoot(validatorRoot, absolute) {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement escapes validator state", validatorID)
	}
	if err := rejectFinalArtifactSymlinkComponents(stateRoot, absolute); err != nil {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement: %w", validatorID, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || uint64(info.Size()) != intent.MeasurementArtifactSize {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement is not the expected private regular file", validatorID)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return FinalArtifactLocator{}, nil, err
	}
	if validatorpkg.ReleaseMeasurementContentHash(data) != intent.MeasurementArtifactHash {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement content hash differs", validatorID)
	}
	artifact, verified, err := validatorpkg.DecodeReleaseMeasurementArtifact(data)
	if err != nil {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement: %w", validatorID, err)
	}
	if err := validatorpkg.VerifyReleaseMeasurementIntent(intent, artifact, verified); err != nil {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement: %w", validatorID, err)
	}
	if terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil || terminal.Status.Contracts.Deployment == nil {
		return FinalArtifactLocator{}, nil, errors.New("terminal deployment is missing during measurement collection")
	}
	deployment := terminal.Status.Contracts.Deployment
	if artifact.DeploymentID != cfg.Config.Deployment.DeploymentID || artifact.ChainID != cfg.ChainID || !strings.EqualFold(artifact.GenesisHash, cfg.Public.Chain.GenesisHash) || artifact.ValidatorID != uint64(validatorID) || artifact.Netuid != cfg.Netuid || !strings.EqualFold(artifact.PolicyHash, cfg.PolicyHash) || !strings.EqualFold(artifact.Coordinator, deployment.CoordinatorProxy.Hex()) || !strings.EqualFold(artifact.SettlementVault, deployment.SettlementVault.Hex()) {
		return FinalArtifactLocator{}, nil, fmt.Errorf("validator %d epoch measurement deployment identity differs", validatorID)
	}
	name := fmt.Sprintf("final-inputs/validators/validator-%d-measurement-%s.json", validatorID, strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:"))
	locator, err := persistFinalCollectedArtifact(runRoot, "validator-release-measurement", name, data)
	return locator, data, err
}

func collectFinalValidatorMeasurementEnvelope(stateRoot, validatorRoot, runRoot string, validatorID int, intent *validatorpkg.SteeringIntent, measurement []byte, startedAt, completedAt time.Time) (FinalArtifactLocator, error) {
	if intent == nil || intent.Prepared == nil || intent.MeasurementEnvelopeSize == 0 || intent.MeasurementEnvelopeSize > 1024*1024 || !validSHA256ContentHash(intent.MeasurementEnvelopeHash) {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope locator is invalid", validatorID)
	}
	wantRelative := filepath.ToSlash(filepath.Join("measurements", "envelopes", strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:")+".json"))
	if intent.MeasurementEnvelopePath != wantRelative || filepath.IsAbs(intent.MeasurementEnvelopePath) || filepath.Clean(filepath.FromSlash(intent.MeasurementEnvelopePath)) != filepath.FromSlash(intent.MeasurementEnvelopePath) {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope path is not canonical", validatorID)
	}
	absolute := filepath.Join(validatorRoot, filepath.FromSlash(intent.MeasurementEnvelopePath))
	if !pathWithinRoot(stateRoot, absolute) || !pathWithinRoot(validatorRoot, absolute) {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope escapes validator state", validatorID)
	}
	if err := rejectFinalArtifactSymlinkComponents(stateRoot, absolute); err != nil {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope: %w", validatorID, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || uint64(info.Size()) != intent.MeasurementEnvelopeSize {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope is not the expected private regular file", validatorID)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return FinalArtifactLocator{}, err
	}
	if validatorpkg.ReleaseMeasurementEnvelopeContentHash(data) != intent.MeasurementEnvelopeHash {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope content hash differs", validatorID)
	}
	envelope, err := validatorpkg.DecodeReleaseMeasurementEnvelope(data)
	if err != nil {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope: %w", validatorID, err)
	}
	hotkeyBytes, err := hex.DecodeString(strings.TrimPrefix(envelope.ValidatorHotkey, "0x"))
	if err != nil || len(hotkeyBytes) != 32 {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope hotkey is invalid", validatorID)
	}
	var hotkey [32]byte
	copy(hotkey[:], hotkeyBytes)
	if _, _, err := validatorpkg.VerifyReleaseMeasurementEnvelope(envelope, measurement, hotkey, intent.SelfUID, strings.ToLower(intent.Prepared.ExtrinsicHash)); err != nil {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope: %w", validatorID, err)
	}
	signedAt, err := time.Parse(time.RFC3339Nano, envelope.SignedAt)
	if err != nil || signedAt.Before(startedAt) || signedAt.After(completedAt) {
		return FinalArtifactLocator{}, fmt.Errorf("validator %d epoch measurement envelope is outside the campaign time window", validatorID)
	}
	name := fmt.Sprintf("final-inputs/validators/validator-%d-measurement-envelope-%s.json", validatorID, strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:"))
	return persistFinalCollectedArtifact(runRoot, "validator-release-measurement-envelope", name, data)
}

func readBoundedResponse(response *http.Response, maximum int64) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, errors.New("HTTP response is empty")
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var buffer bytes.Buffer
	limited := io.LimitReader(response.Body, maximum+1)
	if _, err := buffer.ReadFrom(limited); err != nil {
		return nil, err
	}
	if int64(buffer.Len()) > maximum {
		return nil, fmt.Errorf("HTTP response exceeds %d bytes", maximum)
	}
	return buffer.Bytes(), nil
}

func persistFinalCollectedArtifact(runRoot, kind, relative string, data []byte) (FinalArtifactLocator, error) {
	if len(data) == 0 || kind == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != filepath.FromSlash(relative) {
		return FinalArtifactLocator{}, errors.New("collected semantic artifact path/content is invalid")
	}
	absolute := filepath.Join(runRoot, filepath.FromSlash(relative))
	if !pathWithinRoot(runRoot, absolute) {
		return FinalArtifactLocator{}, errors.New("collected semantic artifact escapes run directory")
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return FinalArtifactLocator{}, err
	}
	if err := writeImmutableEvidenceArchive(absolute, data); err != nil {
		return FinalArtifactLocator{}, err
	}
	return FinalArtifactLocator{Kind: kind, URI: filepath.ToSlash(relative), ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}, nil
}

func verifyFinalSemanticCollectedInputs(cfg *ResolvedConfig, value *FinalSemanticCollectedInputs) error {
	if cfg == nil || cfg.Config == nil || value == nil || value.Schema != finalSemanticCollectedInputsSchema || value.RunID == "" || value.ResultHash == "" || value.Window.Schema != "urnetwork-sim-acceptance-window-v1" || value.Window.FirstEpoch == 0 || value.Window.EpochCount == 0 || len(value.Validators) != cfg.Config.Topology.Validators || len(value.ClosedInputBundles) == 0 {
		return errors.New("collected final semantic inputs are incomplete")
	}
	if value.Phase != "release-1.0" && value.Phase != "production-soak" {
		return errors.New("collected final semantic phase is invalid")
	}
	wantEpochCount := uint64(cfg.Config.Scenarios.ShortEpochs)
	if value.Phase == "production-soak" {
		wantEpochCount = uint64(cfg.Config.Scenarios.ProductionEpochs)
	}
	lastEpoch, ok := checkedAdd(value.Window.FirstEpoch, value.Window.EpochCount-1)
	if !ok || value.Window.EpochCount != wantEpochCount {
		return errors.New("collected final semantic acceptance epoch range is invalid")
	}
	payoutSourceOffset := uint64(1)
	if value.Phase == "production-soak" {
		payoutSourceOffset = 2
	}
	payoutEpochCount, ok := checkedAdd(value.Window.EpochCount, payoutSourceOffset)
	if !ok {
		return errors.New("collected final semantic payout epoch range overflows")
	}
	wantPayouts, ok := checkedMul(uint64(cfg.Config.Topology.Operators), payoutEpochCount)
	if !ok || wantPayouts > uint64(^uint(0)>>1) || uint64(len(value.Payouts)) != wantPayouts {
		return errors.New("collected final semantic payout coverage is incomplete")
	}
	if value.Phase == "release-1.0" && value.PriorPhase != nil {
		return errors.New("release final semantic inputs unexpectedly bind a prior phase")
	}
	if value.Phase == "production-soak" {
		if err := verifyFinalCollectedPriorPhase(value.PriorPhase, value); err != nil {
			return err
		}
	}
	if err := requireFinalHex32("collected result hash", value.ResultHash); err != nil {
		return err
	}
	if err := verifyFinalArtifact("collected policy", value.Policy, "policy"); err != nil {
		return err
	}
	for label, item := range map[string]struct {
		locator FinalArtifactLocator
		kind    string
	}{
		"scenario result":      {locator: value.ScenarioResult, kind: "scenario-result-candidate"},
		"terminal observation": {locator: value.TerminalObservation, kind: "scenario-terminal-observation"},
		"observation history":  {locator: value.ObservationHistory, kind: "scenario-observation-history"},
	} {
		if err := verifyFinalArtifact("collected "+label, item.locator, item.kind); err != nil {
			return err
		}
	}
	for index, locator := range value.ClosedInputBundles {
		if index > 0 && locator.URI <= value.ClosedInputBundles[index-1].URI {
			return errors.New("collected closed-input bundles are not canonical")
		}
		if err := verifyFinalArtifact("collected closed-input bundle", locator, "closed-input-bundle"); err != nil {
			return err
		}
	}
	for index, payout := range value.Payouts {
		wantEpoch := value.Window.FirstEpoch - payoutSourceOffset + uint64(index/cfg.Config.Topology.Operators)
		wantNoID := uint64(index%cfg.Config.Topology.Operators + 1)
		if payout.NoID != wantNoID || payout.Epoch != wantEpoch || payout.Epoch > lastEpoch || verifyFinalArtifact("collected payout", payout.Artifact, "payout-artifact") != nil {
			return errors.New("collected payout manifest is invalid")
		}
	}
	for index, payout := range value.LifecyclePayouts {
		if payout.NoID == 0 || payout.Epoch == 0 || index > 0 && (payout.Epoch < value.LifecyclePayouts[index-1].Epoch || payout.Epoch == value.LifecyclePayouts[index-1].Epoch && payout.NoID <= value.LifecyclePayouts[index-1].NoID) || verifyFinalArtifact("collected lifecycle payout", payout.Artifact, "payout-artifact") != nil {
			return errors.New("collected lifecycle payout manifest is invalid")
		}
	}
	for i, validator := range value.Validators {
		if validator.ValidatorID != uint64(i+1) || len(validator.Intents) < int(value.Window.EpochCount) || len(validator.Attempts) != cfg.Config.Topology.Operators || len(validator.PathProofs) != cfg.Config.Topology.Operators {
			return errors.New("collected validator input coverage is incomplete")
		}
		if _, err := finalEd25519PublicKey("collected validator VPK", validator.PathVPK); err != nil {
			return err
		}
		if err := verifyFinalArtifact("collected validator intent store", validator.IntentStore, "validator-steering-intent-store"); err != nil {
			return err
		}
		if value.Phase == "release-1.0" && validator.DishonestDepositIntent != nil {
			return errors.New("release collected inputs unexpectedly contain a dishonest-deposit intent")
		}
		if value.Phase == "production-soak" {
			intent := validator.DishonestDepositIntent
			if intent == nil || intent.Sequence == 0 || intent.SettlementEpoch+1 != value.Window.FirstEpoch || intent.SubnetEpoch == 0 || intent.Status != "applied" || requireFinalHex32("collected dishonest intent vector hash", intent.VectorHash) != nil || verifyFinalArtifact("collected dishonest validator intent", intent.Artifact, "steering-intent") != nil || verifyFinalArtifact("collected dishonest validator measurement", intent.Measurement, "validator-release-measurement") != nil || verifyFinalArtifact("collected dishonest validator measurement envelope", intent.Envelope, "validator-release-measurement-envelope") != nil {
				return fmt.Errorf("collected validator %d dishonest-deposit intent is incomplete", validator.ValidatorID)
			}
		}
		applied := map[uint64]bool{}
		for intentIndex, intent := range validator.Intents {
			if intent.Sequence == 0 || (intentIndex > 0 && intent.Sequence <= validator.Intents[intentIndex-1].Sequence) || intent.SettlementEpoch < value.Window.FirstEpoch || intent.SettlementEpoch >= value.Window.FirstEpoch+value.Window.EpochCount || intent.SubnetEpoch == 0 || requireFinalHex32("collected intent vector hash", intent.VectorHash) != nil || (intent.Status != "pending" && intent.Status != "finalized" && intent.Status != "applied" && intent.Status != "failed") || verifyFinalArtifact("collected validator intent", intent.Artifact, "steering-intent") != nil || verifyFinalArtifact("collected validator measurement", intent.Measurement, "validator-release-measurement") != nil || verifyFinalArtifact("collected validator measurement envelope", intent.Envelope, "validator-release-measurement-envelope") != nil {
				return errors.New("collected validator intent manifest is invalid")
			}
			if intent.Status == "applied" {
				if applied[intent.SettlementEpoch] {
					return errors.New("collected validator has duplicate applied epoch intent")
				}
				applied[intent.SettlementEpoch] = true
			}
		}
		for intentIndex, intent := range validator.LifecycleIntents {
			if intent.Sequence == 0 || intentIndex > 0 && intent.Sequence <= validator.LifecycleIntents[intentIndex-1].Sequence || intent.SettlementEpoch == 0 || intent.SubnetEpoch == 0 || intent.Status != "applied" || requireFinalHex32("collected lifecycle intent vector hash", intent.VectorHash) != nil || verifyFinalArtifact("collected lifecycle validator intent", intent.Artifact, "steering-intent") != nil || verifyFinalArtifact("collected lifecycle validator measurement", intent.Measurement, "validator-release-measurement") != nil || verifyFinalArtifact("collected lifecycle validator measurement envelope", intent.Envelope, "validator-release-measurement-envelope") != nil {
				return errors.New("collected lifecycle validator intent manifest is invalid")
			}
		}
		for epoch := value.Window.FirstEpoch; epoch <= lastEpoch; epoch++ {
			if !applied[epoch] {
				return fmt.Errorf("collected validator %d accepted epoch %d applied intent is absent", validator.ValidatorID, epoch)
			}
		}
		for noIndex, attempts := range validator.Attempts {
			if attempts.NoID != uint64(noIndex+1) || attempts.RecordCount == 0 || attempts.CompleteCount == 0 || attempts.PendingCount != 0 || attempts.CompleteCount+attempts.FailedCount+attempts.CheckpointCount != attempts.RecordCount || attempts.PendingRecoveredCount > attempts.FailedCount || attempts.PendingRecoveredCount > attempts.CheckpointCount || verifyFinalArtifact("collected validator attempts", attempts.Artifact, "validator-attempt-records") != nil {
				return errors.New("collected validator attempt authority is invalid")
			}
		}
		for noIndex, proof := range validator.PathProofs {
			if proof.NoID != uint64(noIndex+1) || proof.FirstEpoch != value.Window.FirstEpoch || proof.LastEpoch != lastEpoch || proof.ProofCount < value.Window.EpochCount || verifyFinalArtifact("collected validator path proofs", proof.Artifact, "validator-path-proofs") != nil {
				return errors.New("collected validator proof authority is invalid")
			}
		}
	}
	want, err := finalSemanticCollectedInputsHash(value)
	if err != nil {
		return err
	}
	if value.EvidenceHash != want {
		return errors.New("collected final semantic input hash mismatch")
	}
	return nil
}

func verifyFinalCollectedClosedGraph(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, value *FinalSemanticCollectedInputs) error {
	load, err := NewFinalSemanticCampaignArtifactLoader(stateRoot, runRoot)
	if err != nil {
		return err
	}
	locators := []FinalArtifactLocator{value.Policy, value.ScenarioResult, value.TerminalObservation, value.ObservationHistory}
	if value.PriorPhase != nil {
		locators = append(locators, value.PriorPhase.ScenarioResult, value.PriorPhase.OwnerCompletion, value.PriorPhase.EvidenceManifest, value.PriorPhase.CaptureStatus, value.PriorPhase.CollectedInputsManifest, value.PriorPhase.SemanticSupplement)
		locators = append(locators, value.PriorPhase.LiveChainBundles...)
		locators = append(locators, value.PriorPhase.SemanticFileEnvelopes...)
	}
	locators = append(locators, value.ClosedInputBundles...)
	for _, payout := range value.Payouts {
		locators = append(locators, payout.Artifact)
	}
	for _, payout := range value.LifecyclePayouts {
		locators = append(locators, payout.Artifact)
	}
	for _, validator := range value.Validators {
		locators = append(locators, validator.IntentStore)
		if validator.DishonestDepositIntent != nil {
			locators = append(locators, validator.DishonestDepositIntent.Artifact, validator.DishonestDepositIntent.Measurement, validator.DishonestDepositIntent.Envelope)
		}
		for _, intent := range validator.Intents {
			locators = append(locators, intent.Artifact, intent.Measurement, intent.Envelope)
		}
		for _, intent := range validator.LifecycleIntents {
			locators = append(locators, intent.Artifact, intent.Measurement, intent.Envelope)
		}
		for _, proof := range validator.PathProofs {
			locators = append(locators, proof.Artifact)
		}
		for _, attempts := range validator.Attempts {
			locators = append(locators, attempts.Artifact)
		}
	}
	seen := map[string]bool{}
	loaded := map[string][]byte{}
	for _, locator := range locators {
		if seen[locator.URI] {
			continue
		}
		seen[locator.URI] = true
		data, err := load(ctx, locator)
		if err != nil {
			return fmt.Errorf("read back collected semantic input %s: %w", locator.URI, err)
		}
		if uint64(len(data)) != locator.SizeBytes || bytesSHA256(data) != locator.ContentHash {
			return fmt.Errorf("read back collected semantic input %s differs", locator.URI)
		}
		loaded[locator.Kind] = data
		loaded[locator.URI] = data
		if locator.Kind == "closed-input-bundle" {
			if _, err := decodeFinalCollectedFileBundle(data); err != nil {
				return fmt.Errorf("read back collected semantic bundle %s: %w", locator.URI, err)
			}
		} else if locator.Kind == "validator-attempt-records" {
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			var records FinalCollectedAttemptRecords
			if err := decoder.Decode(&records); err != nil || records.Schema != finalCollectedAttemptRecordsSchema || len(records.Records) == 0 {
				return fmt.Errorf("read back collected attempt authority %s is invalid", locator.URI)
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return fmt.Errorf("read back collected attempt authority %s has trailing JSON", locator.URI)
			}
			var summary *FinalCollectedValidatorAttempts
			for validatorIndex := range value.Validators {
				for attemptIndex := range value.Validators[validatorIndex].Attempts {
					candidate := &value.Validators[validatorIndex].Attempts[attemptIndex]
					if candidate.Artifact == locator {
						summary = candidate
					}
				}
			}
			if summary == nil {
				return fmt.Errorf("read back collected attempt authority %s has no manifest summary", locator.URI)
			}
			if err := verifyFinalCollectedAttemptRecords(&records, summary); err != nil {
				return fmt.Errorf("read back collected attempt authority %s: %w", locator.URI, err)
			}
		}
	}
	if value.PriorPhase != nil {
		if err := verifyFinalCollectedPriorPhaseBytes(cfg, value.PriorPhase, loaded); err != nil {
			return err
		}
	}
	var terminal ScenarioObservation
	if err := decodeStrictJSONBytes(loaded[value.TerminalObservation.URI], &terminal); err != nil {
		return fmt.Errorf("decode collected terminal observation for lifecycle payout replay: %w", err)
	}
	if err := verifyFinalCollectedLifecyclePayouts(value, &terminal, loaded); err != nil {
		return err
	}
	if err := verifyFinalCollectedLifecycleIntents(value, &terminal, loaded); err != nil {
		return err
	}
	manifestPath := filepath.Join(runRoot, "final-inputs", "manifest.json")
	var manifest FinalSemanticCollectedInputs
	if err := decodeStrictJSONFile(manifestPath, &manifest); err != nil {
		return fmt.Errorf("read back collected semantic manifest: %w", err)
	}
	if manifest.EvidenceHash != value.EvidenceHash {
		return errors.New("read back collected semantic manifest differs")
	}
	return verifyFinalSemanticCollectedInputs(cfg, &manifest)
}

func verifyFinalCollectedLifecyclePayouts(value *FinalSemanticCollectedInputs, terminal *ScenarioObservation, loaded map[string][]byte) error {
	if value == nil || terminal == nil {
		return errors.New("collected lifecycle payout verification context is incomplete")
	}
	requirements, err := finalLifecyclePayoutRequirements(terminal)
	if err != nil {
		return err
	}
	if len(value.LifecyclePayouts) != len(requirements) {
		return fmt.Errorf("collected lifecycle payout artifact count=%d, want %d", len(value.LifecyclePayouts), len(requirements))
	}
	seen := make(map[string]bool, len(requirements))
	for _, collected := range value.LifecyclePayouts {
		key := fmt.Sprintf("%d/%d", collected.Epoch, collected.NoID)
		requirement, ok := requirements[key]
		if !ok || seen[key] {
			return fmt.Errorf("collected lifecycle payout %s is unexpected or duplicate", key)
		}
		data := loaded[collected.Artifact.URI]
		artifact, err := payoutartifact.Decode(data)
		if err != nil {
			return fmt.Errorf("decode collected lifecycle payout %s: %w", key, err)
		}
		if err := verifyFinalLifecyclePayoutArtifact(requirement, artifact); err != nil {
			return fmt.Errorf("verify collected lifecycle payout %s: %w", key, err)
		}
		seen[key] = true
	}
	return nil
}

func verifyFinalCollectedLifecycleIntents(value *FinalSemanticCollectedInputs, terminal *ScenarioObservation, loaded map[string][]byte) error {
	if value == nil || terminal == nil {
		return errors.New("collected lifecycle intent verification context is incomplete")
	}
	requirements, err := finalLifecycleIntentRequirements(terminal, value.Phase)
	if err != nil {
		return err
	}
	for _, validator := range value.Validators {
		expected := requirements[int(validator.ValidatorID)]
		if len(validator.LifecycleIntents) != len(expected) {
			return fmt.Errorf("collected validator %d lifecycle intent count=%d, want %d", validator.ValidatorID, len(validator.LifecycleIntents), len(expected))
		}
		matched := make([]bool, len(expected))
		for _, collected := range validator.LifecycleIntents {
			var intent validatorpkg.SteeringIntent
			if err := decodeStrictJSONBytes(loaded[collected.Artifact.URI], &intent); err != nil || intent.ValidatorID != validator.ValidatorID || collected.SettlementEpoch != intent.SettlementEpoch || collected.SubnetEpoch != intent.SubnetEpoch || collected.Status != intent.Status || !strings.EqualFold(collected.VectorHash, intent.VectorHash) {
				return stateMismatchError(err, "collected validator %d lifecycle intent differs from its locator", validator.ValidatorID)
			}
			match := -1
			for index, requirement := range expected {
				if finalLifecycleIntentMatches(&intent, requirement) {
					if match >= 0 {
						return fmt.Errorf("collected validator %d lifecycle intent matches duplicate censes", validator.ValidatorID)
					}
					match = index
				}
			}
			if match < 0 || matched[match] {
				return fmt.Errorf("collected validator %d lifecycle intent is unexpected or duplicated", validator.ValidatorID)
			}
			matched[match] = true
		}
	}
	return nil
}

func verifyFinalCollectedPriorPhaseBytes(cfg *ResolvedConfig, prior *FinalCollectedPriorPhaseInputs, loaded map[string][]byte) error {
	if cfg == nil || prior == nil {
		return errors.New("collected prior phase verification context is incomplete")
	}
	var result ScenarioResult
	if err := decodeStrictJSONBytes(loaded["prior-scenario-result"], &result); err != nil {
		return fmt.Errorf("decode collected prior scenario result: %w", err)
	}
	if result.Name != "release-1.0" || result.RunID != prior.RunID || result.EvidenceHash != prior.ResultHash || result.AcceptanceWindow == nil || *result.AcceptanceWindow != prior.Window || result.LifecycleHandoff == nil || result.PriorRelease != nil {
		return errors.New("collected prior scenario result differs from its lineage")
	}
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return err
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return errors.New("collected prior evidence owner is absent")
	}
	ownerKey, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		return err
	}
	var complete ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(loaded["prior-owner-completion-envelope"], &complete); err != nil || verifyEvidence(&complete, &ownerKey.PublicKey) != nil || complete.Kind != "scenario-complete" || complete.RunID != prior.RunID {
		return stateMismatchError(err, "collected prior owner completion is invalid")
	}
	var completePayload scenarioCompletePayload
	if err := decodeStrictJSONBytes(complete.Payload, &completePayload); err != nil || completePayload.ResultHash != prior.ResultHash || !validSHA256ContentHash(completePayload.EvidenceManifestHash) || completePayload.LifecycleHandoff == nil || *completePayload.LifecycleHandoff != *result.LifecycleHandoff || completePayload.PriorRelease != nil {
		return stateMismatchError(err, "collected prior owner completion payload is invalid")
	}
	var manifestEnvelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(loaded["prior-evidence-manifest-envelope"], &manifestEnvelope); err != nil {
		return fmt.Errorf("collected prior evidence manifest envelope is invalid: %w", err)
	}
	if err := verifyFinalCollectedPriorManifestEnvelope(&manifestEnvelope, &ownerKey.PublicKey, prior.RunID, completePayload.EvidenceManifestHash); err != nil {
		return stateMismatchError(err, "collected prior evidence manifest envelope is invalid")
	}
	manifest, err := decodeCampaignEvidenceManifest(&manifestEnvelope)
	if err != nil || manifest.ResultHash != prior.ResultHash || !strings.EqualFold(manifest.BundlePayloadHash, completePayload.BundlePayloadHash) {
		return stateMismatchError(err, "collected prior evidence manifest payload is invalid")
	}
	files, err := campaignEvidenceManifestFiles(manifest.Files)
	if err != nil || !stringMapsEqual(files, completePayload.Files) {
		return stateMismatchError(err, "collected prior evidence manifest files differ from completion")
	}
	var collected FinalSemanticCollectedInputs
	if err := decodeStrictJSONBytes(loaded["prior-collected-input-manifest"], &collected); err != nil || verifyFinalSemanticCollectedInputs(cfg, &collected) != nil || collected.Phase != "release-1.0" || collected.RunID != prior.RunID || collected.ResultHash != prior.ResultHash || collected.Window != prior.Window {
		return stateMismatchError(err, "collected prior semantic inputs are invalid")
	}
	var status FinalSemanticCaptureStatus
	if err := decodeStrictJSONBytes(loaded["prior-capture-status"], &status); err != nil || verifyFinalSemanticCaptureStatus(&status, &collected) != nil || status.Phase != "release-1.0" || status.RunID != prior.RunID || status.ResultHash != prior.ResultHash {
		return stateMismatchError(err, "collected prior capture status is invalid")
	}
	var closedNativeTerminal ChainHead
	seenChainSnapshot := false
	for _, locator := range prior.LiveChainBundles {
		bundle, err := decodeFinalCollectedFileBundle(loaded[locator.URI])
		if err != nil || finalSemanticBundleClass(bundle.Name) != "live-chain" {
			return stateMismatchError(err, "collected prior live-chain bundle is invalid")
		}
		for _, entry := range bundle.Files {
			if entry.Path != "live-chain/final-chain-snapshot.json" {
				continue
			}
			if seenChainSnapshot {
				return errors.New("collected prior live-chain snapshot is duplicated")
			}
			var snapshot FinalCollectedChainSnapshot
			if err := decodeStrictJSONBytes(entry.Data, &snapshot); err != nil || snapshot.Schema != finalCollectedChainSnapshotSchema || snapshot.Phase != "release-1.0" || snapshot.RunID != prior.RunID || snapshot.DeploymentID != result.DeploymentID || snapshot.EVMHead != result.EndHead || snapshot.EVMHead.Number < prior.Window.TerminalBlock || len(snapshot.NativeHeads) == 0 || snapshot.NativeHeads[len(snapshot.NativeHeads)-1] != snapshot.NativeHead {
				return stateMismatchError(err, "collected prior live-chain snapshot differs from the release")
			}
			closedNativeTerminal, seenChainSnapshot = snapshot.NativeHead, true
		}
	}
	if !seenChainSnapshot {
		return errors.New("collected prior live-chain graph lacks its terminal snapshot")
	}
	var supplement ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(loaded[prior.SemanticSupplement.URI], &supplement); err != nil || verifyFinalSemanticOwnerEnvelope(cfg, &supplement, &ownerKey.PublicKey, finalSemanticSupplementKind, prior.RunID) != nil {
		return stateMismatchError(err, "collected prior semantic_verified supplement is invalid")
	}
	var supplementPayload FinalSemanticSupplementPayload
	if err := decodeStrictJSONBytes(supplement.Payload, &supplementPayload); err != nil || supplementPayload.Schema != finalSemanticSupplementSchema || supplementPayload.Status != finalSemanticSupplementStatus || supplementPayload.Phase != "release-1.0" || supplementPayload.RunID != prior.RunID || supplementPayload.ResultHash != prior.ResultHash || supplementPayload.ScenarioCompleteHash != complete.ContentHash || supplementPayload.ScenarioEvidenceManifestHash != manifestEnvelope.ContentHash || supplementPayload.CaptureStatusHash != status.EvidenceHash || supplementPayload.CollectedInputsHash != collected.EvidenceHash || !validCanonicalHashHex(supplementPayload.SemanticEvidenceHash) || !validCanonicalHashHex(supplementPayload.PublicTranscriptHash) {
		return stateMismatchError(err, "collected prior semantic_verified payload does not bind the release closure")
	}
	byEnvelopeHash := make(map[string][]byte, len(prior.SemanticFileEnvelopes))
	for _, locator := range prior.SemanticFileEnvelopes {
		data := loaded[locator.URI]
		var envelope ReleaseEvidenceEnvelope
		if err := decodeStrictJSONBytes(data, &envelope); err != nil || verifyFinalSemanticOwnerEnvelope(cfg, &envelope, &ownerKey.PublicKey, finalSemanticSupplementFileKind, prior.RunID) != nil {
			return stateMismatchError(err, "collected prior semantic file envelope is invalid")
		}
		if byEnvelopeHash[envelope.ContentHash] != nil {
			return errors.New("collected prior semantic file envelope hash is duplicated")
		}
		byEnvelopeHash[envelope.ContentHash] = data
	}
	seenSemantic, seenMarkdown := false, false
	for _, item := range supplementPayload.Files {
		data := byEnvelopeHash[item.EnvelopeHash]
		var envelope ReleaseEvidenceEnvelope
		if len(data) == 0 || decodeStrictJSONBytes(data, &envelope) != nil {
			return fmt.Errorf("collected prior semantic file %s lacks its signed envelope", item.Path)
		}
		var filePayload finalSemanticSupplementFilePayload
		if err := decodeStrictJSONBytes(envelope.Payload, &filePayload); err != nil || filePayload.Schema != finalSemanticSupplementFileSchema || filePayload.RunID != prior.RunID || filePayload.Path != item.Path || filePayload.ContentHash != item.ContentHash || filePayload.Size != item.Size || uint64(len(filePayload.Data)) != item.Size || bytesSHA256(filePayload.Data) != item.ContentHash {
			return stateMismatchError(err, "collected prior semantic file payload differs for %s", item.Path)
		}
		delete(byEnvelopeHash, item.EnvelopeHash)
		if item.Path == finalSemanticEvidenceFilename {
			var semantic FinalSemanticEvidence
			if err := decodeStrictJSONBytes(filePayload.Data, &semantic); err != nil || VerifyFinalSemanticEvidence(&semantic) != nil || semantic.PublicVerification == nil || semantic.Phase != "release-1.0" || semantic.RunID != prior.RunID || semantic.ResultHash != prior.ResultHash || semantic.EvidenceHash != supplementPayload.SemanticEvidenceHash || semantic.PublicVerification.TranscriptHash != supplementPayload.PublicTranscriptHash || semantic.Window != prior.Window || semantic.NativeTerminalHead != closedNativeTerminal || semantic.EVMTerminalHead != result.EndHead {
				return stateMismatchError(err, "collected prior semantic evidence is invalid")
			}
			seenSemantic = true
		}
		seenMarkdown = seenMarkdown || item.Path == finalSemanticMarkdownFilename
	}
	if len(byEnvelopeHash) != 0 || !seenSemantic || !seenMarkdown {
		return errors.New("collected prior semantic_verified file census is incomplete or contains extras")
	}
	return nil
}

// Keep the prior-phase lineage on the same canonical manifest kind emitted by
// publishCampaignEvidenceArchive. A separate literal here previously made a
// valid release archive impossible to consume from the production phase.
func verifyFinalCollectedPriorManifestEnvelope(envelope *ReleaseEvidenceEnvelope, owner *ecdsa.PublicKey, runID, contentHash string) error {
	if envelope == nil || owner == nil || strings.TrimSpace(runID) == "" || !validSHA256ContentHash(contentHash) {
		return errors.New("collected prior evidence manifest verification context is incomplete")
	}
	if err := verifyEvidence(envelope, owner); err != nil {
		return err
	}
	if envelope.Kind != campaignEvidenceManifestKind || envelope.RunID != runID || !strings.EqualFold(envelope.ContentHash, contentHash) {
		return errors.New("collected prior evidence manifest identity differs")
	}
	return nil
}

func verifyFinalCollectedAttemptRecords(records *FinalCollectedAttemptRecords, summary *FinalCollectedValidatorAttempts) error {
	if records == nil || summary == nil || records.Schema != finalCollectedAttemptRecordsSchema || records.ValidatorID == 0 || records.NoID != summary.NoID || len(records.Records) == 0 || records.FirstSequence == 0 || records.LastSequence < records.FirstSequence || records.LastSequence-records.FirstSequence+1 != uint64(len(records.Records)) {
		return errors.New("collected attempt record identity or range is invalid")
	}
	dispositions := map[string]uint64{}
	pending := map[connect.Id]bool{}
	terminal := map[connect.Id]bool{}
	complete, failed, checkpoints, recovered := uint64(0), uint64(0), uint64(0), uint64(0)
	for index, record := range records.Records {
		if record.Sequence != records.FirstSequence+uint64(index) || record.TrailID == (connect.Id{}) || terminal[record.TrailID] {
			return errors.New("collected attempt record sequence or lifecycle is invalid")
		}
		dispositions[record.Disposition]++
		if record.Disposition == validatorpkg.AttemptDispositionPending {
			pending[record.TrailID] = true
			checkpoints++
			continue
		}
		if !pending[record.TrailID] {
			return errors.New("collected terminal attempt has no pending checkpoint")
		}
		delete(pending, record.TrailID)
		terminal[record.TrailID] = true
		if record.Disposition == validatorpkg.AttemptDispositionComplete {
			complete++
		} else {
			failed++
			if record.Disposition == validatorpkg.AttemptDispositionValidatorError {
				recovered++
			}
		}
	}
	if len(pending) != 0 || records.LastSequence != records.Records[len(records.Records)-1].Sequence || !stringUint64MapsEqual(records.DispositionCounts, dispositions) || records.PendingRecoveredCount != recovered || summary.RecordCount != uint64(len(records.Records)) || summary.CheckpointCount != checkpoints || summary.CompleteCount != complete || summary.FailedCount != failed || summary.PendingCount != 0 || summary.PendingRecoveredCount != recovered {
		return errors.New("collected attempt record counts or recovery lifecycle differ")
	}
	return nil
}

func stringUint64MapsEqual(left, right map[string]uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func finalSemanticCollectedInputsHash(value *FinalSemanticCollectedInputs) (string, error) {
	copy := *value
	copy.EvidenceHash = ""
	return canonicalHashHex(copy)
}
