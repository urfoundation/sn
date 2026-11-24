package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
)

// A release feed of one release, published at publishedAt, in a file the
// update source (URNETWORK_UPDATE_URL) names.
func minerTestReleaseFeed(t *testing.T, tag string, publishedAt time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "release.json")
	raw, err := json.Marshal(map[string]any{"tag_name": tag, "published_at": publishedAt.UTC().Format(time.RFC3339), "assets": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("URNETWORK_UPDATE_URL", path)
}

// Who updates a miner decides what its operator is told of a newer release:
// a container's never updates itself, an updater that runs is trusted until
// it is late, and a miner nothing updates says so. The stale image that ran
// for months behind the releases is the container case.
func TestMinerUpdateFindingDecidesWhoUpdates(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	settings := defaultMinerReleaseCheckSettings()
	fresh := &minerRelease{TagName: updaterTestNewerTag, PublishedAt: now.Add(-2 * time.Hour)}
	late := &minerRelease{TagName: updaterTestNewerTag, PublishedAt: now.Add(-3 * 24 * time.Hour)}
	installer := &minerInstaller{tools: "/opt/urnetwork/urnet-tools"}
	for _, c := range []struct {
		name    string
		current string
		release *minerRelease
		updater minerUpdater
		// "" for no finding, else text the fix must hold
		wantFix string
	}{
		{name: "docker", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{container: minerContainerDocker}, wantFix: "docker compose pull && docker compose up -d"},
		{name: "docker pull", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{container: minerContainerDocker}, wantFix: "docker pull " + minerCommunityProviderImage},
		{name: "podman", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{container: minerContainerPodman}, wantFix: "podman pull " + minerCommunityProviderImage},
		{name: "kubernetes", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{container: minerContainerKubernetes}, wantFix: "bringyour/community-provider:" + strings.TrimPrefix(updaterTestNewerTag, "v")},
		{name: "installer, in time", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{installer: installer, installerAuto: true}},
		{name: "installer, late", current: updaterTestCurrentStamp, release: late, updater: minerUpdater{installer: installer, installerAuto: true}, wantFix: "urnet-tools update"},
		{name: "installer off", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{installer: installer}, wantFix: "provider update --on"},
		{name: "off", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{off: "turned off with provider update --off", timer: true}, wantFix: "provider update --on"},
		{name: "timer, in time", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{timer: true}},
		{name: "timer, late", current: updaterTestCurrentStamp, release: late, updater: minerUpdater{timer: true}, wantFix: "provider update"},
		{name: "by hand", current: updaterTestCurrentStamp, release: fresh, updater: minerUpdater{}, wantFix: "provider service install"},
		{name: "the latest", current: updaterTestNewerStamp, release: fresh, updater: minerUpdater{container: minerContainerDocker}},
		{name: "newer than the latest", current: "2026.12.1+1110000000", release: fresh, updater: minerUpdater{}},
		{name: "development build", current: "(development build)", release: fresh, updater: minerUpdater{}},
	} {
		finding := minerUpdateFinding(c.current, c.release, c.updater, minerCommunityProviderImage, now, settings)
		if c.wantFix == "" {
			if finding != nil {
				t.Errorf("%s: %+v, want nothing", c.name, finding)
			}
			continue
		}
		if finding == nil {
			t.Errorf("%s: nothing, want a finding", c.name)
			continue
		}
		if finding.Code != minerFindingUpdateAvailable || !strings.Contains(finding.Title, strings.TrimPrefix(updaterTestNewerTag, "v")+" is out") || !strings.Contains(finding.Title, c.current) || !strings.Contains(finding.Fix, c.wantFix) {
			t.Errorf("%s: %+v", c.name, finding)
		}
	}
}

// One check sets the finding while the build is behind, clears it once it
// isn't, and changes nothing when the feed can't be read.
func TestMinerReleaseCheckSetsAndClearsItsFinding(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	latest := &minerRelease{TagName: updaterTestNewerTag, PublishedAt: now.Add(-time.Hour)}
	var fetchErr error
	reports := []minerFinding{}
	findings := newMinerFindings()
	check := &minerReleaseCheck{
		current:  updaterTestCurrentStamp,
		fetch:    func(context.Context) (*minerRelease, error) { return latest, fetchErr },
		updater:  func() minerUpdater { return minerUpdater{container: minerContainerDocker} },
		image:    "<image>",
		now:      func() time.Time { return now },
		findings: findings,
		report:   func(finding minerFinding) { reports = append(reports, finding) },
		settings: defaultMinerReleaseCheckSettings(),
	}
	check.check(t.Context())
	if current := findings.current(); len(current) != 1 || current[0].Code != minerFindingUpdateAvailable || len(reports) != 1 {
		t.Fatalf("behind: findings %+v, reports %d", current, len(reports))
	}
	fetchErr = errors.New("synthetic feed failure")
	check.check(t.Context())
	if current := findings.current(); len(current) != 1 || len(reports) != 1 {
		t.Fatalf("an unread feed changed the finding: %+v, reports %d", current, len(reports))
	}
	fetchErr = nil
	check.current = updaterTestNewerStamp
	check.check(t.Context())
	if current := findings.current(); len(current) != 0 || len(reports) != 1 {
		t.Fatalf("up to date: findings %+v, reports %d", current, len(reports))
	}
}

// The loop checks once FirstCheckDelay after it starts, and its stop joins it.
func TestMinerReleaseCheckLoopChecksAndJoins(t *testing.T) {
	reported := make(chan minerFinding, 1)
	check := &minerReleaseCheck{
		current: updaterTestCurrentStamp,
		fetch: func(context.Context) (*minerRelease, error) {
			return &minerRelease{TagName: updaterTestNewerTag, PublishedAt: time.Now()}, nil
		},
		updater:  func() minerUpdater { return minerUpdater{} },
		image:    "<image>",
		now:      time.Now,
		findings: newMinerFindings(),
		report:   func(finding minerFinding) { reported <- finding },
		settings: &minerReleaseCheckSettings{FirstCheckDelay: 0, CheckInterval: time.Hour, AutomaticUpdateGrace: time.Hour},
	}
	stop := check.start(t.Context())
	select {
	case finding := <-reported:
		if !strings.Contains(finding.Fix, "provider service install") {
			t.Fatalf("finding %+v", finding)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the release check never checked")
	}
	stop()
}

// A writer that closes said once a write holds text.
type minerTestSignalWriter struct {
	stateLock sync.Mutex
	buffer    bytes.Buffer
	text      string
	said      chan struct{}
	closed    bool
}

func (self *minerTestSignalWriter) Write(raw []byte) (int, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.buffer.Write(raw)
	if !self.closed && strings.Contains(self.buffer.String(), self.text) {
		self.closed = true
		close(self.said)
	}
	return len(raw), nil
}

func (self *minerTestSignalWriter) String() string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.buffer.String()
}

// doctor in a container whose state directory is in the container's own
// filesystem, behind the latest release: it fails both rows with their fixes,
// and says the image, not the binary, updates it. On a volume and up to date,
// both rows pass.
func TestDoctorTellsAContainerOfItsIdentityAndVersion(t *testing.T) {
	dir := phase2State(t)
	updaterTestServices(t)
	setMinerVersion(t, updaterTestCurrentStamp)
	writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "synthetic-network"}))
	previousNetwork, previousRelease := doctorNetwork, doctorRelease
	t.Cleanup(func() { doctorNetwork, doctorRelease = previousNetwork, previousRelease })
	doctorNetwork = func(string) []doctorCheck { return []doctorCheck{{true, "Network", "skipped in tests"}} }
	latest := &minerRelease{TagName: updaterTestNewerTag, PublishedAt: time.Now().Add(-time.Hour)}
	doctorRelease = func(context.Context) (*minerRelease, error) { return latest, nil }
	// every encrypted DNS server answers (exit_dns_test.go)
	previousExitDns := minerExitDnsQuery
	minerExitDnsQuery = func(context.Context, string, string) bool { return true }
	t.Cleanup(func() { minerExitDnsQuery = previousExitDns })

	setMinerHostRoot(t, fakeMinerHost(t, map[string]string{".dockerenv": "", "proc/self/mountinfo": minerTestContainerMounts()}))
	var out bytes.Buffer
	code := doctorCmd(docopt.Opts{}, &out)
	got := out.String()
	for _, want := range []string{
		"✗ Identity     This device's identity is lost with its container",
		"no volume is mounted there",
		"docker volume create urnetwork-provider",
		"-v urnetwork-provider:" + dir,
		"– Updates      by a new image",
		"✗ Version      provider " + strings.TrimPrefix(updaterTestNewerTag, "v") + " is out; this miner runs " + updaterTestCurrentStamp,
		"docker compose pull && docker compose up -d",
		"2 problems found.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor lacks %q:\n%s", want, got)
		}
	}
	if code != 1 {
		t.Fatalf("doctor exit %d", code)
	}

	setMinerHostRoot(t, fakeMinerHost(t, map[string]string{".dockerenv": "", "proc/self/mountinfo": minerTestContainerMounts(minerTestMountLine(104, "/var/lib/docker/volumes/urnetwork-provider/_data", minerResolvedPath(dir), "ext4"))}))
	latest = &minerRelease{TagName: updaterTestCurrentTag, PublishedAt: time.Now().Add(-time.Hour)}
	out.Reset()
	code = doctorCmd(docopt.Opts{}, &out)
	got = out.String()
	for _, want := range []string{"✓ Identity     kept: " + dir + " is on its own mount", "✓ Version      " + updaterTestCurrentStamp + " is the latest release", "No problems found."} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor lacks %q:\n%s", want, got)
		}
	}
	if code != 0 {
		t.Fatalf("doctor exit %d:\n%s", code, got)
	}
}

// provider status shows the running miner's findings, and the state
// directory's as it is now, with doctor as the next step; --json carries them.
func TestStatusShowsTheRunningMinersFindings(t *testing.T) {
	dir := phase2State(t)
	updaterTestServices(t)
	setMinerVersion(t, updaterTestCurrentStamp)
	writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "synthetic-network"}))
	writeState(t, dir, ".provider.key", strings.Repeat("k", 32))
	writeState(t, dir, ".provider.jwt", fakeJwt(map[string]any{"client_id": "01a15cc0-0000-0000-0000-00000000c21f"}))
	setMinerHostRoot(t, fakeMinerHost(t, map[string]string{".dockerenv": "", "proc/self/mountinfo": minerTestContainerMounts()}))
	now := time.Now().UTC()
	update := minerUpdateFinding(updaterTestCurrentStamp, &minerRelease{TagName: updaterTestNewerTag, PublishedAt: now}, minerUpdater{container: minerContainerDocker}, "<image>", now, defaultMinerReleaseCheckSettings())
	status := &minerLocalStatus{Schema: minerStatusSchema, Pid: os.Getpid(), Version: updaterTestCurrentStamp, StartedAt: now, UpdatedAt: now,
		Members:  []minerMemberStatus{{Slot: "direct", ClientId: "01a15cc0-0000-0000-0000-00000000c21f", Lifecycle: "running", Connected: true}},
		Findings: []minerFinding{*update}}
	if err := writeMinerLocalStatus(status); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := statusCmd(docopt.Opts{}, &out); code != 0 {
		t.Fatalf("status exit %d:\n%s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"! Identity     This device's identity is lost with its container", "! Version      provider " + strings.TrimPrefix(updaterTestNewerTag, "v") + " is out", "– Updates      by a new image", "→ provider doctor"} {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q:\n%s", want, got)
		}
	}
	out.Reset()
	statusCmd(docopt.Opts{"--json": true}, &out)
	var report minerStatusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Container != minerContainerDocker || len(report.Findings) != 2 || report.Findings[0].Code != minerFindingStateNotPersistent || report.Findings[1].Code != minerFindingUpdateAvailable || report.State != "running" {
		t.Fatalf("json %+v", report)
	}
}
