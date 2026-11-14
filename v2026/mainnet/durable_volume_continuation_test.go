// Real service continuations distinguish refused admission from a lost journal
// acknowledgement. Synthetic ports never contact a node or an owner device.
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The approved directory survives, but both application files are retained
// elsewhere. An explicit create must not reset the consumed observation budget.
func TestRootServiceDurableLostMembersCannotRecreate(t *testing.T) {
	fixture := newRootServiceFixture(t)
	owner, store := fixture.open(t, true, fixture.ports())
	if event, err := owner.step(t.Context()); err != nil || event.Status != "intent" {
		t.Fatal("fixture did not retain its original decision", event, err)
	}
	path := fixture.config.Packet.Action.Scope.StatePath
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	retained := ownerSigningTestDirectory(t)
	for _, name := range []string{path, path + ".lock"} {
		if err := os.Rename(name, filepath.Join(retained, filepath.Base(name))); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := openRootServiceStore(fixture.config, true, fixture.storage.Context)
	if reopened != nil {
		record, loadErr := reopened.load()
		_ = reopened.close()
		t.Fatalf("lost original members replenished lifetime allowance: observations=%d load=%v open=%v", record.Observations, loadErr, err)
	}
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("lost original marker and journal were not refused as custody loss", err)
	}
	for _, name := range []string{path, path + ".lock"} {
		if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refused create reconstructed lost custody", name, err)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(retained, filepath.Base(path))); err != nil || !bytes.Equal(raw, original) {
		t.Fatal("causal refusal changed retained original decision", err)
	}
}

// A capacity refusal consumes no observation and can resume on the same owner.
func TestRootServiceDurableReserveContinuesSameOwner(t *testing.T) {
	fixture := newRootServiceFixture(t)
	owner, store := fixture.open(t, true, fixture.ports())
	before, err := os.ReadFile(fixture.config.Packet.Action.Scope.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.storage.Host.SetReserve(0, 0)
	event, err := owner.step(t.Context())
	if !errors.Is(err, durablevolume.ErrUnavailable) || owner.poisoned || event.terminalFailure || event.Status != "storage-pending" || fixture.observer.calls != 0 || fixture.signer.signs != 0 || len(fixture.submitter.intents) != 0 {
		t.Fatalf("reserve refusal became terminal or called a port: %+v %v poisoned=%v", event, err, owner.poisoned)
	}
	after, readErr := os.ReadFile(fixture.config.Packet.Action.Scope.StatePath)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatal("admission refusal mutated retained journal", readErr)
	}
	fixture.storage.Host.SetReserve(1024*1024*1024, 1024*1024)
	event, err = owner.step(t.Context())
	record, loadErr := store.load()
	if err != nil || loadErr != nil || event.Status != "intent" || record.Observations != 1 || record.ObservationPending || fixture.observer.calls != 1 || fixture.signer.signs != 0 || len(fixture.submitter.intents) != 0 {
		t.Fatalf("same owner did not resume the original allowance: %+v %v %v", event, err, loadErr)
	}
}

// The public bounded runner stops only its uncertain owner. A sync error after
// rename preserves the new intent and error class; reopening reconciles it.
func TestRootServiceDurableUncertainRunRetainsOriginalOwner(t *testing.T) {
	fixture := newRootServiceFixture(t)
	owner, store := fixture.open(t, true, fixture.ports())
	other := newRootServiceFixture(t)
	otherOwner, otherStore := other.open(t, true, other.ports())
	lostSync := errors.New("synthetic lost parent sync acknowledgement")
	writes := 0
	store.syncDirectoryForTest = func(*os.File) error {
		writes++
		return errors.Join(lostSync, &durablevolume.UnavailableError{Reason: "synthetic post-rename media pressure"})
	}
	output, err := newRootServiceOutput(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = owner.Run(t.Context(), 3, time.Second, output)
	if !errors.Is(err, lostSync) || !errors.Is(err, errMainnetDurablePublicationUncertain) || errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, errRpcIntegrity) || mainnetDurableAdmissionPending(err) || !owner.poisoned || writes != 1 || fixture.observer.calls != 0 || fixture.signer.signs != 0 || len(fixture.submitter.intents) != 0 {
		t.Fatalf("uncertain runner misclassified, retried or leaked an effect: %v writes=%d poisoned=%v", err, writes, owner.poisoned)
	}
	retained, err := os.ReadFile(fixture.config.Packet.Action.Scope.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	var pending rootServiceRecord
	if err := decodePlanJson(retained, &pending); err != nil || pending.validate(fixture.config) != nil || pending.Observations != 1 || !pending.ObservationPending || pending.Action.Action.RequestHash != fixture.config.Packet.Action.RequestHash {
		t.Fatal("post-rename original intent is not retained", err)
	}
	if _, err := owner.step(t.Context()); !errors.Is(err, errMainnetDurablePublicationUncertain) || writes != 1 {
		t.Fatal("same owner lost uncertainty or retried publication", err)
	}
	otherOutput, err := newRootServiceOutput(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := otherOwner.Run(t.Context(), 1, time.Second, otherOutput); err != nil {
		t.Fatal("unrelated service stopped", err)
	}
	completed, err := otherStore.load()
	if err != nil || completed.Phase != "active" || completed.Observations != 1 || other.observer.calls != 1 {
		t.Fatal("unrelated durable decision was reset", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	owner, store = fixture.open(t, false, fixture.ports())
	reopened, err := os.ReadFile(fixture.config.Packet.Action.Scope.StatePath)
	if err != nil || !bytes.Equal(retained, reopened) {
		t.Fatal("reopen rewrote the original journal", err)
	}
	if event, err := owner.step(t.Context()); err != nil || event.Status != "intent" {
		t.Fatal("original pending observation did not continue", event, err)
	}
	record, err := store.load()
	if err != nil || record.Observations != 2 || record.Action.Action.RequestHash != pending.Action.Action.RequestHash || record.Action.Action.Nonce != pending.Action.Action.Nonce || record.Action.Phase != "reserved" || fixture.observer.calls != 1 || fixture.signer.signs != 0 || len(fixture.submitter.intents) != 0 {
		t.Fatalf("recovery changed intent or replenished allowance: %+v %v", record, err)
	}
}
