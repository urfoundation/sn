// Read-only composition borrows committed physical heads without consuming
// a writer lease or making incomplete state eligible for repair.
package main

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Bootstrap cannot consume its own first claim until the independent passive
// monitor has explicit fresh custody. It neither enrolls nor repairs that owner.
func TestBootstrapPassivePreparationRequiresFreshMonitorHead(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	path := f.root.plan.PassiveService.CheckpointPath
	markerPath := path + ".lock"
	originalMarker := markerPath + "-original"
	if err := os.Rename(markerPath, originalMarker); err != nil {
		t.Fatal(err)
	}
	before := f.journals(t)
	var diagnostic bytes.Buffer
	if code := f.root.command(t.Context(), "apply", io.Discard, &diagnostic); code == 0 || !maps.Equal(before, f.journals(t)) {
		t.Fatal("missing monitor custody consumed a bootstrap claim", code, diagnostic.String())
	}
	if err := os.WriteFile(markerPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	diagnostic.Reset()
	if code := f.root.command(t.Context(), "apply", io.Discard, &diagnostic); code == 0 || !maps.Equal(before, f.journals(t)) {
		t.Fatal("empty marker enrolled a monitor or consumed bootstrap authority", code, diagnostic.String())
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("bootstrap wrote an independent monitor checkpoint", err)
	}
	if err := errors.Join(os.Remove(markerPath), os.Rename(originalMarker, markerPath)); err != nil {
		t.Fatal(err)
	}
	reader, err := openBootstrapUnclaimedSnapshot(f.storageContext(t.Context()), path, "mainnet-monitor-checkpoint", maxRpcReplyBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	if result := f.root.result(t, "apply"); result.Broadcasts != 0 || result.ActivationReady || result.SignatureStatus != "not-applicable-passive-observation" {
		t.Fatal("fresh passive custody changed signature authority", result)
	}
	if err := reader.checkpoint(); err != nil {
		t.Fatal("preparation consumed independent monitor custody", err)
	}
}

// Deleting the preprovisioned head from the same original inode must be refused
// just as replacing the marker. The absence is never a fresh monitor signal.
func TestBootstrapPassivePreparationRejectsLostMonitorAnchor(t *testing.T) {
	f := newBootstrapRootPassiveFixture(t)
	path := f.root.plan.PassiveService.CheckpointPath
	marker, err := os.Open(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer marker.Close()
	if err := unix.Fremovexattr(int(marker.Fd()), durablehead.Attribute("mainnet-monitor-checkpoint", filepath.Base(path))); err != nil {
		t.Fatal("fixture lacks explicitly prepared monitor head", err)
	}
	before := f.journals(t)
	var diagnostic bytes.Buffer
	if code := f.root.command(t.Context(), "apply", io.Discard, &diagnostic); code == 0 || !maps.Equal(before, f.journals(t)) {
		t.Fatal("lost original monitor head became fresh custody", code, diagnostic.String())
	}
}

// Both five-marker views coexist through the real public observation command.
// Full media still permits inspection, while every borrowed writer is refused.
func TestBootstrapReadinessSharedSnapshotsRemainReadOnly(t *testing.T) {
	f := newBootstrapChainReadinessFixture(t)
	ctx := f.root.storage.Context
	first, err := openBootstrapChainReadinessState(ctx, f.preparation)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	second, err := openBootstrapChainReadinessState(ctx, f.preparation)
	if err != nil {
		t.Fatal("second passive view consumed exclusive writer custody", err)
	}
	defer second.close()
	original := f.journals(t)
	f.root.storage.Host.SetReserve(0, 0)
	if err := first.checkpoint(ctx); err != nil {
		t.Fatal("full media hid original retained custody", err)
	}
	for index, path := range f.preparation.childPaths() {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := first.locks[index].storage.head.CheckWrite(); !errors.Is(err, durablehead.ErrReadOnly) {
			t.Fatal("borrowed snapshot head acquired writer authority", err)
		}
		if err := first.locks[index].storage.publish(path, raw, nil); err == nil {
			t.Fatal("borrowed readiness view acquired writer authority", err)
		}
	}
	server := bootstrapReadinessTestServer(t, f.census)
	var output, diagnostic bytes.Buffer
	if code := f.command(t.Context(), "readiness", &output, &diagnostic, "--rpc", server.URL); code != 0 {
		t.Fatal("public readiness cannot compose with intact passive views", code, diagnostic.String())
	}
	if err := second.checkpoint(ctx); err != nil || !maps.Equal(original, f.journals(t)) {
		t.Fatal("passive composition changed retained bytes or authority", err)
	}
}

// Explicit fresh heads distinguish a not-yet-claimed child from loss of a
// previously retained member. Absence never enrolls a replacement marker.
func TestBootstrapContractFreshMarkersRequireExplicitHeads(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareVaultPrerequisite()
	result, plans := bootstrapContractTestInspection(t, f)
	before := bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)
	if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); err != nil || !result.CustodyInspectionComplete || result.Actions[1].CustodyStatus != "not-claimed" {
		t.Fatal("explicitly prepared fresh child could not be inspected", result.Actions[1].CustodyStatus, err)
	}
	if !maps.Equal(before, bootstrapContractTestJournals(t, f.config.Plan.RunDirectory)) {
		t.Fatal("fresh inspection changed original custody")
	}
	path := filepath.Join(f.config.Plan.RunDirectory, evmVaultCreateStateFile) + ".lock"
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	result, plans = bootstrapContractTestInspection(t, f)
	if err := inspectBootstrapContractCustody(f.storage.Context, plans, &result); !errors.Is(err, durablevolume.ErrIdentity) || result.CustodyInspectionComplete || result.Actions[1].CustodyStatus == "not-claimed" {
		t.Fatal("empty replacement marker gained fresh-child authority", err)
	}
}

// The operational working/progress directory is separately approved and may
// differ from the protocol coordinator state. Both must reach the inspector.
func TestServiceStorageInspectionIncludesOperationalAndProtocolRoots(t *testing.T) {
	f := newBootstrapChainFixture(t)
	config := f.validators[0]
	raw, err := os.ReadFile(config.path)
	if err != nil {
		t.Fatal(err)
	}
	operation := filepath.Join(filepath.Dir(config.path), "separate-operational-output")
	unit := repairValidatorUnit{Name: "sn-mainnet-validator-majority.service", StateDirectory: operation,
		Config: planFileReference{Path: config.path, Sha256: monitorReadDigest(raw)}}
	host := &repairValidatorHost{rootUid: uint32(os.Geteuid()), trustRoot: filepath.Dir(config.path)}
	paths, err := host.serviceStorageDirectories(t.Context(), unit, raw)
	want := []string{config.config.StateDir, operation}
	for _, operator := range config.config.Operators {
		want = append(want, operator.StateDir)
	}
	for _, operator := range config.config.EvidenceV2.Operators {
		want = append(want, operator.ReplayScratchRoot, operator.SealScratchRoot, filepath.Dir(operator.Activation.Path))
	}
	slices.Sort(paths)
	slices.Sort(want)
	if err != nil || !slices.Equal(paths, want) {
		t.Fatal("preflight omitted a signed state owner or confused operational storage", paths, want, err)
	}
	paths, err = host.serviceStorageDirectories(t.Context(), unit, nil)
	slices.Sort(paths)
	if err != nil || !slices.Equal(paths, want) {
		t.Fatal("preflight cannot reopen its actual pinned runtime config", paths, err)
	}
}
