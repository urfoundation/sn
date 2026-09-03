package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

const (
	releaseCandidateCampaignName        = "release-candidate"
	releaseCampaignGateSchema           = "urnetwork-sim-release-campaign-gate-v1"
	scenarioLifecycleHandoffSchema      = "urnetwork-sim-lifecycle-handoff-v1"
	scenarioLifecycleHandoffFilename    = "fleet-lifecycle-handoff.json"
	scenarioCampaignStartFilename       = "campaign-start.evidence.json"
	scenarioCampaignAttemptSchema       = "urnetwork-sim-scenario-attempt-v2"
	scenarioCampaignAttemptEvidenceKind = "scenario-attempt"
)

var scenarioProcessSessionID, scenarioProcessSessionIDErr = newScenarioProcessSessionID()

func newScenarioProcessSessionID() (string, error) {
	value := make([]byte, sha256.Size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create scenario process session identity: %w", err)
	}
	return "0x" + hex.EncodeToString(value), nil
}

// ScenarioLifecycleHandoff identifies the exact immutable lifecycle bytes
// captured before the release completion was signed. Production authenticates
// this copy before it is allowed to perform any transition mutation.
type ScenarioLifecycleHandoff struct {
	Schema       string `json:"schema"`
	ReleaseRunID string `json:"release_run_id"`
	Stage        string `json:"stage"`
	File         string `json:"file"`
	ContentHash  string `json:"content_hash"`
	SizeBytes    uint64 `json:"byte_length"`
}

// ReleaseCampaignGate is the authenticated live-topology interval which must
// complete before the simulator may schedule production cadence.
type ReleaseCampaignGate struct {
	Schema              string                   `json:"schema"`
	RunID               string                   `json:"run_id"`
	ResultHash          string                   `json:"result_hash"`
	CompleteContentHash string                   `json:"complete_content_hash"`
	StartEpoch          uint64                   `json:"start_epoch"`
	EndEpoch            uint64                   `json:"end_epoch"`
	LifecycleHandoff    ScenarioLifecycleHandoff `json:"lifecycle_handoff"`
}

type scenarioCompletePayload struct {
	ResultHash           string                    `json:"result_hash"`
	Files                map[string]string         `json:"files"`
	BundlePayloadHash    string                    `json:"bundle_payload_hash,omitempty"`
	EvidenceManifestHash string                    `json:"evidence_manifest_hash,omitempty"`
	LifecycleHandoff     *ScenarioLifecycleHandoff `json:"lifecycle_handoff,omitempty"`
	PriorRelease         *ReleaseCampaignGate      `json:"prior_release,omitempty"`
}

// scenarioCampaignAcceptanceBoundary is the owner-authenticated, one-session
// start marker selected after preparation. ObservationLogContentHash binds the
// exact append-only observation prefix through LastObservationHash, while
// Faults is the authoritative fault ledger. A later invocation may inspect
// this state for diagnostics but may never resume its acceptance campaign.
type scenarioCampaignAcceptanceBoundary struct {
	ProcessSessionID             string                   `json:"process_session_id"`
	AcceptanceStartedAt          string                   `json:"acceptance_started_at"`
	ScenarioDefinitionHash       string                   `json:"scenario_definition_hash"`
	AdversarialMatrixHash        string                   `json:"adversarial_matrix_hash"`
	AdversaryStartedAt           string                   `json:"adversary_started_at"`
	AdversaryHappyPathStartedAt  string                   `json:"adversary_happy_path_started_at"`
	CampaignStartHead            ChainHead                `json:"campaign_start_finalized_head"`
	CampaignStartEpoch           uint64                   `json:"campaign_start_epoch"`
	CampaignStartObservationHash string                   `json:"campaign_start_observation_hash"`
	AcceptanceWindow             ScenarioAcceptanceWindow `json:"acceptance_window"`
	ObservationLogContentHash    string                   `json:"observation_log_content_hash"`
	ObservationLogBytes          uint64                   `json:"observation_log_bytes"`
	LastObservationHead          ChainHead                `json:"last_observation_finalized_head"`
	LastObservationEpoch         uint64                   `json:"last_observation_epoch"`
	LastObservationHash          string                   `json:"last_observation_hash"`
	Faults                       []ScenarioFaultRecord    `json:"faults,omitempty"`
}

// scenarioCampaignAttemptPayload is an owner-signed, pre-mutation campaign
// lease. Its stable RunID and predecessor survive cancellation before the
// acceptance marker; after that marker, any exit or new session permanently
// invalidates this deployment attempt instead of merging adversary counters.
type scenarioCampaignAttemptPayload struct {
	Schema                  string                              `json:"schema"`
	Phase                   string                              `json:"phase"`
	RunID                   string                              `json:"run_id"`
	StartedAt               string                              `json:"started_at"`
	ConfigHash              string                              `json:"config_hash"`
	PolicyHash              string                              `json:"policy_hash"`
	PlanHash                string                              `json:"plan_hash"`
	PriorRelease            *ReleaseCampaignGate                `json:"prior_release,omitempty"`
	HandoffAuthenticated    bool                                `json:"handoff_authenticated,omitempty"`
	PreparationComplete     bool                                `json:"preparation_complete,omitempty"`
	AcceptanceBoundary      *scenarioCampaignAcceptanceBoundary `json:"acceptance_boundary,omitempty"`
	AcceptanceInvalidatedAt string                              `json:"acceptance_invalidated_at,omitempty"`
	AcceptanceInvalidation  string                              `json:"acceptance_invalidation,omitempty"`
}

type scenarioCampaignAttempt struct {
	payload  scenarioCampaignAttemptPayload
	cfg      *ResolvedConfig
	stateDir string
	roles    *RoleSecrets
}

func scenarioCampaignAttemptPath(stateDir, phase string) string {
	return filepath.Join(stateDir, "campaign-attempts", phase+".evidence.json")
}

func scenarioCampaignAttemptLockPath(stateDir, phase string) string {
	return filepath.Join(stateDir, "campaign-attempts", phase+".lock")
}

func withScenarioCampaignAttemptLock(stateDir, phase string, action func() error) error {
	if phase != "release-1.0" && phase != "production-soak" {
		return fmt.Errorf("scenario campaign attempt phase %q is unsupported", phase)
	}
	path := scenarioCampaignAttemptLockPath(stateDir, phase)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock scenario campaign attempt: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	return action()
}

func releaseCampaignGatesEqual(a, b *ReleaseCampaignGate) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cloneScenarioFaultRecords(records []ScenarioFaultRecord) []ScenarioFaultRecord {
	cloned := make([]ScenarioFaultRecord, len(records))
	for index := range records {
		cloned[index] = records[index]
		cloned[index].Targets = append([]string(nil), records[index].Targets...)
		cloned[index].Impacts = append([]string(nil), records[index].Impacts...)
		cloned[index].FleetIndices = append([]int(nil), records[index].FleetIndices...)
		cloned[index].Processes = append([]FaultProcessEvidence(nil), records[index].Processes...)
		cloned[index].RestoredProcesses = append([]FaultProcessEvidence(nil), records[index].RestoredProcesses...)
	}
	return cloned
}

func scenarioAcceptanceWindowsEqual(a, b *ScenarioAcceptanceWindow) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func scenarioObservationIdentity(observation *ScenarioObservation) (ChainHead, uint64, string, error) {
	if observation == nil || observation.Status == nil || observation.Status.Contracts == nil {
		return ChainHead{}, 0, "", errors.New("scenario observation has no finalized contract identity")
	}
	head := observation.Status.Contracts.FinalizedHead
	if head.Number == 0 || !validCanonicalHashHex(head.Hash) || !validCanonicalHashHex(observation.ObservationHash) {
		return ChainHead{}, 0, "", errors.New("scenario observation has a noncanonical finalized head or content hash")
	}
	copyObservation := *observation
	copyObservation.ObservationHash = ""
	wantHash, err := canonicalHashHex(&copyObservation)
	if err != nil || !strings.EqualFold(wantHash, observation.ObservationHash) {
		return ChainHead{}, 0, "", stateMismatchError(err, "scenario observation hash differs from its canonical content")
	}
	return head, observation.Status.Contracts.CurrentEpoch, observation.ObservationHash, nil
}

func validateScenarioCampaignAcceptanceBoundary(cfg *ResolvedConfig, phase string, boundary *scenarioCampaignAcceptanceBoundary) error {
	if boundary == nil {
		return nil
	}
	window := &boundary.AcceptanceWindow
	acceptanceStarted, startErr := time.Parse(time.RFC3339Nano, boundary.AcceptanceStartedAt)
	adversaryStarted, adversaryStartErr := time.Parse(time.RFC3339Nano, boundary.AdversaryStartedAt)
	happyPathStarted, happyPathStartErr := time.Parse(time.RFC3339Nano, boundary.AdversaryHappyPathStartedAt)
	if cfg == nil || cfg.Config == nil || cfg.Policy == nil || !validCanonicalHashHex(boundary.ProcessSessionID) || !validCanonicalHashHex(boundary.ScenarioDefinitionHash) || !validCanonicalHashHex(boundary.AdversarialMatrixHash) || startErr != nil || acceptanceStarted.Location() != time.UTC || adversaryStartErr != nil || happyPathStartErr != nil || adversaryStarted.After(happyPathStarted) || happyPathStarted.After(acceptanceStarted) || window.Schema != "urnetwork-sim-acceptance-window-v1" || boundary.CampaignStartHead.Number == 0 || !validCanonicalHashHex(boundary.CampaignStartHead.Hash) || !validCanonicalHashHex(boundary.CampaignStartObservationHash) || boundary.ObservationLogBytes == 0 || !validSHA256ContentHash(boundary.ObservationLogContentHash) || boundary.LastObservationHead.Number == 0 || !validCanonicalHashHex(boundary.LastObservationHead.Hash) || !validCanonicalHashHex(boundary.LastObservationHash) {
		return errors.New("scenario campaign attempt acceptance boundary is incomplete or noncanonical")
	}
	if window.BaselineHead.Number == 0 || !validCanonicalHashHex(window.BaselineHead.Hash) || !validCanonicalHashHex(window.BaselineObservationHash) || boundary.CampaignStartHead.Number > window.BaselineHead.Number || boundary.CampaignStartEpoch > window.BaselineEpoch || boundary.LastObservationHead.Number < window.BaselineHead.Number || boundary.LastObservationEpoch < window.BaselineEpoch {
		return errors.New("scenario campaign attempt observation boundary is not monotonic")
	}
	wantEpochs := uint64(cfg.Config.Scenarios.ShortEpochs)
	wantBlocks := cfg.Policy.Settlement.EpochBlocks
	wantFinalize := cfg.Policy.Settlement.FinalizeOffsetBlocks
	if phase == "production-soak" {
		wantEpochs = uint64(cfg.Config.Scenarios.ProductionEpochs)
		wantBlocks = cfg.Policy.ProductionCadence.EpochBlocks
		wantFinalize = cfg.Policy.ProductionCadence.FinalizeOffsetBlocks
		if window.PolicyEffectiveEpoch == 0 || window.PolicyEffectiveEpoch > window.BaselineEpoch {
			return errors.New("production campaign attempt does not bind an active production policy")
		}
	}
	firstEpoch, firstOK := checkedAdd(window.BaselineEpoch, 1)
	span, spanOK := checkedMul(wantEpochs, wantBlocks)
	endBlock, endOK := checkedAdd(window.StartBlock, span)
	terminalBlock, terminalOK := checkedAdd(endBlock, wantFinalize)
	if !firstOK || !spanOK || !endOK || !terminalOK || window.FirstEpoch != firstEpoch || window.EpochCount != wantEpochs || window.EpochBlocks != wantBlocks || window.FinalizeOffsetBlocks != wantFinalize || window.StartBlock <= window.BaselineHead.Number || window.StartBlock-window.BaselineHead.Number > wantBlocks || window.EndBlock != endBlock || window.TerminalBlock != terminalBlock {
		return errors.New("scenario campaign attempt acceptance geometry is not exact")
	}
	definition, err := scenarioDefinitionFor(cfg, phase)
	if err != nil {
		return fmt.Errorf("scenario campaign attempt definition: %w", err)
	}
	if err := validateScenarioAttemptFaultRecords(definition, window, boundary.Faults); err != nil {
		return err
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil || !strings.EqualFold(boundary.ScenarioDefinitionHash, definitionHash) || !strings.EqualFold(boundary.AdversarialMatrixHash, definition.AdversarialMatrixHash) {
		return stateMismatchError(err, "scenario campaign attempt acceptance definition or adversarial matrix changed")
	}
	return nil
}

func scenarioFaultRecordMatchesSchedule(record, expected ScenarioFaultRecord) bool {
	return record.ID == expected.ID && record.Kind == expected.Kind && slices.Equal(record.Targets, expected.Targets) && slices.Equal(record.Impacts, expected.Impacts) && record.ValidatorID == expected.ValidatorID && record.FleetIndex == expected.FleetIndex && slices.Equal(record.FleetIndices, expected.FleetIndices) && record.PreAcceptance == expected.PreAcceptance && record.PostAcceptanceEvidenceTail == expected.PostAcceptanceEvidenceTail && record.ActivationCondition == expected.ActivationCondition && record.RestoreCondition == expected.RestoreCondition && record.MinimumDurationBlocks == expected.MinimumDurationBlocks && record.TriggerBlock == expected.TriggerBlock && record.RestoreBlock == expected.RestoreBlock
}

func validateScenarioAttemptFaultRecords(definition scenarioDefinition, window *ScenarioAcceptanceWindow, records []ScenarioFaultRecord) error {
	if window == nil {
		return errors.New("scenario campaign fault ledger has no acceptance window")
	}
	expected, err := initializeFaultRecords(window.StartBlock, definition.Faults)
	if err != nil {
		return err
	}
	if len(records) != len(expected) {
		return fmt.Errorf("scenario campaign fault ledger has %d records, want %d", len(records), len(expected))
	}
	seen := make(map[string]bool, len(records))
	for index := range records {
		record := records[index]
		if seen[record.ID] || !scenarioFaultRecordMatchesSchedule(record, expected[index]) {
			return fmt.Errorf("scenario campaign fault ledger record %q differs from its exact schedule", record.ID)
		}
		seen[record.ID] = true
		if record.PreAcceptance {
			if record.ArmedBlock == 0 || record.ArmedBlock >= window.StartBlock || !validCanonicalHashHex(record.ArmedBlockHash) {
				return fmt.Errorf("scenario campaign pre-acceptance fault %q has no exact armed boundary", record.ID)
			}
		} else if record.ArmedBlock != 0 || record.ArmedBlockHash != "" {
			return fmt.Errorf("scenario campaign fault %q has foreign pre-acceptance state", record.ID)
		}
		switch record.Status {
		case "pending":
			if record.AppliedBlock != 0 || record.AppliedBlockHash != "" || record.RestoredBlock != 0 || record.RestoredBlockHash != "" || len(record.Processes) != 0 || len(record.RestoredProcesses) != 0 || record.Error != "" {
				return fmt.Errorf("scenario campaign pending fault %q contains transition evidence", record.ID)
			}
		case "active":
			if record.AppliedBlock < record.TriggerBlock || !validCanonicalHashHex(record.AppliedBlockHash) || record.RestoredBlock != 0 || record.RestoredBlockHash != "" || len(record.Processes) != len(record.Targets) || len(record.RestoredProcesses) != 0 || record.Error != "" {
				return fmt.Errorf("scenario campaign active fault %q has malformed transition evidence", record.ID)
			}
		case "restored":
			if record.AppliedBlock < record.TriggerBlock || !validCanonicalHashHex(record.AppliedBlockHash) || record.RestoredBlock < record.AppliedBlock || !validCanonicalHashHex(record.RestoredBlockHash) || len(record.Processes) != len(record.Targets) || record.Error != "" {
				return fmt.Errorf("scenario campaign restored fault %q has malformed transition evidence", record.ID)
			}
		case "failed":
			return fmt.Errorf("scenario campaign fault %q is terminally failed", record.ID)
		default:
			return fmt.Errorf("scenario campaign fault %q has unknown status %q", record.ID, record.Status)
		}
	}
	return nil
}

func validateScenarioLifecycleHandoffBinding(cfg *ResolvedConfig, binding ScenarioLifecycleHandoff, data []byte) error {
	if cfg == nil || cfg.Config == nil || binding.Schema != scenarioLifecycleHandoffSchema || binding.ReleaseRunID == "" || binding.Stage != fleetLifecycleStageReleaseHandoff || binding.File != scenarioLifecycleHandoffFilename || !validSHA256ContentHash(binding.ContentHash) || binding.SizeBytes == 0 || uint64(len(data)) != binding.SizeBytes || !strings.EqualFold(bytesSHA256(data), binding.ContentHash) {
		return errors.New("release lifecycle handoff binding is incomplete or differs from its bytes")
	}
	var lifecycle FleetLifecycleEvidence
	if err := decodeStrictJSONBytes(data, &lifecycle); err != nil {
		return fmt.Errorf("decode release lifecycle handoff: %w", err)
	}
	if lifecycle.Schema != fleetLifecycleEvidenceSchema || lifecycle.DeploymentID != cfg.Config.Deployment.DeploymentID || lifecycle.RunID != binding.ReleaseRunID || lifecycle.ProductionRunID != "" || lifecycle.Stage != binding.Stage || !validCanonicalHashHex(lifecycle.PlanHash) {
		return errors.New("release lifecycle handoff bytes have the wrong deployment, run, plan, or stage")
	}
	return nil
}

func captureScenarioLifecycleHandoff(cfg *ResolvedConfig, stateDir, runDir, runID string) (*ScenarioLifecycleHandoff, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, "public", "fleet-lifecycle.json"))
	if err != nil {
		return nil, fmt.Errorf("read release lifecycle handoff: %w", err)
	}
	binding := &ScenarioLifecycleHandoff{
		Schema: scenarioLifecycleHandoffSchema, ReleaseRunID: runID, Stage: fleetLifecycleStageReleaseHandoff,
		File: scenarioLifecycleHandoffFilename, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data)),
	}
	if err := validateScenarioLifecycleHandoffBinding(cfg, *binding, data); err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(runDir, binding.File), data, 0o644); err != nil {
		return nil, fmt.Errorf("persist release lifecycle handoff: %w", err)
	}
	return binding, nil
}

func validateScenarioLifecycleHandoffFile(cfg *ResolvedConfig, runDir string, binding ScenarioLifecycleHandoff) ([]byte, error) {
	if binding.File != scenarioLifecycleHandoffFilename {
		return nil, errors.New("release lifecycle handoff file is noncanonical")
	}
	data, err := os.ReadFile(filepath.Join(runDir, binding.File))
	if err != nil {
		return nil, fmt.Errorf("read immutable release lifecycle handoff: %w", err)
	}
	if err := validateScenarioLifecycleHandoffBinding(cfg, binding, data); err != nil {
		return nil, err
	}
	return data, nil
}

func validateReleaseCampaignGateShape(cfg *ResolvedConfig, gate *ReleaseCampaignGate) error {
	if cfg == nil || cfg.Config == nil || gate == nil || gate.Schema != releaseCampaignGateSchema || gate.RunID == "" || !validCanonicalHashHex(gate.ResultHash) || !validSHA256ContentHash(gate.CompleteContentHash) || gate.StartEpoch == 0 || gate.EndEpoch < gate.StartEpoch || gate.LifecycleHandoff.Schema != scenarioLifecycleHandoffSchema || gate.LifecycleHandoff.ReleaseRunID != gate.RunID || gate.LifecycleHandoff.Stage != fleetLifecycleStageReleaseHandoff || gate.LifecycleHandoff.File != scenarioLifecycleHandoffFilename || !validSHA256ContentHash(gate.LifecycleHandoff.ContentHash) || gate.LifecycleHandoff.SizeBytes == 0 {
		return errors.New("release campaign gate is incomplete or noncanonical")
	}
	return nil
}

func validateScenarioCampaignAttemptPayload(cfg *ResolvedConfig, planHash, phase string, payload *scenarioCampaignAttemptPayload) error {
	if cfg == nil || cfg.Config == nil || payload == nil || payload.Schema != scenarioCampaignAttemptSchema || payload.Phase != phase || payload.RunID == "" || !strings.EqualFold(payload.ConfigHash, cfg.ConfigHash) || !strings.EqualFold(payload.PolicyHash, cfg.PolicyHash) || !strings.EqualFold(payload.PlanHash, planHash) || !validCanonicalHashHex(payload.PlanHash) {
		return errors.New("scenario campaign attempt differs from the approved phase, configuration, policy, or plan")
	}
	started, err := time.Parse(time.RFC3339Nano, payload.StartedAt)
	if err != nil || payload.RunID != fmt.Sprintf("%s-%s", started.UTC().Format("20060102T150405.000000000Z"), phase) {
		return errors.New("scenario campaign attempt has a noncanonical stable run identity")
	}
	if payload.AcceptanceBoundary != nil {
		if !payload.PreparationComplete {
			return errors.New("scenario campaign attempt acceptance boundary precedes completed preparation")
		}
		if err := validateScenarioCampaignAcceptanceBoundary(cfg, phase, payload.AcceptanceBoundary); err != nil {
			return err
		}
		if (payload.AcceptanceInvalidatedAt == "") != (payload.AcceptanceInvalidation == "") {
			return errors.New("scenario campaign attempt has an incomplete acceptance invalidation")
		}
		if payload.AcceptanceInvalidatedAt != "" {
			invalidated, err := time.Parse(time.RFC3339Nano, payload.AcceptanceInvalidatedAt)
			started, startErr := time.Parse(time.RFC3339Nano, payload.AcceptanceBoundary.AcceptanceStartedAt)
			if err != nil || startErr != nil || invalidated.Before(started) || !slices.Contains([]string{"execution-exited-before-completion", "process-session-changed", "attempt-reentered-after-acceptance"}, payload.AcceptanceInvalidation) {
				return errors.New("scenario campaign attempt has a malformed acceptance invalidation")
			}
		}
	} else if payload.AcceptanceInvalidatedAt != "" || payload.AcceptanceInvalidation != "" {
		return errors.New("scenario campaign attempt invalidation has no signed acceptance boundary")
	}
	if phase == "release-1.0" {
		if payload.PriorRelease != nil || payload.HandoffAuthenticated {
			return errors.New("release campaign attempt unexpectedly has a predecessor")
		}
		return nil
	}
	if err := validateReleaseCampaignGateShape(cfg, payload.PriorRelease); err != nil {
		return fmt.Errorf("production campaign attempt predecessor: %w", err)
	}
	return nil
}

func readScenarioCampaignAttempt(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash, phase string) (*scenarioCampaignAttempt, error) {
	if roles == nil {
		return nil, errors.New("scenario campaign attempt has no owner roles")
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return nil, errors.New("scenario campaign attempt has no testnet owner")
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, err
	}
	var envelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(scenarioCampaignAttemptPath(stateDir, phase), &envelope); err != nil {
		return nil, err
	}
	if err := verifyEvidence(&envelope, &key.PublicKey); err != nil {
		return nil, fmt.Errorf("scenario campaign attempt signature: %w", err)
	}
	if envelope.Kind != scenarioCampaignAttemptEvidenceKind || envelope.DeploymentID != cfg.Config.Deployment.DeploymentID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || !strings.EqualFold(envelope.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return nil, errors.New("scenario campaign attempt envelope has the wrong deployment identity")
	}
	var payload scenarioCampaignAttemptPayload
	if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
		return nil, fmt.Errorf("scenario campaign attempt payload: %w", err)
	}
	if envelope.RunID != payload.RunID {
		return nil, errors.New("scenario campaign attempt envelope run differs from its payload")
	}
	if err := validateScenarioCampaignAttemptPayload(cfg, planHash, phase, &payload); err != nil {
		return nil, err
	}
	return &scenarioCampaignAttempt{payload: payload, cfg: cfg, stateDir: stateDir, roles: roles}, nil
}

func writeScenarioCampaignAttempt(attempt *scenarioCampaignAttempt) error {
	if attempt == nil || attempt.cfg == nil || attempt.roles == nil {
		return errors.New("scenario campaign attempt writer is incomplete")
	}
	owner, ok := attempt.roles.EVM["testnet-owner"]
	if !ok {
		return errors.New("scenario campaign attempt writer has no testnet owner")
	}
	envelope, err := signEvidence(attempt.cfg, scenarioCampaignAttemptEvidenceKind, attempt.payload.RunID, attempt.payload, owner)
	if err != nil {
		return err
	}
	return writePublicJSON(scenarioCampaignAttemptPath(attempt.stateDir, attempt.payload.Phase), envelope)
}

func loadOrCreateScenarioCampaignAttempt(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, planHash, phase string, prior *ReleaseCampaignGate, now time.Time) (*scenarioCampaignAttempt, error) {
	var result *scenarioCampaignAttempt
	err := withScenarioCampaignAttemptLock(stateDir, phase, func() error {
		attempt, err := readScenarioCampaignAttempt(cfg, stateDir, roles, planHash, phase)
		if err == nil {
			if prior != nil && !releaseCampaignGatesEqual(attempt.payload.PriorRelease, prior) {
				return errors.New("scenario campaign attempt is already bound to a different release predecessor")
			}
			result = attempt
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		started := now.UTC()
		payload := scenarioCampaignAttemptPayload{
			Schema: scenarioCampaignAttemptSchema, Phase: phase,
			RunID: fmt.Sprintf("%s-%s", started.Format("20060102T150405.000000000Z"), phase), StartedAt: started.Format(time.RFC3339Nano),
			ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, PlanHash: planHash, PriorRelease: prior,
		}
		if err := validateScenarioCampaignAttemptPayload(cfg, planHash, phase, &payload); err != nil {
			return err
		}
		attempt = &scenarioCampaignAttempt{payload: payload, cfg: cfg, stateDir: stateDir, roles: roles}
		if err := writeScenarioCampaignAttempt(attempt); err != nil {
			return err
		}
		result = attempt
		return nil
	})
	return result, err
}

func (attempt *scenarioCampaignAttempt) updateProgress(handoffAuthenticated, preparationComplete bool) error {
	if attempt == nil {
		return errors.New("scenario campaign attempt progress is unavailable")
	}
	return withScenarioCampaignAttemptLock(attempt.stateDir, attempt.payload.Phase, func() error {
		current, err := readScenarioCampaignAttempt(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, attempt.payload.Phase)
		if err != nil {
			return err
		}
		if current.payload.RunID != attempt.payload.RunID || !releaseCampaignGatesEqual(current.payload.PriorRelease, attempt.payload.PriorRelease) {
			return errors.New("scenario campaign attempt identity changed while updating progress")
		}
		if current.payload.HandoffAuthenticated && !handoffAuthenticated || current.payload.PreparationComplete && !preparationComplete {
			return errors.New("scenario campaign attempt progress cannot move backward")
		}
		current.payload.HandoffAuthenticated = handoffAuthenticated
		current.payload.PreparationComplete = preparationComplete
		if err := writeScenarioCampaignAttempt(current); err != nil {
			return err
		}
		attempt.payload = current.payload
		return nil
	})
}

func scenarioCampaignRunDir(attempt *scenarioCampaignAttempt, runDir string) error {
	if attempt == nil || filepath.Clean(runDir) != filepath.Clean(filepath.Join(attempt.stateDir, "runs", attempt.payload.RunID)) {
		return errors.New("scenario campaign attempt run directory differs from its stable run identity")
	}
	return nil
}

func decodeScenarioObservationLog(data []byte) ([]*ScenarioObservation, error) {
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, errors.New("scenario observation log is empty or does not end at a durable record boundary")
	}
	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	history := make([]*ScenarioObservation, len(lines))
	var previousHead ChainHead
	var previousEpoch uint64
	for index, line := range lines {
		if len(line) == 0 {
			return nil, fmt.Errorf("scenario observation log record %d is empty", index)
		}
		var observation ScenarioObservation
		if err := decodeStrictJSONBytes(line, &observation); err != nil {
			return nil, fmt.Errorf("scenario observation log record %d: %w", index, err)
		}
		head, epoch, _, err := scenarioObservationIdentity(&observation)
		if err != nil {
			return nil, fmt.Errorf("scenario observation log record %d: %w", index, err)
		}
		if index > 0 && (head.Number < previousHead.Number || epoch < previousEpoch || head.Number == previousHead.Number && !strings.EqualFold(head.Hash, previousHead.Hash)) {
			return nil, fmt.Errorf("scenario observation log record %d regresses or substitutes finalized history", index)
		}
		previousHead, previousEpoch = head, epoch
		history[index] = &observation
	}
	return history, nil
}

func validateScenarioAttemptObservationHistory(boundary *scenarioCampaignAcceptanceBoundary, data []byte) ([]*ScenarioObservation, *ScenarioObservation, *ScenarioObservation, *ScenarioObservation, error) {
	if boundary == nil || uint64(len(data)) != boundary.ObservationLogBytes || !strings.EqualFold(bytesSHA256(data), boundary.ObservationLogContentHash) {
		return nil, nil, nil, nil, errors.New("scenario observation log differs from its owner-authenticated prefix")
	}
	history, err := decodeScenarioObservationLog(data)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if len(history) == 0 {
		return nil, nil, nil, nil, errors.New("scenario observation log has no authenticated campaign boundary")
	}
	campaignStart := history[0]
	startHead, startEpoch, startHash, err := scenarioObservationIdentity(campaignStart)
	if err != nil || startHead != boundary.CampaignStartHead || startEpoch != boundary.CampaignStartEpoch || !strings.EqualFold(startHash, boundary.CampaignStartObservationHash) {
		return nil, nil, nil, nil, stateMismatchError(err, "scenario observation log campaign start differs from its signed attempt")
	}
	var baseline *ScenarioObservation
	for _, observation := range history {
		head, epoch, hash, identityErr := scenarioObservationIdentity(observation)
		if identityErr != nil {
			return nil, nil, nil, nil, identityErr
		}
		if head == boundary.AcceptanceWindow.BaselineHead && epoch == boundary.AcceptanceWindow.BaselineEpoch && strings.EqualFold(hash, boundary.AcceptanceWindow.BaselineObservationHash) {
			if baseline != nil {
				return nil, nil, nil, nil, errors.New("scenario observation log duplicates its signed acceptance baseline")
			}
			baseline = observation
		}
	}
	if baseline == nil {
		return nil, nil, nil, nil, errors.New("scenario observation log omits its signed acceptance baseline")
	}
	current := history[len(history)-1]
	lastHead, lastEpoch, lastHash, err := scenarioObservationIdentity(current)
	if err != nil || lastHead != boundary.LastObservationHead || lastEpoch != boundary.LastObservationEpoch || !strings.EqualFold(lastHash, boundary.LastObservationHash) {
		return nil, nil, nil, nil, stateMismatchError(err, "scenario observation log terminal record differs from its signed attempt")
	}
	return history, campaignStart, baseline, current, nil
}

// loadAuthenticatedRuntimeForensics authenticates the immutable observation
// prefix and fault ledger of a failed attempt. It must never be used to resume
// execution after an acceptance boundary has been signed.
func (attempt *scenarioCampaignAttempt) loadAuthenticatedRuntimeForensics(runDir string) ([]*ScenarioObservation, *ScenarioObservation, *ScenarioObservation, *ScenarioObservation, []ScenarioFaultRecord, error) {
	if err := scenarioCampaignRunDir(attempt, runDir); err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if attempt.payload.AcceptanceBoundary == nil {
		return nil, nil, nil, nil, nil, errors.New("scenario campaign attempt has no signed acceptance boundary")
	}
	path := filepath.Join(runDir, "observations.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("read authenticated scenario observation log: %w", err)
	}
	boundBytes := attempt.payload.AcceptanceBoundary.ObservationLogBytes
	if uint64(len(data)) < boundBytes {
		return nil, nil, nil, nil, nil, errors.New("scenario observation log is shorter than its owner-authenticated prefix")
	}
	prefix := data[:boundBytes]
	if !strings.EqualFold(bytesSHA256(prefix), attempt.payload.AcceptanceBoundary.ObservationLogContentHash) {
		return nil, nil, nil, nil, nil, errors.New("scenario observation log owner-authenticated prefix was substituted")
	}
	if uint64(len(data)) != boundBytes {
		return nil, nil, nil, nil, nil, errors.New("scenario observation log has an unauthenticated suffix after its signed boundary")
	}
	history, campaignStart, baseline, current, err := validateScenarioAttemptObservationHistory(attempt.payload.AcceptanceBoundary, prefix)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return history, campaignStart, baseline, current, cloneScenarioFaultRecords(attempt.payload.AcceptanceBoundary.Faults), nil
}

func scenarioFaultStatusRank(status string) int {
	switch status {
	case "pending":
		return 0
	case "active":
		return 1
	case "restored":
		return 2
	case "failed":
		return 3
	default:
		return -1
	}
}

func validateScenarioFaultProgress(previous, next []ScenarioFaultRecord) error {
	if len(previous) != len(next) {
		return errors.New("scenario campaign fault ledger changed length")
	}
	for index := range previous {
		before, after := previous[index], next[index]
		if !scenarioFaultRecordMatchesSchedule(after, before) || before.ArmedBlock != after.ArmedBlock || before.ArmedBlockHash != after.ArmedBlockHash || scenarioFaultStatusRank(after.Status) < scenarioFaultStatusRank(before.Status) {
			return fmt.Errorf("scenario campaign fault %q moved backward or changed identity", before.ID)
		}
		if before.AppliedBlock != 0 && (after.AppliedBlock != before.AppliedBlock || after.AppliedBlockHash != before.AppliedBlockHash || !slices.Equal(after.Processes, before.Processes)) {
			return fmt.Errorf("scenario campaign fault %q changed its applied transition", before.ID)
		}
		if before.RestoredBlock != 0 && (after.RestoredBlock != before.RestoredBlock || after.RestoredBlockHash != before.RestoredBlockHash || !slices.Equal(after.RestoredProcesses, before.RestoredProcesses)) {
			return fmt.Errorf("scenario campaign fault %q changed its restored transition", before.ID)
		}
		if before.ActivationConditionMet && (!after.ActivationConditionMet || after.ActivationConditionBlock != before.ActivationConditionBlock) || before.RestoreConditionMet && (!after.RestoreConditionMet || after.RestoreConditionBlock != before.RestoreConditionBlock) {
			return fmt.Errorf("scenario campaign fault %q changed its condition evidence", before.ID)
		}
	}
	return nil
}

func (attempt *scenarioCampaignAttempt) bindAcceptanceBoundary(runDir, processSessionID, definitionHash string, adversary *AdversaryCampaignEvidence, acceptanceStarted time.Time, campaignStart, baseline *ScenarioObservation, window *ScenarioAcceptanceWindow, faults []ScenarioFaultRecord) error {
	if err := scenarioCampaignRunDir(attempt, runDir); err != nil {
		return err
	}
	if window == nil {
		return errors.New("scenario campaign acceptance window is unavailable")
	}
	definition, err := scenarioDefinitionFor(attempt.cfg, attempt.payload.Phase)
	if err != nil {
		return err
	}
	if adversary == nil || adversary.Status != "running" || !strings.EqualFold(adversary.MatrixHash, definition.AdversarialMatrixHash) {
		return errors.New("scenario campaign acceptance has no exact running adversary marker")
	}
	startHead, startEpoch, startHash, err := scenarioObservationIdentity(campaignStart)
	if err != nil {
		return err
	}
	baselineHead, baselineEpoch, baselineHash, err := scenarioObservationIdentity(baseline)
	if err != nil {
		return err
	}
	if baselineHead != window.BaselineHead || baselineEpoch != window.BaselineEpoch || !strings.EqualFold(baselineHash, window.BaselineObservationHash) {
		return errors.New("scenario campaign acceptance window differs from its baseline observation")
	}
	data, err := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
	if err != nil {
		return err
	}
	boundary := &scenarioCampaignAcceptanceBoundary{
		ProcessSessionID: processSessionID, AcceptanceStartedAt: acceptanceStarted.UTC().Format(time.RFC3339Nano),
		ScenarioDefinitionHash: definitionHash, AdversarialMatrixHash: adversary.MatrixHash,
		AdversaryStartedAt: adversary.StartedAt, AdversaryHappyPathStartedAt: adversary.HappyPathStartedAt,
		CampaignStartHead: startHead, CampaignStartEpoch: startEpoch, CampaignStartObservationHash: startHash,
		AcceptanceWindow: *window, ObservationLogContentHash: bytesSHA256(data), ObservationLogBytes: uint64(len(data)),
		LastObservationHead: baselineHead, LastObservationEpoch: baselineEpoch, LastObservationHash: baselineHash,
		Faults: cloneScenarioFaultRecords(faults),
	}
	if err := validateScenarioCampaignAcceptanceBoundary(attempt.cfg, attempt.payload.Phase, boundary); err != nil {
		return err
	}
	if _, _, _, _, err := validateScenarioAttemptObservationHistory(boundary, data); err != nil {
		return err
	}
	return withScenarioCampaignAttemptLock(attempt.stateDir, attempt.payload.Phase, func() error {
		current, err := readScenarioCampaignAttempt(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, attempt.payload.Phase)
		if err != nil {
			return err
		}
		if current.payload.RunID != attempt.payload.RunID || !releaseCampaignGatesEqual(current.payload.PriorRelease, attempt.payload.PriorRelease) {
			return errors.New("scenario campaign attempt identity changed while binding acceptance")
		}
		if current.payload.AcceptanceBoundary != nil {
			return errors.New("scenario campaign attempt acceptance boundary is already bound")
		}
		current.payload.AcceptanceBoundary = boundary
		if err := writeScenarioCampaignAttempt(current); err != nil {
			return err
		}
		attempt.payload = current.payload
		encoded, err := os.ReadFile(scenarioCampaignAttemptPath(attempt.stateDir, attempt.payload.Phase))
		if err != nil {
			return fmt.Errorf("read signed scenario campaign start marker: %w", err)
		}
		startPath := filepath.Join(runDir, scenarioCampaignStartFilename)
		if _, err := os.Stat(startPath); err == nil {
			return errors.New("scenario campaign start marker already exists before first acceptance boundary")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := atomicWrite(startPath, encoded, 0o644); err != nil {
			return fmt.Errorf("persist signed scenario campaign start marker: %w", err)
		}
		return nil
	})
}

func (attempt *scenarioCampaignAttempt) updateAuthenticatedRuntime(runDir string, faults []ScenarioFaultRecord) error {
	if err := scenarioCampaignRunDir(attempt, runDir); err != nil {
		return err
	}
	return withScenarioCampaignAttemptLock(attempt.stateDir, attempt.payload.Phase, func() error {
		current, err := readScenarioCampaignAttempt(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, attempt.payload.Phase)
		if err != nil {
			return err
		}
		if current.payload.RunID != attempt.payload.RunID || current.payload.AcceptanceBoundary == nil || current.payload.AcceptanceInvalidation != "" || !releaseCampaignGatesEqual(current.payload.PriorRelease, attempt.payload.PriorRelease) {
			return errors.New("scenario campaign attempt boundary changed while updating runtime evidence")
		}
		data, err := os.ReadFile(filepath.Join(runDir, "observations.jsonl"))
		if err != nil {
			return err
		}
		old := current.payload.AcceptanceBoundary
		if uint64(len(data)) < old.ObservationLogBytes || !strings.EqualFold(bytesSHA256(data[:old.ObservationLogBytes]), old.ObservationLogContentHash) {
			return errors.New("scenario observation log changed before its signed append boundary")
		}
		history, err := decodeScenarioObservationLog(data)
		if err != nil || len(history) == 0 {
			return stateMismatchError(err, "scenario observation log has no valid runtime checkpoint")
		}
		lastHead, lastEpoch, lastHash, err := scenarioObservationIdentity(history[len(history)-1])
		if err != nil || lastHead.Number < old.LastObservationHead.Number || lastEpoch < old.LastObservationEpoch || lastHead.Number == old.LastObservationHead.Number && !strings.EqualFold(lastHead.Hash, old.LastObservationHead.Hash) {
			return stateMismatchError(err, "scenario observation runtime checkpoint regressed finalized history")
		}
		definition, err := scenarioDefinitionFor(attempt.cfg, attempt.payload.Phase)
		if err != nil {
			return err
		}
		if err := validateScenarioAttemptFaultRecords(definition, &old.AcceptanceWindow, faults); err != nil {
			return err
		}
		if err := validateScenarioFaultProgress(old.Faults, faults); err != nil {
			return err
		}
		updated := *old
		updated.ObservationLogContentHash = bytesSHA256(data)
		updated.ObservationLogBytes = uint64(len(data))
		updated.LastObservationHead = lastHead
		updated.LastObservationEpoch = lastEpoch
		updated.LastObservationHash = lastHash
		updated.Faults = cloneScenarioFaultRecords(faults)
		current.payload.AcceptanceBoundary = &updated
		if err := writeScenarioCampaignAttempt(current); err != nil {
			return err
		}
		attempt.payload = current.payload
		return nil
	})
}

func (attempt *scenarioCampaignAttempt) invalidateAcceptance(reason string, invalidatedAt time.Time) error {
	if attempt == nil || !slices.Contains([]string{"execution-exited-before-completion", "process-session-changed", "attempt-reentered-after-acceptance"}, reason) {
		return errors.New("scenario campaign acceptance invalidation is unavailable or unsupported")
	}
	return withScenarioCampaignAttemptLock(attempt.stateDir, attempt.payload.Phase, func() error {
		current, err := readScenarioCampaignAttempt(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PlanHash, attempt.payload.Phase)
		if err != nil {
			return err
		}
		if current.payload.RunID != attempt.payload.RunID || current.payload.AcceptanceBoundary == nil || !releaseCampaignGatesEqual(current.payload.PriorRelease, attempt.payload.PriorRelease) {
			return errors.New("scenario campaign attempt changed before acceptance invalidation")
		}
		if current.payload.AcceptanceInvalidation != "" {
			attempt.payload = current.payload
			return nil
		}
		current.payload.AcceptanceInvalidation = reason
		current.payload.AcceptanceInvalidatedAt = invalidatedAt.UTC().Format(time.RFC3339Nano)
		if err := writeScenarioCampaignAttempt(current); err != nil {
			return err
		}
		attempt.payload = current.payload
		return nil
	})
}

func (attempt *scenarioCampaignAttempt) authenticateProductionHandoff() ([]byte, error) {
	if attempt == nil || attempt.payload.Phase != "production-soak" || attempt.payload.PriorRelease == nil {
		return nil, errors.New("production campaign attempt has no release handoff")
	}
	_, immutable, err := validateExactReleaseCampaignGate(attempt.cfg, attempt.stateDir, attempt.roles, attempt.payload.PriorRelease)
	if err != nil {
		return nil, err
	}
	// Until preparation is durably committed, every retry must still start
	// from the exact release bytes. HandoffAuthenticated alone is not enough:
	// a crash can occur after that bit is signed but before Prepare begins.
	if !attempt.payload.PreparationComplete {
		live, err := os.ReadFile(filepath.Join(attempt.stateDir, "public", "fleet-lifecycle.json"))
		if err != nil {
			return nil, fmt.Errorf("read live release lifecycle handoff: %w", err)
		}
		if !bytes.Equal(live, immutable) {
			return nil, errors.New("live fleet lifecycle differs from the exact signed release handoff before production preparation")
		}
	}
	if !attempt.payload.HandoffAuthenticated {
		if err := attempt.updateProgress(true, attempt.payload.PreparationComplete); err != nil {
			return nil, fmt.Errorf("commit authenticated production handoff: %w", err)
		}
	}
	return immutable, nil
}

func applyScenarioAttemptBinding(result *ScenarioResult, attempt *scenarioCampaignAttempt) {
	if result == nil || attempt == nil {
		return
	}
	if attempt.payload.Phase == "production-soak" && attempt.payload.PriorRelease != nil {
		copyGate := *attempt.payload.PriorRelease
		result.PriorRelease = &copyGate
	}
}

func decodeStrictJSONBytes(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON has a trailing value")
		}
		return err
	}
	return nil
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if !strings.EqualFold(value, b[key]) {
			return false
		}
	}
	return true
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// validateScenarioAcceptanceResult independently reconstructs the complete
// epoch and terminal-finalization boundaries authenticated by a campaign
// result. It rejects partial baseline epochs and faults outside the window.
func validateScenarioAcceptanceResult(cfg *ResolvedConfig, definition scenarioDefinition, result *ScenarioResult) error {
	if cfg == nil || cfg.Policy == nil || result == nil || result.AcceptanceWindow == nil {
		return errors.New("release campaign has no complete-epoch acceptance window")
	}
	window := result.AcceptanceWindow
	wantEpochs := uint64(cfg.Config.Scenarios.ShortEpochs)
	wantBlocks := cfg.Policy.Settlement.EpochBlocks
	wantFinalize := cfg.Policy.Settlement.FinalizeOffsetBlocks
	if definition.Name == "production-soak" {
		wantEpochs = uint64(cfg.Config.Scenarios.ProductionEpochs)
		wantBlocks = cfg.Policy.ProductionCadence.EpochBlocks
		wantFinalize = cfg.Policy.ProductionCadence.FinalizeOffsetBlocks
		if window.PolicyEffectiveEpoch == 0 || window.PolicyEffectiveEpoch > window.BaselineEpoch {
			return errors.New("production acceptance window does not bind an active production policy")
		}
	}
	if window.Schema != "urnetwork-sim-acceptance-window-v1" || window.EpochCount != wantEpochs || definition.GoalEpochs != wantEpochs || window.EpochBlocks != wantBlocks || window.FinalizeOffsetBlocks != wantFinalize {
		return fmt.Errorf("release acceptance geometry=%s/%d/%d/%d want epochs/blocks/finalize=%d/%d/%d", window.Schema, window.EpochCount, window.EpochBlocks, window.FinalizeOffsetBlocks, wantEpochs, wantBlocks, wantFinalize)
	}
	firstEpoch, ok := checkedAdd(window.BaselineEpoch, 1)
	if !ok || window.FirstEpoch != firstEpoch || window.BaselineEpoch != result.StartEpoch || window.BaselineHead != result.StartHead || window.BaselineObservationHash == "" {
		return errors.New("release acceptance baseline does not match the result start")
	}
	if _, err := decodeHex32("release acceptance baseline observation hash", window.BaselineObservationHash); err != nil {
		return err
	}
	if window.StartBlock <= window.BaselineHead.Number || window.StartBlock-window.BaselineHead.Number > window.EpochBlocks {
		return errors.New("release acceptance start is not the next complete epoch boundary")
	}
	span, ok := checkedMul(window.EpochCount, window.EpochBlocks)
	if !ok {
		return errors.New("release acceptance span overflows uint64")
	}
	endBlock, ok := checkedAdd(window.StartBlock, span)
	if !ok || window.EndBlock != endBlock {
		return errors.New("release acceptance end block is not canonical")
	}
	terminalBlock, ok := checkedAdd(window.EndBlock, window.FinalizeOffsetBlocks)
	if !ok || window.TerminalBlock != terminalBlock || result.EndHead.Number < terminalBlock {
		return errors.New("release acceptance terminal finalization is incomplete")
	}
	endEpoch, ok := checkedAdd(window.FirstEpoch, window.EpochCount)
	if !ok || result.EndEpoch < endEpoch {
		return fmt.Errorf("release campaign end epoch %d precedes accepted boundary %d", result.EndEpoch, endEpoch)
	}
	if result.CampaignStartHead.Number == 0 || result.CampaignStartHead.Number > result.StartHead.Number || result.CampaignStartEpoch > result.StartEpoch {
		return errors.New("release campaign preparation interval is invalid")
	}
	if _, err := decodeHex32("release campaign initial finalized hash", result.CampaignStartHead.Hash); err != nil {
		return err
	}
	tailCount := 0
	allowedTail := map[string]bool{"fleet-lifecycle-target-prune": true, "fleet-lifecycle-companion-prune": true}
	for _, fault := range result.Faults {
		if fault.PostAcceptanceEvidenceTail {
			tailCount++
			if definition.Name != "release-1.0" || !allowedTail[fault.ID] || fault.TriggerBlock < window.StartBlock || fault.AppliedBlock < window.StartBlock || fault.AppliedBlock > window.EndBlock || fault.RestoreBlock <= window.EndBlock || fault.RestoredBlock == 0 || fault.RestoredBlock > fault.RestoreBlock || !fault.RestoreConditionMet {
				return fmt.Errorf("release fault %s is not an exact bounded post-acceptance lifecycle tail", fault.ID)
			}
			continue
		}
		if fault.TriggerBlock < window.StartBlock || fault.RestoreBlock > window.EndBlock || fault.AppliedBlock < window.StartBlock || fault.RestoredBlock > window.EndBlock {
			return fmt.Errorf("release fault %s is outside the complete-epoch acceptance window", fault.ID)
		}
	}
	if tailCount != 0 && tailCount != 2 {
		return fmt.Errorf("release campaign has %d post-acceptance lifecycle faults, want exactly 2", tailCount)
	}
	return nil
}

// validateScenarioCampaignResult validates one release-phase result
// independently of its complete marker. The expected name selects the exact
// executable definition, epoch span, faults, and adversarial evidence.
func validateScenarioCampaignResult(cfg *ResolvedConfig, result *ScenarioResult, name string) error {
	if cfg == nil || cfg.Config == nil || result == nil || strings.TrimSpace(name) == "" {
		return errors.New("release campaign result context is incomplete")
	}
	definition, err := scenarioDefinitionFor(cfg, name)
	if err != nil {
		return err
	}
	definitionHash, err := scenarioDefinitionHash(definition)
	if err != nil {
		return err
	}
	if result.Schema != "urnetwork-sim-scenario-result-v1" || result.Release != "1.0" || result.Name != name || result.RunID == "" || result.DeploymentID != cfg.Config.Deployment.DeploymentID || result.ChainID != cfg.ChainID || result.Netuid != cfg.Netuid || !strings.EqualFold(result.GenesisHash, cfg.Public.Chain.GenesisHash) || !strings.EqualFold(result.ConfigHash, cfg.ConfigHash) || !strings.EqualFold(result.PolicyHash, cfg.PolicyHash) {
		return errors.New("release campaign result identity does not match the approved deployment")
	}
	if !strings.EqualFold(result.ScenarioDefinition, definitionHash) || !strings.EqualFold(result.ScenarioMatrix, definition.MatrixHash) || !strings.EqualFold(result.AdversarialMatrix, definition.AdversarialMatrixHash) || result.Adversaries == nil {
		return errors.New("release campaign result does not bind the approved checks, faults, and adversaries")
	}
	if name == "release-1.0" {
		if result.LifecycleHandoff == nil || result.PriorRelease != nil || result.LifecycleHandoff.ReleaseRunID != result.RunID || result.LifecycleHandoff.Schema != scenarioLifecycleHandoffSchema || result.LifecycleHandoff.Stage != fleetLifecycleStageReleaseHandoff || result.LifecycleHandoff.File != scenarioLifecycleHandoffFilename || !validSHA256ContentHash(result.LifecycleHandoff.ContentHash) || result.LifecycleHandoff.SizeBytes == 0 {
			return errors.New("release campaign result does not bind its exact lifecycle handoff")
		}
	} else if name == "production-soak" {
		if result.LifecycleHandoff != nil || validateReleaseCampaignGateShape(cfg, result.PriorRelease) != nil {
			return errors.New("production campaign result does not bind its exact release predecessor")
		}
	} else if result.LifecycleHandoff != nil || result.PriorRelease != nil {
		return errors.New("non-release scenario unexpectedly contains release lineage")
	}
	adversaries := result.Adversaries
	adversaryConfig := cfg.Config.Scenarios.Adversaries
	wantSampleGapMillis := int64(adversaryConfig.SampleIntervalMilliseconds + 2*adversaryConfig.RequestTimeoutMilliseconds)
	if adversaries.Schema != "urnetwork-adversary-campaign-v1" || adversaries.Release != "1.0" || adversaries.Status != "stopped" || !strings.EqualFold(adversaries.MatrixHash, definition.AdversarialMatrixHash) || adversaries.Seed != adversaryConfig.Seed || adversaries.MinimumSamplesPerActor != adversaryConfig.MinimumSamplesPerActor || adversaries.MaximumActorErrorRatePPM != adversaryConfig.MaximumActorErrorRatePPM || adversaries.MaximumP99Milliseconds != adversaryConfig.MaximumP99LatencyMilliseconds || adversaries.MaximumAttackControlRatio != adversaryConfig.MaximumAttackControlP95Ratio || adversaries.MaximumSampleGapMillis != wantSampleGapMillis || adversaries.OperatorRequestCeilingQPS != adversaryConfig.MaximumOperatorRequestsPerSec || adversaries.RPCRequestCeilingQPS != adversaryConfig.MaximumRPCRequestsPerSec {
		return errors.New("release campaign adversary evidence does not match the approved limits")
	}
	if err := validateScenarioAcceptanceResult(cfg, definition, result); err != nil {
		return err
	}
	if result.StartHead.Number == 0 || result.EndHead.Number < result.StartHead.Number {
		return errors.New("release campaign finalized-head interval is invalid")
	}
	if _, err := decodeHex32("release start finalized hash", result.StartHead.Hash); err != nil {
		return err
	}
	if _, err := decodeHex32("release end finalized hash", result.EndHead.Hash); err != nil {
		return err
	}
	started, startErr := time.Parse(time.RFC3339Nano, result.StartedAt)
	completed, completeErr := time.Parse(time.RFC3339Nano, result.CompletedAt)
	if startErr != nil || completeErr != nil || completed.Before(started) {
		return errors.New("release campaign wall-clock interval is invalid")
	}
	if result.Result != "pass" || result.AssertionCount != len(result.Assertions) || result.FailedAssertionCount != 0 || len(result.Assertions) == 0 {
		return errors.New("release campaign result is not an assertion-clean pass")
	}
	seenAssertions := map[string]bool{}
	for _, assertion := range result.Assertions {
		if assertion.ID == "" || seenAssertions[assertion.ID] || !assertion.Passed {
			return errors.New("release campaign has a missing, duplicate, or failed assertion")
		}
		seenAssertions[assertion.ID] = true
	}
	for _, check := range definition.Checks {
		if !seenAssertions[check.ID] {
			return fmt.Errorf("release campaign is missing approved assertion %s", check.ID)
		}
	}
	if !seenAssertions["faults_within_acceptance_window"] {
		return errors.New("release campaign is missing its fault-window assertion")
	}
	hasTail := false
	for _, fault := range result.Faults {
		hasTail = hasTail || fault.PostAcceptanceEvidenceTail
	}
	if hasTail && !seenAssertions["fleet_lifecycle_fault_tail_bounded"] {
		return errors.New("release campaign is missing its bounded lifecycle-fault-tail assertion")
	}
	if len(result.Faults) != len(definition.Faults) {
		return fmt.Errorf("release campaign has %d fault records, want %d", len(result.Faults), len(definition.Faults))
	}
	faultSpecs := make(map[string]scenarioFaultSpec, len(definition.Faults))
	for _, fault := range definition.Faults {
		faultSpecs[fault.ID] = fault
		if !seenAssertions["fault_"+fault.ID] {
			return fmt.Errorf("release campaign is missing fault assertion %s", fault.ID)
		}
	}
	for _, record := range result.Faults {
		spec, ok := faultSpecs[record.ID]
		if !ok || record.Kind != spec.Kind || !stringSlicesEqual(record.Targets, spec.Targets) || !stringSlicesEqual(record.Impacts, spec.Impacts) || record.PreAcceptance != spec.PreAcceptance || record.PostAcceptanceEvidenceTail != spec.PostAcceptanceEvidenceTail || !slices.Equal(record.FleetIndices, spec.FleetIndices) {
			return fmt.Errorf("release campaign fault record %s does not match its approved spec", record.ID)
		}
	}
	for _, assertion := range appendFaultAssertions(nil, result.Faults, started, &ScenarioObservation{}) {
		if !assertion.Passed || !seenAssertions[assertion.ID] {
			return fmt.Errorf("release campaign fault evidence failed or omitted assertion %s", assertion.ID)
		}
	}
	for _, assertion := range adversaryAssertions(result.Adversaries, started, "") {
		if !assertion.Passed || !seenAssertions[assertion.ID] {
			return fmt.Errorf("release campaign adversary evidence failed or omitted assertion %s", assertion.ID)
		}
	}
	for _, required := range []string{"adversaries_overlap_happy_path", "adversary_matrix_coverage", "adversary_signed_start_continuity", anomalyGateAssertionID} {
		if !seenAssertions[required] {
			return fmt.Errorf("release campaign is missing required assertion %s", required)
		}
	}
	if result.Anomalies == nil || result.Anomalies.Schema != "urnetwork-sim-anomaly-ledger-v1" || result.Anomalies.Release != "1.0" || result.Anomalies.RunID != result.RunID || result.Anomalies.Status != "clean" || len(result.Anomalies.Entries) != 0 || !seenAssertions[anomalyGateAssertionID] {
		return errors.New("release campaign anomaly ledger is not clean and complete")
	}
	wantHash, err := canonicalScenarioResultHash(result)
	if err != nil || !strings.EqualFold(result.EvidenceHash, wantHash) {
		return stateMismatchError(err, "release campaign result hash does not match its canonical content")
	}
	return nil
}

func validateScenarioCampaignStartMarker(cfg *ResolvedConfig, roles *RoleSecrets, runDir string, result *ScenarioResult, name string) error {
	if cfg == nil || cfg.Config == nil || roles == nil || result == nil {
		return errors.New("scenario campaign start marker context is incomplete")
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return errors.New("scenario campaign start marker has no owner")
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		return err
	}
	var envelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(filepath.Join(runDir, scenarioCampaignStartFilename), &envelope); err != nil {
		return fmt.Errorf("scenario campaign start marker: %w", err)
	}
	if err := verifyEvidence(&envelope, &key.PublicKey); err != nil {
		return fmt.Errorf("scenario campaign start marker signature: %w", err)
	}
	if envelope.Kind != scenarioCampaignAttemptEvidenceKind || envelope.RunID != result.RunID || envelope.DeploymentID != result.DeploymentID || envelope.ChainID != result.ChainID || envelope.Netuid != result.Netuid || !strings.EqualFold(envelope.GenesisHash, result.GenesisHash) {
		return errors.New("scenario campaign start marker has the wrong deployment or run identity")
	}
	var payload scenarioCampaignAttemptPayload
	if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("scenario campaign start marker payload: %w", err)
	}
	if err := validateScenarioCampaignAttemptPayload(cfg, payload.PlanHash, name, &payload); err != nil {
		return err
	}
	boundary := payload.AcceptanceBoundary
	if payload.RunID != result.RunID || boundary == nil || payload.AcceptanceInvalidation != "" || payload.AcceptanceInvalidatedAt != "" || !scenarioAcceptanceWindowsEqual(&boundary.AcceptanceWindow, result.AcceptanceWindow) || boundary.CampaignStartHead != result.CampaignStartHead || boundary.CampaignStartEpoch != result.CampaignStartEpoch || !strings.EqualFold(boundary.ScenarioDefinitionHash, result.ScenarioDefinition) || !strings.EqualFold(boundary.AdversarialMatrixHash, result.AdversarialMatrix) {
		return errors.New("scenario campaign start marker differs from its completed result boundary")
	}
	if result.Adversaries == nil || result.Adversaries.StartedAt != boundary.AdversaryStartedAt || result.Adversaries.HappyPathStartedAt != boundary.AdversaryHappyPathStartedAt || !result.Adversaries.StartedBeforeHappyPath || !result.Adversaries.StoppedAfterHappyPath {
		return errors.New("scenario campaign result adversary interval does not continue from its signed start marker")
	}
	definition, err := scenarioDefinitionFor(cfg, name)
	if err != nil {
		return err
	}
	if err := validateScenarioAttemptFaultRecords(definition, result.AcceptanceWindow, result.Faults); err != nil {
		return err
	}
	if err := validateScenarioFaultProgress(boundary.Faults, result.Faults); err != nil {
		return fmt.Errorf("scenario campaign result fault ledger: %w", err)
	}
	started, startErr := time.Parse(time.RFC3339Nano, result.StartedAt)
	accepted, acceptedErr := time.Parse(time.RFC3339Nano, boundary.AcceptanceStartedAt)
	completed, completeErr := time.Parse(time.RFC3339Nano, result.CompletedAt)
	if startErr != nil || acceptedErr != nil || completeErr != nil || accepted.Before(started) || completed.Before(accepted) {
		return errors.New("scenario campaign signed start marker has an invalid wall-clock interval")
	}
	return nil
}

// validateReleaseCampaignResult retains the production-policy gate's narrow
// release-1.0 entry point while sharing the exact verifier with M3 adoption.
func validateReleaseCampaignResult(cfg *ResolvedConfig, result *ScenarioResult) error {
	return validateScenarioCampaignResult(cfg, result, "release-1.0")
}

// validateScenarioCampaignComplete authenticates the result plus the signed
// complete marker and every immutable evidence file named by that marker.
func validateScenarioCampaignComplete(cfg *ResolvedConfig, roles *RoleSecrets, runDir string, result *ScenarioResult, name string) (*ReleaseEvidenceEnvelope, error) {
	if err := validateScenarioCampaignResult(cfg, result, name); err != nil {
		return nil, err
	}
	if name == "release-1.0" || name == "production-soak" {
		if err := validateScenarioCampaignStartMarker(cfg, roles, runDir, result, name); err != nil {
			return nil, err
		}
	}
	if roles == nil {
		return nil, errors.New("release campaign gate requires role secrets")
	}
	owner, ok := roles.EVM["testnet-owner"]
	if !ok {
		return nil, errors.New("release campaign gate has no testnet owner role")
	}
	ownerKey, err := crypto.HexToECDSA(strings.TrimPrefix(owner.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, err
	}
	var complete ReleaseEvidenceEnvelope
	if err := decodeStrictJSONFile(filepath.Join(runDir, "complete.json"), &complete); err != nil {
		return nil, fmt.Errorf("release complete marker: %w", err)
	}
	if err := verifyEvidence(&complete, &ownerKey.PublicKey); err != nil {
		return nil, fmt.Errorf("release complete marker: %w", err)
	}
	if complete.Kind != "scenario-complete" || complete.RunID != result.RunID || complete.DeploymentID != cfg.Config.Deployment.DeploymentID || complete.ChainID != cfg.ChainID || complete.Netuid != cfg.Netuid || !strings.EqualFold(complete.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return nil, errors.New("release complete marker identity does not match its result")
	}
	var payload scenarioCompletePayload
	if err := decodeStrictJSONBytes(complete.Payload, &payload); err != nil {
		return nil, fmt.Errorf("release complete payload: %w", err)
	}
	if !strings.EqualFold(payload.ResultHash, result.EvidenceHash) || len(payload.Files) == 0 {
		return nil, errors.New("release complete marker does not bind the result hash and files")
	}
	if len(result.PublishedEvidence) != 0 && (!validSHA256ContentHash(payload.BundlePayloadHash) || !validSHA256ContentHash(payload.EvidenceManifestHash)) {
		return nil, errors.New("release complete marker does not bind its published scenario bundle and evidence manifest")
	}
	if len(result.PublishedEvidence) != 0 {
		var manifestEnvelope ReleaseEvidenceEnvelope
		if err := decodeStrictJSONFile(filepath.Join(runDir, campaignEvidenceManifestFilename), &manifestEnvelope); err != nil {
			return nil, fmt.Errorf("release campaign evidence manifest: %w", err)
		}
		manifest, err := decodeCampaignEvidenceManifest(&manifestEnvelope)
		if err != nil || verifyEvidence(&manifestEnvelope, &ownerKey.PublicKey) != nil || !strings.EqualFold(manifestEnvelope.ContentHash, payload.EvidenceManifestHash) || !strings.EqualFold(manifest.ResultHash, result.EvidenceHash) || !strings.EqualFold(manifest.BundlePayloadHash, payload.BundlePayloadHash) {
			return nil, stateMismatchError(err, "release campaign evidence manifest is invalid")
		}
		manifestFiles, err := campaignEvidenceManifestFiles(manifest.Files)
		if err != nil || !stringMapsEqual(manifestFiles, payload.Files) {
			return nil, stateMismatchError(err, "release campaign evidence manifest files do not match its completion")
		}
		if len(result.PublishedEvidence) != cfg.Config.Topology.Operators {
			return nil, errors.New("release complete marker has an incomplete published scenario bundle")
		}
		for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
			role, ok := roles.EVM[fmt.Sprintf("operator-%d-artifact", operator)]
			if !ok {
				return nil, fmt.Errorf("release completion commit operator %d signer is missing", operator)
			}
			key, err := crypto.HexToECDSA(strings.TrimPrefix(role.PrivateKeyHex, "0x"))
			if err != nil {
				return nil, err
			}
			bundlePath := filepath.Join(runDir, fmt.Sprintf("scenario-bundle.operator-%d.evidence.json", operator))
			var bundle ReleaseEvidenceEnvelope
			if err := decodeStrictJSONFile(bundlePath, &bundle); err != nil {
				return nil, fmt.Errorf("release scenario bundle operator %d: %w", operator, err)
			}
			if verifyEvidence(&bundle, &key.PublicKey) != nil || bundle.Kind != "scenario-bundle" || bundle.RunID != result.RunID || bundle.DeploymentID != cfg.Config.Deployment.DeploymentID || bundle.ChainID != cfg.ChainID || bundle.Netuid != cfg.Netuid || !strings.EqualFold(bundle.GenesisHash, cfg.Public.Chain.GenesisHash) || !strings.EqualFold(bundle.ContentHash, result.PublishedEvidence[operator-1].ContentHash) || !strings.EqualFold(bytesSHA256(bundle.Payload), payload.BundlePayloadHash) {
				return nil, fmt.Errorf("release scenario bundle operator %d is invalid", operator)
			}
			path := filepath.Join(runDir, fmt.Sprintf("scenario-complete-commit.operator-%d.evidence.json", operator))
			var commit ReleaseEvidenceEnvelope
			if err := decodeStrictJSONFile(path, &commit); err != nil {
				return nil, fmt.Errorf("release completion commit operator %d: %w", operator, err)
			}
			var nestedComplete ReleaseEvidenceEnvelope
			if decodeStrictJSONBytes(commit.Payload, &nestedComplete) != nil || verifyEvidence(&nestedComplete, &ownerKey.PublicKey) != nil || !strings.EqualFold(nestedComplete.ContentHash, complete.ContentHash) || nestedComplete.Signature != complete.Signature || verifyEvidence(&commit, &key.PublicKey) != nil || commit.Kind != "scenario-complete-commit" || commit.RunID != result.RunID || commit.DeploymentID != cfg.Config.Deployment.DeploymentID || commit.ChainID != cfg.ChainID || commit.Netuid != cfg.Netuid || !strings.EqualFold(commit.GenesisHash, cfg.Public.Chain.GenesisHash) {
				return nil, fmt.Errorf("release completion commit operator %d is invalid", operator)
			}
		}
	}
	hashes, err := evidenceFileHashes(runDir, cfg.Config.Topology.Operators)
	if err != nil {
		return nil, err
	}
	if !stringMapsEqual(payload.Files, hashes) || hashes["result.json"] == "" || hashes["assertions.json"] == "" || hashes["anomalies.json"] == "" || hashes["adversaries.json"] == "" {
		return nil, errors.New("release complete marker file hashes do not match the immutable run directory")
	}
	if name == "release-1.0" {
		if result.LifecycleHandoff == nil || payload.LifecycleHandoff == nil || *payload.LifecycleHandoff != *result.LifecycleHandoff || payload.PriorRelease != nil || !strings.EqualFold(hashes[result.LifecycleHandoff.File], result.LifecycleHandoff.ContentHash) {
			return nil, errors.New("release complete marker does not bind the exact lifecycle handoff")
		}
		if _, err := validateScenarioLifecycleHandoffFile(cfg, runDir, *result.LifecycleHandoff); err != nil {
			return nil, err
		}
	} else if name == "production-soak" {
		if result.PriorRelease == nil || payload.PriorRelease == nil || !releaseCampaignGatesEqual(payload.PriorRelease, result.PriorRelease) || payload.LifecycleHandoff != nil {
			return nil, errors.New("production complete marker does not bind the exact release predecessor")
		}
		stateDir := filepath.Dir(filepath.Dir(runDir))
		if _, _, err := validateExactReleaseCampaignGate(cfg, stateDir, roles, result.PriorRelease); err != nil {
			return nil, fmt.Errorf("production complete marker release predecessor: %w", err)
		}
	} else if payload.LifecycleHandoff != nil || payload.PriorRelease != nil {
		return nil, errors.New("non-release completion unexpectedly contains release lineage")
	}
	if name == "release-1.0" || name == "production-soak" {
		if err := validateFinalSemanticCaptureClosure(cfg, runDir, result); err != nil {
			return nil, fmt.Errorf("release complete marker has no closed final-semantic input graph: %w", err)
		}
	}
	return &complete, nil
}

func validSHA256ContentHash(value string) bool {
	hexHash := strings.TrimPrefix(strings.ToLower(value), "sha256:")
	if !strings.HasPrefix(strings.ToLower(value), "sha256:") || len(hexHash) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(hexHash)
	return err == nil
}

func validCanonicalHashHex(value string) bool {
	hexHash := stringsTrim0x(strings.ToLower(value))
	if !strings.HasPrefix(strings.ToLower(value), "0x") || len(hexHash) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(hexHash)
	return err == nil
}

// validateReleaseCampaignComplete converts one fully authenticated release
// result into the narrow gate consumed by production policy scheduling.
func validateReleaseCampaignComplete(cfg *ResolvedConfig, roles *RoleSecrets, runDir string, result *ScenarioResult) (*ReleaseCampaignGate, error) {
	complete, err := validateScenarioCampaignComplete(cfg, roles, runDir, result, "release-1.0")
	if err != nil {
		return nil, err
	}
	if result.LifecycleHandoff == nil {
		return nil, errors.New("release campaign has no lifecycle handoff")
	}
	gate := &ReleaseCampaignGate{
		Schema: releaseCampaignGateSchema, RunID: result.RunID, ResultHash: result.EvidenceHash,
		CompleteContentHash: complete.ContentHash, StartEpoch: result.StartEpoch, EndEpoch: result.EndEpoch,
		LifecycleHandoff: *result.LifecycleHandoff,
	}
	if err := validateReleaseCampaignGateShape(cfg, gate); err != nil {
		return nil, err
	}
	return gate, nil
}

// validateExactReleaseCampaignGate resolves only the run named by gate. It
// never scans for a newer release, and returns the authenticated handoff bytes
// used by the first production transition.
func validateExactReleaseCampaignGate(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, gate *ReleaseCampaignGate) (*ScenarioResult, []byte, error) {
	if err := validateReleaseCampaignGateShape(cfg, gate); err != nil {
		return nil, nil, err
	}
	runDir := filepath.Join(stateDir, "runs", gate.RunID)
	var result ScenarioResult
	if err := decodeStrictJSONFile(filepath.Join(runDir, "result.json"), &result); err != nil {
		return nil, nil, fmt.Errorf("read exact release campaign result: %w", err)
	}
	derived, err := validateReleaseCampaignComplete(cfg, roles, runDir, &result)
	if err != nil {
		return nil, nil, err
	}
	if !releaseCampaignGatesEqual(derived, gate) {
		return nil, nil, errors.New("release campaign gate differs from the exact signed result, completion, or lifecycle handoff")
	}
	data, err := validateScenarioLifecycleHandoffFile(cfg, runDir, gate.LifecycleHandoff)
	if err != nil {
		return nil, nil, err
	}
	return &result, data, nil
}

var errNoCompletedScenarioCampaign = errors.New("no signed anomaly-clean scenario campaign is complete")

// loadCompletedScenarioCampaign loads the newest fully signed passing result
// for one exact scenario definition. Failed runs have no complete marker and
// remain available for root-cause analysis; a malformed completed candidate
// fails closed instead of being skipped.
func loadCompletedScenarioCampaign(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, name string) (*ScenarioResult, *ReleaseEvidenceEnvelope, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil, errors.New("completed scenario name is empty")
	}
	runsDir := filepath.Join(stateDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("%w: %s", errNoCompletedScenarioCampaign, name)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read scenario campaign runs: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var selected *ScenarioResult
	var selectedComplete *ReleaseEvidenceEnvelope
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runDir := filepath.Join(runsDir, entry.Name())
		if _, statErr := os.Stat(filepath.Join(runDir, "complete.json")); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			return nil, nil, statErr
		}
		var result ScenarioResult
		if err := decodeStrictJSONFile(filepath.Join(runDir, "result.json"), &result); err != nil {
			return nil, nil, fmt.Errorf("decode completed scenario %s: %w", entry.Name(), err)
		}
		if result.Name != name {
			continue
		}
		complete, err := validateScenarioCampaignComplete(cfg, roles, runDir, &result, name)
		if err != nil {
			return nil, nil, fmt.Errorf("validate completed %s campaign %s: %w", name, entry.Name(), err)
		}
		if selected == nil || result.EndEpoch > selected.EndEpoch || result.EndEpoch == selected.EndEpoch && result.RunID > selected.RunID {
			copyResult := result
			copyComplete := *complete
			selected = &copyResult
			selectedComplete = &copyComplete
		}
	}
	if selected == nil {
		return nil, nil, fmt.Errorf("%w: %s", errNoCompletedScenarioCampaign, name)
	}
	return selected, selectedComplete, nil
}

func loadCompletedScenarioCampaignByRunID(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, name, runID string) (*ScenarioResult, *ReleaseEvidenceEnvelope, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(runID) == "" || filepath.Base(runID) != runID {
		return nil, nil, errors.New("completed scenario exact identity is invalid")
	}
	runDir := filepath.Join(stateDir, "runs", runID)
	if _, err := os.Stat(filepath.Join(runDir, "complete.json")); errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("%w: %s/%s", errNoCompletedScenarioCampaign, name, runID)
	} else if err != nil {
		return nil, nil, err
	}
	var result ScenarioResult
	if err := decodeStrictJSONFile(filepath.Join(runDir, "result.json"), &result); err != nil {
		return nil, nil, fmt.Errorf("decode exact completed scenario %s: %w", runID, err)
	}
	if result.RunID != runID || result.Name != name {
		return nil, nil, errors.New("exact completed scenario directory does not match its result identity")
	}
	complete, err := validateScenarioCampaignComplete(cfg, roles, runDir, &result, name)
	if err != nil {
		return nil, nil, fmt.Errorf("validate exact completed %s campaign %s: %w", name, runID, err)
	}
	return &result, complete, nil
}

// Load the newest fully signed passing release result and convert it into the
// production-policy authorization gate.
func loadReleaseCampaignGate(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) (*ReleaseCampaignGate, error) {
	result, _, err := loadCompletedScenarioCampaign(cfg, stateDir, roles, "release-1.0")
	if err != nil {
		return nil, err
	}
	return validateReleaseCampaignComplete(cfg, roles, filepath.Join(stateDir, "runs", result.RunID), result)
}

// scenarioCampaignRunner executes one named scenario with its owner-signed
// durable attempt, the already-open journal, and transaction-capable executor.
type scenarioCampaignRunner func(context.Context, *ResolvedConfig, string, string, *Journal, *Executor, *scenarioCampaignAttempt) error

// runReleaseCandidateCampaign adopts only fully authenticated completed
// phases, executes the first missing phase, and revalidates its signed marker
// before crossing the M2-to-M3 boundary. Failed or canceled phases resume the
// exact pre-mutation attempt instead of silently creating a new lineage.
func runReleaseCandidateCampaign(ctx context.Context, cfg *ResolvedConfig, stateDir string, journal *Journal, executor *Executor, roles *RoleSecrets, runner scenarioCampaignRunner) error {
	return runReleaseCandidateCampaignWithAnalyzer(ctx, cfg, stateDir, journal, executor, roles, runner, runFinalCampaignArchivePreflight, runFinalSemanticCampaignAnalyzer)
}

func runFinalSemanticCampaignAnalyzer(ctx context.Context, cfg *ResolvedConfig, stateDir, runDir string, roles *RoleSecrets, result *ScenarioResult) error {
	_, err := PublishOrResumeFinalSemanticSupplement(ctx, cfg, roles, stateDir, runDir, result)
	return err
}
