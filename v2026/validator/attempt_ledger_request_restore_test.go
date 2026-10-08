//go:build linux || darwin

// Exact original request bytes survive the same physical export and public
// plan/apply used for the containing assignment ledger, including a lost ack.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Public wire messages use the independently retained synthetic client key;
// the adapter never receives that key or fabricates a returned assignment.
func attemptRequestRestoreSeed(t *testing.T, key ed25519.PrivateKey, client connect.Id, nonce byte) (body, message, signature []byte) {
	t.Helper()
	public := key.Public().(ed25519.PublicKey)
	clientNonce := bytes.Repeat([]byte{nonce}, 32)
	message, err := connect.BuildVerifySeedMessage(public, clientNonce, 8)
	if err != nil {
		t.Fatal(err)
	}
	signature = ed25519.Sign(key, message)
	body, err = json.Marshal(connect.VerifySeedArgs{ClientId: client, Vpk: public, ClientNonce: clientNonce, M: 8, SeedSig: signature})
	if err != nil {
		t.Fatal(err)
	}
	return body, message, signature
}

// The second request stops at an actual production fsync barrier when asked.
// Its signed pending original and journal suffix are exported without retry.
func newAttemptRequestRestoreFixture(t *testing.T, pending bool) (*attemptLedgerRestoreFixture, ProviderAttemptRequestCheckpoint, [][]byte) {
	t.Helper()
	fixture := newAttemptRecordStoreTestFixture(t, 1)
	var original ProviderAttemptRequestCheckpoint
	var records [][]byte
	f := newAttemptLedgerRestoreFixture(t, fixture, false, "", func(f *attemptLedgerRestoreFixture) {
		preparation := ProviderAttemptRequestPreparation{Identity: ProviderAttemptRequestIdentity{Ledger: fixture.identity, Coordinator: f.scope.Coordinator, ClientId: connect.Id{7}, PolicyHash: [32]byte{9}}, Limits: ProviderAttemptRequestLimits{MaxRecords: 8, MaxRecordBytes: 8192, MaxJournalBytes: 64 * 1024}, Birth: attemptLedgerTestBoundary()}
		prepareProviderRequestTestOwner(t, f.root, preparation)
		journal, err := OpenProviderAttemptRequestJournal(f.storage.Context, f.root, preparation, fixture.validatorKey)
		if err != nil {
			t.Fatal(err)
		}
		defer journal.Close()
		lost := errors.New("synthetic original request synced before checkpoint acknowledgement")
		for index := byte(1); index <= 2; index++ {
			if pending && index == 2 {
				journal.step = func(stage string) error {
					if stage == "after-request-file-sync" {
						return lost
					}
					return nil
				}
			}
			body, message, signature := attemptRequestRestoreSeed(t, fixture.validatorKey, preparation.Identity.ClientId, index)
			record, err := journal.Append(t.Context(), preparation.Birth, connect.Id{11}, body, message, signature)
			var raw []byte
			if pending && index == 2 {
				if !errors.Is(err, lost) || record != nil {
					t.Fatal("source did not retain exact interrupted request", err)
				}
				raw, err = os.ReadFile(filepath.Join(f.root, ProviderAttemptRequestPendingName))
			} else if err == nil {
				raw, err = json.Marshal(record)
			}
			if err != nil {
				t.Fatal(err)
			}
			records = append(records, raw)
		}
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
		root, err := os.Open(f.root)
		if err != nil {
			t.Fatal(err)
		}
		raw, absent, readErr := readProviderAttemptRequestAttribute(root)
		if err := errors.Join(readErr, root.Close(), attemptStoreDecode(raw, &original)); err != nil || absent {
			t.Fatal("source lost original request checkpoint", err)
		}
		f.scope.Requests = &AttemptLedgerRequestPreparationScope{Preparation: preparation, ExpectedHead: original.Committed}
	})
	return f, original, records
}

// Restore keeps the committed/pending distinction, then the real constructor
// finishes the exact interrupted original once before allowing sequence three.
func TestAttemptLedgerRequestRestoreRetainsCommittedAndPendingOriginals(t *testing.T) {
	for _, pending := range []bool{false, true} {
		f, original, records := newAttemptRequestRestoreFixture(t, pending)
		ctx := f.apply(t)
		root, err := os.Open(f.root)
		if err != nil {
			t.Fatal(err)
		}
		raw, absent, readErr := readProviderAttemptRequestAttribute(root)
		var rebound ProviderAttemptRequestCheckpoint
		if err := errors.Join(readErr, root.Close(), attemptStoreDecode(raw, &rebound)); err != nil || absent {
			t.Fatal("restore omitted original request attribute", pending, err)
		}
		if rebound.DirectoryInode == original.DirectoryInode || rebound.FileInode == original.FileInode {
			t.Fatal("restore retained stale physical request coordinates")
		}
		rebound.DirectoryInode, rebound.FileInode = original.DirectoryInode, original.FileInode
		if !reflect.DeepEqual(rebound, original) || (original.Pending != nil) != pending {
			t.Fatal("restore changed original request head, birth, limits or pending status")
		}
		preparation := f.scope.Requests.Preparation
		journal, err := OpenProviderAttemptRequestJournal(ctx, f.root, preparation, f.fixture.validatorKey)
		if err != nil {
			t.Fatal("restored original request owner could not reopen", pending, err)
		}
		func() {
			defer journal.Close()
			head, birth, err := journal.Head(t.Context())
			if err != nil || head.Sequence != 2 || birth != preparation.Birth {
				t.Fatal("runtime lost or duplicated original pending request", pending, head, err)
			}
			var actual [][]byte
			if err := journal.Walk(t.Context(), func(record ProviderAttemptRequestRecord) error {
				raw, err := json.Marshal(record)
				actual = append(actual, raw)
				return err
			}); err != nil || !reflect.DeepEqual(actual, records) {
				t.Fatal("restored request signatures or original transport bytes changed", pending, err)
			}
			body, message, signature := attemptRequestRestoreSeed(t, f.fixture.validatorKey, preparation.Identity.ClientId, 3)
			next, err := journal.Append(t.Context(), preparation.Birth, connect.Id{11}, body, message, signature)
			if err != nil || next.Sequence != 3 || next.PreviousHash != head.Hash {
				t.Fatal("restored original request did not continue exact signed chain", pending, err)
			}
		}()
		ledger, err := NewDiskAttemptLedger(ctx, f.root, f.fixture.identity, f.scope.Coordinator, f.fixture.validatorKey, f.scope.Limits)
		if err != nil {
			t.Fatal(err)
		}
		head, headErr := ledger.Head()
		if err := errors.Join(headErr, ledger.Close()); err != nil || head != f.scope.ExpectedHead || head.LastSequence != 0 {
			t.Fatal("unanswered requests became returned provider assignments", head, err)
		}
		if _, err := os.Lstat(filepath.Join(f.root, ProviderAttemptRequestPendingName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("runtime did not finish original pending cleanup", pending, err)
		}
	}
}

// Export metadata cannot replace absent birth, widen a signed owner, omit its
// journal, or rename an unfinished temporary publication as a clean original.
func TestAttemptLedgerRequestRestoreRefusesMissingOrForeignCustody(t *testing.T) {
	f, _, _ := newAttemptRequestRestoreFixture(t, true)
	for _, fault := range []string{"missing-attribute", "missing-journal", "missing-pending", "changed-birth", "changed-limits", "changed-inode", "omitted-scope", "temporary-member", "noncanonical-attribute"} {
		raw, err := json.Marshal(f.report)
		if err != nil {
			t.Fatal(err)
		}
		var report durablevolume.Inventory
		if err := json.Unmarshal(raw, &report); err != nil {
			t.Fatal(err)
		}
		scope := f.scope
		if fault == "omitted-scope" {
			scope.Requests = nil
		}
		for index := 0; index < len(report.Entries); index++ {
			entry := &report.Entries[index]
			if fault == "missing-journal" && entry.Path == ProviderAttemptRequestJournalName || fault == "missing-pending" && entry.Path == ProviderAttemptRequestPendingName {
				report.Entries = append(report.Entries[:index], report.Entries[index+1:]...)
				break
			}
			if fault == "temporary-member" && entry.Path == ProviderAttemptRequestPendingName {
				entry.Path += ".tmp"
			}
			for attributeIndex := range entry.OwnerAttributes {
				attribute := &entry.OwnerAttributes[attributeIndex]
				if attribute.Name != ProviderAttemptRequestAttribute {
					continue
				}
				if fault == "missing-attribute" {
					entry.OwnerAttributes = append(entry.OwnerAttributes[:attributeIndex], entry.OwnerAttributes[attributeIndex+1:]...)
					break
				}
				var checkpoint ProviderAttemptRequestCheckpoint
				if err := json.Unmarshal(attribute.Value, &checkpoint); err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "changed-birth":
					checkpoint.Birth.EVMBlock--
				case "changed-limits":
					checkpoint.Limits.MaxRecords++
				case "changed-inode":
					checkpoint.FileInode++
				}
				attribute.Value, err = json.Marshal(checkpoint)
				if err != nil {
					t.Fatal(err)
				}
				if fault == "noncanonical-attribute" {
					attribute.Value = append(attribute.Value, '\n')
				}
				attribute.Sha256 = attemptLedgerCustodyDigest(attribute.Value)
			}
		}
		inputs, err := json.Marshal(scope)
		if err != nil {
			t.Fatal(err)
		}
		owner := durablevolume.PreparationOwner{Kind: AttemptLedgerPreparationKind, RelativePath: ".", Purpose: "restore", Inputs: inputs}
		if _, err := PlanAttemptLedgerRestore(t.Context(), "exact-stage", owner, report); err == nil {
			t.Fatal("restore accepted missing or foreign original request custody", fault)
		}
		entries, err := os.ReadDir(f.root)
		if err != nil || len(entries) != 0 {
			t.Fatal("refused original request custody created target bytes", fault, err)
		}
	}
}

// A self-consistent physical census and replacement hash cannot authenticate
// forged transport bytes; the complete original signed prefix is still read.
func TestAttemptLedgerRequestPreparationVerifiesEveryOriginalSignature(t *testing.T) {
	f, original, records := newAttemptRequestRestoreFixture(t, false)
	var forged ProviderAttemptRequestRecord
	if err := json.Unmarshal(records[1], &forged); err != nil {
		t.Fatal(err)
	}
	forged.RequestSignature[0] ^= 1
	raw, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	journalBytes := append(append(append(bytes.Clone(records[0]), '\n'), raw...), '\n')
	path := f.root + ".original-held"
	if err := os.WriteFile(filepath.Join(path, ProviderAttemptRequestJournalName), journalBytes, 0600); err != nil {
		t.Fatal(err)
	}
	original.Committed.Hash, original.Committed.Bytes = sha256.Sum256(raw), uint64(len(journalBytes))
	anchor, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(path, ProviderAttemptRequestAttribute, anchor, unix.XATTR_REPLACE); err != nil {
		t.Fatal(err)
	}
	f.scope.Requests.ExpectedHead = original.Committed
	root, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := InspectAttemptLedgerPreparation(t.Context(), root, f.scope); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) {
		t.Fatal("preparation accepted a forged original request signature", err)
	}
	after, err := os.ReadFile(filepath.Join(path, ProviderAttemptRequestJournalName))
	if err != nil || !bytes.Equal(after, journalBytes) {
		t.Fatal("refused request inspection rewrote retained evidence", err)
	}
}
