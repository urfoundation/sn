// Public-header regressions cross the contract command, signed custody, genuine
// local EVM execution and the shared owned-HTTP fixture for all eight actions.
package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
)

// Both unsupported raw selectors use authenticated public headers throughout
// the complete prefix, without widening readiness or spending another nonce.
func TestEvmPublicMappingCompletesEveryContractAction(t *testing.T) {
	for _, unavailableCode := range []int{-32601, -32602} {
		f := newEvmEvidenceFixture(t)
		f.override = func(method string, _ []any, result any) any {
			if method == "debug_getRawHeader" {
				return mappingFixtureRpcError{code: unavailableCode}
			}
			return result
		}
		f.prepareEvidenceSigned()
		if _, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit"); code != 0 {
			t.Fatal(diagnostic)
		}
		result, code, diagnostic := f.command("resume", "--action", "evidence-create", "--online", "--submit")
		if code != 0 || result.Status != "evidence-created-unanchored" || result.Attempts != 1 || result.Receipt == nil || len(f.writes) != 8 || !bytes.Equal(f.writes[7], f.raw) || f.counts["eth_getBlockByHash"] == 0 || f.counts["debug_getRawHeader"] != f.counts["eth_getBlockByHash"] {
			t.Fatalf("raw code %d changed public completion or writes: %+v calls=%v exit=%d %s", unavailableCode, result, f.counts, code, diagnostic)
		}
		counts := maps.Clone(f.counts)
		statuses := []string{"reserve-created", "vault-created", "coordinator-created", "escrow-registered", "proxy-created-initialized", "reserve-recorder-bound", "vault-coordinator-bound", "evidence-created-unanchored"}
		for index, action := range f.config.Plan.Actions {
			retained, code, diagnostic := f.command("resume", "--action", action.Id)
			var transaction types.Transaction
			if err := transaction.UnmarshalBinary(f.writes[index]); err != nil {
				t.Fatal(err)
			}
			if code != 0 || retained.Status != statuses[index] || retained.Attempts != 1 || retained.Receipt == nil || retained.TransactionHash != transaction.Hash().Hex() || retained.ReceiptObservation != "retained" || retained.InstallationComplete || retained.ActivationReady || !maps.Equal(counts, f.counts) {
				t.Fatalf("raw code %d changed retained %s: %+v exit=%d %s", unavailableCode, action.Id, retained, code, diagnostic)
			}
			if retained.Receipt.TransactionHash != transaction.Hash().Hex() || retained.Receipt.BlockNumber != uint64(38+index) || retained.Receipt.NativeNumber != uint64(101+index) || retained.Receipt.NativeHash != f.hashes[uint64(101+index)] || f.evmHeaders[retained.Receipt.BlockHash] == nil || f.evmHeaders[retained.Receipt.BlockHash].Number.Uint64() != retained.Receipt.BlockNumber {
				t.Fatalf("raw code %d lost independent native/EVM inclusion for %s: %+v", unavailableCode, action.Id, retained.Receipt)
			}
		}
	}
}

// Incomplete or contradictory public evidence cannot seal a receipt or change
// custody. Repairing that same public path recovers the one original attempt.
func TestEvmCreatePublicMappingRefusesBadEvidenceWithoutResubmission(t *testing.T) {
	for _, fault := range []string{"missing", "changed"} {
		f := newEvmCreateFixture(t)
		f.prepareSigned()
		if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
			t.Fatal(diagnostic)
		}
		path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		repaired := false
		f.override = func(method string, _ []any, result any) any {
			if method == "debug_getRawHeader" {
				return mappingFixtureRpcError{code: -32601}
			}
			if method == "eth_getBlockByHash" && !repaired {
				fields := result.(map[string]any)
				if fault == "missing" {
					delete(fields, "nonce")
				} else {
					fields["stateRoot"] = testGenesisHash
				}
			}
			return result
		}
		_, code, diagnostic := f.command("resume", "--online", "--submit")
		wantDiagnostic := "mapping is unavailable"
		if fault == "changed" {
			wantDiagnostic = "public EVM fields cannot reproduce the native commitment"
		}
		if code != 1 || !strings.Contains(diagnostic, wantDiagnostic) || f.counts["eth_getBlockByHash"] != 1 || len(f.writes) != 1 {
			t.Fatalf("%s public header was accepted or misclassified: %d %s", fault, code, diagnostic)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s public header changed original custody: %v", fault, err)
		}
		repaired = true
		result, code, diagnostic := f.command("resume", "--online", "--submit")
		if code != 0 || result.Status != "reserve-created" || result.Attempts != 1 || result.Receipt == nil || result.Receipt.BlockHash != f.receipt["blockHash"] || result.TransactionHash != f.tx.Hash().Hex() || len(f.writes) != 1 || !bytes.Equal(f.writes[0], f.raw) || f.counts["eth_getBlockByHash"] <= 1 {
			t.Fatalf("repaired %s public header lost original attempt: %+v exit=%d %s", fault, result, code, diagnostic)
		}
	}
}
