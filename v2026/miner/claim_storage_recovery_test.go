//go:build linux

// These controls keep real queue files, locks, anchors and syncs. Kernel facts
// and post-syscall barriers select the physical failure without wall-clock races.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"github.com/urnetwork/sdk/v2026"
	"gopkg.in/yaml.v3"
)

func claimStorageTestOwner(t *testing.T, ctx context.Context, cfg *ClaimDaemonConfig) (*claimQueueStore, *durablefixture.Fixture) {
	t.Helper()
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	durablefixture.ProvisionSnapshot(t, cfg.StateDir, "provider-claim-queue", "claim-queue.json", maximumClaimQueueBytes, "", nil)
	fixture := durablefixture.New(t, ctx, cfg.StateDir)
	store, err := newClaimQueueStore(cfg.StateDir, fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	return store, fixture
}

// A prepublication reserve refusal retains the same descriptor generation and
// every original attempt counter. No reopen or queue reconstruction is needed.
func TestClaimStorageRecoveryKeepsOwnerDuringReservePressure(t *testing.T) {
	cfg, _, entry, _, _ := signedClaimFixture(t, 70, 23)
	store, fixture := claimStorageTestOwner(t, t.Context(), cfg)
	entry.Attempts, entry.ReconcileAttempts = 7, 11
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	originalDirectory := store.directory
	runs, waits := 0, 0
	err = runClaimOwner(fixture.Context, cfg, store, claimOwnerHooks{
		run: func(owner *claimQueueStore) error {
			runs++
			if owner != store || owner.directory != originalDirectory {
				t.Fatal("resource pressure replaced the owner")
			}
			if runs == 1 {
				fixture.Host.SetReserve(0, 0)
				if retained, err := owner.load(); err != nil || !reflect.DeepEqual(retained, queue) {
					t.Fatalf("readback incorrectly required write reserve: %v", err)
				}
				return owner.save(queue)
			}
			return owner.save(queue)
		},
		wait: func(context.Context, time.Duration) bool {
			waits++
			if err := store.requireWrite(fixture.Context); !errors.Is(err, durablevolume.ErrUnavailable) {
				t.Fatalf("reserve transition was not causal: %v", err)
			}
			fixture.Host.SetReserve(1024*1024, 1024)
			return true
		},
	})
	after, readErr := os.ReadFile(store.path)
	if err != nil || readErr != nil || runs != 2 || waits != 1 || !bytes.Equal(before, after) {
		t.Fatalf("same-owner continuation changed retained work: runs=%d waits=%d err=%v read=%v", runs, waits, err, readErr)
	}
}

// A lost acknowledgement after the actual rename and directory sync is joined
// before reopening. The controller reloads exact signed bytes and lifetime
// counters while another owner remains open and usable.
func TestClaimStorageRecoveryReopensExactPendingAfterJoin(t *testing.T) {
	cfg, _, entry, _, _ := signedClaimFixture(t, 70, 23)
	store, fixture := claimStorageTestOwner(t, t.Context(), cfg)
	entry.Attempts, entry.ReconcileAttempts = 7, 11
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	raw, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	otherCfg := &ClaimDaemonConfig{StateDir: filepath.Join(t.TempDir(), "sibling")}
	other, _ := claimStorageTestOwner(t, t.Context(), otherCfg)
	otherQueue := &ClaimQueue{Schema: queue.Schema, LastDiscovered: -1, Entries: map[string]*ClaimQueueEntry{}}
	if err := other.save(otherQueue); err != nil {
		t.Fatal(err)
	}
	runs, waits, joined := 0, 0, false
	admission := &claimAdmission{}
	err = runClaimOwner(fixture.Context, cfg, store, claimOwnerHooks{
		run: func(owner *claimQueueStore) error {
			runs++
			if runs == 1 {
				defer func() { joined = true }()
				return owner.head.PublishWithHooks(raw, durablehead.PublicationHooks{AfterDirectorySync: func(*os.File) error { return syscall.EIO }})
			}
			if owner == store || !joined || owner.directory == nil {
				t.Fatal("pending continuation did not join and reopen its owner")
			}
			loaded, err := owner.load()
			if err != nil || !reflect.DeepEqual(loaded, queue) {
				t.Fatalf("pending continuation reset signed bytes or counters: %v %+v", err, loaded)
			}
			if err := admission.seedMember(cfg, owner); err != nil || admission.nonceMinimum() != 24 {
				t.Fatalf("retained pending signature did not seed nonce: %v", err)
			}
			return owner.save(loaded)
		},
		wait: func(context.Context, time.Duration) bool {
			waits++
			if !joined || store.directory != nil {
				t.Fatal("recovery preceded the old owner's joined close")
			}
			if err := other.save(otherQueue); err != nil {
				t.Fatalf("unrelated owner stopped during reconciliation: %v", err)
			}
			return true
		},
	})
	actual, readErr := os.ReadFile(store.path)
	if err != nil || readErr != nil || runs != 2 || waits != 1 || !bytes.Equal(actual, raw) {
		t.Fatalf("pending recovery changed original bytes: runs=%d waits=%d err=%v read=%v", runs, waits, err, readErr)
	}
}

// Repeated uncertain publications have a finite controller budget. Exhaustion
// retains the last pending bytes for a separately reviewed continuation.
func TestClaimStorageRecoveryBoundsUncertainGenerations(t *testing.T) {
	cfg := &ClaimDaemonConfig{StateDir: filepath.Join(t.TempDir(), "claims")}
	store, fixture := claimStorageTestOwner(t, t.Context(), cfg)
	runs, waits := 0, 0
	err := runClaimOwner(fixture.Context, cfg, store, claimOwnerHooks{
		run: func(owner *claimQueueStore) error {
			runs++
			queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: int64(runs), Entries: map[string]*ClaimQueueEntry{}}
			raw, err := json.Marshal(queue)
			if err != nil {
				return err
			}
			return owner.head.PublishWithHooks(raw, durablehead.PublicationHooks{AfterDirectorySync: func(*os.File) error { return syscall.EIO }})
		},
		wait: func(context.Context, time.Duration) bool { waits++; return true },
	})
	if !errors.Is(err, durablehead.ErrUncertain) || !strings.Contains(err.Error(), "budget exhausted") || runs != claimStorageReconciliations+1 || waits != claimStorageReconciliations {
		t.Fatalf("uncertain recovery escaped finite bound: runs=%d waits=%d err=%v", runs, waits, err)
	}
	raw, readErr := os.ReadFile(store.path)
	var retained ClaimQueue
	if readErr != nil || json.Unmarshal(raw, &retained) != nil || retained.LastDiscovered != int64(runs) {
		t.Fatalf("exhaustion removed pending actual bytes: %v", readErr)
	}
}

// Public production entrypoints refuse a missing reference before opening even
// the deliberately absent config, key, listener or state directory.
func TestClaimStoragePublicEntrypointsRequireDeclaration(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-volume", "claim.yml")
	for name, run := range map[string]func(context.Context, string) error{"daemon": RunClaimDaemon, "swarm": RunClaimSwarm} {
		err := run(t.Context(), missing)
		if err == nil || !strings.Contains(err.Error(), "durable-volume declaration") {
			t.Fatalf("%s reached config or host effects without declaration: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("public admission created missing state: %v", err)
	}
}

// A real finalized-state Rpc boundary changes only available reserve. Replay
// must retain its original raw bytes and refuse sending until the SAME owner
// regains reserve; a successful retry sends that exact transaction once.
func TestClaimStorageReplayRechecksAfterFinalizedRpc(t *testing.T) {
	rpc := newClaimClockTestRPC(t, nil)
	store, fixture := claimStorageTestOwner(t, t.Context(), rpc.cfg)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": rpc.entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	tx, _, from, err := authenticateSignedClaim(rpc.cfg, rpc.entry)
	if err != nil {
		t.Fatal(err)
	}
	rpc.stateLock.Lock()
	rpc.reply = func(_ context.Context, method string, _ []json.RawMessage, result any) (any, error) {
		if method == "eth_call" {
			fixture.Host.SetReserve(0, 0)
		}
		return result, nil
	}
	rpc.stateLock.Unlock()
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := rebroadcastSignedClaim(t.Context(), rpc.cfg, tx, from, store)
	_, _, _, sends := rpc.evidence()
	after, readErr := os.ReadFile(store.path)
	if consumed || !errors.Is(err, durablevolume.ErrUnavailable) || len(sends) != 0 || readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("storage refusal did not fence exact replay: consumed=%t err=%v sends=%v read=%v", consumed, err, sends, readErr)
	}
	rpc.stateLock.Lock()
	rpc.reply = nil
	rpc.stateLock.Unlock()
	fixture.Host.SetReserve(1024*1024, 1024)
	consumed, err = rebroadcastSignedClaim(t.Context(), rpc.cfg, tx, from, store)
	_, _, _, sends = rpc.evidence()
	if consumed || err != nil || len(sends) != 1 || sends[0] != rpc.entry.RawTxHex {
		t.Fatalf("same-owner recovery did not replay original bytes: consumed=%t err=%v sends=%v", consumed, err, sends)
	}
}

// A lost post-sync acknowledgement leaves an exact signed liability in the
// pending head. Shared admission must retain its nonce before a sibling runs,
// even though this worker has not yet reconciled the checkpoint acknowledgement.
func TestClaimStoragePreparedLostAckRetainsNonceFloor(t *testing.T) {
	rpc := newClaimClockTestRPC(t, nil)
	store, fixture := claimStorageTestOwner(t, t.Context(), rpc.cfg)
	entry := &ClaimQueueEntry{Epoch: 70, Status: "submitting", Attempts: 3}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	store.afterSync = func(*os.File) error { return syscall.EIO }
	admission := &claimAdmission{}
	api := claimApiFunction(func(context.Context, *sdk.SnPoolClaimArgs) (*sdk.SnPoolClaimResult, error) { return rpc.claim, nil })
	err := submitClaimDirect(t.Context(), rpc.cfg, api, entry, store, queue, admission)
	_, _, _, sends := rpc.evidence()
	if !errors.Is(err, durablehead.ErrUncertain) || len(sends) != 0 || admission.nonceMinimum() != 24 {
		t.Fatalf("uncertain prepared bytes lost nonce custody: err=%v sends=%v floor=%d", err, sends, admission.nonceMinimum())
	}
	pendingRaw, err := os.ReadFile(store.path)
	var pending ClaimQueue
	if err != nil || json.Unmarshal(pendingRaw, &pending) != nil || pending.Entries["70"] == nil {
		t.Fatalf("post-sync fixture has no real pending signed queue: %v", err)
	}
	tx, intent, _, err := authenticateSignedClaim(rpc.cfg, pending.Entries["70"])
	if err != nil || tx.Nonce() != 23 || intent.E.Int64() != 70 {
		t.Fatalf("pending bytes do not authenticate the actual generated intent: %v", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newClaimQueueStore(rpc.cfg.StateDir, fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	loaded, err := reopened.load()
	actualRaw, readErr := os.ReadFile(store.path)
	if err != nil || readErr != nil || !bytes.Equal(actualRaw, pendingRaw) || loaded.Entries["70"].RawTxHex != pending.Entries["70"].RawTxHex || loaded.Entries["70"].Attempts != 3 {
		t.Fatalf("pending checkpoint did not retain the original signed intent: %v %+v", err, loaded)
	}
}

// Actual daemon workers join their own Api tree. Losing one acknowledged queue
// stops that member, while another member reaches its independent Api request
// and retains its own original queue without a campaign cancellation/reset.
func TestClaimStorageSwarmLossDoesNotCancelSibling(t *testing.T) {
	var badPath string
	var lostOnce sync.Once
	badConfig, badCfg, _ := claimQueueOwnerDaemonFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		lostOnce.Do(func() {
			if err := os.Remove(badPath); err != nil {
				t.Error(err)
			}
		})
		_, _ = writer.Write([]byte(`{"epoch":71}`))
	})
	badPath = filepath.Join(badCfg.StateDir, "claim-queue.json")
	healthyRead := make(chan struct{})
	var healthyOnce sync.Once
	healthyConfig, healthyCfg, _ := claimQueueOwnerDaemonFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		healthyOnce.Do(func() { close(healthyRead) })
		_, _ = writer.Write([]byte(`{"epoch":0}`))
	})
	healthyCfg.KeyFile, healthyCfg.RPC = badCfg.KeyFile, badCfg.RPC
	raw, err := yaml.Marshal(healthyCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(healthyConfig, raw, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	swarm, err := NewClaimSwarm(&ClaimSwarmConfig{Schema: ClaimSwarmSchema, ListenAddress: address, Members: []ClaimSwarmMember{{ID: "bad", ConfigPath: badConfig}, {ID: "healthy", ConfigPath: healthyConfig}}})
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := claimSwarmTestContext(t, parent, swarm)
	type result struct {
		id              string
		err, contextErr error
	}
	stopped := make(chan result, 2)
	done := make(chan error, 1)
	go func() {
		done <- swarm.run(ctx, func(id string, err error, current context.Context) {
			stopped <- result{id: id, err: err, contextErr: current.Err()}
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case failed := <-stopped:
		if failed.id != "bad" || !errors.Is(failed.err, durablevolume.ErrIdentity) || failed.contextErr != nil {
			t.Fatalf("member loss reset the shared campaign: %+v", failed)
		}
	case <-t.Context().Done():
		t.Fatal("lost queue did not stop its actual worker")
	}
	select {
	case <-healthyRead:
	case <-t.Context().Done():
		t.Fatal("unrelated member did not continue after joined loss")
	}
	status := swarm.status()
	if status.Running != 1 || len(status.Failures) != 1 || status.Failures["bad"] == "" {
		t.Fatalf("affected member failure was not isolated and observable: %+v", status)
	}
	if _, err := os.Lstat(badPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing signed custody was recreated: %v", err)
	}
}
