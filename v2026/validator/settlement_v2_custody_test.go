//go:build linux || darwin

// The settlement v2 audit's durable custody: objects by sha256, valid rows
// and null declarations per pool epoch, audit records, and a finalized
// root's accounts rebuilt from custody alone.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/stabi"
)

func newSettlementV2CustodyTest(t *testing.T) *settlementV2Custody {
	t.Helper()
	custody, err := openSettlementV2Custody(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return custody
}

// block puts a file where custody keeps a directory, so nothing can be
// written there.
func settlementV2CustodyTestBlock(t *testing.T, directory string) {
	t.Helper()
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directory, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// VA-7: custody keeps each object under its sha256, owner-only in an
// owner-only directory, returns exactly its bytes, and never writes a held
// object again.
func TestSettlementV2CustodyKeepsObjects(t *testing.T) {
	t.Parallel()
	custody := newSettlementV2CustodyTest(t)
	ctx := context.Background()
	data := []byte("ledger object")
	ref := ledger.RefOf(data)
	if _, ok, err := custody.Object(ctx, ref); ok || err != nil {
		t.Fatalf("an object was held before it was kept: %v", err)
	}
	if err := custody.Keep(ctx, ref, data); err != nil {
		t.Fatal(err)
	}
	held, ok, err := custody.Object(ctx, ref)
	if err != nil || !ok || !bytes.Equal(held, data) {
		t.Fatalf("held %q, %v: %v", held, ok, err)
	}
	path := custody.path("objects", hex.EncodeToString(ref[:]))
	for _, check := range []struct {
		path string
		mode os.FileMode
	}{{path, 0o600}, {filepath.Dir(path), 0o700}, {custody.directory, 0o700}} {
		if info, err := os.Stat(check.path); err != nil || info.Mode().Perm() != check.mode {
			t.Fatalf("%s mode: %v", check.path, err)
		}
	}
	// A write would first make the directory private again.
	if err := os.Chmod(filepath.Dir(path), 0o500); err != nil {
		t.Fatal(err)
	}
	if err := custody.Keep(ctx, ref, data); err != nil {
		t.Fatalf("keeping a held object: %v", err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("a held object was written again: %v", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	settlementV2CustodyTestBlock(t, filepath.Dir(path))
	other := []byte("another object")
	if err := custody.Keep(ctx, ledger.RefOf(other), other); err == nil {
		t.Fatal("an unwritable custody kept an object")
	}
}

// VA-7: held objects are trusted, so bytes that no longer match their
// reference are an error, never an object; so is an unreadable object.
func TestSettlementV2CustodyRefusesCorruptObjects(t *testing.T) {
	t.Parallel()
	custody := newSettlementV2CustodyTest(t)
	ctx := context.Background()
	data := []byte("ledger object")
	ref := ledger.RefOf(data)
	if err := custody.Keep(ctx, ref, data); err != nil {
		t.Fatal(err)
	}
	path := custody.path("objects", hex.EncodeToString(ref[:]))
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := custody.Object(ctx, ref); err == nil || !strings.Contains(err.Error(), "does not match its reference") {
		t.Fatalf("a corrupt object: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := custody.Object(ctx, ref); err == nil {
		t.Fatal("an unreadable object was returned")
	}
}

// VA-7: the epochs whose rows were found valid, with their chunks, and every
// null declaration are recorded per pool epoch.
func TestSettlementV2CustodyRecordsRowsAndNulls(t *testing.T) {
	t.Parallel()
	custody := newSettlementV2CustodyTest(t)
	ctx := context.Background()
	if _, ok, err := custody.ValidRows(ctx, 1, 4); ok || err != nil {
		t.Fatalf("rows held before they were kept: %v", err)
	}
	chunks := []ledger.Ref{ledger.RefOf([]byte("a")), ledger.RefOf([]byte("b"))}
	for _, want := range [][]ledger.Ref{chunks, {}} {
		if err := custody.KeepValidRows(ctx, 1, 4, want); err != nil {
			t.Fatal(err)
		}
		if held, ok, err := custody.ValidRows(ctx, 1, 4); err != nil || !ok || !slices.Equal(held, want) {
			t.Fatalf("rows held as %v: %v", held, err)
		}
	}
	if _, ok, err := custody.ValidRows(ctx, 1, 5); ok || err != nil {
		t.Fatalf("another epoch's rows: %v", err)
	}
	if err := custody.KeepNull(ctx, 1, 4, 9); err != nil {
		t.Fatal(err)
	}
	var null settlementV2NullRecord
	if ok, err := custody.readJSON(custody.path("nulls", "1-4-9.json"), &null); err != nil || !ok || null != (settlementV2NullRecord{Schema: settlementV2CustodySchema, NoID: 1, Epoch: 4, Seq: 9}) {
		t.Fatalf("null declaration recorded as %+v: %v", null, err)
	}
	settlementV2CustodyTestBlock(t, custody.path("rows", ""))
	if err := custody.KeepValidRows(ctx, 1, 6, chunks); err == nil {
		t.Fatal("an unwritable custody recorded rows")
	}
}

// VA-7: a rows record that does not decode, names another pool epoch or
// holds a malformed reference is refused.
func TestSettlementV2CustodyRefusesForeignRows(t *testing.T) {
	t.Parallel()
	for name, record := range map[string]string{
		"malformed":       `{`,
		"another schema":  `{"schema":"x","no_id":1,"epoch":4,"chunks":[]}`,
		"another pool":    `{"schema":"` + settlementV2CustodySchema + `","no_id":2,"epoch":4,"chunks":[]}`,
		"another epoch":   `{"schema":"` + settlementV2CustodySchema + `","no_id":1,"epoch":5,"chunks":[]}`,
		"no chunk list":   `{"schema":"` + settlementV2CustodySchema + `","no_id":1,"epoch":4,"chunks":null}`,
		"a bad reference": `{"schema":"` + settlementV2CustodySchema + `","no_id":1,"epoch":4,"chunks":["sha256:00"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			custody := newSettlementV2CustodyTest(t)
			if err := atomicStateWrite(custody.path("rows", "1-4.json"), []byte(record), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := custody.ValidRows(context.Background(), 1, 4); err == nil || ok {
				t.Fatalf("record %s was accepted", record)
			}
		})
	}
}

// VA-7: the audit record of a pending root is new until kept, then read
// back as kept; a record of another root under its seq, or one that does
// not decode, is refused.
func TestSettlementV2CustodyAuditRecords(t *testing.T) {
	t.Parallel()
	custody := newSettlementV2CustodyTest(t)
	pending := stabi.ISTSettlementVault2Pending{Seq: 7, Root: [32]byte{0xa0}, Total: big.NewInt(150), LeafCount: 5, Epoch: 3, LedgerHash: [32]byte{0xa1}, CommittedBlock: 405}
	record, err := custody.auditRecord(1, pending)
	if err != nil || record.Decision != "" || record.Root != "0xa0"+strings.Repeat("00", 31) || record.Total != "150" ||
		record.LedgerHash != ledger.Ref(pending.LedgerHash).String() || record.CommittedBlock != 405 || record.Schema != settlementV2CustodySchema {
		t.Fatalf("new record %+v: %v", record, err)
	}
	record.Decision, record.Demands, record.FetchedBytes = settlementV2DecisionDemand, []uint64{1, 2}, 99
	if err := custody.keepAuditRecord(record); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(custody.path("audits", "1-7.json"))
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("record file: %v", err)
	}
	again, err := custody.auditRecord(1, pending)
	if err != nil || again.Decision != settlementV2DecisionDemand || !slices.Equal(again.Demands, []uint64{1, 2}) || again.FetchedBytes != 99 {
		t.Fatalf("record read back as %+v: %v", again, err)
	}
	other := pending
	other.Root[0] ^= 1
	if _, err := custody.auditRecord(1, other); err == nil || !strings.Contains(err.Error(), "is not the audit of pool 1's pending root seq 7") {
		t.Fatalf("another root's record: %v", err)
	}
	if err := os.WriteFile(custody.path("audits", "1-7.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := custody.auditRecord(1, pending); err == nil {
		t.Fatal("a record that does not decode was read")
	}
}

// G2: a finalized root's accounts come from custody alone: rebuilt when
// custody holds every object and they rebuild the root, nil otherwise, and
// an error only for custody's own failures or the context.
func TestSettlementV2CustodyAccounts(t *testing.T) {
	t.Parallel()
	f := settlementV2AuditTestF(t)
	hold := func(t *testing.T, objects ledger.Objects) *settlementV2Custody {
		t.Helper()
		custody := newSettlementV2CustodyTest(t)
		for ref, data := range objects {
			if err := custody.Keep(context.Background(), ref, data); err != nil {
				t.Fatal(err)
			}
		}
		return custody
	}
	accounts, tree, err := hold(t, f.Objects).accounts(context.Background(), f.Commit)
	if root, total := tree.Root(); err != nil || !slices.Equal(accounts, f.Accounts) || root != f.Commit.Root || !total.Eq(&f.Commit.Total) {
		t.Fatalf("accounts not rebuilt: %v", err)
	}
	header, err := ledger.DecodeHeader(f.Header)
	if err != nil {
		t.Fatal(err)
	}
	unusable := map[string]func() (ledger.Objects, ledger.Root){
		"header not held": func() (ledger.Objects, ledger.Root) { return ledger.Objects{}, f.Commit },
		"header not canonical": func() (ledger.Objects, ledger.Root) {
			data := append(slices.Clone(f.Header), ' ')
			root := f.Commit
			root.LedgerHash = ledger.RefOf(data)
			return ledger.Objects{root.LedgerHash: data}, root
		},
		"chunk not held": func() (ledger.Objects, ledger.Root) {
			return ledger.Objects{f.Commit.LedgerHash: f.Header}, f.Commit
		},
		"chunk not whole rows": func() (ledger.Objects, ledger.Root) {
			chunk := append(slices.Clone(f.Objects[header.Accounts[0]]), 0)
			changed := *header
			changed.Accounts = []ledger.Ref{ledger.RefOf(chunk)}
			data, err := ledger.EncodeHeader(&changed)
			if err != nil {
				t.Fatal(err)
			}
			root := f.Commit
			root.LedgerHash = ledger.RefOf(data)
			return ledger.Objects{root.LedgerHash: data, changed.Accounts[0]: chunk}, root
		},
		"no accounts": func() (ledger.Objects, ledger.Root) {
			changed := *header
			changed.Accounts = []ledger.Ref{}
			data, err := ledger.EncodeHeader(&changed)
			if err != nil {
				t.Fatal(err)
			}
			root := f.Commit
			root.LedgerHash = ledger.RefOf(data)
			return ledger.Objects{root.LedgerHash: data}, root
		},
		"another root": func() (ledger.Objects, ledger.Root) {
			root := f.Commit
			root.Root[0] ^= 1
			return f.Objects, root
		},
		"another total": func() (ledger.Objects, ledger.Root) {
			root := f.Commit
			root.Total.AddUint64(&root.Total, 1)
			return f.Objects, root
		},
		"another leaf count": func() (ledger.Objects, ledger.Root) {
			root := f.Commit
			root.LeafCount++
			return f.Objects, root
		},
	}
	for name, setup := range unusable {
		t.Run(name, func(t *testing.T) {
			objects, root := setup()
			if accounts, tree, err := hold(t, objects).accounts(context.Background(), root); accounts != nil || tree != nil || err != nil {
				t.Fatalf("custody gave accounts %d, tree %v: %v", len(accounts), tree != nil, err)
			}
		})
	}
	t.Run("unreadable chunk", func(t *testing.T) {
		custody := hold(t, ledger.Objects{f.Commit.LedgerHash: f.Header})
		if err := os.Mkdir(custody.path("objects", hex.EncodeToString(header.Accounts[0][:])), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, _, err := custody.accounts(context.Background(), f.Commit); err == nil {
			t.Fatal("an unreadable chunk gave no error")
		}
	})
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := hold(t, f.Objects).accounts(ctx, f.Commit); err != context.Canceled {
			t.Fatalf("canceled: %v", err)
		}
	})
}

// VA-7: custody lives under an absolute state directory, and refuses one it
// cannot make private.
func TestSettlementV2CustodyOpen(t *testing.T) {
	t.Parallel()
	if _, err := openSettlementV2Custody("relative/state"); err == nil || !strings.Contains(err.Error(), "absolute state directory") {
		t.Fatalf("a relative state directory: %v", err)
	}
	file := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openSettlementV2Custody(file); err == nil {
		t.Fatal("custody opened under a file")
	}
}
