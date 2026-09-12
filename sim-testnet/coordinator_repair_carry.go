package main

// Completed corrective writes retain their original approval and receipts.
// A strict revision can adopt those exact effects only after independently
// replaying their signed transactions, canonical inclusion and current state.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const coordinatorRepairCarryActionID = "coordinator.repair-carry"

type CoordinatorRepairCarry struct {
	Schema string `json:"schema"`
	Request signedCoordinatorRepairRequest `json:"request"`
	Result signedCoordinatorRepairResult `json:"result"`
}

type coordinatorRepairCarryObservation struct {
	reference CoordinatorRepairCarry
	source *SetupPlan
	creation, runtime []byte
	transactions [2]*types.Transaction
	deployerNonce uint64
}

func coordinatorRepairOriginalAction(id string) bool {
	return id == "evm.coordinator-upgrade-implementation" || id == "evm.coordinator-upgrade-activate"
}

func coordinatorRepairHistoryRequired(plan *SetupPlan, entries []JournalEntry) bool {
	if plan.CoordinatorRepairCarry != nil { return true }
	for _, entry := range entries {
		if plan.allowedPlanHashes()[entry.PlanHash] && (entry.ActionID == "repair.coordinator-rounding.deploy" || entry.ActionID == "repair.coordinator-rounding.activate") { return true }
	}
	return false
}

// This pure check binds serialized approval. It never issues an observation;
// source archives, original journal and both live readers are still required.
func validateCoordinatorRepairCarryPlan(plan *SetupPlan) error {
	if plan == nil { return errors.New("coordinator repair plan is absent") }
	carry := plan.CoordinatorRepairCarry
	if carry == nil { return nil }
	r, result := carry.Request.Request, carry.Result.Result
	if carry.Schema != "urnetwork-coordinator-repair-carry-v1" || r.PlanHash == plan.PlanHash || !plan.allowedPlanHashes()[r.PlanHash] || r.ConfigHash != plan.ConfigHash || r.DeploymentID != plan.DeploymentID || r.Proxy != plan.Deployment.CoordinatorProxy || r.Vault != plan.Deployment.SettlementVault || r.Reserve != plan.Deployment.ReserveSink || r.Owner != common.HexToAddress(plan.Roles.Owner) || r.Deployer != common.HexToAddress(plan.Roles.Deployer) || plan.CoordinatorUpgrade != r.Upgrade {
		return errors.New("coordinator repair carry differs from its approved source or custody")
	}
	if r.Schema != "urnetwork-provisional-coordinator-repair-v1" || !r.Provisional || r.FinalAcceptance || !validCoordinatorRepairSHA256(r.ArtifactSHA256) || !validCoordinatorRepairSHA256(r.BudgetSHA256) || !validCanonicalHashHex(r.IdentityHash) || r.Upgrade.DeployerNonce <= r.OldUpgrade.DeployerNonce || r.Upgrade.DeployerNonce == ^uint64(0) {
		return errors.New("coordinator repair carry changes the original corrective scope")
	}
	if err := verifyCoordinatorRepairSignature(r, carry.Request.Hash, carry.Request.Signature, r.Owner); err != nil { return err }
	if err := validateCoordinatorRepairCarryResult(carry); err != nil { return err }
	if result.Deploy.PlanHash != r.PlanHash || result.Activate.PlanHash != r.PlanHash { return errors.New("coordinator repair carry crosses original action approvals") }
	return nil
}

func validateCoordinatorRepairCarryResult(carry *CoordinatorRepairCarry) error {
	if carry == nil { return errors.New("coordinator repair carry is absent") }
	r, result := carry.Request.Request, carry.Result.Result
	if result.Schema != "urnetwork-provisional-coordinator-repair-result-v1" || !result.Provisional || result.FinalAcceptance || result.RequestHash != carry.Request.Hash || result.IdentityHash != r.IdentityHash || result.ObservedHead.Number < result.Activate.BlockNumber || !validCanonicalHashHex(result.ObservedHead.Hash) || result.Deploy.BlockNumber > result.Activate.BlockNumber || result.Deploy.Sequence >= result.Activate.Sequence || result.Deploy.TransactionHash == result.Activate.TransactionHash {
		return errors.New("coordinator repair result is not the exact completed correction")
	}
	for i, entry := range []JournalEntry{result.Deploy, result.Activate} {
		action := []Action{r.Deploy, r.Activate}[i]
		if entry.Stage != StageFinalized || entry.DeploymentID != r.DeploymentID || entry.PlanHash != r.PlanHash || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash || entry.BlockNumber == 0 || !validCanonicalHashHex(entry.BlockHash) || !validCanonicalHashHex(entry.TransactionHash) { return errors.New("coordinator repair result lacks its exact finalized action") }
	}
	return verifyCoordinatorRepairSignature(result, carry.Result.Hash, carry.Result.Signature, r.Owner)
}

func readCoordinatorRepairCarry(stateDir string, plan *SetupPlan, entries []JournalEntry) (*coordinatorRepairCarryObservation, error) {
	requestRaw, err := readValidatorEvidenceHistoricalFile(stateDir, coordinatorRepairDirectory+"/request.json", 128<<10)
	if errors.Is(err, os.ErrNotExist) && !coordinatorRepairHistoryRequired(plan, entries) { return nil, nil }
	if err != nil { return nil, err }
	var reference CoordinatorRepairCarry
	reference.Schema = "urnetwork-coordinator-repair-carry-v1"
	if err := decodeExactCoordinatorRepairJSON(requestRaw, &reference.Request); err != nil { return nil, err }
	resultRaw, err := readValidatorEvidenceHistoricalFile(stateDir, coordinatorRepairDirectory+"/result.json", 128<<10)
	if err != nil { return nil, err }
	if err := decodeExactCoordinatorRepairJSON(resultRaw, &reference.Result); err != nil { return nil, err }
	r := reference.Request.Request
	if !plan.allowedPlanHashes()[r.PlanHash] { return nil, errors.New("coordinator repair source is outside approved lineage") }
	source, err := readValidatorEvidenceHistoricalPlan(stateDir, r.PlanHash)
	if err != nil { return nil, err }
	if source.CoordinatorRepairCarry != nil || !contractDeploymentAddressesEqual(source.Deployment, plan.Deployment) || !contractDeploymentRuntimeHashesCompatible(source.Deployment, plan.Deployment) || source.ConfigHash != plan.ConfigHash || !reflect.DeepEqual(source.Roles, plan.Roles) { return nil, errors.New("coordinator repair source changes retained custody") }
	baseline := plan.CoordinatorUpgradeBaseline
	baseline.ReleaseDeploymentHash = source.CoordinatorUpgradeBaseline.ReleaseDeploymentHash
	if baseline != source.CoordinatorUpgradeBaseline { return nil, errors.New("coordinator repair changed the original precompile baseline") }
	if err := validateCoordinatorRepairRequest(source, &reference.Request); err != nil { return nil, err }
	if err := validateCoordinatorRepairCarryResult(&reference); err != nil { return nil, err }
	if plan.CoordinatorRepairCarry != nil {
		if err := validateCoordinatorRepairCarryPlan(plan); err != nil { return nil, err }
		if !reflect.DeepEqual(*plan.CoordinatorRepairCarry, reference) { return nil, errors.New("coordinator repair source bytes differ from approved carry") }
	} else if plan.CoordinatorUpgrade != r.OldUpgrade { return nil, errors.New("coordinator repair original upgrade differs") }
	artifact, err := readValidatorEvidenceHistoricalFile(stateDir, coordinatorRepairDirectory+"/artifact.json", 16<<20)
	if err != nil { return nil, err }
	budgetRaw, err := readValidatorEvidenceHistoricalFile(stateDir, coordinatorRepairDirectory+"/budget.json", 128<<10)
	if err != nil { return nil, err }
	if bytesSHA256(artifact) != "sha256:"+r.ArtifactSHA256 || bytesSHA256(budgetRaw) != "sha256:"+r.BudgetSHA256 { return nil, errors.New("coordinator repair artifact or budget bytes changed") }
	var budget coordinatorRepairBudget
	if err := decodeExactCoordinatorRepairJSON(budgetRaw, &budget); err != nil { return nil, err }
	if !reflect.DeepEqual(budget, r.Budget) { return nil, errors.New("coordinator repair budget differs from signed request") }
	creation, runtime, err := coordinatorRepairArtifact(artifact, r.Upgrade.Implementation)
	if err != nil { return nil, err }
	if crypto.Keccak256Hash(runtime).Hex() != r.Upgrade.RuntimeCodeHash { return nil, errors.New("coordinator repair runtime differs from signed artifact") }
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil { return nil, err }
	activateData, err := parsed.Pack("upgradeToAndCall", r.Upgrade.Implementation, []byte{})
	if err != nil { return nil, err }
	activateNonce, err := strconv.ParseUint(r.Activate.Parameters["expected_nonce"], 10, 64)
	if err != nil { return nil, err }
	wantDeploy, err := coordinatorRepairAction(r.Deploy.ID, r.Deployer, r.Upgrade.DeployerNonce, nil, creation, 7_500_000)
	if err != nil { return nil, err }
	wantActivate, err := coordinatorRepairAction(r.Activate.ID, r.Owner, activateNonce, &r.Proxy, activateData, 500_000)
	if err != nil { return nil, err }
	if !reflect.DeepEqual(wantDeploy, r.Deploy) || !reflect.DeepEqual(wantActivate, r.Activate) { return nil, errors.New("coordinator repair signed actions differ from exact CREATE and activation bytes") }
	observation := &coordinatorRepairCarryObservation{reference: reference, source: source, creation: creation, runtime: runtime}
	anchor := uint64(0)
	for _, entry := range entries { if entry.EntryHash == r.Budget.JournalHash && entry.PlanHash == source.PlanHash { if anchor != 0 { return nil, errors.New("coordinator repair budget anchor is ambiguous") }; anchor = entry.Sequence } }
	if anchor == 0 { return nil, errors.New("coordinator repair budget lacks its original journal anchor") }
	for i, final := range []JournalEntry{reference.Result.Result.Deploy, reference.Result.Result.Activate} {
		action := []Action{r.Deploy, r.Activate}[i]
		var broadcast *JournalEntry
		finals := 0
		for _, entry := range entries {
			if entry.PlanHash != source.PlanHash || entry.ActionID != action.ID { continue }
			if entry.IntentHash != action.IntentHash || entry.DeploymentID != source.DeploymentID { return nil, errors.New("coordinator repair journal has competing action identity") }
			if entry.Stage == StageFinalized { if entry != final { return nil, errors.New("coordinator repair finalized journal entry changed") }; finals++ }
			if entry.Stage == StageBroadcast { if broadcast != nil || entry.TransactionHash != final.TransactionHash || entry.Sequence <= anchor || entry.Sequence >= final.Sequence { return nil, errors.New("coordinator repair original broadcast is ambiguous") }; copy := entry; broadcast = &copy }
		}
		if finals != 1 || broadcast == nil { return nil, errors.New("coordinator repair lacks one original broadcast and finalization") }
		ref := ValidatorEvidenceCarryReceipt{PlanHash: source.PlanHash, IntentHash: action.IntentHash, TransactionHash: final.TransactionHash, BlockNumber: final.BlockNumber, BlockHash: final.BlockHash}
		transaction, err := readValidatorEvidenceSourceTransaction(stateDir, source, action, ref, *broadcast)
		if err != nil { return nil, err }
		observation.transactions[i] = transaction
	}
	return observation, nil
}

func decodeExactCoordinatorRepairJSON(raw []byte, target any) error {
	if _, err := decodeOrderedJSONObject(raw); err != nil { return err }
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil { return err }
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) { return errors.New("coordinator repair JSON has trailing content") }
	return nil
}

func verifyCoordinatorRepairReceipt(ctx context.Context, client *ethclient.Client, head ChainHead, final JournalEntry, transaction *types.Transaction, created common.Address) error {
	if err := verifyEVMCheckpointFromReader(ctx, ethEVMReceiptFinalityReader{client: client}, head, ChainHead{Number: final.BlockNumber, Hash: final.BlockHash}); err != nil { return err }
	receipt, err := client.TransactionReceipt(ctx, transaction.Hash())
	if err != nil { return err }
	if !receiptMatchesEvidence(head, receipt, final.TransactionHash, final.BlockNumber, final.BlockHash) || receipt.ContractAddress != created { return errors.New("coordinator repair receipt differs from successful canonical inclusion") }
	actual, pending, err := client.TransactionByHash(ctx, transaction.Hash())
	if err != nil { return err }
	if actual == nil || pending { return errors.New("coordinator repair included transaction is absent or pending") }
	want, err := transaction.MarshalBinary()
	if err != nil { return err }
	got, err := actual.MarshalBinary()
	if err != nil || !bytes.Equal(want, got) { return errors.New("coordinator repair inclusion differs from signed bytes") }
	return nil
}

func authenticateCoordinatorRepairCarry(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, operational, independent *ethclient.Client) (*coordinatorRepairCarryObservation, error) {
	if ctx == nil || cfg == nil || plan == nil || operational == nil || independentRPCRequired(cfg) && independent == nil { return nil, errors.New("coordinator repair reader ownership is incomplete") }
	if err := ctx.Err(); err != nil { return nil, err }
	observation, err := readCoordinatorRepairCarry(stateDir, plan, entries)
	if err != nil || observation == nil { return observation, err }
	r, result := observation.reference.Request.Request, observation.reference.Result.Result
	if cfg.ChainID != plan.ChainID || cfg.ChainID != testnetChainID || cfg.Public == nil || cfg.Public.Chain.GenesisHash != plan.GenesisHash || plan.GenesisHash != testnetGenesis || cfg.ConfigHash != plan.ConfigHash { return nil, errors.New("coordinator repair configured strict domain differs") }
	for _, client := range []*ethclient.Client{operational, independent} {
		if client == nil { continue }
		chainID, err := client.ChainID(ctx)
		if err != nil || chainID == nil || chainID.Cmp(new(big.Int).SetUint64(plan.ChainID)) != 0 { return nil, stateMismatchError(err, "coordinator repair provider has another chain") }
		head, err := finalizedEVMHead(ctx, client)
		if err != nil { return nil, err }
		if err := verifyEVMCheckpointFromReader(ctx, ethEVMReceiptFinalityReader{client: client}, head, result.ObservedHead); err != nil { return nil, err }
		manager := &EvmTxManager{client: client}
		for _, address := range contractDeploymentAddresses(observation.source.Deployment) {
			code, err := codeAtCanonicalHead(ctx, manager, address, head)
			if err != nil || len(code) == 0 || !strings.EqualFold(crypto.Keccak256Hash(code).Hex(), observation.source.Deployment.RuntimeHashes[address.Hex()]) { return nil, stateMismatchError(err, "coordinator repair changed immutable custody runtime at %s", address) }
		}
		probe := effectivePrecompileProbe(observation.source.Deployment, observation.source.CoordinatorUpgradeBaseline)
		if probe != observation.source.Deployment.PrecompileProbe {
			code, err := codeAtCanonicalHead(ctx, manager, probe, head)
			if err != nil || len(code) == 0 || !strings.EqualFold(crypto.Keccak256Hash(code).Hex(), observation.source.CoordinatorUpgradeBaseline.ReplacementPrecompileProbeHash) { return nil, stateMismatchError(err, "coordinator repair changed retained replacement probe") }
		}
		for i, final := range []JournalEntry{result.Deploy, result.Activate} {
			created := common.Address{}
			if i == 0 { created = r.Upgrade.Implementation }
			if err := verifyCoordinatorRepairReceipt(ctx, client, head, final, observation.transactions[i], created); err != nil { return nil, err }
			at := ChainHead{Number: final.BlockNumber, Hash: final.BlockHash}
			code, err := codeAtCanonicalHead(ctx, manager, r.Upgrade.Implementation, at)
			if err != nil || !bytes.Equal(code, observation.runtime) { return nil, stateMismatchError(err, "coordinator repair historical runtime changed") }
			if i == 1 {
				active, err := implementationAt(ctx, manager, r.Proxy, at)
				if err != nil || active != r.Upgrade.Implementation { return nil, stateMismatchError(err, "coordinator repair historical activation differs") }
				identity, err := coordinatorRepairIdentity(ctx, manager, observation.source, at)
				if err != nil || identity != r.IdentityHash { return nil, stateMismatchError(err, "coordinator repair historical custody differs") }
			}
		}
		code, err := codeAtCanonicalHead(ctx, manager, r.Upgrade.Implementation, head)
		if err != nil || !bytes.Equal(code, observation.runtime) { return nil, stateMismatchError(err, "coordinator repair current runtime changed") }
		active, err := implementationAt(ctx, manager, r.Proxy, head)
		if err != nil || active != r.Upgrade.Implementation { return nil, stateMismatchError(err, "coordinator repair current activation differs") }
		identity, err := coordinatorRepairIdentity(ctx, manager, observation.source, head)
		if err != nil || identity != r.IdentityHash { return nil, stateMismatchError(err, "coordinator repair current custody differs") }
		nonce, err := client.NonceAt(ctx, r.Deployer, new(big.Int).SetUint64(head.Number))
		if err != nil || nonce != r.Upgrade.DeployerNonce+1 { return nil, stateMismatchError(err, "coordinator repair current deployer boundary differs") }
		pending, err := client.PendingNonceAt(ctx, r.Deployer)
		if err != nil || pending != nonce { return nil, stateMismatchError(err, "coordinator repair deployer still has pending writes") }
		observation.deployerNonce = nonce
	}
	if err := ctx.Err(); err != nil { return nil, err }
	return observation, nil
}

func observeCoordinatorRepairCarry(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry) (*coordinatorRepairCarryObservation, error) {
	if ctx == nil || cfg == nil || plan == nil { return nil, errors.New("coordinator repair planning owner is absent") }
	// File presence is only a reason to perform authentication. A partial or
	// changed retained correction fails; absence never authorizes a new write.
	if _, err := readValidatorEvidenceHistoricalFile(stateDir, coordinatorRepairDirectory+"/request.json", 128<<10); errors.Is(err, os.ErrNotExist) && !coordinatorRepairHistoryRequired(plan, entries) { return nil, nil } else if err != nil { return nil, err }
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil { return nil, err }
	defer client.Close()
	var independent *ethclient.Client
	if independentRPCRequired(cfg) {
		independent, err = dialConfiguredEVMClient(ctx, cfg, cfg.Public.Chain.EVMPublicReadEndpoint)
		if err != nil { return nil, err }
		defer independent.Close()
	}
	return authenticateCoordinatorRepairCarry(ctx, cfg, stateDir, plan, entries, client, independent)
}

// The original v4 probe checkpoint remains a proof of its original adjacent
// CREATE, not of the later correction. Only explicit repair carry supplies
// the separately authenticated completed successor.
func coordinatorRepairBaselineUpgrade(plan *SetupPlan) CoordinatorUpgrade {
	if plan.CoordinatorRepairCarry != nil { return plan.CoordinatorRepairCarry.Request.Request.OldUpgrade }
	return plan.CoordinatorUpgrade
}

func coordinatorRepairBaselinePayloads(payloads *DeploymentPayloads, carry *CoordinatorRepairCarry) *DeploymentPayloads {
	if carry == nil { return payloads }
	copy := *payloads
	copy.CoordinatorUpgrade = carry.Request.Request.OldUpgrade
	return &copy
}

func bindCoordinatorRepairCarryPayloads(payloads *DeploymentPayloads, observation *coordinatorRepairCarryObservation) error {
	if payloads == nil || observation == nil || observation.source == nil { return errors.New("coordinator repair payload owner is absent") }
	r := observation.reference.Request.Request
	if payloads.CoordinatorUpgrade != r.Upgrade || !bytes.Equal(payloads.UpgradeImplementation, observation.creation) || !bytes.Equal(payloads.ExpectedRuntime[r.Upgrade.Implementation], observation.runtime) { return errors.New("completed coordinator repair differs from the current locked release") }
	batcher, err := exactPlanActionByID(observation.source, "fleet.refresh.deploy-batcher")
	if err != nil { return err }
	nonce, err := strconv.ParseUint(batcher.Parameters["expected_nonce"], 10, 64)
	if err != nil || nonce != r.OldUpgrade.DeployerNonce+1 { return errors.New("coordinator repair retained batcher boundary differs") }
	if err := configureFleetBatcherNonce(payloads, nonce); err != nil { return err }
	if common.HexToAddress(batcher.Target) != payloads.FleetBatcherAddress || !strings.EqualFold(batcher.Parameters["runtime_code_hash"], crypto.Keccak256Hash(payloads.FleetBatcherRuntime).Hex()) { return errors.New("coordinator repair retained batcher differs from locked release") }
	return nil
}

func coordinatorRepairCarryMigration(prior *SetupPlan, current *SetupFacts, built *DeploymentPayloads, entries []JournalEntry) (*coordinatorUpgradeMigration, error) {
	observation := prior.coordinatorRepairObserved
	if observation == nil || observation.source == nil || current.DeployerNonce != observation.deployerNonce { return nil, errors.New("coordinator repair migration lost its finalized observation") }
	source := observation.source
	for _, id := range []string{"evm.coordinator-upgrade-implementation", "evm.coordinator-upgrade-activate", "fleet.refresh.deploy-batcher"} { if !exactVerifiedPlanAction(source, entries, id) { return nil, fmt.Errorf("coordinator repair predecessor %s lacks exact original verification", id) } }
	if err := configureCoordinatorUpgradeNonce(built, observation.reference.Request.Request.Upgrade.DeployerNonce); err != nil { return nil, err }
	if source.CoordinatorUpgradeBaseline.Schema == "urnetwork-coordinator-upgrade-baseline-v4" { if err := configurePrecompileProbeNonce(built, source.CoordinatorUpgradeBaseline.ReplacementPrecompileProbeNonce); err != nil { return nil, err } }
	if err := bindCoordinatorRepairCarryPayloads(built, observation); err != nil { return nil, err }
	baseline := source.CoordinatorUpgradeBaseline
	var err error
	baseline.ReleaseDeploymentHash, err = contractDeploymentIdentityHash(built.Manifest)
	if err != nil { return nil, err }
	if err := validateCoordinatorUpgradeBaselineRelease(baseline, source.Deployment, built.Manifest, source.CoordinatorUpgrade); err != nil { return nil, err }
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, source.Deployment, coordinatorRepairBaselinePayloads(built, &observation.reference)); err != nil { return nil, err }
	return &coordinatorUpgradeMigration{Deployment: source.Deployment, Baseline: baseline, Upgrade: built.CoordinatorUpgrade, Repair: observation}, nil
}

func carryCoordinatorRepairPlan(revised, prior *SetupPlan, observation *coordinatorRepairCarryObservation, entries []JournalEntry) error {
	if observation == nil || observation.source == nil { return errors.New("coordinator repair plan carry lacks authenticated source") }
	reference := observation.reference
	revised.CoordinatorRepairCarry = &reference
	for index, action := range revised.Actions {
		if !coordinatorRepairOriginalAction(action.ID) && action.ID != "fleet.refresh.deploy-batcher" { continue }
		source, err := exactPlanActionByID(observation.source, action.ID)
		if err != nil || !exactVerifiedPlanAction(observation.source, entries, action.ID) { return stateMismatchError(err, "coordinator repair cannot carry an unverified predecessor") }
		revised.Actions[index] = source
	}
	deploymentHash, err := contractDeploymentIdentityHash(revised.Deployment)
	if err != nil { return err }
	action := Action{ID: coordinatorRepairCarryActionID, Kind: "evm-read", Target: revised.Deployment.CoordinatorProxy.Hex(), Description: "independently authenticate the completed corrective runtime and retain its original signed receipts", Parameters: map[string]string{deploymentManifestHashParameter: deploymentHash, "request_hash": reference.Request.Hash, "result_hash": reference.Result.Hash, "source_plan_hash": reference.Request.Request.PlanHash}, DependsOn: []string{"evm.coordinator-upgrade-activate"}}
	action.IntentHash, err = actionIntentHash(action)
	if err != nil { return err }
	// Insert the read-only admission before any remaining setup or launch work.
	for index, existing := range revised.Actions {
		if existing.ID == "evm.coordinator-upgrade-activate" {
			revised.Actions = append(revised.Actions[:index+1], append([]Action{action}, revised.Actions[index+1:]...)...)
			break
		}
	}
	revised.MaximumSpend, err = maximumActionSpend(revised.Actions)
	return err
}

func (e *Executor) authenticateCoordinatorRepairCarry(ctx context.Context) (*coordinatorRepairCarryObservation, error) {
	if e == nil || e.plan == nil || e.plan.CoordinatorRepairCarry == nil || e.deployer == nil || e.journal == nil { return nil, errors.New("coordinator repair execution owner is absent") }
	return authenticateCoordinatorRepairCarry(ctx, e.cfg, e.stateDir, e.plan, e.journal.Entries(), e.deployer.client, e.independentEVM)
}

func (e *Executor) verifyCoordinatorRepairCarryAction(ctx context.Context, action Action) error {
	if action.ID != coordinatorRepairCarryActionID || e.plan.CoordinatorRepairCarry == nil || action.Parameters["request_hash"] != e.plan.CoordinatorRepairCarry.Request.Hash || action.Parameters["result_hash"] != e.plan.CoordinatorRepairCarry.Result.Hash || action.Parameters["source_plan_hash"] != e.plan.CoordinatorRepairCarry.Request.Request.PlanHash { return errors.New("coordinator repair admission action differs from approved carry") }
	_, err := e.authenticateCoordinatorRepairCarry(ctx)
	return err
}

func (e *Executor) verifyCoordinatorRepairOriginalAction(ctx context.Context, action Action, verified JournalEntry, record *ActionPostcondition) error {
	observation, err := e.authenticateCoordinatorRepairCarry(ctx)
	if err != nil { return err }
	source, err := exactPlanActionByID(observation.source, action.ID)
	if err != nil || !reflect.DeepEqual(action, source) || !observation.source.allowedPlanHashes()[verified.PlanHash] || !actionAcceptsIntent(source, verified.IntentHash) { return errors.New("coordinator repair predecessor differs from exact original action") }
	if record == nil || record.EVMFinalized.Number >= observation.reference.Result.Result.Activate.BlockNumber || record.IndependentEVMFinalized.Number >= observation.reference.Result.Result.Activate.BlockNumber { return errors.New("coordinator repair predecessor verification is not historical") }
	copy := *e
	copy.plan = observation.source
	copy.payloads = &DeploymentPayloads{Manifest: observation.source.Deployment, CoordinatorUpgrade: observation.source.CoordinatorUpgrade, ExpectedRuntime: map[common.Address][]byte{}}
	return copy.verifyHistoricalEVMPostcondition(ctx, action, record)
}
