// Borrowed original markers remain part of receipt and installation authority
// throughout network observations, even when replacement bytes are identical.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Marker replacement occurs after native proof work has begun. The genuine
// earlier installation proves the fixture can complete without the custody loss.
func TestBootstrapContractInstallationLostBorrowedMarkerRefusesReadback(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	chain := f.original.contracts
	seal := bootstrapContractInstallationTestProofs(t, f)
	seal(true)
	var stdout bytes.Buffer
	if code, diagnostic := f.online(&stdout, true); code != 0 {
		t.Fatal("synthetic anchor submission", code, diagnostic)
	}
	seal(false)
	owner, adapter, close := f.openRuntimeRevisions()
	defer close()
	healthy, err := inspectBootstrapContractInstallation(t.Context(), owner, adapter)
	if err != nil || !healthy.InstallationComplete {
		t.Fatal("healthy installation control", err)
	}
	seal(true)
	markerPath := filepath.Join(f.original.config.RunDirectory, bootstrapContractStateFile(7)) + ".lock"
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	chain.stateLock.Lock()
	proofCalls, replaced := 0, false
	priorOverride := chain.override
	chain.override = func(method string, params []any, value any) any {
		value = priorOverride(method, params, value)
		if method == "state_getReadProof" {
			proofCalls++
			if proofCalls == 3 {
				if err := os.Rename(markerPath, markerPath+".retained"); err != nil {
					t.Error(err)
					return value
				}
				if err := os.WriteFile(markerPath, marker, 0600); err != nil {
					t.Error(err)
					return value
				}
				replaced = true
			}
		}
		return value
	}
	chain.stateLock.Unlock()
	observed, err := inspectBootstrapContractInstallation(t.Context(), owner, adapter)
	chain.stateLock.Lock()
	defer chain.stateLock.Unlock()
	if !replaced || err == nil || observed.InstallationComplete || len(chain.writes) != 9 {
		t.Fatalf("borrowed marker loss became installation: replaced=%v complete=%v error=%v writes=%d", replaced, observed.InstallationComplete, err, len(chain.writes))
	}
}

// A late receipt read can lose an earlier shared marker without changing its
// valid record. Historical verification must retain that physical ownership.
func TestBootstrapContractReceiptLostBorrowedMarkerRefusesReadback(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	bootstrapSuccessorCommandTestComplete(t, f)
	scope, err := openBootstrapContractReceiptScope(f.storageContext(t.Context()), f.path, f.config.RunDirectory, f.preparation.Plan.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.close()
	chain := f.contracts
	markerPath := filepath.Join(f.config.RunDirectory, bootstrapContractStateFile(0)) + ".lock"
	chain.stateLock.Lock()
	priorOverride := chain.override
	replaced := false
	chain.override = func(method string, params []any, value any) any {
		value = priorOverride(method, params, value)
		if method == "eth_getTransactionReceipt" && params[0] == scope.records[7].TransactionHash && !replaced {
			marker, err := os.ReadFile(markerPath)
			if err != nil {
				t.Error(err)
				return value
			}
			if err := os.Rename(markerPath, markerPath+".retained"); err != nil {
				t.Error(err)
				return value
			}
			if err := os.WriteFile(markerPath, marker, 0600); err != nil {
				t.Error(err)
				return value
			}
			replaced = true
		}
		return value
	}
	chain.stateLock.Unlock()
	observed, err := scope.inspect(t.Context())
	chain.stateLock.Lock()
	defer chain.stateLock.Unlock()
	if !replaced || err == nil || observed.CanonicalReceiptsVerified || len(chain.writes) != 8 {
		t.Fatalf("borrowed marker loss became receipt readiness: replaced=%v complete=%v error=%v writes=%d", replaced, observed.CanonicalReceiptsVerified, err, len(chain.writes))
	}
}
