// A failed, unfunded probe may be replaced once without repeating the native
// commitment drill or changing the retained coordinator and fleet deployment.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Binds one empty CREATE boundary and the original, completed native phases.
// Receipt references retain their source plans; no receipt is reissued.
type PrecompileProbeSuccessor struct {
	Schema             string                        `json:"schema"`
	SourcePlanHash     string                        `json:"source_plan_hash"`
	RetiredProbe       string                        `json:"retired_probe"`
	RetiredRuntimeHash string                        `json:"retired_runtime_hash"`
	Probe              string                        `json:"probe"`
	DeployerNonce      uint64                        `json:"deployer_nonce"`
	RuntimeHash        string                        `json:"runtime_hash"`
	CreationHash       string                        `json:"creation_hash"`
	FinalizedHead      ChainHead                     `json:"finalized_head"`
	JournalSequence    uint64                        `json:"journal_sequence"`
	JournalHash        string                        `json:"journal_hash"`
	Write              JournalEntry                  `json:"write"`
	Restore            JournalEntry                  `json:"restore"`
	Evidence           PrecompileConformanceEvidence `json:"evidence"`
}

// Labels native proof carried into the replacement's fresh value evidence.
type PrecompileCommitmentSource struct {
	PlanHash                 string `json:"plan_hash"`
	Probe                    string `json:"probe"`
	EvidenceHash             string `json:"evidence_hash"`
	WritePostconditionHash   string `json:"write_postcondition_hash"`
	RestorePostconditionHash string `json:"restore_postcondition_hash"`
}

// Keeps the completed native actions under their exact original intents.
func precompileNativeAction(actionId string) bool {
	return actionId == "precompile.commitment-write" || actionId == "precompile.commitment-restore"
}

// Resolves the current probe for execution and companion CREATE collision checks.
func approvedPrecompileProbe(plan *SetupPlan) common.Address {
	if plan.PrecompileProbeSuccessor != nil {
		return common.HexToAddress(plan.PrecompileProbeSuccessor.Probe)
	}
	return effectivePrecompileProbe(plan.Deployment, plan.CoordinatorUpgradeBaseline)
}

// Resolves the public observation's current probe while preserving old baselines.
func contractViewPrecompileProbe(view *ContractView) common.Address {
	if view == nil || view.Deployment == nil {
		return common.Address{}
	}
	if view.PrecompileProbeSuccessor != nil {
		return common.HexToAddress(view.PrecompileProbeSuccessor.Probe)
	}
	baseline := CoordinatorUpgradeBaseline{}
	if view.CoordinatorUpgradeBaseline != nil {
		baseline = *view.CoordinatorUpgradeBaseline
	}
	return effectivePrecompileProbe(*view.Deployment, baseline)
}

// Published successor custody is bound by the original signed repair request.
func publicPrecompileProbePlan(public *PublicDeploymentManifest, identities finalPublicIdentities) (*SetupPlan, error) {
	plan := &SetupPlan{
		PlanHash: public.PlanHash, DeploymentID: public.DeploymentID, ConfigHash: public.ConfigHash, PolicyHash: public.PolicyHash,
		ChainID: public.ChainID, GenesisHash: public.GenesisHash, Netuid: public.Netuid, Deployment: *public.Contracts,
		Roles:              PublicRoles{Deployer: identities.EVM["deployer"], Owner: identities.EVM["testnet-owner"]},
		CoordinatorUpgrade: public.CoordinatorUpgrade, CoordinatorUpgradeBaseline: *public.CoordinatorUpgradeBaseline,
		CoordinatorRepairCarry: public.CoordinatorRepairCarry, PrecompileProbeSuccessor: public.PrecompileProbeSuccessor,
	}
	if plan.CoordinatorRepairCarry != nil {
		plan.PriorPlanHashes = append(plan.PriorPlanHashes, plan.CoordinatorRepairCarry.Request.Request.PlanHash)
		if err := validateCoordinatorRepairCarryPlan(plan); err != nil {
			return nil, err
		}
	}
	if successor := plan.PrecompileProbeSuccessor; successor != nil {
		plan.PriorPlanHashes = append(plan.PriorPlanHashes, successor.SourcePlanHash, successor.Write.PlanHash, successor.Restore.PlanHash)
		if err := validatePrecompileProbeSuccessor(plan); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// Refuses partially attempted value phases as well as completed probe funding.
func precompileProbeHasOnlyNativeEvidence(evidence *PrecompileConformanceEvidence) bool {
	return evidence != nil && evidence.CommitmentSource == nil && !evidence.Complete &&
		evidence.Battery == (PrecompileBatteryEvidence{}) && evidence.Seed == (PrecompileValueStep{}) &&
		evidence.Forward == (PrecompileMoveStep{}) && evidence.Back == (PrecompileMoveStep{}) &&
		evidence.Snapshot == (PrecompileSnapshotStep{}) && evidence.Dividend == (PrecompileDividendStep{}) && evidence.Transfer == (PrecompileTransferStep{})
}

// Validates the descriptor's bounded identity before archives or chain reads.
func validatePrecompileProbeSuccessor(plan *SetupPlan) error {
	if plan == nil {
		return errors.New("precompile probe successor plan is absent")
	}
	successor := plan.PrecompileProbeSuccessor
	if successor == nil {
		return nil
	}
	baseline := plan.CoordinatorUpgradeBaseline
	if successor.Schema != "urnetwork-precompile-probe-successor-v1" || plan.CoordinatorRepairCarry == nil || baseline.Schema != "urnetwork-coordinator-upgrade-baseline-v4" || successor.SourcePlanHash == plan.PlanHash || !plan.allowedPlanHashes()[successor.SourcePlanHash] {
		return errors.New("precompile probe successor lacks its exact approved predecessor")
	}
	if successor.RetiredProbe != baseline.ReplacementPrecompileProbe || successor.RetiredRuntimeHash != baseline.ReplacementPrecompileProbeHash || !common.IsHexAddress(successor.Probe) || !common.IsHexAddress(plan.Roles.Deployer) || successor.DeployerNonce == ^uint64(0) || plan.CoordinatorUpgrade.DeployerNonce == ^uint64(0) || successor.DeployerNonce != plan.CoordinatorUpgrade.DeployerNonce+1 || common.HexToAddress(successor.Probe) != crypto.CreateAddress(common.HexToAddress(plan.Roles.Deployer), successor.DeployerNonce) || strings.EqualFold(successor.Probe, successor.RetiredProbe) || successor.RuntimeHash == successor.RetiredRuntimeHash {
		return errors.New("precompile probe successor changes its deterministic CREATE or retired identity")
	}
	for _, value := range []string{successor.SourcePlanHash, successor.RuntimeHash, successor.CreationHash, successor.RetiredRuntimeHash, successor.JournalHash, successor.FinalizedHead.Hash} {
		if !validCanonicalHashHex(value) {
			return errors.New("precompile probe successor has a malformed approval hash")
		}
	}
	if successor.FinalizedHead.Number == 0 || successor.JournalSequence == 0 || !precompileProbeHasOnlyNativeEvidence(&successor.Evidence) {
		return errors.New("precompile probe successor has no empty, unfunded admission boundary")
	}
	evidence := successor.Evidence
	want := evidence.EvidenceHash
	evidence.EvidenceHash = ""
	hash, err := canonicalHashHex(&evidence)
	if err != nil || hash != want || !evidence.Commitment.Restored || evidence.Commitment.CanonicalGeneration != precompileCanonicalFleetGeneration || evidence.Commitment.RestoreCommitmentBlock <= evidence.Commitment.WriteCommitmentBlock || evidence.ProbeAddress != successor.RetiredProbe || evidence.DeploymentID != plan.DeploymentID || evidence.ChainID != plan.ChainID || evidence.GenesisHash != plan.GenesisHash || evidence.Netuid != plan.Netuid || !strings.EqualFold(evidence.Owner, plan.Roles.Deployer) {
		return errors.New("precompile probe successor changed the original native evidence")
	}
	for index, entry := range []JournalEntry{successor.Write, successor.Restore} {
		actionId := []string{"precompile.commitment-write", "precompile.commitment-restore"}[index]
		if entry.Stage != StageVerified || entry.ActionID != actionId || entry.DeploymentID != plan.DeploymentID || !plan.allowedPlanHashes()[entry.PlanHash] || !validCanonicalHashHex(entry.PostconditionHash) || entry.Sequence == 0 || entry.Sequence > successor.JournalSequence {
			return errors.New("precompile probe successor lacks exact original native receipts")
		}
	}
	if successor.Write.Sequence >= successor.Restore.Sequence {
		return errors.New("precompile probe successor reverses the native drill")
	}
	return nil
}

// Checks each phase's identity, including the two deliberate source exceptions.
func validatePrecompileProbeSuccessorActions(plan *SetupPlan) error {
	if err := validatePrecompileProbeSuccessor(plan); err != nil || plan.PrecompileProbeSuccessor == nil {
		return err
	}
	successor := plan.PrecompileProbeSuccessor
	count := 0
	for _, action := range plan.Actions {
		if !strings.HasPrefix(action.ID, "precompile.") {
			continue
		}
		count++
		probe, runtimeHash := successor.Probe, successor.RuntimeHash
		if precompileNativeAction(action.ID) {
			probe, runtimeHash = successor.RetiredProbe, successor.RetiredRuntimeHash
			entry := successor.Write
			if action.ID == "precompile.commitment-restore" {
				entry = successor.Restore
			}
			if action.IntentHash != entry.IntentHash || len(action.AcceptedPriorIntentHashes) != 0 {
				return errors.New("precompile probe successor rewrites a completed native intent")
			}
		}
		if action.Parameters[precompileProbeAddressParameter] != probe || action.Parameters[precompileProbeRuntimeParameter] != runtimeHash {
			return fmt.Errorf("precompile probe successor action %s has the wrong probe identity", action.ID)
		}
		switch action.ID {
		case "precompile.probe-deploy":
			if action.Kind != "evm-transaction" || action.Parameters["expected_nonce"] != strconv.FormatUint(successor.DeployerNonce, 10) || !strings.EqualFold(action.Parameters["expected_signer"], plan.Roles.Deployer) || action.Parameters["expected_transaction_to"] != "create" || action.Parameters["expected_created_address"] != successor.Probe || action.Parameters["expected_data_keccak256"] != successor.CreationHash || action.Parameters["expected_value_wei"] != "0" {
				return errors.New("precompile probe successor CREATE envelope differs")
			}
		case "precompile.commitment-write", "precompile.commitment-restore", "precompile.read-battery", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out":
		default:
			return errors.New("precompile probe successor contains an unsupported conformance phase")
		}
	}
	if count != 10 {
		return errors.New("precompile probe successor lost a conformance phase")
	}
	return nil
}

// The failed battery must be the only attempted probe-dependent phase.
func failedPrecompileProbeNativeEntries(plan *SetupPlan, entries []JournalEntry) (JournalEntry, JournalEntry, error) {
	var write, restore JournalEntry
	failedBattery := false
	for _, entry := range entries {
		if !plan.allowedPlanHashes()[entry.PlanHash] || !strings.HasPrefix(entry.ActionID, "precompile.") || entry.ActionID == "precompile.probe-deploy" {
			continue
		}
		action, err := exactPlanActionByID(plan, entry.ActionID)
		if err != nil || entry.IntentHash != action.IntentHash {
			return write, restore, errors.New("failed precompile probe history changed an original phase intent")
		}
		if precompileNativeAction(entry.ActionID) {
			if entry.Stage == StageVerified {
				if entry.ActionID == "precompile.commitment-write" {
					write = entry
				} else {
					restore = entry
				}
			}
			continue
		}
		if entry.ActionID != "precompile.read-battery" || entry.TransactionHash != "" || entry.Stage != StageIntent && entry.Stage != StageFailed {
			return write, restore, fmt.Errorf("precompile probe has attempted a value or successful battery phase: %s/%s", entry.ActionID, entry.Stage)
		}
		failedBattery = failedBattery || entry.Stage == StageFailed && entry.Sequence > restore.Sequence && restore.Sequence != 0
	}
	if write.Sequence == 0 || restore.Sequence <= write.Sequence || !failedBattery || !exactVerifiedPlanAction(plan, entries, "precompile.probe-deploy") {
		return write, restore, errors.New("failed precompile probe lacks completed native drill and failed battery")
	}
	return write, restore, nil
}

// Projects the original native write before the later restore fields appeared.
func precompileNativeHistoricalObservation(action Action, evidence *PrecompileConformanceEvidence, record *ActionPostcondition) (map[string]any, error) {
	if action.ID == "precompile.commitment-restore" {
		return precompileRestoreHistoricalObservation(action, evidence, record)
	}
	if action.ID != "precompile.commitment-write" || evidence == nil || record == nil {
		return nil, errors.New("precompile native phase observation is unavailable")
	}
	snapshot := *evidence
	if record.Observed["evidence_hash"] != evidence.EvidenceHash {
		snapshot.Commitment.RestoreTransactionHash = ""
		snapshot.Commitment.RestoreFinalizedHead = ChainHead{}
		snapshot.Commitment.RestoreCommitmentBlock = 0
		snapshot.Commitment.Restored = false
		snapshot.EvidenceHash = ""
		var err error
		snapshot.EvidenceHash, err = canonicalHashHex(&snapshot)
		if err != nil {
			return nil, err
		}
	}
	state := map[string]any{"kind": action.Kind, "target": action.Target, "probe": snapshot.ProbeAddress, "evidence_hash": snapshot.EvidenceHash, "complete": snapshot.Complete, "canonical_chain_evidence": true}
	if err := observedPostconditionMatches(record.Observed, state); err != nil {
		return nil, err
	}
	return state, nil
}

// Authenticates the frozen admission prefix and both original persisted maps.
func readPrecompileProbeSuccessorSource(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry) (*SetupPlan, error) {
	if err := validatePrecompileProbeSuccessor(plan); err != nil || plan.PrecompileProbeSuccessor == nil {
		return nil, errors.Join(errors.New("precompile probe successor source is unavailable"), err)
	}
	successor := plan.PrecompileProbeSuccessor
	source, err := readValidatorEvidenceHistoricalPlan(stateDir, successor.SourcePlanHash)
	if err != nil {
		return nil, err
	}
	if err := validatePrecompileProbeSuccessorSource(cfg, stateDir, plan, source, entries); err != nil {
		return nil, err
	}
	return source, nil
}

// Consumes an authenticated archive and checks its original receipt files.
func validatePrecompileProbeSuccessorSource(cfg *ResolvedConfig, stateDir string, plan, source *SetupPlan, entries []JournalEntry) error {
	if err := validatePrecompileProbeSuccessor(plan); err != nil || plan.PrecompileProbeSuccessor == nil || source == nil {
		return errors.Join(errors.New("precompile probe successor source is absent"), err)
	}
	successor := plan.PrecompileProbeSuccessor
	baseline := source.CoordinatorUpgradeBaseline
	baseline.ReleaseDeploymentHash = plan.CoordinatorUpgradeBaseline.ReleaseDeploymentHash
	if source.PrecompileProbeSuccessor != nil || source.PlanHash != successor.SourcePlanHash || !contractDeploymentAddressesEqual(source.Deployment, plan.Deployment) || !contractDeploymentRuntimeHashesCompatible(source.Deployment, plan.Deployment) || source.ConfigHash != successor.Evidence.ConfigHash || source.PolicyHash != successor.Evidence.PolicyHash || source.ChainID != successor.Evidence.ChainID || source.GenesisHash != successor.Evidence.GenesisHash || source.Netuid != successor.Evidence.Netuid || !strings.EqualFold(source.Roles.Deployer, successor.Evidence.Owner) || !reflect.DeepEqual(source.Roles, plan.Roles) || source.CoordinatorUpgrade != plan.CoordinatorUpgrade || baseline != plan.CoordinatorUpgradeBaseline || !reflect.DeepEqual(source.CoordinatorRepairCarry, plan.CoordinatorRepairCarry) {
		return errors.New("precompile probe successor changes original approval or retained custody")
	}
	var prefix []JournalEntry
	for _, entry := range entries {
		if entry.Sequence <= successor.JournalSequence {
			prefix = append(prefix, entry)
		}
	}
	if len(prefix) == 0 || prefix[len(prefix)-1].Sequence != successor.JournalSequence || prefix[len(prefix)-1].EntryHash != successor.JournalHash {
		return errors.New("precompile probe successor changed its original journal prefix")
	}
	write, restore, err := failedPrecompileProbeNativeEntries(source, prefix)
	if err != nil || !reflect.DeepEqual(write, successor.Write) || !reflect.DeepEqual(restore, successor.Restore) {
		return errors.Join(errors.New("precompile probe successor changed original native receipt references"), err)
	}
	reader := &Executor{cfg: cfg, stateDir: stateDir, plan: source}
	for _, entry := range []JournalEntry{write, restore} {
		action, err := exactPlanActionByID(source, entry.ActionID)
		if err != nil {
			return err
		}
		current, err := exactPlanActionByID(plan, entry.ActionID)
		if err != nil || !reflect.DeepEqual(action, current) {
			return errors.New("precompile probe successor rewrote an original native action")
		}
		record, err := reader.readPersistedPostcondition(entry)
		if err != nil {
			return err
		}
		if _, err := precompileNativeHistoricalObservation(action, &successor.Evidence, record); err != nil {
			return fmt.Errorf("precompile probe successor original phase: %w", err)
		}
		independent := *record
		independent.Observed = record.IndependentObserved
		if _, err := precompileNativeHistoricalObservation(action, &successor.Evidence, &independent); err != nil {
			return fmt.Errorf("precompile probe successor independent original phase: %w", err)
		}
	}
	return nil
}

// Validates the new artifact independently from the old probe's baseline.
func bindPrecompileProbeSuccessorPayloads(payloads *DeploymentPayloads, successor *PrecompileProbeSuccessor) error {
	if payloads == nil || successor == nil {
		return errors.New("precompile probe successor payload context is absent")
	}
	if err := configurePrecompileProbeNonce(payloads, successor.DeployerNonce); err != nil {
		return err
	}
	if payloads.PrecompileProbeAddress.Hex() != successor.Probe || crypto.Keccak256Hash(payloads.PrecompileProbe).Hex() != successor.CreationHash || crypto.Keccak256Hash(payloads.ExpectedRuntime[payloads.PrecompileProbeAddress]).Hex() != successor.RuntimeHash {
		return errors.New("precompile probe successor differs from locked creation or runtime bytes")
	}
	return nil
}

// Renders only the replacement and unstarted phases; native actions stay exact.
func rebindPrecompileProbeSuccessor(plan, source *SetupPlan, payloads *DeploymentPayloads, successor *PrecompileProbeSuccessor) error {
	if err := bindPrecompileProbeSuccessorPayloads(payloads, successor); err != nil {
		return err
	}
	copy := *successor
	plan.PrecompileProbeSuccessor = &copy
	for index, action := range plan.Actions {
		if !strings.HasPrefix(action.ID, "precompile.") {
			continue
		}
		if precompileNativeAction(action.ID) {
			original, err := exactPlanActionByID(source, action.ID)
			if err != nil {
				return err
			}
			plan.Actions[index] = original
			continue
		}
		action.Parameters = cloneStrings(action.Parameters)
		action.Parameters[precompileProbeAddressParameter] = successor.Probe
		action.Parameters[precompileProbeRuntimeParameter] = successor.RuntimeHash
		if action.ID == "precompile.probe-deploy" {
			envelope, ok := deploymentActionEnvelope(payloads, action.ID, plan.RegistrationBurnLimitRao)
			if !ok {
				return errors.New("precompile probe successor CREATE envelope is absent")
			}
			for key, value := range envelope {
				action.Parameters[key] = value
			}
		}
		var err error
		action.IntentHash, err = actionIntentHash(action)
		if err != nil {
			return err
		}
		plan.Actions[index] = action
	}
	return validatePrecompileProbeSuccessorActions(plan)
}

// Accepts only the contiguous approved CREATE and value-call prefix.
func precompileProbeSuccessorNonce(plan *SetupPlan, entries []JournalEntry, nonce uint64) (bool, error) {
	if plan == nil || plan.PrecompileProbeSuccessor == nil {
		return false, nil
	}
	_, err := precompileProbeSuccessorPrefix(plan, entries, nonce)
	return err == nil, err
}

// Rechecks retired custody and either the empty or exact completed new CREATE.
func verifyPrecompileProbeSuccessorAt(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, client *ethclient.Client, head ChainHead, nonce uint64) error {
	if _, err := readPrecompileProbeSuccessorSource(cfg, stateDir, plan, entries); err != nil {
		return err
	}
	if allowed, err := precompileProbeSuccessorNonce(plan, entries, nonce); err != nil || !allowed {
		return errors.Join(errors.New("precompile probe successor nonce is outside approval"), err)
	}
	successor := plan.PrecompileProbeSuccessor
	block := new(big.Int).SetUint64(head.Number)
	if err := verifyEVMCheckpoint(ctx, client, head, successor.FinalizedHead); err != nil {
		return err
	}
	admissionBlock := new(big.Int).SetUint64(successor.FinalizedHead.Number)
	admissionNonce, err := client.NonceAt(ctx, common.HexToAddress(plan.Roles.Deployer), admissionBlock)
	if err != nil || admissionNonce != successor.DeployerNonce {
		return stateMismatchError(err, "precompile probe successor changed its admitted deployer nonce")
	}
	admissionCode, err := client.CodeAt(ctx, common.HexToAddress(successor.Probe), admissionBlock)
	if err != nil || len(admissionCode) != 0 {
		return stateMismatchError(err, "precompile probe successor was occupied at admission")
	}
	stakeReader := &Executor{cfg: cfg, deployer: &EvmTxManager{client: client}}
	if err := verifyPrecompileProbeSuccessorUnfunded(ctx, successor, head, nonce, client.BalanceAt, stakeReader.readStakeAt); err != nil {
		return err
	}
	for _, check := range []struct {
		address common.Address
		hash    string
	}{
		{address: common.HexToAddress(successor.RetiredProbe), hash: successor.RetiredRuntimeHash},
		{address: common.HexToAddress(successor.Probe), hash: successor.RuntimeHash},
	} {
		code, err := client.CodeAt(ctx, check.address, block)
		if err != nil {
			return err
		}
		if check.address == common.HexToAddress(successor.Probe) && nonce == successor.DeployerNonce {
			if len(code) != 0 {
				return errors.New("precompile probe successor CREATE address is occupied")
			}
		} else if len(code) == 0 || crypto.Keccak256Hash(code).Hex() != check.hash {
			return errors.New("precompile probe successor runtime differs from exact approval")
		}
	}
	if nonce > successor.DeployerNonce {
		action, err := exactPlanActionByID(plan, "precompile.probe-deploy")
		if err != nil {
			return err
		}
		owner := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, journal: &Journal{entries: entries}}
		if verified, ok := owner.verifiedActionEntry(action); ok {
			if _, err := owner.readPersistedPostcondition(verified); err != nil {
				return err
			}
		}
		receipt, err := finalizedContractCreationReceipt(ctx, ethEVMReceiptFinalityReader{client: client}, head, plan.DeploymentID, action, common.HexToAddress(successor.Probe), entries, plan.allowedPlanHashes())
		if err != nil || receipt == nil {
			return errors.Join(errors.New("precompile probe successor has no exact canonical CREATE"), err)
		}
		if err := verifyPrecompileProbeSuccessorCalls(ctx, cfg, stateDir, plan, entries, ethEVMReceiptFinalityReader{client: client}, head, nonce); err != nil {
			return err
		}
	}
	return nil
}

// Admission rechecks current custody as well as the signed historical anchor;
// later exact create/call recovery keeps its existing historical custody scope.
func verifyPrecompileProbeSuccessorUnfunded(ctx context.Context, successor *PrecompileProbeSuccessor, current ChainHead, nonce uint64, balanceAt func(context.Context, common.Address, *big.Int) (*big.Int, error), stakeAt func(context.Context, uint64, [32]byte, [32]byte) (uint64, error)) error {
	checkpoints := []ChainHead{successor.FinalizedHead}
	if nonce == successor.DeployerNonce && current != successor.FinalizedHead {
		checkpoints = append(checkpoints, current)
	}
	retired := common.HexToAddress(successor.RetiredProbe)
	for _, checkpoint := range checkpoints {
		balance, err := balanceAt(ctx, retired, new(big.Int).SetUint64(checkpoint.Number))
		if err != nil || balance == nil || balance.Sign() != 0 {
			return stateMismatchError(err, "failed precompile probe retained an EVM balance at custody checkpoint %d", checkpoint.Number)
		}
		for _, encoded := range []string{successor.Evidence.SampleHotkey, successor.Evidence.MoveHotkey} {
			hotkey, err := decodeHex32("failed probe stake hotkey", encoded)
			if err != nil {
				return err
			}
			stake, err := stakeAt(ctx, checkpoint.Number, hotkey, ss58Mirror(retired))
			if err != nil || stake != 0 {
				return stateMismatchError(err, "failed precompile probe retained alpha stake at custody checkpoint %d", checkpoint.Number)
			}
		}
	}
	return nil
}

// The source's signed repair checkpoint is stable between preview and apply;
// the fresh head only establishes that the checkpoint is already finalized.
func newPrecompileProbeSuccessor(prior *SetupPlan, built *DeploymentPayloads, entries []JournalEntry, evidence *PrecompileConformanceEvidence, head ChainHead) (*PrecompileProbeSuccessor, error) {
	if prior == nil || prior.CoordinatorRepairCarry == nil || prior.PrecompileProbeSuccessor != nil || built == nil || len(entries) == 0 || !precompileProbeHasOnlyNativeEvidence(evidence) {
		return nil, errors.New("failed precompile probe successor source is incomplete")
	}
	if err := validateCoordinatorRepairCarryPlan(prior); err != nil {
		return nil, err
	}
	anchor := prior.CoordinatorRepairCarry.Result.Result.ObservedHead
	if anchor.Number == 0 || !validCanonicalHashHex(anchor.Hash) || head.Number < anchor.Number || head.Number == anchor.Number && head.Hash != anchor.Hash {
		return nil, errors.New("failed precompile probe successor has no finalized signed repair checkpoint")
	}
	if prior.CoordinatorUpgrade.DeployerNonce == ^uint64(0) || built.PrecompileProbeNonce != prior.CoordinatorUpgrade.DeployerNonce+1 {
		return nil, errors.New("failed precompile probe successor changed its signed repair nonce boundary")
	}
	write, restore, err := failedPrecompileProbeNativeEntries(prior, entries)
	if err != nil {
		return nil, err
	}
	last := entries[len(entries)-1]
	return &PrecompileProbeSuccessor{
		Schema: "urnetwork-precompile-probe-successor-v1", SourcePlanHash: prior.PlanHash,
		RetiredProbe: prior.CoordinatorUpgradeBaseline.ReplacementPrecompileProbe, RetiredRuntimeHash: prior.CoordinatorUpgradeBaseline.ReplacementPrecompileProbeHash,
		Probe: built.PrecompileProbeAddress.Hex(), DeployerNonce: built.PrecompileProbeNonce,
		RuntimeHash: crypto.Keccak256Hash(built.ExpectedRuntime[built.PrecompileProbeAddress]).Hex(), CreationHash: crypto.Keccak256Hash(built.PrecompileProbe).Hex(),
		FinalizedHead: anchor, JournalSequence: last.Sequence, JournalHash: last.EntryHash, Write: write, Restore: restore, Evidence: *evidence,
	}, nil
}

// Observes only the failed-probe successor of a completed, retained repair.
func observePrecompileProbeSuccessor(ctx context.Context, cfg *ResolvedConfig, stateDir string, prior *SetupPlan, current *SetupFacts, entries []JournalEntry, built *DeploymentPayloads) (*coordinatorUpgradeMigration, bool, error) {
	if prior == nil || built == nil || current == nil {
		return nil, false, errors.New("precompile probe successor observation is absent")
	}
	if prior.PrecompileProbeSuccessor == nil && prior.CoordinatorUpgradeBaseline.Schema != "urnetwork-coordinator-upgrade-baseline-v4" {
		return nil, false, nil
	}
	_, _, changed, err := comparePrecompileProbeRelease(nil, prior.CoordinatorUpgradeBaseline.PrecompileProbeExecutableHash, built.ExpectedRuntime[built.PrecompileProbeAddress])
	if err != nil {
		return nil, false, err
	}
	if prior.PrecompileProbeSuccessor == nil && !changed {
		return nil, false, nil
	}
	if prior.coordinatorRepairObserved == nil || prior.CoordinatorRepairCarry == nil || prior.CoordinatorUpgradeBaseline.Schema != "urnetwork-coordinator-upgrade-baseline-v4" {
		return nil, true, errors.New("failed precompile probe replacement requires the exact completed repair baseline")
	}
	if err := configureCoordinatorUpgradeNonce(built, prior.CoordinatorUpgrade.DeployerNonce); err != nil {
		return nil, true, err
	}
	if err := bindCoordinatorRepairCarryPayloads(built, prior.coordinatorRepairObserved); err != nil {
		return nil, true, err
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, true, err
	}
	defer client.Close()
	head, err := finalizedEVMHead(ctx, client)
	if err != nil || head.Number < current.EVMFinalizedBlock {
		return nil, true, stateMismatchError(err, "precompile probe successor finalized observation predates setup facts")
	}
	nonce, err := client.NonceAt(ctx, built.Deployer, new(big.Int).SetUint64(head.Number))
	if err != nil || nonce != current.DeployerNonce {
		return nil, true, stateMismatchError(err, "precompile probe successor deployer nonce changed")
	}
	pending, err := client.PendingNonceAt(ctx, built.Deployer)
	if err != nil || pending != nonce {
		return nil, true, stateMismatchError(err, "precompile probe successor deployer has pending writes")
	}
	successor := prior.PrecompileProbeSuccessor
	if successor == nil {
		if nonce != prior.CoordinatorUpgrade.DeployerNonce+1 || len(entries) == 0 {
			return nil, true, errors.New("failed precompile probe replacement changed its next CREATE boundary")
		}
		evidence, err := loadPrecompileEvidence(stateDir)
		if err != nil || !precompileProbeHasOnlyNativeEvidence(evidence) {
			return nil, true, errors.Join(errors.New("failed precompile probe has value or battery evidence"), err)
		}
		if err := configurePrecompileProbeNonce(built, nonce); err != nil {
			return nil, true, err
		}
		successor, err = newPrecompileProbeSuccessor(prior, built, entries, evidence, head)
		if err != nil {
			return nil, true, err
		}
	}
	if err := bindPrecompileProbeSuccessorPayloads(built, successor); err != nil {
		return nil, true, err
	}
	candidate := *prior
	candidate.PlanHash = ""
	candidate.PriorPlanHashes = append(append([]string(nil), prior.PriorPlanHashes...), prior.PlanHash)
	candidate.Actions = append([]Action(nil), prior.Actions...)
	if err := rebindPrecompileProbeSuccessor(&candidate, prior, built, successor); err != nil {
		return nil, true, err
	}
	if err := verifyPrecompileProbeSuccessorAt(ctx, cfg, stateDir, &candidate, entries, client, head, nonce); err != nil {
		return nil, true, err
	}
	baseline := prior.CoordinatorUpgradeBaseline
	baseline.ReleaseDeploymentHash, err = contractDeploymentIdentityHash(built.Manifest)
	if err != nil {
		return nil, true, err
	}
	baselinePayloads := coordinatorRepairBaselinePayloads(built, prior.CoordinatorRepairCarry)
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, prior.Deployment, built.Manifest, baselinePayloads.CoordinatorUpgrade); err != nil {
		return nil, true, err
	}
	if err := validateCoordinatorUpgradePayloadBaselineWithProbe(baseline, prior.Deployment, baselinePayloads, successor); err != nil {
		return nil, true, err
	}
	return &coordinatorUpgradeMigration{Deployment: prior.Deployment, Baseline: baseline, Upgrade: built.CoordinatorUpgrade, Repair: prior.coordinatorRepairObserved, ProbeSuccessor: successor}, true, nil
}

// Retains an immutable native proof label on the replacement's fresh evidence.
func precompileProbeCommitmentSource(successor *PrecompileProbeSuccessor) *PrecompileCommitmentSource {
	return &PrecompileCommitmentSource{PlanHash: successor.SourcePlanHash, Probe: successor.RetiredProbe, EvidenceHash: successor.Evidence.EvidenceHash, WritePostconditionHash: successor.Write.PostconditionHash, RestorePostconditionHash: successor.Restore.PostconditionHash}
}

// Starts a fresh probe evidence generation without issuing either native write.
func precompileProbeSuccessorEvidence(plan *SetupPlan, identity, evidence *PrecompileConformanceEvidence) (*PrecompileConformanceEvidence, error) {
	if plan == nil || plan.PrecompileProbeSuccessor == nil {
		return evidence, nil
	}
	if err := validatePrecompileProbeSuccessor(plan); err != nil {
		return nil, err
	}
	successor := plan.PrecompileProbeSuccessor
	if evidence == nil || identity == nil || identity.ProbeAddress != successor.Probe {
		return nil, errors.New("precompile probe successor evidence generation is absent")
	}
	if evidence.ProbeAddress == successor.RetiredProbe {
		if !reflect.DeepEqual(*evidence, successor.Evidence) {
			return nil, errors.New("precompile probe successor changed retired evidence before migration")
		}
		fresh := *identity
		fresh.Commitment = successor.Evidence.Commitment
		fresh.CommitmentSource = precompileProbeCommitmentSource(successor)
		return &fresh, nil
	}
	if evidence.ProbeAddress != successor.Probe || evidence.Commitment != successor.Evidence.Commitment || !reflect.DeepEqual(evidence.CommitmentSource, precompileProbeCommitmentSource(successor)) {
		return nil, errors.New("precompile probe successor changed the original native proof source")
	}
	return evidence, nil
}

// An admitted successor can replay the original proof but can never resend it.
func (self *Executor) precompileProbeNativeSource(action Action, verified JournalEntry, record *ActionPostcondition) (*Executor, bool, error) {
	if self == nil || self.plan == nil || self.plan.PrecompileProbeSuccessor == nil || !precompileNativeAction(action.ID) {
		return nil, false, nil
	}
	if self.journal == nil || self.payloads == nil {
		return nil, true, errors.New("precompile probe successor historical owner is absent")
	}
	if _, err := readPrecompileProbeSuccessorSource(self.cfg, self.stateDir, self.plan, self.journal.Entries()); err != nil {
		return nil, true, err
	}
	successor := self.plan.PrecompileProbeSuccessor
	want := successor.Write
	if action.ID == "precompile.commitment-restore" {
		want = successor.Restore
	}
	if !reflect.DeepEqual(verified, want) || record == nil || record.PlanHash != want.PlanHash || record.ActionID != want.ActionID || record.IntentHash != want.IntentHash {
		return nil, true, errors.New("precompile probe successor changed original native receipt identity")
	}
	source := *self
	if action.ID == "precompile.commitment-restore" {
		renewed, handled, err := self.fleetRenewalHistoricalSource(action, verified, record)
		if err != nil || !handled {
			return nil, true, errors.Join(errors.New("precompile probe successor requires authenticated completed renewal"), err)
		}
		source = *renewed
	} else {
		original, err := readValidatorEvidenceHistoricalPlan(self.stateDir, verified.PlanHash)
		if err != nil {
			return nil, true, err
		}
		source.plan = original
	}
	payloads := *self.payloads
	payloads.PrecompileProbeAddress = common.HexToAddress(successor.RetiredProbe)
	source.payloads = &payloads
	evidence := successor.Evidence
	source.precompileHistoryEvidence = &evidence
	return &source, true, nil
}

// Reads only the original phase after the successor constructor authenticated it.
func (self *Executor) historicalPrecompileEvidence() (*PrecompileConformanceEvidence, error) {
	if self.precompileHistoryEvidence != nil {
		copy := *self.precompileHistoryEvidence
		return &copy, nil
	}
	return loadPrecompileEvidence(self.stateDir)
}

// Replays the exact original write observation and canonical native transaction.
func (self *Executor) verifyHistoricalPrecompileWrite(ctx context.Context, action Action, record *ActionPostcondition) (map[string]any, error) {
	if self.precompileHistoryEvidence == nil || self.payloads == nil || self.plan == nil || record == nil || record.PlanHash != self.plan.PlanHash || record.ActionID != action.ID || record.IntentHash != action.IntentHash {
		return nil, errors.New("precompile native write lacks authenticated successor source")
	}
	evidence := self.precompileHistoryEvidence
	if err := validatePrecompileEvidenceIdentity(self.cfg, self.payloads.PrecompileProbeAddress, evidence); err != nil {
		return nil, err
	}
	state, err := precompileNativeHistoricalObservation(action, evidence, record)
	if err != nil {
		return nil, err
	}
	if err := self.verifySubstrateTransactionEvidence(ctx, evidence.Commitment.WriteFinalizedHead, evidence.Commitment.WriteTransactionHash); err != nil {
		return nil, err
	}
	return state, nil
}
