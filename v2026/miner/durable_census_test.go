//go:build linux

// Losing every member under the same approved root is not a fresh launch.
package miner

import (
	"os"
	"path/filepath"
	"testing"
)

// An intact volume/root declaration cannot replace a lost signed inventory.
func TestFleetRecoveryDurableCensusRefusesLostJournalAndMarker(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	record, signer := fleetRecoveryTestPrepared(t, fixture)
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.put(record, signer); err != nil {
		store.close()
		t.Fatal(err)
	}
	directory := store.directory.Name()
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"journal.json", "initialized"} {
		if err := os.Rename(filepath.Join(directory, name), filepath.Join(filepath.Dir(directory), "retained-"+name)); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := openFleetRecoveryStore(fixture.durable.Context)
	if reopened != nil {
		reopened.close()
	}
	if err == nil {
		t.Fatal("lost signed fleet inventory and marker were reinitialized")
	}
}

// Signed raw transaction, nonce floor and completed outcomes cannot disappear
// merely because a restarted queue owner sees an absent JSON file.
func TestClaimQueueDurableCensusRefusesLostCompletedQueue(t *testing.T) {
	cfg, _, entry, _, _ := signedClaimFixture(t, 70, 23)
	store := newClaimQueueTestStore(t, cfg.StateDir)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	path := store.path
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	reopened, err := newClaimQueueStore(cfg.StateDir, store.ctx)
	if err == nil {
		defer reopened.close()
		_, err = reopened.load()
	}
	if err == nil {
		t.Fatal("lost signed claim queue was reinitialized")
	}
}
