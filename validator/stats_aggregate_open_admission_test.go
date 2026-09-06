//go:build linux || darwin

package validator

// Open-admission controls use a fully signed M8 aggregate and observe actual
// filesystem effects before namespace refusal. No writer/verifier is replaced;
// bounded physical snapshots and existing storage hooks are test-only owners.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

// Only public backend metadata and content digests are retained. FileInfo is
// used for descriptor identity, never access-time comparisons or key contents.
type statsAggregateOpenTestFile struct {
	name string
	info os.FileInfo
	hash [32]byte
}

// The physical census is independently limited to the unchanged local fixture
// storage bounds; no provider-, record- or generation-history map is introduced.
type statsAggregateOpenTestSnapshot struct {
	directory os.FileInfo
	files     []statsAggregateOpenTestFile
}

// Reads one no-follow descriptor at a time, hashing rather than copying data.
// A rejected open must not replace names/inodes or change any existing bytes.
func snapshotStatsAggregateOpenTestStore(t *testing.T, fixture *statsAggregateTestFixture) statsAggregateOpenTestSnapshot {
	t.Helper()
	if fixture.bounds != statsAggregateTestBounds() {
		t.Fatal("physical snapshot fixture bounds changed")
	}
	directoryInfo, err := os.Lstat(fixture.path)
	if err != nil || !attemptStorePrivateDirectory(directoryInfo) {
		t.Fatalf("physical snapshot directory: %v", err)
	}
	root, err := os.OpenRoot(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("physical snapshot root close: %v", err)
		}
	}()
	directory, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	openedDirectory, err := directory.Stat()
	if err != nil || !os.SameFile(directoryInfo, openedDirectory) {
		directory.Close()
		t.Fatalf("physical snapshot changed directory descriptor: %v", err)
	}
	var names []string
	for {
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			names = append(names, entry.Name())
			if uint64(len(names)) > fixture.bounds.MaxStorageFiles {
				directory.Close()
				t.Fatal("physical snapshot exceeds the unchanged file bound")
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			directory.Close()
			t.Fatal(readErr)
		}
	}
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	result := statsAggregateOpenTestSnapshot{directory: directoryInfo}
	var total uint64
	for _, name := range names {
		info, err := root.Lstat(name)
		if err != nil || !attemptStorePrivateFile(info) || info.Size() < 0 || uint64(info.Size()) > fixture.bounds.MaxStorageBytes-total {
			t.Fatalf("physical snapshot refuses file %q or its bound: %v", name, err)
		}
		file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := file.Stat()
		if err != nil || !attemptStorePrivateFile(opened) || !os.SameFile(info, opened) {
			file.Close()
			t.Fatalf("physical snapshot changed file descriptor %q: %v", name, err)
		}
		digest := sha256.New()
		size, readErr := io.Copy(digest, io.LimitReader(file, int64(fixture.bounds.MaxStorageBytes-total)+1))
		after, statErr := file.Stat()
		closeErr := file.Close()
		current, currentErr := root.Lstat(name)
		if err := errors.Join(readErr, statErr, closeErr, currentErr); err != nil || size != info.Size() || size != after.Size() || !os.SameFile(info, after) || !os.SameFile(info, current) || info.Mode() != current.Mode() || !info.ModTime().Equal(current.ModTime()) {
			t.Fatalf("physical snapshot file changed while hashing %q: %v", name, err)
		}
		total += uint64(size)
		var hash [32]byte
		copy(hash[:], digest.Sum(nil))
		result.files = append(result.files, statsAggregateOpenTestFile{name: name, info: info, hash: hash})
	}
	current, err := os.Lstat(fixture.path)
	if err != nil || !os.SameFile(directoryInfo, current) || directoryInfo.Mode() != current.Mode() || !directoryInfo.ModTime().Equal(current.ModTime()) {
		t.Fatalf("physical snapshot namespace changed during read: %v", err)
	}
	return result
}

// The bounded test-only comparison reports names/metadata/digests, not data.
// Same logical head is deliberately not used as the no-mutation oracle.
func (self statsAggregateOpenTestSnapshot) changes(next statsAggregateOpenTestSnapshot) []string {
	var changes []string
	if !os.SameFile(self.directory, next.directory) || self.directory.Mode() != next.directory.Mode() || !self.directory.ModTime().Equal(next.directory.ModTime()) {
		changes = append(changes, "directory-identity-or-metadata")
	}
	previousKVs := make(map[string]statsAggregateOpenTestFile, len(self.files))
	for _, file := range self.files {
		previousKVs[file.name] = file
	}
	for _, file := range next.files {
		prior, exists := previousKVs[file.name]
		if !exists {
			changes = append(changes, "created:"+file.name)
		} else if !os.SameFile(prior.info, file.info) || prior.info.Mode() != file.info.Mode() || prior.info.Size() != file.info.Size() || !prior.info.ModTime().Equal(file.info.ModTime()) || prior.hash != file.hash {
			changes = append(changes, "changed:"+file.name)
		}
		delete(previousKVs, file.name)
	}
	for name := range previousKVs {
		changes = append(changes, "removed:"+name)
	}
	sort.Strings(changes)
	return changes
}

// Existing completed-operation hooks may run on backend workers. Their bounded
// copied metadata is released only after Open's failed owner has fully joined.
type statsAggregateOpenTestObserver struct {
	stateLock sync.Mutex
	events    []string
}

// Records completed mutations/synchronization, not just attempted API calls.
// Zero-length physical writes remain distinguishable through the byte snapshot.
func (self *statsAggregateOpenTestObserver) step(operation, name string) error {
	switch operation {
	case "after-create", "after-write", "after-set-meta", "after-directory-sync", "after-file-sync":
	default:
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.events) == 256 {
		return errors.New("open observation exceeded its fixed metadata bound")
	}
	self.events = append(self.events, operation+":"+name)
	return nil
}

// Returns a detached event census after the owner joins.
func (self *statsAggregateOpenTestObserver) snapshot() []string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]string(nil), self.events...)
}

// Every refusing-open test begins with real full admission of one completed
// and one failed M8 trail, then a closed owner and an actual nonempty WAL.
func newStatsAggregateOpenTestFixture(t *testing.T) (*statsAggregateTestFixture, statsAggregateHead) {
	t.Helper()
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{})
	if err != nil || fixture.source.policy.Verify.TrailDepth != 8 || fixture.cut.RecordCount != 10 || fixture.cut.CompleteCount != 1 || fixture.cut.FailedCount != 1 {
		t.Fatalf("full M8 complete/failed authority: %+v error=%v", head, err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	physical := snapshotStatsAggregateOpenTestStore(t, fixture)
	nonemptyWAL, current := false, false
	for _, file := range physical.files {
		nonemptyWAL = nonemptyWAL || strings.HasSuffix(file.name, ".log") && file.info.Size() > 0
		current = current || file.name == "CURRENT"
	}
	if !nonemptyWAL || !current {
		t.Fatal("full admitted fixture has no actual recoverable WAL and CURRENT")
	}
	return fixture, head
}

// The original owner remains semantically usable after a refusal. This is a
// separate full-census control, never the physical no-mutation assertion.
func assertStatsAggregateOpenTestPriorAuthority(t *testing.T, fixture *statsAggregateTestFixture, prior statsAggregateHead) {
	t.Helper()
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	head, err := store.Head(context.Background())
	if err != nil || head != prior {
		t.Fatalf("refused Open changed original authority or leaked its owner: %v", err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

// Every substituted input is internally valid and policy-bound. The required
// refusal must still precede writable recovery of this other namespace.
func observeStatsAggregateOpenTestRefusal(t *testing.T, fixture *statsAggregateTestFixture, variant string, expected AttemptCutV2Context, config StatsConfig) bool {
	t.Helper()
	if err := statsAggregatePolicyConfig(expected, fixture.source.policy, config.withDefaults()); err != nil {
		t.Fatalf("%s is not a valid alternate namespace/config: %v", variant, err)
	}
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	expectedBefore, configBefore := expected, config
	observer := &statsAggregateOpenTestObserver{}
	store, err := openStatsAggregateStore(context.Background(), fixture.path, expected, fixture.source.policy, config, fixture.bounds, statsAggregateHooks{StorageStep: observer.step})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || err == nil || !strings.Contains(err.Error(), "existing namespace or config differs") || expected != expectedBefore || config != configBefore {
		t.Fatalf("%s did not reach the exact valid-input namespace refusal: store=%v error=%v", variant, store != nil, err)
	}
	changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture))
	events := observer.snapshot()
	t.Logf("open_refusal variant=%s physical_changes=%v completed_operations=%v", variant, changes, events)
	return len(changes) != 0 || len(events) != 0
}

// Coherent alternate chain/deployment/activation domains are rejected before
// their expected owner can rewrite any file of the existing full M8 aggregate.
func TestStatsAggregateWrongDomainOpenPreservesPhysicalState(t *testing.T) {
	t.Parallel()
	fixture, prior := newStatsAggregateOpenTestFixture(t)
	var changed []string
	variants := []struct {
		name   string
		change func(*AttemptCutV2Context)
	}{
		{name: "chain", change: func(expected *AttemptCutV2Context) { expected.Identity.ChainID++; expected.Activation.Domain.ChainID++ }},
		{name: "genesis", change: func(expected *AttemptCutV2Context) {
			expected.Activation.Domain.GenesisHash[0] ^= 1
			expected.Identity.GenesisHash = attemptHex32(expected.Activation.Domain.GenesisHash)
		}},
		{name: "netuid", change: func(expected *AttemptCutV2Context) { expected.Identity.Netuid++; expected.Activation.Domain.Netuid++ }},
		{name: "deployment", change: func(expected *AttemptCutV2Context) {
			expected.Identity.DeploymentID += "-other"
			expected.Activation.Domain.DeploymentIDHash = sha256.Sum256([]byte(expected.Identity.DeploymentID))
		}},
		{name: "coordinator", change: func(expected *AttemptCutV2Context) { expected.Activation.Domain.Coordinator[0] ^= 128 }},
		{name: "vault", change: func(expected *AttemptCutV2Context) { expected.Activation.Domain.SettlementVault[0] ^= 128 }},
		{name: "activation-hash", change: func(expected *AttemptCutV2Context) { expected.Activation.Domain.ActivationHash[0] ^= 128 }},
	}
	for _, variant := range variants {
		expected := fixture.source.expected
		variant.change(&expected)
		if observeStatsAggregateOpenTestRefusal(t, fixture, variant.name, expected, fixture.config) {
			changed = append(changed, variant.name)
		}
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, prior)
	if len(changed) != 0 {
		t.Fatalf("wrong-domain aggregate Open mutated physical storage before refusal: %v", changed)
	}
}

// Operator, VPK, validator coordinates and native hotkey are independent
// namespace fields; none is inferred from an unchanged chain domain.
func TestStatsAggregateWrongOwnerOpenPreservesPhysicalState(t *testing.T) {
	t.Parallel()
	fixture, prior := newStatsAggregateOpenTestFixture(t)
	alternate := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{93}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	var alternateVPK [32]byte
	copy(alternateVPK[:], alternate)
	var changed []string
	variants := []struct {
		name   string
		change func(*AttemptCutV2Context)
	}{
		{name: "operator", change: func(expected *AttemptCutV2Context) { expected.Identity.NoID++ }},
		{name: "validator", change: func(expected *AttemptCutV2Context) { expected.Identity.ValidatorID++ }},
		{name: "uid", change: func(expected *AttemptCutV2Context) { expected.Identity.ValidatorUID++ }},
		{name: "vpk", change: func(expected *AttemptCutV2Context) { expected.Identity.ValidatorVPK = attemptHex32(alternateVPK) }},
		{name: "hotkey", change: func(expected *AttemptCutV2Context) { expected.Activation.Hotkey[0] ^= 128 }},
	}
	for _, variant := range variants {
		expected := fixture.source.expected
		variant.change(&expected)
		if observeStatsAggregateOpenTestRefusal(t, fixture, variant.name, expected, fixture.config) {
			changed = append(changed, variant.name)
		}
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, prior)
	if len(changed) != 0 {
		t.Fatalf("wrong-owner aggregate Open mutated physical storage before refusal: %v", changed)
	}
}

// The separate valid legacy/reporting transform is part of the stored config;
// equal authoritative PPM settings do not authorize silently replacing it.
func TestStatsAggregateWrongLegacyConfigOpenPreservesPhysicalState(t *testing.T) {
	t.Parallel()
	fixture, prior := newStatsAggregateOpenTestFixture(t)
	var changed []string
	variants := []struct {
		name   string
		change func(*StatsConfig)
	}{
		{name: "alpha", change: func(config *StatsConfig) { config.Alpha /= 2 }},
		{name: "wilson", change: func(config *StatsConfig) { config.Z *= 2 }},
		{name: "latency", change: func(config *StatsConfig) { config.LatRefMs *= 2 }},
	}
	for _, variant := range variants {
		config := fixture.config
		variant.change(&config)
		if observeStatsAggregateOpenTestRefusal(t, fixture, variant.name, fixture.source.expected, config) {
			changed = append(changed, variant.name)
		}
	}
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, prior)
	if len(changed) != 0 {
		t.Fatalf("wrong-config aggregate Open mutated physical storage before refusal: %v", changed)
	}
}

// Cancellation is delivered at the existing actual directory-check hook,
// before parent/owner Sync or backend recovery. It is not an I/O sentinel.
func TestStatsAggregateOpenCancellationBeforeWritableAdmission(t *testing.T) {
	t.Parallel()
	fixture, prior := newStatsAggregateOpenTestFixture(t)
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var canceled atomic.Int64
	observer := &statsAggregateOpenTestObserver{}
	store, err := openStatsAggregateStore(ctx, fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if operation == "after-directory-check" {
			canceled.Add(1)
			cancel()
		}
		return observer.step(operation, name)
	}})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || !errors.Is(err, context.Canceled) || canceled.Load() != 1 {
		t.Fatalf("actual pre-writer cancellation was not reached: store=%v cancellations=%d error=%v", store != nil, canceled.Load(), err)
	}
	changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture))
	events := observer.snapshot()
	t.Logf("canceled_open physical_changes=%v completed_operations=%v", changes, events)
	assertStatsAggregateOpenTestPriorAuthority(t, fixture, prior)
	if len(changes) != 0 || len(events) != 0 {
		t.Fatal("canceled aggregate Open mutated physical storage after directory-check cancellation")
	}
}

// A matching owner may open and recover its real backend, including advanced
// independent clocks. The initial bootstrap context is not a clock rollback.
func TestStatsAggregateMatchingAdvancedOpenRetainsAuthority(t *testing.T) {
	t.Parallel()
	fixture := newStatsAggregateTestFixture(t, 1, 1, statsAggregateHooks{})
	head, err := fixture.store.Replay(context.Background(), fixture.cut, fixture.source.expected, fixture.source.policy, fixture.source.bounds, fixture.replayOptions(t), statsAggregateAdvance{RotateNative: true, ToSettlementEpoch: 43})
	if err != nil || head.Generation != 1 || head.SettlementEpoch != 43 || head.EgressGeneration != 2 || head.LastAppliedSequence != 10 || head.SettlementFirstSequence != 11 || head.EgressFirstSequence != 11 || head.SettlementPriorRoot != fixture.cut.Root {
		t.Fatalf("genuine advanced namespace: %+v error=%v", head, err)
	}
	rows := statsAggregateTestRows(t, fixture.store, head.Generation)
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	observer := &statsAggregateOpenTestObserver{}
	store := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{StorageStep: observer.step})
	reopened, err := store.Head(context.Background())
	if err != nil || reopened != head || !reflect.DeepEqual(rows, statsAggregateTestRows(t, store, reopened.Generation)) || reopened.EgressHashCount != 0 || reopened.EgressClaimCount != 0 {
		t.Fatalf("matching reopen changed complete rows or either clock: %v", err)
	}
	if len(observer.snapshot()) == 0 {
		t.Fatal("matching control never reached actual writable recovery")
	}
}

// Structurally valid activation with the wrong decoded policy is refused
// before the storage opener, not after any actual filesystem operation.
func TestStatsAggregateOpenPolicyMismatchStopsBeforeStorage(t *testing.T) {
	t.Parallel()
	fixture, _ := newStatsAggregateOpenTestFixture(t)
	expected := fixture.source.expected
	expected.Activation.Domain.PolicyHash[0] ^= 128
	if err := expected.Validate(); err != nil {
		t.Fatal(err)
	}
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	var calls atomic.Int64
	store, err := openStatsAggregateStore(context.Background(), fixture.path, expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(string, string) error { calls.Add(1); return nil }})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || err == nil || !strings.Contains(err.Error(), "decoded policy differs") || calls.Load() != 0 || len(before.changes(snapshotStatsAggregateOpenTestStore(t, fixture))) != 0 {
		t.Fatalf("policy mismatch reached physical storage: store=%v calls=%d error=%v", store != nil, calls.Load(), err)
	}
}

// An inconsistent duplicated chain field is malformed input, unlike the
// coherent wrong-domain causal cases above, and already refuses pre-storage.
func TestStatsAggregateOpenInvalidNamespaceStopsBeforeStorage(t *testing.T) {
	t.Parallel()
	fixture, _ := newStatsAggregateOpenTestFixture(t)
	expected := fixture.source.expected
	expected.Identity.ChainID++
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	var calls atomic.Int64
	store, err := openStatsAggregateStore(context.Background(), fixture.path, expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(string, string) error { calls.Add(1); return nil }})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || err == nil || !strings.Contains(err.Error(), "activation differs") || calls.Load() != 0 || len(before.changes(snapshotStatsAggregateOpenTestStore(t, fixture))) != 0 {
		t.Fatalf("malformed namespace reached physical storage: store=%v calls=%d error=%v", store != nil, calls.Load(), err)
	}
}

// Both nil and already-canceled operation contexts stop before any storage
// hook; neither is mislabeled as a physical corruption or recovery failure.
func TestStatsAggregateOpenCanceledContextStopsBeforeStorage(t *testing.T) {
	t.Parallel()
	fixture, _ := newStatsAggregateOpenTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := snapshotStatsAggregateOpenTestStore(t, fixture)
	var calls atomic.Int64
	for _, operationCtx := range []context.Context{nil, ctx} {
		store, err := openStatsAggregateStore(operationCtx, fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(string, string) error { calls.Add(1); return nil }})
		if store != nil {
			_ = store.Close()
		}
		if store != nil || err == nil || operationCtx == nil && !strings.Contains(err.Error(), "context is nil") || operationCtx != nil && !errors.Is(err, context.Canceled) || calls.Load() != 0 {
			t.Fatalf("invalid operation context reached storage or changed error class: store=%v calls=%d error=%v", store != nil, calls.Load(), err)
		}
	}
	if changes := before.changes(snapshotStatsAggregateOpenTestStore(t, fixture)); len(changes) != 0 {
		t.Fatalf("already-refused contexts changed physical state: %v", changes)
	}
}

// For the matching owner an actual writer failure must remain a physical error,
// not be recast as domain refusal or cancellation. Reopening joins and recovers.
func TestStatsAggregateOpenAuthorizedStorageFailureRemainsPhysical(t *testing.T) {
	t.Parallel()
	fixture, prior := newStatsAggregateOpenTestFixture(t)
	failure := errors.New("authorized aggregate recovery create sentinel")
	var attempted atomic.Int64
	store, err := openStatsAggregateStore(context.Background(), fixture.path, fixture.source.expected, fixture.source.policy, fixture.config, fixture.bounds, statsAggregateHooks{StorageStep: func(operation, name string) error {
		if operation == "before-create" {
			attempted.Add(1)
			return failure
		}
		return nil
	}})
	if store != nil {
		_ = store.Close()
	}
	if store != nil || !errors.Is(err, failure) || attempted.Load() != 1 || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "namespace or config differs") {
		t.Fatalf("authorized physical fault lost its boundary: store=%v attempts=%d error=%v", store != nil, attempted.Load(), err)
	}
	reopened := reopenStatsAggregateTestFixture(t, fixture, statsAggregateHooks{})
	head, err := reopened.Head(context.Background())
	if err != nil || head != prior {
		t.Fatalf("failed Open leaked ownership or changed authority: %v", err)
	}
	assertStatsAggregateTestParity(t, fixture, head)
}
