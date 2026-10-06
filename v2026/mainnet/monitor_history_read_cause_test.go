//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Every case first produces its plan through the actual public dispatcher.
// Hooks only select when a real observation fails; they cannot supply bytes,
// identity facts, successful reads or publication acknowledgments.
type monitorHistoryReadFixture struct {
	ctx        context.Context
	role       string
	checkpoint string
	published  string
	original   []byte
	args       []string
	apply      func(context.Context, monitorServiceHooks) error
}

func newMonitorHistoryReadFixture(t *testing.T, kind string) *monitorHistoryReadFixture {
	t.Helper()
	switch kind {
	case "native-archive":
		f := newMonitorNativeArchiveFixture(t, false)
		plan, args := f.plan(t)
		return &monitorHistoryReadFixture{ctx: f.native.ctx, role: f.native.policy.Role, checkpoint: f.checkpoint, published: f.archive, original: f.original, args: args,
			apply: func(ctx context.Context, hooks monitorServiceHooks) error {
				return applyMonitorNativeArchive(ctx, plan, hooks)
			}}
	case "evm-archive":
		f := newMonitorEvmArchiveFixture(t, false)
		plan, args := f.plan(t)
		return &monitorHistoryReadFixture{ctx: f.evm.ctx, role: f.evm.policy.Role, checkpoint: f.checkpoint, published: f.archive, original: f.original, args: args,
			apply: func(ctx context.Context, hooks monitorServiceHooks) error {
				return applyMonitorEvmArchive(ctx, plan, hooks)
			}}
	case "native-catalog":
		f := newMonitorNativeCatalogFixture(t, false)
		plan := f.plan(t)
		approval := f.approve(t, plan)
		return &monitorHistoryReadFixture{ctx: f.archive.native.ctx, role: f.archive.native.policy.Role, checkpoint: f.archive.checkpoint, published: f.archive.checkpoint, original: f.archive.original, args: f.applyArgs(t, plan, approval),
			apply: func(ctx context.Context, hooks monitorServiceHooks) error {
				return applyMonitorNativeCatalog(ctx, plan, approval, hooks)
			}}
	case "evm-catalog":
		f := newMonitorEvmCatalogFixture(t, false)
		plan := f.plan(t)
		approval := f.approve(t, plan)
		return &monitorHistoryReadFixture{ctx: f.archive.evm.ctx, role: f.archive.evm.policy.Role, checkpoint: f.archive.checkpoint, published: f.archive.checkpoint, original: f.archive.original, args: f.applyArgs(t, plan, approval),
			apply: func(ctx context.Context, hooks monitorServiceHooks) error {
				return applyMonitorEvmCatalog(ctx, plan, approval, hooks)
			}}
	default:
		t.Fatal("unknown fixed fixture kind", kind)
		return nil
	}
}

// The replacement host retains the original declaration and physical facts.
// Its only changed operation returns EIO after the selected guarded read begins.
func (self *monitorHistoryReadFixture) observationContext(t *testing.T) (context.Context, *atomic.Bool) {
	t.Helper()
	facts := durablefixture.New(t, self.ctx, filepath.Dir(self.checkpoint))
	original, ok := durablevolume.ReferenceFromContext(self.ctx)
	if !ok {
		t.Fatal("fixture lost original declaration")
	}
	declaration, err := durablevolume.Load(original)
	if err != nil || len(declaration.Volumes) != 1 {
		t.Fatal("fixture declaration unavailable", err)
	}
	mounts, err := facts.Host.Mounts()
	if err != nil || len(mounts) != 2 || mounts[1].Path != declaration.Volumes[0].MountPath {
		t.Fatal("observation adapter would change the declared mount", err)
	}
	failed := &atomic.Bool{}
	host := &compositionObservationHost{Host: facts.Host, observe: func(*os.File) error {
		if failed.Load() {
			return unix.EIO
		}
		return nil
	}}
	return durablepath.WithHost(self.ctx, host), failed
}

func (self *monitorHistoryReadFixture) resume(t *testing.T) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(self.ctx, self.args, &output, &diagnostic); code != 0 || output.Len() == 0 {
		t.Fatal("same reviewed public operation could not resume", code, diagnostic.String())
	}
}

// Read errors must retain their actual cause without inventing observed loss.
// Real post-publication failures preserve their exact head for joined readback;
// the public retry cannot append a second approval or republish an archive.
func monitorHistoryReadFailureCases(t *testing.T, kind string) {
	t.Helper()
	prefix := "archive"
	if strings.HasSuffix(kind, "catalog") {
		prefix = "catalog"
	}
	for _, stage := range []string{prefix + "-original", prefix + "-published"} {
		for _, fault := range []string{"io", "cancel"} {
			f := newMonitorHistoryReadFixture(t, kind)
			ctx, failed := f.observationContext(t)
			ctx, cancel := context.WithCancel(ctx)
			before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
			calls := 0
			err := f.apply(ctx, monitorServiceHooks{historyRead: func(role, step string) {
				if role == f.role && step == stage {
					calls++
					if fault == "io" {
						failed.Store(true)
					} else {
						cancel()
					}
				}
			}})
			cancel()
			failed.Store(false)
			if calls != 1 || err == nil || errors.Is(err, durablevolume.ErrIdentity) {
				t.Fatal("selected real read did not retain observation failure", kind, stage, fault, calls, err)
			}
			if fault == "io" && (!errors.Is(err, unix.EIO) || !errors.Is(err, durablevolume.ErrUnavailable)) ||
				fault == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("read cause was discarded or reclassified", kind, stage, fault, err)
			}
			for _, invented := range []string{"owner disappeared", "checkpoint is absent", "publication cannot be authenticated", "lost exact original progress"} {
				if strings.Contains(err.Error(), invented) {
					t.Fatal("unobserved custody was declared lost", kind, stage, fault, err)
				}
			}
			after := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
			if strings.HasSuffix(stage, "-original") && !reflect.DeepEqual(before, after) {
				t.Fatal("before-publication read failure changed custody", kind, fault)
			}
			if strings.HasSuffix(stage, "-published") {
				published, ok := after[filepath.Base(f.published)]
				if !ok || published.Inode == 0 || published.Data == "" {
					t.Fatal("failed readback lost its completed publication", kind, fault)
				}
				if prefix == "archive" && (published.Data != string(f.original) || !reflect.DeepEqual(before[filepath.Base(f.checkpoint)], after[filepath.Base(f.checkpoint)])) {
					t.Fatal("archive readback failure changed original progress", kind, fault)
				}
			}
			f.resume(t)
			resumed := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
			if strings.HasSuffix(stage, "-published") && !reflect.DeepEqual(after[filepath.Base(f.published)], resumed[filepath.Base(f.published)]) {
				t.Fatal("joined public retry rewrote the already published head", kind, fault)
			}
			f.resume(t)
			if !reflect.DeepEqual(resumed, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
				t.Fatal("same reviewed retry duplicated a retained publication", kind, fault)
			}
		}
	}
}

func TestMonitorNativeArchiveReadErrorsRetainOriginalCauseAndResume(t *testing.T) {
	monitorHistoryReadFailureCases(t, "native-archive")
}

func TestMonitorEvmArchiveReadErrorsRetainOriginalCauseAndResume(t *testing.T) {
	monitorHistoryReadFailureCases(t, "evm-archive")
}

func TestMonitorNativeCatalogReadErrorsRetainOriginalCauseAndResume(t *testing.T) {
	monitorHistoryReadFailureCases(t, "native-catalog")
}

func TestMonitorEvmCatalogReadErrorsRetainOriginalCauseAndResume(t *testing.T) {
	monitorHistoryReadFailureCases(t, "evm-catalog")
}

// All four actual public commands distinguish inability to read a reviewed
// document from returned bytes that contradict its reviewed digest.
func TestMonitorHistoryPublicInputObservationDiffersFromConflictingBytes(t *testing.T) {
	for _, kind := range []string{"native-archive", "evm-archive", "native-catalog", "evm-catalog"} {
		f := newMonitorHistoryReadFixture(t, kind)
		before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
		path := f.args[3]
		held := path + ".retained"
		if err := os.Rename(path, held); err != nil {
			t.Fatal(err)
		}
		var output, diagnostic bytes.Buffer
		code := runMain(f.ctx, f.args, &output, &diagnostic)
		restoreErr := os.Rename(held, path)
		if restoreErr != nil {
			t.Fatal(restoreErr)
		}
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "input read failed") || strings.Contains(diagnostic.String(), "input differs") {
			t.Fatal("missing reviewed input asserted a digest conflict", kind, code, diagnostic.String())
		}
		args := append([]string(nil), f.args...)
		args[5] = "sha256:" + strings.Repeat("f", 64)
		output.Reset()
		diagnostic.Reset()
		if code := runMain(f.ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "input differs: digest mismatch") {
			t.Fatal("observed digest conflict was not refused", kind, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
			t.Fatal("input refusal mutated owner custody", kind)
		}
		f.resume(t)
	}
}

// The actual forecast loader rereads the declaration after owner admission.
// Losing that observation cannot assert that original progress changed.
func TestMonitorCatalogForecastObservationRetainsOriginalCause(t *testing.T) {
	for _, kind := range []string{"native-catalog", "evm-catalog"} {
		f := newMonitorHistoryReadFixture(t, kind)
		before := mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))
		ref, _ := durablevolume.ReferenceFromContext(f.ctx)
		held := ref.Path + ".retained"
		calls := 0
		var moveErr error
		err := f.apply(f.ctx, monitorServiceHooks{historyRead: func(role, step string) {
			if role == f.role && step == "catalog-forecast" {
				calls++
				moveErr = os.Rename(ref.Path, held)
			}
		}})
		if moveErr != nil || calls != 1 {
			t.Fatal("forecast observation fault did not reach original declaration", kind, calls, moveErr)
		}
		if restoreErr := os.Rename(held, ref.Path); restoreErr != nil {
			t.Fatal(restoreErr)
		}
		if !errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "lost exact original progress") {
			t.Fatal("unobserved forecast was asserted to change original progress", kind, err)
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.checkpoint))) {
			t.Fatal("failed forecast observation changed custody", kind)
		}
		f.resume(t)
	}
}

// Actual named-inode replacement is still identity loss, even if replacement
// bytes match. Observation-only taxonomy must not relax this positive proof.
func TestMonitorHistoryConfirmedReplacementRetainsIdentityRefusal(t *testing.T) {
	for _, kind := range []string{"native-archive", "evm-archive", "native-catalog", "evm-catalog"} {
		f := newMonitorHistoryReadFixture(t, kind)
		prefix := "archive"
		if strings.HasSuffix(kind, "catalog") {
			prefix = "catalog"
		}
		held := filepath.Join(filepath.Dir(f.checkpoint), ".original-read-cause-retained")
		var mutationErr error
		calls := 0
		err := f.apply(f.ctx, monitorServiceHooks{historyRead: func(role, step string) {
			if role == f.role && step == prefix+"-original" {
				calls++
				mutationErr = os.Rename(f.checkpoint, held)
				if mutationErr == nil {
					mutationErr = os.WriteFile(f.checkpoint, f.original, 0600)
				}
			}
		}})
		if mutationErr != nil || calls != 1 {
			t.Fatal("replacement control did not reach retained owner", kind, calls, mutationErr)
		}
		if !errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, durablevolume.ErrUnavailable) {
			t.Fatal("confirmed named replacement was treated as transient", kind, err)
		}
		raw, readErr := os.ReadFile(held)
		if readErr != nil || !bytes.Equal(raw, f.original) {
			t.Fatal("identity refusal changed original bytes", kind, readErr)
		}
	}
}
