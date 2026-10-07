// Atomic proxy regressions use genuine delegatecall initialization and exact
// historical state through the public command. Native escrow remains modeled.
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

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Real proxy creation initializes policy, independent governance roles and all
// five storage slots in one transaction, preserving all four ancestor journals.
func TestEvmProxyCreateExecutesAtomicInitializerAndPreservesAncestors(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	before := map[string][]byte{}
	for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile} {
		raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = raw
	}
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("proxy submission: %+v %d %s", result, code, diagnostic)
	}
	proxyStorageReads := 0
	f.override = func(method string, params []any, result any) any {
		if method == "eth_getStorageAt" && params[0] == f.plan.Address.Hex() {
			block := params[2].(map[string]any)
			if block["blockHash"] != f.receipt["blockHash"] || block["requireCanonical"] != true {
				t.Error("proxy storage escaped canonical inclusion")
			}
			proxyStorageReads++
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit")
	if code != 0 || result.Status != "proxy-created-initialized" || result.Receipt == nil || result.Receipt.ContractAddress != f.plan.Address.Hex() || result.Receipt.StorageHash != rootObjectHash(f.plan.Storage) || proxyStorageReads != 5 || result.Receipt.NativeNumber == result.Receipt.BlockNumber {
		t.Fatalf("proxy initialization: %+v %d %s", result, code, diagnostic)
	}
	getters, err := f.plan.receiptGetters(*result.Receipt)
	if err != nil || len(getters) != 24 || result.Receipt.GetterHash != rootObjectHash(getters) || result.Receipt.RuntimeHash != crypto.Keccak256Hash(f.plan.Runtime).Hex() || result.Receipt.RegistrationHash != "" || result.ReceiptObservation != "revalidated-online" {
		t.Fatalf("proxy postcondition projection: %+v %v", result, err)
	}
	if result.ProxyAddress != f.plan.Address.Hex() || result.ProxyConstructor == nil || *result.ProxyConstructor != *f.plan.ProxyConstructor || result.ProxyConstructor.ApprovedPolicy.EffectiveEpoch != 99 || result.ProxyConstructor.ApprovedPolicy.EffectiveBlock != 999 || result.CoordinatorAddress != f.plan.Coordinator.Address.Hex() || result.VaultAddress != f.plan.Vault.Address.Hex() || result.ExecutableAction != "proxy-create" || result.PlanHash != f.config.Plan.hash() || result.InstallationComplete || result.ActivationReady || len(result.RemainingActions) != 4 || len(f.writes) != 5 || !bytes.Equal(f.writes[4], f.raw) {
		t.Fatalf("proxy changed approval or claimed installation: %+v", result)
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "proxy-create")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || retained.ReceiptObservation != "retained" || !maps.Equal(counts, f.counts) {
		t.Fatalf("proxy offline completion: %+v %d %s", retained, code, diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 || len(f.writes) != 5 {
		t.Fatalf("proxy completion resent: %d %s", code, diagnostic)
	}
	for name, raw := range before {
		after, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, name))
		if err != nil || !bytes.Equal(raw, after) {
			t.Fatalf("proxy changed historical %s: %v", name, err)
		}
	}
	for _, action := range []string{"reserve-create", "vault-create", "coordinator-create", "escrow-register"} {
		legacy, code, diagnostic := f.command("resume", "--action", action)
		raw, err := json.Marshal(legacy)
		if err != nil || code != 0 || bytes.Contains(raw, []byte("coordinator_proxy_address")) || bytes.Contains(raw, []byte("proxy_constructor")) {
			t.Fatalf("legacy %s acquired proxy fields: %d %s %v", action, code, diagnostic, err)
		}
	}
}

// Constructor and initializer ABI layers must repack exactly, even when an
// altered envelope has a new valid fixture approval. Governance comes from it.
func TestEvmProxyCreateRejectsChangedConstructorAndInitializer(t *testing.T) {
	f := newEvmProxyFixture(t)
	original := f.config.Plan.Actions[4]
	var artifact contractReleaseArtifact
	for _, candidate := range evmTestRelease(t).Artifacts {
		if candidate.Name == "ERC1967Proxy" {
			artifact = candidate
		}
	}
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		t.Fatal(err)
	}
	creation, _ := hex.DecodeString(artifact.Creation)
	raw, _ := hex.DecodeString(original.Data[2:])
	arguments, err := parsed.Constructor.Inputs.Unpack(raw[len(creation):])
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"creation", "constructor-trailing", "constructor-offset", "implementation", "empty-initializer", "initializer-selector", "initializer-trailing", "netuid", "mirror", "vault", "reserve", "zero-owner", "zero-guardian", "zero-oracle", "wide-uint64", "nonce", "sender", "call-target", "value"} {
		action := original
		initializer := append([]byte(nil), arguments[1].([]byte)...)
		implementation := arguments[0].(common.Address)
		switch fault {
		case "creation":
			action.Data = "0x00"
		case "constructor-trailing":
			action.Data += "00"
		case "constructor-offset":
			changed := append([]byte(nil), raw...)
			changed[len(creation)+63] = 0x60
			action.Data = "0x" + hex.EncodeToString(changed)
		case "implementation":
			implementation = crypto.CreateAddress(action.Sender, 0)
		case "empty-initializer":
			initializer = nil
		case "initializer-selector":
			initializer[0] ^= 1
		case "initializer-trailing":
			initializer = append(initializer, 0)
		case "netuid":
			initializer[4+31] = 26
		case "mirror":
			initializer[4+3*32] ^= 1
		case "vault":
			initializer[4+4*32+31] ^= 1
		case "reserve":
			initializer[4+5*32+31] ^= 1
		case "zero-owner", "zero-guardian", "zero-oracle":
			index := map[string]int{"zero-owner": 1, "zero-guardian": 2, "zero-oracle": 6}[fault]
			clear(initializer[4+index*32 : 4+(index+1)*32])
		case "wide-uint64":
			initializer[4+10*32] = 1
		case "nonce":
			action.Nonce++
		case "sender":
			action.Sender = common.Address{44}
		case "call-target":
			target := common.Address{44}
			action.To = &target
		case "value":
			action.ValueWei = "1"
		}
		if !strings.HasPrefix(fault, "constructor-") && fault != "creation" {
			packed, err := parsed.Pack("", implementation, initializer)
			if err != nil {
				t.Fatal(err)
			}
			action.Data = "0x" + artifact.Creation + hex.EncodeToString(packed)
		}
		f.config.Plan.Actions[4], f.config.Plan.MaximumTotalWei = action, "2190000001"
		f.publishConfig()
		if _, code, diagnostic := f.command("plan", "--action", "proxy-create"); code != 2 || !strings.Contains(diagnostic, "selected action") || len(f.counts) != 0 {
			t.Fatalf("proxy %s accepted: %d %s", fault, code, diagnostic)
		}
	}
}

// Source validity bounds are checked before custody, including uint256 claim
// horizon arithmetic at the vault's immutable minimum window boundary.
func TestEvmProxyCreateRejectsInvalidInitialPolicy(t *testing.T) {
	f := newEvmProxyFixture(t)
	original := f.config.Plan.Actions[4]
	var artifact contractReleaseArtifact
	for _, candidate := range evmTestRelease(t).Artifacts {
		if candidate.Name == "ERC1967Proxy" {
			artifact = candidate
		}
	}
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := hex.DecodeString(original.Data[2+len(artifact.Creation):])
	arguments, err := parsed.Constructor.Inputs.Unpack(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []struct {
		index int
		value uint64
	}{{index: 7, value: 0}, {index: 10, value: 0}, {index: 11, value: 0}, {index: 12, value: 0}, {index: 13, value: 0}, {index: 14, value: 0}, {index: 16, value: 0}, {index: 17, value: 0}, {index: 18, value: 0}, {index: 19, value: 0}, {index: 13, value: 61}, {index: 11, value: 121}, {index: 12, value: 3600}, {index: 15, value: 3}, {index: 18, value: 1_000_000_001}, {index: 15, value: 0}} {
		initializer := append([]byte(nil), arguments[1].([]byte)...)
		copy(initializer[4+fault.index*32:4+(fault.index+1)*32], common.LeftPadBytes(new(big.Int).SetUint64(fault.value).Bytes(), 32))
		packed, err := parsed.Pack("", arguments[0], initializer)
		if err != nil {
			t.Fatal(err)
		}
		f.config.Plan.Actions[4] = original
		f.config.Plan.Actions[4].Data = "0x" + artifact.Creation + hex.EncodeToString(packed)
		f.publishConfig()
		if _, code, diagnostic := f.command("plan", "--action", "proxy-create"); code != 2 || !strings.Contains(diagnostic, "policy") || len(f.counts) != 0 {
			t.Fatalf("proxy policy word %d=%d accepted: %d %s", fault.index, fault.value, code, diagnostic)
		}
	}
}

// uint256 caps survive both ABI layers and genuine storage. The exact allowed
// claim horizon succeeds; one more finalize block fails before any new custody.
func TestEvmProxyCreatePreservesFullWidthCapsAndExactClaimWindow(t *testing.T) {
	f := newEvmProxyFixture(t)
	var artifact contractReleaseArtifact
	for _, candidate := range evmTestRelease(t).Artifacts {
		if candidate.Name == "ERC1967Proxy" {
			artifact = candidate
		}
	}
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := hex.DecodeString(f.config.Plan.Actions[4].Data[2+len(artifact.Creation):])
	arguments, err := parsed.Constructor.Inputs.Unpack(raw)
	if err != nil {
		t.Fatal(err)
	}
	initializer := append([]byte(nil), arguments[1].([]byte)...)
	clear(initializer[4+15*32 : 4+16*32])
	maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for _, index := range []int{18, 19} {
		copy(initializer[4+index*32:4+(index+1)*32], maximum.Bytes())
	}
	packed, err := parsed.Pack("", arguments[0], initializer)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Plan.Actions[4].Data = "0x" + artifact.Creation + hex.EncodeToString(packed)
	vault, _ := hex.DecodeString(f.config.Plan.Actions[1].Data[2:])
	start := len(vault) - 6*32
	copy(vault[start+3*32:start+4*32], common.LeftPadBytes(big.NewInt(7079).Bytes(), 32))
	f.config.Plan.Actions[1].Data = "0x" + hex.EncodeToString(vault)
	f.publishConfig()
	f.prepareProxySigned()
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online"); code != 0 || result.Status != "proxy-created-initialized" || result.ProxyConstructor.ApprovedPolicy.EpochDepositCapRao != maximum.String() || result.ProxyConstructor.ApprovedPolicy.CampaignDepositCapRao != maximum.String() {
		t.Fatalf("full-width exact-window proxy: %+v %d %s", result, code, diagnostic)
	}
	copy(initializer[4+12*32:4+13*32], common.LeftPadBytes(big.NewInt(121).Bytes(), 32))
	packed, err = parsed.Pack("", arguments[0], initializer)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Plan.Actions[4].Data = "0x" + artifact.Creation + hex.EncodeToString(packed)
	f.publishConfig()
	counts := maps.Clone(f.counts)
	if _, code, diagnostic := f.command("plan", "--action", "proxy-create"); code != 2 || !strings.Contains(diagnostic, "immutable vault claim window") || !maps.Equal(counts, f.counts) {
		t.Fatalf("proxy accepted one-block deficient window: %d %s", code, diagnostic)
	}
}

// Every static/dynamic getter and each distinct proxy namespace is observed at
// its genuine inclusion; clearing one fault recovers without another CREATE.
func TestEvmProxyCreateRequiresEveryGetterAndStorageWord(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	blockNumber, err := evmQuantity(f.receipt["blockNumber"].(string), 64)
	if err != nil {
		t.Fatal(err)
	}
	getters, err := f.plan.receiptGetters(evmCreateReceipt{BlockNumber: blockNumber.Uint64()})
	if err != nil {
		t.Fatal(err)
	}
	for fault := -6; fault < len(getters); fault++ {
		observed := false
		f.override = func(method string, params []any, result any) any {
			if fault == -6 && method == "eth_getCode" && params[0] == f.plan.Address.Hex() {
				observed = true
				return "0x00"
			}
			if fault < 0 && fault >= -5 && method == "eth_getStorageAt" && params[0] == f.plan.Address.Hex() && params[1] == f.plan.Storage[fault+5].Slot {
				observed = true
				return "0x" + strings.Repeat("01", 32)
			}
			if fault >= 0 && method == "eth_call" {
				input := params[0].(map[string]any)
				if input["to"] == f.plan.Address.Hex() && input["data"] == getters[fault].Data {
					observed = true
					return "0x00"
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "differs") || len(f.writes) != 5 {
			t.Fatalf("proxy postcondition %d accepted: %d %s", fault, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online"); code != 0 || result.Status != "proxy-created-initialized" {
		t.Fatalf("proxy postcondition recovery: %+v %d %s", result, code, diagnostic)
	}
}

// A successful status cannot waive explicit CREATE identity or the original
// transaction's financial bounds. Repairing the observation needs no resend.
func TestEvmProxyCreateRequiresOriginalReceiptIdentity(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for _, fault := range []string{"sender", "missing-sender", "target", "missing-target", "created-address", "transaction", "gas", "fee"} {
		f.override = func(method string, params []any, result any) any {
			if method != "eth_getTransactionReceipt" || params[0] != f.tx.Hash().Hex() {
				return result
			}
			changed := maps.Clone(result.(map[string]any))
			switch fault {
			case "sender":
				changed["from"] = f.plan.ProxyConstructor.Owner.Hex()
			case "missing-sender":
				delete(changed, "from")
			case "target":
				changed["to"] = f.plan.Address.Hex()
			case "missing-target":
				delete(changed, "to")
			case "created-address":
				changed["contractAddress"] = f.plan.Coordinator.Address.Hex()
			case "transaction":
				changed["transactionHash"] = common.Hash{44}.Hex()
			case "gas":
				changed["gasUsed"] = fmt.Sprintf("0x%x", f.tx.Gas()+1)
			case "fee":
				changed["effectiveGasPrice"] = "0xb"
			}
			return changed
		}
		if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 1 || len(f.writes) != 5 {
			t.Fatalf("proxy receipt %s accepted: %d %s", fault, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online"); code != 0 || result.Status != "proxy-created-initialized" {
		t.Fatalf("proxy receipt recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Policy effective block must be the distinct EVM inclusion height and remain
// there after later heads advance. Input epoch/block words stay approved bytes.
func TestEvmProxyCreateBindsInitialPolicyToEvmInclusion(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	f.advanceEmpty()
	f.advanceEmpty()
	// Deliberately move the fixture's live VM clock beyond initialization. The
	// historical adapter must select the included header for every eth_call.
	f.vm.BlockNumber = big.NewInt(8_000)
	for _, wrong := range []uint64{105, 999, 8_000} {
		f.override = func(method string, params []any, result any) any {
			if method == "eth_call" && params[0].(map[string]any)["to"] == f.plan.Address.Hex() {
				getters, err := f.plan.receiptGetters(evmCreateReceipt{BlockNumber: wrong})
				if err != nil {
					t.Error(err)
					return mappingFixtureRpcError{code: -32602}
				}
				for _, getter := range getters[len(f.plan.Getters):] {
					if params[0].(map[string]any)["data"] == getter.Data {
						return getter.Expected
					}
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "constructor getter differs") || len(f.writes) != 5 {
			t.Fatalf("proxy trusted policy height %d: %d %s", wrong, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online"); code != 0 || result.Status != "proxy-created-initialized" || result.Receipt.BlockNumber == 105 || result.Receipt.BlockNumber == f.vm.BlockNumber.Uint64() {
		t.Fatalf("proxy historical block normalization: %+v %d %s", result, code, diagnostic)
	}
}

// Proxy implementation identity is also read at the proxy inclusion, rather
// than inferred solely from its earlier successful constructor receipt.
func TestEvmProxyCreateRechecksImplementationAtProxyInclusion(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	for _, fault := range []string{"runtime", "initializer", "implementation", "archive"} {
		observed := false
		f.override = func(method string, params []any, result any) any {
			if (method != "eth_getCode" && method != "eth_getStorageAt") || params[0] != f.plan.Coordinator.Address.Hex() || params[len(params)-1].(map[string]any)["blockHash"] != f.receipt["blockHash"] {
				return result
			}
			if fault == "runtime" && method == "eth_getCode" || method == "eth_getStorageAt" && (fault == "archive" || fault == "initializer" && params[1] == f.plan.Coordinator.Storage[0].Slot || fault == "implementation" && params[1] == f.plan.Coordinator.Storage[1].Slot) {
				observed = true
				if fault == "archive" {
					return mappingFixtureRpcError{code: -32602}
				}
				return "0x00"
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 1 || !observed || len(f.writes) != 5 {
			t.Fatalf("proxy implementation %s accepted: %d %s", fault, code, diagnostic)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online"); code != 0 || result.Status != "proxy-created-initialized" {
		t.Fatalf("proxy implementation recovery: %+v %d %s", result, code, diagnostic)
	}
}

// Unsigned review exposes exact governance/policy and five storage expectations
// under the whole original graph hash, without inspecting the future directory.
func TestEvmProxyCreatePreviewPreservesWholeApprovedGraph(t *testing.T) {
	f := newEvmProxyFixture(t)
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
	code := runMain(f.storageContext(context.Background()), []string{"bootstrap-contracts", "preview", "--action", "proxy-create", "--config", f.configPath}, &stdout, &stderr)
	var preview evmPhasePreview
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil || code != 0 || preview.PlanHash != f.config.Plan.hash() || preview.ApprovalVerified || preview.InstallationComplete || preview.ExecutableAction != "proxy-create" || preview.ProxyConstructor == nil || preview.ProxyConstructor.ApprovedPolicy.EffectiveBlock != 999 || preview.ProxyAddress != crypto.CreateAddress(f.config.Plan.Actions[4].Sender, 4).Hex() || len(preview.ProxyStorage) != 5 || preview.Plan.Actions[4].Data != f.config.Plan.Actions[4].Data {
		t.Fatalf("proxy preview: %+v %d %v %s", preview, code, err, stderr.String())
	}
	if _, err := os.Lstat(f.config.Plan.RunDirectory); !errors.Is(err, os.ErrNotExist) || len(f.counts) != 0 {
		t.Fatalf("proxy preview acquired custody: %v", err)
	}
}

// Signed or merely included escrow is insufficient; only its exact retained
// event/mapping completion opens the fifth journal under the original graph.
func TestEvmProxyCreateRequiresCompletedEscrow(t *testing.T) {
	f := newEvmProxyFixture(t)
	refused := func(stage string) {
		t.Helper()
		counts := maps.Clone(f.counts)
		if _, code, diagnostic := f.command("apply", "--action", "proxy-create"); code != 3 {
			t.Fatalf("%s escrow admitted proxy: %d %s", stage, code, diagnostic)
		}
		if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmProxyCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) || !maps.Equal(counts, f.counts) {
			t.Fatalf("%s escrow opened proxy custody: %v", stage, err)
		}
	}
	refused("absent")
	f.prepareEscrowSigned()
	refused("signed")
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	refused("included")
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("apply", "--action", "proxy-create"); code != 0 || result.Status != "signature-awaiting-import" {
		t.Fatalf("completed escrow did not unlock proxy: %+v %d %s", result, code, diagnostic)
	}
}

// All four independent historical receipts are audited, including the escrow's
// registered state separately from its earlier unregistered vault constructor.
func TestEvmProxyCreateReauditsFourPredecessors(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	path := filepath.Join(f.config.Plan.RunDirectory, evmProxyCreateStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for ancestor := 0; ancestor < 4; ancestor++ {
		observed := false
		f.override = func(method string, params []any, result any) any {
			if method == "eth_getTransactionReceipt" && result != nil {
				if receipt, ok := result.(map[string]any); ok && receipt != nil {
					for hash, tx := range f.history.transactions {
						if tx.Nonce() == uint64(ancestor) && receipt["blockHash"] == hash {
							observed = true
							changed := maps.Clone(receipt)
							changed["gasUsed"] = "0x0"
							return changed
						}
					}
				}
			}
			return result
		}
		if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 1 || !observed || !strings.Contains(diagnostic, "contradicts") || len(f.writes) != 4 {
			t.Fatalf("proxy skipped ancestor %d: %d %s", ancestor, code, diagnostic)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("proxy audit fault changed custody: %v", err)
		}
	}
	f.override = nil
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("proxy ancestor recovery: %+v %d %s", result, code, diagnostic)
	}
}

// After all four audits, each retained checkpoint still fences initial and
// refreshed proxy admission. A shared start hash cannot replace that lineage.
func TestEvmProxyCreateFencesFourPredecessorCheckpoints(t *testing.T) {
	for _, number := range []uint64{101, 102, 103, 104} {
		for _, refresh := range []bool{false, true} {
			f := newEvmProxyFixture(t)
			f.prepareProxySigned()
			originalHash := f.hashes[number]
			original := f.headers[originalHash]
			fork, forkHash := evmTestNativeHeader(t, original.ParentHash, number, append(append([]string(nil), original.Digest.Logs...), "0x0000"))
			f.headers[forkHash] = fork
			mappingObserved, targetObserved, changed := false, false, false
			f.override = func(method string, params []any, result any) any {
				if method == "eth_call" && params[0].(map[string]any)["to"] == "0x0000000000000000000000000000000000000802" {
					mappingObserved = true
				}
				if method == "eth_getCode" && params[0] == f.plan.Address.Hex() && params[1] == "pending" {
					targetObserved = true
				}
				if method == "chain_getFinalizedHead" && mappingObserved && (!refresh || targetObserved) && !changed {
					changed = true
					f.hashes[number] = forkHash
					return f.hashes[f.head]
				}
				return result
			}
			if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 1 || !changed || !strings.Contains(diagnostic, "ancestry") || len(f.writes) != 4 {
				t.Fatalf("proxy checkpoint %d refresh=%v accepted: %d %s", number, refresh, code, diagnostic)
			}
			f.override = nil
			f.hashes[number] = originalHash
			if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
				t.Fatalf("proxy checkpoint recovery: %+v %d %s", result, code, diagnostic)
			}
		}
	}
}

// A lost acknowledgement retains the one already executed atomic CREATE and
// recovers its original signature/nonce without creating another proxy.
func TestEvmProxyCreateLostReplyRecoversOriginalInitializer(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	f.loseReply = true
	if _, code, _ := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 1 || len(f.writes) != 5 {
		t.Fatalf("uncertain proxy reply retried or acknowledged: %d", code)
	}
	retained, code, diagnostic := f.command("resume", "--action", "proxy-create")
	if code != 0 || retained.Attempts != 1 || retained.Receipt != nil || retained.TransactionHash != f.tx.Hash().Hex() {
		t.Fatalf("uncertain proxy liability lost: %+v %d %s", retained, code, diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit")
	if code != 0 || result.Status != "proxy-created-initialized" || result.Attempts != 1 || result.TransactionHash != retained.TransactionHash || len(f.writes) != 5 || !bytes.Equal(f.writes[4], f.raw) {
		t.Fatalf("uncertain proxy acquired replacement authority: %+v %d %s", result, code, diagnostic)
	}
}

// Four predecessor submissions have spent four of five approved attempts.
func TestEvmProxyCreateKeepsCumulativeGraphAttempts(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.config.Plan.MaximumAttempts = 5
	f.publishConfig()
	f.prepareProxySigned()
	f.mine = false
	for attempt := 0; attempt < 3; attempt++ {
		result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit")
		if code != 0 || result.Attempts != 1 || attempt > 0 && result.Status != "attempt-allowance-exhausted" {
			t.Fatalf("proxy renewed graph allowance: %+v %d %s", result, code, diagnostic)
		}
	}
	if len(f.writes) != 5 {
		t.Fatal("proxy exceeded cumulative graph attempts")
	}
}

// Later sealed value and gas remain funded while only proxy creation is enabled.
func TestEvmProxyCreatePreservesLaterValueAndGasFunding(t *testing.T) {
	f := newEvmProxyFixture(t)
	target := crypto.CreateAddress(f.config.Plan.Actions[0].Sender, 0)
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "reserve-link", Sender: f.config.Plan.Actions[0].Sender, Nonce: 5, To: &target, Data: "0x00", ValueWei: "3000000000", Gas: 1_000_000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = "5200000000"
	f.publishConfig()
	f.prepareProxySigned()
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 3_029_999_999)
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "maximum liability") || len(f.writes) != 4 {
		t.Fatalf("proxy spent later reserved funds: %d %s", code, diagnostic)
	}
	f.override = func(method string, _ []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", 3_030_000_000)
		}
		return result
	}
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("proxy exact sufficient funding refused: %+v %d %s", result, code, diagnostic)
	}
}

// Durable publication can fail after rename. The poisoned owner cannot write,
// and reopening counts that ambiguous attempt alongside all four ancestors.
func TestEvmProxyCreateAmbiguousPublicationRetainsAttempt(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	f.mine = false
	stores, records := f.openProxyAncestors()
	store, err := openEvmProxyActionStore(f.plan, records[0], records[1], records[2], records[3], false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.client.httpClient.CloseIdleConnections()
	owner, err := newEvmProxyCreateOwner(f.plan, store, stores[0], stores[1], stores[2], stores[3], chain)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	store.syncDirectory = func(*os.File) error {
		count++
		if count == 2 {
			return errors.New("synthetic proxy attempt publication interruption")
		}
		return nil
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil {
		t.Fatal("ambiguous proxy publication acknowledged")
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil || len(f.writes) != 4 {
		t.Fatal("poisoned proxy owner reached transport")
	}
	store.close()
	for _, prior := range stores {
		prior.close()
	}
	result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit")
	if code != 0 || result.Attempts != 2 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 5 {
		t.Fatalf("proxy reopen renewed ambiguous attempt: %+v %d %s", result, code, diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 || result.Status != "attempt-allowance-exhausted" || result.Attempts != 2 || len(f.writes) != 5 {
		t.Fatalf("proxy exceeded original ambiguous allowance: %+v %d %s", result, code, diagnostic)
	}
}

// Exact initial-claim boundaries recover under five locks. A lost completed
// child cannot reset signature or submission authority on apply or resume.
func TestEvmProxyCreateClaimRecoveryKeepsFiveLocks(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "record-synced"} {
		f := newEvmProxyFixture(t)
		f.prepareProxyPrerequisites()
		stores, records := f.openProxyAncestors()
		store, err := openEvmProxyActionStore(f.plan, records[0], records[1], records[2], records[3], true, func(stage string) error {
			if stage == boundary {
				return errors.New("synthetic proxy initial claim interruption")
			}
			return nil
		}, f.storage.Context)
		if err == nil || store != nil {
			t.Fatalf("proxy %s interruption acknowledged", boundary)
		}
		store, err = openEvmProxyActionStore(f.plan, records[0], records[1], records[2], records[3], false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.load()
		if err != nil || record.Signed != "" || record.Attempts != 0 || record.PredecessorHash != rootObjectHash(records[3]) {
			t.Fatalf("proxy claim recovery changed custody: %+v %v", record, err)
		}
		for index := 0; index < 5; index++ {
			predecessor := ""
			if index > 0 {
				predecessor = rootObjectHash(records[index-1])
			}
			if other, err := openEvmSelectedActionStore(f.config, index, predecessor, false, nil, f.storage.Context); err == nil {
				other.close()
				t.Fatalf("proxy lost held lock %d", index)
			}
		}
		store.close()
		if err := os.Remove(filepath.Join(f.config.Plan.RunDirectory, evmProxyCreateStateFile)); err != nil {
			t.Fatal(err)
		}
		for _, create := range []bool{false, true} {
			if other, err := openEvmProxyActionStore(f.plan, records[0], records[1], records[2], records[3], create, nil, f.storage.Context); err == nil {
				other.close()
				t.Fatal("lost proxy child renewed allowance")
			}
		}
		for _, prior := range stores {
			prior.close()
		}
	}
}

// A self-consistent changed ancestor still differs from the exact hash sealed
// into the next marker, including the immediate escrow event/mapping journal.
func TestEvmProxyCreateRejectsChangedFourAncestorLineage(t *testing.T) {
	for ancestor := 0; ancestor < 4; ancestor++ {
		f := newEvmProxyFixture(t)
		f.prepareProxySigned()
		stores, records := f.openProxyAncestors()
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
		if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 3 || !strings.Contains(diagnostic, "marker differs") || !maps.Equal(counts, f.counts) || len(f.writes) != 4 {
			t.Fatalf("proxy learned changed ancestor %d: %d %s", ancestor, code, diagnostic)
		}
	}
}

// A genuine exhausted constructor leaves no proxy code or initialized storage,
// while consuming exactly the original CREATE nonce and retaining that outcome.
func TestEvmProxyCreateRevertRetainsConsumedNonce(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.config.Plan.Actions[4].Gas = 21_000
	f.publishConfig()
	f.prepareProxySigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit")
	if code != 0 || result.Status != "create-reverted-nonce-consumed" || result.Receipt == nil || result.Receipt.Status != 0 || result.Receipt.RuntimeHash != "" || result.Receipt.GetterHash != "" || result.Receipt.StorageHash != "" || len(f.writes) != 5 || len(f.state.GetCode(f.plan.Address)) != 0 || f.state.GetNonce(f.config.Plan.Actions[4].Sender) != 5 {
		t.Fatalf("proxy revert created success or replacement authority: %+v %d %s", result, code, diagnostic)
	}
	for _, word := range f.plan.Storage {
		if f.state.GetState(f.plan.Address, common.HexToHash(word.Slot)) != (common.Hash{}) {
			t.Fatalf("failed proxy retained storage at %s", word.Slot)
		}
	}
	counts := maps.Clone(f.counts)
	retained, code, diagnostic := f.command("resume", "--action", "proxy-create")
	if code != 0 || retained.Status != result.Status || retained.Receipt == nil || *retained.Receipt != *result.Receipt || !maps.Equal(counts, f.counts) {
		t.Fatalf("offline proxy revert changed custody: %+v %d %s", retained, code, diagnostic)
	}
}

// A consumed escrow nonce that reverted is not a completed prerequisite even
// though the deployer's next nonce equals the approved proxy reservation.
func TestEvmProxyCreateRefusesRevertedEscrow(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.config.Plan.Actions[3].Gas = 21_000
	f.publishConfig()
	f.prepareEscrowSigned()
	f.gasFailure = true
	if _, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := f.command("resume", "--action", "escrow-register", "--online"); code != 0 || result.Status != "escrow-registration-reverted-nonce-consumed" {
		t.Fatalf("escrow did not genuinely revert: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("apply", "--action", "proxy-create"); code != 3 || !strings.Contains(diagnostic, "successful escrow") {
		t.Fatalf("reverted escrow admitted proxy: %d %s", code, diagnostic)
	}
	if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, evmProxyCreateStateFile) + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reverted escrow acquired proxy custody: %v", err)
	}
}

// Convenience output failure follows durable initialization receipt publication.
func TestEvmProxyCreateOutputFailureRetainsInitialization(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	args := []string{"bootstrap-contracts", "resume", "--action", "proxy-create", "--config", f.configPath, "--run-dir", f.config.Plan.RunDirectory, "--accept-plan-hash", f.config.Plan.hash(), "--online", "--submit"}
	var stderr bytes.Buffer
	if code := runMain(f.storageContext(context.Background()), args, bootstrapRootFailedWriter{}, &stderr); code != 1 || !strings.Contains(stderr.String(), "output failed") {
		t.Fatalf("proxy output failed before authoritative publication: %d %s", code, stderr.String())
	}
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create"); code != 0 || result.Status != "proxy-created-initialized" || result.Receipt == nil || result.Receipt.StorageHash == "" || len(f.writes) != 5 {
		t.Fatalf("output loss erased proxy initialization: %+v %d %s", result, code, diagnostic)
	}
}

// Escrow's signature and every earlier custody path remain action-specific.
func TestEvmProxyCreateRejectsOtherSignatureAndFiveCustodyAliases(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxyPrerequisites()
	if _, code, diagnostic := f.command("apply", "--action", "proxy-create"); code != 0 {
		t.Fatal(diagnostic)
	}
	path := filepath.Join(f.config.Plan.RunDirectory, "escrow-register.signed.bin")
	_, hash, err := readBootstrapRootFile(context.Background(), path, 128*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.Contains(diagnostic, "signed-byte envelope") {
		t.Fatalf("escrow signature filled proxy custody: %d %s", code, diagnostic)
	}
	for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile} {
		path = filepath.Join(f.config.Plan.RunDirectory, name)
		_, hash, err = readBootstrapRootFile(context.Background(), path, 128*1024)
		if err != nil {
			t.Fatal(err)
		}
		if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--signed-transaction", path, "--signed-transaction-hash", hash); code != 2 || !strings.Contains(diagnostic, "aliases custody") {
			t.Fatalf("proxy input aliased %s: %d %s", name, code, diagnostic)
		}
	}
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create"); code != 0 || result.Status != "signature-awaiting-import" || result.Attempts != 0 {
		t.Fatalf("proxy invalid import changed custody: %+v %d %s", result, code, diagnostic)
	}
}

// Expiry, runtime/nonce movement and existing pending target code preserve the
// original signature without opening a replacement or another graph attempt.
func TestEvmProxyCreateCurrentAdmissionRetainsOriginalLiability(t *testing.T) {
	for _, fault := range []string{"expiry", "confirmed", "pending", "runtime", "target-code"} {
		f := newEvmProxyFixture(t)
		if fault == "expiry" {
			f.config.Plan.ValidThroughNative = 104
			f.publishConfig()
		}
		f.prepareProxySigned()
		status := "approval-expired-signed-liability-retained"
		if fault == "expiry" || fault == "runtime" {
			f.advanceEmpty()
		}
		if fault != "expiry" {
			status = "nonce-consumed-receipt-unresolved"
			if fault == "pending" {
				status = "pending-nonce-unresolved"
			}
			f.override = func(method string, params []any, result any) any {
				if (fault == "confirmed" || fault == "pending") && method == "eth_getTransactionCount" && (fault == "confirmed" || params[1] == "pending") {
					return "0x5"
				}
				if fault == "runtime" && method == "state_getRuntimeVersion" && params[0] == f.hashes[f.head] {
					version := f.config.Plan.Runtime.RuntimeVersion
					version.SpecVersion++
					return version
				}
				if fault == "target-code" && method == "eth_getCode" && params[0] == f.plan.Address.Hex() && params[1] == "pending" {
					return "0x00"
				}
				return result
			}
		}
		result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit")
		if fault == "runtime" || fault == "target-code" {
			if code != 1 {
				t.Fatalf("proxy %s admitted: %d %s", fault, code, diagnostic)
			}
			result, code, diagnostic = f.command("resume", "--action", "proxy-create")
		} else if result.Status != status {
			t.Fatalf("proxy %s status: %+v %d %s", fault, result, code, diagnostic)
		}
		if code != 0 || result.Attempts != 0 || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 4 {
			t.Fatalf("proxy %s replaced original liability: %+v %d %s", fault, result, code, diagnostic)
		}
	}
}

// Later runtime change and expiry cannot erase a construction authenticated at
// its historical execution/parent runtime with the correct EVM policy height.
func TestEvmProxyCreateHistoricalInitializationAfterRuntimeChange(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.config.Plan.ValidThroughNative = 105
	f.publishConfig()
	f.prepareProxySigned()
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
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
	if result, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 || result.Status != "proxy-created-initialized" || result.Receipt == nil || result.Receipt.NativeNumber != 105 || len(f.writes) != 5 {
		t.Fatalf("later runtime erased proxy initialization: %+v %d %s", result, code, diagnostic)
	}
}

// Rehashing a record with static-only getters cannot omit the dynamic initial
// policy observation. Retained state is checked before any route is opened.
func TestEvmProxyCreateRetainedCompletionRequiresPolicyDigest(t *testing.T) {
	f := newEvmProxyFixture(t)
	f.prepareProxySigned()
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create", "--online"); code != 0 {
		t.Fatal(diagnostic)
	}
	stores, records := f.openProxyAncestors()
	store, err := openEvmProxyActionStore(f.plan, records[0], records[1], records[2], records[3], false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	record.Receipt.GetterHash = rootObjectHash(f.plan.Getters)
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
	if _, code, diagnostic := f.command("resume", "--action", "proxy-create"); code != 3 || !strings.Contains(diagnostic, "initialized proxy postconditions") || !maps.Equal(counts, f.counts) {
		t.Fatalf("proxy retained digest omitted initial policy: %d %s", code, diagnostic)
	}
}
