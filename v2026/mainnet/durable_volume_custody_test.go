// The facts-only host controls admission; actual private files, locks, writes,
// renames and sync boundaries are exercised by the production custody owner.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Every test provisions its declaration outside the owned journal root.
func mainnetDurableCustodyFixture(t *testing.T) (*durablefixture.Fixture, *mainnetDurableDirectory, string) {
	t.Helper()
	root := filepath.Join(ownerSigningTestDirectory(t), "custody")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := durablefixture.New(t, t.Context(), root)
	owner, err := openMainnetDurableDirectory(fixture.Context, root, durablevolume.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	return fixture, owner, filepath.Join(root, "original.json")
}

// Direct owners cannot gain ordinary pathname I/O from an omitted guard, even
// when a caller constructs an internal wrapper without command dispatch.
func TestMainnetDurableMissingOwnerHasNoPathFallback(t *testing.T) {
	path := filepath.Join(ownerSigningTestDirectory(t), "must-not-exist")
	var owner *mainnetDurableDirectory
	if err := owner.checkWrite(nil); err == nil {
		t.Fatal("nil owner admitted write capability")
	}
	if fd, err := owner.openFile(path, os.O_WRONLY|os.O_CREATE, 0600); err == nil || fd >= 0 {
		t.Fatal("nil owner opened an ordinary path", fd, err)
	}
	if err := owner.publish(path, []byte("unadmitted state"), nil); err == nil {
		t.Fatal("nil owner published without physical admission")
	}
	if _, _, err := owner.readFile(t.Context(), path, 1024); err == nil {
		t.Fatal("nil owner admitted a custody read")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing admission created a file", err)
	}
}

// The same owner resumes after either reserve recovers. A refused admission
// cannot change the original record or turn a pending operation into corruption.
func TestMainnetDurableCustodyReserveRecoversSameOwner(t *testing.T) {
	fixture, owner, path := mainnetDurableCustodyFixture(t)
	original, successor := []byte("original retained signed intent\n"), []byte("same intent with retained receipt\n")
	if err := owner.publish(path, original, nil); err != nil {
		t.Fatal(err)
	}
	for _, reserve := range [][2]uint64{{0, 1024}, {1024 * 1024, 0}} {
		fixture.Host.SetReserve(reserve[0], reserve[1])
		if err := owner.publish(path, successor, nil); !errors.Is(err, durablevolume.ErrUnavailable) || owner.failed != nil {
			t.Fatalf("pressure poisoned or admitted owner: %v, sticky=%v", err, owner.failed)
		}
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, original) {
			t.Fatalf("refused publication changed predecessor: %q %v", raw, err)
		}
		fixture.Host.SetReserve(1024*1024, 1024)
		if err := owner.publish(path, original, nil); err != nil {
			t.Fatalf("same owner could not resume: %v", err)
		}
	}
	if err := owner.publish(path, successor, nil); err != nil {
		t.Fatal(err)
	}
}

// A lost directory-sync acknowledgement is uncertain after rename. Reopening
// the original custody reads the actual successor without resetting any bytes.
func TestMainnetDurableCustodyPostRenameSyncRequiresOriginalReopen(t *testing.T) {
	fixture, owner, path := mainnetDurableCustodyFixture(t)
	original, successor := []byte("signed intent\n"), []byte("same signed intent and receipt\n")
	if err := owner.publish(path, original, nil); err != nil {
		t.Fatal(err)
	}
	syncFailure := errors.New("synthetic lost directory sync receipt")
	if err := owner.publish(path, successor, func(*os.File) error { return syncFailure }); !errors.Is(err, syncFailure) || owner.failed == nil {
		t.Fatalf("post-rename uncertainty was acknowledged: %v", err)
	}
	if err := owner.publish(path, original, nil); err == nil {
		t.Fatal("uncertain owner overwrote the renamed successor")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openMainnetDurableDirectory(fixture.Context, filepath.Dir(path), durablevolume.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	raw, _, err := reopened.readFile(t.Context(), path, 4096)
	if err != nil || !bytes.Equal(raw, successor) {
		t.Fatalf("recovery lost actual retained successor: %q %v", raw, err)
	}
}

// Equal bytes are not an equal lock or journal. Restoring an observed lost
// inode cannot revive the old owner, including the original containing root.
func TestMainnetDurableCustodyReplacementIsSticky(t *testing.T) {
	for _, kind := range []string{"journal", "marker", "directory"} {
		_, owner, path := mainnetDurableCustodyFixture(t)
		raw := []byte("retained intent\n")
		if err := owner.publish(path, raw, nil); err != nil {
			t.Fatal(err)
		}
		target := path
		if kind == "marker" {
			target = path + ".lock"
			if err := os.WriteFile(target, raw, 0600); err != nil {
				t.Fatal(err)
			}
			marker, err := os.Open(target)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = marker.Close() })
			if err := owner.bindMarker(marker, true); err != nil {
				t.Fatal(err)
			}
		} else if kind == "directory" {
			target = filepath.Dir(path)
		}
		if err := os.Rename(target, target+".retained"); err != nil {
			t.Fatal(err)
		}
		if kind == "directory" {
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(target, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := owner.readFile(t.Context(), path, 4096); !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatalf("%s replacement accepted: %v", kind, err)
		}
		if err := errors.Join(os.Remove(target), os.Rename(target+".retained", target)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := owner.readFile(t.Context(), path, 4096); !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatalf("%s restoration revived old owner: %v", kind, err)
		}
	}
}

// Inspection remains available under reserve pressure, while mutation remains
// unavailable. The read-only owner cannot use inspection to acquire a write.
func TestMainnetDurableReadOnlyInspectionSurvivesFullVolume(t *testing.T) {
	fixture, owner, path := mainnetDurableCustodyFixture(t)
	raw := []byte("original signed bytes\n")
	if err := owner.publish(path, raw, nil); err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	fixture.Host.SetReserve(0, 0)
	reader, err := openMainnetDurableDirectory(fixture.Context, filepath.Dir(path), durablevolume.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	got, _, err := reader.readFile(t.Context(), path, 4096)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("retained inspection failed: %q %v", got, err)
	}
	if err := reader.publish(path, []byte("replacement"), nil); err == nil {
		t.Fatal("read-only inspection acquired publication authority")
	}
}

// A missing declared root is never an empty fresh campaign directory.
func TestMainnetDurableCustodyMissingRootCannotRecreate(t *testing.T) {
	fixture, owner, path := mainnetDurableCustodyFixture(t)
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(path)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if reopened, err := openMainnetDurableDirectory(fixture.Context, root, durablevolume.ReadWrite); err == nil {
		_ = reopened.close()
		t.Fatal("missing root silently recreated")
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused open changed root: %v", err)
	}
}

// Direct persistent constructors refuse absent declarations even when no CLI
// has admitted the caller. They cannot create a marker, response, or journal.
func TestMainnetDurableCustodyAbsentReferenceHasNoEffects(t *testing.T) {
	root := ownerSigningTestDirectory(t)
	for _, open := range []func() (*mainnetDurableDirectory, error){
		func() (*mainnetDurableDirectory, error) {
			return openMainnetDurableDirectory(t.Context(), root, durablevolume.ReadWrite)
		},
		func() (*mainnetDurableDirectory, error) { return openOwnerLocalDurableDirectory(t.Context(), root) },
	} {
		owner, err := open()
		if err == nil || owner != nil {
			_ = owner.close()
			t.Fatal("direct persistent open accepted implicit storage")
		}
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatalf("refusal mutated custody: %v, %d entries", err, len(files))
	}
}

// A canceled owner cannot perform a bounded predecessor read or publication
// under a substituted background context; original bytes remain recoverable.
func TestMainnetDurableCustodyCancellationStopsPublication(t *testing.T) {
	fixture, originalOwner, path := mainnetDurableCustodyFixture(t)
	if err := originalOwner.close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(fixture.Context)
	owner, err := openMainnetDurableDirectory(ctx, filepath.Dir(path), durablevolume.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	raw := []byte("retained original\n")
	if err := owner.publish(path, raw, nil); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := owner.publish(path, []byte("replacement"), nil); !errors.Is(err, context.Canceled) || owner.failed != nil {
		t.Fatalf("canceled publication escaped owner: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("canceled owner changed journal: %q %v", got, err)
	}
}

// Identical paths and bytes on another observed mount generation cannot grant
// the old owner permission to advance its signed journal.
func TestMainnetDurableCustodyRemountRefusesPublication(t *testing.T) {
	fixture, owner, path := mainnetDurableCustodyFixture(t)
	raw := []byte("original signed bytes\n")
	if err := owner.publish(path, raw, nil); err != nil {
		t.Fatal(err)
	}
	fixture.Host.ReplaceMount()
	if err := owner.publish(path, []byte("replacement"), nil); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("remounted owner admitted write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("remount refusal changed bytes: %q %v", got, err)
	}
}

// Owner signing explicitly selects its own schema; production daemon loaders
// cannot inherit that permission from the filename or supplied context.
func ownerLocalDurableTestContext(t *testing.T, root string, storageContexts ...context.Context) context.Context {
	t.Helper()
	ctx := t.Context()
	if len(storageContexts) == 1 {
		ctx = storageContexts[0]
	}
	fixture := durablefixture.New(t, ctx, root)
	config, err := durablevolume.Load(fixture.Reference)
	if err != nil {
		t.Fatal(err)
	}
	config.Schema = durablevolume.OwnerLocalSchema
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.Reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ref := durablevolume.Reference{Path: fixture.Reference.Path, Sha256: durablefixture.Digest(raw)}
	return durablepath.WithHost(durablevolume.WithReference(ctx, ref), fixture.Host)
}

// Historical device tests still exercise the real guarded constructor. This
// helper provisions only test policy; it cannot skip any production admission.
func ownerSigningTestSign(t *testing.T, ctx context.Context, config ownerSigningDeviceConfig, request ownerSigningRequest, adapter ownerSigningAdapter) (ownerSigningReply, error) {
	t.Helper()
	return signOwnerRequest(ownerLocalDurableTestContext(t, filepath.Dir(config.StatePath), ctx), config, request, adapter)
}

// Calling an actual host method directly cannot bypass the common dispatcher.
// Neither installing static files nor starting a unit reaches its first effect.
func TestMainnetDurableUnitEffectsRejectMissingDeclaration(t *testing.T) {
	root := ownerSigningTestDirectory(t)
	executions := 0
	host := &repairValidatorHost{execute: func(context.Context, string, []string) ([]byte, error) {
		executions++
		return nil, errors.New("unexpected manager effect")
	}}
	activation := &validatorActivationHost{host: host, unitDirectory: root}
	unit := repairValidatorUnit{File: planFileReference{Path: filepath.Join(root, "role.service")}, Config: planFileReference{Path: filepath.Join(root, "runtime.yml")}}
	plan := validatorActivationPlan{Units: []validatorActivationUnit{{Unit: unit}}}
	if err := activation.install(t.Context(), plan, 0, planFileReference{}); err == nil || !bytes.Contains([]byte(err.Error()), []byte("durable")) {
		t.Fatalf("direct installation escaped admission: %v", err)
	}
	if err := host.start(t.Context(), repairValidatorPlan{Unit: unit}, func() error { return nil }); err == nil || !bytes.Contains([]byte(err.Error()), []byte("durable")) {
		t.Fatalf("direct start escaped admission: %v", err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 || executions != 0 {
		t.Fatalf("missing declaration caused host effect: files=%d calls=%d err=%v", len(files), executions, err)
	}
}

// A valid declaration for another controller cannot replace the one named by
// the independently signed unit. The real start boundary rejects before exec.
func TestMainnetDurableUnitRejectsDifferentControllerDeclaration(t *testing.T) {
	root := ownerSigningTestDirectory(t)
	fixture := durablefixture.New(t, t.Context(), root)
	other := durablefixture.New(t, t.Context(), ownerSigningTestDirectory(t))
	calls := 0
	host := &repairValidatorHost{execute: func(context.Context, string, []string) ([]byte, error) {
		calls++
		return nil, nil
	}}
	plan := repairValidatorPlan{Unit: repairValidatorUnit{DurableVolumes: &fixture.Reference, Name: "synthetic-role.service"}, CommandTimeoutSeconds: 1}
	if err := host.start(other.Context, plan, func() error { return nil }); err == nil || calls != 0 {
		t.Fatal("different valid controller declaration reached unit start", calls, err)
	}
	if err := host.start(fixture.Context, plan, func() error { return nil }); err == nil || calls != 0 {
		t.Fatal("declaration alone bypassed actual service-credential physical inspection", calls, err)
	}
}

// Missing and invalid owner policy stop the real retained signing path before
// any metadata preparation, device invocation, or request-marker creation.
func TestOwnerSigningInvalidCustodyCannotReachAdapter(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	adapter := &ownerSigningDeviceTestAdapter{request: request}
	prepared, err := os.Lstat(config.StatePath + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	bad := durablevolume.WithReference(t.Context(), durablevolume.Reference{Path: filepath.Join(filepath.Dir(config.StatePath), "absent-policy.json"), Sha256: durablefixture.Digest([]byte("absent"))})
	for _, ctx := range []context.Context{t.Context(), bad} {
		if _, err := signOwnerRequest(ctx, config, request, adapter.invoke); err == nil {
			t.Fatal("unadmitted owner policy reached signing path")
		}
	}
	files, err := os.ReadDir(filepath.Dir(config.StatePath))
	marker, markerErr := os.Lstat(config.StatePath + ".lock")
	if err != nil || markerErr != nil || len(files) != 1 || !os.SameFile(prepared, marker) || marker.Size() != 0 || adapter.prepares.Load() != 0 || adapter.signs.Load() != 0 {
		t.Fatalf("invalid policy created device or disk effect: files=%d prepares=%d signs=%d err=%v", len(files), adapter.prepares.Load(), adapter.signs.Load(), err)
	}
}

// Portable inspection continues without a daemon declaration. Stateful device
// commands refuse absent custody before even parsing a device invocation.
func TestOwnerSigningCommandRequiresOwnerLocalCustody(t *testing.T) {
	adapterCalls := 0
	adapter := func(context.Context, ownerSigningDeviceConfig, ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
		adapterCalls++
		return ownerSigningAdapterResult{}, errors.New("device must remain unreachable")
	}
	var stdout, stderr bytes.Buffer
	if code := runOwnerSigningCommandWithAdapter(t.Context(), []string{"sign"}, &stdout, &stderr, adapter); code != 2 || !bytes.Contains(stderr.Bytes(), []byte("durable")) || adapterCalls != 0 {
		t.Fatalf("missing custody reached device or changed refusal: %d %s calls=%d", code, stderr.String(), adapterCalls)
	}
	for _, mode := range []string{"inspect", "reply", "verify", "ledger-plan"} {
		stderr.Reset()
		if code := runMain(t.Context(), []string{"owner-signing", mode}, &stdout, &stderr); code != 2 || bytes.Contains(stderr.Bytes(), []byte("durable")) {
			t.Fatalf("portable %s required deployment custody: %d %s", mode, code, stderr.String())
		}
	}
}

// The guarded local schema carries original response custody through restart;
// neither the same command nor a full-volume refusal can sign a second time.
func TestOwnerSigningLocalCustodyReplaysOriginalResponse(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	ctx := ownerLocalDurableTestContext(t, filepath.Dir(config.StatePath))
	adapter := &ownerSigningDeviceTestAdapter{request: request}
	first, err := signOwnerRequest(ctx, config, request, adapter.invoke)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(config.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := signOwnerRequest(ctx, config, request, adapter.invoke)
	if err != nil || rootObjectHash(first) != rootObjectHash(second) || adapter.signs.Load() != 1 {
		t.Fatalf("guarded restart reissued original request: %v, signs=%d", err, adapter.signs.Load())
	}
	after, err := os.ReadFile(config.StatePath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("replay rewrote original signed journal: %v", err)
	}
}
