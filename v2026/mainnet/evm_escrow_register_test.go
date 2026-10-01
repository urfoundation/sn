// Escrow action regressions cross public commands, retained journals, local
// owned HTTP and real vault execution with explicit synthetic native models.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Both UID endpoints complete through genuine vault execution. The bounded
// zero-burn model returns the supplied value and leaves all prior bytes intact.
func TestEvmEscrowRegisterExecutesExactCallAndPreservesAncestors(t *testing.T) {
	for _, uid := range []uint16{0, 65535} {
		f := newEvmEscrowFixture(t, uid)
		f.prepareEscrowSigned()
		before := map[string][]byte{}
		for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile} {
			raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
			if err != nil {
				t.Fatal(err)
			}
			before[name] = raw
		}
		senderBalance := f.state.GetBalance(f.config.Plan.Actions[3].Sender).ToBig()
		if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || result.Attempts != 1 {
			t.Fatalf("escrow submission: %+v %d %s", result, code, diagnostic)
		}
		mappingReads := 0
		f.override = func(method string, params []any, result any) any {
			if method == "eth_call" {
				input := params[0].(map[string]any)
				if input["to"] == "0x0000000000000000000000000000000000000804" || input["to"] == "0x0000000000000000000000000000000000000802" {
					block := params[1].(map[string]any)
					if block["blockHash"] != f.receipt["blockHash"] || block["requireCanonical"] != true {
						t.Error("escrow mapping escaped canonical inclusion")
					}
					mappingReads++
				}
			}
			return result
		}
		result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit")
		if code != 0 || result.Status != "escrow-registered" || result.Receipt == nil || result.Receipt.ContractAddress != "" || result.Receipt.EscrowUid != uid || result.Receipt.RegistrationHash != evmEscrowRegistrationHash(f.plan, *result.Receipt) || mappingReads != 3 {
			t.Fatalf("escrow canonical completion: %+v %d %s", result, code, diagnostic)
		}
		if result.Receipt.RuntimeHash != crypto.Keccak256Hash(f.plan.Runtime).Hex() || result.Receipt.GetterHash != rootObjectHash(f.plan.Getters) || result.EscrowRegistration == nil || result.EscrowRegistration.MaximumBurnRao != 2 || result.EscrowRegistration.FundingWei != "2000000000" || result.ExecutableAction != "escrow-register" || result.PlanHash != f.config.Plan.hash() || result.VaultAddress != f.plan.Vault.Address.Hex() || result.CoordinatorAddress != f.plan.Coordinator.Address.Hex() || result.InstallationComplete || result.ActivationReady || len(result.RemainingActions) != 5 || len(f.writes) != 4 || !bytes.Equal(f.writes[3], f.raw) {
			t.Fatalf("escrow changed approved authority or postconditions: %+v", result)
		}
		if f.state.GetBalance(f.plan.Address).Sign() != 0 || f.state.GetBalance(f.config.Plan.Actions[3].Sender).ToBig().Cmp(senderBalance) != 0 {
			t.Fatal("genuine vault did not return zero-burn model surplus")
		}
		counts := maps.Clone(f.counts)
		retained, code, diagnostic := f.command("resume", "--action", "escrow-register")
		if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || retained.ReceiptObservation != "retained" || !maps.Equal(counts, f.counts) {
			t.Fatalf("escrow offline custody: %+v %d %s", retained, code, diagnostic)
		}
		if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || len(f.writes) != 4 {
			t.Fatalf("escrow completion resent: %d %s", code, diagnostic)
		}
		for name, raw := range before {
			after, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
			if err != nil || !bytes.Equal(raw, after) {
				t.Fatalf("escrow rewrote %s: %v", name, err)
			}
		}
		for _, action := range []string{"reserve-create", "vault-create", "coordinator-create"} {
			legacy, code, diagnostic := f.command("resume", "--action", action)
			raw, err := json.Marshal(legacy)
			if err != nil || code != 0 || bytes.Contains(raw, []byte("escrow_registration")) || bytes.Contains(raw, []byte("registration_hash")) || bytes.Contains(raw, []byte("escrow_uid")) {
				t.Fatalf("legacy %s acquired escrow fields: %d %s %v", action, code, diagnostic, err)
			}
		}
	}
}

// Even independently reapproved mutations cannot change the reviewed call
// domain, ABI width, one-shot nonzero cap, or exact rao-to-wei conversion.
func TestEvmEscrowRegisterRejectsChangedEnvelope(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	original := f.config.Plan.Actions[3]
	for _, fault := range []string{"selector", "trailing", "short", "high-bits", "zero-cap", "nonce", "sender", "create", "target", "underfund", "overfund"} {
		action := original
		switch fault {
		case "selector":
			action.Data = "0x00000000" + action.Data[10:]
		case "trailing":
			action.Data += "00"
		case "short":
			action.Data = action.Data[:len(action.Data)-2]
		case "high-bits":
			action.Data = action.Data[:10] + "01" + action.Data[12:]
		case "zero-cap":
			action.Data, action.ValueWei = "0x"+hex.EncodeToString(stabi.NewSTSettlementVault().PackRegisterEscrow(0)), "0"
		case "nonce":
			action.Nonce++
		case "sender":
			action.Sender = common.Address{42}
		case "create":
			action.To = nil
		case "target":
			target := crypto.CreateAddress(action.Sender, 2)
			action.To = &target
		case "underfund":
			action.ValueWei = "1999999999"
		case "overfund":
			action.ValueWei = "2000000001"
		}
		f.config.Plan.Actions[3] = action
		f.config.Plan.MaximumTotalWei = "2170000001"
		f.publishConfig()
		if _, code, diagnostic := f.command("plan", "--action", "escrow-register"); code != 2 || !strings.Contains(diagnostic, "selected action") || len(f.counts) != 0 {
			t.Fatalf("escrow envelope %s accepted: %d %s", fault, code, diagnostic)
		}
	}
}

// Maximum uint64 rao must multiply without truncation. The complete graph still
// rejects one wei below its original value-plus-gas maximum liability.
func TestEvmEscrowRegisterKeepsFullWidthCapAndGraphLiability(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	cap := ^uint64(0)
	funding := new(big.Int).Mul(new(big.Int).SetUint64(cap), big.NewInt(1_000_000_000))
	f.config.Plan.Actions[3].Data = "0x" + hex.EncodeToString(stabi.NewSTSettlementVault().PackRegisterEscrow(cap))
	f.config.Plan.Actions[3].ValueWei = funding.String()
	maximum := new(big.Int).Add(funding, big.NewInt(170_000_000))
	f.config.Plan.MaximumTotalWei = maximum.String()
	f.publishConfig()
	plan, err := selectEvmCreatePlan(context.Background(), f.plan, "escrow-register", f.configPath)
	if err != nil || plan.EscrowRegistration.MaximumBurnRao != cap || plan.EscrowRegistration.FundingWei != funding.String() {
		t.Fatalf("full-width cap narrowed: %+v %v", plan.EscrowRegistration, err)
	}
	f.config.Plan.MaximumTotalWei = maximum.Sub(maximum, big.NewInt(1)).String()
	if err := f.config.validateStructure(); err == nil {
		t.Fatal("full graph accepted insufficient total liability")
	}
}

// Review fields derive from the existing action-three payload and preserve the
// entire graph's signing bytes without opening a future journal directory.
func TestEvmEscrowRegisterPreviewPreservesApprovedGraph(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.config.Signature = ""
	f.config.Plan.RunDirectory = filepath.Join(f.config.Plan.RunDirectory, "future-run")
	raw, err := json.Marshal(f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), []string{"bootstrap-contracts", "preview", "--action", "escrow-register", "--config", f.configPath}, &stdout, &stderr)
	var preview evmPhasePreview
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil || code != 0 || preview.PlanHash != f.config.Plan.hash() || preview.ApprovalVerified || preview.InstallationComplete || preview.ExecutableAction != "escrow-register" || preview.EscrowRegistration == nil || preview.EscrowRegistration.FundingWei != "2000000000" || preview.Plan.Actions[3].Data != f.config.Plan.Actions[3].Data || preview.VaultAddress != f.config.Plan.Actions[3].To.Hex() || preview.CoordinatorAddress != crypto.CreateAddress(f.config.Plan.Actions[2].Sender, 2).Hex() {
		t.Fatalf("escrow preview: %+v %d %v %s", preview, code, err, stderr.String())
	}
	if _, err := os.Lstat(f.config.Plan.RunDirectory); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("escrow preview acquired custody: %v", err)
	}
}

// Each malformed event or call identity refuses completion through the public
// adapter. Clearing the fault recovers the one executed original transaction.
func TestEvmEscrowRegisterRequiresExactReceiptAndEvent(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowSigned()
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for _, fault := range []string{"to", "from", "create-address", "zero-create-address", "missing-logs", "null-logs", "empty-logs", "duplicate-event", "event-address", "event-topic", "event-hotkey", "event-extra-topic", "event-data-width", "event-uid-width", "event-removed", "event-missing-removed", "event-block", "event-transaction", "event-block-number", "event-transaction-index", "event-missing-index"} {
		f.override = func(method string, params []any, result any) any {
			if method != "eth_getTransactionReceipt" || params[0] != f.tx.Hash().Hex() {
				return result
			}
			changed := maps.Clone(result.(map[string]any))
			log := maps.Clone(changed["logs"].([]map[string]any)[0])
			log["topics"] = append([]string(nil), log["topics"].([]string)...)
			changed["logs"] = []map[string]any{log}
			switch fault {
			case "to", "from":
				changed[fault] = common.Address{42}.Hex()
			case "create-address":
				changed["contractAddress"] = f.plan.Address.Hex()
			case "zero-create-address":
				changed["contractAddress"] = common.Address{}.Hex()
			case "missing-logs":
				delete(changed, "logs")
			case "null-logs":
				changed["logs"] = nil
			case "empty-logs":
				changed["logs"] = []any{}
			case "duplicate-event":
				duplicate := maps.Clone(log)
				duplicate["logIndex"] = "0x1"
				changed["logs"] = []map[string]any{log, duplicate}
			case "event-address":
				log["address"] = common.Address{42}.Hex()
			case "event-topic":
				log["topics"].([]string)[0] = common.Hash{42}.Hex()
			case "event-hotkey":
				log["topics"].([]string)[1] = f.config.Plan.ReserveHotkey
			case "event-extra-topic":
				log["topics"] = append(log["topics"].([]string), common.Hash{}.Hex())
			case "event-data-width":
				log["data"] = "0x00"
			case "event-uid-width":
				log["data"] = "0x01" + strings.Repeat("00", 31)
			case "event-removed":
				log["removed"] = true
			case "event-missing-removed":
				delete(log, "removed")
			case "event-block":
				log["blockHash"] = common.Hash{42}.Hex()
			case "event-transaction":
				log["transactionHash"] = common.Hash{42}.Hex()
			case "event-block-number":
				log["blockNumber"] = "0x1"
			case "event-transaction-index":
				log["transactionIndex"] = "0x1"
			case "event-missing-index":
				delete(log, "logIndex")
			}
			return changed
		}
		if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 1 {
			t.Fatalf("escrow receipt %s accepted: %d %s", fault, code, diagnostic)
		}
		retained, code, diagnostic := f.command("resume", "--action", "escrow-register")
		if code != 0 || retained.Receipt != nil || retained.Attempts != 1 || len(f.writes) != 4 {
			t.Fatalf("escrow receipt %s changed custody: %+v %d %s", fault, retained, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || result.Status != "escrow-registered" || len(f.writes) != 4 {
		t.Fatalf("escrow receipt recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Runtime and all thirteen vault getters are checked at this call's inclusion,
// separately from the earlier vault constructor's successful historical state.
func TestEvmEscrowRegisterRequiresEveryVaultPostcondition(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowSigned()
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for fault := -1; fault < len(f.plan.Getters); fault++ {
		f.override = func(method string, params []any, result any) any {
			if method == "eth_getCode" && fault == -1 && params[0] == f.plan.Address.Hex() && params[1].(map[string]any)["blockHash"] == f.receipt["blockHash"] {
				return "0x00"
			}
			if method == "eth_call" && fault >= 0 && params[1].(map[string]any)["blockHash"] == f.receipt["blockHash"] {
				input := params[0].(map[string]any)
				if input["to"] == f.plan.Address.Hex() && input["data"] == f.plan.Getters[fault].Data {
					return "0x00"
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "differs") || len(f.writes) != 4 {
			t.Fatalf("escrow vault postcondition %d accepted: %d %s", fault, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online"); code != 0 || result.Status != "escrow-registered" {
		t.Fatalf("escrow vault postcondition recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Exact event UID, netuid and hotkey must reach all three reviewed precompile
// selectors at one inclusion. Missing archive reads cannot fabricate mappings.
func TestEvmEscrowRegisterRequiresCanonicalNativeMappings(t *testing.T) {
	f := newEvmEscrowFixture(t, 65535)
	f.prepareEscrowSigned()
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for _, signature := range []string{"getUid(uint16,bytes32)", "getHotkey(uint16,uint16)", "getColdkey(uint16,uint16)"} {
		for _, unavailable := range []bool{false, true} {
			observed := false
			f.override = func(method string, params []any, result any) any {
				if method == "eth_call" {
					input := params[0].(map[string]any)
					selector := "0x" + hex.EncodeToString(crypto.Keccak256([]byte(signature))[:4])
					if strings.HasPrefix(input["data"].(string), selector) {
						second := evmEscrowTestWord(65535)[2:]
						if signature == "getUid(uint16,bytes32)" {
							second = f.plan.EscrowRegistration.Hotkey[2:]
						}
						if input["data"] != selector+evmEscrowTestWord(25)[2:]+second {
							t.Error("escrow mapping changed approved arguments")
						}
						observed = true
						if unavailable {
							return mappingFixtureRpcError{code: -32602}
						}
						return "0x" + strings.Repeat("00", 32)
					}
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 1 || !observed || !unavailable && !strings.Contains(diagnostic, "registration mapping differs") || unavailable && strings.Contains(diagnostic, "registration mapping differs") || len(f.writes) != 4 {
				t.Fatalf("escrow mapping %s unavailable=%v accepted: %d %s", signature, unavailable, code, diagnostic)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online"); code != 0 || result.Status != "escrow-registered" {
		t.Fatalf("escrow mapping recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Existing code is required at this call target. Both finalized and pending
// views must retain the unregistered constructor state and absent native UID.
func TestEvmEscrowRegisterAdmitsOnlyUnregisteredCurrentVault(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowSigned()
	var currentHash string
	for hash, tx := range f.history.transactions {
		if tx.Nonce() == 2 {
			currentHash = hash
		}
	}
	for _, pending := range []bool{false, true} {
		for fault := -2; fault < len(f.plan.Vault.Getters); fault++ {
			observed := false
			f.override = func(method string, params []any, result any) any {
				if method != "eth_getCode" && method != "eth_call" {
					return result
				}
				block, historical := params[1].(map[string]any)
				if pending && params[1] != "pending" || !pending && (!historical || block["blockHash"] != currentHash) {
					return result
				}
				if fault == -2 && method == "eth_getCode" && params[0] == f.plan.Address.Hex() {
					observed = true
					return "0x"
				}
				if method == "eth_call" {
					input := params[0].(map[string]any)
					if fault == -1 && input["to"] == "0x0000000000000000000000000000000000000804" {
						observed = true
						return evmEscrowTestWord(1) + evmEscrowTestWord(0)[2:]
					}
					if fault >= 0 && input["to"] == f.plan.Address.Hex() && input["data"] == f.plan.Vault.Getters[fault].Data {
						observed = true
						return "0x00"
					}
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 1 || !observed || len(f.writes) != 3 {
				t.Fatalf("escrow current fault %d pending=%v accepted: %d %s", fault, pending, code, diagnostic)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || result.Attempts != 1 || len(f.writes) != 4 {
		t.Fatalf("escrow current target recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Included but unauthenticated implementation work cannot open action three.
func TestEvmEscrowRegisterRequiresCompletedCoordinator(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	refused := func(stage string) {
		t.Helper()
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("apply", "--action", "escrow-register"); code != 3 {
			t.Fatalf("%s predecessor admitted escrow: %d %s", stage, code, diagnostic)
		}
		if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmEscrowRegisterStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) || !maps.Equal(counts, f.counts) {
			t.Fatalf("%s predecessor acquired escrow custody: %v", stage, err)
		}
	}
	refused("absent")
	f.prepareCoordinatorSigned()
	refused("signed")
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	refused("included")
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("apply", "--action", "escrow-register"); code != 0 || result.Status != "signature-awaiting-import" {
		t.Fatalf("completed implementation did not unlock escrow: %+v %d %s", result, code, diagnostic)
	}
}

// All three historical executions are checked before any fresh escrow effect.
func TestEvmEscrowRegisterReauditsThreePredecessors(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowSigned()
	path := filepath.Join(f.config.Plan.RunDirectory, evmEscrowRegisterStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for ancestor := 0; ancestor < 3; ancestor++ {
		address := f.plan.priorPlan(ancestor).Address.Hex()
		f.override = func(method string, params []any, result any) any {
			if method == "eth_call" && params[0].(map[string]any)["to"] == address {
				return "0x00"
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "constructor getter differs") || len(f.writes) != 3 {
			t.Fatalf("escrow skipped ancestor %d: %d %s", ancestor, code, diagnostic)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("escrow ancestor fault changed custody: %v", err)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("escrow ancestor recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Each ancestor can change after all three audits have completed. Both initial
// and refreshed selected-action admission must retain those checkpoints.
func TestEvmEscrowRegisterFencesThreePredecessorCheckpoints(t *testing.T) {
	for _, number := range []uint64{101, 102, 103} {
		for _, refresh := range []bool{false, true} {
			f := newEvmEscrowFixture(t, 0)
			f.prepareEscrowSigned()
			originalHash := f.hashes[number]
			original := f.headers[originalHash]
			fork, forkHash := evmTestNativeHeader(t, original.ParentHash, number, append(append([]string(nil), original.Digest.Logs...), "0x0000"))
			f.headers[forkHash] = fork
			storageObserved, targetObserved, changed := false, false, false
			f.override = func(method string, params []any, result any) any {
				if method == "eth_getStorageAt" && params[0] == f.plan.Coordinator.Address.Hex() && params[1] == f.plan.Coordinator.Storage[1].Slot {
					storageObserved = true
				}
				if method == "eth_call" && params[1] == "pending" && params[0].(map[string]any)["to"] == "0x0000000000000000000000000000000000000804" {
					targetObserved = true
				}
				if method == "chain_getFinalizedHead" && storageObserved && (!refresh || targetObserved) && !changed {
					changed = true
					f.hashes[number] = forkHash
					return f.hashes[f.head]
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 1 || !changed || !strings.Contains(diagnostic, "ancestry") || len(f.writes) != 3 {
				t.Fatalf("escrow checkpoint %d refresh=%v accepted: %d %s", number, refresh, code, diagnostic)
			}
			f.override = nil
			f.hashes[number] = originalHash
			if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || result.Attempts != 1 {
				t.Fatalf("escrow checkpoint recovery: %+v %d %s", result, code, diagnostic)
			}
		}
	}
}

// A lost acknowledgement preserves the original fourth signature and counted
// attempt. Canonical recovery never sends a replacement or duplicates the call.
func TestEvmEscrowRegisterLostReplyRecoversOriginalCall(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowSigned()
	f.loseReply = true
	if _, code, _ := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 1 || len(f.writes) != 4 {
		t.Fatalf("lost escrow reply retried or acknowledged: %d", code)
	}
	retained, code, diagnostic := f.command("resume", "--action", "escrow-register")
	if code != 0 || retained.Attempts != 1 || retained.Receipt != nil || retained.TransactionHash != f.tx.Hash().Hex() {
		t.Fatalf("uncertain escrow liability lost: %+v %d %s", retained, code, diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit")
	if code != 0 || result.Status != "escrow-registered" || result.TransactionHash != retained.TransactionHash || result.Attempts != 1 || len(f.writes) != 4 || !bytes.Equal(f.writes[3], f.raw) {
		t.Fatalf("uncertain escrow replaced original intent: %+v %d %s", result, code, diagnostic)
	}
}

// Three predecessor sends have spent three of the four original graph attempts.
func TestEvmEscrowRegisterKeepsCumulativeGraphAttempts(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.config.Plan.MaximumAttempts = 4
	f.publishConfig()
	f.prepareEscrowSigned()
	f.mine = false
	for attempt := 0; attempt < 3; attempt++ {
		result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit")
		if code != 0 || result.Attempts != 1 || attempt > 0 && result.Status != "attempt-allowance-exhausted" {
			t.Fatalf("escrow renewed graph allowance: %+v %d %s", result, code, diagnostic)
		}
	}
	if len(f.writes) != 4 {
		t.Fatal("escrow exceeded cumulative approved attempts")
	}
}

// The value cap and every later same-sender gas/value reservation retain their
// funding. The next proxy reservation remains unexecuted and uninterpreted.
func TestEvmEscrowRegisterPreservesCurrentAndFutureFunding(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "proxy-create", Sender: f.config.Plan.Actions[0].Sender, Nonce: 4, Data: "0x00", ValueWei: "3000000000", Gas: 1_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "5180000000"
	f.publishConfig()
	f.prepareEscrowSigned()
	for _, balance := range []uint64{9_999_999, 2_009_999_999, 5_019_999_999} {
		f.override = func(method string, _ []any, result any) any {
			if method == "eth_getBalance" {
				return fmt.Sprintf("0x%x", balance)
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "maximum liability") || len(f.writes) != 3 {
			t.Fatalf("escrow balance %d spent reserved funding: %d %s", balance, code, diagnostic)
		}
	}
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 5_020_000_000)
		}
		return result
	}
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("escrow exact sufficient funding refused: %+v %d %s", result, code, diagnostic)
	}
}

// Failed durable attempt acknowledgement poisons transport. Reopening retains
// that spent allowance alongside all three ancestors' previous submissions.
func TestEvmEscrowRegisterAmbiguousPublicationRetainsAttempt(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowSigned()
	f.mine = false
	stores, records := f.openEscrowAncestors()
	store, err := openEvmEscrowActionStore(f.plan, records[0], records[1], records[2], false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.client.httpClient.CloseIdleConnections()
	owner, err := newEvmEscrowRegisterOwner(f.plan, store, stores[0], stores[1], stores[2], chain)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	store.syncDirectory = func(*os.File) error {
		count++
		if count == 2 {
			return errors.New("synthetic escrow attempt publication interruption")
		}
		return nil
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil {
		t.Fatal("ambiguous escrow publication acknowledged")
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil || len(f.writes) != 3 {
		t.Fatal("poisoned escrow owner reached transport")
	}
	store.close()
	for _, prior := range stores {
		prior.close()
	}
	result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit")
	if code != 0 || result.Attempts != 2 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 4 {
		t.Fatalf("escrow reopen renewed attempt: %+v %d %s", result, code, diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || result.Status != "attempt-allowance-exhausted" || result.Attempts != 2 || len(f.writes) != 4 {
		t.Fatalf("escrow ambiguous publication exceeded graph: %+v %d %s", result, code, diagnostic)
	}
}

// Both initial claim boundaries recover under all four locks; losing a completed
// child never opens a fresh signature or submission allowance.
func TestEvmEscrowRegisterClaimRecoveryKeepsFourLocks(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "record-synced"} {
		f := newEvmEscrowFixture(t, 0)
		f.prepareEscrowPrerequisites()
		stores, records := f.openEscrowAncestors()
		store, err := openEvmEscrowActionStore(f.plan, records[0], records[1], records[2], true, func(stage string) error {
			if stage == boundary {
				return errors.New("synthetic escrow initial claim interruption")
			}
			return nil
		})
		if err == nil || store != nil {
			t.Fatalf("escrow %s interruption acknowledged", boundary)
		}
		store, err = openEvmEscrowActionStore(f.plan, records[0], records[1], records[2], false, nil)
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.load()
		if err != nil || record.Signed != "" || record.Attempts != 0 || record.PredecessorHash != rootObjectHash(records[2]) {
			t.Fatalf("escrow claim recovery changed custody: %+v %v", record, err)
		}
		for index := 0; index < 4; index++ {
			predecessor := ""
			if index > 0 {
				predecessor = rootObjectHash(records[index-1])
			}
			if other, err := openEvmSelectedActionStore(f.config, index, predecessor, false, nil); err == nil {
				other.close()
				t.Fatalf("escrow lost lock %d", index)
			}
		}
		store.close()
		if err := os.Remove(filepath.Join(f.config.Plan.RunDirectory, evmEscrowRegisterStateFile)); err != nil {
			t.Fatal(err)
		}
		for _, create := range []bool{false, true} {
			if other, err := openEvmEscrowActionStore(f.plan, records[0], records[1], records[2], create, nil); err == nil {
				other.close()
				t.Fatal("lost escrow child renewed allowance")
			}
		}
		for _, prior := range stores {
			prior.close()
		}
	}
}

// Self-consistently hashed changed ancestors still break the exact sealed chain.
// No historical journal is learned from current RPC or silently rewritten.
func TestEvmEscrowRegisterRejectsChangedThreeAncestorLineage(t *testing.T) {
	for ancestor := 0; ancestor < 3; ancestor++ {
		f := newEvmEscrowFixture(t, 0)
		f.prepareEscrowSigned()
		stores, records := f.openEscrowAncestors()
		record := records[ancestor]
		record.Attempts++
		record.ContentHash = ""
		record.ContentHash = rootObjectHash(record)
		if err := stores[ancestor].save(record); err != nil {
			t.Fatal(err)
		}
		for _, store := range stores {
			store.close()
		}
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 3 || !strings.Contains(diagnostic, "marker differs") || !maps.Equal(counts, f.counts) || len(f.writes) != 3 {
			t.Fatalf("escrow learned changed ancestor %d: %d %s", ancestor, code, diagnostic)
		}
	}
}

// A genuine exhausted call rolls back its one-shot flag and event while the
// original sender nonce stays consumed. No replacement is inferred on reopen.
func TestEvmEscrowRegisterRevertRetainsConsumedNonce(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.config.Plan.Actions[3].Gas = 21_000
	f.publishConfig()
	f.prepareEscrowSigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit")
	if code != 0 || result.Status != "escrow-registration-reverted-nonce-consumed" || result.Receipt == nil || result.Receipt.Status != 0 || result.Receipt.RuntimeHash != "" || result.Receipt.GetterHash != "" || result.Receipt.RegistrationHash != "" || len(f.writes) != 4 || len(f.receipt["logs"].([]map[string]any)) != 0 || f.state.GetNonce(f.config.Plan.Actions[3].Sender) != 4 {
		t.Fatalf("escrow revert acquired success or another nonce: %+v %d %s", result, code, diagnostic)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "escrow-register")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || !maps.Equal(counts, f.counts) {
		t.Fatalf("escrow reverted custody changed: %+v %d %s", retained, code, diagnostic)
	}
}

// Consuming the implementation nonce through a failed CREATE cannot authorize
// a call that depends on its exact disabled runtime and constructor state.
func TestEvmEscrowRegisterRefusesRevertedCoordinator(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.config.Plan.Actions[2].Gas = 200_000
	f.publishConfig()
	f.prepareCoordinatorSigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "coordinator-create", "--online"); code != 0 || result.Status != "create-reverted-nonce-consumed" {
		t.Fatalf("implementation did not genuinely revert: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("apply", "--action", "escrow-register"); code != 3 || !strings.Contains(diagnostic, "successful coordinator") {
		t.Fatalf("reverted implementation admitted escrow: %d %s", code, diagnostic)
	}
	if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmEscrowRegisterStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reverted implementation opened escrow journal: %v", err)
	}
}

// The receipt is durably authoritative before convenience output is emitted.
func TestEvmEscrowRegisterOutputFailureRetainsCompletion(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowSigned()
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	args := []string{"bootstrap-contracts", "resume", "--action", "escrow-register", "--config", f.configPath, "--run-dir", f.config.Plan.RunDirectory, "--accept-plan-hash", f.config.Plan.hash(), "--online", "--submit"}
	var stderr bytes.Buffer
	if code := runMain(context.Background(), args, bootstrapRootFailedWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output failed") {
		t.Fatalf("escrow output fault occurred before retention: %d %s", code, stderr.String())
	}
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register"); code != 0 || result.Status != "escrow-registered" || result.Receipt == nil || result.Receipt.RegistrationHash == "" || len(f.writes) != 4 {
		t.Fatalf("output loss erased escrow completion: %+v %d %s", result, code, diagnostic)
	}
}

// No ancestor signature or private custody path can be imported as action three.
func TestEvmEscrowRegisterRejectsOtherSignatureAndCustodyAliases(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowPrerequisites()
	if _, code, diagnostic := f.command("apply", "--action", "escrow-register"); code != 0 {
		t.Fatal(diagnostic)
	}
	path := filepath.Join(f.config.Plan.RunDirectory, "coordinator-create.signed.bin")
	_, hash, err := readBootstrapRootFile(context.Background(), path, 128*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.Contains(diagnostic, "signed-byte envelope") {
		t.Fatalf("implementation signature filled escrow custody: %d %s", code, diagnostic)
	}
	for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile} {
		path = filepath.Join(f.config.Plan.RunDirectory, name)
		_, hash, err = readBootstrapRootFile(context.Background(), path, 128*1024)
		if err != nil {
			t.Fatal(err)
		}
		if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.Contains(diagnostic, "aliases custody") {
			t.Fatalf("escrow signature input aliased %s: %d %s", name, code, diagnostic)
		}
	}
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register"); code != 0 || result.Status != "signature-awaiting-import" || result.Attempts != 0 {
		t.Fatalf("escrow invalid import changed custody: %+v %d %s", result, code, diagnostic)
	}
}

// Expiry and consumed/pending nonces preserve exact funded signed liability and
// cannot authorize a new nonce, another signature, or an extra graph attempt.
func TestEvmEscrowRegisterExpiryAndNonceMovementRetainLiability(t *testing.T) {
	for _, fault := range []string{"expiry", "confirmed", "pending"} {
		f := newEvmEscrowFixture(t, 0)
		if fault == "expiry" {
			f.config.Plan.ValidThroughNative = 103
			f.publishConfig()
		}
		f.prepareEscrowSigned()
		status := "approval-expired-signed-liability-retained"
		if fault == "expiry" {
			f.advanceEmpty()
		} else {
			status = "nonce-consumed-receipt-unresolved"
			if fault == "pending" {
				status = "pending-nonce-unresolved"
			}
			f.override = func(method string, params []any, result any) any {
				if method == "eth_getTransactionCount" && (fault == "confirmed" || params[1] == "pending") {
					return "0x4"
				}
				return result
			}
		}
		result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit")
		if code != 0 || result.Status != status || result.Attempts != 0 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 3 {
			t.Fatalf("escrow %s replaced liability: %+v %d %s", fault, result, code, diagnostic)
		}
	}
}

// Historical successful registration remains observable after later runtime
// change and expiry; fresh admission still uses the separately approved tuple.
func TestEvmEscrowRegisterHistoricalCompletionAfterRuntimeChange(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.config.Plan.ValidThroughNative = 104
	f.publishConfig()
	f.prepareEscrowSigned()
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	f.advanceEmpty()
	f.override = func(method string, params []any, result any) any {
		if method == "state_getRuntimeVersion" && params[0] == f.hashes[f.head] {
			version := f.config.Plan.Runtime.RuntimeVersion
			version.SpecVersion++
			return version
		}
		return result
	}
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 || result.Status != "escrow-registered" || result.Receipt == nil || result.Receipt.NativeNumber != 104 || len(f.writes) != 4 {
		t.Fatalf("later runtime erased escrow receipt: %+v %d %s", result, code, diagnostic)
	}
}

// Locally recomputed record hashes do not replace the registration semantic
// digest or allow escrow-only metadata to leak into historical CREATE receipts.
func TestEvmEscrowRegisterRetainedReceiptNeedsRegistrationDigest(t *testing.T) {
	f := newEvmEscrowFixture(t, 0)
	f.prepareEscrowSigned()
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	stores, records := f.openEscrowAncestors()
	store, err := openEvmEscrowActionStore(f.plan, records[0], records[1], records[2], false, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	record.Receipt.EscrowUid = 1
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	store.close()
	for _, prior := range stores {
		prior.close()
	}
	counts := maps.Clone(f.counts)
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register"); code != 3 || !strings.Contains(diagnostic, "registration postconditions differ") || !maps.Equal(counts, f.counts) {
		t.Fatalf("retained escrow event changed without digest: %d %s", code, diagnostic)
	}
	legacy := records[0]
	legacy.Receipt.RegistrationHash = record.Receipt.RegistrationHash
	legacy.ContentHash = ""
	legacy.ContentHash = rootObjectHash(legacy)
	if err := legacy.validateForAction(f.config, 0); err == nil {
		t.Fatal("legacy receipt accepted additive escrow fields")
	}
}
