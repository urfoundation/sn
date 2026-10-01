// Reserve binding tests exercise exact original calldata, genuine one-shot EVM
// effects, canonical observations and durable five-predecessor recovery.
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

// Real execution changes only the reviewed reserve binding and preserves every
// predecessor journal, marker and legacy result encoding under the same graph.
func TestEvmReserveLinkExecutesExactCallAndPreservesAncestors(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	before := map[string][]byte{}
	for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile} {
		for _, suffix := range []string{"", ".lock"} {
			raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name+suffix))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte("recorder_binding_hash")) || bytes.Contains(raw, []byte("recorder_log_index")) {
				t.Fatal("binding receipt fields altered historical encoding")
			}
			before[name+suffix] = raw
		}
	}
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("binding submission: %+v %d %s", result, code, diagnostic)
	}
	getters, storage := 0, 0
	f.override = func(method string, params []any, result any) any {
		if method == "eth_getStorageAt" && params[0] == f.plan.Address.Hex() {
			block := params[2].(map[string]any)
			if block["blockHash"] != f.receipt["blockHash"] || block["requireCanonical"] != true {
				t.Error("reserve storage escaped binding inclusion")
			}
			storage++
		}
		if method == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Address.Hex() && params[1].(map[string]any)["blockHash"] == f.receipt["blockHash"] {
			getters++
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit")
	if code != 0 || result.Status != "reserve-recorder-bound" || result.Receipt == nil || result.Receipt.ContractAddress != "" || result.Receipt.StorageHash != rootObjectHash(f.plan.Storage) || result.Receipt.GetterHash != rootObjectHash(f.plan.Getters) || result.Receipt.RecorderBindingHash != evmReserveBindingHash(f.plan, *result.Receipt) || getters != 6 || storage != 2 {
		t.Fatalf("reserve binding postconditions: %+v getters=%d storage=%d %d %s", result, getters, storage, code, diagnostic)
	}
	if result.PlanHash != f.config.Plan.hash() || result.ReserveBinding == nil || *result.ReserveBinding != *f.plan.ReserveBinding || result.ProxyAddress != f.plan.Proxy.Address.Hex() || result.ProxyConstructor == nil || result.ProxyConstructor.ApprovedPolicy.EffectiveBlock != 999 || result.VaultAddress != f.plan.Vault.Address.Hex() || result.CoordinatorAddress != f.plan.Coordinator.Address.Hex() || result.ExecutableAction != "reserve-link" || len(result.RemainingActions) != 3 || result.InstallationComplete || result.ActivationReady || len(f.writes) != 6 || !bytes.Equal(f.writes[5], f.raw) {
		t.Fatalf("binding changed graph or claimed full installation: %+v", result)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "reserve-link")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || retained.ReceiptObservation != "retained" || !maps.Equal(counts, f.counts) {
		t.Fatalf("offline binding completion: %+v %d %s", retained, code, diagnostic)
	}
	f.override = nil
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || len(f.writes) != 6 {
		t.Fatalf("completed binding resent: %d %s", code, diagnostic)
	}
	for name, raw := range before {
		after, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
		if err != nil || !bytes.Equal(raw, after) {
			t.Fatalf("binding changed ancestor %s: %v", name, err)
		}
	}
	for _, action := range []string{"reserve-create", "vault-create", "coordinator-create", "escrow-register", "proxy-create"} {
		legacy, code, diagnostic := f.command("resume", "--action", action)
		raw, err := json.Marshal(legacy)
		if err != nil || code != 0 || bytes.Contains(raw, []byte("reserve_binding")) || bytes.Contains(raw, []byte("recorder_log_index")) {
			t.Fatalf("legacy %s acquired binding fields: %d %s %v", action, code, diagnostic, err)
		}
	}
}

// Fresh synthetic approval still cannot change the reviewed target, proxy,
// selector, canonical address word, value or nonce relationship.
func TestEvmReserveLinkRejectsChangedEnvelope(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	// Leave one wei of approved headroom so the nonzero-value case reaches the
	// action-specific semantic check instead of the generic graph cost limit.
	f.config.Plan.MaximumTotalWei = "2191500001"
	original := f.config.Plan.Actions[5]
	for _, fault := range []string{"target", "create", "value", "sender", "nonce", "selector", "implementation", "zero-recorder", "wide-recorder", "short", "trailing"} {
		action := original
		data, _ := rootReceiptHex(original.Data, 36)
		switch fault {
		case "target":
			target := crypto.CreateAddress(action.Sender, 1)
			action.To = &target
		case "create":
			action.To = nil
		case "value":
			action.ValueWei = "1"
		case "sender":
			action.Sender = common.Address{55}
		case "nonce":
			action.Nonce++
		case "selector":
			data[0] ^= 1
		case "implementation":
			data = stabi.NewSTReserveSink().PackSetRecorderOnce(crypto.CreateAddress(action.Sender, 2))
		case "zero-recorder":
			data = stabi.NewSTReserveSink().PackSetRecorderOnce(common.Address{})
		case "wide-recorder":
			data[4] = 1
		case "short":
			data = data[:35]
		case "trailing":
			data = append(data, 0)
		}
		action.Data = "0x" + hex.EncodeToString(data)
		f.config.Plan.Actions[5] = action
		f.publishConfig()
		if _, code, diagnostic := f.command("plan", "--action", "reserve-link"); code != 2 {
			t.Fatalf("binding %s accepted: %d %s", fault, code, diagnostic)
		}
	}
	f.config.Plan.Actions[5] = original
	f.publishConfig()
	if _, code, diagnostic := f.command("plan", "--action", "reserve-link"); code != 0 || len(f.counts) != 0 {
		t.Fatalf("exact binding plan refused or opened route: %d %s", code, diagnostic)
	}
}

// Preview exports the same approval bytes and all original constructor fields,
// with only derived binding/storage review fields and no future custody access.
func TestEvmReserveLinkPreviewPreservesApprovedGraph(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.config.Signature = ""
	f.config.Plan.RunDirectory = filepath.Join(f.config.Plan.RunDirectory, "future-binding-run")
	raw, err := json.Marshal(f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), []string{"bootstrap-contracts", "preview", "--action", "reserve-link", "--config", f.configPath}, &stdout, &stderr)
	var preview evmPhasePreview
	message, _ := f.config.Plan.signingBytes()
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil || code != 0 || preview.PlanHash != f.config.Plan.hash() || preview.ApprovalSigningMessageHex != hex.EncodeToString(message) || preview.ApprovalVerified || preview.InstallationComplete || preview.ExecutableAction != "reserve-link" || preview.ReserveBinding == nil || preview.ReserveBinding.Recorder.Hex() != preview.ProxyAddress || preview.ReserveBinding.Reserve.Hex() != preview.ReserveAddress || len(preview.ReserveBindingStorage) != 2 || len(preview.ProxyStorage) != 5 || preview.ProxyConstructor == nil || preview.ProxyConstructor.ApprovedPolicy.EffectiveBlock != 999 || rootObjectHash(preview.Plan) != rootObjectHash(f.config.Plan) {
		t.Fatalf("binding preview: %+v %d %v %s", preview, code, err, stderr.String())
	}
	if _, err := os.Lstat(f.config.Plan.RunDirectory); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("binding preview opened custody: %v", err)
	}
}

// One exact indexed event, receipt identity and fee tuple are required; missing
// or conflicting data cannot retain success or authorize another call.
func TestEvmReserveLinkRequiresExactReceiptAndEvent(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for _, fault := range []string{"to", "from", "missing-to", "missing-from", "create-address", "zero-create-address", "gas", "fee", "missing-logs", "null-logs", "empty-logs", "duplicate-event", "event-address", "event-topic", "event-recorder", "event-wide-recorder", "event-extra-topic", "event-data", "event-removed", "event-missing-removed", "event-block", "event-transaction", "event-block-number", "event-transaction-index", "event-missing-index"} {
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
				changed[fault] = common.Address{55}.Hex()
			case "missing-to", "missing-from":
				delete(changed, strings.TrimPrefix(fault, "missing-"))
			case "create-address":
				changed["contractAddress"] = f.plan.Address.Hex()
			case "zero-create-address":
				changed["contractAddress"] = common.Address{}.Hex()
			case "gas":
				changed["gasUsed"] = fmt.Sprintf("0x%x", f.tx.Gas()+1)
			case "fee":
				changed["effectiveGasPrice"] = "0xb"
			case "missing-logs":
				delete(changed, "logs")
			case "null-logs":
				changed["logs"] = nil
			case "empty-logs":
				changed["logs"] = []any{}
			case "duplicate-event":
				changed["logs"] = []map[string]any{log, maps.Clone(log)}
			case "event-address":
				log["address"] = f.plan.Proxy.Address.Hex()
			case "event-topic":
				log["topics"].([]string)[0] = common.Hash{55}.Hex()
			case "event-recorder":
				log["topics"].([]string)[1] = common.BytesToHash(f.plan.Coordinator.Address[:]).Hex()
			case "event-wide-recorder":
				log["topics"].([]string)[1] = "0x01" + log["topics"].([]string)[1][4:]
			case "event-extra-topic":
				log["topics"] = append(log["topics"].([]string), common.Hash{}.Hex())
			case "event-data":
				log["data"] = "0x00"
			case "event-removed":
				log["removed"] = true
			case "event-missing-removed":
				delete(log, "removed")
			case "event-block":
				log["blockHash"] = common.Hash{55}.Hex()
			case "event-transaction":
				log["transactionHash"] = common.Hash{55}.Hex()
			case "event-block-number":
				log["blockNumber"] = "0x1"
			case "event-transaction-index":
				log["transactionIndex"] = "0x1"
			case "event-missing-index":
				delete(log, "logIndex")
			}
			return changed
		}
		if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || len(f.writes) != 6 {
			t.Fatalf("binding receipt %s accepted: %d %s", fault, code, diagnostic)
		}
		retained, code, diagnostic := f.command("resume", "--action", "reserve-link")
		if code != 0 || retained.Receipt != nil || retained.Attempts != 1 {
			t.Fatalf("binding receipt %s mutated custody: %+v %d %s", fault, retained, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online"); code != 0 || result.Status != "reserve-recorder-bound" || len(f.writes) != 6 {
		t.Fatalf("binding receipt recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Inclusion checks cannot borrow reserve-creation's zero recorder observation.
// Each bound getter and both storage words are independently necessary.
func TestEvmReserveLinkRequiresEveryReservePostcondition(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for fault := -3; fault < len(f.plan.Getters); fault++ {
		observed := false
		f.override = func(method string, params []any, result any) any {
			if method != "eth_getCode" && method != "eth_getStorageAt" && method != "eth_call" {
				return result
			}
			block, ok := params[len(params)-1].(map[string]any)
			if !ok || block["blockHash"] != f.receipt["blockHash"] {
				return result
			}
			if fault == -3 && method == "eth_getCode" && params[0] == f.plan.Address.Hex() || fault < 0 && fault >= -2 && method == "eth_getStorageAt" && params[0] == f.plan.Address.Hex() && params[1] == f.plan.Storage[fault+2].Slot || fault >= 0 && method == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Address.Hex() && params[0].(map[string]any)["data"] == f.plan.Getters[fault].Data {
				observed = true
				return "0x00"
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "differs") || len(f.writes) != 6 {
			t.Fatalf("binding postcondition %d accepted: %d %s", fault, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online"); code != 0 || result.Status != "reserve-recorder-bound" {
		t.Fatalf("binding postcondition recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Canonical and pending preconditions are independent from historical success.
// Even the intended recorder already being fixed cannot permit a second call.
func TestEvmReserveLinkRejectsCurrentRecorderCollisions(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	f.advanceEmpty()
	path := filepath.Join(f.config.Plan.RunDirectory, evmReserveLinkStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	recorder := "0x" + hex.EncodeToString(stabi.NewSTReserveSink().PackRecorder())
	for _, pending := range []bool{false, true} {
		for _, value := range []common.Address{f.plan.Proxy.Address, common.Address{55}} {
			observed := false
			f.override = func(method string, params []any, result any) any {
				if method != "eth_call" || params[0].(map[string]any)["to"] != f.plan.Address.Hex() || params[0].(map[string]any)["data"] != recorder {
					return result
				}
				block, canonical := params[1].(map[string]any)
				if pending && params[1] == "pending" || !pending && canonical && block["blockHash"] == f.newestEvmHash() {
					observed = true
					return common.BytesToHash(value[:]).Hex()
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "recorder is already fixed") || len(f.writes) != 5 {
				t.Fatalf("current recorder pending=%v value=%s admitted: %d %s", pending, value, code, diagnostic)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("collision changed binding custody: %v", err)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("cleared recorder collision did not recover: %+v %d %s", result, code, diagnostic)
	}
}

// Current target code, accounting getters and raw slots are checked independently
// at both canonical and pending state before any binding attempt is published.
func TestEvmReserveLinkRequiresCurrentTargetState(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	f.advanceEmpty()
	for _, pending := range []bool{false, true} {
		for _, fault := range []string{"runtime", "bootstrap", "principal", "recorder-slot", "principal-slot"} {
			observed := false
			f.override = func(method string, params []any, result any) any {
				if method != "eth_getCode" && method != "eth_getStorageAt" && method != "eth_call" {
					return result
				}
				selector := params[len(params)-1]
				block, canonical := selector.(map[string]any)
				if pending && selector != "pending" || !pending && (!canonical || block["blockHash"] != f.newestEvmHash()) {
					return result
				}
				match := fault == "runtime" && method == "eth_getCode" && params[0] == f.plan.Address.Hex()
				if method == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Address.Hex() {
					data := params[0].(map[string]any)["data"]
					match = fault == "bootstrap" && data == f.plan.Getters[3].Data || fault == "principal" && data == f.plan.Getters[5].Data
				}
				if method == "eth_getStorageAt" && params[0] == f.plan.Address.Hex() {
					match = fault == "recorder-slot" && params[1] == f.plan.Storage[0].Slot || fault == "principal-slot" && params[1] == f.plan.Storage[1].Slot
				}
				if match {
					observed = true
					return "0x00"
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "differs") || len(f.writes) != 5 {
				t.Fatalf("target pending=%v %s admitted: %d %s", pending, fault, code, diagnostic)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("target recovery failed: %+v %d %s", result, code, diagnostic)
	}
}

// Each stable proxy getter, namespace and implementation observation is made at
// binding inclusion independently of its successful historical construction.
func TestEvmReserveLinkRechecksRecorderAtBindingInclusion(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	getters, err := f.plan.Proxy.receiptGetters(evmCreateReceipt{BlockNumber: 1})
	if err != nil {
		t.Fatal(err)
	}
	clock := "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackCurrentEpoch())
	for fault := -9; fault < len(getters); fault++ {
		if fault >= 0 && getters[fault].Data == clock {
			continue
		}
		observed := false
		f.override = func(method string, params []any, result any) any {
			if method != "eth_getCode" && method != "eth_getStorageAt" && method != "eth_call" {
				return result
			}
			block, ok := params[len(params)-1].(map[string]any)
			if !ok || block["blockHash"] != f.receipt["blockHash"] {
				return result
			}
			match := fault == -9 && method == "eth_getCode" && params[0] == f.plan.Proxy.Address.Hex() || fault == -8 && method == "eth_getCode" && params[0] == f.plan.Coordinator.Address.Hex()
			if method == "eth_getStorageAt" {
				match = fault >= -7 && fault <= -3 && params[0] == f.plan.Proxy.Address.Hex() && params[1] == f.plan.Proxy.Storage[fault+7].Slot || fault >= -2 && fault < 0 && params[0] == f.plan.Coordinator.Address.Hex() && params[1] == f.plan.Coordinator.Storage[fault+2].Slot
			}
			if fault >= 0 && method == "eth_call" {
				input := params[0].(map[string]any)
				match = input["to"] == f.plan.Proxy.Address.Hex() && input["data"] == getters[fault].Data
			}
			if match {
				observed = true
				return "0x00"
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "differs") || len(f.writes) != 6 {
			t.Fatalf("binding recorder postcondition %d accepted: %d %s", fault, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online"); code != 0 || result.Status != "reserve-recorder-bound" {
		t.Fatalf("recorder inclusion recovery: %+v %d %s", result, code, diagnostic)
	}
}

// A changed present proxy cannot borrow earlier initialized receipts. Pending
// state is checked independently, including the implementation behind the slot.
func TestEvmReserveLinkRequiresCurrentRecorderState(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	f.advanceEmpty()
	for _, pending := range []bool{false, true} {
		for _, fault := range []string{"proxy-code", "proxy-owner", "proxy-implementation", "proxy-initialized", "implementation-code", "implementation-disabled"} {
			observed := false
			f.override = func(method string, params []any, result any) any {
				if method != "eth_getCode" && method != "eth_getStorageAt" && method != "eth_call" {
					return result
				}
				selector := params[len(params)-1]
				block, canonical := selector.(map[string]any)
				if pending && selector != "pending" || !pending && (!canonical || block["blockHash"] != f.newestEvmHash()) {
					return result
				}
				match := method == "eth_getCode" && (fault == "proxy-code" && params[0] == f.plan.Proxy.Address.Hex() || fault == "implementation-code" && params[0] == f.plan.Coordinator.Address.Hex())
				if method == "eth_call" && fault == "proxy-owner" {
					input := params[0].(map[string]any)
					match = input["to"] == f.plan.Proxy.Address.Hex() && input["data"] == "0x"+hex.EncodeToString(stabi.NewSTCoordinator().PackOwner())
				}
				if method == "eth_getStorageAt" {
					match = params[0] == f.plan.Proxy.Address.Hex() && (fault == "proxy-implementation" && params[1] == f.plan.Proxy.Storage[0].Slot || fault == "proxy-initialized" && params[1] == f.plan.Proxy.Storage[3].Slot) || fault == "implementation-disabled" && params[0] == f.plan.Coordinator.Address.Hex() && params[1] == f.plan.Coordinator.Storage[0].Slot
				}
				if match {
					observed = true
					return "0x00"
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "differs") || len(f.writes) != 5 {
				t.Fatalf("recorder pending=%v %s admitted: %d %s", pending, fault, code, diagnostic)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("recorder current recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Missing getter/storage observations remain unresolved, without recording a
// contradiction, success or attempt; recovery uses the original signed bytes.
func TestEvmReserveLinkKeepsUnavailableReadsUnresolved(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	f.advanceEmpty()
	for _, inclusion := range []bool{false, true} {
		if inclusion {
			f.override = nil
			if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
				t.Fatal(diagnostic)
			}
		}
		path := filepath.Join(f.config.Plan.RunDirectory, evmReserveLinkStateFile)
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
				block, ok := params[len(params)-1].(map[string]any)
				if !ok || block["blockHash"] != f.newestEvmHash() {
					return result
				}
				if query == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Address.Hex() || query == "eth_getStorageAt" && params[0] == f.plan.Address.Hex() {
					observed = true
					return mappingFixtureRpcError{code: -32602}
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !observed || strings.Contains(diagnostic, "differs") || strings.Contains(diagnostic, "already fixed") {
				t.Fatalf("unavailable inclusion=%v %s became a contradiction: %d %s", inclusion, method, code, diagnostic)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("unavailable read changed journal: %v", err)
			}
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Status != "reserve-recorder-bound" || result.Attempts != 1 || len(f.writes) != 6 {
		t.Fatalf("unavailable binding recovery: %+v %d %s", result, code, diagnostic)
	}
}

// The proxy's normalized policy is pinned to its earlier EVM receipt even as
// current clocks advance and the binding is mined in a distinct later block.
func TestEvmReserveLinkPreservesOriginalProxyPolicyHeight(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	stores, records := f.openReserveLinkAncestors()
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
				if canonical && block["blockHash"] != proxyReceipt.BlockHash || params[1] == "pending" {
					observed = true
					data, _ := rootReceiptHex(result.(string), 14*32)
					copy(data[64:96], common.LeftPadBytes(new(big.Int).SetUint64(height).Bytes(), 32))
					return "0x" + hex.EncodeToString(data)
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "proxy getter differs") || len(f.writes) != 5 {
			t.Fatalf("binding renormalized policy to %d: %d %s", height, code, diagnostic)
		}
	}
	f.override = nil
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online")
	if code != 0 || result.Status != "reserve-recorder-bound" || result.Receipt == nil || result.Receipt.BlockNumber <= proxyReceipt.BlockNumber || result.ProxyConstructor.ApprovedPolicy.EffectiveBlock != 999 || len(f.writes) != 6 {
		t.Fatalf("binding lost original proxy height: %+v %d %s", result, code, diagnostic)
	}
}

// Later graph reservations retain both value and maximum gas; a one-wei deficit
// cannot spend the selected action even when its own zero-value call is funded.
func TestEvmReserveLinkPreservesFutureValueAndGasFunding(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	target := crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 1)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "vault-link", Sender: f.config.Plan.Actions[0].Sender, Nonce: 6, To: &target, Data: "0x00", ValueWei: "3000000000", Gas: 1_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "5201500000"
	f.publishConfig()
	f.prepareReserveLinkSigned()
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 3_011_499_999)
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "maximum liability") || len(f.writes) != 5 {
		t.Fatalf("binding spent later funding: %d %s", code, diagnostic)
	}
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 3_011_500_000)
		}
		return result
	}
	if result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("binding exact funding refused: %+v %d %s", result, code, diagnostic)
	}
}

// Genuine call gas exhaustion consumes its original nonce while preserving the
// deployed reserve and unbound recorder; it never becomes replacement authority.
func TestEvmReserveLinkRevertRetainsConsumedNonce(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.config.Plan.Actions[5].Gas = 21_000
	f.publishConfig()
	f.prepareReserveLinkSigned()
	f.gasFailure = true
	f.advanceEmpty()
	currentHash := f.newestEvmHash()
	policySelector := "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackPolicyByIndex(big.NewInt(0)))
	observed := map[string]bool{}
	f.override = func(method string, params []any, result any) any {
		if method == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Proxy.Address.Hex() && params[0].(map[string]any)["data"] == policySelector {
			if params[1] == "pending" {
				observed["pending"] = true
			} else if params[1].(map[string]any)["blockHash"] == currentHash {
				observed["current"] = true
			} else {
				observed["historical"] = true
			}
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if !observed["historical"] || !observed["current"] || !observed["pending"] || f.vm.GasLimit != 21_000 || f.tx.Gas() != 21_000 || len(f.writes) != 6 || !bytes.Equal(f.writes[5], f.raw) {
		t.Fatalf("binding read budget changed execution or skipped a read scope: %v, execution gas %d", observed, f.vm.GasLimit)
	}
	result, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit")
	if code != 0 || result.Status != "reserve-binding-reverted-nonce-consumed" || result.Receipt == nil || result.Receipt.Status != 0 || result.Receipt.RuntimeHash != "" || result.Receipt.GetterHash != "" || result.Receipt.StorageHash != "" || result.Receipt.RecorderBindingHash != "" || len(f.writes) != 6 || !bytes.Equal(f.state.GetCode(f.plan.Address), f.plan.Runtime) || f.state.GetState(f.plan.Address, common.Hash{}) != (common.Hash{}) || f.state.GetNonce(f.config.Plan.Actions[5].Sender) != 6 {
		t.Fatalf("binding revert created success or a second spend: %+v %d %s", result, code, diagnostic)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "reserve-link")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || !maps.Equal(counts, f.counts) {
		t.Fatalf("offline binding revert lost custody: %+v %d %s", retained, code, diagnostic)
	}
}

// A consumed proxy nonce with a reverted initializer cannot unlock binding even
// when the deployer nonce now matches the next approved reservation.
func TestEvmReserveLinkRefusesRevertedProxy(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.config.Plan.Actions[4].Gas = 21_000
	f.publishConfig()
	f.prepareProxySigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online"); code != 0 || result.Status != "create-reverted-nonce-consumed" {
		t.Fatalf("proxy did not genuinely revert: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("apply", "--action", "reserve-link"); code != 3 || !strings.Contains(diagnostic, "successful initialized proxy") {
		t.Fatalf("reverted proxy admitted reserve binding: %d %s", code, diagnostic)
	}
	if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmReserveLinkStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reverted proxy acquired binding custody: %v", err)
	}
}

// Rehashing an altered event position or semantic digest cannot satisfy retained
// completion. Additive binding fields cannot be grafted onto ancestor outcomes.
func TestEvmReserveLinkRetainedCompletionRequiresBindingDigest(t *testing.T) {
	f := newEvmReserveLinkFixture(t)
	f.prepareReserveLinkSigned()
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	stores, records := f.openReserveLinkAncestors()
	store, err := openEvmReserveLinkActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], false, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"event-position", "event-hash", "getter-hash", "storage-hash"} {
		changed := record
		receipt := *record.Receipt
		changed.Receipt = &receipt
		switch fault {
		case "event-position":
			receipt.RecorderLogIndex++
		case "event-hash":
			receipt.RecorderBindingHash = ""
		case "getter-hash":
			receipt.GetterHash = rootObjectHash(f.plan.Reserve.Getters)
		case "storage-hash":
			receipt.StorageHash = ""
		}
		changed.ContentHash = ""
		changed.ContentHash = rootObjectHash(changed)
		if err := validateEvmCreateCompletion(f.plan, changed); err == nil {
			t.Fatalf("retained %s omitted binding postcondition", fault)
		}
	}
	record.Receipt.RecorderLogIndex++
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
	if _, code, diagnostic := f.command("resume", "--action", "reserve-link"); code != 3 || !strings.Contains(diagnostic, "binding postconditions differ") || !maps.Equal(counts, f.counts) {
		t.Fatalf("changed retained binding was trusted: %d %s", code, diagnostic)
	}
	for index, legacy := range records {
		legacy.Receipt.RecorderBindingHash = record.Receipt.RecorderBindingHash
		legacy.ContentHash = ""
		legacy.ContentHash = rootObjectHash(legacy)
		if err := legacy.validateForAction(f.config, index); err == nil {
			t.Fatalf("ancestor %d accepted additive binding fields", index)
		}
	}
}
