//go:build linux || darwin

package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// The cheap chain projection must never admit fields that only the subsequent
// complete canonical decoder or signature verifier can reject. Keep the chain
// headers and lifetime byte count consistent so each fault reaches that pass.
func TestAttemptRecordStoreReplayAuthenticatesFieldsOutsideChainProjection(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	for _, fault := range []string{"signature", "identity", "shape", "unknown field", "duplicate field", "escaped field", "whitespace"} {
		t.Run(fault, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "store")
			bounds := attemptRecordStoreTestBounds()
			store := openAttemptRecordStoreTest(t, path, fixture, bounds, attemptRecordStoreHooks{})
			appendAttemptRecordStoreTest(t, store, fixture.recordTs)
			original, err := store.db.Get(attemptStoreRecordKey(1), nil)
			if err != nil {
				t.Fatal(err)
			}
			var record AttemptRecord
			if err := json.Unmarshal(original, &record); err != nil {
				t.Fatal(err)
			}
			var raw []byte
			switch fault {
			case "signature":
				record.Signature[0] ^= 1
			case "identity":
				record.Identity.ValidatorID++
			case "shape":
				record.M = 0
			case "unknown field":
				raw = append(bytes.Clone(original[:len(original)-1]), []byte(`,"unowned":true}`)...)
			case "duplicate field":
				raw = append([]byte(`{"sequence":1,`), original[1:]...)
			case "escaped field":
				raw = bytes.Replace(original, []byte(`"assignments"`), []byte(`"\u0061ssignments"`), 1)
			case "whitespace":
				raw = append([]byte{' '}, original...)
			}
			if raw == nil {
				raw, err = json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
			}
			head, err := store.Head()
			if err != nil {
				t.Fatal(err)
			}
			head.RecordBytes = head.RecordBytes - uint64(len(original)) + uint64(len(raw))
			headBytes, err := json.Marshal(head)
			if err != nil {
				t.Fatal(err)
			}
			batch := new(leveldb.Batch)
			batch.Put(attemptStoreRecordKey(1), raw)
			batch.Put([]byte("head"), headBytes)
			if err := store.db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := openAttemptRecordStore(t.Context(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), bounds)
			if err == nil {
				_ = reopened.Close()
				t.Fatal("chain-only projection admitted unauthenticated record fields")
			}
			if !strings.Contains(err.Error(), "marker does not identify its signed record") {
				t.Fatalf("fault did not reach full indexed-record authentication: %v", err)
			}
		})
	}
}

// Valid signatures and a consistent global chain do not authorize a changed
// pending context. Rebuild all affected hashes and indexes to isolate lifecycle.
func TestAttemptRecordStoreReplayRejectsSignedLifecycleMutation(t *testing.T) {
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	path := filepath.Join(t.TempDir(), "store")
	bounds := attemptRecordStoreTestBounds()
	store := openAttemptRecordStoreTest(t, path, fixture, bounds, attemptRecordStoreHooks{})
	appendAttemptRecordStoreTest(t, store, fixture.recordTs)
	head, err := store.Head()
	if err != nil {
		t.Fatal(err)
	}
	batch := new(leveldb.Batch)
	root := zeroAttemptHash()
	head.RecordBytes = 0
	for index, record := range fixture.recordTs {
		if index == 1 {
			record.Boundary.SettlementEpoch++
		}
		record.PreviousHash = root
		record = resignAttemptRecordStoreTest(t, record, fixture.validatorKey)
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		batch.Put(attemptStoreRecordKey(record.Sequence), raw)
		batch.Put(attemptStoreTrailRecordKey(record.TrailID, record.Sequence), []byte(record.RecordHash))
		root = record.RecordHash
		head.RecordBytes += uint64(len(raw))
		if index == len(fixture.recordTs)-1 {
			state, err := json.Marshal(attemptRecordStoreTrail{LastSequence: record.Sequence, RecordHash: root, Terminal: true})
			if err != nil {
				t.Fatal(err)
			}
			batch.Put(attemptStoreTrailKey(record.TrailID), state)
		}
	}
	head.Root = root
	headBytes, err := json.Marshal(head)
	if err != nil {
		t.Fatal(err)
	}
	batch.Put([]byte("head"), headBytes)
	if err := store.db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openAttemptRecordStore(t.Context(), path, fixture.identity, "0x1111111111111111111111111111111111111111", fixture.validatorKey.Public().(ed25519.PublicKey), bounds)
	if err == nil {
		_ = reopened.Close()
		t.Fatal("signed global chain bypassed lifecycle validation")
	}
	if !strings.Contains(err.Error(), "checkpoint changed its pinned identity") {
		t.Fatalf("forgery did not reach complete lifecycle validation: %v", err)
	}
}
