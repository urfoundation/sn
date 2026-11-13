// Original proxy custody fixes the epoch clock even after normal head progress.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"maps"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
)

// The fixture executes all original contracts and loads the actual journals.
// Faults alter current getter bytes or real packed storage, never a receipt.
func TestBootstrapContractInstallationCurrentPolicyKeepsOriginalReceiptClock(t *testing.T) {
	f, client, plans, mapping := newValidatorActivationContractFixture(t)
	stores, records := f.openEvidenceAncestors()
	for _, store := range stores {
		t.Cleanup(func() { _ = store.close() })
	}
	store, err := openEvmEvidenceActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], records[5], records[6], false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	evidence, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, evidence)
	originalHash := rootObjectHash(records)
	proxyReceipt := *records[4].Receipt
	if proxyReceipt.BlockNumber == proxyReceipt.NativeNumber || proxyReceipt.BlockNumber == mapping.EvmHeader.Number || proxyReceipt.BlockNumber == plans[4].ProxyConstructor.ApprovedPolicy.EffectiveBlock {
		t.Fatal("fixture conflates original EVM inclusion with another clock")
	}
	evidenceOnly, err := client.observeValidatorActivationContracts(t.Context(), plans, mapping)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := client.observeBootstrapContractInstallationContracts(t.Context(), plans, records, mapping)
	if err != nil || !reflect.DeepEqual(installed, evidenceOnly) {
		t.Fatal("exact original policy changed current observation bytes", err)
	}
	for _, fault := range []string{"absent records", "missing receipt", "zero block", "wrong address", "future block"} {
		changed := slices.Clone(records)
		receipt := proxyReceipt
		changed[4].Receipt = &receipt
		switch fault {
		case "absent records":
			changed = nil
		case "missing receipt":
			changed[4].Receipt = nil
		case "zero block":
			receipt.BlockNumber = 0
		case "wrong address":
			receipt.ContractAddress = plans[7].Address.Hex()
		case "future block":
			receipt.BlockNumber = mapping.EvmHeader.Number + 1
		}
		f.stateLock.Lock()
		counts := maps.Clone(f.counts)
		f.stateLock.Unlock()
		if _, err := client.observeBootstrapContractInstallationContracts(t.Context(), plans, changed, mapping); err == nil || !strings.Contains(err.Error(), "lacks its original proxy inclusion") {
			t.Fatal("invalid original inclusion reached current observation", fault, err)
		}
		f.stateLock.Lock()
		same := maps.Equal(counts, f.counts)
		f.stateLock.Unlock()
		if !same {
			t.Fatal("invalid original inclusion contacted RPC", fault)
		}
	}
	selector := "0x" + hex.EncodeToString(stabi.NewSTCoordinator().PackPolicyByIndex(big.NewInt(0)))
	for _, height := range []uint64{proxyReceipt.BlockNumber - 1, proxyReceipt.BlockNumber + 1, mapping.EvmHeader.Number} {
		reached := false
		f.stateLock.Lock()
		f.override = func(method string, params []any, value any) any {
			if method == "eth_call" && params[0].(map[string]any)["to"] == plans[4].Address.Hex() && params[0].(map[string]any)["data"] == selector {
				reached = true
				data := common.FromHex(value.(string))
				copy(data[64:96], common.BigToHash(new(big.Int).SetUint64(height)).Bytes())
				return "0x" + hex.EncodeToString(data)
			}
			return value
		}
		f.stateLock.Unlock()
		_, err := client.observeBootstrapContractInstallationContracts(t.Context(), plans, records, mapping)
		f.stateLock.Lock()
		observed := reached
		f.override = nil
		f.stateLock.Unlock()
		if err == nil || !observed || !strings.Contains(err.Error(), "current policy clock differs from original proxy inclusion") {
			t.Fatal("changed current getter renewed the original policy clock", height, err)
		}
	}
	// This changes actual storage read by published code at a new EVM block.
	// The receipt and original inclusion state still contain the original clock.
	clockSlot := common.BigToHash(new(big.Int).Add(new(big.Int).SetBytes(crypto.Keccak256(common.LeftPadBytes([]byte{6}, 32))), big.NewInt(1)))
	f.stateLock.Lock()
	clock := f.state.GetState(plans[4].Address, clockSlot)
	f.stateLock.Unlock()
	if binary.BigEndian.Uint64(clock[16:24]) != proxyReceipt.BlockNumber {
		t.Fatal("independent storage oracle differs from original receipt")
	}
	binary.BigEndian.PutUint64(clock[16:24], proxyReceipt.BlockNumber+1)
	f.stateLock.Lock()
	f.state.SetState(plans[4].Address, clockSlot, clock)
	f.advanceEmpty()
	f.stateLock.Unlock()
	head, err := client.readIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	mapping, err = client.readFinalizedMappingAtIdentity(t.Context(), head)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.observeValidatorActivationContracts(t.Context(), plans, mapping); err != nil {
		t.Fatal("evidence-only fixture did not expose the past-block ambiguity", err)
	}
	if _, err := client.observeBootstrapContractInstallationContracts(t.Context(), plans, records, mapping); err == nil || !strings.Contains(err.Error(), "current policy clock differs from original proxy inclusion") {
		t.Fatal("real rewritten storage renewed the original policy clock", err)
	}
	if rootObjectHash(records) != originalHash {
		t.Fatal("current policy observation changed original custody")
	}
	f.stateLock.Lock()
	defer f.stateLock.Unlock()
	if len(f.writes) != 8 {
		t.Fatal("current policy observation submitted a transaction")
	}
}
