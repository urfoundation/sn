//go:build linux

// Adjacent controls retain original custody across pressure, restart and handoff.
package chain

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/client"
	gethrpc "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A read-only mount does not invalidate intact retained bytes or enable mutation.
func TestDurableNativeJournalReadOnlyInspectionRetainsCustody(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	raw := []byte("synthetic-retained-before-read-only")
	hash := ExtrinsicHash(raw)
	if err := journal.SaveRaw(hash, raw); err != nil {
		t.Fatal(err)
	}
	fixture.Host.SetReadOnly(true)
	if entries, err := journal.Entries(); err != nil || len(entries) != 1 {
		t.Fatal("read-only volume blocked read", entries, err)
	}
	if err := journal.SaveRaw(hash, raw); err != nil {
		t.Fatal("read-only volume blocked exact raw read", err)
	}
	if err := journal.Append(entry); !errors.Is(err, durablevolume.ErrUnavailable) {
		t.Fatal("read-only admitted write", err)
	}
	fixture.Host.SetReadOnly(false)
	if err := journal.Append(entry); err != nil {
		t.Fatal("same owner did not recover read-only availability", err)
	}
}

// Unavailable leaf observations preserve the same owner and original errno.
func TestDurableNativeJournalObservationFailureCanRetry(t *testing.T) {
	for _, stage := range []string{"leaf-stat", "leaf-name"} {
		journal, _ := nativeJournalFixture(t)
		entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
		if err := journal.Append(entry); err != nil {
			t.Fatal(err)
		}
		owner := journal.guard.(*guardedNativeJournal)
		owner.checkpoint = func(step string, _ *os.File) error {
			if step == stage {
				return syscall.EIO
			}
			return nil
		}
		_, err := journal.Entries()
		if !errors.Is(err, durablevolume.ErrUnavailable) || !errors.Is(err, syscall.EIO) || errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("observation invented loss", err)
		}
		owner.checkpoint = nil
		if err := journal.Append(entry); err != nil {
			t.Fatal("same owner did not retry observation", err)
		}
	}
}

// Replacement between calls is refused even though the old file descriptor closed.
func TestDurableNativeJournalSequentialLeafReplacementStopsHandoff(t *testing.T) {
	journal, _ := nativeJournalFixture(t)
	raw := []byte("synthetic-immutable-native-raw")
	hash := ExtrinsicHash(raw)
	if err := journal.SaveRaw(hash, raw); err != nil {
		t.Fatal(err)
	}
	path := journal.RawPath(hash)
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := journal.CheckWrite(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("handoff admitted replaced raw custody", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".retained", path); err != nil {
		t.Fatal(err)
	}
	if err := journal.CheckWrite(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("old owner resurrected after restored name", err)
	}
}

// A partial tail remains unchanged on reopen, until a separate reviewed repair.
func TestDurableNativeJournalReopenRefusesPartialTail(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	completed, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	owner := journal.guard.(*guardedNativeJournal)
	owner.writeFile = func(file *os.File, raw []byte) (int, error) {
		n, err := file.Write(raw[:len(raw)/2])
		return n, errors.Join(err, syscall.ENOSPC)
	}
	if err := journal.Append(entry); !errors.Is(err, ErrJournalUncertain) {
		t.Fatal(err)
	}
	partial, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDurableJournal(fixture.Context, journal.Dir())
	if reopened != nil {
		reopened.Close()
	}
	if !errors.Is(err, ErrJournalUncertain) {
		t.Fatal("reopen accepted partial tail", err)
	}
	retained, err := os.ReadFile(journal.Path())
	if err != nil || !bytes.Equal(retained, partial) {
		t.Fatal("refusal edited retained tail", err)
	}
	// This explicit test-fixture reconciliation is outside the runtime opener.
	if err := os.WriteFile(journal.Path(), completed, 0600); err != nil {
		t.Fatal(err)
	}
	reconciled, err := ReconcileDurableJournal(fixture.Context, journal.Dir())
	if err != nil {
		t.Fatal(err)
	}
	defer reconciled.Close()
	if entries, err := reconciled.Entries(); err != nil || len(entries) != 1 {
		t.Fatal("completed prefix was lost", entries, err)
	}
}

// Crash-left temporary raw bytes stay retained and block fresh custody creation.
func TestDurableNativeJournalReopenRefusesPendingRawPublication(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	owner := journal.guard.(*guardedNativeJournal)
	owner.writeFile = func(file *os.File, raw []byte) (int, error) {
		n, err := file.Write(raw[:len(raw)/2])
		return n, errors.Join(err, syscall.ENOSPC)
	}
	raw := []byte("synthetic-interrupted-native-raw")
	if err := journal.SaveRaw(ExtrinsicHash(raw), raw); !errors.Is(err, ErrJournalUncertain) {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(filepath.Join(journal.Dir(), journalRawDir))
	if err != nil || len(before) != 1 {
		t.Fatal("temporary custody missing", err)
	}
	path := filepath.Join(journal.Dir(), journalRawDir, before[0].Name())
	bytesBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDurableJournal(fixture.Context, journal.Dir())
	if reopened != nil {
		reopened.Close()
	}
	if !errors.Is(err, ErrJournalUncertain) {
		t.Fatal("pending raw publication silently bypassed", err)
	}
	bytesAfter, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(bytesBefore, bytesAfter) {
		t.Fatal("pending bytes changed", err)
	}
}

// Post-sync replacement is checked while the actual data descriptor is still held.
func TestDurableNativeJournalPostDirectorySyncLeafLossIsUncertain(t *testing.T) {
	journal, _ := nativeJournalFixture(t)
	owner := journal.guard.(*guardedNativeJournal)
	owner.checkpoint = func(stage string, _ *os.File) error {
		if stage != "append-directory-synced" {
			return nil
		}
		return os.Rename(journal.Path(), journal.Path()+".retained")
	}
	err := journal.Append(JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized})
	if !errors.Is(err, durablevolume.ErrIdentity) || !errors.Is(err, ErrJournalUncertain) {
		t.Fatal("post-sync custody loss acknowledged", err)
	}
	if raw, err := os.ReadFile(journal.Path() + ".retained"); err != nil || len(raw) == 0 {
		t.Fatal("completed bytes were not retained", err)
	}
}

// Explicit owner-local admission remains separate from daemon production policy.
func TestDurableNativeJournalRequiresCorrectExplicitPolicy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner-root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "journal")
	if journal, err := OpenDurableJournal(context.Background(), path); err == nil {
		journal.Close()
		t.Fatal("missing policy created custody")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unguarded journal directory was created", err)
	}
	fixture := durablefixture.NewOwnerLocal(t, t.Context(), root)
	if journal, err := OpenDurableJournal(fixture.Context, path); err == nil {
		journal.Close()
		t.Fatal("daemon accepted owner-local schema")
	}
	durablefixture.ProvisionNativeJournal(t, path)
	journal, err := OpenOwnerLocalJournal(fixture.Context, path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := journal.Append(JournalEntry{Command: "synthetic", Stage: JournalStageFinalized}); err != nil {
		t.Fatal(err)
	}
}

// Unexpected RPC work is counted before any synthetic transport response.
type nativeJournalAdmissionClient struct {
	client.Client
	calls int
}

// No network is contacted by this causal admission boundary.
func (self *nativeJournalAdmissionClient) CallContext(context.Context, any, string, ...any) error {
	self.calls++
	return errors.New("synthetic transport must remain unused")
}

// Public submission refuses storage pressure before querying or signing anything.
func TestDurableNativeSubmissionChecksStorageBeforeRpc(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	fixture.Host.SetReserve(0, 0)
	client := &nativeJournalAdmissionClient{}
	bound := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: &types.Metadata{}, Runtime: &types.RuntimeVersion{}}
	signer, err := crv4.KeypairFromSeed([32]byte{31, 19, 7})
	if err != nil {
		t.Fatal(err)
	}
	_, err = SubmitCall(fixture.Context, bound, SubmitRequest{Signer: signer, Journal: journal, Apply: true, Output: io.Discard})
	if !errors.Is(err, durablevolume.ErrUnavailable) || client.calls != 0 {
		t.Fatalf("public submit crossed refused storage: calls=%d err=%v", client.calls, err)
	}
}

// A request can stop while its longer-lived custody owner remains healthy.
func TestDurableNativeSubmissionCancellationPrecedesOwnedIo(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	ctx, cancel := context.WithCancel(fixture.Context)
	cancel()
	client := &nativeJournalAdmissionClient{}
	bound := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: &types.Metadata{}, Runtime: &types.RuntimeVersion{}}
	signer, err := crv4.KeypairFromSeed([32]byte{31, 19, 7})
	if err != nil {
		t.Fatal(err)
	}
	_, err = SubmitCall(ctx, bound, SubmitRequest{Signer: signer, Journal: journal, Apply: true, Output: io.Discard})
	if !errors.Is(err, context.Canceled) || client.calls != 0 {
		t.Fatalf("stopped request crossed live owner: calls=%d err=%v", client.calls, err)
	}
	if err := journal.CheckWrite(); err != nil {
		t.Fatal("request cancellation poisoned independent owner", err)
	}
}

// Cancel during one successful kernel observation without fabricating its facts.
type nativeJournalCancelHost struct {
	durablevolume.Host
	cancel context.CancelFunc
	armed  bool
}

// The selected fact boundary closes the actual retained context deterministically.
func (self *nativeJournalCancelHost) Filesystem(file *os.File) (durablevolume.Filesystem, error) {
	facts, err := self.Host.Filesystem(file)
	if self.armed {
		self.armed = false
		self.cancel()
	}
	return facts, err
}

// A successful empty read must still close its context admission after observation.
func TestDurableNativeJournalCancellationDuringEmptyReadAdmission(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	path := journal.Dir()
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(fixture.Context)
	defer cancel()
	host := &nativeJournalCancelHost{Host: fixture.Host, cancel: cancel}
	journal, err := OpenDurableJournal(durablepath.WithHost(ctx, host), path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	host.armed = true
	entries, err := journal.Entries()
	if !errors.Is(err, context.Canceled) || entries != nil {
		t.Fatal("empty read acknowledged canceled admission", entries, err)
	}
}

// Losing only the log cannot turn retained raw custody into a fresh journal.
func TestDurableNativeJournalReopenRefusesMissingLog(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	if err := journal.Append(JournalEntry{Command: "synthetic", Stage: JournalStageFinalized}); err != nil {
		t.Fatal(err)
	}
	path := journal.Path()
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDurableJournal(fixture.Context, journal.Dir())
	if reopened != nil {
		reopened.Close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("missing completed journal reopened as pristine custody", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refusal recreated the lost log", err)
	}
}

// The persisted member census must outlive the process-local open-leaf map.
func TestDurableNativeJournalReopenRefusesMissingRawMember(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	raw := []byte("synthetic-completed-native-raw-member")
	hash := ExtrinsicHash(raw)
	if err := journal.SaveRaw(hash, raw); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(journal.RawPath(hash), filepath.Join(journal.Dir(), "retained.scale")); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDurableJournal(fixture.Context, journal.Dir())
	if reopened != nil {
		reopened.Close()
	}
	if !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("lost completed raw member reopened as pristine custody", err)
	}
}

// Admission owns the raw directory even before the first raw leaf exists.
func TestDurableNativeJournalEmptyRawDirectoryLossStopsHandoff(t *testing.T) {
	journal, _ := nativeJournalFixture(t)
	path := filepath.Join(journal.Dir(), journalRawDir)
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := journal.CheckWrite(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("empty raw directory replacement admitted handoff", err)
	}
}

// Synthetic nonce and fee responses permit real test-only encoding without a node.
type nativeJournalCallbackClient struct {
	client.Client
	calls         []string
	subscriptions int
}

func (self *nativeJournalCallbackClient) CallContext(_ context.Context, result any, method string, _ ...any) error {
	self.calls = append(self.calls, method)
	switch method {
	case "system_accountNextIndex":
		*result.(*uint32) = 0
		return nil
	case "payment_queryInfo":
		result.(*NativeTransactionFeeResponse).PartialFee = []byte("1")
		return nil
	default:
		return errors.New("unexpected synthetic native callback transport")
	}
}

// An unexpected canceled submission is observed without any network or panic.
func (self *nativeJournalCallbackClient) Subscribe(ctx context.Context, _, _, _, _ string, _ any, _ ...any) (*gethrpc.ClientSubscription, error) {
	self.subscriptions++
	return nil, errors.Join(ctx.Err(), errors.New("synthetic subscription must remain unused"))
}

// A callback's successful return does not override a stopped signing request.
func TestDurableNativeSubmissionRuntimeCallbackCancellationPrecedesSigning(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	ctx, cancel := context.WithCancel(fixture.Context)
	defer cancel()
	transport := &nativeJournalCallbackClient{}
	bound := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: transport}, Meta: &types.Metadata{}, Runtime: &types.RuntimeVersion{}}
	signer, err := crv4.KeypairFromSeed([32]byte{31, 19, 7})
	if err != nil {
		t.Fatal(err)
	}
	result, err := SubmitCall(ctx, bound, SubmitRequest{Signer: signer, Journal: journal, FeeLimitRao: 1, Output: io.Discard, RuntimeAdmission: func(context.Context, types.Hash) error { cancel(); return nil }})
	if !errors.Is(err, context.Canceled) || len(result.Raw) != 0 || len(transport.calls) != 1 {
		t.Fatalf("canceled callback crossed signing: raw=%d calls=%v err=%v", len(result.Raw), transport.calls, err)
	}
}

// Before-send cancellation preserves prepared bytes without inventing a send failure.
func TestDurableNativeSubmissionBeforeBroadcastCancellationRetainsPreparedOnly(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	ctx, cancel := context.WithCancel(fixture.Context)
	defer cancel()
	transport := &nativeJournalCallbackClient{}
	bound := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: transport}, Meta: &types.Metadata{}, Runtime: &types.RuntimeVersion{}}
	signer, err := crv4.KeypairFromSeed([32]byte{31, 19, 7})
	if err != nil {
		t.Fatal(err)
	}
	result, err := SubmitCall(ctx, bound, SubmitRequest{Signer: signer, Journal: journal, Apply: true, FeeLimitRao: 1, Output: io.Discard, BeforeBroadcast: func() error { cancel(); return nil }})
	if !errors.Is(err, context.Canceled) || len(result.Raw) == 0 || len(transport.calls) != 2 || transport.subscriptions != 0 {
		t.Errorf("unexpected request boundary: raw=%d calls=%v subscriptions=%d err=%v", len(result.Raw), transport.calls, transport.subscriptions, err)
	}
	entries, err := journal.Entries()
	if err != nil || len(entries) != 1 || entries[0].Stage != JournalStageBroadcast {
		t.Fatal("canceled pre-send callback invented a send result", entries, err)
	}
}
