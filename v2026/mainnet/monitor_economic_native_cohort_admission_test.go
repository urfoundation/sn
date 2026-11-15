//go:build linux || darwin

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The copied original lock and head are explicit plan inputs, distinct from
// the deliberately rebound staged/target lock. Nothing owns them by suffix.
func monitorCohortArchivePlan(t *testing.T, f *monitorNativeCohortFixture, ref durablevolume.Reference) durablevolume.PreparationPlan {
	t.Helper()
	raw, err := os.ReadFile(ref.Path)
	if err != nil || monitorReadDigest(raw) != ref.Sha256 {
		t.Fatal("cohort reference differs", err)
	}
	var cohort durablevolume.PreparationCohort
	if err := json.Unmarshal(raw, &cohort); err != nil || len(cohort.Plans) != 2 {
		t.Fatal("cohort plan census differs", err)
	}
	raw, err = os.ReadFile(cohort.Plans[1].Path)
	if err != nil || monitorReadDigest(raw) != cohort.Plans[1].Sha256 {
		t.Fatal("archive plan differs", err)
	}
	var plan durablevolume.PreparationPlan
	if err := json.Unmarshal(raw, &plan); err != nil || plan.RestoreArchive == nil {
		t.Fatal("archive physical identity absent", err)
	}
	if plan.RestoreArchive.Inode == 0 {
		t.Fatal("archive identity is not retained")
	}
	foundFile, foundHead := false, false
	attribute := durablehead.Attribute("mainnet-monitor-checkpoint", "a000.json")
	for _, source := range plan.Sources {
		if source.File.Path == "a000.json.lock" {
			if source.Path == filepath.Join(f.targets[1].archive, source.File.Path) || source.Identity.Inode == 0 {
				t.Fatal("staged target lock is not a separate reviewed inode")
			}
			foundFile = true
		}
	}
	for _, owner := range plan.Owners {
		for _, spec := range owner.Attributes {
			if spec.Path == "a000.json.lock" && spec.Name == attribute {
				foundHead = true
			}
		}
	}
	if !foundFile || !foundHead {
		t.Fatal("accepted plan did not explicitly bind the tested lock/head")
	}
	return plan
}

// Lost original head, replaced archive and lost staged payload are independent
// admission failures. All are checked before the first root gets a control.
func TestMonitorNativeCohortRestoreChecksOriginalAndStagedCustodyBeforeEffects(t *testing.T) {
	for _, fault := range []string{"original-head", "original-root", "staged-lock", "busy-original"} {
		f := newMonitorNativeCohortFixture(t, 1)
		ref := f.plan(t)
		plan := monitorCohortArchivePlan(t, f, ref)
		var held *os.File
		var err error
		switch fault {
		case "original-head":
			err = unix.Removexattr(filepath.Join(f.targets[1].archive, "a000.json.lock"), durablehead.Attribute("mainnet-monitor-checkpoint", "a000.json"))
		case "original-root":
			err = os.Rename(f.targets[1].archive, f.targets[1].archive+".retained")
			if err == nil {
				err = os.Mkdir(f.targets[1].archive, 0700)
			}
		case "staged-lock":
			for _, source := range plan.Sources {
				if source.File.Path == "a000.json.lock" {
					err = os.Remove(source.Path)
				}
			}
		case "busy-original":
			held, err = os.Open(f.targets[1].archive)
			if err == nil {
				err = unix.Flock(int(held.Fd()), unix.LOCK_EX|unix.LOCK_NB)
			}
		}
		if err != nil {
			t.Fatal("fault was not installed", fault, err)
		}
		for _, mode := range []string{"cohort-check", "cohort-apply", "cohort-config"} {
			var output, diagnostic bytes.Buffer
			code := runMain(f.native.ctx, []string{"storage-prepare", mode, "--cohort", ref.Path, "--cohort-sha256", ref.Sha256}, &output, &diagnostic)
			if code == 0 || output.Len() != 0 {
				t.Fatal("missing planned custody was admitted", fault, mode, code, diagnostic.String())
			}
			if fault == "busy-original" && !strings.Contains(diagnostic.String(), durablevolume.ErrBusy.Error()) {
				t.Fatal("archive contention failed at unrelated boundary", mode, diagnostic.String())
			}
		}
		if held != nil {
			if err := held.Close(); err != nil {
				t.Fatal(err)
			}
		}
		for _, request := range f.request.Preparations {
			entries, err := os.ReadDir(request.RootPath)
			if err != nil || len(entries) != 0 {
				t.Fatal("later missing custody changed target root", fault, err)
			}
			for _, path := range []string{request.ControlPath, request.MarkerPath, request.LeasePath, request.DeclarationPath} {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refused cohort published authority", fault, path, err)
				}
			}
		}
	}
}

// A completed peer survives loss of the other root's original archive head.
// Reopening cannot use its staged replacement as evidence of source survival.
func TestMonitorNativeCohortRestoreRetainsCompletedPeerAcrossSourceLoss(t *testing.T) {
	f := newMonitorNativeCohortFixture(t, 1)
	ref := f.plan(t)
	_ = monitorCohortArchivePlan(t, f, ref)
	lock := filepath.Join(f.targets[1].archive, "a000.json.lock")
	attribute := durablehead.Attribute("mainnet-monitor-checkpoint", "a000.json")
	original := make([]byte, 4096)
	n, err := unix.Getxattr(lock, attribute, original)
	if err != nil {
		t.Fatal(err)
	}
	original = original[:n]
	removed := false
	host := &compositionObservationHost{Host: f.sources[0].storage.Host, observe: func(*os.File) error {
		if removed {
			return nil
		}
		if _, err := os.Stat(f.request.Preparations[0].DeclarationPath); err == nil {
			if err := unix.Removexattr(lock, attribute); err != nil {
				return err
			}
			removed = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}}
	ctx := durablepath.WithHost(f.native.ctx, host)
	args := []string{"storage-prepare", "cohort-apply", "--cohort", ref.Path, "--cohort-sha256", ref.Sha256}
	var output, diagnostic bytes.Buffer
	if code := runMain(ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !removed || !strings.Contains(diagnostic.String(), durablevolume.ErrIdentity.Error()) {
		t.Fatal("late source loss did not stop only the affected cohort", code, removed, diagnostic.String())
	}
	firstControl := f.request.Preparations[0].ControlPath
	prefix, err := os.ReadFile(firstControl)
	if err != nil || !bytes.Contains(prefix, []byte(ref.Sha256)) {
		t.Fatal("first root lost original cohort progress", err)
	}
	first := mainnetNamespaceTest(t, f.request.Preparations[0].RootPath)
	if _, err := os.Lstat(f.request.Preparations[1].ControlPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("late missing source reserved second root", err)
	}
	output.Reset()
	diagnostic.Reset()
	if code := runMain(f.native.ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 {
		t.Fatal("reopen reconstructed missing source head", code, diagnostic.String())
	}
	if !reflect.DeepEqual(first, mainnetNamespaceTest(t, f.request.Preparations[0].RootPath)) {
		t.Fatal("source refusal rewrote completed peer")
	}
	if err := unix.Setxattr(lock, attribute, original, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	f.native.ctx = f.apply(t, ref, false)
	after, err := os.ReadFile(firstControl)
	if err != nil || !bytes.Equal(prefix, after) || !reflect.DeepEqual(first, mainnetNamespaceTest(t, f.request.Preparations[0].RootPath)) {
		t.Fatal("same-plan source recovery reset completed peer", err)
	}
	run := f.native.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current || event.State.Cursor.Number != 102 || event.State.PendingThrough != nil || event.State.BatchCount != 2 {
		t.Fatal("recovered source lost original pending economic outcome", event)
	}
	run.stop(t)
}
