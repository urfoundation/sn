// Policy revisions after campaign activity are accepted only as an exact,
// future-effective acceleration of the authenticated release policy.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/protocol"
)

// Distinguish an unchanged policy, a pre-activity replacement, and the one
// bounded post-activity migration supported by the release harness.
type policyRevisionClass uint8

const (
	policyRevisionNone policyRevisionClass = iota
	policyRevisionPristine
	policyRevisionFutureAcceleration
)

// Carry the authenticated prior policy and whether a launch/stop generation
// must remain part of the approval boundary.
type policyRevisionDecision struct {
	Class           policyRevisionClass
	PreviousPolicy  *protocol.Policy
	RestartRequired bool
}

// Reconstruct the exact contract accounting permitted before the campaign
// begins, including the next signer nonce after recovered convictions.
type policyRevisionReserveAccounting struct {
	CampaignReservedRao uint64
	NextDepositNonces   map[int]uint64
}

// Derive accounting from an interrupted duplicate transaction before its
// no-broadcast reconciliation was added to an approved descendant plan.
func policyRevisionAccountingFromRecovery(cfg *ResolvedConfig, recovery voluntaryConvictionDuplicateRecovery) (policyRevisionReserveAccounting, error) {
	if err := validateVoluntaryConvictionDuplicateRecovery(cfg, recovery); err != nil {
		return policyRevisionReserveAccounting{}, err
	}
	nonce, ok := new(big.Int).SetString(recovery.DuplicateNonce, 10)
	if !ok || !nonce.IsUint64() || nonce.Uint64() == ^uint64(0) {
		return policyRevisionReserveAccounting{}, errors.New("duplicate voluntary-conviction nonce cannot define the next nonce")
	}
	return policyRevisionReserveAccounting{
		CampaignReservedRao: recovery.CumulativeAfterRao,
		NextDepositNonces:   map[int]uint64{1: nonce.Uint64() + 1},
	}, nil
}

// Derive accounting from the exact reconciliation already verified in the
// active plan lineage.
func policyRevisionAccountingFromReconciliation(cfg *ResolvedConfig, prior *SetupPlan, entries []JournalEntry, action Action) (policyRevisionReserveAccounting, error) {
	if _, err := validateVoluntaryConvictionReconciliationAction(prior, action, prior.allowedPlanHashes()); err != nil {
		return policyRevisionReserveAccounting{}, err
	}
	verified := false
	for _, entry := range entries {
		if prior.allowedPlanHashes()[entry.PlanHash] && entry.ActionID == action.ID && entry.IntentHash == action.IntentHash && entry.Stage == StageVerified {
			verified = true
		}
	}
	if !verified {
		return policyRevisionReserveAccounting{}, errors.New("voluntary-conviction reconciliation is not durably verified")
	}
	reserved, reservedErr := strconv.ParseUint(action.Parameters[voluntaryRecoveryCumulativeAfterParameter], 10, 64)
	nonce, nonceOK := new(big.Int).SetString(action.Parameters[voluntaryRecoveryDuplicateNonceParameter], 10)
	wantReserved, multiplyOK := checkedMul(cfg.Config.Scenarios.VoluntaryConvictionRao, 2)
	if reservedErr != nil || !multiplyOK || reserved != wantReserved || !nonceOK || !nonce.IsUint64() || nonce.Uint64() == ^uint64(0) {
		return policyRevisionReserveAccounting{}, errors.New("voluntary-conviction reconciliation accounting is invalid")
	}
	return policyRevisionReserveAccounting{CampaignReservedRao: reserved, NextDepositNonces: map[int]uint64{1: nonce.Uint64() + 1}}, nil
}

// Recover the only permitted pre-campaign reserve accounting from authenticated
// receipts/journal evidence. This makes any unplanned demand deposit fatal to a
// stopped-topology policy acceleration.
func authenticatedPolicyRevisionReserveAccounting(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry, recoveries planRevisionRecoveries) (policyRevisionReserveAccounting, error) {
	if cfg == nil || cfg.Config == nil || prior == nil {
		return policyRevisionReserveAccounting{}, errors.New("policy revision reserve accounting context is incomplete")
	}
	if len(recoveries.VoluntaryConvictions) > 1 {
		return policyRevisionReserveAccounting{}, errors.New("multiple voluntary-conviction recoveries cannot define policy migration accounting")
	}
	if len(recoveries.VoluntaryConvictions) == 1 {
		return policyRevisionAccountingFromRecovery(cfg, recoveries.VoluntaryConvictions[0])
	}
	var reconciliation *Action
	for index := range prior.Actions {
		if prior.Actions[index].ID != voluntaryConvictionReconciliationActionID {
			continue
		}
		if reconciliation != nil {
			return policyRevisionReserveAccounting{}, errors.New("multiple voluntary-conviction reconciliations cannot define policy migration accounting")
		}
		reconciliation = &prior.Actions[index]
	}
	if reconciliation != nil {
		return policyRevisionAccountingFromReconciliation(cfg, prior, entries, *reconciliation)
	}

	var evidence VoluntaryConvictionEvidence
	if err := decodeStrictJSONFile(filepath.Join(stateDir, "public", "voluntary-conviction.json"), &evidence); err != nil {
		return policyRevisionReserveAccounting{}, fmt.Errorf("read policy-migration voluntary-conviction evidence: %w", err)
	}
	var finalized *JournalEntry
	for index := range entries {
		entry := &entries[index]
		if !prior.allowedPlanHashes()[entry.PlanHash] || entry.ActionID != voluntaryConvictionActionID || entry.Stage != StageFinalized ||
			!strings.EqualFold(entry.TransactionHash, evidence.TransactionHash) || entry.BlockNumber != evidence.FinalizedBlock || !strings.EqualFold(entry.BlockHash, evidence.FinalizedHash) {
			continue
		}
		if finalized != nil {
			return policyRevisionReserveAccounting{}, errors.New("voluntary-conviction evidence matches multiple finalized journal entries")
		}
		copy := *entry
		finalized = &copy
	}
	if finalized == nil {
		return policyRevisionReserveAccounting{}, errors.New("policy-migration voluntary conviction lacks finalized journal evidence")
	}
	verified := false
	for _, entry := range entries {
		if entry.PlanHash == finalized.PlanHash && entry.ActionID == finalized.ActionID && entry.IntentHash == finalized.IntentHash && entry.Stage == StageVerified {
			verified = true
		}
	}
	if !verified {
		return policyRevisionReserveAccounting{}, errors.New("policy-migration voluntary conviction lacks verified journal evidence")
	}
	sourcePlan, err := loadVoluntaryConvictionLineagePlan(stateDir, prior, finalized.PlanHash)
	if err != nil {
		return policyRevisionReserveAccounting{}, err
	}
	if err := voluntaryConvictionEvidenceMatches(cfg, sourcePlan, evidence); err != nil {
		return policyRevisionReserveAccounting{}, err
	}
	reserved, reservedErr := strconv.ParseUint(evidence.AfterConvictionRao, 10, 64)
	nonce, nonceOK := new(big.Int).SetString(evidence.Nonce, 10)
	if reservedErr != nil || reserved != cfg.Config.Scenarios.VoluntaryConvictionRao || !nonceOK || !nonce.IsUint64() || nonce.Uint64() == ^uint64(0) {
		return policyRevisionReserveAccounting{}, errors.New("policy-migration voluntary conviction accounting is invalid")
	}
	return policyRevisionReserveAccounting{CampaignReservedRao: reserved, NextDepositNonces: map[int]uint64{1: nonce.Uint64() + 1}}, nil
}

// Hash the historical canonical wire form without applying today's stricter
// duration validation to a policy which was valid when it was approved.
func historicalPolicyHash(policy *protocol.Policy) (string, error) {
	if policy == nil {
		return "", errors.New("historical policy is unavailable")
	}
	wire, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(wire)
	return "0x" + hex.EncodeToString(hash[:]), nil
}

// Extract only the policy and its declared hash from one private rendered
// validator config. Strict decoding rejects fields unknown to the policy wire
// schema while allowing the surrounding runtime config to evolve separately.
func readRenderedValidatorPolicy(path string) (*protocol.Policy, string, error) {
	wire, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(wire))
	if err := decoder.Decode(&document); err != nil {
		return nil, "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, "", errors.New("rendered validator config has multiple YAML documents")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, "", errors.New("rendered validator config is not a mapping")
	}
	root := document.Content[0]
	var policyNode *yaml.Node
	declaredHash := ""
	for index := 0; index+1 < len(root.Content); index += 2 {
		key, value := root.Content[index], root.Content[index+1]
		switch key.Value {
		case "policy":
			if policyNode != nil {
				return nil, "", errors.New("rendered validator config has duplicate policy fields")
			}
			policyNode = value
		case "policy_hash":
			if declaredHash != "" || value.Kind != yaml.ScalarNode {
				return nil, "", errors.New("rendered validator config has an invalid policy hash field")
			}
			declaredHash = strings.TrimSpace(value.Value)
		}
	}
	if policyNode == nil || declaredHash == "" {
		return nil, "", errors.New("rendered validator config lacks policy identity")
	}
	policyWire, err := yaml.Marshal(policyNode)
	if err != nil {
		return nil, "", err
	}
	var policy protocol.Policy
	policyDecoder := yaml.NewDecoder(bytes.NewReader(policyWire))
	policyDecoder.KnownFields(true)
	if err := policyDecoder.Decode(&policy); err != nil {
		return nil, "", fmt.Errorf("decode rendered validator policy: %w", err)
	}
	if _, err := decodeHex32("rendered validator policy hash", declaredHash); err != nil {
		return nil, "", err
	}
	return &policy, declaredHash, nil
}

// Require two independent rendered copies to reproduce the hash approved by
// the prior plan. A modified state file cannot substitute a different policy
// without breaking that SHA-256 identity.
func authenticatedPreviousPolicy(stateDir string, prior *SetupPlan) (*protocol.Policy, error) {
	if prior == nil {
		return nil, errors.New("prior policy plan is unavailable")
	}
	var authenticated *protocol.Policy
	for validator := 1; validator <= 2; validator++ {
		path := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validator), "validator.yml")
		policy, declaredHash, err := readRenderedValidatorPolicy(path)
		if err != nil {
			return nil, fmt.Errorf("read authenticated validator-%d policy: %w", validator, err)
		}
		computedHash, err := historicalPolicyHash(policy)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(declaredHash, computedHash) || !strings.EqualFold(computedHash, prior.PolicyHash) {
			return nil, fmt.Errorf("validator-%d policy does not authenticate prior hash %s", validator, prior.PolicyHash)
		}
		if authenticated != nil && !reflect.DeepEqual(*authenticated, *policy) {
			return nil, errors.New("rendered validators disagree on the prior policy")
		}
		authenticated = policy
	}
	return authenticated, nil
}

// Only the future cadence and its aggregate campaign ceiling may decrease.
// Every field used by the active accelerated epoch remains byte-identical.
func validateFuturePolicyAcceleration(previous, next *protocol.Policy) error {
	if previous == nil || next == nil {
		return errors.New("future policy acceleration context is incomplete")
	}
	if next.ProductionCadence.AfterAcceleratedEpochs > previous.ProductionCadence.AfterAcceleratedEpochs ||
		next.ProductionCadence.EpochBlocks > previous.ProductionCadence.EpochBlocks ||
		next.ProductionCadence.RootCommitWindowBlocks > previous.ProductionCadence.RootCommitWindowBlocks ||
		next.ProductionCadence.FinalizeOffsetBlocks > previous.ProductionCadence.FinalizeOffsetBlocks ||
		next.ProductionCadence.CloseGraceBlocks > previous.ProductionCadence.CloseGraceBlocks ||
		next.Deposit.TotalTestCampaignCapRao > previous.Deposit.TotalTestCampaignCapRao {
		return errors.New("future policy revision is not a bounded acceleration")
	}
	normalized := *previous
	normalized.Deposit = previous.Deposit
	normalized.ProductionCadence = next.ProductionCadence
	normalized.Deposit.TotalTestCampaignCapRao = next.Deposit.TotalTestCampaignCapRao
	if !reflect.DeepEqual(normalized, *next) {
		return errors.New("policy revision changes fields outside future cadence and campaign cap")
	}
	return nil
}

// Prove that a previously launched topology reached the harness's bounded stop
// state. PID start times distinguish a dead supervisor from PID reuse, and all
// children must have published the supervisor-owned terminal reason.
func validateStoppedTopologyPolicyRevision(cfg *ResolvedConfig, stateDir string) error {
	if cfg == nil || cfg.Config == nil {
		return errors.New("stopped topology config is unavailable")
	}
	var manifest SupervisorFile
	if err := decodeStrictJSONFile(filepath.Join(stateDir, "supervisor.json"), &manifest); err != nil {
		return fmt.Errorf("read stopped topology manifest: %w", err)
	}
	var state SupervisorState
	if err := decodeStrictJSONFile(filepath.Join(stateDir, "supervisor.state.json"), &state); err != nil {
		return err
	}
	expectedProcesses := 2 + 3*cfg.Config.Topology.Operators + cfg.Config.Topology.MinerSwarmProcesses + cfg.Config.Topology.Operators + cfg.Config.Topology.Validators
	if manifest.Schema != "urnetwork-sim-supervisor-v1" || manifest.DeploymentID != cfg.Config.Deployment.DeploymentID || len(manifest.Specs) != expectedProcesses {
		return errors.New("stopped topology manifest identity is incomplete")
	}
	binaryDigest, binaryHashErr := hex.DecodeString(strings.TrimPrefix(manifest.BinaryHash, "sha256:"))
	if !strings.HasPrefix(manifest.BinaryHash, "sha256:") || binaryHashErr != nil || len(binaryDigest) != sha256.Size {
		return errors.New("stopped topology binary hash is invalid")
	}
	manifestHash, err := canonicalHashHex(manifest)
	if err != nil {
		return err
	}
	if state.Schema != "urnetwork-sim-supervisor-state-v1" || state.SupervisorPID <= 1 || len(state.Processes) != expectedProcesses || state.ManifestHash != manifestHash {
		return errors.New("stopped topology supervisor identity is incomplete")
	}
	if _, err := decodeHex32("stopped topology manifest hash", manifestHash); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, state.UpdatedAt); err != nil {
		return errors.New("stopped topology timestamp is invalid")
	}
	if observedStart, err := processStartTimeTicks(state.SupervisorPID); err == nil {
		if state.SupervisorStartTimeTicks == 0 {
			return errors.New("legacy topology supervisor PID is live and has no recorded generation")
		}
		if observedStart == state.SupervisorStartTimeTicks {
			return errors.New("topology supervisor is still running")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("observe stopped topology supervisor: %w", err)
	}
	specs := make(map[string]ProcessSpec, len(manifest.Specs))
	for _, spec := range manifest.Specs {
		if spec.ID == "" || specs[spec.ID].ID != "" {
			return errors.New("stopped topology manifest has invalid process identities")
		}
		specs[spec.ID] = spec
	}
	seen := map[string]bool{}
	for _, process := range state.Processes {
		spec, ok := specs[process.ID]
		if !ok || seen[process.ID] || process.Role != spec.Role || process.Identity != spec.Identity || process.PID != 0 || process.Healthy || process.ExitError != "supervisor stopped" {
			return fmt.Errorf("topology process %q lacks the exact stopped state", process.ID)
		}
		seen[process.ID] = true
	}
	if _, err := os.Stat(temporaryProcessFilePath(stateDir)); err == nil {
		return errors.New("topology still has temporary process ownership state")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Require the stopped topology to be the exact completed M0A setup boundary.
// Any later journal entry means a live scenario started and makes this narrow
// pre-campaign migration unavailable, even when that action did not broadcast.
func validateStoppedTopologyJournalBoundary(prior *SetupPlan, entries []JournalEntry) error {
	if prior == nil {
		return errors.New("stopped topology plan is unavailable")
	}
	actions := map[string]Action{}
	for _, action := range prior.Actions {
		if action.ID != "topology.launch" && action.ID != "churn.tournament-complete" {
			continue
		}
		if _, duplicate := actions[action.ID]; duplicate {
			return fmt.Errorf("stopped topology plan has duplicate %s actions", action.ID)
		}
		actions[action.ID] = action
	}
	topology, topologyOK := actions["topology.launch"]
	churn, churnOK := actions["churn.tournament-complete"]
	if !topologyOK || !churnOK || topology.IntentHash == "" || churn.IntentHash == "" {
		return errors.New("stopped topology plan lacks exact M0A boundary actions")
	}
	var topologySequence, churnSequence uint64
	for _, entry := range entries {
		if entry.PlanHash != prior.PlanHash || entry.Stage != StageVerified {
			continue
		}
		switch {
		case entry.ActionID == topology.ID && entry.IntentHash == topology.IntentHash:
			topologySequence = entry.Sequence
		case entry.ActionID == churn.ID && entry.IntentHash == churn.IntentHash:
			churnSequence = entry.Sequence
		}
	}
	if topologySequence == 0 || churnSequence <= topologySequence {
		return errors.New("stopped topology journal lacks the exact completed M0A boundary")
	}
	for _, entry := range entries {
		if entry.Sequence > churnSequence {
			return fmt.Errorf("stopped topology journal advanced after M0A at sequence %d", entry.Sequence)
		}
	}
	return nil
}

// Classify a policy change from authenticated journal history. Once any other
// live workload starts, even the narrow acceleration is no longer admissible.
func classifyPolicyRevision(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, entries []JournalEntry) (policyRevisionDecision, error) {
	if cfg == nil || prior == nil {
		return policyRevisionDecision{}, errors.New("policy revision context is incomplete")
	}
	if strings.EqualFold(prior.PolicyHash, cfg.PolicyHash) {
		return policyRevisionDecision{Class: policyRevisionNone}, nil
	}
	allowedPlans := prior.allowedPlanHashes()
	convictionStarted := false
	topologyStarted := false
	for _, entry := range entries {
		if !allowedPlans[entry.PlanHash] || (entry.Stage != StageBroadcast && entry.Stage != StageFinalized && entry.Stage != StageVerified) {
			continue
		}
		switch entry.ActionID {
		case voluntaryConvictionActionID:
			convictionStarted = true
		case "topology.launch":
			topologyStarted = true
		case dishonestDepositActionID, "production.schedule-policy":
			return policyRevisionDecision{}, fmt.Errorf("policy revision is forbidden after %s reached %s", entry.ActionID, entry.Stage)
		}
	}
	if !convictionStarted && !topologyStarted {
		return policyRevisionDecision{Class: policyRevisionPristine}, nil
	}
	previous, err := authenticatedPreviousPolicy(stateDir, prior)
	if err != nil {
		return policyRevisionDecision{}, fmt.Errorf("policy revision is forbidden after %s reached a transaction stage: %w", voluntaryConvictionActionID, err)
	}
	if err := validateFuturePolicyAcceleration(previous, cfg.Policy); err != nil {
		return policyRevisionDecision{}, fmt.Errorf("policy revision is forbidden after %s reached a transaction stage: %w", voluntaryConvictionActionID, err)
	}
	if topologyStarted {
		if err := validateStoppedTopologyPolicyRevision(cfg, stateDir); err != nil {
			return policyRevisionDecision{}, fmt.Errorf("policy revision is forbidden after topology.launch without a proved stop boundary: %w", err)
		}
		if err := validateStoppedTopologyJournalBoundary(prior, entries); err != nil {
			return policyRevisionDecision{}, fmt.Errorf("policy revision is forbidden after topology.launch without a proved journal boundary: %w", err)
		}
	}
	return policyRevisionDecision{Class: policyRevisionFutureAcceleration, PreviousPolicy: previous, RestartRequired: topologyStarted}, nil
}
