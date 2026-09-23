// Conformance evidence keeps its original approval identity across compatible
// configuration revisions. Reuse authenticates that source; it never relabels a
// receipt, completes an unfinished phase, or authorizes a transaction replay.
package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/crv4"
)

// The approved action graph, custody, and policy must remain identical. Only
// transaction gas-unit ceilings and their derived spend may differ.
func validatePrecompileEvidenceCarry(cfg *ResolvedConfig, plan, source *SetupPlan, probe common.Address, evidence *PrecompileConformanceEvidence) error {
	if cfg == nil || plan == nil || source == nil || evidence == nil || cfg.ConfigHash != plan.ConfigHash || cfg.PolicyHash != plan.PolicyHash || !plan.allowedPlanHashes()[source.PlanHash] {
		return errors.New("precompile evidence source is outside the approved configuration lineage")
	}
	if source.ConfigHash != evidence.ConfigHash {
		return errors.New("precompile evidence differs from its original configuration")
	}
	if err := validatePrecompileEvidenceCarryScope(plan, source, probe); err != nil {
		return err
	}
	if err := validatePrecompileEvidenceIdentity(historicalPlanConfig(cfg, source, plan), probe, evidence); err != nil {
		return err
	}
	if !strings.EqualFold(evidence.Owner, plan.Roles.Deployer) {
		return errors.New("precompile evidence carry changed its approved owner")
	}
	if plan.PrecompileProbeSuccessor != nil {
		if _, err := precompileProbeSuccessorEvidence(plan, evidence, evidence); err != nil {
			return err
		}
		original := plan.PrecompileProbeSuccessor.Evidence
		if evidence.SampleHotkey != original.SampleHotkey || evidence.SampleUID != original.SampleUID || evidence.AbsentHotkey != original.AbsentHotkey || evidence.MoveHotkey != original.MoveHotkey || evidence.RecoveryColdkey != original.RecoveryColdkey {
			return errors.New("precompile evidence carry changed its approved native roles")
		}
	}
	if plan.PrecompileProbeSuccessor == nil {
		for label, actual := range map[string]string{
			validatorHotkeyLabel(1): evidence.SampleHotkey, validatorHotkeyLabel(2): evidence.MoveHotkey,
			fleetColdkeyLabel(1): evidence.RecoveryColdkey,
		} {
			keypair, err := crv4.KeypairFromSeed(derive32(cfg, "substrate/"+label))
			if err != nil {
				return err
			}
			key := keypair.PublicKey()
			if !strings.EqualFold(actual, hexBytesValue(key[:])) {
				return errors.New("precompile evidence carry changed its approved native roles")
			}
		}
		absent := derive32(cfg, "precompile/absent-hotkey")
		if !strings.EqualFold(evidence.AbsentHotkey, hexBytesValue(absent[:])) {
			return errors.New("precompile evidence carry changed its absent-hotkey control")
		}
	}
	return nil
}

// Receipt routing uses the same immutable action scope even if a battery was
// recorded by a later compatible approval than the evidence's original label.
func validatePrecompileEvidenceCarryScope(plan, source *SetupPlan, probe common.Address) error {
	if plan == nil || source == nil || !plan.allowedPlanHashes()[source.PlanHash] {
		return errors.New("precompile evidence source is outside approved plan lineage")
	}
	if (source.PolicyHash != plan.PolicyHash && !policyRateAmendmentAllowsAncestor(plan, source)) || source.DeploymentID != plan.DeploymentID || source.ChainID != plan.ChainID || source.GenesisHash != plan.GenesisHash || source.Netuid != plan.Netuid || source.Owner != plan.Owner || !reflect.DeepEqual(source.Roles, plan.Roles) || !contractDeploymentAddressesEqual(source.Deployment, plan.Deployment) || !contractDeploymentRuntimeHashesCompatible(source.Deployment, plan.Deployment) || approvedPrecompileProbe(source) != probe || approvedPrecompileProbe(plan) != probe || !reflect.DeepEqual(source.PrecompileProbeSuccessor, plan.PrecompileProbeSuccessor) {
		return errors.New("precompile evidence carry changed its policy, probe, roles, or deployment")
	}
	if source.LiveFacts.ProbeTAORao != plan.LiveFacts.ProbeTAORao || source.LiveFacts.NominatorMinimumRao != plan.LiveFacts.NominatorMinimumRao {
		return errors.New("precompile evidence carry changed its approved value input")
	}
	count := 0
	for _, original := range source.Actions {
		if !strings.HasPrefix(original.ID, "precompile.") {
			continue
		}
		count++
		current, err := exactPlanActionByID(plan, original.ID)
		if err != nil {
			return err
		}
		originalHash, originalErr := actionIntentHash(original)
		currentHash, currentErr := actionIntentHash(current)
		if originalErr != nil || currentErr != nil || originalHash != original.IntentHash || currentHash != current.IntentHash || (originalHash != currentHash && !sameEVMTransactionExceptGasUnits(original, current)) {
			return fmt.Errorf("precompile evidence carry changed action semantics: %s", original.ID)
		}
	}
	currentCount := 0
	for _, action := range plan.Actions {
		if strings.HasPrefix(action.ID, "precompile.") {
			currentCount++
		}
	}
	if count != 10 || currentCount != count {
		return errors.New("precompile evidence carry changed the conformance phase set")
	}
	return nil
}

// The journal supplies bounded source candidates instead of scanning every
// large archived setup plan. Each candidate is hash-authenticated before use.
func readPrecompileEvidenceSource(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, probe common.Address, evidence *PrecompileConformanceEvidence) (*SetupPlan, error) {
	if cfg == nil || plan == nil || evidence == nil {
		return nil, errors.New("precompile evidence carry context is unavailable")
	}
	// Config identity is the sole permitted mismatch. Foreign chain, policy,
	// probe, or coldkey evidence never enters historical source resolution.
	identity := *cfg
	identity.ConfigHash = evidence.ConfigHash
	if err := validatePrecompileEvidenceIdentity(&identity, probe, evidence); err != nil {
		return nil, err
	}
	allowed, seen := plan.allowedPlanHashes(), map[string]bool{}
	var sourceErrors []error
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if !strings.HasPrefix(entry.ActionID, "precompile.") || entry.DeploymentID != plan.DeploymentID || !allowed[entry.PlanHash] || seen[entry.PlanHash] {
			continue
		}
		seen[entry.PlanHash] = true
		source := plan
		if entry.PlanHash != plan.PlanHash {
			var err error
			source, err = readValidatorEvidenceHistoricalPlan(stateDir, entry.PlanHash)
			if err != nil {
				sourceErrors = append(sourceErrors, err)
				continue
			}
		}
		if source.ConfigHash != evidence.ConfigHash {
			continue
		}
		action, err := exactPlanActionByID(source, entry.ActionID)
		if err != nil || !actionAcceptsIntent(action, entry.IntentHash) {
			return nil, errors.New("precompile evidence journal differs from its source action")
		}
		if err := validatePrecompileEvidenceCarry(cfg, plan, source, probe, evidence); err != nil {
			return nil, err
		}
		return source, nil
	}
	return nil, errors.Join(append([]error{errors.New("precompile evidence has no authenticated compatible source approval")}, sourceErrors...)...)
}

// Both mutation and audit paths retain the source label while the current
// executor continues enforcing its own transaction intents and spend limits.
func (self *Executor) validatePrecompileEvidence(probe common.Address, evidence *PrecompileConformanceEvidence) error {
	if evidence != nil && evidence.Recovery != nil {
		if err := validatePrecompileRecoveryPlan(self.plan, evidence, &evidence.Recovery.Authorization); err != nil {
			return err
		}
	}
	if err := validatePrecompileEvidenceIdentity(self.cfg, probe, evidence); err == nil {
		return nil
	}
	if self.journal == nil {
		return errors.New("precompile evidence carry has no authenticated journal")
	}
	_, err := readPrecompileEvidenceSource(self.cfg, self.stateDir, self.plan, self.journal.Entries(), probe, evidence)
	return err
}

// Standalone analysis loads the same authenticated owner once. Live campaigns
// already have that plan and journal, so snapshots do not repeat a large census.
func (self *liveScenarioProbe) validatePrecompileEvidence(probe common.Address, evidence *PrecompileConformanceEvidence) error {
	if evidence != nil && evidence.Recovery != nil {
		if self.precompilePlan == nil {
			raw, err := readSetupPlanBytes(self.stateDir, "plan.json")
			if err != nil {
				return err
			}
			self.precompilePlan, err = decodePersistedPlanBytesForHistory(raw, true)
			if err != nil {
				return err
			}
		}
		if err := validatePrecompileRecoveryPlan(self.precompilePlan, evidence, &evidence.Recovery.Authorization); err != nil {
			return err
		}
	}
	if err := validatePrecompileEvidenceIdentity(self.cfg, probe, evidence); err == nil {
		return nil
	}
	if self.precompilePlan == nil {
		raw, err := readSetupPlanBytes(self.stateDir, "plan.json")
		if err != nil {
			return err
		}
		self.precompilePlan, err = decodePersistedPlanBytesForHistory(raw, true)
		if err != nil {
			return err
		}
	}
	if self.precompileSourcePlan != nil && evidence != nil && self.precompileSourcePlan.ConfigHash == evidence.ConfigHash {
		return validatePrecompileEvidenceCarry(self.cfg, self.precompilePlan, self.precompileSourcePlan, probe, evidence)
	}
	var entries []JournalEntry
	if self.precompileJournal != nil {
		entries = self.precompileJournal.Entries()
	} else {
		var err error
		entries, err = readJournalEntries(self.stateDir)
		if err != nil {
			return err
		}
	}
	source, err := readPrecompileEvidenceSource(self.cfg, self.stateDir, self.precompilePlan, entries, probe, evidence)
	if err != nil {
		return err
	}
	self.precompileSourcePlan = source
	return nil
}

// Reconstruct only the battery phase; later seed inputs can be durable even
// when estimate/send failed. The exact recorded observation must still match.
func precompileBatteryHistoricalEvidence(action Action, evidence *PrecompileConformanceEvidence, record *ActionPostcondition) (*PrecompileConformanceEvidence, error) {
	if action.ID != "precompile.read-battery" || evidence == nil || record == nil {
		return nil, errors.New("precompile battery historical evidence is unavailable")
	}
	snapshot := *evidence
	if record.Observed["evidence_hash"] != snapshot.EvidenceHash {
		snapshot.Seed = PrecompileValueStep{}
		snapshot.Forward, snapshot.Back = PrecompileMoveStep{}, PrecompileMoveStep{}
		snapshot.Snapshot = PrecompileSnapshotStep{}
		snapshot.Dividend = PrecompileDividendStep{}
		snapshot.Transfer = PrecompileTransferStep{}
		snapshot.Complete = false
		snapshot.EvidenceHash = ""
		var err error
		snapshot.EvidenceHash, err = canonicalHashHex(&snapshot)
		if err != nil {
			return nil, err
		}
	}
	observed := map[string]any{
		"kind": action.Kind, "target": action.Target, "probe": snapshot.ProbeAddress,
		"evidence_hash": snapshot.EvidenceHash, "complete": snapshot.Complete, "canonical_chain_evidence": true,
	}
	if err := observedPostconditionMatches(record.Observed, observed); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
