package miner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
)

// A release binary is stamped with WARP_VERSION, "<base>+<code>"
// (urnetwork/build all/run.sh), and its release is tagged with
// EXTERNAL_WARP_VERSION, "v<base>-<code>": docker tags can't hold a "+", so
// warpctl turns it into a "-" outside the binary. warpctl writes the base
// unpadded ("%d.%d.%d", so 2026.11.5 and 2026.9.30). These are the shapes of
// real releases: the latest one's provider --version prints
// 2026.10.27+1083160450, from the tag v2026.10.27-1083160450.
const (
	updaterTestCurrentStamp = "2026.10.27+1083160450"
	updaterTestCurrentTag   = "v2026.10.27-1083160450"
	updaterTestNewerTag     = "v2026.11.11-1096286830"
	updaterTestNewerStamp   = "2026.11.11+1096286830"
)

// setMinerVersion stamps this test binary the way the release build does.
func setMinerVersion(t *testing.T, version string) {
	t.Helper()
	previous := Version
	Version = version
	t.Setenv("WARP_VERSION", "")
	t.Cleanup(func() { Version = previous })
}

func mustParseMinerReleaseVersion(t *testing.T, version string) minerReleaseVersion {
	t.Helper()
	parsed, ok := parseMinerReleaseVersion(version)
	if !ok {
		t.Fatalf("%q isn't read as a release version", version)
	}
	return parsed
}

func TestReleaseVersionsReadTheBinaryStamp(t *testing.T) {
	current := mustParseMinerReleaseVersion(t, updaterTestCurrentStamp)
	latest := mustParseMinerReleaseVersion(t, updaterTestNewerTag)
	if !latest.newerThan(current) || current.newerThan(latest) {
		t.Fatalf("%s must be newer than %s", updaterTestNewerTag, updaterTestCurrentStamp)
	}
	// release tags keep the "-", with or without the v
	for _, tag := range []string{updaterTestCurrentTag, "2026.10.27-1083160450", "v2026.10.9-1067985620"} {
		mustParseMinerReleaseVersion(t, tag)
	}
}

// One release has two spellings, its tag's and its binary's: they are the
// same version, so neither is newer than the other.
func TestReleaseTagAndStampAreOneVersion(t *testing.T) {
	for _, pair := range [][2]string{
		{updaterTestNewerStamp, "2026.11.11-1096286830"},
		{updaterTestNewerStamp, updaterTestNewerTag},
		{updaterTestCurrentStamp, updaterTestCurrentTag},
		{"2026.11.5+1093000000", "v2026.11.05-1093000000"},
	} {
		a := mustParseMinerReleaseVersion(t, pair[0])
		b := mustParseMinerReleaseVersion(t, pair[1])
		if a.newerThan(b) || b.newerThan(a) {
			t.Fatalf("%s and %s are one version, but one reads as newer", pair[0], pair[1])
		}
	}
}

// Dates order by year, month and day as numbers, whatever their padding;
// on the same date the build number decides.
func TestReleaseVersionsOrderUnpaddedDates(t *testing.T) {
	ascending := []string{
		"2026.1.2-5",
		"2026.9.30+1060000000",
		"v2026.10.1-1060500000",
		"v2026.10.9-1067985620", // a real tag
		updaterTestCurrentStamp,
		"2026.11.5-1093000000",
		"2026.11.11-1096286829",
		updaterTestNewerStamp,
		"2026.11.11-1096286831",
		// across dates the date decides, even over a smaller build number
		"2026.11.12+1",
		"2027.1.1-1",
	}
	versions := make([]minerReleaseVersion, len(ascending))
	for i, version := range ascending {
		versions[i] = mustParseMinerReleaseVersion(t, version)
	}
	for i := range versions {
		for j := range versions {
			if got := versions[i].newerThan(versions[j]); got != (j < i) {
				t.Errorf("%s newer than %s: %v", ascending[i], ascending[j], got)
			}
		}
	}
}

func TestDevelopmentBuildsAreNoReleases(t *testing.T) {
	for _, dev := range []string{
		"", "(development build)", "development build", "dev", "0.0.0-dev", "local",
		"2026.11.11", "2026.11.11-", "2026.11.11+", "2026.11.11-dev", "2026.11.11+dev",
		"2026.11.11-beta+1096286830", "2026.11.11-beta-1096286830",
		"2026.10.21-friendly-cli-d4d7e06", "v2026.11.11-1096286830-dirty", "2026.11.11+1096286830.dirty",
		"2026.11.11+-1096286830", "2026.11.11 1096286830", "26.11.11-1", "2026.111.1-1", "2026.11.111-1",
		"V2026.11.11-1096286830", "vv2026.11.11-1096286830",
	} {
		if _, ok := parseMinerReleaseVersion(dev); ok {
			t.Errorf("%q parsed as a release", dev)
		}
	}
	// and a development build still never replaces itself from the real feed
	phase2State(t)
	for _, version := range []string{"", "2026.11.11-dev"} {
		setMinerVersion(t, version)
		available, why := minerUpdateAvailable(&minerRelease{TagName: updaterTestNewerTag})
		if available || !strings.Contains(why, "development build") {
			t.Fatalf("%q: available=%v why=%q", version, available, why)
		}
	}
}

// updaterTestServices answers like a device without a service manager.
func updaterTestServices(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cliRun = func(string, ...string) (string, error) { return "", fmt.Errorf("no service manager in tests") }
	t.Cleanup(func() { cliRun = defaultCliRun })
}

func TestTheUpdaterTakesAStampedBuildForARelease(t *testing.T) {
	phase2State(t)
	updaterTestServices(t)
	setMinerVersion(t, updaterTestCurrentStamp)
	if available, why := minerUpdateAvailable(&minerRelease{TagName: updaterTestNewerTag}); !available {
		t.Fatalf("%s doesn't update %s: %s", updaterTestNewerTag, updaterTestCurrentStamp, why)
	}
	for _, tag := range []string{updaterTestCurrentTag, "v2026.10.9-1067985620"} {
		if available, why := minerUpdateAvailable(&minerRelease{TagName: tag}); available || why != "" {
			t.Fatalf("%s on %s: available=%v why=%q, want up to date", tag, updaterTestCurrentStamp, available, why)
		}
	}

	// provider status
	report, _ := buildMinerStatusReport(time.Now())
	if !report.Update.Auto || report.Update.Why != "" {
		t.Fatalf("status reads a stamped build's updates as %+v", report.Update)
	}
	status := func() string {
		var out bytes.Buffer
		statusCmd(docopt.Opts{}, &out)
		return out.String()
	}
	if got := status(); strings.Contains(got, "development build") || !strings.Contains(got, "Updates") || !strings.Contains(got, "automatic") {
		t.Fatalf("status of a stamped build:\n%s", got)
	}
	state := readMinerUpdateState()
	state.Latest = strings.TrimPrefix(updaterTestCurrentTag, "v")
	writeMinerUpdateState(state)
	if got := status(); !strings.Contains(got, updaterTestCurrentStamp+" is the latest") || strings.Contains(got, "is out") {
		t.Fatalf("status with the running version's tag as the latest:\n%s", got)
	}
	state.Latest = strings.TrimPrefix(updaterTestNewerTag, "v")
	writeMinerUpdateState(state)
	if got := status(); !strings.Contains(got, state.Latest+" is out; it installs automatically") {
		t.Fatalf("status with a newer release out:\n%s", got)
	}

	// provider update --status
	var out bytes.Buffer
	printMinerUpdateStatus(&out, readMinerUpdateState())
	if got := out.String(); strings.Contains(got, "never") || strings.Contains(got, "development build") {
		t.Fatalf("update --status of a stamped build:\n%s", got)
	}

	// provider doctor, off the network
	fakeExecutable(t)
	previous := doctorNetwork
	doctorNetwork = func(string) []doctorCheck { return []doctorCheck{{true, "Network", "skipped in tests"}} }
	defer func() { doctorNetwork = previous }()
	out.Reset()
	doctorCmd(docopt.Opts{}, &out)
	if got := out.String(); !strings.Contains(got, "Updates") || !strings.Contains(got, "is writable") {
		t.Fatalf("doctor doesn't check a stamped build's updates:\n%s", got)
	}
}

// updaterTestArchive is a release archive of every platform holding a
// provider script that prints stamp, as the release pipeline lays it out.
func updaterTestArchive(t *testing.T, stamp string) []byte {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for _, platform := range []string{"linux/arm", runtime.GOOS + "/" + runtime.GOARCH, "windows/amd64"} {
		script := []byte("#!/bin/sh\necho '" + stamp + "'\n")
		if platform != runtime.GOOS+"/"+runtime.GOARCH {
			script = []byte("not this one")
		}
		if err := tw.WriteHeader(&tar.Header{Name: platform + "/provider", Mode: 0755, Size: int64(len(script)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(script)
	}
	tw.Close()
	gz.Close()
	return archive.Bytes()
}

func updaterTestFeed(tag string, publishedAt time.Time, name string, location string, archive []byte) []byte {
	sum := sha256.Sum256(archive)
	feed, _ := json.Marshal(map[string]any{
		"tag_name":     tag,
		"published_at": publishedAt.UTC().Format(time.RFC3339),
		"assets": []any{map[string]any{
			"name":                 name,
			"digest":               "sha256:" + hex.EncodeToString(sum[:]),
			"size":                 len(archive),
			"browser_download_url": location,
		}},
	})
	return feed
}

// The new binary prints its "+" stamp; its release is tagged with a "-".
// That is the release it claims to be. Another version is still refused.
func TestInstallAcceptsTheStampOfItsTag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	phase2State(t)
	for stamp, want := range map[string]bool{
		updaterTestNewerStamp:   true,
		"2026.11.11-1096286830": true,
		"2026.11.10+1096286830": false,
		"2026.11.11+1096286831": false,
		"(development build)":   false,
	} {
		executable := fakeExecutable(t)
		dir := t.TempDir()
		name := "urnetwork-provider-" + strings.TrimPrefix(updaterTestNewerTag, "v") + ".tar.gz"
		archive := updaterTestArchive(t, stamp)
		os.WriteFile(filepath.Join(dir, name), archive, 0644)
		os.WriteFile(filepath.Join(dir, "release.json"), updaterTestFeed(updaterTestNewerTag, time.Now(), name, name, archive), 0644)
		t.Setenv("URNETWORK_UPDATE_URL", filepath.Join(dir, "release.json"))
		release, err := fetchMinerRelease(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		err = installMinerRelease(t.Context(), release, executable)
		if raw, _ := os.ReadFile(executable); (err == nil) != want || strings.Contains(string(raw), stamp) != want {
			t.Fatalf("a binary that says %s for %s: installed=%v err=%v", stamp, updaterTestNewerTag, strings.Contains(string(raw), stamp), err)
		}
	}
}

// updaterTestTransport is GitHub for the updater: the release feed and its
// download, and nothing else, so no test reaches the network.
type updaterTestTransport struct {
	t     *testing.T
	files map[string][]byte
}

func (self *updaterTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, ok := self.files[request.URL.String()]
	if !ok {
		self.t.Errorf("the updater asked for %s", request.URL)
		return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request}, nil
}

// updaterTestService is the miner's installed service. A restart starts
// whatever binary is at executable now, which reports the version that
// binary prints, connected or not as connects says.
type updaterTestService struct {
	t          *testing.T
	executable string
	connects   func(version string) bool
	restarts   int
}

func (self *updaterTestService) kind() string                            { return "test service" }
func (self *updaterTestService) installed() bool                         { return true }
func (self *updaterTestService) active() bool                            { return true }
func (self *updaterTestService) install(string, string, io.Writer) error { return nil }
func (self *updaterTestService) uninstall(io.Writer) error               { return nil }
func (self *updaterTestService) logs(bool) error                         { return nil }
func (self *updaterTestService) foreign() string                         { return "" }
func (self *updaterTestService) configured() (string, string)            { return self.executable, "" }

func (self *updaterTestService) control(action string) error {
	if action != "restart" {
		self.t.Fatalf("the update asked the service to %s", action)
	}
	self.restarts++
	out, err := exec.Command(self.executable, "--version").Output()
	if err != nil {
		self.t.Fatal(err)
	}
	self.report(strings.TrimSpace(string(out)))
	return nil
}

// report writes the running miner's status file, as provider start does.
func (self *updaterTestService) report(version string) {
	now := time.Now().UTC()
	status := &minerLocalStatus{Schema: minerStatusSchema, Pid: os.Getpid(), Version: version, StartedAt: now, UpdatedAt: now,
		Members: []minerMemberStatus{{Slot: "direct", ClientId: "01a15cc0-0000-0000-0000-00000000c21f", Lifecycle: "running", Connected: self.connects(version)}}}
	if err := writeMinerLocalStatus(status); err != nil {
		self.t.Fatal(err)
	}
}

// updaterTestDevice is a device whose service runs the latest release's
// binary (stamped 2026.10.27+1083160450), with the real feed's newest
// release, v2026.11.11-1096286830, published two days ago.
func updaterTestDevice(t *testing.T, connects func(version string) bool) (*updaterTestService, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binaries are shell scripts")
	}
	phase2State(t)
	updaterTestServices(t)
	setMinerVersion(t, updaterTestCurrentStamp)
	executable := filepath.Join(t.TempDir(), "provider")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\necho "+updaterTestCurrentStamp+"\n"), 0755); err != nil {
		t.Fatal(err)
	}

	version := strings.TrimPrefix(updaterTestNewerTag, "v")
	name := "urnetwork-provider-" + version + ".tar.gz"
	location := "https://github.com/urnetwork/build/releases/download/" + updaterTestNewerTag + "/" + name
	archive := updaterTestArchive(t, updaterTestNewerStamp)
	transport := &updaterTestTransport{t: t, files: map[string][]byte{
		minerUpdateDefaultSource: updaterTestFeed(updaterTestNewerTag, time.Now().Add(-48*time.Hour), name, location, archive),
		location:                 archive,
	}}
	client := minerUpdateClient
	minerUpdateClient = &http.Client{Transport: transport, CheckRedirect: client.CheckRedirect}
	t.Cleanup(func() { minerUpdateClient = client })

	service := &updaterTestService{t: t, executable: executable, connects: connects}
	service.report(updaterTestCurrentStamp)
	readRunning := readMinerRunningState
	readMinerRunningState = func() minerRunningState {
		return minerRunningState{running: true, connected: true, service: service}
	}
	t.Cleanup(func() { readMinerRunningState = readRunning })

	window, poll := minerUpdateHealthWindow, minerUpdateHealthPoll
	minerUpdateHealthWindow, minerUpdateHealthPoll = 300*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { minerUpdateHealthWindow, minerUpdateHealthPoll = window, poll })
	return service, executable
}

func binaryVersion(t *testing.T, executable string) string {
	t.Helper()
	out, err := exec.Command(executable, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// The timer's provider update --auto on a stamped release installs the next
// release from the real feed, restarts the service and keeps it once it
// comes back connected as its "+" stamp.
func TestAutoUpdateInstallsTheNextReleaseOnAStampedBuild(t *testing.T) {
	service, executable := updaterTestDevice(t, func(string) bool { return true })
	var out bytes.Buffer
	autoUpdate(&out, readMinerUpdateState(), executable)
	if got := binaryVersion(t, executable); got != updaterTestNewerStamp {
		t.Fatalf("running %s after the update, want %s:\n%s", got, updaterTestNewerStamp, out.String())
	}
	state := readMinerUpdateState()
	if state.HeldBack != "" || state.Updated == "" || !strings.HasPrefix(state.LastResult, "updated to") || service.restarts != 1 {
		t.Fatalf("state %+v, %d restarts:\n%s", state, service.restarts, out.String())
	}
	// the timer's next check runs the new binary, which finds itself up to date
	setMinerVersion(t, binaryVersion(t, executable))
	out.Reset()
	autoUpdate(&out, readMinerUpdateState(), executable)
	if got := readMinerUpdateState().LastResult; got != "up to date" || service.restarts != 1 {
		t.Fatalf("second check: %q, %d restarts:\n%s", got, service.restarts, out.String())
	}
}

// A stamped release whose next version doesn't come back connected within
// the health window is rolled back, and that version is skipped after.
func TestAutoUpdateRollsBackOnAStampedBuild(t *testing.T) {
	service, executable := updaterTestDevice(t, func(version string) bool { return version != updaterTestNewerStamp })
	var out bytes.Buffer
	autoUpdate(&out, readMinerUpdateState(), executable)
	if got := binaryVersion(t, executable); got != updaterTestCurrentStamp {
		t.Fatalf("running %s after a failed update, want %s back:\n%s", got, updaterTestCurrentStamp, out.String())
	}
	state := readMinerUpdateState()
	if service.restarts != 2 || !strings.Contains(state.LastResult, "rolled back") || state.HeldUntil == nil || state.HeldBack == "" {
		t.Fatalf("state %+v, %d restarts:\n%s", state, service.restarts, out.String())
	}
	if !strings.Contains(out.String(), "didn't come back connected within") {
		t.Fatalf("no rollback notice:\n%s", out.String())
	}
	// the rolled back version is skipped, not retried at the next check
	out.Reset()
	autoUpdate(&out, readMinerUpdateState(), executable)
	if !strings.Contains(out.String(), "rolled back; skipped until") || service.restarts != 2 || binaryVersion(t, executable) != updaterTestCurrentStamp {
		t.Fatalf("second check after a rollback, %d restarts:\n%s", service.restarts, out.String())
	}
}

// A held back version is skipped in either spelling.
func TestAutoUpdateSkipsTheStampOfAHeldBackVersion(t *testing.T) {
	phase2State(t)
	updaterTestServices(t)
	t.Setenv("URNETWORK_UPDATE_URL", fakeRelease(t, "2026.10.22-6", false, true))
	until := time.Now().Add(time.Hour)
	var out bytes.Buffer
	autoUpdate(&out, minerUpdateState{HeldBack: "2026.10.22+6", HeldUntil: &until}, "/nonexistent/provider")
	if !strings.Contains(out.String(), "rolled back; skipped") {
		t.Fatalf("%q", out.String())
	}
}
