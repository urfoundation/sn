package main

// One corrective operation under the retained deployment lock. This neither
// revises setup nor claims that its previously approved implementation changed.
import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const coordinatorRepairMaximumWei = "800000000000000000"
const coordinatorRepairDirectory = "provisional-coordinator-repair"
const coordinatorRepairOwnerRole = "testnet-owner"
const coordinatorRepairDeployerRole = "deployer"

func validCoordinatorRepairSHA256(value string) bool {
	return releaseSHA256.MatchString("sha256:" + value)
}

type coordinatorRepairBudget struct {
	Schema                   string   `json:"schema"`
	PlanHash                 string   `json:"plan_hash"`
	JournalHash              string   `json:"journal_hash"`
	CampaignReserveWei       string   `json:"campaign_reserve_wei"`
	CommittedOrPendingMaxWei string   `json:"committed_or_pending_max_wei"`
	AvailableWei             string   `json:"available_wei"`
	RepairMaxWei             string   `json:"repair_max_wei"`
	Verified                 bool     `json:"verified"`
	Evidence                 []string `json:"evidence,omitempty"`
}

type coordinatorRepairRequest struct {
	Schema          string                  `json:"schema"`
	Provisional     bool                    `json:"provisional"`
	FinalAcceptance bool                    `json:"final_acceptance"`
	PlanHash        string                  `json:"retained_plan_hash"`
	ConfigHash      string                  `json:"config_hash"`
	DeploymentID    string                  `json:"deployment_id"`
	ArtifactSHA256  string                  `json:"artifact_sha256"`
	BudgetSHA256    string                  `json:"budget_sha256"`
	Budget          coordinatorRepairBudget `json:"budget"`
	Proxy           common.Address          `json:"proxy"`
	Vault           common.Address          `json:"settlement_vault"`
	Reserve         common.Address          `json:"reserve_sink"`
	Owner           common.Address          `json:"owner"`
	Deployer        common.Address          `json:"deployer"`
	OldUpgrade      CoordinatorUpgrade      `json:"retained_upgrade"`
	Upgrade         CoordinatorUpgrade      `json:"corrective_upgrade"`
	IdentityHash    string                  `json:"custody_identity_hash"`
	Deploy          Action                  `json:"deploy"`
	Activate        Action                  `json:"activate"`
}

type signedCoordinatorRepairRequest struct {
	Request   coordinatorRepairRequest `json:"request"`
	Hash      string                   `json:"hash"`
	Signature string                   `json:"owner_signature"`
}

type coordinatorRepairResult struct {
	Schema          string       `json:"schema"`
	Provisional     bool         `json:"provisional"`
	FinalAcceptance bool         `json:"final_acceptance"`
	RequestHash     string       `json:"request_hash"`
	Deploy          JournalEntry `json:"deploy_finalization"`
	Activate        JournalEntry `json:"activate_finalization"`
	ObservedAt      string       `json:"observed_at"`
	ObservedHead    ChainHead    `json:"observed_head"`
	IdentityHash    string       `json:"custody_identity_hash"`
}

type signedCoordinatorRepairResult struct {
	Result    coordinatorRepairResult `json:"result"`
	Hash      string                  `json:"hash"`
	Signature string                  `json:"owner_signature"`
}

func validateCoordinatorRepairOptions(command string, o cliOptions) error {
	if command != "coordinator-repair" {
		if o.RepairArtifact != "" || o.RepairArtifactSHA256 != "" || o.RepairBudget != "" || o.RepairBudgetSHA256 != "" {
			return errors.New("repair artifact/budget options require coordinator-repair")
		}
		return nil
	}
	if !o.ProvisionalResume || !o.Apply || !validCanonicalHashHex(o.PlanHash) || !filepath.IsAbs(o.RepairArtifact) || !filepath.IsAbs(o.RepairBudget) || !validCoordinatorRepairSHA256(o.RepairArtifactSHA256) || !validCoordinatorRepairSHA256(o.RepairBudgetSHA256) {
		return errors.New("coordinator-repair requires --provisional-resume --apply --plan-hash and absolute artifact/budget paths with exact SHA256 hashes")
	}
	return nil
}

func validateCoordinatorRepairBudget(plan *SetupPlan, budget coordinatorRepairBudget) error {
	if plan == nil || budget.Schema != "urnetwork-provisional-coordinator-repair-budget-v1" || !budget.Verified || budget.PlanHash != plan.PlanHash || !validCanonicalHashHex(budget.JournalHash) || budget.RepairMaxWei != coordinatorRepairMaximumWei {
		return errors.New("repair budget is not an explicit retained-plan suballocation")
	}
	var reserve *Action
	for i := range plan.Actions {
		if plan.Actions[i].ID == "campaign.evm-gas-reserve" {
			reserve = &plan.Actions[i]
		}
	}
	if reserve == nil || reserve.Kind != "budget-reserve" || string(reserve.Spend.EVMGasWei) != budget.CampaignReserveWei {
		return errors.New("repair budget differs from the existing campaign reserve")
	}
	values := make([]*big.Int, 4)
	for i, raw := range []string{budget.CampaignReserveWei, budget.CommittedOrPendingMaxWei, budget.AvailableWei, budget.RepairMaxWei} {
		value, ok := new(big.Int).SetString(raw, 10)
		if !ok || value.Sign() < 0 || value.String() != raw {
			return errors.New("repair budget contains invalid amounts")
		}
		values[i] = value
	}
	if new(big.Int).Add(values[1], values[2]).Cmp(values[0]) != 0 || values[2].Cmp(values[3]) < 0 {
		return errors.New("existing campaign reserve does not cover the repair after committed/pending liabilities")
	}
	return nil
}

func readCoordinatorRepairInput(path, want string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16<<20 {
		return nil, errors.New("repair input is not a bounded regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimPrefix(bytesSHA256(raw), "sha256:") != want {
		return nil, errors.New("repair input SHA256 differs from the explicit command")
	}
	return raw, nil
}

// The reviewed coordinator has only UUPS's __self immutable. Its exact word
// offsets come from the SHA-bound Foundry artifact, not a guessed code offset.
func coordinatorRepairArtifact(raw []byte, implementation common.Address) ([]byte, []byte, error) {
	type code struct {
		Object              string                                   `json:"object"`
		LinkReferences      map[string]json.RawMessage               `json:"linkReferences"`
		ImmutableReferences map[string][]struct{ Start, Length int } `json:"immutableReferences"`
	}
	var artifact struct {
		ABI      json.RawMessage `json:"abi"`
		Bytecode code            `json:"bytecode"`
		Runtime  code            `json:"deployedBytecode"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return nil, nil, err
	}
	parsed, err := abi.JSON(bytes.NewReader(artifact.ABI))
	if err != nil {
		return nil, nil, err
	}
	expected, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return nil, nil, err
	}
	if len(parsed.Methods) != len(expected.Methods) {
		return nil, nil, errors.New("corrective artifact changes the coordinator function ABI")
	}
	for name, method := range expected.Methods {
		got, ok := parsed.Methods[name]
		if !ok || got.Sig != method.Sig || !bytes.Equal(got.ID, method.ID) {
			return nil, nil, errors.New("corrective artifact changes the coordinator function ABI")
		}
	}
	creation, err := hex.DecodeString(strings.TrimPrefix(artifact.Bytecode.Object, "0x"))
	if err != nil {
		return nil, nil, err
	}
	runtime, err := hex.DecodeString(strings.TrimPrefix(artifact.Runtime.Object, "0x"))
	if err != nil {
		return nil, nil, err
	}
	if len(creation) == 0 || len(creation) > 49_152 || len(runtime) == 0 || len(runtime) > 24_576 || len(artifact.Bytecode.LinkReferences) != 0 || len(artifact.Runtime.LinkReferences) != 0 || len(artifact.Runtime.ImmutableReferences) != 1 || implementation == (common.Address{}) {
		return nil, nil, errors.New("corrective artifact has unsupported linking or empty code")
	}
	for _, refs := range artifact.Runtime.ImmutableReferences {
		if len(refs) == 0 {
			return nil, nil, errors.New("coordinator __self references are absent")
		}
		for _, ref := range refs {
			if ref.Start < 0 || ref.Length != 32 || ref.Start > len(runtime)-32 || !bytes.Equal(runtime[ref.Start:ref.Start+32], make([]byte, 32)) {
				return nil, nil, errors.New("coordinator __self reference is invalid")
			}
			copy(runtime[ref.Start:ref.Start+32], common.LeftPadBytes(implementation.Bytes(), 32))
		}
	}
	return creation, runtime, nil
}

func coordinatorRepairSignature(value any, key *ecdsa.PrivateKey) (string, string, error) {
	hash, err := canonicalHashHex(value)
	if err != nil {
		return "", "", err
	}
	sig, err := crypto.Sign(common.HexToHash(hash).Bytes(), key)
	return hash, hex.EncodeToString(sig), err
}

func verifyCoordinatorRepairSignature(value any, hash, signature string, owner common.Address) error {
	got, err := canonicalHashHex(value)
	if err != nil || got != hash {
		return errors.New("corrective receipt content hash differs")
	}
	sig, err := hex.DecodeString(signature)
	if err != nil {
		return err
	}
	key, err := crypto.SigToPub(common.HexToHash(hash).Bytes(), sig)
	if err != nil || crypto.PubkeyToAddress(*key) != owner {
		return errors.New("corrective receipt owner signature differs")
	}
	return nil
}

func writeCoordinatorRepairFile(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if prior, err := os.ReadFile(path); err == nil {
		if bytes.Equal(prior, raw) {
			return nil
		}
		return errors.New("corrective receipt already exists with different immutable bytes")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(path, raw, 0o600)
}

func coordinatorRepairAction(id string, signer common.Address, nonce uint64, to *common.Address, data []byte, gas uint64) (Action, error) {
	target := "create"
	if to != nil {
		target = to.Hex()
	}
	a := Action{ID: id, Kind: "evm-transaction", Target: target, Description: "actual provisional coordinator runtime correction; final_acceptance=false", Parameters: map[string]string{
		"expected_signer": signer.Hex(), "expected_nonce": strconv.FormatUint(nonce, 10), "expected_transaction_to": target,
		"expected_value_wei": "0", "expected_data_keccak256": crypto.Keccak256Hash(data).Hex(),
		evmMaximumGasUnitsParameter: strconv.FormatUint(gas, 10), evmMaximumFeePerGasParameter: "100000000000",
	}, Spend: Spend{EVMGasWei: multiplyUint64Decimal(gas, 100_000_000_000)}}
	if to == nil {
		a.Parameters["expected_created_address"] = crypto.CreateAddress(signer, nonce).Hex()
	}
	var err error
	a.IntentHash, err = actionIntentHash(a)
	return a, err
}

func coordinatorRepairIdentity(ctx context.Context, manager *EvmTxManager, plan *SetupPlan, head ChainHead) (string, error) {
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return "", err
	}
	values := make(map[string]string)
	for _, method := range []string{"owner", "netuid", "selfColdkey", "settlementVault", "reserveSink", "validatorEvidence"} {
		result, err := contractCallAt(ctx, manager.client, plan.Deployment.CoordinatorProxy, parsed, method, head.Number)
		if err != nil || len(result) != 1 {
			return "", stateMismatchError(err, "coordinator corrective identity %s is absent", method)
		}
		encoded, err := parsed.Methods[method].Outputs.Pack(result...)
		if err != nil {
			return "", err
		}
		values[method] = hex.EncodeToString(encoded)
	}
	if values["owner"] != hex.EncodeToString(common.LeftPadBytes(common.HexToAddress(plan.Roles.Owner).Bytes(), 32)) || values["settlementVault"] != hex.EncodeToString(common.LeftPadBytes(plan.Deployment.SettlementVault.Bytes(), 32)) || values["reserveSink"] != hex.EncodeToString(common.LeftPadBytes(plan.Deployment.ReserveSink.Bytes(), 32)) || values["netuid"] != hex.EncodeToString(common.LeftPadBytes(new(big.Int).SetUint64(uint64(plan.Netuid)).Bytes(), 32)) {
		return "", errors.New("live coordinator owner/custody/netuid differs from the retained plan")
	}
	return canonicalHashHex(values)
}

func validateCoordinatorRepairRequest(plan *SetupPlan, record *signedCoordinatorRepairRequest) error {
	if plan == nil || record == nil {
		return errors.New("corrective request is absent")
	}
	r := record.Request
	if r.Schema != "urnetwork-provisional-coordinator-repair-v1" || !r.Provisional || r.FinalAcceptance || r.PlanHash != plan.PlanHash || r.ConfigHash != plan.ConfigHash || r.DeploymentID != plan.DeploymentID || r.Proxy != plan.Deployment.CoordinatorProxy || r.Vault != plan.Deployment.SettlementVault || r.Reserve != plan.Deployment.ReserveSink || r.Owner != common.HexToAddress(plan.Roles.Owner) || r.Deployer != common.HexToAddress(plan.Roles.Deployer) || r.OldUpgrade != plan.CoordinatorUpgrade || r.Upgrade.Implementation != crypto.CreateAddress(r.Deployer, r.Upgrade.DeployerNonce) || r.Upgrade.Implementation == r.OldUpgrade.Implementation || !validCanonicalHashHex(r.IdentityHash) || !validCoordinatorRepairSHA256(r.ArtifactSHA256) || !validCoordinatorRepairSHA256(r.BudgetSHA256) {
		return errors.New("corrective request changes the retained deployment identity")
	}
	if err := validateCoordinatorRepairBudget(plan, r.Budget); err != nil {
		return err
	}
	if err := validateCoordinatorUpgradeIdentity(r.Upgrade, r.Deployer, plan.Deployment); err != nil {
		return err
	}
	for _, a := range []Action{r.Deploy, r.Activate} {
		got, err := actionIntentHash(a)
		if err != nil || got != a.IntentHash {
			return errors.New("corrective action intent hash differs")
		}
		gas, fee, err := evmActionFeeEnvelope(a)
		wantGas := uint64(7_500_000)
		if a.ID == r.Activate.ID {
			wantGas = 500_000
		}
		if err != nil || gas != wantGas || fee != 100_000_000_000 || fee > plan.MaximumEVMFeePerGasWei {
			return errors.New("corrective action changes approved gas or fee bounds")
		}
	}
	if r.Deploy.ID != "repair.coordinator-rounding.deploy" || r.Activate.ID != "repair.coordinator-rounding.activate" || r.Deploy.Spend.EVMGasWei != DecimalUint("750000000000000000") || r.Activate.Spend.EVMGasWei != DecimalUint("50000000000000000") {
		return errors.New("corrective operation exceeds its two-action allowance")
	}
	return verifyCoordinatorRepairSignature(r, record.Hash, record.Signature, r.Owner)
}

func validateCoordinatorRepairSigningRoles(plan *SetupPlan, roles *RoleSecrets) error {
	if plan == nil || roles == nil || roles.Schema != "urnetwork-sim-role-secrets-v1" || roles.DeploymentID != plan.DeploymentID {
		return errors.New("retained signing roles belong to another deployment or schema")
	}
	for label, expected := range map[string]string{coordinatorRepairOwnerRole: plan.Roles.Owner, coordinatorRepairDeployerRole: plan.Roles.Deployer} {
		address, err := roles.EVMAddress(label)
		if err != nil || address != common.HexToAddress(expected) {
			return errors.New("retained corrective signing role differs")
		}
	}
	return nil
}

// Early deployments sometimes completed their exact postcondition after a
// canceled receipt wait without backfilling StageFinalized. Reuse that recorded
// completion only when the existing current nonce read proves its slot consumed.
func coordinatorRepairJournalNonceClear(entries []JournalEntry, signer string, finalizedNonce uint64) error {
	pending := map[string]JournalEntry{}
	included := map[string]bool{}
	for _, entry := range entries {
		if entry.Stage == StageBroadcast && strings.EqualFold(entry.Signer, signer) {
			pending[entry.TransactionHash] = entry
		}
		if entry.Stage == StageIncluded && entry.BlockNumber != 0 {
			included[entry.TransactionHash] = true
		}
		if entry.Stage == StageFinalized {
			delete(pending, entry.TransactionHash)
		}
		if entry.Stage == StageVerified && validCanonicalHashHex(entry.PostconditionHash) && entry.PostconditionPath != "" {
			for hash, broadcast := range pending {
				if included[hash] && entry.PlanHash == broadcast.PlanHash && entry.ActionID == broadcast.ActionID && entry.IntentHash == broadcast.IntentHash {
					nonce, err := strconv.ParseUint(broadcast.Nonce, 10, 64)
					if err == nil && nonce < finalizedNonce {
						delete(pending, hash)
					}
				}
			}
		}
	}
	if len(pending) != 0 {
		return errors.New("repair signer still owns an unresolved journal broadcast")
	}
	return nil
}

func runCoordinatorRepair(ctx context.Context, cfg *ResolvedConfig, stateDir string, o cliOptions) error {
	if err := validateCoordinatorRepairOptions("coordinator-repair", o); err != nil {
		return err
	}
	plan, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		return err
	}
	if err := prepareProvisionalResume(ctx, cfg, stateDir, "coordinator-repair", o, plan); err != nil {
		return err
	}
	if err := prepareProvisionalRPCOverride(cfg, stateDir, o.ProvisionalRPCAuthority); err != nil {
		return err
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		return err
	}
	defer journal.Close()
	artifact, err := readCoordinatorRepairInput(o.RepairArtifact, o.RepairArtifactSHA256)
	if err != nil {
		return err
	}
	budgetBytes, err := readCoordinatorRepairInput(o.RepairBudget, o.RepairBudgetSHA256)
	if err != nil {
		return err
	}
	var budget coordinatorRepairBudget
	if err := json.Unmarshal(budgetBytes, &budget); err != nil {
		return err
	}
	if err := validateCoordinatorRepairBudget(plan, budget); err != nil {
		return err
	}
	var roles RoleSecrets
	if err := readJSONFile(filepath.Join(stateDir, "secrets", "roles.json"), &roles); err != nil {
		return err
	}
	if err := validateCoordinatorRepairSigningRoles(plan, &roles); err != nil {
		return err
	}
	runtimeCfg, err := campaignRPCConfig(cfg)
	if err != nil {
		return err
	}
	deployer, err := DialEvmTxManager(ctx, runtimeCfg, stateDir, journal, &roles, coordinatorRepairDeployerRole)
	if err != nil {
		return err
	}
	defer deployer.Close()
	owner, err := DialEvmTxManager(ctx, runtimeCfg, stateDir, journal, &roles, coordinatorRepairOwnerRole)
	if err != nil {
		return err
	}
	defer owner.Close()
	root := filepath.Join(stateDir, coordinatorRepairDirectory)
	if err := ensurePrivateDir(root); err != nil {
		return err
	}
	if err := rejectFinalArtifactSymlinkComponents(stateDir, root); err != nil {
		return err
	}
	requestPath, resultPath := filepath.Join(root, "request.json"), filepath.Join(root, "result.json")
	var record signedCoordinatorRepairRequest
	readErr := readJSONFile(requestPath, &record)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	head, err := finalizedEVMHead(ctx, owner.client)
	if err != nil {
		return err
	}
	identity, err := coordinatorRepairIdentity(ctx, owner, plan, head)
	if err != nil {
		return err
	}
	if errors.Is(readErr, os.ErrNotExist) {
		entries := journal.Entries()
		if len(entries) == 0 || entries[len(entries)-1].EntryHash != budget.JournalHash {
			return errors.New("repair budget journal anchor changed before exclusive admission")
		}
		active, err := implementationAt(ctx, owner, plan.Deployment.CoordinatorProxy, head)
		if err != nil || active != plan.CoordinatorUpgrade.Implementation {
			return stateMismatchError(err, "repair active implementation differs from the retained baseline")
		}
		oldCode, err := codeAtCanonicalHead(ctx, owner, active, head)
		if err != nil || crypto.Keccak256Hash(oldCode).Hex() != plan.CoordinatorUpgrade.RuntimeCodeHash {
			return stateMismatchError(err, "repair baseline runtime hash differs")
		}
		deployNonce, err := deployer.PendingNonce(ctx)
		if err != nil {
			return err
		}
		activateNonce, err := owner.PendingNonce(ctx)
		if err != nil {
			return err
		}
		for manager, nonce := range map[*EvmTxManager]uint64{deployer: deployNonce, owner: activateNonce} {
			finalizedNonce, err := manager.client.NonceAt(ctx, crypto.PubkeyToAddress(manager.key.PublicKey), new(big.Int).SetUint64(head.Number))
			if err != nil || finalizedNonce != nonce {
				return stateMismatchError(err, "repair signer has an unresolved pending nonce")
			}
			if err := coordinatorRepairJournalNonceClear(entries, crypto.PubkeyToAddress(manager.key.PublicKey).Hex(), finalizedNonce); err != nil {
				return err
			}
		}
		implementation := crypto.CreateAddress(common.HexToAddress(plan.Roles.Deployer), deployNonce)
		creation, runtime, err := coordinatorRepairArtifact(artifact, implementation)
		if err != nil {
			return err
		}
		deploy, err := coordinatorRepairAction("repair.coordinator-rounding.deploy", common.HexToAddress(plan.Roles.Deployer), deployNonce, nil, creation, 7_500_000)
		if err != nil {
			return err
		}
		parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
		if err != nil {
			return err
		}
		calldata, err := parsed.Pack("upgradeToAndCall", implementation, []byte{})
		if err != nil {
			return err
		}
		activate, err := coordinatorRepairAction("repair.coordinator-rounding.activate", common.HexToAddress(plan.Roles.Owner), activateNonce, &plan.Deployment.CoordinatorProxy, calldata, 500_000)
		if err != nil {
			return err
		}
		record.Request = coordinatorRepairRequest{Schema: "urnetwork-provisional-coordinator-repair-v1", Provisional: true, PlanHash: plan.PlanHash, ConfigHash: plan.ConfigHash, DeploymentID: plan.DeploymentID, ArtifactSHA256: o.RepairArtifactSHA256, BudgetSHA256: o.RepairBudgetSHA256, Budget: budget, Proxy: plan.Deployment.CoordinatorProxy, Vault: plan.Deployment.SettlementVault, Reserve: plan.Deployment.ReserveSink, Owner: common.HexToAddress(plan.Roles.Owner), Deployer: common.HexToAddress(plan.Roles.Deployer), OldUpgrade: plan.CoordinatorUpgrade, Upgrade: CoordinatorUpgrade{Schema: "urnetwork-coordinator-upgrade-v2", DeploymentID: plan.DeploymentID, Implementation: implementation, DeployerNonce: deployNonce, RuntimeCodeHash: crypto.Keccak256Hash(runtime).Hex()}, IdentityHash: identity, Deploy: deploy, Activate: activate}
		record.Hash, record.Signature, err = coordinatorRepairSignature(record.Request, owner.key)
		if err != nil {
			return err
		}
		if err := validateCoordinatorRepairRequest(plan, &record); err != nil {
			return err
		}
		if err := writeCoordinatorRepairFile(requestPath, record); err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(root, "artifact.json"), artifact, 0o600); err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(root, "budget.json"), budgetBytes, 0o600); err != nil {
			return err
		}
	}
	if err := validateCoordinatorRepairRequest(plan, &record); err != nil {
		return err
	}
	r := record.Request
	if r.ArtifactSHA256 != o.RepairArtifactSHA256 || r.BudgetSHA256 != o.RepairBudgetSHA256 || r.IdentityHash != identity {
		return errors.New("corrective retry changed its artifact, allowance or custody")
	}
	if _, err := os.Stat(resultPath); err == nil {
		active, hash, err := loadProvisionalCoordinatorRepair(cfg, stateDir, plan.CoordinatorUpgrade, head)
		if err == nil && (active != r.Upgrade || hash == "") {
			err = errors.New("corrective completion is ahead of the current finalized observation")
		}
		return printResult(o.Format, map[string]any{"result_path": resultPath, "already_completed": true, "final_acceptance": false}, err)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	creation, runtime, err := coordinatorRepairArtifact(artifact, r.Upgrade.Implementation)
	if err != nil {
		return err
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	calldata, err := parsed.Pack("upgradeToAndCall", r.Upgrade.Implementation, []byte{})
	if err != nil {
		return err
	}
	for _, step := range []struct {
		action  Action
		manager *EvmTxManager
		to      *common.Address
		data    []byte
	}{{r.Deploy, deployer, nil, creation}, {r.Activate, owner, &r.Proxy, calldata}} {
		if step.action.ID == r.Activate.ID {
			head, err = finalizedEVMHead(ctx, owner.client)
			if err != nil {
				return err
			}
			code, err := codeAtCanonicalHead(ctx, owner, r.Upgrade.Implementation, head)
			if err != nil || !bytes.Equal(code, runtime) {
				return stateMismatchError(err, "deployed corrective runtime differs from the approved artifact")
			}
			active, err := implementationAt(ctx, owner, r.Proxy, head)
			if err != nil || active != r.OldUpgrade.Implementation && active != r.Upgrade.Implementation {
				return stateMismatchError(err, "corrective activation baseline changed")
			}
		}
		if _, exists := journal.LastStage(step.action.ID, step.action.IntentHash, plan.PlanHash); !exists {
			if err := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: step.action.ID, IntentHash: step.action.IntentHash, Stage: StageIntent}); err != nil {
				return err
			}
		}
		if _, err := step.manager.Send(ctx, plan.PlanHash, step.action, step.to, new(big.Int), step.data); err != nil {
			appendErr := journal.Append(JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: step.action.ID, IntentHash: step.action.IntentHash, Stage: StageFailed, Error: err.Error()})
			return errors.Join(err, appendErr)
		}
	}
	head, err = finalizedEVMHead(ctx, owner.client)
	if err != nil {
		return err
	}
	active, err := implementationAt(ctx, owner, r.Proxy, head)
	if err != nil || active != r.Upgrade.Implementation {
		return stateMismatchError(err, "corrective activation postcondition differs")
	}
	identity, err = coordinatorRepairIdentity(ctx, owner, plan, head)
	if err != nil || identity != r.IdentityHash {
		return stateMismatchError(err, "corrective activation changed custody identity")
	}
	deployFinal, deployOK := journal.LatestTransaction(plan.PlanHash, r.Deploy.ID, r.Deploy.IntentHash)
	activateFinal, activateOK := journal.LatestTransaction(plan.PlanHash, r.Activate.ID, r.Activate.IntentHash)
	if !deployOK || !activateOK || deployFinal.Stage != StageFinalized || activateFinal.Stage != StageFinalized {
		return errors.New("corrective operation lacks two actual finalized transactions")
	}
	result := signedCoordinatorRepairResult{Result: coordinatorRepairResult{Schema: "urnetwork-provisional-coordinator-repair-result-v1", Provisional: true, RequestHash: record.Hash, Deploy: deployFinal, Activate: activateFinal, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ObservedHead: head, IdentityHash: identity}}
	result.Hash, result.Signature, err = coordinatorRepairSignature(result.Result, owner.key)
	if err != nil {
		return err
	}
	if err := writeCoordinatorRepairFile(resultPath, result); err != nil {
		return err
	}
	return printResult(o.Format, map[string]any{"result_path": resultPath, "receipt_hash": result.Hash, "implementation": r.Upgrade.Implementation.Hex(), "final_acceptance": false}, nil)
}

// Only current explicitly provisional observation selects this correction.
// The retained public manifest/plan and historical setup receipts stay intact.
func loadProvisionalCoordinatorRepair(cfg *ResolvedConfig, stateDir string, original CoordinatorUpgrade, head ChainHead) (CoordinatorUpgrade, string, error) {
	if !provisionalResumeEnabled(cfg) {
		return original, "", nil
	}
	root := filepath.Join(stateDir, coordinatorRepairDirectory)
	var result signedCoordinatorRepairResult
	if err := readJSONFile(filepath.Join(root, "result.json"), &result); errors.Is(err, os.ErrNotExist) {
		return original, "", nil
	} else if err != nil {
		return original, "", err
	}
	plan, err := readPersistedPlan(stateDir)
	if err != nil {
		return original, "", err
	}
	if plan.CoordinatorUpgrade != original || plan.PlanHash != cfg.provisionalResume.Record.PlanHash {
		return original, "", errors.New("corrective observer retained plan differs")
	}
	var request signedCoordinatorRepairRequest
	if err := readJSONFile(filepath.Join(root, "request.json"), &request); err != nil {
		return original, "", err
	}
	if err := validateCoordinatorRepairRequest(plan, &request); err != nil {
		return original, "", err
	}
	r := result.Result
	if r.Schema != "urnetwork-provisional-coordinator-repair-result-v1" || !r.Provisional || r.FinalAcceptance || r.RequestHash != request.Hash || r.IdentityHash != request.Request.IdentityHash || r.ObservedHead.Number < r.Activate.BlockNumber || !validCanonicalHashHex(r.ObservedHead.Hash) {
		return original, "", errors.New("corrective result is incomplete or inconsistent")
	}
	for i, entry := range []JournalEntry{r.Deploy, r.Activate} {
		action := []Action{request.Request.Deploy, request.Request.Activate}[i]
		if entry.Stage != StageFinalized || entry.PlanHash != plan.PlanHash || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash || entry.BlockNumber == 0 || !validCanonicalHashHex(entry.BlockHash) || !validCanonicalHashHex(entry.TransactionHash) {
			return original, "", errors.New("corrective result lacks exact finalized action identity")
		}
	}
	if err := verifyCoordinatorRepairSignature(r, result.Hash, result.Signature, request.Request.Owner); err != nil {
		return original, "", err
	}
	if head.Number < r.ObservedHead.Number {
		return original, "", nil
	}
	return request.Request.Upgrade, result.Hash, nil
}
