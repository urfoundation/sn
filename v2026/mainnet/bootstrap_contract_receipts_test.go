// Local fixtures execute the pinned deployment bytecode and authenticate its
// real native/EVM inclusions. No production identity or route is contacted.
package main

import (
	"bytes"
	"context"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The new command borrows only exact original custody and an explicit read port.
func bootstrapContractReceiptTestArgs(f *bootstrapChainFixture) []string {
	return []string{"bootstrap-chain", "contract-installation-receipts", "--config", f.path,
		"--run-dir", f.config.RunDirectory, "--accept-plan-hash", f.preparation.Plan.ContentHash, "--online"}
}

// One real graph covers public admission, renewed per-receipt budgets, advancing
// finality, exact missing/swapped receipt rejection and unchanged local custody.
func TestBootstrapContractReceiptAdmissionCommandRetainsHistory(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	bootstrapSuccessorCommandTestComplete(t, f)
	before := bootstrapContractTestJournals(t, f.config.RunDirectory)
	maps.Copy(before, f.journals(t))
	scope, err := openBootstrapContractReceiptScope(f.storageContext(t.Context()), f.path, f.config.RunDirectory, f.preparation.Plan.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.close() })
	transport := &bootstrapSuccessorCanonicalDeadlineTransport{base: scope.chain.client.httpClient.Transport}
	scope.chain.client.httpClient.Transport = transport
	chain := f.contracts
	baseOverride := chain.override
	setFault := func(fault func(string, []any, any) any) {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		chain.override = func(method string, params []any, result any) any {
			result = baseOverride(method, params, result)
			if fault != nil {
				return fault(method, params, result)
			}
			return result
		}
	}
	setFault(func(method string, _ []any, result any) any {
		if method == "chain_getFinalizedHead" {
			chain.advanceEmpty()
			return chain.hashes[chain.head]
		}
		return result
	})
	observed, err := scope.inspect(t.Context())
	if err != nil || !observed.CanonicalReceiptsVerified || len(observed.Actions) != 8 || observed.CheckedThroughNativeBlock <= observed.CanonicalSnapshotNativeBlock {
		t.Fatal("historical receipt admission blocked ordinary head advancement", observed, err)
	}
	func() {
		transport.stateLock.Lock()
		defer transport.stateLock.Unlock()
		if len(transport.deadlines) != 8 {
			t.Fatal("historical receipt deadline census differs", len(transport.deadlines))
		}
		for i, deadline := range transport.deadlines {
			if deadline.IsZero() || i > 0 && !deadline.After(transport.deadlines[i-1]) {
				t.Fatal("historical receipt admission reused an aggregate budget", i)
			}
		}
	}()
	journal := filepath.Join(f.config.RunDirectory, bootstrapContractStateFile(0))
	original := []byte(before[journal])
	if err := os.WriteFile(journal, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err = scope.checkpoint(t.Context())
	if restoreErr := os.WriteFile(journal, original, 0600); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err == nil || !strings.Contains(err.Error(), "original custody changed") {
		t.Fatal("historical receipt admission ignored replaced local custody", err)
	}
	if err := scope.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.inspect(t.Context()); err == nil {
		t.Fatal("closed receipt scope retained authority")
	}
	var stdout, stderr bytes.Buffer
	args := bootstrapContractReceiptTestArgs(f)
	if code := runMain(f.storageContext(t.Context()), args, &stdout, &stderr); code != 0 {
		t.Fatal("public historical receipt admission refused", code, stderr.String())
	}
	var result bootstrapContractReceiptAdmission
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	seal := result.ContentHash
	result.ContentHash = ""
	if seal != rootObjectHash(result) || result.Schema != bootstrapContractReceiptSchema || result.PreparationHash != f.preparation.Plan.ContentHash ||
		result.DeclarationHash != scope.declaration.ContentHash || result.ContractPlanHash != f.contracts.config.Plan.hash() ||
		result.OriginalConfigHash != rootObjectHash(f.contracts.config) || !result.CanonicalReceiptsVerified || !result.HistoricalStateVerified ||
		!result.DeploymentScanFloorsVerified || result.FinalityAssumption != "owned-rpc-assertion" || result.RetainedAttempts != 8 || result.OriginalMaximumAttempts != 8 ||
		len(result.Actions) != 8 || !reflect.DeepEqual(result.Validators, scope.declaration.Validators) ||
		result.CurrentStateVerified || result.EvidenceAnchorVerified || result.InstallationComplete || result.ActivationReady || result.NetworkEffects ||
		!reflect.DeepEqual(result.PendingChainPhases, bootstrapChainPendingPhases()) || result.CheckedThroughNativeBlock <= result.CanonicalSnapshotNativeBlock {
		t.Fatal("historical receipt report lost custody or granted current authority", result)
	}
	for i, action := range result.Actions {
		record := scope.records[i]
		if action.Id != f.contracts.config.Plan.Actions[i].Id || action.CustodyHash != rootObjectHash(record) || action.JournalHash != record.ContentHash ||
			action.TransactionHash != record.TransactionHash || action.Attempts != record.Attempts || action.Receipt != *record.Receipt ||
			result.EarliestOriginalEvmBlock > action.Receipt.BlockNumber {
			t.Fatal("historical receipt report changed original inclusion", i)
		}
	}
	for _, item := range []struct {
		name string
		swap bool
	}{{name: "missing original receipt"}, {name: "swapped original receipt", swap: true}} {
		setFault(func(method string, params []any, result any) any {
			if method == "eth_getTransactionReceipt" && params[0] == scope.records[0].TransactionHash {
				if item.swap {
					return chain.history.receipts[scope.records[1].TransactionHash]
				}
				return nil
			}
			return result
		})
		stdout.Reset()
		stderr.Reset()
		if code := runMain(f.storageContext(t.Context()), args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "canonical inclusion or historical postcondition differs") {
			t.Fatal("historical receipt admission accepted "+item.name, code, stderr.String())
		}
	}
	setFault(nil)
	after := bootstrapContractTestJournals(t, f.config.RunDirectory)
	maps.Copy(after, f.journals(t))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("historical receipt admission rewrote original custody")
	}
	chain.stateLock.Lock()
	defer chain.stateLock.Unlock()
	if len(chain.writes) != 8 {
		t.Fatal("historical receipt admission submitted a transaction", len(chain.writes))
	}
}

// Inclusive EVM indexing cannot use the numerically unrelated native inclusion.
func TestBootstrapContractReceiptScanFloorUsesEvmInclusion(t *testing.T) {
	records := make([]evmActionRecord, 8)
	for i := range records {
		records[i].Receipt = &evmCreateReceipt{Status: 1, BlockNumber: 38 + uint64(i), NativeNumber: 101 + uint64(i)}
	}
	roles := []bootstrapContractValidatorBinding{{DeclaredDeployBlock: 10}, {DeclaredDeployBlock: 38}}
	if earliest, err := bootstrapContractReceiptScanFloor(roles, records); err != nil || earliest != 38 {
		t.Fatal("inclusive original deployment floor refused", earliest, err)
	}
	roles[1].DeclaredDeployBlock = 39
	if _, err := bootstrapContractReceiptScanFloor(roles, records); err == nil {
		t.Fatal("scan floor above earliest EVM inclusion was admitted")
	}
	roles[1].DeclaredDeployBlock = 0
	if _, err := bootstrapContractReceiptScanFloor(roles, records); err == nil {
		t.Fatal("absent signed scan floor was admitted")
	}
	roles[1].DeclaredDeployBlock = 38
	records[7].Receipt = nil
	if _, err := bootstrapContractReceiptScanFloor(roles, records); err == nil {
		t.Fatal("scan floor was certified without all original inclusions")
	}
}

// The final continuity check tolerates advancement but rechecks each original
// inclusion even when the snapshot and latest header hashes stay unchanged.
func TestBootstrapContractReceiptFinalCanonicalContinuity(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	chain, err := newEvmOwnedChain(f.contracts.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { chain.client.httpClient.CloseIdleConnections() })
	var inclusion evmCreateReceipt
	var snapshot evmActionRecord
	func() {
		f.contracts.stateLock.Lock()
		defer f.contracts.stateLock.Unlock()
		f.contracts.advanceEmpty()
		inclusion = evmCreateReceipt{NativeNumber: f.contracts.head, NativeHash: f.contracts.hashes[f.contracts.head]}
		f.contracts.advanceEmpty()
		snapshot = evmActionRecord{ScanNumber: f.contracts.head, ScanHash: f.contracts.hashes[f.contracts.head]}
		f.contracts.advanceEmpty()
	}()
	head, err := chain.client.readIdentity(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	scope := &bootstrapContractReceiptScope{chain: chain, preparation: f.preparation}
	for range 8 {
		scope.records = append(scope.records, evmActionRecord{Receipt: &inclusion})
	}
	if err := scope.continuity(t.Context(), snapshot, head); err != nil || head.FinalizedNumber <= snapshot.ScanNumber {
		t.Fatal("final canonical receipt check refused head advancement", err)
	}
	func() {
		f.contracts.stateLock.Lock()
		defer f.contracts.stateLock.Unlock()
		f.contracts.hashes[inclusion.NativeNumber] = "0x" + strings.Repeat("ef", 32)
	}()
	if err := scope.continuity(t.Context(), snapshot, head); err == nil {
		t.Fatal("final canonical receipt check ignored changed original inclusion")
	}
	func() {
		f.contracts.stateLock.Lock()
		defer f.contracts.stateLock.Unlock()
		f.contracts.hashes[inclusion.NativeNumber] = inclusion.NativeHash
		f.contracts.hashes[snapshot.ScanNumber] = "0x" + strings.Repeat("ed", 32)
	}()
	if err := scope.continuity(t.Context(), snapshot, head); err == nil {
		t.Fatal("final canonical receipt check ignored changed snapshot")
	}
}

// A valid declaration and offline preparation cannot fabricate the missing
// original successful receipts or trigger chain reads to fill absent custody.
func TestBootstrapContractReceiptRejectsUnretainedCustody(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	f.result(t, "apply")
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), bootstrapContractReceiptTestArgs(f), &stdout, &stderr); code != 2 || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "original complete record differs") {
		t.Fatal("unretained contract receipts acquired canonical authority", code, stderr.String())
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !maps.Equal(reads, f.contracts.counts) {
		t.Fatal("missing historical custody caused mutation or chain reads")
	}
}

// Alternate acceptance, invented write options and cancellation all fail before
// any journal is claimed, signature imported or original route contacted.
func TestBootstrapContractReceiptRejectsChangedAcceptance(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	before, reads := f.journals(t), maps.Clone(f.contracts.counts)
	args := bootstrapContractReceiptTestArgs(f)
	for _, suffix := range [][]string{{"--submit"}, {"--signed-transaction", "synthetic-absent"}, {"--accept-plan-hash", "sha256:" + strings.Repeat("f", 64)}, {"--run-dir", t.TempDir()}, {"--online=false"}} {
		var stdout, stderr bytes.Buffer
		if code := runMain(f.storageContext(t.Context()), append(append([]string{}, args...), suffix...), &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatal("historical receipt command accepted changed authority", suffix, code, stderr.String())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := runMain(f.storageContext(ctx), args, io.Discard, io.Discard); code != 2 {
		t.Fatal("canceled receipt admission reported success", code)
	}
	if !reflect.DeepEqual(before, f.journals(t)) || !maps.Equal(reads, f.contracts.counts) {
		t.Fatal("historical receipt preflight changed custody or contacted the chain")
	}
}
