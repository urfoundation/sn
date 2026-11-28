// Completed public custody remains completed across offline command restarts.
// Retained observations never imply a fresh chain read or release a used nonce.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// The successful case completes real constructor/getter admission before a
// fresh offline invocation recovers the authoritative completed receipt.
func TestEvmCreateOfflineResumeRetainsCreatedReceipt(t *testing.T) {
	testEvmCreateOfflineReceipt(t, false)
}

// The failed case consumes its original nonce through genuine EVM gas failure,
// then retains the canonical status-zero receipt without another attempt.
func TestEvmCreateOfflineResumeRetainsRevertedReceipt(t *testing.T) {
	testEvmCreateOfflineReceipt(t, true)
}

// The legacy fixture branch without historical snapshots also separates read
// simulation from the execution budget, without modifying signed bytes or state.
func TestEvmCreateGetterReadBudgetPreservesExecutionBudget(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	f.vm.GasLimit = 1
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || result.Receipt == nil || result.Attempts != 1 || f.counts["eth_call"] != len(f.plan.Getters) {
		t.Fatalf("getter simulation inherited the transaction budget: %+v %d %s", result, code, diagnostic)
	}
	if f.vm.GasLimit != 1 || f.tx.Gas() != f.config.Plan.Actions[0].Gas || len(f.writes) != 1 || !bytes.Equal(f.writes[0], f.raw) || !bytes.Equal(f.state.GetCode(f.plan.Address), f.plan.Runtime) || f.state.GetNonce(f.config.Plan.Actions[0].Sender) != 1 {
		t.Fatal("getter simulation changed execution authority or deployed state")
	}
}

// Both terminal states cross the real command, disk, HTTP and execution paths;
// RPC counters and journal bytes are compared after reopening without --online.
func testEvmCreateOfflineReceipt(t *testing.T, reverted bool) {
	t.Helper()
	f := newEvmCreateFixture(t)
	status := "reserve-created"
	if reverted {
		status = "create-reverted-nonce-consumed"
		f.config.Plan.Actions[0].Gas = 200_000
		f.vm.GasLimit = f.config.Plan.Actions[0].Gas
		f.gasFailure = true
		f.publishConfig()
		private, err := crypto.HexToECDSA(strings.Repeat("17", 32))
		if err != nil {
			t.Fatal(err)
		}
		unsigned, err := f.config.Plan.Actions[0].unsigned()
		if err != nil {
			t.Fatal(err)
		}
		f.tx, err = types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(964)), private)
		if err != nil {
			t.Fatal(err)
		}
		f.raw, err = f.tx.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.signedPath, f.raw, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(f.raw)
		f.signedHash = "sha256:" + hex.EncodeToString(digest[:])
	}
	f.prepareSigned()
	if result, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 || result.Attempts != 1 {
		t.Fatalf("original CREATE failed to submit: %+v %d %s", result, code, diagnostic)
	}
	completed, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || completed.Status != status || completed.Receipt == nil || completed.ReceiptObservation != "revalidated-online" {
		t.Fatalf("canonical terminal observation did not complete: %+v %d %s", completed, code, diagnostic)
	}
	if reverted && (completed.Receipt.Status != 0 || completed.Receipt.RuntimeHash != "" || completed.Receipt.GetterHash != "") {
		t.Fatalf("failed constructor claimed installed runtime: %+v", completed.Receipt)
	}
	path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f.stateLock.Lock()
	counts := maps.Clone(f.counts)
	writes := len(f.writes)
	f.stateLock.Unlock()
	if writes != 1 || completed.Attempts != 1 {
		t.Fatalf("terminal fixture did not retain exactly one original attempt: %d %+v", writes, completed)
	}
	retained, code, diagnostic := f.command("resume")
	if code != 0 || retained.Status != status || retained.Receipt == nil || *retained.Receipt != *completed.Receipt || retained.TransactionHash != completed.TransactionHash || retained.Attempts != completed.Attempts {
		t.Fatalf("offline reopen downgraded or replaced terminal custody: %+v %d %s", retained, code, diagnostic)
	}
	if retained.ReceiptObservation != "retained" || retained.InstallationComplete || retained.ActivationReady {
		t.Fatalf("offline completion claimed a fresh audit or full installation: %+v", retained)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("offline terminal projection rewrote custody: %v", err)
	}
	f.stateLock.Lock()
	unchanged := maps.Equal(counts, f.counts) && writes == len(f.writes)
	f.stateLock.Unlock()
	if !unchanged {
		t.Fatal("offline terminal projection reached an RPC read or write")
	}
}
