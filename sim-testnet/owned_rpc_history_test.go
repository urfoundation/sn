package main

import (
	"bytes"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

type ownedRPCHistoryTest struct {
	executor *Executor
	source   *SetupPlan
	action   Action
	entry    JournalEntry
	record   *ActionPostcondition
}

func newOwnedRPCHistoryTest(t *testing.T, nativeTransaction bool) ownedRPCHistoryTest {
	t.Helper()
	sourceCfg := ownedRPCSourceConfigTest(t)
	roles, err := BuildRoleSecrets(sourceCfg)
	if err != nil {
		t.Fatal(err)
	}
	public, err := derivePublicRoles(sourceCfg)
	if err != nil {
		t.Fatal(err)
	}
	source, err := buildPlan(sourceCfg, testSetupFacts(), public, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := prepareOwnedRPCConfiguration(sourceCfg, "192.168.1.162:9944")
	if err != nil {
		t.Fatal(err)
	}
	current := *source
	current.PriorPlanHashes = append([]string{source.PlanHash}, source.PriorPlanHashes...)
	current.OwnedRPCAuthority = cfg.ownedRPCAuthority
	current.ResolvedInputsHash, err = resolvedInputsHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "owner")
	if err := ensurePrivateDir(stateDir); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []*SetupPlan{source, &current} {
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(plan.PlanHash)+".json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	executor := &Executor{cfg: cfg, plan: &current, roles: roles, stateDir: stateDir, journal: journal}
	action := actionByID(t, source, "evm.fund-owner")
	usable, err := strconv.ParseUint(action.Parameters["usable_evm_rao"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	ed := source.LiveFacts.ExistentialDepositRao
	balance := new(big.Int).Mul(new(big.Int).SetUint64(usable+100), new(big.Int).SetUint64(evmWeiPerRao))
	observed := fundingPostconditionObservation(action, usable, ed, balance)
	independent, err := cloneObservedPostState(observed)
	if err != nil {
		t.Fatal(err)
	}
	record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: rpcModePublicOverride, IndependentRPC: false, SubstrateFinalized: testEVMHead(9, 0x09), EVMFinalized: testEVMHead(10, 0x10), EVMHashDomain: "evm-rpc", Observed: observed,
		IndependentSubstrateFinalized: testEVMHead(9, 0x09), IndependentEVMFinalized: testEVMHead(10, 0x10), IndependentEVMHashDomain: "evm-rpc", IndependentObserved: independent}
	path, hash, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}
	if nativeTransaction {
		for _, stage := range []JournalStage{StageIntent, StageBroadcast, StageFinalized} {
			row := entry
			row.Stage, row.PostconditionPath, row.PostconditionHash = stage, "", ""
			if stage != StageIntent {
				row.TransactionHash, row.BlockNumber, row.BlockHash = testEVMHead(1, 0x11).Hash, 9, record.SubstrateFinalized.Hash
			}
			if stage == StageBroadcast {
				row.Signer, row.Nonce = source.Owner, "0"
				row.RecoveryBlock, row.RecoveryBlockHash = 8, testEVMHead(8, 0x08).Hash
			}
			if err := journal.Append(row); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	return ownedRPCHistoryTest{executor: executor, source: source, action: action, entry: entry, record: record}
}

func TestOwnedRPCHistoryAuthenticatesOriginalApprovalWithoutRelabelingReceipts(t *testing.T) {
	fixture := newOwnedRPCHistoryTest(t, false)
	e := fixture.executor
	path := filepath.Join(e.stateDir, filepath.FromSlash(fixture.entry.PostconditionPath))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []*SetupPlan{fixture.source, e.plan} {
		got, err := readValidatorEvidenceSourcePostcondition(e.stateDir, e.cfg, scope, fixture.entry)
		if err != nil || got.OperationalRPCMode != rpcModePublicOverride || got.IndependentRPC {
			t.Fatalf("original source assurance was not retained: %v", err)
		}
	}
	got, err := e.readPersistedPostcondition(fixture.entry)
	if err != nil || got.OperationalRPCMode != rpcModePublicOverride || got.IndependentRPC {
		t.Fatalf("strict generic history rejected its exact original approval: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("history admission rewrote the original receipt")
	}
	for _, fault := range []string{"implicit-private", "unbound-origin", "wrong-authority", "current-public-receipt", "false-independent-clone", "unapproved-plan"} {
		t.Run(fault, func(t *testing.T) {
			cfg, record := *e.cfg, *fixture.record
			switch fault {
			case "implicit-private":
				cfg.ownedRPCAuthority = ""
			case "unbound-origin":
				cfg.ObjectStoreHost = "another-owner.example"
			case "wrong-authority":
				cfg.OperationalEVM = "http://192.168.1.163:9944"
			case "current-public-receipt":
				record.PlanHash = e.plan.PlanHash
			case "false-independent-clone":
				record.IndependentEVMFinalized = testEVMHead(11, 0x11)
			case "unapproved-plan":
				record.PlanHash = testEVMHead(2, 0x22).Hash
			}
			if err := historicalPostconditionRPCIdentity(e.stateDir, &cfg, e.plan, &record); err == nil {
				t.Fatalf("%s reused the original public receipt", fault)
			}
		})
	}
	sourcePath := filepath.Join(e.stateDir, "plans", stringsTrim0x(fixture.source.PlanHash)+".json")
	original, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	changed := *fixture.source
	changed.ResolvedInputsHash = testEVMHead(3, 0x33).Hash
	raw, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(sourcePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.readPersistedPostcondition(fixture.entry); err == nil {
		t.Fatal("changed archived authority reused the old plan hash")
	}
	if err := atomicWrite(sourcePath, original, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A successful read of the historical label never substitutes for the fresh
// independent RPC result. Both actual HTTP readers replay the original block;
// disagreement, missing readers and a noncanonical peer all reject acceptance.
func TestOwnedRPCHistoryReplaysOriginalFundingThroughBothCurrentReaders(t *testing.T) {
	fixture := newOwnedRPCHistoryTest(t, false)
	e := fixture.executor
	record, err := e.readPersistedPostcondition(fixture.entry)
	if err != nil {
		t.Fatal(err)
	}
	usable, err := strconv.ParseUint(fixture.action.Parameters["usable_evm_rao"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	wei := func(rao uint64) *big.Int {
		return new(big.Int).Mul(new(big.Int).SetUint64(rao), new(big.Int).SetUint64(evmWeiPerRao))
	}
	for _, fault := range []string{"none", "missing-independent", "peer-balance", "peer-reorg"} {
		t.Run(fault, func(t *testing.T) {
			first := &historicalFundingRPCFixture{t: t, finalized: testEVMHead(12, 0x12), historical: record.EVMFinalized, currentWei: wei(1), historicalWei: wei(usable + 100)}
			second := &historicalFundingRPCFixture{t: t, finalized: testEVMHead(12, 0x12), historical: record.EVMFinalized, currentWei: wei(1), historicalWei: wei(usable + 100)}
			if fault == "peer-balance" {
				second.historicalWei = wei(usable + 101)
			}
			if fault == "peer-reorg" {
				second.historical.Hash = testEVMHead(10, 0x44).Hash
			}
			firstServer, secondServer := httptest.NewServer(first), httptest.NewServer(second)
			defer firstServer.Close()
			defer secondServer.Close()
			firstClient, err := ethclient.Dial(firstServer.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer firstClient.Close()
			secondClient, err := ethclient.Dial(secondServer.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer secondClient.Close()
			current := *e
			current.deployer, current.independentEVM = &EvmTxManager{client: firstClient}, secondClient
			if fault == "missing-independent" {
				current.independentEVM = nil
			}
			err = current.verifyConsumedActionHistory(t.Context(), fixture.action, fixture.entry, record, nil)
			if fault == "none" {
				if err != nil || len(first.balanceBlocks) != 1 || len(second.balanceBlocks) != 1 || first.balanceBlocks[0] != "0xa" || second.balanceBlocks[0] != "0xa" {
					t.Fatalf("strict original funding replay did not use both current observers: %v", err)
				}
			} else if err == nil {
				t.Fatalf("%s independent conflict was accepted", fault)
			}
		})
	}
}

func TestOwnedRPCHistoryNativeReplayCannotBorrowOneReader(t *testing.T) {
	fixture := newOwnedRPCHistoryTest(t, true)
	e := fixture.executor
	record, err := e.readPersistedPostcondition(fixture.entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, shared := range []bool{false, true} {
		current := *e
		current.substrate = &SubstrateManager{}
		if shared {
			current.independentSubstrate = current.substrate
		}
		if err := current.verifyConsumedActionHistory(t.Context(), fixture.action, fixture.entry, record, nil); err == nil || !strings.Contains(err.Error(), "independent Substrate reader") {
			t.Fatalf("one native reader could upgrade original public assurance: %v", err)
		}
	}
}
