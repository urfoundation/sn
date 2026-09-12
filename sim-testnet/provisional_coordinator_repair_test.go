package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func coordinatorRepairFixture(t *testing.T) (*SetupPlan, signedCoordinatorRepairRequest) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(key.PublicKey)
	deployer := common.HexToAddress("0x2222222222222222222222222222222222222222")
	hash := "0x" + strings.Repeat("12", 32)
	plan := &SetupPlan{PlanHash: hash, ConfigHash: hash, DeploymentID: "repair-fixture", MaximumEVMFeePerGasWei: 100_000_000_000,
		Roles:              PublicRoles{Owner: owner.Hex(), Deployer: deployer.Hex()},
		Deployment:         ContractDeployment{DeploymentID: "repair-fixture", InitialNonce: 13, CoordinatorProxy: common.HexToAddress("0x1001"), SettlementVault: common.HexToAddress("0x1002"), ReserveSink: common.HexToAddress("0x1003")},
		CoordinatorUpgrade: CoordinatorUpgrade{Implementation: crypto.CreateAddress(deployer, 30)},
		Actions:            []Action{{ID: "campaign.evm-gas-reserve", Kind: "budget-reserve", Spend: Spend{EVMGasWei: DecimalUint("12726750000000000000")}}},
	}
	budget := coordinatorRepairBudget{Schema: "urnetwork-provisional-coordinator-repair-budget-v1", PlanHash: hash, JournalHash: hash, CampaignReserveWei: "12726750000000000000", CommittedOrPendingMaxWei: "1047421069877763664", AvailableWei: "11679328930122236336", RepairMaxWei: coordinatorRepairMaximumWei, Verified: true}
	implementation := crypto.CreateAddress(deployer, 33)
	deploy, err := coordinatorRepairAction("repair.coordinator-rounding.deploy", deployer, 33, nil, []byte{0x60, 0}, 7_500_000)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	data, err := parsed.Pack("upgradeToAndCall", implementation, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	activate, err := coordinatorRepairAction("repair.coordinator-rounding.activate", owner, 19, &plan.Deployment.CoordinatorProxy, data, 500_000)
	if err != nil {
		t.Fatal(err)
	}
	r := coordinatorRepairRequest{Schema: "urnetwork-provisional-coordinator-repair-v1", Provisional: true, PlanHash: hash, ConfigHash: hash, DeploymentID: plan.DeploymentID, ArtifactSHA256: strings.Repeat("ab", 32), BudgetSHA256: strings.Repeat("cd", 32), Budget: budget, Proxy: plan.Deployment.CoordinatorProxy, Vault: plan.Deployment.SettlementVault, Reserve: plan.Deployment.ReserveSink, Owner: owner, Deployer: deployer, OldUpgrade: plan.CoordinatorUpgrade, Upgrade: CoordinatorUpgrade{Schema: "urnetwork-coordinator-upgrade-v2", DeploymentID: plan.DeploymentID, DeployerNonce: 33, Implementation: implementation, RuntimeCodeHash: hash}, IdentityHash: hash, Deploy: deploy, Activate: activate}
	record := signedCoordinatorRepairRequest{Request: r}
	record.Hash, record.Signature, err = coordinatorRepairSignature(r, key)
	if err != nil {
		t.Fatal(err)
	}
	return plan, record
}

func TestCoordinatorRepairAllowanceAndOwnerBinding(t *testing.T) {
	plan, original := coordinatorRepairFixture(t)
	if err := validateCoordinatorRepairRequest(plan, &original); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(original)
	for name, mutate := range map[string]func(*signedCoordinatorRepairRequest){
		"insufficient available":  func(r *signedCoordinatorRepairRequest) { r.Request.Budget.AvailableWei = "0" },
		"invented reserve":        func(r *signedCoordinatorRepairRequest) { r.Request.Budget.CampaignReserveWei = "99999999999999999999" },
		"unverified liabilities":  func(r *signedCoordinatorRepairRequest) { r.Request.Budget.Verified = false },
		"changed plan":            func(r *signedCoordinatorRepairRequest) { r.Request.PlanHash = "0x" + strings.Repeat("ab", 32) },
		"changed custody":         func(r *signedCoordinatorRepairRequest) { r.Request.Vault = common.HexToAddress("0x9999") },
		"acceptance claim":        func(r *signedCoordinatorRepairRequest) { r.Request.FinalAcceptance = true },
		"changed artifact":        func(r *signedCoordinatorRepairRequest) { r.Request.ArtifactSHA256 = strings.Repeat("ef", 32) },
		"changed owner signature": func(r *signedCoordinatorRepairRequest) { r.Signature = strings.Repeat("00", 65) },
		"changed gas envelope": func(r *signedCoordinatorRepairRequest) {
			r.Request.Deploy.Parameters[evmMaximumGasUnitsParameter] = "7500001"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var record signedCoordinatorRepairRequest
			if err := json.Unmarshal(encoded, &record); err != nil {
				t.Fatal(err)
			}
			mutate(&record)
			if err := validateCoordinatorRepairRequest(plan, &record); err == nil {
				t.Fatal("changed repair admitted")
			}
		})
	}
	// The normal sender still enforces exact custody/nonce/data and its fee cap.
	r := original.Request
	data := []byte{0x60, 0}
	if err := validateApprovedEVMTransactionFields(r.Deploy, r.Deployer, 33, nil, new(big.Int), data); err != nil {
		t.Fatal(err)
	}
	if err := validateApprovedEVMTransactionFields(r.Deploy, r.Deployer, 34, nil, new(big.Int), data); err == nil {
		t.Fatal("changed deployment nonce admitted")
	}
	if _, _, err := validateEVMTransactionEnvelope(r.Deploy, 7_500_000, big.NewInt(100_000_000_000), new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil), new(big.Int)); err == nil {
		t.Fatal("padded gas exceeded maximum but was admitted")
	}
}

func TestCoordinatorRepairArtifactBoundedAndExactlyLinked(t *testing.T) {
	implementation := common.HexToAddress("0x1234")
	makeArtifact := func(size int, length int) []byte {
		artifact := map[string]any{"abi": json.RawMessage(CoordinatorABI), "bytecode": map[string]any{"object": "0x6000"}, "deployedBytecode": map[string]any{"object": "0x" + hex.EncodeToString(make([]byte, size)), "immutableReferences": map[string]any{"867": []map[string]int{{"start": 0, "length": length}}}}}
		raw, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := makeArtifact(64, 32)
	creation, runtime, err := coordinatorRepairArtifact(raw, implementation)
	if err != nil || !bytes.Equal(creation, []byte{0x60, 0}) || !bytes.Equal(runtime[:32], common.LeftPadBytes(implementation.Bytes(), 32)) {
		t.Fatalf("exact artifact failed: %v", err)
	}
	for _, bad := range [][]byte{makeArtifact(24_577, 32), makeArtifact(64, 31), makeArtifact(16, 32)} {
		if _, _, err := coordinatorRepairArtifact(bad, implementation); err == nil {
			t.Fatal("invalid artifact accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCoordinatorRepairInput(path, strings.TrimPrefix(bytesSHA256(raw), "sha256:")); err != nil {
		t.Fatal(err)
	}
	if _, err := readCoordinatorRepairInput(path, strings.Repeat("00", 32)); err == nil {
		t.Fatal("wrong artifact SHA accepted")
	}
}

func TestCoordinatorRepairCLIAndStrictObservation(t *testing.T) {
	args := []string{"coordinator-repair", "--provisional-resume", "--apply", "--plan-hash", "0x" + strings.Repeat("ab", 32), "--repair-artifact", "/tmp/artifact.json", "--repair-artifact-sha256", strings.Repeat("ab", 32), "--repair-budget", "/tmp/budget.json", "--repair-budget-sha256", strings.Repeat("cd", 32)}
	if _, _, err := parseCLI(args); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseCLI([]string{"coordinator-repair"}); err == nil {
		t.Fatal("unapproved corrective command accepted")
	}
	original := CoordinatorUpgrade{Implementation: common.HexToAddress("0x1111")}
	got, hash, err := loadProvisionalCoordinatorRepair(nil, "/nonexistent", original, ChainHead{})
	if err != nil || got != original || hash != "" {
		t.Fatalf("strict observation changed: %v", err)
	}
}

func TestCoordinatorRepairFinalizedResultSignatureBindsBothActions(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(key.PublicKey)
	hash := "0x" + strings.Repeat("12", 32)
	result := coordinatorRepairResult{Schema: "urnetwork-provisional-coordinator-repair-result-v1", Provisional: true, RequestHash: hash,
		Deploy:       JournalEntry{Stage: StageFinalized, ActionID: "repair.coordinator-rounding.deploy", TransactionHash: hash, BlockNumber: 10},
		Activate:     JournalEntry{Stage: StageFinalized, ActionID: "repair.coordinator-rounding.activate", TransactionHash: hash, BlockNumber: 11},
		ObservedHead: ChainHead{Number: 12, Hash: hash}, IdentityHash: hash}
	digest, signature, err := coordinatorRepairSignature(result, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyCoordinatorRepairSignature(result, digest, signature, owner); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*coordinatorRepairResult){
		"different request":            func(r *coordinatorRepairResult) { r.RequestHash = "0x" + strings.Repeat("34", 32) },
		"different deploy receipt":     func(r *coordinatorRepairResult) { r.Deploy.TransactionHash = "0x" + strings.Repeat("34", 32) },
		"different activation receipt": func(r *coordinatorRepairResult) { r.Activate.TransactionHash = "0x" + strings.Repeat("34", 32) },
		"unfinalized activation":       func(r *coordinatorRepairResult) { r.Activate.Stage = StageBroadcast },
	} {
		t.Run(name, func(t *testing.T) {
			changed := result
			mutate(&changed)
			if err := verifyCoordinatorRepairSignature(changed, digest, signature, owner); err == nil {
				t.Fatal("altered finalized correction accepted")
			}
		})
	}
}

func TestCoordinatorRepairUsesRetainedCanonicalSigningRoles(t *testing.T) {
	plan, _ := coordinatorRepairFixture(t)
	roles := RoleSecrets{Schema: "urnetwork-sim-role-secrets-v1", DeploymentID: plan.DeploymentID, EVM: map[string]EVMRoleSecret{
		"testnet-owner": {Label: "testnet-owner", Address: plan.Roles.Owner},
		"deployer":      {Label: "deployer", Address: plan.Roles.Deployer},
	}}
	if err := validateCoordinatorRepairSigningRoles(plan, &roles); err != nil {
		t.Fatal(err)
	}
	// The actual retained schema has no "owner" alias. Do not silently pick a
	// different signer when that canonical role is absent or has changed.
	roles.EVM["owner"] = roles.EVM["testnet-owner"]
	delete(roles.EVM, "testnet-owner")
	if err := validateCoordinatorRepairSigningRoles(plan, &roles); err == nil {
		t.Fatal("noncanonical owner alias admitted")
	}
	roles.EVM["testnet-owner"] = EVMRoleSecret{Address: plan.Roles.Deployer}
	if err := validateCoordinatorRepairSigningRoles(plan, &roles); err == nil {
		t.Fatal("changed owner address admitted")
	}
}

func TestCoordinatorRepairHistoricalVerifiedNonceIsConsumed(t *testing.T) {
	hash := "0x" + strings.Repeat("12", 32)
	broadcast := JournalEntry{Stage: StageBroadcast, Signer: "deployer", Nonce: "0", TransactionHash: hash, PlanHash: hash, ActionID: "evm.reserve-sink", IntentHash: hash}
	included := broadcast
	included.Stage = StageIncluded
	included.BlockNumber = 7888433
	verified := broadcast
	verified.Stage = StageVerified
	verified.TransactionHash = ""
	verified.PostconditionHash = hash
	verified.PostconditionPath = "receipts/old.json"
	entries := []JournalEntry{broadcast, included, {Stage: StageFailed, Error: "context canceled"}, verified}
	if err := coordinatorRepairJournalNonceClear(entries, "deployer", 33); err != nil {
		t.Fatal(err)
	}
	if err := coordinatorRepairJournalNonceClear(entries, "deployer", 0); err == nil {
		t.Fatal("unconsumed nonce admitted")
	}
	if err := coordinatorRepairJournalNonceClear(entries[:3], "deployer", 33); err == nil {
		t.Fatal("unverified broadcast admitted")
	}
	verified.PlanHash = "0x" + strings.Repeat("34", 32)
	entries[3] = verified
	if err := coordinatorRepairJournalNonceClear(entries, "deployer", 33); err == nil {
		t.Fatal("another plan's completion admitted")
	}
}
