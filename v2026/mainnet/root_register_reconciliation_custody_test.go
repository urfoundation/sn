// Original inclusion custody survives missing readback without accepting a
// replacement receipt or recreating a physically lost registration journal.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A later individually valid receipt may describe a different inclusion. Reopen
// retains the first receipt and rejects that replacement before any publication.
func TestRootRegisterReadbackRetryCannotReplaceOriginalReceipt(t *testing.T) {
	for _, mutation := range []string{"inclusion-hash", "body-count", "fee"} {
		f := newRootRegisterTestFixture(t, true)
		evidence, signed := rootRegisterTestReconciliation(t, f, true)
		custody, store, request := rootRegisterSignedTestCustody(t, f)
		if err := evidence.validate(request, signed); err != nil {
			t.Fatal(err)
		}
		gap := ownerTrimTestCopy(t, evidence)
		gap.Readback, gap.ReadbackIssue = nil, "synthetic inclusion-block storage outage"
		chain := rootRegisterReconcileBoundary(func(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error) {
			return gap, nil
		})
		if result, err := custody.reconcile(t.Context(), chain); err != nil || result.Phase != "finalized-readback-pending" || !result.TransactionFinalized || result.RootSeatObserved || result.ActivationReady {
			t.Fatal("original receipt did not retain its explicit readback gap", result, err)
		}
		if err := store.close(); err != nil {
			t.Fatal(err)
		}
		resumed, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		defer resumed.close()
		recovered := rootRegisterCustody{config: f.config, key: f.key, store: resumed}
		changed := ownerTrimTestCopy(t, evidence)
		switch mutation {
		case "inclusion-hash":
			changed.Receipt.BlockHash = "0x" + strings.Repeat("af", 32)
			if changed.FinalizedNumber == changed.Receipt.BlockNumber {
				changed.FinalizedHash = changed.Receipt.BlockHash
			}
			changed.Readback.FinalizedHash = changed.Receipt.BlockHash
			changed.Readback.ContentHash = ""
			changed.Readback.ContentHash = rootObjectHash(*changed.Readback)
		case "body-count":
			changed.BodyCount++
		case "fee":
			operator, _ := hex.DecodeString(f.config.Action.Policy.Operator[2:])
			hotkey, _ := hex.DecodeString(f.config.Action.Policy.Hotkey[2:])
			events := rootCompact(3)
			events = append(events, rootReceiptEventFixture(t, f.metadata, "SubtensorModule.NeuronRegistered", 1, []byte{0, 0}, []byte{0, 0}, hotkey)...)
			changed.Receipt.ActualFeeRao++
			events = append(events, rootReceiptEventFixture(t, f.metadata, "TransactionPayment.TransactionFeePaid", 1, operator, binary.LittleEndian.AppendUint64(nil, changed.Receipt.ActualFeeRao), make([]byte, 8))...)
			events = append(events, rootReceiptEventFixture(t, f.metadata, "System.ExtrinsicSuccess", 1)...)
			changed.RawEvents, changed.Receipt.EventHash = "0x"+hex.EncodeToString(events), rootExtrinsicHash(events)
		}
		if err := changed.validate(request, signed); err != nil {
			t.Fatal("alternate inclusion should be internally valid before predecessor comparison", mutation, err)
		}
		chain = rootRegisterReconcileBoundary(func(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error) {
			return changed, nil
		})
		if _, err := recovered.reconcile(t.Context(), chain); err == nil {
			t.Fatal("readback repair replaced original inclusion evidence", mutation)
		}
		record, err := resumed.load()
		if err != nil || record.Phase != "finalized-readback-pending" || rootObjectHash(record.Reconciliation.Receipt) != rootObjectHash(evidence.Receipt) || record.Reconciliation.Readback != nil {
			t.Fatal("refused replacement changed the original receipt", mutation, err)
		}
	}
}

// Failure can precede account-association side effects. Its missing inclusion
// state remains recoverable after restart while the original fee/dispatch stay fixed.
func TestRootRegisterFailedDispatchRecoversInclusionReadbackAfterRestart(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	evidence, signed := rootRegisterTestReconciliation(t, f, false)
	custody, store, request := rootRegisterSignedTestCustody(t, f)
	if err := evidence.validate(request, signed); err != nil || evidence.Readback == nil {
		t.Fatal("failed dispatch fixture must retain authenticated inclusion state", err)
	}
	gap := ownerTrimTestCopy(t, evidence)
	gap.Readback, gap.ReadbackIssue = nil, "synthetic failure-state readback outage"
	chain := rootRegisterReconcileBoundary(func(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error) {
		return gap, nil
	})
	if result, err := custody.reconcile(t.Context(), chain); err != nil || result.Phase != "dispatch-failed" || !result.TransactionFinalized || result.RootSeatObserved || result.ActivationReady {
		t.Fatal("failed original receipt was lost or promoted to seat readiness", result, err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.close()
	recovered := rootRegisterCustody{config: f.config, key: f.key, store: resumed}
	chain = rootRegisterReconcileBoundary(func(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error) {
		return evidence, nil
	})
	if result, err := recovered.reconcile(t.Context(), chain); err != nil || result.Phase != "dispatch-failed" || !result.TransactionFinalized || result.RootSeatObserved || result.Seat != nil || result.ActivationReady || result.ReadbackIssue != "" {
		t.Fatal("failed receipt could not recover state without changing its outcome", result, err)
	}
	record, err := recovered.load()
	if err != nil || record.Reconciliation.Readback == nil || rootObjectHash(record.Reconciliation.Receipt) != rootObjectHash(evidence.Receipt) {
		t.Fatal("failure-state recovery erased the original financial evidence", err)
	}
}

// Read-only chain success cannot restore missing host custody, even when the
// original signature, event and inclusion state are otherwise fully available.
func TestRootRegisterReconciliationCannotRecreateLostJournal(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	evidence, signed := rootRegisterTestReconciliation(t, f, true)
	custody, _, request := rootRegisterSignedTestCustody(t, f)
	if err := evidence.validate(request, signed); err != nil {
		t.Fatal(err)
	}
	chain := rootRegisterReconcileBoundary(func(context.Context, rootRegisterSigningRequest, []byte) (rootRegisterReconciliation, error) {
		if err := os.Remove(f.config.Action.StatePath); err != nil {
			t.Fatal(err)
		}
		return evidence, nil
	})
	result, err := custody.reconcile(t.Context(), chain)
	if !errors.Is(err, durablevolume.ErrIdentity) || result.TransactionFinalized || result.RootSeatObserved || result.Seat != nil || result.ActivationReady {
		t.Fatal("readback recreated lost original custody or returned readiness", result, err)
	}
	if _, err := os.Lstat(f.config.Action.StatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reconciliation recreated a missing completed registration journal", err)
	}
}
