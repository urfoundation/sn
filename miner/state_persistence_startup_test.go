//go:build linux || darwin

// Provider startup tells of a state directory lost with its container at the
// real run boundary: the real key custody, the real SDK registration path to a
// local synthetic server, and the run's own diagnostics and status file.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/operatorlist"
	"github.com/urnetwork/connect"
)

// One decoded diagnostics record.
type providerTestDiagnosticRecord struct {
	Domain   string `json:"domain"`
	Event    string `json:"event"`
	Guidance string `json:"guidance"`
	File     string `json:"file"`
}

// Every record the run wrote, after it returned and joined its exporter.
func drainProviderTestRecords(t *testing.T, recorder *providerDiagnosticRecorder) []providerTestDiagnosticRecord {
	t.Helper()
	records := []providerTestDiagnosticRecord{}
	for {
		select {
		case raw := <-recorder.records:
			var record providerTestDiagnosticRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			records = append(records, record)
		default:
			return records
		}
	}
}

func findProviderTestRecord(records []providerTestDiagnosticRecord, event string) *providerTestDiagnosticRecord {
	for i := range records {
		if records[i].Event == event {
			return &records[i]
		}
	}
	return nil
}

// A log the run writes its finding blocks to. Safe for concurrent use.
type providerTestFindingLog struct {
	stateLock sync.Mutex
	buffer    bytes.Buffer
}

func (self *providerTestFindingLog) Write(raw []byte) (int, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.buffer.Write(raw)
}

func (self *providerTestFindingLog) String() string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.buffer.String()
}

// The fleet that came back as new devices about every 45 minutes ran the
// provider in Docker with no volume at its state directory: each new
// container registered a new provider client, key and extender key, and its
// logs never said why. Startup now says so the moment it registers the new
// device, in the log, the diagnostics and the status file, with the volume
// that fixes it; a restart of the same container still warns; a volume at
// the state directory silences it.
func TestProviderStartupTellsOfStateLostWithItsContainer(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	t.Setenv(minerStateSubstitutedDirEnv, "")
	t.Setenv(minerStateSubstitutedProblemEnv, "")
	container := fakeMinerHost(t, map[string]string{
		".dockerenv":          "",
		"proc/self/mountinfo": minerTestContainerMounts(),
	})
	stop := errors.New("synthetic stop after authentication")
	hooks := providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error { return stop }}
	start := func(root string, allow bool) ([]providerTestDiagnosticRecord, string) {
		t.Helper()
		settings := fixture.settings(allow)
		settings.hostRoot = root
		findingLog := &providerTestFindingLog{}
		settings.findingLog = findingLog
		recorder := &providerDiagnosticRecorder{records: make(chan []byte, 64)}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		if err := settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), recorder); !errors.Is(err, stop) {
			t.Fatal(err)
		}
		return drainProviderTestRecords(t, recorder), findingLog.String()
	}

	// the first start in a new container registers a new device
	records, log := start(container, true)
	record := findProviderTestRecord(records, minerFindingStateNotPersistent)
	if record == nil {
		t.Fatalf("startup in a container's filesystem said nothing of it: %+v", records)
	}
	if record.Domain != "keys" || record.File != fixture.dir {
		t.Fatalf("record %+v", record)
	}
	for _, want := range []string{"This device's identity is lost with its container", fixture.dir, "This start registered a new device", "docker volume create urnetwork-provider", "-v urnetwork-provider:" + fixture.dir} {
		if !strings.Contains(record.Guidance, want) {
			t.Errorf("guidance lacks %q: %s", want, record.Guidance)
		}
	}
	if !strings.Contains(log, "provider: ! This device's identity is lost with its container.") || !strings.Contains(log, "provider: !   docker volume create urnetwork-provider") {
		t.Fatalf("log:\n%s", log)
	}
	status, err := readMinerLocalStatus()
	if err != nil || len(status.Findings) != 1 || status.Findings[0].Code != minerFindingStateNotPersistent || !strings.Contains(status.Findings[0].Fix, "urnetwork-provider:"+fixture.dir) {
		t.Fatalf("status file findings: %+v (%v)", status, err)
	}
	posts, _, allocated, _ := fixture.counts()
	if posts != 1 || allocated != 1 {
		t.Fatalf("the warning changed registration: posts=%d allocated=%d", posts, allocated)
	}

	// a restart of the same container keeps its device, and still warns
	records, log = start(container, false)
	record = findProviderTestRecord(records, minerFindingStateNotPersistent)
	if record == nil || strings.Contains(record.Guidance, "registered a new device") || !strings.Contains(log, "identity is lost with its container") {
		t.Fatalf("restart: record %+v, log:\n%s", record, log)
	}

	// a volume at the state directory keeps it: nothing is said
	volume := fakeMinerHost(t, map[string]string{
		".dockerenv":          "",
		"proc/self/mountinfo": minerTestContainerMounts(minerTestMountLine(104, "/var/lib/docker/volumes/urnetwork-provider/_data", minerResolvedPath(fixture.dir), "ext4")),
	})
	records, log = start(volume, false)
	if record := findProviderTestRecord(records, minerFindingStateNotPersistent); record != nil || log != "" {
		t.Fatalf("a state directory on a volume: record %+v, log:\n%s", record, log)
	}
	if status, err := readMinerLocalStatus(); err != nil || len(status.Findings) != 0 {
		t.Fatalf("status file findings on a volume: %+v (%v)", status, err)
	}
}

// An operator child of provide --all-operators leaves the block to its
// supervisor unless it registered a new device itself; its record and status
// say it either way.
func TestProviderStartupOperatorChildWritesItsBlockOnlyForANewDevice(t *testing.T) {
	dir := operatorlist.DomainStateDir(providerRegistrationPrivateDir(t), "operator.example")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := newProviderRegistrationFixtureIn(t, dir)
	t.Setenv(minerStateSubstitutedDirEnv, "")
	t.Setenv(minerStateSubstitutedProblemEnv, "")
	container := fakeMinerHost(t, map[string]string{
		".dockerenv":          "",
		"proc/self/mountinfo": minerTestContainerMounts(),
	})
	stop := errors.New("synthetic stop after authentication")
	hooks := providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error { return stop }}
	for _, c := range []struct {
		allow     bool
		wantBlock bool
	}{
		{allow: true, wantBlock: true},
		{allow: false, wantBlock: false},
	} {
		settings := fixture.settings(c.allow)
		settings.hostRoot = container
		settings.operatorChild = true
		findingLog := &providerTestFindingLog{}
		settings.findingLog = findingLog
		recorder := &providerDiagnosticRecorder{records: make(chan []byte, 64)}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		err := settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), recorder)
		cancel()
		if !errors.Is(err, stop) {
			t.Fatal(err)
		}
		if findProviderTestRecord(drainProviderTestRecords(t, recorder), minerFindingStateNotPersistent) == nil {
			t.Fatalf("allow=%v: an operator child recorded nothing", c.allow)
		}
		if got := strings.Contains(findingLog.String(), "identity is lost with its container"); got != c.wantBlock {
			t.Fatalf("allow=%v: block written %v, want %v:\n%s", c.allow, got, c.wantBlock, findingLog.String())
		}
	}
}

// The image that ran for months behind the releases never said so. A run in a
// container now checks the release feed, and a newer release is told in the
// log, the diagnostics and the status file, with the pull that updates it. The
// run's authentication waits until the check has told it, so the check can't
// lose the race to the run's end.
func TestProviderRunTellsAContainerOfANewerRelease(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	t.Setenv(minerStateSubstitutedDirEnv, "")
	t.Setenv(minerStateSubstitutedProblemEnv, "")
	setMinerVersion(t, updaterTestCurrentStamp)
	minerTestReleaseFeed(t, updaterTestNewerTag, time.Now().Add(-time.Hour))
	// on a volume, so the state directory says nothing
	volume := fakeMinerHost(t, map[string]string{
		".dockerenv":          "",
		"proc/self/mountinfo": minerTestContainerMounts(minerTestMountLine(104, "/var/lib/docker/volumes/urnetwork-provider/_data", minerResolvedPath(fixture.dir), "ext4")),
	})
	findingLog := &minerTestSignalWriter{text: "docker compose pull", said: make(chan struct{})}
	stop := errors.New("synthetic stop after authentication")
	hooks := providerRegistrationHooks{afterAuthenticated: func(string, connect.Id, []byte) error {
		select {
		case <-findingLog.said:
		case <-time.After(10 * time.Second):
			t.Error("the release check never told the log")
		}
		return stop
	}}
	settings := fixture.settings(true)
	settings.hostRoot = volume
	settings.findingLog = findingLog
	settings.releaseCheck = &minerReleaseCheckSettings{FirstCheckDelay: 0, CheckInterval: time.Hour, AutomaticUpdateGrace: time.Hour}
	recorder := &providerDiagnosticRecorder{records: make(chan []byte, 64)}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), recorder); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	log := findingLog.String()
	newer := strings.TrimPrefix(updaterTestNewerTag, "v")
	for _, want := range []string{"provider: ! provider " + newer + " is out; this miner runs " + updaterTestCurrentStamp + ".", "provider: !   With compose: docker compose pull && docker compose up -d", "docker pull <image>"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	records := drainProviderTestRecords(t, recorder)
	if record := findProviderTestRecord(records, minerFindingUpdateAvailable); record == nil || record.Domain != "runtime" || !strings.Contains(record.Guidance, "docker compose pull") {
		t.Fatalf("records %+v", records)
	}
	if record := findProviderTestRecord(records, minerFindingStateNotPersistent); record != nil {
		t.Fatalf("a state directory on a volume reported %+v", record)
	}
	status, err := readMinerLocalStatus()
	if err != nil || len(status.Findings) != 1 || status.Findings[0].Code != minerFindingUpdateAvailable {
		t.Fatalf("status file findings: %+v (%v)", status, err)
	}
}
