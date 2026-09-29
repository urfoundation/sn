// Evidence CREATE tests cross the real command, signed approval, local HTTP and
// reviewed EVM constructor while preserving the separate unimplemented anchor.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"github.com/urfoundation/sn/stabi"
)

// Genuine constructor execution and six exact getters agree with an independently
// encoded domain; all seven ancestor bytes and original review outputs survive.
func TestEvmEvidenceCreateExecutesExactConstructorAndPreservesAncestors(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	before := map[string][]byte{}
	for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile, evmReserveLinkStateFile, evmVaultLinkStateFile} {
		for _, suffix := range []string{"", ".lock"} {
			raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name+suffix))
			if err != nil {
				t.Fatal(err)
			}
			before[name+suffix] = raw
		}
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("evidence submission: %+v %d %s", result, code, diagnostic)
	}
	getters, storage := 0, 0
	f.override = func(method string, params []any, result any) any {
		if method == "eth_getStorageAt" && params[0] == f.plan.Address.Hex() {
			block := params[2].(map[string]any)
			if block["blockHash"] != f.receipt["blockHash"] || block["requireCanonical"] != true {
				t.Error("evidence storage escaped CREATE inclusion")
			}
			storage++
		}
		if method == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Address.Hex() {
			block := params[1].(map[string]any)
			if block["blockHash"] != f.receipt["blockHash"] || block["requireCanonical"] != true {
				t.Error("evidence getter escaped CREATE inclusion")
			}
			getters++
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit")
	if code != 0 || result.Status != "evidence-created-unanchored" || result.Receipt == nil || result.Receipt.ContractAddress != f.plan.Address.Hex() || result.Receipt.StorageHash != rootObjectHash(f.plan.Storage) || result.Receipt.GetterHash != rootObjectHash(f.plan.Getters) || getters != 6 || storage != 2 || !bytes.Equal(f.state.GetCode(f.plan.Address), f.plan.Runtime) {
		t.Fatalf("evidence canonical postconditions: %+v getters=%d storage=%d %d %s", result, getters, storage, code, diagnostic)
	}
	deployment := sha256.Sum256([]byte(f.config.Plan.DeploymentId))
	wanted := contractEvidenceConstructor{Coordinator: f.plan.Proxy.Address, SettlementVault: f.plan.Vault.Address, ChainId: 964, Netuid: 25, GenesisHash: common.HexToHash(testGenesisHash), DeploymentIdHash: deployment}
	if result.EvidenceConstructor == nil || *result.EvidenceConstructor != wanted || result.EvidenceAddress != crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 7).Hex() || result.ExecutableAction != "evidence-create" || result.PlanHash != f.config.Plan.hash() || len(result.RemainingActions) != 1 || result.RemainingActions[0] != "evidence-anchor" || result.InstallationComplete || result.ActivationReady || len(f.writes) != 8 || !bytes.Equal(f.writes[7], f.raw) || result.VaultBinding == nil || result.ReserveBinding == nil {
		t.Fatalf("evidence changed domain, graph or installation scope: %+v", result)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "evidence-create")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || retained.ReceiptObservation != "retained" || !maps.Equal(counts, f.counts) {
		t.Fatalf("offline evidence completion: %+v %d %s", retained, code, diagnostic)
	}
	f.override = nil
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || len(f.writes) != 8 {
		t.Fatalf("completed evidence resent: %d %s", code, diagnostic)
	}
	for name, raw := range before {
		after, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
		if err != nil || !bytes.Equal(raw, after) {
			t.Fatalf("evidence changed ancestor %s: %v", name, err)
		}
	}
	for _, action := range []string{"reserve-create", "vault-create", "coordinator-create", "escrow-register", "proxy-create", "reserve-link", "vault-link"} {
		legacy, code, diagnostic := f.command("resume", "--action", action)
		raw, err := json.Marshal(legacy)
		if err != nil || code != 0 || bytes.Contains(raw, []byte("evidence_constructor")) || bytes.Contains(raw, []byte("validator_evidence_address")) {
			t.Fatalf("ancestor %s acquired evidence result fields: %d %s %v", action, code, diagnostic, err)
		}
	}
}

// Fresh synthetic approval cannot authorize different constructor semantics,
// noncanonical words or a changed reserved CREATE identity. Selection rejects
// with the CLI's authority exit before any journal or lock can be opened.
func TestEvmEvidenceCreateRejectsChangedConstructor(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.config.Plan.MaximumTotalWei = "2243000001"
	original := f.config.Plan.Actions[7]
	for _, fault := range []string{"target", "value", "sender", "nonce", "creation", "proxy", "wide-proxy", "genesis", "deployment", "keccak-deployment", "short", "trailing"} {
		action := original
		data, _ := rootReceiptHex(original.Data, 64*1024)
		arguments := data[len(data)-96:]
		switch fault {
		case "target":
			target := crypto.CreateAddress(action.Sender, 4)
			action.To = &target
		case "value":
			action.ValueWei = "1"
		case "sender":
			action.Sender = common.Address{55}
		case "nonce":
			action.Nonce++
		case "creation":
			data[0] ^= 1
		case "proxy":
			wrong := crypto.CreateAddress(action.Sender, 2)
			copy(arguments[12:32], wrong[:])
		case "wide-proxy":
			arguments[0] = 1
		case "genesis":
			arguments[32] ^= 1
		case "deployment":
			arguments[64] ^= 1
		case "keccak-deployment":
			copy(arguments[64:], crypto.Keccak256([]byte(f.config.Plan.DeploymentId)))
		case "short":
			data = data[:len(data)-1]
		case "trailing":
			data = append(data, 0)
		}
		action.Data = "0x" + hex.EncodeToString(data)
		f.config.Plan.Actions[7] = action
		f.publishConfig()
		if _, code, diagnostic := f.command("apply", "--action", "evidence-create"); code != 2 || !strings.HasPrefix(diagnostic, "contract phase selected action:") || !strings.Contains(diagnostic, "evidence") {
			t.Fatalf("changed evidence %s escaped selection rejection: %d %s", fault, code, diagnostic)
		}
		for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile, evmReserveLinkStateFile, evmVaultLinkStateFile, evmEvidenceCreateStateFile} {
			for _, suffix := range []string{"", ".lock"} {
				if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, name+suffix)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("changed constructor %s opened %s%s: %v", fault, name, suffix, err)
				}
			}
		}
	}
	f.config.Plan.Actions[7] = original
	f.publishConfig()
	if _, code, diagnostic := f.command("plan", "--action", "evidence-create"); code != 0 || len(f.counts) != 0 || len(f.writes) != 0 {
		t.Fatalf("restored evidence constructor refused or opened RPC: %d %s", code, diagnostic)
	}
}

// The deployment identifier is a signed domain input; an otherwise valid new
// approval must not reuse constructor bytes carrying the old identifier hash.
func TestEvmEvidenceCreateBindsApprovedDeploymentDomain(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	original := f.config.Plan.DeploymentId
	f.config.Plan.DeploymentId = "synthetic-other-install"
	f.publishConfig()
	for _, command := range []string{"plan", "apply"} {
		if _, code, diagnostic := f.command(command, "--action", "evidence-create"); code != 2 || !strings.HasPrefix(diagnostic, "contract phase selected action:") || !strings.Contains(diagnostic, "deployment domain") {
			t.Fatalf("evidence %s escaped deployment-domain selection rejection: %d %s", command, code, diagnostic)
		}
		for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile, evmReserveLinkStateFile, evmVaultLinkStateFile, evmEvidenceCreateStateFile} {
			for _, suffix := range []string{"", ".lock"} {
				if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, name+suffix)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("changed deployment domain opened %s%s: %v", name, suffix, err)
				}
			}
		}
	}
	f.config.Plan.DeploymentId = original
	f.publishConfig()
	if _, code, diagnostic := f.command("plan", "--action", "evidence-create"); code != 0 || len(f.counts) != 0 || len(f.writes) != 0 {
		t.Fatalf("restored evidence domain refused or opened RPC: %d %s", code, diagnostic)
	}
}

// Review adds evidence's six-word domain while preserving every earlier
// projection and the exact graph signing bytes; it opens no future run directory.
func TestEvmEvidenceCreatePreviewPreservesApprovedGraph(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.config.Signature = ""
	f.config.Plan.RunDirectory = filepath.Join(f.config.Plan.RunDirectory, "future-evidence-run")
	raw, err := json.Marshal(f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), []string{"bootstrap-contracts", "preview", "--action", "evidence-create", "--config", f.configPath}, &stdout, &stderr)
	var preview evmPhasePreview
	message, _ := f.config.Plan.signingBytes()
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil || code != 0 || preview.PlanHash != f.config.Plan.hash() || preview.ApprovalSigningMessageHex != hex.EncodeToString(message) || preview.ApprovalVerified || preview.InstallationComplete || preview.ExecutableAction != "evidence-create" || preview.EvidenceConstructor == nil || preview.EvidenceConstructor.Coordinator.Hex() != preview.ProxyAddress || preview.EvidenceConstructor.SettlementVault.Hex() != preview.VaultAddress || preview.EvidenceAddress != crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 7).Hex() || len(preview.EvidenceStorage) != 2 || len(preview.VaultBindingStorage) != 7 || len(preview.ReserveBindingStorage) != 2 || len(preview.ProxyStorage) != 5 || preview.ProxyConstructor == nil || preview.ProxyConstructor.ApprovedPolicy.EffectiveBlock != 999 || rootObjectHash(preview.Plan) != rootObjectHash(f.config.Plan) {
		t.Fatalf("evidence preview: %+v %d %v %s", preview, code, err, stderr.String())
	}
	if _, err := os.Lstat(f.config.Plan.RunDirectory); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("evidence preview opened custody: %v", err)
	}
}

// Constructor success requires exact CREATE identity, bounded gas/fee and
// explicit empty logs; a status code alone does not establish this deployment.
func TestEvmEvidenceCreateRequiresExactReceipt(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for _, fault := range []string{"missing-from", "from", "missing-to", "to", "address", "null-address", "hash", "block", "position", "status", "gas-zero", "gas-high", "fee", "missing-logs", "null-logs", "event"} {
		observed := false
		f.override = func(method string, params []any, result any) any {
			if method != "eth_getTransactionReceipt" || params[0] != f.tx.Hash().Hex() {
				return result
			}
			observed = true
			receipt := maps.Clone(f.receipt)
			switch fault {
			case "missing-from":
				delete(receipt, "from")
			case "from":
				receipt["from"] = (common.Address{55}).Hex()
			case "missing-to":
				delete(receipt, "to")
			case "to":
				receipt["to"] = f.plan.Proxy.Address.Hex()
			case "address":
				receipt["contractAddress"] = f.plan.Proxy.Address.Hex()
			case "null-address":
				receipt["contractAddress"] = nil
			case "hash":
				receipt["transactionHash"] = (common.Hash{55}).Hex()
			case "block":
				for hash, transaction := range f.history.transactions {
					if transaction.Nonce() == 6 {
						receipt["blockHash"] = hash
					}
				}
			case "position":
				receipt["transactionIndex"] = "0x1"
			case "status":
				receipt["status"] = "0x0"
			case "gas-zero":
				receipt["gasUsed"] = "0x0"
			case "gas-high":
				receipt["gasUsed"] = fmt.Sprintf("0x%x", f.tx.Gas()+1)
			case "fee":
				receipt["effectiveGasPrice"] = "0xb"
			case "missing-logs":
				delete(receipt, "logs")
			case "null-logs":
				receipt["logs"] = nil
			case "event":
				receipt["logs"] = []map[string]any{{"address": f.plan.Address.Hex(), "data": "0x", "topics": []string{}}}
			}
			return receipt
		}
		if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !observed || len(f.writes) != 8 {
			t.Fatalf("evidence receipt %s accepted: %d %s", fault, code, diagnostic)
		}
		if retained, code, diagnostic := f.command("resume", "--action", "evidence-create"); code != 0 || retained.Receipt != nil || retained.Attempts != 1 {
			t.Fatalf("invalid receipt became evidence completion: %+v %d %s", retained, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 || result.Status != "evidence-created-unanchored" {
		t.Fatalf("evidence receipt recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Every immutable getter, patched runtime and both explicit mapping-root words
// are observed independently at the authenticated original inclusion.
func TestEvmEvidenceCreateRequiresEveryOwnPostcondition(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for fault := -3; fault < len(f.plan.Getters); fault++ {
		observed := false
		f.override = func(method string, params []any, result any) any {
			match := fault == -3 && method == "eth_getCode" && params[0] == f.plan.Address.Hex()
			if fault >= -2 && fault < 0 && method == "eth_getStorageAt" {
				match = params[0] == f.plan.Address.Hex() && params[1] == common.BigToHash(big.NewInt(int64(fault+2))).Hex()
			}
			if fault >= 0 && method == "eth_call" {
				input := params[0].(map[string]any)
				match = input["to"] == f.plan.Address.Hex() && input["data"] == f.plan.Getters[fault].Data
			}
			if match {
				observed = true
				return "0x00"
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "differs") || len(f.writes) != 8 {
			t.Fatalf("evidence postcondition %d accepted: %d %s", fault, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 || result.Status != "evidence-created-unanchored" {
		t.Fatalf("evidence postcondition recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Bound vault observations include its packed registration flag and all counters;
// successful old custody cannot replace canonical, pending or inclusion reads.
func TestEvmEvidenceCreateRechecksBoundVaultAtCurrentAndInclusion(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	f.advanceEmpty()
	for _, scope := range []string{"canonical", "pending", "inclusion"} {
		if scope == "inclusion" {
			f.override = nil
			if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
				t.Fatal(diagnostic)
			}
		}
		for fault := -8; fault < len(f.plan.VaultLink.Getters); fault++ {
			observed := false
			f.override = func(method string, params []any, result any) any {
				if method != "eth_getCode" && method != "eth_getStorageAt" && method != "eth_call" {
					return result
				}
				selector := params[len(params)-1]
				block, canonical := selector.(map[string]any)
				if scope == "pending" && selector != "pending" || scope != "pending" && (!canonical || block["blockHash"] != f.newestEvmHash()) {
					return result
				}
				match := fault == -8 && method == "eth_getCode" && params[0] == f.plan.Vault.Address.Hex()
				if fault >= -7 && fault < 0 && method == "eth_getStorageAt" {
					match = params[0] == f.plan.Vault.Address.Hex() && params[1] == f.plan.VaultLink.Storage[fault+7].Slot
				}
				if fault >= 0 && method == "eth_call" {
					input := params[0].(map[string]any)
					match = input["to"] == f.plan.Vault.Address.Hex() && input["data"] == f.plan.VaultLink.Getters[fault].Data
				}
				if match {
					observed = true
					return "0x00"
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "bound vault") {
				t.Fatalf("evidence %s vault fault %d accepted: %d %s", scope, fault, code, diagnostic)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 || result.Status != "evidence-created-unanchored" || len(f.writes) != 8 {
		t.Fatalf("bound vault recovery: %+v %d %s", result, code, diagnostic)
	}
}

// The bound reserve, initialized proxy and implementation are separately required
// at the same current or inclusion selector, including the still-zero anchor.
func TestEvmEvidenceCreateRechecksReserveProxyAndImplementation(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	f.advanceEmpty()
	for _, scope := range []string{"canonical", "pending", "inclusion"} {
		if scope == "inclusion" {
			f.override = nil
			if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
				t.Fatal(diagnostic)
			}
		}
		for _, fault := range []string{"reserve-code", "reserve-recorder", "reserve-principal", "reserve-recorder-slot", "reserve-principal-slot", "proxy-code", "proxy-owner", "proxy-vault", "proxy-netuid", "anchor", "implementation-slot", "admin-slot", "beacon-slot", "initializer-slot", "owner-slot", "implementation-code", "implementation-guard", "implementation-proxy-slot"} {
			observed := false
			f.override = func(method string, params []any, result any) any {
				if method != "eth_getCode" && method != "eth_getStorageAt" && method != "eth_call" {
					return result
				}
				selector := params[len(params)-1]
				block, canonical := selector.(map[string]any)
				if scope == "pending" && selector != "pending" || scope != "pending" && (!canonical || block["blockHash"] != f.newestEvmHash()) {
					return result
				}
				match := false
				if method == "eth_getCode" {
					match = fault == "reserve-code" && params[0] == f.plan.Reserve.Address.Hex() || fault == "proxy-code" && params[0] == f.plan.Proxy.Address.Hex() || fault == "implementation-code" && params[0] == f.plan.Coordinator.Address.Hex()
				}
				if method == "eth_getStorageAt" {
					for index, name := range []string{"implementation-slot", "admin-slot", "beacon-slot", "initializer-slot", "owner-slot"} {
						match = match || fault == name && params[0] == f.plan.Proxy.Address.Hex() && params[1] == f.plan.Proxy.Storage[index].Slot
					}
					match = match || fault == "implementation-guard" && params[0] == f.plan.Coordinator.Address.Hex() && params[1] == f.plan.Coordinator.Storage[0].Slot
					match = match || fault == "implementation-proxy-slot" && params[0] == f.plan.Coordinator.Address.Hex() && params[1] == f.plan.Coordinator.Storage[1].Slot
					for index, name := range []string{"reserve-recorder-slot", "reserve-principal-slot"} {
						match = match || fault == name && params[0] == f.plan.Reserve.Address.Hex() && params[1] == f.plan.ReserveLink.Storage[index].Slot
					}
				}
				if method == "eth_call" {
					input := params[0].(map[string]any)
					selectors := map[string][]byte{"reserve-recorder": stabi.NewSTReserveSink().PackRecorder(), "reserve-principal": stabi.NewSTReserveSink().PackPrincipal(), "proxy-owner": stabi.NewSTCoordinator().PackOwner(), "proxy-vault": stabi.NewSTCoordinator().PackSettlementVault(), "proxy-netuid": stabi.NewSTCoordinator().PackNetuid(), "anchor": stabi.NewSTCoordinator().PackValidatorEvidence()}
					target := f.plan.Proxy.Address.Hex()
					if strings.HasPrefix(fault, "reserve-") {
						target = f.plan.Reserve.Address.Hex()
					}
					match = input["to"] == target && input["data"] == "0x"+hex.EncodeToString(selectors[fault])
				}
				if match {
					observed = true
					return "0x00"
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "differs") {
				t.Fatalf("evidence %s %s accepted: %d %s", scope, fault, code, diagnostic)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 || result.Status != "evidence-created-unanchored" || len(f.writes) != 8 {
		t.Fatalf("evidence dependency recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Missing canonical/pending observations stay unresolved without a contradiction,
// success or attempt, and failed inclusion reads retain original signed liability.
func TestEvmEvidenceCreateKeepsUnavailableReadsUnresolved(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	f.advanceEmpty()
	for _, scope := range []string{"canonical", "pending", "inclusion"} {
		if scope == "inclusion" {
			f.override = nil
			if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
				t.Fatal(diagnostic)
			}
		}
		path := filepath.Join(f.config.Plan.RunDirectory, evmEvidenceCreateStateFile)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, method := range []string{"eth_call", "eth_getStorageAt"} {
			observed := false
			f.override = func(query string, params []any, result any) any {
				if query != method {
					return result
				}
				selector := params[len(params)-1]
				block, canonical := selector.(map[string]any)
				if scope == "pending" && selector != "pending" || scope != "pending" && (!canonical || block["blockHash"] != f.newestEvmHash()) {
					return result
				}
				observed = true
				return mappingFixtureRpcError{code: -32602}
			}
			if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !observed || strings.Contains(diagnostic, "differs") {
				t.Fatalf("evidence %s unavailable %s became contradiction: %d %s", scope, method, code, diagnostic)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("unavailable observation changed custody: %v", err)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 || result.Status != "evidence-created-unanchored" || len(f.writes) != 8 {
		t.Fatalf("evidence read recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Later same-sender reservations retain value and maximum gas liability even
// though this executor neither selects nor executes the final anchor action.
func TestEvmEvidenceCreatePreservesFutureValueAndGasFunding(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	target := crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 4)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "evidence-anchor", Sender: f.config.Plan.Actions[0].Sender, Nonce: 8, To: &target, Data: "0x00", ValueWei: "3000000000", Gas: 1_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "5253000000"
	f.publishConfig()
	f.prepareEvidenceSigned()
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 3_059_999_999)
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "maximum liability") || len(f.writes) != 7 {
		t.Fatalf("evidence spent future reservation: %d %s", code, diagnostic)
	}
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 3_060_000_000)
		}
		return result
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || result.Attempts != 1 || len(f.writes) != 8 {
		t.Fatalf("evidence exact funding refused: %+v %d %s", result, code, diagnostic)
	}
}

// Genuine code-deposit exhaustion leaves no evidence runtime, consumes the
// original CREATE nonce and retains failure without replacement authority.
func TestEvmEvidenceCreateRevertRetainsConsumedNonce(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.config.Plan.Actions[7].Gas = 500_000
	f.publishConfig()
	f.prepareEvidenceSigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if f.vm.GasLimit != 500_000 || f.tx.Gas() != 500_000 {
		t.Fatal("evidence getter simulation changed CREATE gas")
	}
	result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit")
	if code != 0 || result.Status != "create-reverted-nonce-consumed" || result.Receipt == nil || result.Receipt.Status != 0 || result.Receipt.RuntimeHash != "" || result.Receipt.GetterHash != "" || result.Receipt.StorageHash != "" || len(f.writes) != 8 || len(f.state.GetCode(f.plan.Address)) != 0 || f.state.GetNonce(f.config.Plan.Actions[7].Sender) != 8 {
		t.Fatalf("evidence revert created success or another spend: %+v %d %s", result, code, diagnostic)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "evidence-create")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || !maps.Equal(counts, f.counts) {
		t.Fatalf("evidence offline revert lost custody: %+v %d %s", retained, code, diagnostic)
	}
}

// A consumed vault-binding nonce with genuine failure is not a prerequisite.
func TestEvmEvidenceCreateRefusesRevertedVaultBinding(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.config.Plan.Actions[6].Gas = 23_000
	f.publishConfig()
	f.prepareVaultLinkSigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "vault-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "vault-link", "--online"); code != 0 || result.Status != "vault-binding-reverted-nonce-consumed" {
		t.Fatalf("vault binding did not genuinely revert: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("apply", "--action", "evidence-create"); code != 3 || !strings.Contains(diagnostic, "successful vault coordinator binding") {
		t.Fatalf("reverted vault binding admitted evidence: %d %s", code, diagnostic)
	}
	if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmEvidenceCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reverted binding acquired evidence custody: %v", err)
	}
}

// Creation cannot widen the command into the separately authorized owner call.
func TestEvmEvidenceCreateKeepsAnchorUnimplemented(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	if _, code, diagnostic := f.command("apply", "--action", "evidence-anchor"); code != 2 || len(f.counts) != 0 || len(f.writes) != 0 {
		t.Fatalf("evidence executor admitted anchor: %d %s", code, diagnostic)
	}
}

// Unmapped receipt claims remain nonfinal, retain the spent allowance and cannot
// trigger replay; a later genuine inclusion resolves the same original bytes.
func TestEvmEvidenceCreateKeepsUnmappedReceiptUnresolved(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	f.mine = false
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("evidence unmined send: %+v %d %s", result, code, diagnostic)
	}
	f.override = func(method string, params []any, result any) any {
		if method == "eth_getTransactionReceipt" && params[0] == f.tx.Hash().Hex() {
			return map[string]any{"transactionHash": f.tx.Hash().Hex(), "blockHash": (common.Hash{55}).Hex(), "blockNumber": "0x100", "transactionIndex": "0x0", "status": "0x1", "gasUsed": "0x10000", "effectiveGasPrice": "0x2", "contractAddress": f.plan.Address.Hex(), "from": f.config.Plan.Actions[7].Sender.Hex(), "to": nil, "logs": []any{}}
		}
		return result
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || result.Status != "receipt-awaiting-finalized-mapping" || result.Receipt != nil || result.Attempts != 1 || len(f.writes) != 8 {
		t.Fatalf("unmapped evidence receipt acquired finality or replay: %+v %d %s", result, code, diagnostic)
	}
	f.override = nil
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 || result.Status != "evidence-created-unanchored" || result.Receipt == nil || result.Attempts != 1 || len(f.writes) != 8 {
		t.Fatalf("genuine evidence inclusion did not resolve original bytes: %+v %d %s", result, code, diagnostic)
	}
}

// Advancing clocks cannot reinterpret the proxy's normalized initial policy.
// Its EVM inclusion is distinct from both native height and this later CREATE.
func TestEvmEvidenceCreatePreservesOriginalProxyPolicyHeight(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	stores, records := f.openEvidenceAncestors()
	proxyReceipt := *records[4].Receipt
	for _, store := range stores {
		store.close()
	}
	f.advanceEmpty()
	f.vm.BlockNumber = big.NewInt(8000)
	policySelector := "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackPolicyByIndex(big.NewInt(0)))
	for _, height := range []uint64{proxyReceipt.NativeNumber, 999, 8000} {
		observed := false
		f.override = func(method string, params []any, result any) any {
			if method == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Proxy.Address.Hex() && params[0].(map[string]any)["data"] == policySelector {
				block, canonical := params[1].(map[string]any)
				if canonical && block["blockHash"] == f.newestEvmHash() || params[1] == "pending" {
					observed = true
					data, _ := rootReceiptHex(result.(string), 14*32)
					copy(data[64:96], common.LeftPadBytes(new(big.Int).SetUint64(height).Bytes(), 32))
					return "0x" + hex.EncodeToString(data)
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "proxy getter differs") || len(f.writes) != 7 {
			t.Fatalf("evidence renormalized policy to %d: %d %s", height, code, diagnostic)
		}
	}
	f.override = nil
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 || result.Status != "evidence-created-unanchored" || result.Receipt == nil || result.Receipt.BlockNumber <= proxyReceipt.BlockNumber || result.ProxyConstructor.ApprovedPolicy.EffectiveBlock != 999 || len(f.writes) != 8 {
		t.Fatalf("evidence lost original proxy height: %+v %d %s", result, code, diagnostic)
	}
}

// Retained completion requires the exact evidence runtime, domain and two root
// words. Rehashed receipt changes remain invalid when the command reopens offline.
func TestEvmEvidenceCreateRetainedCompletionRequiresDomain(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	f.prepareEvidenceSigned()
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	stores, records := f.openEvidenceAncestors()
	store, err := openEvmEvidenceActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], records[5], records[6], false, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"runtime", "getters", "storage", "address", "binding-fields"} {
		changed := record
		receipt := *record.Receipt
		changed.Receipt = &receipt
		switch fault {
		case "runtime":
			receipt.RuntimeHash = ""
		case "getters":
			receipt.GetterHash = rootObjectHash(f.plan.Proxy.Getters)
		case "storage":
			receipt.StorageHash = ""
		case "address":
			receipt.ContractAddress = f.plan.Proxy.Address.Hex()
		case "binding-fields":
			receipt.VaultBindingHash = records[6].Receipt.VaultBindingHash
		}
		changed.ContentHash = ""
		changed.ContentHash = rootObjectHash(changed)
		if err := validateEvmCreateCompletion(f.plan, changed); err == nil {
			t.Fatalf("retained evidence accepted changed %s", fault)
		}
	}
	record.Receipt.GetterHash = rootObjectHash(f.plan.Proxy.Getters)
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
	if _, code, diagnostic := f.command("resume", "--action", "evidence-create"); code != 3 || !strings.Contains(diagnostic, "successful unanchored validator evidence") || !maps.Equal(counts, f.counts) {
		t.Fatalf("offline evidence trusted another domain: %d %s", code, diagnostic)
	}
}
