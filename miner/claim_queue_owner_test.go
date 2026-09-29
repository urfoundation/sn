// Queue ownership regressions exercise the real store and daemon/swarm startup.
// No fixture supplies transaction submission authority or contacts a live route.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Existing store tests retain explicit ownership until their test work ends.
func newClaimQueueTestStore(t *testing.T, directory string) *claimQueueStore {
	t.Helper()
	store, err := newClaimQueueStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

// Daemon fixtures use generated signing material but only offline custody and
// a local request counter. Any unexpected request is observable and refused.
func claimQueueOwnerDaemonFixture(t *testing.T, handler ...http.HandlerFunc) (string, *ClaimDaemonConfig, *atomic.Int64) {
	t.Helper()
	cfg, _, _, _, _ := signedClaimFixture(t, 70, 23)
	requests := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if len(handler) != 0 {
			handler[0](writer, request)
			return
		}
		http.Error(writer, "synthetic ownership fixture refuses network work", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	cfg.SchemaVersion, cfg.Release, cfg.PollSeconds, cfg.LookbackEpochs = 1, "1.0", 5, 2
	cfg.APIURL, cfg.RPC = server.URL, []string{server.URL}
	cfg.JWTFile = filepath.Join(t.TempDir(), "synthetic.jwt")
	if err := os.WriteFile(cfg.JWTFile, []byte("synthetic-ownership-jwt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "claim.yml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, cfg, requests
}

// A second open cannot lose signed bytes, and releasing the first owner allows
// the unchanged on-disk format to reconstruct the exact original nonce floor.
func TestClaimQueueOwnerRejectsDuplicateAndAllowsClosedRestart(t *testing.T) {
	cfg, _, entry, _, _ := signedClaimFixture(t, 70, 23)
	store := newClaimQueueTestStore(t, cfg.StateDir)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := newClaimQueueStore(cfg.StateDir)
	if duplicate != nil {
		_ = duplicate.close()
	}
	if err == nil || !strings.Contains(err.Error(), "already has an owner") {
		t.Fatalf("second queue owner was not refused: %v", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	restarted := newClaimQueueTestStore(t, cfg.StateDir)
	loaded, err := restarted.load()
	if err != nil || loaded.Entries["70"].TxHash != entry.TxHash || loaded.Entries["70"].RawTxHex != entry.RawTxHex {
		t.Fatalf("restart changed signed custody: %v, %+v", err, loaded)
	}
	admission := &claimAdmission{}
	if err := admission.seedMember(cfg, restarted); err != nil || admission.nonceMinimum() != 24 {
		t.Fatalf("retained owner did not reconstruct nonce custody: floor=%d error=%v", admission.nonceMinimum(), err)
	}
	if _, err := store.load(); err == nil {
		t.Fatal("closed owner read retained custody")
	}
	if err := store.save(queue); err == nil {
		t.Fatal("closed owner rewrote retained custody")
	}
	after, err := os.ReadFile(restarted.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("ownership admission or closed owner changed queue bytes: %v", err)
	}
}

// Flock must protect independent processes, not just a process-local map.
func TestClaimQueueOwnerExcludesAnotherProcess(t *testing.T) {
	store := newClaimQueueTestStore(t, filepath.Join(t.TempDir(), "claims"))
	for _, action := range []string{"refuse", "acquire"} {
		if action == "acquire" {
			if err := store.close(); err != nil {
				t.Fatal(err)
			}
		}
		command := exec.Command(os.Args[0], "-test.run=^TestClaimQueueOwnerChildProcess$", "-test.count=1")
		command.Env = append(os.Environ(), "URNETWORK_TEST_CLAIM_QUEUE_OWNER_ACTION="+action, "URNETWORK_TEST_CLAIM_QUEUE_OWNER_DIRECTORY="+filepath.Dir(store.path))
		output, err := command.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "synthetic queue child "+action) {
			t.Fatalf("independent process %s: %v\n%s", action, err, output)
		}
	}
}

// The child executes only for the parent fixture's synthetic temporary path.
func TestClaimQueueOwnerChildProcess(t *testing.T) {
	action := os.Getenv("URNETWORK_TEST_CLAIM_QUEUE_OWNER_ACTION")
	if action == "" {
		return
	}
	store, err := newClaimQueueStore(os.Getenv("URNETWORK_TEST_CLAIM_QUEUE_OWNER_DIRECTORY"))
	if store != nil {
		defer store.close()
	}
	switch action {
	case "refuse":
		if err == nil || !strings.Contains(err.Error(), "already has an owner") {
			t.Fatalf("child acquired an already owned queue: %v", err)
		}
	case "acquire":
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("invalid synthetic child action")
	}
	fmt.Println("synthetic queue child", action)
}

// An existing alias and a new child under an aliased parent lock the same
// physical directory. Changing the spelling cannot create another writer.
func TestClaimQueueOwnerPinsPhysicalAliases(t *testing.T) {
	physical := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	store := newClaimQueueTestStore(t, filepath.Join(alias, "claims"))
	for _, directory := range []string{filepath.Join(alias, "claims"), filepath.Join(physical, "claims")} {
		duplicate, err := newClaimQueueStore(directory)
		if duplicate != nil {
			_ = duplicate.close()
		}
		if err == nil || !strings.Contains(err.Error(), "already has an owner") {
			t.Fatalf("alias admitted a second owner: %v", err)
		}
	}
	if filepath.Dir(store.path) != filepath.Join(physical, "claims") {
		t.Fatalf("owner did not retain the physical namespace: %s", store.path)
	}
}

// A replaced path cannot send an old owner's writes into a new queue. The
// directory descriptor remains locked even after its pathname moves.
func TestClaimQueueOwnerRejectsReplacedDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "claims")
	store := newClaimQueueTestStore(t, directory)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: -1, Entries: map[string]*ClaimQueueEntry{}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	retained := directory + "-retained"
	if err := os.Rename(directory, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("synthetic replacement queue must stay untouched")
	if err := os.WriteFile(store.path, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("old owner read replacement directory: %v", err)
	}
	if err := store.save(queue); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("old owner wrote replacement directory: %v", err)
	}
	duplicate, err := newClaimQueueStore(retained)
	if duplicate != nil {
		_ = duplicate.close()
	}
	if err == nil || !strings.Contains(err.Error(), "already has an owner") {
		t.Fatalf("moving a directory released its live owner: %v", err)
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(after, sentinel) {
		t.Fatalf("replacement queue was overwritten: %v", err)
	}
}

// Even a new alias back to the same inode is a changed admitted namespace.
// It cannot silently become the path used by an already running daemon.
func TestClaimQueueOwnerRejectsChangedDirectoryAlias(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "claims")
	store := newClaimQueueTestStore(t, directory)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: -1, Entries: map[string]*ClaimQueueEntry{}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	retained := directory + "-retained"
	if err := os.Rename(directory, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(retained, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("changed directory alias supplied custody: %v", err)
	}
	if err := store.save(queue); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("changed directory alias accepted publication: %v", err)
	}
}

// Descriptor-relative I/O is the backstop if a directory moves after a store
// has checked its path. It cannot read or replace the new pathname's queue.
func TestClaimQueueOwnerIoStaysWithinPinnedDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "claims")
	store := newClaimQueueTestStore(t, directory)
	retained := directory + "-retained"
	if err := os.Rename(directory, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("synthetic replacement queue must stay untouched")
	if err := os.WriteFile(store.path, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	intended := []byte("synthetic original queue publication")
	if err := claimQueuePublish(store.directory, "claim-queue.json", intended); err != nil {
		t.Fatal(err)
	}
	read, _, err := claimQueueReadFile(store.directory, "claim-queue.json")
	if err != nil || !bytes.Equal(read, intended) {
		t.Fatalf("read escaped the held directory: %v", err)
	}
	for _, expected := range []struct {
		path string
		raw  []byte
	}{{path: store.path, raw: replacement}, {path: filepath.Join(retained, "claim-queue.json"), raw: intended}} {
		after, err := os.ReadFile(expected.path)
		if err != nil || !bytes.Equal(after, expected.raw) {
			t.Fatalf("publication escaped the held directory: %v", err)
		}
	}
}

// Admission reads existing v1 records without reformatting, migration or an
// inferred success; submitting remains uncertain only in the loaded snapshot.
func TestClaimQueueOwnerPreservesLegacyBytesDuringAdmission(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "claims")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(" { \"schema\": \"urnetwork-provider-claim-queue-v1\", \"last_discovered\": 3, \"entries\": { \"3\": {\"epoch\":3,\"status\":\"submitting\",\"attempts\":1,\"updated_at\":\"2026-01-01T00:00:00Z\",\"tx_hash\":\"synthetic retained hash\",\"raw_tx_hex\":\"synthetic retained raw\"} } }\n")
	path := filepath.Join(directory, "claim-queue.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store := newClaimQueueTestStore(t, directory)
	loaded, err := store.load()
	if err != nil || loaded.Entries["3"].Status != "uncertain" || loaded.Entries["3"].TxHash != "synthetic retained hash" || loaded.Entries["3"].RawTxHex != "synthetic retained raw" {
		t.Fatalf("legacy custody changed at admission: %v, %+v", err, loaded)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("opening queue migrated existing bytes: %v", err)
	}
}

// A bare path is not custody authority, and a held owner never imports a
// symlink's external contents while reconstructing the nonce floor.
func TestClaimQueueOwnerRejectsUnownedAndUnsafeReads(t *testing.T) {
	store := newClaimQueueTestStore(t, filepath.Join(t.TempDir(), "claims"))
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: -1, Entries: map[string]*ClaimQueueEntry{}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	unowned := &claimQueueStore{path: store.path}
	if _, err := unowned.load(); err == nil {
		t.Fatal("a path without a directory lock read custody")
	}
	if err := unowned.save(queue); err == nil {
		t.Fatal("a path without a directory lock wrote custody")
	}
	external := filepath.Join(t.TempDir(), "external.json")
	raw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, store.path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); !errors.Is(err, errClaimQueueUnsafeFile) {
		t.Fatalf("external symlink supplied queue custody: %v", err)
	}
	after, err := os.ReadFile(external)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("external queue was altered: %v", err)
	}
}

// Duplicate command startup must fail at ownership, before any signed state,
// API readiness, route request or queue publication can be consumed or changed.
func TestClaimDaemonRejectsDuplicateOwnerBeforeNetwork(t *testing.T) {
	configPath, cfg, requests := claimQueueOwnerDaemonFixture(t)
	store := newClaimQueueTestStore(t, cfg.StateDir)
	retained := []byte("synthetic retained custody that a second process must not parse")
	if err := os.WriteFile(store.path, retained, 0o600); err != nil {
		t.Fatal(err)
	}
	ready := false
	admission := &claimAdmission{}
	err := runClaimDaemonWithAdmission(context.Background(), configPath, admission, 0, func() { ready = true })
	if err == nil || !strings.Contains(err.Error(), "already has an owner") {
		t.Fatalf("duplicate daemon passed the ownership boundary: %v", err)
	}
	if ready || requests.Load() != 0 || admission.nonceMinimum() != 0 {
		t.Fatalf("duplicate daemon consumed custody or started work: ready=%t requests=%d nonce=%d", ready, requests.Load(), admission.nonceMinimum())
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(retained, after) {
		t.Fatalf("duplicate daemon changed retained custody: %v", err)
	}
}

// The daemon owns the queue during readiness and releases it only after its
// normal canceled return has joined all package-owned API/epoch work.
func TestClaimDaemonRetainsOwnerUntilJoinedShutdown(t *testing.T) {
	configPath, cfg, requests := claimQueueOwnerDaemonFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := false
	var duplicateErr error
	err := runClaimDaemonWithAdmission(ctx, configPath, &claimAdmission{}, time.Hour, func() {
		ready = true
		var duplicate *claimQueueStore
		duplicate, duplicateErr = newClaimQueueStore(cfg.StateDir)
		if duplicate != nil {
			_ = duplicate.close()
		}
		cancel()
	})
	if err != nil || !ready || duplicateErr == nil || !strings.Contains(duplicateErr.Error(), "already has an owner") {
		t.Fatalf("daemon owner lifetime: ready=%t duplicate=%v exit=%v", ready, duplicateErr, err)
	}
	if requests.Load() != 0 {
		t.Fatalf("canceled pre-poll daemon reached a route: %d", requests.Load())
	}
	restarted := newClaimQueueTestStore(t, cfg.StateDir)
	if _, err := restarted.load(); err != nil {
		t.Fatal(err)
	}
}

// Failures before and after queue initialization both release their descriptor;
// malformed retained bytes are not rewritten to make a restart succeed.
func TestClaimDaemonReleasesOwnerOnStartupFailure(t *testing.T) {
	for _, failure := range []string{"queue", "jwt"} {
		configPath, cfg, requests := claimQueueOwnerDaemonFixture(t)
		if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if failure == "queue" {
			if err := os.WriteFile(filepath.Join(cfg.StateDir, "claim-queue.json"), []byte("synthetic malformed custody"), 0o600); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(cfg.JWTFile, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		ready := false
		if err := runClaimDaemonWithAdmission(context.Background(), configPath, &claimAdmission{}, 0, func() { ready = true }); err == nil {
			t.Fatalf("%s startup failure was ignored", failure)
		}
		if ready || requests.Load() != 0 {
			t.Fatalf("%s startup failure reached network work: ready=%t requests=%d", failure, ready, requests.Load())
		}
		reopened := newClaimQueueTestStore(t, cfg.StateDir)
		if failure == "queue" {
			raw, err := os.ReadFile(reopened.path)
			if err != nil || string(raw) != "synthetic malformed custody" {
				t.Fatalf("failed startup rewrote malformed custody: %v", err)
			}
		}
	}
}

// The swarm must acquire every member before seeding even its first queue.
// A later conflict cannot partially start siblings or rewrite any custody.
func TestClaimSwarmAcquiresAllQueueOwnersBeforeCustodyOrNetwork(t *testing.T) {
	firstPath, firstCfg, firstRequests := claimQueueOwnerDaemonFixture(t)
	secondPath, secondCfg, secondRequests := claimQueueOwnerDaemonFixture(t)
	secondCfg.KeyFile, secondCfg.RPC = firstCfg.KeyFile, append([]string(nil), firstCfg.RPC...)
	raw, err := yaml.Marshal(secondCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(firstCfg.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	firstQueue := filepath.Join(firstCfg.StateDir, "claim-queue.json")
	retained := []byte("synthetic first custody must not be parsed before every owner is acquired")
	if err := os.WriteFile(firstQueue, retained, 0o600); err != nil {
		t.Fatal(err)
	}
	secondOwner := newClaimQueueTestStore(t, secondCfg.StateDir)
	secondRetained := []byte("synthetic second custody belongs to the existing owner")
	if err := os.WriteFile(secondOwner.path, secondRetained, 0o600); err != nil {
		t.Fatal(err)
	}
	swarm, err := NewClaimSwarm(&ClaimSwarmConfig{Schema: ClaimSwarmSchema, ListenAddress: "127.0.0.1:22081", Members: []ClaimSwarmMember{
		{ID: "second", ConfigPath: secondPath}, {ID: "first", ConfigPath: firstPath},
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = swarm.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "second queue ownership") || !strings.Contains(err.Error(), "already has an owner") {
		t.Fatalf("swarm consumed custody before all directory locks: %v", err)
	}
	if firstRequests.Load() != 0 || secondRequests.Load() != 0 || swarm.status().Running != 0 {
		t.Fatalf("partially admitted swarm started network workers: %+v", swarm.status())
	}
	firstOwner := newClaimQueueTestStore(t, firstCfg.StateDir)
	for _, expected := range []struct {
		path string
		raw  []byte
	}{{path: firstOwner.path, raw: retained}, {path: secondOwner.path, raw: secondRetained}} {
		after, err := os.ReadFile(expected.path)
		if err != nil || !bytes.Equal(after, expected.raw) {
			t.Fatalf("partial swarm admission changed custody: %v", err)
		}
	}
	duplicate, err := newClaimQueueStore(secondCfg.StateDir)
	if duplicate != nil {
		_ = duplicate.close()
	}
	if err == nil || !strings.Contains(err.Error(), "already has an owner") {
		t.Fatalf("partial admission released another owner's lock: %v", err)
	}
}

// Swarm seed failure releases every acquired directory before any member can
// start. This also exercises retained config/store ownership in the caller.
func TestClaimSwarmReleasesAllOwnersOnCustodyFailure(t *testing.T) {
	configPath, cfg, requests := claimQueueOwnerDaemonFixture(t)
	secondPath, secondCfg, secondRequests := claimQueueOwnerDaemonFixture(t)
	secondCfg.KeyFile, secondCfg.RPC = cfg.KeyFile, append([]string(nil), cfg.RPC...)
	secondRaw, err := yaml.Marshal(secondCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, secondRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(&ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 3, Entries: map[string]*ClaimQueueEntry{"3": nil}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.StateDir, "claim-queue.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	swarm, err := NewClaimSwarm(&ClaimSwarmConfig{Schema: ClaimSwarmSchema, ListenAddress: "127.0.0.1:22081", Members: []ClaimSwarmMember{{ID: "first", ConfigPath: configPath}, {ID: "second", ConfigPath: secondPath}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := swarm.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "seed relayer nonce custody") {
		t.Fatalf("swarm accepted invalid custody: %v", err)
	}
	if requests.Load() != 0 || secondRequests.Load() != 0 || swarm.status().Running != 0 {
		t.Fatal("invalid swarm custody reached network work")
	}
	reopened := newClaimQueueTestStore(t, cfg.StateDir)
	after, err := os.ReadFile(reopened.path)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("failed swarm changed retained bytes: %v", err)
	}
	secondOwner := newClaimQueueTestStore(t, secondCfg.StateDir)
	if _, err := os.Stat(secondOwner.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed custody admission published another member's queue: %v", err)
	}
}

// A real member reaches a blocked epoch read with its pre-acquired store.
// Cancellation joins the member before the swarm releases that directory.
func TestClaimSwarmRetainsOwnerThroughActiveMemberShutdown(t *testing.T) {
	entered := make(chan struct{}, 1)
	configPath, cfg, requests := claimQueueOwnerDaemonFixture(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/sn/epoch" {
			t.Errorf("unexpected synthetic claim request: %s %s", request.Method, request.URL.Path)
			http.NotFound(writer, request)
			return
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-request.Context().Done()
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	swarm, err := NewClaimSwarm(&ClaimSwarmConfig{Schema: ClaimSwarmSchema, ListenAddress: listener.Addr().String(), Members: []ClaimSwarmMember{{ID: "synthetic", ConfigPath: configPath}}})
	if closeErr := listener.Close(); err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- swarm.Run(ctx) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("swarm exited before the real epoch read: %v", err)
	}
	duplicate, duplicateErr := newClaimQueueStore(cfg.StateDir)
	if duplicate != nil {
		_ = duplicate.close()
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if duplicateErr == nil || !strings.Contains(duplicateErr.Error(), "already has an owner") {
		t.Fatalf("active swarm member had no directory owner: %v", duplicateErr)
	}
	if requests.Load() < 1 {
		t.Fatal("swarm shutdown bypassed the real epoch reader")
	}
	reopened := newClaimQueueTestStore(t, cfg.StateDir)
	queue, err := reopened.load()
	if err != nil || len(queue.Entries) != 0 || queue.LastDiscovered != -1 {
		t.Fatalf("canceled read fabricated claim work: %v, %+v", err, queue)
	}
}
