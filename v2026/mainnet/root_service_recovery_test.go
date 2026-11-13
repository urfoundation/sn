// Recovery owns previously issued public bytes. Explicit durable boundaries
// and barriers prove that no fresh decision or effect is needed to retain them.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
)

func TestRootServiceIssuedRecoveryPreservesInterruptedDecision(t *testing.T) {
	f := newRootServiceFixture(t)
	f.observer.view.Enabled = false
	owner, store := f.open(t, true, f.ports())
	if event, err := owner.step(t.Context()); err != nil || event.Status != "setter-disabled" {
		t.Fatal("fixture did not retain a hold decision", event, err)
	}
	f.observer.err = errors.New("synthetic interrupted next observation")
	if _, err := owner.step(t.Context()); err == nil {
		t.Fatal("fixture did not interrupt observation")
	}
	signature, err := f.signer.signOnce(t.Context(), f.config.Packet.Action)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.retainIssuedSignature(t.Context(), signature, 1); err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil || record.Observations != 2 || !record.ObservationPending || record.Decision.Outcome != "setter-disabled" || record.Decision.Attempt != 1 ||
		!record.RecoveredSignature || record.Action.Phase != "pending" || record.Action.Broadcasts != 1 || record.Action.Signature != hex.EncodeToString(signature) {
		t.Fatal("recovery rewrote decision/allowance history", record, err)
	}
	if event, err := owner.step(t.Context()); err == nil || event.Status != "blocked" || f.observer.calls != 2 || len(f.submitter.intents) != 0 || f.signer.signs != 1 {
		t.Fatal("recovered liability used supplied mutation ports", event, err)
	}
	raw, err := f.config.Packet.Action.signed(signature)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&rootServiceChain{owner: owner}).submit(t.Context(), raw); err == nil || len(f.submitter.intents) != 0 {
		t.Fatal("direct service adapter bypassed receipt-only recovery")
	}
	f.finalize(t, store, 40)
	if event, err := owner.step(t.Context()); err != nil || event.Status != "complete" {
		t.Fatal("recovery could not retain canonical outcome", event, err)
	}
	store.close()
	_, store = f.open(t, false, rootServicePorts{})
	record, err = store.load()
	if err != nil || record.Phase != "complete" || record.Observations != 2 || !record.ObservationPending || record.Decision.Outcome != "setter-disabled" || record.Action.Broadcasts != 1 {
		t.Fatal("restart erased interrupted decision or liability", record, err)
	}
}

func TestRootServiceIssuedRecoverySurvivesAmbiguousWrites(t *testing.T) {
	for _, commit := range []bool{false, true} {
		f := newRootServiceFixture(t)
		_, store := f.open(t, true, f.ports())
		failure := &rootServiceStoreFailure{store: store, failAt: 1, commit: commit}
		owner, err := newRootServiceOwner(f.config, failure, f.ports())
		if err != nil {
			t.Fatal(err)
		}
		signature, err := f.signer.signOnce(t.Context(), f.config.Packet.Action)
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.retainIssuedSignature(t.Context(), signature, 0); err == nil || !owner.poisoned {
			t.Fatal("ambiguous signature retention was not poisoned", err)
		}
		if err := owner.retainIssuedSignature(t.Context(), signature, 0); err == nil || failure.saves != 1 {
			t.Fatal("poisoned recovery retried its write", err)
		}
		store.close()
		owner, store = f.open(t, false, f.ports())
		if err := owner.retainIssuedSignature(t.Context(), signature, 0); err != nil {
			t.Fatal("reopen could not recover original signature", commit, err)
		}
		record, err := store.load()
		if err != nil || record.Observations != 0 || record.Decision != nil || record.Action.Signature != hex.EncodeToString(signature) || record.Action.Broadcasts != 0 || !record.RecoveredSignature {
			t.Fatal("ambiguous recovery changed original liability", commit, record, err)
		}
	}
}

func TestRootServiceIssuedRecoveryRejectsReplacementAndInvalidState(t *testing.T) {
	f := newRootServiceFixture(t)
	owner, store := f.open(t, true, f.ports())
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	record.RecoveredSignature = true
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(f.config); err == nil {
		t.Fatal("unsigned state claimed issued-signature recovery")
	}
	signature, err := f.signer.signOnce(t.Context(), f.config.Packet.Action)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.retainIssuedSignature(t.Context(), signature, f.config.Packet.Action.Scope.MaxBroadcasts+1); err == nil {
		t.Fatal("recovery admitted an excessive broadcast floor")
	}
	if err := owner.retainIssuedSignature(t.Context(), signature, 1); err != nil {
		t.Fatal(err)
	}
	otherSigner := &rootSignerFixture{pair: f.signer.pair}
	other, err := otherSigner.signOnce(t.Context(), f.config.Packet.Action)
	if err != nil || hex.EncodeToString(other) == hex.EncodeToString(signature) {
		t.Fatal("fixture did not produce a distinct valid native signature", err)
	}
	if err := owner.retainIssuedSignature(t.Context(), other, 1); err == nil {
		t.Fatal("recovery replaced original signature bytes")
	}
	if err := owner.retainIssuedSignature(t.Context(), signature, 0); err != nil {
		t.Fatal(err)
	}
	record, err = store.load()
	if err != nil || record.Action.Broadcasts != 1 || record.Action.Signature != hex.EncodeToString(signature) {
		t.Fatal("idempotent recovery lowered original broadcast floor", record, err)
	}
}

func TestRootServiceIssuedRecoveryCanceledWaiterCannotEnter(t *testing.T) {
	f := newRootServiceFixture(t)
	f.observer.entered, f.observer.release = make(chan struct{}), make(chan struct{})
	owner, store := f.open(t, true, f.ports())
	done := make(chan error, 1)
	go func() {
		_, err := owner.step(t.Context())
		done <- err
	}()
	<-f.observer.entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := owner.retainIssuedSignature(ctx, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled recovery waiter entered another owner's operation", err)
	}
	close(f.observer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil || record.Action.Phase != "reserved" || record.RecoveredSignature || record.Observations != 1 || f.signer.signs != 0 {
		t.Fatal("canceled recovery changed owner state", record, err)
	}
}
