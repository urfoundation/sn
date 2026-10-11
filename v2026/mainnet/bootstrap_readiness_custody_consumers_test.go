// These consumers join original preparation to actual contract custody. Local
// EVM execution and host-free transport seams expose exact decision boundaries.
package main

import (
	"bytes"
	"errors"
	"maps"
	"testing"
)

// An original root marker is independent of all eight EVM action locks. Receipt
// readback must retain both cohorts through its last historical receipt read.
func TestBootstrapReceiptsRejectLostPreparationMarker(t *testing.T) {
	f, _ := newBootstrapContractRoleFixture(t)
	bootstrapSuccessorCommandTestComplete(t, f)
	scope, err := openBootstrapContractReceiptScope(f.storageContext(t.Context()), f.path, f.config.RunDirectory, f.preparation.Plan.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.close()
	chain := f.contracts
	chain.stateLock.Lock()
	prior := chain.override
	var restore func()
	chain.override = func(method string, params []any, value any) any {
		value = prior(method, params, value)
		if method == "eth_getTransactionReceipt" && params[0] == scope.records[7].TransactionHash && restore == nil {
			restore = bootstrapReadinessTestReplace(t, f.preparation.childPaths()[4]+".lock")
		}
		return value
	}
	chain.stateLock.Unlock()
	result, err := scope.inspect(t.Context())
	chain.stateLock.Lock()
	defer chain.stateLock.Unlock()
	if restore != nil {
		defer restore()
	}
	if restore == nil || !errors.Is(err, errRpcIntegrity) || result.CanonicalReceiptsVerified || len(chain.writes) != 8 {
		t.Fatalf("lost preparation marker became canonical receipt readiness: replaced=%v complete=%v writes=%d err=%v", restore != nil, result.CanonicalReceiptsVerified, len(chain.writes), err)
	}
}

// Rechecking the original cohort after native proofs must precede publication
// of the final installation seal, even with a genuinely installed anchor.
func TestBootstrapInstallationRejectsLostPreparationMarker(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	chain := f.original.contracts
	seal := bootstrapContractInstallationTestProofs(t, f)
	seal(true)
	var stdout bytes.Buffer
	if code, diagnostic := f.online(&stdout, true); code != 0 {
		t.Fatal("healthy synthetic installation", code, diagnostic)
	}
	seal(false)
	owner, adapter, close := f.openRuntimeRevisions()
	defer close()
	if result, err := inspectBootstrapContractInstallation(t.Context(), owner, adapter); err != nil || !result.InstallationComplete {
		t.Fatal("healthy readback control", err)
	}
	seal(true)
	chain.stateLock.Lock()
	prior := chain.override
	proofs := 0
	var restore func()
	chain.override = func(method string, params []any, value any) any {
		value = prior(method, params, value)
		if method == "state_getReadProof" {
			proofs++
			if proofs == 3 {
				restore = bootstrapReadinessTestReplace(t, f.original.preparation.childPaths()[2]+".lock")
			}
		}
		return value
	}
	chain.stateLock.Unlock()
	result, err := inspectBootstrapContractInstallation(t.Context(), owner, adapter)
	chain.stateLock.Lock()
	defer chain.stateLock.Unlock()
	if restore != nil {
		defer restore()
	}
	if restore == nil || !errors.Is(err, errRpcIntegrity) || result.InstallationComplete || len(chain.writes) != 9 {
		t.Fatalf("lost preparation marker became installation: replaced=%v complete=%v writes=%d err=%v", restore != nil, result.InstallationComplete, len(chain.writes), err)
	}
}

// The second admission follows synced attempt reservation. Losing preparation
// must preserve the signed transaction and counted attempt without sending it.
func TestBootstrapSuccessorRejectsLostPreparationBeforeSend(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	owner, adapter, close := f.openRuntimeRevisions()
	defer close()
	original := f.original.journals(t)
	admissions := 0
	var restore func()
	f.afterProvenance = func() {
		admissions++
		if admissions == 2 {
			restore = bootstrapReadinessTestReplace(t, f.original.preparation.childPaths()[0]+".lock")
		}
	}
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, adapter, true)
	if restore != nil {
		restore()
	}
	if restore == nil || !errors.Is(err, errRpcIntegrity) || result.SubmissionAttempted || result.InstallationComplete || owner.last.CumulativeAttempts != 9 ||
		len(f.original.contracts.writes) != 8 || owner.planCopy().SignedRelayer != f.approval.Plan.SignedRelayer || !maps.Equal(original, f.original.journals(t)) {
		t.Fatalf("lost preparation custody spent or erased a counted intent: changed=%v attempts=%d writes=%d result=%+v err=%v", restore != nil, owner.last.CumulativeAttempts, len(f.original.contracts.writes), result, err)
	}
}
