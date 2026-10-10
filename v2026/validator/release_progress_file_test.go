//go:build linux || darwin

// The actual publisher owns filesystem operations and joined cancellation.
// Barriers expose failed, blocked and ambiguous writes without timed sleeps.
package validator

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Each wait follows a completed publication attempt. Tests explicitly release
// the next attempt; no shorter sleep stands in for a worker lifecycle event.
type releaseProgressPublisherTest struct {
	owner   *releaseProgressPublisher
	waits   chan time.Duration
	resume  chan struct{}
	reports chan string
}

// Start the production run loop with only physical-sync and wait observers.
func newReleaseProgressPublisherTest(t testing.TB, progress *releaseProgress, path string, syncDirectory func(*os.File) error) *releaseProgressPublisherTest {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	self := &releaseProgressPublisherTest{waits: make(chan time.Duration), resume: make(chan struct{}), reports: make(chan string, 32)}
	self.owner = &releaseProgressPublisher{ctx: ctx, cancel: cancel, done: make(chan struct{}), progress: progress,
		path: path, stateDir: filepath.Join(filepath.Dir(path), "protocol-state"), sync: syncDirectory,
		report: func(value string) { self.reports <- value },
		wait: func(ctx context.Context, duration time.Duration) bool {
			select {
			case self.waits <- duration:
			case <-ctx.Done():
				return false
			}
			select {
			case <-self.resume:
				return true
			case <-ctx.Done():
				return false
			}
		}}
	go self.owner.run()
	t.Cleanup(self.owner.close)
	return self
}

// A canceled test releases every wait; the test deadline remains only a hang
// guard, never the proof that a publication did or did not occur.
func (self *releaseProgressPublisherTest) attempted(t testing.TB) time.Duration {
	t.Helper()
	select {
	case duration := <-self.waits:
		return duration
	case <-self.owner.done:
		t.Fatal("publisher exited before the expected publication boundary")
		return 0
	case <-t.Context().Done():
		t.Fatal("test canceled before publication boundary")
		return 0
	}
}

// Existing protected directories keep ownership tests independent of umask.
func releaseProgressFileTestPath(t testing.TB) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, "progress.json")
}

// Read the actual visible textfile and its strict bounded decoder.
func releaseProgressFileTestRead(t testing.TB, path string) *protocol.ValidatorProgress {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := protocol.DecodeValidatorProgress(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// An initial unavailable output retries with a capped backoff while the core
// intent owner continues. Repair allows a complete fresh publication.
func TestReleaseProgressPublisherInitialOutageBackoffAndRepair(t *testing.T) {
	progress, clock := newReleaseProgressTest(t)
	path := filepath.Join(filepath.Dir(releaseProgressFileTestPath(t)), "initially-missing", "progress.json")
	publisher := newReleaseProgressPublisherTest(t, progress, path, nil)
	store := intentV2PublicationTest(t)
	store.v2.runtime.progress = progress
	for index, expected := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute} {
		if delay := publisher.attempted(t); delay != expected {
			t.Fatalf("attempt %d delay %v, want %v", index, delay, expected)
		}
		if report := <-publisher.reports; report != "publication_unavailable_retrying" {
			t.Fatal("outage leaked an arbitrary error or disappeared", report)
		}
		clock.set(clock.now().Add(time.Minute))
		if _, err := store.currentV2(t.Context()); err != nil || t.Context().Err() != nil {
			t.Fatal("publication outage stopped the core owner", err)
		}
		if index != 7 {
			publisher.resume <- struct{}{}
		}
	}
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	publisher.resume <- struct{}{}
	if delay := publisher.attempted(t); delay != releaseProgressHeartbeat || <-publisher.reports != "publication_recovered" {
		t.Fatal("repaired output did not recover its ordinary heartbeat", delay)
	}
	value := releaseProgressFileTestRead(t, path)
	if value.Intent == nil || !value.Intent.Current || value.HeartbeatAt != clock.now().Format(time.RFC3339Nano) {
		t.Fatal("repaired output did not contain the real core observation")
	}
}

// A failure after the real directory Sync is an ambiguous successful rename.
// Its exact visible bytes remain the next owned predecessor and can recover.
func TestReleaseProgressPublisherAmbiguousWriteRetainsExactSuccessor(t *testing.T) {
	progress, clock := newReleaseProgressTest(t)
	path := releaseProgressFileTestPath(t)
	attempt := 0
	publisher := newReleaseProgressPublisherTest(t, progress, path, func(directory *os.File) error {
		attempt++
		err := directory.Sync()
		if attempt == 1 {
			return errors.Join(err, errors.New("synthetic post-sync failure"))
		}
		return err
	})
	if publisher.attempted(t) != time.Second || <-publisher.reports != "publication_unavailable_retrying" {
		t.Fatal("ambiguous write was silently called a success")
	}
	before := releaseProgressFileTestRead(t, path)
	clock.set(clock.now().Add(time.Minute))
	publisher.resume <- struct{}{}
	if publisher.attempted(t) != releaseProgressHeartbeat || <-publisher.reports != "publication_recovered" {
		t.Fatal("exact visible postimage could not recover")
	}
	after := releaseProgressFileTestRead(t, path)
	if after.InstanceId != before.InstanceId || after.HeartbeatAt == before.HeartbeatAt || after.Intent != nil {
		t.Fatal("ambiguous write invented protocol progress or lost its owner")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 2 {
		t.Fatal("publisher retained unbounded temporary outputs", err)
	}
}

// Repeated post-rename failures can leave a fresh visible file. Its previous
// acknowledged publication remains old and the retry status stays explicit.
func TestReleaseProgressAmbiguousWritesCannotRefreshPublicationSuccess(t *testing.T) {
	progress, clock := newReleaseProgressTest(t)
	path := releaseProgressFileTestPath(t)
	attempt := 0
	publisher := newReleaseProgressPublisherTest(t, progress, path, func(directory *os.File) error {
		attempt++
		err := directory.Sync()
		if attempt >= 3 {
			return errors.Join(err, errors.New("synthetic repeated durability uncertainty"))
		}
		return err
	})
	publisher.attempted(t)
	clock.set(clock.now().Add(time.Minute))
	publisher.resume <- struct{}{}
	publisher.attempted(t)
	acknowledged := clock.now().Format(time.RFC3339Nano)
	clock.set(clock.now().Add(time.Minute))
	publisher.resume <- struct{}{}
	if publisher.attempted(t) != time.Second || <-publisher.reports != "publication_unavailable_retrying" {
		t.Fatal("first uncertain write was not visible")
	}
	clock.set(clock.now().Add(time.Minute))
	publisher.resume <- struct{}{}
	if publisher.attempted(t) != 2*time.Second || <-publisher.reports != "publication_unavailable_retrying" {
		t.Fatal("second uncertain write lost bounded retry")
	}
	value := releaseProgressFileTestRead(t, path)
	if value.Publisher.Outcome != "retrying" || value.Publisher.LastSuccessAt != acknowledged || value.HeartbeatAt == acknowledged {
		t.Fatal("fresh ambiguous output was reported as acknowledged publication")
	}
}

// A physically blocked directory sync holds no protocol owner. Cancellation
// requests shutdown immediately and joins the worker after that I/O returns.
func TestReleaseProgressBlockedExportDoesNotOwnCoreAndCancellationJoins(t *testing.T) {
	progress, _ := newReleaseProgressTest(t)
	path := releaseProgressFileTestPath(t)
	entered, resume := make(chan struct{}), make(chan struct{})
	publisher := newReleaseProgressPublisherTest(t, progress, path, func(directory *os.File) error {
		close(entered)
		<-resume
		return directory.Sync()
	})
	defer close(resume)
	<-entered
	store := intentV2PublicationTest(t)
	store.v2.runtime.progress = progress
	if _, err := store.currentV2(t.Context()); err != nil {
		t.Fatal("blocked export retained the actual intent owner", err)
	}
	if value := releaseProgressTestSnapshot(t, progress); value.Intent == nil || !value.Intent.Current {
		t.Fatal("blocked export held the observation lock")
	}
	publisher.owner.cancel()
	select {
	case <-publisher.owner.done:
		t.Fatal("cancellation detached the blocked filesystem worker")
	default:
	}
	// Cleanup runs after the explicit physical I/O release and joins this worker.
}

// A local replacement is an ownership fault, never permission to overwrite.
// It stops only the publisher; validation's actual store remains usable.
func TestReleaseProgressPublisherForeignReplacementDisablesOnlyExporter(t *testing.T) {
	progress, _ := newReleaseProgressTest(t)
	path := releaseProgressFileTestPath(t)
	publisher := newReleaseProgressPublisherTest(t, progress, path, nil)
	if publisher.attempted(t) != releaseProgressHeartbeat {
		t.Fatal("initial complete output failed")
	}
	foreign := []byte("synthetic foreign owner\n")
	if err := os.WriteFile(path, foreign, 0644); err != nil {
		t.Fatal(err)
	}
	publisher.resume <- struct{}{}
	<-publisher.owner.done
	if <-publisher.reports != "publisher_disabled_ownership" {
		t.Fatal("path replacement was not visibly disabled")
	}
	store := intentV2PublicationTest(t)
	store.v2.runtime.progress = progress
	if _, err := store.currentV2(t.Context()); err != nil || t.Context().Err() != nil {
		t.Fatal("disabled exporter canceled core validation", err)
	}
	if raw, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, foreign) {
		t.Fatal("publisher erased a foreign replacement", err)
	}
}

// Both names are ownership witnesses: neither a replaced directory nor a new
// lock inode can inherit an already-running publisher's authority.
func TestReleaseProgressPublisherRejectsChangedDirectoryOrLockIdentity(t *testing.T) {
	for _, replacement := range []string{"directory", "lock"} {
		progress, _ := newReleaseProgressTest(t)
		path := releaseProgressFileTestPath(t)
		publisher := newReleaseProgressPublisherTest(t, progress, path, nil)
		publisher.attempted(t)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if replacement == "directory" {
			directory := filepath.Dir(path)
			if err := os.Rename(directory, directory+"-displaced"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			before = []byte("synthetic replacement directory owner\n")
			if err := os.WriteFile(path, before, 0644); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Remove(path + ".lock"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".lock", nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		publisher.resume <- struct{}{}
		<-publisher.owner.done
		if report := <-publisher.reports; report != "publisher_disabled_ownership" || t.Context().Err() != nil {
			t.Fatal("replaced identity did not disable only its publisher", replacement, report)
		}
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
			t.Fatal("publisher followed a replacement identity", replacement, err)
		}
	}
}

// The next instance retains aged evidence while its domains remain unknown.
// Clock rollback and a new config identity cannot reset an old pending intent.
func TestReleaseProgressPublisherRestartRetainsOriginalIntentAge(t *testing.T) {
	progress, clock := newReleaseProgressTest(t)
	created := clock.now().Add(-time.Hour).Format(time.RFC3339Nano)
	progress.observeIntent(progress.nextSequence(), &protocol.ValidatorIntentProgress{
		ConfigHash: progress.value.Source.ConfigHash, VectorHash: "0x" + strings.Repeat("4", 64),
		NativeEpoch: 8, SettlementEpoch: 7, Status: "pending", CreatedAt: created, ProgressAt: created, RevealBlock: 120}, nil)
	path := releaseProgressFileTestPath(t)
	first := newReleaseProgressPublisherTest(t, progress, path, nil)
	first.attempted(t)
	previous := releaseProgressFileTestRead(t, path)
	first.owner.close()
	restarted, restartClock := newReleaseProgressTest(t)
	restartClock.set(clock.now().Add(-time.Minute))
	restarted.value.InstanceId = strings.Repeat("5", 32)
	restarted.value.Source.ConfigHash = "sha256:" + strings.Repeat("6", 64)
	second := newReleaseProgressPublisherTest(t, restarted, path, nil)
	second.attempted(t)
	after := releaseProgressFileTestRead(t, path)
	if after.Intent == nil || after.Intent.Current || after.Intent.Value.CreatedAt != created || after.Intent.Value.ProgressAt != created || after.Intent.LastSuccessAt != previous.Intent.LastSuccessAt || after.Intent.Value.ConfigHash != previous.Source.ConfigHash || after.Source.ConfigHash == previous.Source.ConfigHash || after.HeartbeatAt >= previous.HeartbeatAt {
		t.Fatal("restart reset age, rewrote original authority, or fabricated current success")
	}
}

// Malformed, oversized or foreign output is never replaced with healthy zeros.
func TestReleaseProgressFileRejectsForeignAndUnboundedExistingBytes(t *testing.T) {
	progress, _ := newReleaseProgressTest(t)
	foreign, err := progress.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	foreign = bytes.Replace(foreign, []byte("synthetic-progress"), []byte("different-synthetic-role"), 1)
	for _, raw := range [][]byte{[]byte("{}\n"), bytes.Repeat([]byte(" "), protocol.MaxValidatorProgressBytes+1), foreign} {
		path := releaseProgressFileTestPath(t)
		if err := os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
		owner, previous, err := openReleaseProgressFile(path, filepath.Join(filepath.Dir(path), "protocol-state"), progress.value.Source)
		var ownership *releaseProgressOwnershipError
		if owner != nil || previous != nil || !errors.As(err, &ownership) {
			t.Fatal("foreign existing output was admitted", err)
		}
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, raw) {
			t.Fatal("rejected output was altered", err)
		}
	}
}

// Role output is separate from private protocol state and has exactly one
// active publisher; aliases cannot redirect that ownership to another file.
func TestReleaseProgressFileRejectsProtocolPathsAliasesAndSecondOwner(t *testing.T) {
	progress, _ := newReleaseProgressTest(t)
	path := releaseProgressFileTestPath(t)
	owner, _, err := openReleaseProgressFile(path, filepath.Join(filepath.Dir(path), "protocol-state"), progress.value.Source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.close(); err != nil {
			t.Error(err)
		}
	})
	if second, _, err := openReleaseProgressFile(path, filepath.Join(filepath.Dir(path), "protocol-state"), progress.value.Source); err == nil || second != nil {
		t.Fatal("two publishers acquired the same output")
	}
	if inside, _, err := openReleaseProgressFile(path, filepath.Dir(path), progress.value.Source); err == nil || inside != nil {
		t.Fatal("publisher acquired a protocol-state path")
	}
	alias := filepath.Join(filepath.Dir(path), "alias.json")
	if err := os.Symlink("progress.json", alias); err != nil {
		t.Fatal(err)
	}
	if aliased, _, err := openReleaseProgressFile(alias, filepath.Join(filepath.Dir(path), "protocol-state"), progress.value.Source); err == nil || aliased != nil {
		t.Fatal("publisher followed a destination alias")
	}
}
