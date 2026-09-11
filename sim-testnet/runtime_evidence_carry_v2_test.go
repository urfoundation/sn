//go:build linux || darwin

// Real consent signatures, signed transactions and the native journal writer
// exercise source-only revision without relabeling the original setup files.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Retain the genuine companion fixture's already authenticated creation and
// anchor, then sign all four original sources under that same approval.
func newRuntimeEvidenceSetupOriginalCarryV2Test(t *testing.T) *runtimeEvidenceProvisionV2TestFixture {
	t.Helper()
	return newRuntimeEvidenceSetupOriginalCarryHistoryV2Test(t, nil)
}

// Insert predecessor history before any original setup bytes are signed.
func newRuntimeEvidenceSetupOriginalCarryHistoryV2Test(t *testing.T, beforePreparation func(*Executor)) *runtimeEvidenceProvisionV2TestFixture {
	t.Helper()
	creation := newValidatorEvidenceActivationCarryTestFixture(t)
	executor := creation.executor
	executor.plan.validatorEvidenceObserved = creation.authenticate(t)
	if err := executor.journal.Close(); err != nil {
		t.Fatal(err)
	}
	if beforePreparation != nil {
		beforePreparation(executor)
	}
	prepared := &runtimeEvidenceActivationPreparedV2{Schema: "urnetwork-sim-evidence-activation-prepared-v2", PlanHash: executor.plan.PlanHash, ConfigHash: executor.cfg.ConfigHash, PolicyHash: executor.cfg.PolicyHash, Epoch: 9,
		Native: ChainHead{Number: 100, Hash: common.Hash{0x31}.Hex()}, Evm: ChainHead{Number: 200, Hash: common.Hash{0x32}.Hex()}}
	for validatorId := 1; validatorId <= executor.cfg.Config.Topology.Validators; validatorId++ {
		for noId := 1; noId <= executor.cfg.Config.Topology.Operators; noId++ {
			hotkey, key, err := runtimeEvidenceActivationKeysV2(executor.roles, uint64(validatorId), uint64(noId))
			if err != nil {
				t.Fatal(err)
			}
			companion := executor.plan.ValidatorEvidence
			activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: executor.plan.ChainID, GenesisHash: [32]byte(companion.GenesisHash), Netuid: executor.cfg.Netuid,
				Coordinator: [20]byte(companion.Coordinator), SettlementVault: [20]byte(companion.SettlementVault), DeploymentIDHash: [32]byte(companion.DeploymentIDHash), PolicyHash: [32]byte(common.HexToHash(executor.cfg.PolicyHash)), Epoch: prepared.Epoch},
				Hotkey: hotkey.PublicKey(), NoID: uint64(noId), VPK: [32]byte(key[ed25519.SeedSize:]), FirstSequence: 1, NativeBlock: prepared.Native.Number, NativeHash: [32]byte(common.HexToHash(prepared.Native.Hash)), EVMBlock: prepared.Evm.Number, EVMHash: [32]byte(common.HexToHash(prepared.Evm.Hash))}
			vpkSignature, err := activation.SignVPK(key)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := activation.Digest()
			if err != nil {
				t.Fatal(err)
			}
			hotkeySignature, err := hotkey.Sign(digest[:])
			if err != nil {
				t.Fatal(err)
			}
			prepared.Members = append(prepared.Members, runtimeEvidenceActivationMemberV2{ValidatorId: uint64(validatorId), NoId: uint64(noId), ValidatorUid: uint16(validatorId), Activation: activation, VpkSignature: vpkSignature, HotkeySignature: hotkeySignature})
		}
	}
	limit, err := runtimeEvidenceProvisionLimit(executor.cfg)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := writeRuntimeEvidenceSetupV2(t.Context(), filepath.Join(executor.stateDir, "evidence-v2-setup", "prepared.json"), prepared, limit)
	if err != nil {
		t.Fatal(err)
	}
	completed := &runtimeEvidenceActivationCompletedV2{Schema: "urnetwork-sim-evidence-activation-completed-v2", PlanHash: executor.plan.PlanHash, PreparedHash: fmt.Sprintf("0x%x", sha256.Sum256(encoded)), Boundary: ChainHead{Number: 210, Hash: common.Hash{0x33}.Hex()}}
	return &runtimeEvidenceProvisionV2TestFixture{cfg: executor.cfg, plan: executor.plan, roles: executor.roles, stateDir: executor.stateDir, prepared: prepared, preparedBytes: encoded, completed: completed}
}

// Construct durable original ownership before changing only the current source
// lock. Every counterexample starts from this complete local history.
func prepareRuntimeEvidenceSetupCarryV2Test(t *testing.T, fixture *runtimeEvidenceProvisionV2TestFixture) (*SetupPlan, []JournalEntry) {
	t.Helper()
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir}
	if err := executor.retainRuntimeEvidenceInputsV2(t.Context(), fixture.prepared, fixture.preparedBytes, fixture.completed); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(fixture.plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.plan.PlanHash)+".json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	appendEntry := func(action Action, entry JournalEntry) {
		entry.DeploymentID, entry.PlanHash, entry.ActionID, entry.IntentHash = fixture.plan.DeploymentID, fixture.plan.PlanHash, action.ID, action.IntentHash
		if err := journal.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	retainRecord := func(action Action, observed map[string]any, head ChainHead) {
		record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
			OperationalRPCMode: fixture.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(fixture.cfg), SubstrateFinalized: fixture.prepared.Native, EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: observed,
			IndependentSubstrateFinalized: fixture.prepared.Native, IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: maps.Clone(observed)}
		path, hash, err := executor.persistActionPostcondition(record)
		if err != nil {
			t.Fatal(err)
		}
		appendEntry(action, JournalEntry{Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash})
	}
	key, err := crypto.HexToECDSA(fixture.roles.EVM["keeper"].PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	for index, member := range fixture.prepared.Members {
		action := actionByID(t, fixture.plan, runtimeEvidenceActivationActionId(int(member.ValidatorId), int(member.NoId)))
		appendEntry(action, JournalEntry{Stage: StageIntent})
		data, err := stabi.PackValidatorEvidenceActivation(member.Activation, member.Activation, member.VpkSignature, member.HotkeySignature)
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(index), To: &fixture.plan.ValidatorEvidence.Address, Value: new(big.Int), Gas: 100_000, GasPrice: big.NewInt(3), Data: data}), types.LatestSignerForChainID(new(big.Int).SetUint64(fixture.plan.ChainID)), key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := transaction.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(fixture.stateDir, "transactions", stringsTrim0x(transaction.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		appendEntry(action, JournalEntry{Stage: StageBroadcast, Signer: fixture.roles.EVM["keeper"].Address, Nonce: strconv.Itoa(index), TransactionHash: transaction.Hash().Hex(), RecoveryBlock: fixture.prepared.Evm.Number, RecoveryBlockHash: fixture.prepared.Evm.Hash})
		head := ChainHead{Number: fixture.prepared.Evm.Number + uint64(index) + 1, Hash: common.Hash{byte(index + 1)}.Hex()}
		appendEntry(action, JournalEntry{Stage: StageFinalized, TransactionHash: transaction.Hash().Hex(), BlockNumber: head.Number, BlockHash: head.Hash})
		digest, err := member.Activation.Digest()
		if err != nil {
			t.Fatal(err)
		}
		retainRecord(action, map[string]any{"kind": action.Kind, "target": action.Target, "prepared_hash": fixture.completed.PreparedHash, "activation_hash": common.Hash(digest).Hex(), "published_block": head.Number}, head)
	}
	boundary := actionByID(t, fixture.plan, runtimeEvidenceActivationBoundaryActionId)
	appendEntry(boundary, JournalEntry{Stage: StageIntent})
	retainRecord(boundary, map[string]any{"kind": boundary.Kind, "target": boundary.Target, "prepared_hash": fixture.completed.PreparedHash, "boundary": fixture.completed.Boundary, "pair_count": len(fixture.prepared.Members)}, fixture.completed.Boundary)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := readJournalEntries(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	lock := *fixture.cfg.Release
	lock.Repositories = maps.Clone(lock.Repositories)
	lock.Repositories["sn"] = map[string]any{"go_source_hash": "sha256:" + strings.Repeat("12", 32)}
	fixture.cfg.Release = &lock
	revised := *fixture.plan
	revised.PriorPlanHashes = append(slices.Clone(revised.PriorPlanHashes), fixture.plan.PlanHash)
	revised.ReleaseLockHash, err = canonicalHashHex(fixture.cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	revised.ResolvedInputsHash, err = resolvedInputsHash(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	revised.PlanHash, err = revised.hash()
	if err != nil || revised.PlanHash == fixture.plan.PlanHash {
		t.Fatalf("fixture did not change its source approval: %v", err)
	}
	encoded, err = json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "plan.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeEvidenceSetupSourcePlanV2(fixture.cfg, &revised, fixture.stateDir, fixture.roles, fixture.prepared, fixture.preparedBytes, fixture.completed, entries); err != nil {
		t.Fatalf("complete original fixture must be admitted before any counterexample: %v", err)
	}
	return &revised, entries
}

func TestRuntimeEvidenceSetupCarryV2ResolvesOriginalFilesAfterSourceRevision(t *testing.T) {
	t.Parallel()
	fixture := newRuntimeEvidenceSetupOriginalCarryV2Test(t)
	_, entries := prepareRuntimeEvidenceSetupCarryV2Test(t, fixture)
	current := fixture.plan.LiveFacts
	current.DeployerNonce = fixture.plan.ValidatorEvidence.DeployerNonce + 1
	revised, err := buildPlanRevisionFromFacts(fixture.cfg, fixture.stateDir, fixture.plan, &current, entries, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("actual source-only revision refused fully used original setup: %v", err)
	}
	source, err := readValidatorEvidenceHistoricalPlan(fixture.stateDir, fixture.plan.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	boundary := actionByID(t, revised, runtimeEvidenceActivationBoundaryActionId)
	original := actionByID(t, source, runtimeEvidenceActivationBoundaryActionId)
	if string(boundary.Spend.EVMGasWei) != "" || string(original.Spend.EVMGasWei) != "0" || boundary.IntentHash != original.IntentHash {
		t.Fatal("actual revision did not exercise canonical zero across the persisted boundary action")
	}
	t.Logf("%s: in-memory evm gas=%q, archived evm gas=%q, unchanged intent=%s", boundary.ID, string(boundary.Spend.EVMGasWei), string(original.Spend.EVMGasWei), boundary.IntentHash)
	encoded, err := json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "plan.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	before := validatorNamespaceTreeSnapshot(t, fixture.stateDir)
	for range 2 {
		if err := validateRuntimeEvidenceSetupRevisionV2(fixture.cfg, fixture.stateDir, revised, fixture.roles, entries); err != nil {
			t.Fatalf("completed setup refused source-only revision: %v", err)
		}
		resolved, err := runtimeEvidenceV2ResolvedConfig(fixture.cfg, fixture.stateDir)
		if err != nil {
			t.Fatalf("new plan could not render the original used setup: %v", err)
		}
		expected, _, err := runtimeEvidenceFixedInputsV2(fixture.cfg, fixture.plan, fixture.stateDir, fixture.roles, fixture.prepared, fixture.completed)
		if err != nil || !reflect.DeepEqual(resolved.Config.ValidatorEvidenceV2, expected) {
			t.Fatalf("revision replaced the original activation domain, window or fixed references: %v", err)
		}
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, fixture.stateDir)) || revised.MaximumSpend != fixture.plan.MaximumSpend || revised.SupersededSpend != fixture.plan.SupersededSpend {
		t.Fatal("read-only carry changed retained signed bytes, progress or spending")
	}
	executor := &Executor{cfg: fixture.cfg, plan: revised, roles: fixture.roles, stateDir: fixture.stateDir}
	if _, _, err := executor.prepareRuntimeEvidenceActivationsV2(t.Context(), nil); err == nil {
		t.Fatal("read-only source carry admitted a new activation mutation")
	}
}

func TestRuntimeEvidenceSetupCarryV2RejectsForeignLineageAndDomain(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"lineage", "source-wire", "configuration", "policy", "companion", "action"} {
		fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
		revised, entries := prepareRuntimeEvidenceSetupCarryV2Test(t, fixture)
		switch fault {
		case "lineage":
			revised.PriorPlanHashes = nil
		case "source-wire":
			raw, err := json.Marshal(fixture.plan)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(raw, []byte(fixture.plan.ConfigHash), []byte(common.Hash{0x71}.Hex()), 1)
			if err := os.WriteFile(filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.plan.PlanHash)+".json"), append(raw, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		case "configuration":
			revised.ConfigHash = common.Hash{0x72}.Hex()
		case "policy":
			revised.PolicyHash = common.Hash{0x73}.Hex()
		case "companion":
			companion := *revised.ValidatorEvidence
			companion.Address = common.Address{0x74}
			revised.ValidatorEvidence = &companion
		case "action":
			revised.Actions = slices.Clone(revised.Actions)
			for index := range revised.Actions {
				if revised.Actions[index].ID == runtimeEvidenceActivationActionId(2, 2) {
					revised.Actions[index].AcceptedPriorIntentHashes = []string{common.Hash{0x75}.Hex()}
				}
			}
		}
		if _, err := runtimeEvidenceSetupSourcePlanV2(fixture.cfg, revised, fixture.stateDir, fixture.roles, fixture.prepared, fixture.preparedBytes, fixture.completed, entries); err == nil {
			t.Fatalf("%s became a source owner by naming a prior hash", fault)
		}
	}
}

func TestRuntimeEvidenceSetupCarryV2RejectsPartialAndCompetingProgress(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"activation-finality", "activation-verified", "boundary-verified", "boundary-order", "new-plan-progress", "missing-prepared", "missing-completed"} {
		fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
		revised, entries := prepareRuntimeEvidenceSetupCarryV2Test(t, fixture)
		lastId := runtimeEvidenceActivationActionId(2, 2)
		switch fault {
		case "activation-finality", "activation-verified", "boundary-verified":
			entries = slices.DeleteFunc(entries, func(entry JournalEntry) bool {
				return fault == "activation-finality" && entry.ActionID == lastId && entry.Stage == StageFinalized || fault == "activation-verified" && entry.ActionID == lastId && entry.Stage == StageVerified || fault == "boundary-verified" && entry.ActionID == runtimeEvidenceActivationBoundaryActionId && entry.Stage == StageVerified
			})
		case "boundary-order":
			entries = append(slices.Clone(entries[len(entries)-2:]), entries[:len(entries)-2]...)
		case "new-plan-progress":
			action := actionByID(t, revised, lastId)
			entries = append(entries, JournalEntry{DeploymentID: revised.DeploymentID, PlanHash: revised.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent})
		}
		stateDir := runtimeReservedCreationStateTest(t, fixture.stateDir, entries)
		if fault == "missing-prepared" || fault == "missing-completed" {
			name := strings.TrimPrefix(fault, "missing-") + ".json"
			if err := os.Remove(filepath.Join(stateDir, "evidence-v2-setup", name)); err != nil {
				t.Fatal(err)
			}
		}
		entries, err := readJournalEntries(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateRuntimeEvidenceSetupRevisionV2(fixture.cfg, stateDir, revised, fixture.roles, entries); err == nil {
			t.Fatalf("%s was accepted as completed setup by the actual revision admission", fault)
		}
	}
}

func TestRuntimeEvidenceSetupCarryV2RejectsChangedReceiptAndSignedTransaction(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"raw-transaction", "signed-wrong-calldata", "prepared-hash", "completed-boundary", "postcondition-bytes", "independent-native", "independent-evm"} {
		fixture := newRuntimeEvidenceProvisionV2TestFixture(t)
		revised, entries := prepareRuntimeEvidenceSetupCarryV2Test(t, fixture)
		for _, entry := range entries {
			if fault == "signed-wrong-calldata" && entry.Stage == StageBroadcast {
				raw, err := os.ReadFile(filepath.Join(fixture.stateDir, "transactions", stringsTrim0x(entry.TransactionHash)+".rlp"))
				if err != nil {
					t.Fatal(err)
				}
				var original types.Transaction
				if err := original.UnmarshalBinary(raw); err != nil {
					t.Fatal(err)
				}
				key, err := crypto.HexToECDSA(fixture.roles.EVM["keeper"].PrivateKeyHex)
				if err != nil {
					t.Fatal(err)
				}
				data := bytes.Clone(original.Data())
				data[len(data)-1] ^= 1
				changed, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: original.Nonce(), To: original.To(), Value: original.Value(), Gas: original.Gas(), GasPrice: original.GasPrice(), Data: data}), types.LatestSignerForChainID(original.ChainId()), key)
				if err != nil {
					t.Fatal(err)
				}
				raw, err = changed.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				if err := atomicWrite(filepath.Join(fixture.stateDir, "transactions", stringsTrim0x(changed.Hash().Hex())+".rlp"), raw, 0o600); err != nil {
					t.Fatal(err)
				}
				for index := range entries {
					if entries[index].TransactionHash == entry.TransactionHash {
						entries[index].TransactionHash = changed.Hash().Hex()
					}
				}
				break
			}
			if fault == "raw-transaction" && entry.Stage == StageFinalized {
				if err := os.WriteFile(filepath.Join(fixture.stateDir, "transactions", stringsTrim0x(entry.TransactionHash)+".rlp"), []byte{1, 2, 3}, 0o600); err != nil {
					t.Fatal(err)
				}
				break
			}
			if fault == "postcondition-bytes" && entry.Stage == StageVerified {
				if err := os.WriteFile(filepath.Join(fixture.stateDir, entry.PostconditionPath), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				break
			}
			if strings.HasPrefix(fault, "independent-") && entry.Stage == StageVerified {
				record, err := readValidatorEvidenceSourcePostcondition(fixture.stateDir, fixture.cfg, fixture.plan, entry)
				if err != nil {
					t.Fatal(err)
				}
				if fault == "independent-native" {
					record.IndependentSubstrateFinalized.Number = fixture.prepared.Native.Number - 1
				} else {
					record.IndependentEVMFinalized.Number = fixture.prepared.Evm.Number
				}
				raw, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(fixture.stateDir, entry.PostconditionPath), append(raw, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
				hash, err := canonicalHashHex(record)
				if err != nil {
					t.Fatal(err)
				}
				for index := range entries {
					if entries[index].Sequence == entry.Sequence {
						entries[index].PostconditionHash = hash
					}
				}
				break
			}
		}
		completed := *fixture.completed
		if fault == "prepared-hash" {
			completed.PreparedHash = common.Hash{0x76}.Hex()
		}
		if fault == "completed-boundary" {
			completed.Boundary.Number++
		}
		if _, err := runtimeEvidenceSetupSourcePlanV2(fixture.cfg, revised, fixture.stateDir, fixture.roles, fixture.prepared, fixture.preparedBytes, &completed, entries); err == nil {
			t.Fatalf("%s changed the original signed or verified owner", fault)
		}
	}
}

func TestRuntimeEvidenceSetupCarryV2StillChecksCurrentHistoricalEligibility(t *testing.T) {
	fixture := newRuntimeEvidenceActivationRpcV2TestFixture(t)
	prepared, encoded, err := fixture.executor.prepareRuntimeEvidenceActivationsV2(t.Context(), fixture.chain)
	if err != nil {
		t.Fatal(err)
	}
	fixture.base.stateDir, fixture.base.prepared, fixture.base.preparedBytes = fixture.executor.stateDir, prepared, encoded
	// The genuine preparation uses randomized consent bytes; keep their exact
	// resulting digest in the original completion and every durable receipt.
	fixture.base.completed.PreparedHash = fmt.Sprintf("0x%x", sha256.Sum256(encoded))
	revised, _ := prepareRuntimeEvidenceSetupCarryV2Test(t, fixture.base)
	journal, err := OpenJournal(fixture.executor.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	fixture.executor.plan, fixture.executor.journal = revised, journal
	fixture.executor.cfg.OperationalEVM = fixture.executor.substrate.chain.API.Client.URL()
	func() { fixture.stateLock.Lock(); defer fixture.stateLock.Unlock(); fixture.permits[1] = false }()
	before := fixture.callCount("state_call")
	_, err = fixture.executor.runtimeEvidenceActivationPostStateV2(t.Context(), actionByID(t, revised, runtimeEvidenceActivationActionId(1, 1)), fixture.base.completed.Boundary, map[string]any{})
	if err == nil || fixture.callCount("state_call") <= before || strings.Contains(err.Error(), "prepared census or plan differs") {
		t.Fatalf("valid carry skipped or never reached actual historical eligibility: %v", err)
	}
}
