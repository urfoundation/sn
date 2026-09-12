package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These exact public request/result/budget bytes are the retained CLI47 wire
// that strict setup refused. No original runtime path, key or RPC is required.
func TestCoordinatorRepairCarryRetainedBudgetWireKeepsSignedAuthority(t *testing.T) {
	t.Parallel()
	read := func(name, hash string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("testdata", "coordinator-repair-budget-v1", name))
		if err != nil || bytesSHA256(raw) != "sha256:"+hash {
			t.Fatalf("retained %s bytes changed: %v", name, err)
		}
		return raw
	}
	requestRaw := read("request.json", "8fd288dfb29ac8ac8c5b93ec1d5c05a94418bf6ac90ae963fca7c8939d388ab9")
	resultRaw := read("result.json", "7f9ea3efc5c51c4f5d765fe7008d534339c79f9a45496b81b1468ac9ca0dcbb5")
	raw := read("budget.json", "b9416587bef1ebb051b49d44c28c8bd0e4837580b77acb8ff6e80fa4d696c16a")
	before := bytes.Clone(raw)
	var carry CoordinatorRepairCarry
	if err := decodeExactCoordinatorRepairJSON(requestRaw, &carry.Request); err != nil {
		t.Fatal(err)
	}
	if err := decodeExactCoordinatorRepairJSON(resultRaw, &carry.Result); err != nil {
		t.Fatal(err)
	}
	request := carry.Request.Request
	if err := verifyCoordinatorRepairSignature(request, carry.Request.Hash, carry.Request.Signature, request.Owner); err != nil {
		t.Fatalf("original signed projection changed: %v", err)
	}
	if err := validateCoordinatorRepairCarryResult(&carry); err != nil {
		t.Fatalf("original signed finalization changed: %v", err)
	}
	if err := validateCoordinatorRepairBudgetDocument(raw, request); err != nil {
		t.Fatalf("retained full budget document was refused: %v", err)
	}
	var document coordinatorRepairBudgetDocument
	if err := decodeExactCoordinatorRepairJSON(raw, &document); err != nil || document.ObservedAt != "2026-09-12T02:17:25.751430+00:00" || document.JournalSequence != 10250 || len(document.UnsignedPreparedIntents) != 4 || document.ActualPaidFeeTotalWei != nil || document.NewFundingWei != "0" {
		t.Fatalf("original audit context was not decoded exactly: %v", err)
	}
	// The projection-only document used by other producers remains supported
	// under its own distinct byte hash, without changing the signed wire type.
	compact, err := json.Marshal(request.Budget)
	if err != nil || bytes.Contains(compact, []byte("observed_at")) {
		t.Fatalf("audit metadata widened the executable projection: %v", err)
	}
	projectionRequest := request
	projectionRequest.BudgetSHA256 = strings.TrimPrefix(bytesSHA256(compact), "sha256:")
	if err := validateCoordinatorRepairBudgetDocument(compact, projectionRequest); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"changed-observation", "changed-projection", "unknown-field", "unknown-intent-field", "wrong-field-type"} {
		t.Run(fault, func(t *testing.T) {
			changed := bytes.Clone(raw)
			want := request
			switch fault {
			case "changed-observation":
				changed = bytes.Replace(changed, []byte("02:17:25.751430"), []byte("02:17:26.751430"), 1)
			case "changed-projection":
				changed = bytes.Replace(changed, []byte(`"available_wei": "11679328930122236336"`), []byte(`"available_wei": "0"`), 1)
			case "unknown-field":
				changed = bytes.Replace(changed, []byte(`"observed_at":`), []byte(`"allow_unbounded": true, "observed_at":`), 1)
			case "unknown-intent-field":
				changed = bytes.Replace(changed, []byte(`"attempt_count": 0`), []byte(`"attempt_count": 0, "ignore_liability": true`), 1)
			case "wrong-field-type":
				changed = bytes.Replace(changed, []byte(`"nonce": 6`), []byte(`"nonce": "6"`), 1)
			}
			if bytes.Equal(changed, raw) {
				t.Fatal("counterexample did not change actual wire bytes")
			}
			if fault != "changed-observation" {
				// Reach the typed/projection guard independently of the raw
				// hash check; a real request would also need its owner signature.
				want.BudgetSHA256 = strings.TrimPrefix(bytesSHA256(changed), "sha256:")
			}
			if err := validateCoordinatorRepairBudgetDocument(changed, want); err == nil {
				t.Fatal("changed or unknown budget authority was accepted")
			}
		})
	}
	if !bytes.Equal(before, raw) {
		t.Fatal("admission rewrote original budget bytes")
	}
}
