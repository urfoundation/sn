// Journal tests force filesystem commit and ownership boundaries with signed
// synthetic transactions. No scheduler timing is part of the proof.
package miner

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Constructs a genuine signed native liability with independently pinned
// synthetic authority; tests may then interrupt its local custody commit.
func fleetRecoveryTestPrepared(t *testing.T, fixture *fleetMainnetTestFixture) (*fleetRecoveryRecord, fleetRecoverySigner) {
	t.Helper()
	authority, err := loadFleetMainnetRuntimeAuthority(fixture.opts, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := fleetRecoveryNewIntent("publish", authority, fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	key, err := snchain.LoadKeypairFile(fleetOpt(fixture.opts, "--hotkey_seed_file"))
	if err != nil {
		t.Fatal(err)
	}
	chain, err := crv4.DialChainContext(t.Context(), fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFleetNative(chain)
	view, err := authority.viewAt(t.Context(), chain, fixture.head)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := fixture.manifest.CommitmentHash()
	call, err := view.NewSetFleetCommitmentCall(fixture.manifest.Netuid, hash)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := snchain.EncodeSignedCall(view, key.Ring, call, 0)
	if err != nil {
		t.Fatal(err)
	}
	record := fleetRecoveryPrepared(intent, authority, fixture.head, 100)
	record.NativeSigner, record.Raw, record.TxHash = key.PublicKey(), raw, snchain.ExtrinsicHash(raw).Hex()
	return record, fleetRecoverySigner{native: key}
}

// Reads the durable record after releasing the command's owner.
func fleetRecoveryTestRecord(t *testing.T, fixture *fleetMainnetTestFixture) *fleetRecoveryRecord {
	t.Helper()
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if len(store.records) != 1 {
		t.Fatalf("got %d records, want one", len(store.records))
	}
	return store.records[0]
}

// A second owner is refused while the first holds its actual directory lock.
func TestFleetRecoveryStoreExclusiveOwner(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	first, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := openFleetRecoveryStore(fixture.durable.Context); err == nil {
		second.close()
		first.close()
		t.Fatal("concurrent owner admitted")
	}
	if err := first.close(); err != nil {
		t.Fatal(err)
	}
	second, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	second.close()
}

// Each durable publication barrier survives reopening without replacing the
// transaction. A returned fsync error cannot authorize a network send.
func TestFleetRecoveryStoreInterruptedCommitRetainsExactSignedBytes(t *testing.T) {
	for _, barrier := range []string{"file-synced", "renamed", "directory-synced"} {
		fixture := newFleetMainnetTestFixture(t)
		record, signer := fleetRecoveryTestPrepared(t, fixture)
		store, err := openFleetRecoveryStore(fixture.durable.Context)
		if err != nil {
			t.Fatal(err)
		}
		store.checkpoint = func(stage string) error {
			if stage == barrier {
				return errors.New("synthetic interrupted commit")
			}
			return nil
		}
		if err := store.put(record, signer); err == nil {
			t.Fatal("barrier did not stop the commit")
		}
		store.close()
		retained := fleetRecoveryTestRecord(t, fixture)
		if retained.TxHash != record.TxHash || !bytes.Equal(retained.Raw, record.Raw) || retained.Stage != "prepared" || retained.Nonce != record.Nonce {
			t.Fatalf("%s discarded the original transaction", barrier)
		}
		if fixture.count("author_submitAndWatchExtrinsic") != 0 {
			t.Fatal("filesystem recovery transmitted")
		}
	}
}

// All identity/progress changes invalidate custody unless signed by the
// original actor. A valid signer still cannot replace immutable prepared data.
func TestFleetRecoveryStoreRejectsTamperedCustodyAndReplacement(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	record, signer := fleetRecoveryTestPrepared(t, fixture)
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if err := store.put(record, signer); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*fleetRecoveryRecord){
		func(copy *fleetRecoveryRecord) { copy.Nonce++ },
		func(copy *fleetRecoveryRecord) { copy.AuthoritySha256 = strings.Repeat("a", 64) },
		func(copy *fleetRecoveryRecord) { copy.Stage = "may_have_sent" },
		func(copy *fleetRecoveryRecord) { copy.StartHash = types.Hash{0xff} },
		func(copy *fleetRecoveryRecord) { copy.Intent.FromEpoch++ },
		func(copy *fleetRecoveryRecord) { copy.Raw = append(append([]byte(nil), copy.Raw...), 0) },
	} {
		copy := *record
		change(&copy)
		if err := copy.validate(); err == nil {
			t.Fatal("tampered custody accepted")
		}
	}
	copy := *record
	copy.Nonce++
	if err := store.put(&copy, signer); err == nil {
		t.Fatal("same actor replaced an immutable signed transaction")
	}
}

// Missing/corrupt state cannot become a fresh empty journal on restart.
func TestFleetRecoveryStoreMissingOrCorruptJournalFailsClosed(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		fixture := newFleetMainnetTestFixture(t)
		store, err := openFleetRecoveryStore(fixture.durable.Context)
		if err != nil {
			t.Fatal(err)
		}
		store.close()
		state, _ := providerStateDir()
		path := filepath.Join(state, "fleet-mainnet-recovery", "journal.json")
		if corrupt {
			err = os.WriteFile(path, []byte(`[{"schema":"broken"}]`), 0600)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if store, err := openFleetRecoveryStore(fixture.durable.Context); err == nil {
			store.close()
			t.Fatal("missing/corrupt journal was reset")
		}
	}
}

// A symlink cannot redirect either the exclusive journal or its commit file.
func TestFleetRecoveryStoreRefusesSymlinkAndOversizedJournal(t *testing.T) {
	for _, kind := range []string{"journal", "candidate", "oversized"} {
		fixture := newFleetMainnetTestFixture(t)
		store, err := openFleetRecoveryStore(fixture.durable.Context)
		if err != nil {
			t.Fatal(err)
		}
		store.close()
		state, _ := providerStateDir()
		dir := filepath.Join(state, "fleet-mainnet-recovery")
		if kind == "oversized" {
			err = os.WriteFile(filepath.Join(dir, "journal.json"), bytes.Repeat([]byte{' '}, fleetRecoveryMaxBytes+1), 0600)
		} else {
			name := "journal.next"
			if kind == "journal" {
				name = "journal.json"
				if err := os.Remove(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			err = os.Symlink(filepath.Join(dir, "initialized"), filepath.Join(dir, name))
		}
		if err != nil {
			t.Fatal(err)
		}
		if store, err := openFleetRecoveryStore(fixture.durable.Context); err == nil {
			store.close()
			t.Fatalf("%s admitted", kind)
		}
	}
}

// Runtime/fee changes cannot fork an unresolved operation into a new nonce;
// distinct semantic targets must wait for the existing liability as well.
func TestFleetRecoveryStorePendingIntentBlocksChangedTarget(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	record, signer := fleetRecoveryTestPrepared(t, fixture)
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if err := store.put(record, signer); err != nil {
		t.Fatal(err)
	}
	other := record.Intent
	other.FromEpoch++
	if _, err := store.find(other); err == nil {
		t.Fatal("changed target bypassed pending ownership")
	}
	if found, err := store.find(record.Intent); err != nil || found.TxHash != record.TxHash {
		t.Fatal("original pending transaction not selected")
	}
}

// Deleting the only record cannot turn initialized custody into an empty
// journal. The inventory and its initialization marker are checked together.
func TestFleetRecoveryStoreRejectsInventoryRemoval(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	record, signer := fleetRecoveryTestPrepared(t, fixture)
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.put(record, signer); err != nil {
		t.Fatal(err)
	}
	store.close()
	state, _ := providerStateDir()
	raw, err := json.Marshal(fleetRecoveryJournal{Schema: fleetRecoverySchema})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "fleet-mainnet-recovery", "journal.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if store, err := openFleetRecoveryStore(fixture.durable.Context); err == nil {
		store.close()
		t.Fatal("removed inventory became fresh state")
	}
}

// Even a correctly signed interrupted candidate cannot overwrite immutable
// transaction bytes/nonce in the previous durable journal.
func TestFleetRecoveryStoreRejectsCandidateReplacementWithoutMutation(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	record, signer := fleetRecoveryTestPrepared(t, fixture)
	store, err := openFleetRecoveryStore(fixture.durable.Context)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.put(record, signer); err != nil {
		t.Fatal(err)
	}
	store.close()
	state, _ := providerStateDir()
	dir := filepath.Join(state, "fleet-mainnet-recovery")
	original, err := os.ReadFile(filepath.Join(dir, "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	copy := *record
	copy.Nonce++
	if err := signer.sign(&copy); err != nil {
		t.Fatal(err)
	}
	journal := fleetRecoveryJournal{Schema: fleetRecoverySchema, Records: []*fleetRecoveryRecord{&copy}}
	digest := journal.digest()
	journal.Signature, err = signer.native.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "journal.next"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if store, err := openFleetRecoveryStore(fixture.durable.Context); err == nil {
		store.close()
		t.Fatal("immutable replacement promoted")
	}
	after, err := os.ReadFile(filepath.Join(dir, "journal.json"))
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("refused candidate mutated original custody")
	}
}
