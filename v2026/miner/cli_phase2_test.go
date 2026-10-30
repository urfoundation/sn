package miner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/sdk/v2026"
)

// phase2State points the state directory at a fresh private folder.
func phase2State(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("URNETWORK_STATE_DIR", dir)
	t.Setenv("URNETWORK_UPDATE_URL", "")
	t.Setenv("URNETWORK_AUTO_UPDATE", "")
	t.Setenv("URNETWORK_SERVICE_NAME", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	return dir
}

func fakeJwt(claims map[string]any) string {
	segment := func(value any) string {
		raw, _ := json.Marshal(value)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return segment(map[string]string{"alg": "none", "typ": "JWT"}) + "." + segment(claims) + "." + segment("sig")
}

func writeState(t *testing.T, dir string, name string, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestProvidePreflight(t *testing.T) {
	dir := phase2State(t)
	var out, errOut bytes.Buffer
	opts := docopt.Opts{}
	if providePreflight(opts, true, nil, &out, &errOut) || !strings.Contains(errOut.String(), "provider login") {
		t.Fatalf("not signed in: %q", errOut.String())
	}
	errOut.Reset()
	if providePreflight(opts, false, nil, &out, &errOut) {
		t.Fatal("provide without a sign-in should not start")
	}
	writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "kitchen-pi"}))
	errOut.Reset()
	// start outside a terminal never registers on its own
	if providePreflight(docopt.Opts{}, true, nil, &out, &errOut) || !strings.Contains(errOut.String(), "--allow-client-registration") {
		t.Fatalf("fresh device: %q", errOut.String())
	}
	// provide keeps its own behaviour once signed in
	if !providePreflight(docopt.Opts{}, false, nil, &out, &errOut) {
		t.Fatal("provide refused a signed-in device")
	}
	if !providePreflight(docopt.Opts{"--allow-client-registration": true}, true, nil, &out, &errOut) {
		t.Fatal("the explicit flag should start")
	}
	writeState(t, dir, ".provider.key", strings.Repeat("k", 32))
	if !providePreflight(docopt.Opts{}, true, nil, &out, &errOut) {
		t.Fatal("a device with an identity should start")
	}
	// fleet launches are never second-guessed
	os.Remove(filepath.Join(dir, "jwt"))
	os.Remove(filepath.Join(dir, ".provider.key"))
	if !providePreflight(docopt.Opts{"--whole-work-capture": "profile.json"}, false, nil, &out, &errOut) {
		t.Fatal("a capture launch was stopped")
	}
}

func TestMinerStateReads(t *testing.T) {
	dir := phase2State(t)
	if readMinerSignIn().signedIn || readMinerDevice().hasHistory {
		t.Fatal("empty state reads as set up")
	}
	writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "kitchen-pi"}))
	writeState(t, dir, ".provider.key", strings.Repeat("k", 32))
	writeState(t, dir, ".provider.jwt", fakeJwt(map[string]any{"client_id": "01a15cc0-0000-0000-0000-00000000c21f", "device_id": "x"}))
	writeState(t, dir, minerStatusFileName, "{}")
	signIn := readMinerSignIn()
	device := readMinerDevice()
	if !signIn.signedIn || signIn.networkName != "kitchen-pi" || !device.hasKey || device.clientId == "" {
		t.Fatalf("%+v %+v", signIn, device)
	}
	// the friendly commands' own files are no provider history
	for _, name := range []string{minerStatusFileName, minerUpdateFileName, minerLogFileName, "service.log"} {
		if strings.HasPrefix(name, ".provider") || strings.HasPrefix(name, ".validator") || strings.HasPrefix(name, ".jwt") {
			t.Fatalf("%s looks like custody history", name)
		}
	}
}

func TestMinerStatusFileFollowsProgress(t *testing.T) {
	phase2State(t)
	progress, err := newProviderProgressOwner("standalone", []providerProgressConfigMember{{Slot: "direct", ApiUrl: DefaultApiUrl, ConnectUrl: DefaultConnectUrl}})
	if err != nil {
		t.Fatal(err)
	}
	defer progress.close()
	stop := startMinerStatusFile(t.Context(), progress)
	deadline := time.Now().Add(5 * time.Second)
	var status *minerLocalStatus
	for time.Now().Before(deadline) {
		if status, err = readMinerLocalStatus(); err == nil && 0 < len(status.Members) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status == nil || !status.running(time.Now()) || status.connected() || status.Pid != os.Getpid() {
		t.Fatalf("running status: %+v %v", status, err)
	}
	stop()
	status, err = readMinerLocalStatus()
	if err != nil || status.StoppedAt == nil || status.running(time.Now()) {
		t.Fatalf("stopped status: %+v %v", status, err)
	}
}

func writeLocalStatus(t *testing.T, connected bool, startedAgo time.Duration) {
	t.Helper()
	now := time.Now().UTC()
	status := &minerLocalStatus{Schema: minerStatusSchema, Pid: os.Getpid(), Version: "2026.10.21-7", StartedAt: now.Add(-startedAgo), UpdatedAt: now,
		Members: []minerMemberStatus{{Slot: "direct", ClientId: "01a15cc0-0000-0000-0000-00000000c21f", Lifecycle: "running", Connected: connected}}}
	if err := writeMinerLocalStatus(status); err != nil {
		t.Fatal(err)
	}
}

func TestStatusReportAndExitCodes(t *testing.T) {
	dir := phase2State(t)
	cliRun = func(string, ...string) (string, error) { return "", fmt.Errorf("no service manager in tests") }
	defer func() { cliRun = defaultCliRun }()
	cases := []struct {
		setup func()
		state string
		code  int
		next  string
	}{
		{func() {}, "needs_setup", minerExitNeedsSetup, "provider setup"},
		{func() { writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "kitchen-pi"})) }, "needs_setup", minerExitNeedsSetup, "provider setup"},
		// a key alone is a registration that didn't finish
		{func() { writeState(t, dir, ".provider.key", strings.Repeat("k", 32)) }, "needs_setup", minerExitNeedsSetup, "provider setup"},
		{func() {
			writeState(t, dir, ".provider.jwt", fakeJwt(map[string]any{"client_id": "01a15cc0-0000-0000-0000-00000000c21f", "device_id": "x"}))
		}, "stopped", minerExitOffline, "provider start"},
		{func() { writeLocalStatus(t, false, time.Minute) }, "connecting", minerExitOffline, "provider doctor"},
		{func() { writeLocalStatus(t, true, 76*time.Hour) }, "running", 0, ""},
	}
	for _, c := range cases {
		c.setup()
		report, code := buildMinerStatusReport(time.Now())
		if report.State != c.state || code != c.code || report.Next != c.next {
			t.Fatalf("want %s/%d/%q, got %s/%d/%q", c.state, c.code, c.next, report.State, code, report.Next)
		}
		var out bytes.Buffer
		if got := statusCmd(docopt.Opts{}, &out); got != c.code {
			t.Fatalf("statusCmd exit %d, want %d", got, c.code)
		}
		if c.next != "" && !strings.Contains(out.String(), "→ "+c.next) {
			t.Fatalf("status doesn't name the next step:\n%s", out.String())
		}
		out.Reset()
		statusCmd(docopt.Opts{"--json": true}, &out)
		decoded := minerStatusReport{}
		if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded.State != c.state {
			t.Fatalf("json: %v %s", err, out.String())
		}
	}
	var out bytes.Buffer
	statusCmd(docopt.Opts{}, &out)
	for _, want := range []string{"running for 3d 4h", `network "kitchen-pi"`, "miner identity 01a1…c21f", "Connected"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q:\n%s", want, out.String())
		}
	}
	// a stale file means the miner is gone, whatever it last said
	status, _ := readMinerLocalStatus()
	status.UpdatedAt = time.Now().Add(-time.Hour)
	writeMinerLocalStatus(status)
	if report, _ := buildMinerStatusReport(time.Now()); report.Running {
		t.Fatal("a stale status file reads as running")
	}
}

var defaultCliRun = cliRun

func TestReleaseVersions(t *testing.T) {
	a, ok := parseMinerReleaseVersion("v2026.10.20-1077406510")
	if !ok {
		t.Fatal("release tag not parsed")
	}
	b, _ := parseMinerReleaseVersion("2026.10.21-1")
	c, _ := parseMinerReleaseVersion("2026.10.21-12")
	if !b.newerThan(a) || !c.newerThan(b) || a.newerThan(b) || b.newerThan(b) {
		t.Fatal("version order")
	}
	for _, dev := range []string{"", "(development build)", "2026.10.21-friendly-cli-d4d7e06"} {
		if _, ok := parseMinerReleaseVersion(dev); ok {
			t.Fatalf("%q parsed as a release", dev)
		}
	}
}

func TestRolloutDelayIsStableAndInsideTheWindow(t *testing.T) {
	phase2State(t)
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		version := fmt.Sprintf("2026.10.%02d-1", i)
		delay := minerRolloutDelay(version)
		if delay < 0 || minerUpdateRollout <= delay || delay != minerRolloutDelay(version) {
			t.Fatalf("%s: delay %s", version, delay)
		}
		seen[delay] = true
	}
	if len(seen) < 40 {
		t.Fatal("delays don't spread")
	}
	// the slowest device, checking every 7 hours at worst, updates within a day
	if 24*time.Hour < minerUpdateRollout+7*time.Hour {
		t.Fatal("rollout window and check interval exceed 24 hours")
	}
}

// fakeRelease writes a feed, and an archive of every platform holding a
// provider script that prints version, as the release pipeline lays it out.
func fakeRelease(t *testing.T, version string, corrupt bool, digest bool) string {
	t.Helper()
	dir := t.TempDir()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for _, platform := range []string{"linux/arm", runtime.GOOS + "/" + runtime.GOARCH, "windows/amd64"} {
		script := []byte("#!/bin/sh\necho " + version + "\n")
		if platform != runtime.GOOS+"/"+runtime.GOARCH {
			script = []byte("not this one")
		}
		tw.WriteHeader(&tar.Header{Name: platform + "/provider", Mode: 0755, Size: int64(len(script)), Typeflag: tar.TypeReg})
		tw.Write(script)
	}
	tw.Close()
	gz.Close()
	name := "urnetwork-provider-" + version + ".tar.gz"
	os.WriteFile(filepath.Join(dir, name), archive.Bytes(), 0644)
	sum := sha256.Sum256(archive.Bytes())
	if corrupt {
		sum[0] ^= 1
	}
	asset := map[string]any{"name": name, "browser_download_url": name, "size": archive.Len()}
	if digest {
		asset["digest"] = "sha256:" + hex.EncodeToString(sum[:])
	}
	feed, _ := json.Marshal(map[string]any{"tag_name": "v" + version, "published_at": time.Now().UTC().Format(time.RFC3339), "assets": []any{asset}})
	path := filepath.Join(dir, "release.json")
	os.WriteFile(path, feed, 0644)
	return path
}

func fakeExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "provider")
	os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0755)
	cliExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { cliExecutable = defaultCliExecutable })
	return path
}

var defaultCliExecutable = cliExecutable

func TestInstallMinerRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	phase2State(t)
	executable := fakeExecutable(t)
	t.Setenv("URNETWORK_UPDATE_URL", fakeRelease(t, "2026.10.22-3", false, true))
	release, err := fetchMinerRelease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if available, _ := minerUpdateAvailable(release); !available {
		t.Fatal("a test feed should update a development build")
	}
	if err := installMinerRelease(t.Context(), release, executable); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(executable); !strings.Contains(string(raw), "2026.10.22-3") {
		t.Fatalf("not swapped: %q", raw)
	}
	if raw, _ := os.ReadFile(executable + ".previous"); !strings.Contains(string(raw), "old") {
		t.Fatal("previous binary not kept")
	}
	if err := rollBackMiner(executable); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(executable); !strings.Contains(string(raw), "old") {
		t.Fatal("not rolled back")
	}
	// a bad or missing digest changes nothing
	for _, feed := range []string{fakeRelease(t, "2026.10.22-4", true, true), fakeRelease(t, "2026.10.22-5", false, false)} {
		t.Setenv("URNETWORK_UPDATE_URL", feed)
		release, err := fetchMinerRelease(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := installMinerRelease(t.Context(), release, executable); err == nil {
			t.Fatal("an unverifiable download was installed")
		}
		if raw, _ := os.ReadFile(executable); !strings.Contains(string(raw), "old") {
			t.Fatal("a refused update changed the binary")
		}
		if _, err := os.Stat(executable + ".new"); !os.IsNotExist(err) {
			t.Fatal("a refused update left its download")
		}
	}
}

func TestUpdateOptOut(t *testing.T) {
	phase2State(t)
	var out, errOut bytes.Buffer
	if on, _ := minerAutoUpdate(readMinerUpdateState()); !on {
		t.Fatal("automatic updates must be on by default")
	}
	if updateCmd(docopt.Opts{"--off": true}, &out, &errOut) != 0 {
		t.Fatal(errOut.String())
	}
	if on, _ := minerAutoUpdate(readMinerUpdateState()); on {
		t.Fatal("--off didn't stick")
	}
	out.Reset()
	if autoUpdate(&out, readMinerUpdateState(), "/nonexistent/provider"); !strings.Contains(out.String(), "skipped") {
		t.Fatalf("--auto ran while off: %q", out.String())
	}
	updateCmd(docopt.Opts{"--on": true}, &out, &errOut)
	if on, _ := minerAutoUpdate(readMinerUpdateState()); !on {
		t.Fatal("--on didn't stick")
	}
	t.Setenv("URNETWORK_AUTO_UPDATE", "0")
	if on, why := minerAutoUpdate(readMinerUpdateState()); on || !strings.Contains(why, "URNETWORK_AUTO_UPDATE") {
		t.Fatal("the environment opt-out is ignored")
	}
	// the overview doesn't advertise the opt-out; update's own help does
	if strings.Contains(cliOverview(), "--off") || strings.Contains(cliOverview(), "AUTO_UPDATE") {
		t.Fatal("the overview advertises the opt-out")
	}
	if help := cliHelpFor([]string{"update"}); !strings.Contains(help, "--off") || !strings.Contains(help, "URNETWORK_AUTO_UPDATE=0") {
		t.Fatal("update's help doesn't document the opt-out")
	}
}

func TestAutoUpdateSkipsAHeldBackVersion(t *testing.T) {
	phase2State(t)
	t.Setenv("URNETWORK_UPDATE_URL", fakeRelease(t, "2026.10.22-6", false, true))
	until := time.Now().Add(time.Hour)
	state := minerUpdateState{HeldBack: "2026.10.22-6", HeldUntil: &until}
	var out bytes.Buffer
	autoUpdate(&out, state, "/nonexistent/provider")
	if !strings.Contains(out.String(), "rolled back; skipped") {
		t.Fatalf("%q", out.String())
	}
}

func TestExtractMinerBinaryPicksThisPlatform(t *testing.T) {
	feed := fakeRelease(t, "2026.10.22-7", false, true)
	raw, _ := os.ReadFile(filepath.Join(filepath.Dir(feed), "urnetwork-provider-2026.10.22-7.tar.gz"))
	var out bytes.Buffer
	found, err := extractMinerBinary(bytes.NewReader(raw), &out)
	if err != nil || !found || !strings.Contains(out.String(), "2026.10.22-7") {
		t.Fatalf("found=%v err=%v %q", found, err, out.String())
	}
	if _, err := extractMinerBinary(strings.NewReader("not gzip"), io.Discard); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestSystemdUnits(t *testing.T) {
	units := systemdUnits("urnetwork-provider", "/home/me/.local/bin/provider", "/home/me/.urnetwork", false)
	service := units["urnetwork-provider.service"]
	for _, want := range []string{"ExecStart=/home/me/.local/bin/provider start", "Environment=URNETWORK_STATE_DIR=/home/me/.urnetwork", "Restart=on-failure", "RestartPreventExitStatus=3", "WantedBy=default.target"} {
		if !strings.Contains(service, want) {
			t.Fatalf("missing %q:\n%s", want, service)
		}
	}
	if !strings.Contains(units["urnetwork-provider-update.service"], "update --auto") || !strings.Contains(units["urnetwork-provider-update.timer"], "OnUnitActiveSec=6h") {
		t.Fatal("update units")
	}
	quoted := systemdUnits("test-name", "/opt/my miner/provider", "/var/lib/ur state", true)["test-name.service"]
	if !strings.Contains(quoted, `ExecStart="/opt/my miner/provider" start`) || !strings.Contains(quoted, `"URNETWORK_STATE_DIR=/var/lib/ur state"`) || !strings.Contains(quoted, "URNETWORK_SERVICE_NAME=test-name") || !strings.Contains(quoted, "multi-user.target") {
		t.Fatal(quoted)
	}
}

func TestLaunchdPlistsAreWellFormed(t *testing.T) {
	for label, text := range launchdPlists("xyz.ur.provider", "/Users/me/bin/provider & co", "/Users/me/.urnetwork", "urnetwork-provider") {
		decoder := xml.NewDecoder(strings.NewReader(text))
		decoder.Strict = true
		decoder.Entity = xml.HTMLEntity
		for {
			if _, err := decoder.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
		}
		if !strings.Contains(text, "/Users/me/bin/provider &amp; co") {
			t.Fatalf("%s doesn't escape the path", label)
		}
	}
}

func TestServiceInstallWithSystemd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd")
	}
	phase2State(t)
	fakeExecutable(t)
	var calls []string
	cliRun = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	}
	defer func() { cliRun = defaultCliRun }()
	manager := newSystemdService(false)
	var out bytes.Buffer
	stateDir, _ := providerStateDir()
	if err := manager.install("/bin/provider", stateDir, &out); err != nil {
		t.Fatal(err)
	}
	if !manager.installed() {
		t.Fatal("unit not written")
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{"systemctl --user daemon-reload", "systemctl --user enable --now urnetwork-provider.service urnetwork-provider-update.timer", "loginctl enable-linger"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in\n%s", want, joined)
		}
	}
	if err := manager.uninstall(&out); err != nil || manager.installed() {
		t.Fatal("not uninstalled")
	}
	// the URnetwork installer's own unit is left alone
	os.MkdirAll(manager.dir, 0755)
	os.WriteFile(filepath.Join(manager.dir, "urnetwork.service"), []byte("[Unit]\n"), 0644)
	if manager.foreign() != "urnetwork.service" {
		t.Fatal("installer unit not noticed")
	}
}

func TestSetupReadsTheWalletChallenge(t *testing.T) {
	message := "URnetwork wallet mapping\nnonce: 42\ncoldkey: 5Grw"
	text := snWalletChallengeText("5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY", &sdk.AuthWalletChallengeResult{MessageTemplate: message, ExpiresIn: 300})
	match := setupMessagePattern.FindStringSubmatch(text)
	if match == nil || match[1] != message {
		t.Fatalf("%q", match)
	}
}

func TestPhase2CommandsParse(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, SkipHelpFlags: true}
	for _, args := range [][]string{
		{"setup"}, {"setup", "--yes", "--auth-code-file=code.txt", "--no-service"}, {"setup", "--system", "--yes"},
		{"status"}, {"status", "--json"}, {"doctor"},
		{"service", "install"}, {"service", "install", "--system"}, {"service", "uninstall"},
		{"service", "start"}, {"service", "stop"}, {"service", "restart"}, {"service", "status"}, {"service", "logs", "--follow"},
		{"update"}, {"update", "--check"}, {"update", "--status"}, {"update", "--off"}, {"update", "--on"}, {"update", "--auto"},
	} {
		opts, err := parser.ParseArgs(mainUsage(), args, "")
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if chosen, _ := opts.Bool(args[0]); !chosen {
			t.Fatalf("%v: command not chosen", args)
		}
	}
	if _, err := parser.ParseArgs(mainUsage(), []string{"update", "--on", "--off"}, ""); err == nil {
		t.Fatal("--on with --off parsed")
	}
}

func TestServiceFilesRoundTrip(t *testing.T) {
	phase2State(t)
	unitDir := t.TempDir()
	systemd := &systemdService{name: "urnetwork-provider", dir: unitDir}
	executable, stateDir := "/opt/50% $HOME/provider", "/var/lib/ur state"
	for file, text := range systemdUnits(systemd.name, executable, stateDir, false) {
		os.WriteFile(filepath.Join(unitDir, file), []byte(text), 0644)
	}
	unit, _ := os.ReadFile(filepath.Join(unitDir, "urnetwork-provider.service"))
	if !strings.Contains(string(unit), "50%% $$HOME") {
		t.Fatalf("specifiers not escaped:\n%s", unit)
	}
	if gotExe, gotDir := systemd.configured(); gotExe != executable || gotDir != stateDir {
		t.Fatalf("systemd: %q %q", gotExe, gotDir)
	}
	launchd := &launchdService{label: minerLaunchdLabel, dir: t.TempDir()}
	for label, text := range launchdPlists(launchd.label, "/Users/me/a&b/provider", "/Users/me/.urnetwork", minerServiceDefaultName) {
		os.WriteFile(launchd.plistPath(label), []byte(text), 0644)
	}
	if gotExe, gotDir := launchd.configured(); gotExe != "/Users/me/a&b/provider" || gotDir != "/Users/me/.urnetwork" {
		t.Fatalf("launchd: %q %q", gotExe, gotDir)
	}
}

func TestUpdateDownloadsOnlyFromGitHub(t *testing.T) {
	for location, want := range map[string]bool{
		"https://github.com/urnetwork/build/releases/download/v1/a.tar.gz": true,
		"https://objects.githubusercontent.com/x":                          true,
		"http://github.com/x":                                              false,
		"https://example.com/a.tar.gz":                                     false,
		"https://github.com.evil.example/a":                                false,
		"a.tar.gz":                                                         false,
		"file:///tmp/a.tar.gz":                                             false,
	} {
		if minerTrustedDownload(location) != want {
			t.Errorf("%s: want %v", location, want)
		}
	}
}

func TestSystemServiceNeedsARootOnlyBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix ownership")
	}
	path := filepath.Join(t.TempDir(), "provider")
	os.WriteFile(path, []byte("x"), 0755)
	if os.Geteuid() == 0 {
		os.Chmod(filepath.Dir(path), 0777)
	}
	if rootOnlyProblem(path) == "" {
		t.Fatal("a binary others can change passed")
	}
	if rootOnlyProblem("/bin/sh") != "" && os.Geteuid() == 0 {
		t.Log("/bin/sh is not root-only here")
	}
}

func TestStartLeavesAnUnfinishedRegistrationToAPerson(t *testing.T) {
	dir := phase2State(t)
	writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "kitchen-pi"}))
	writeState(t, dir, ".provider.key", strings.Repeat("k", 32))
	opts := docopt.Opts{}
	var out, errOut bytes.Buffer
	// outside a terminal the miner starts as it always did and says why it can't
	if !providePreflight(opts, true, nil, &out, &errOut) {
		t.Fatal(errOut.String())
	}
	if allow, _ := opts.Bool("--allow-client-registration"); allow {
		t.Fatal("registration allowed without a person")
	}
	if report, code := buildMinerStatusReport(time.Now()); report.Registered || code != minerExitNeedsSetup {
		t.Fatalf("a key alone reads as registered: %+v", report)
	}
}

// A binary the official installer put beside urnet-tools is updated by
// urnet-tools, never swapped behind it, and its unit is driven as is.
func TestTheOfficialInstallerKeepsItsUpdates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Linux installer")
	}
	phase2State(t)
	// the installer's unit mines ~/.urnetwork
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, ".urnetwork"), 0700)
	t.Setenv("URNETWORK_STATE_DIR", filepath.Join(home, ".urnetwork"))
	executable := fakeExecutable(t)
	os.WriteFile(filepath.Join(filepath.Dir(executable), "urnet-tools"), []byte("#!/bin/sh\n"), 0755)
	var calls []string
	timer := "enabled"
	cliRun = func(name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch strings.Join(args, " ") {
		case "--user is-enabled urnetwork-update.timer":
			return timer, nil
		case "--user is-active urnetwork.service":
			return "active", nil
		case "--user is-enabled urnetwork.service":
			return "enabled", nil
		}
		return "", nil
	}
	cliRunAttached = func(name string, args ...string) error {
		calls = append(calls, "attached "+filepath.Base(name)+" "+strings.Join(args, " "))
		return nil
	}
	defer func() { cliRun, cliRunAttached = defaultCliRun, defaultCliRunAttached }()

	run := func(opts docopt.Opts) string {
		calls = nil
		var out, errOut bytes.Buffer
		if code := updateCmd(opts, &out, &errOut); code != 0 {
			t.Fatalf("%v: exit %d: %s", opts, code, errOut.String())
		}
		return out.String() + strings.Join(calls, "\n")
	}
	for opts, want := range map[*docopt.Opts]string{
		{}:                "attached urnet-tools update",
		{"--off": true}:   "attached urnet-tools auto-update off",
		{"--on": true}:    "attached urnet-tools auto-update on",
		{"--check": true}: "attached urnet-tools --version",
		{"--auto": true}:  "skipped, urnet-tools updates this miner",
	} {
		if got := run(*opts); !strings.Contains(got, want) {
			t.Fatalf("%v: want %q in\n%s", *opts, want, got)
		}
	}
	if path, _ := providerStatePath(minerUpdateFileName); fileExists(path) {
		t.Fatal("the miner kept its own update state for an installer's binary")
	}
	timer = "disabled"
	if got := run(docopt.Opts{"--status": true}); !strings.Contains(got, "off") || !strings.Contains(got, "urnet-tools") {
		t.Fatal(got)
	}
	report, _ := buildMinerStatusReport(time.Now())
	if report.Update.Auto || report.Update.By != "urnet-tools" {
		t.Fatalf("%+v", report.Update)
	}

	// service start/stop/status/logs drive the installer's urnetwork.service
	unitDir := newSystemdService(false).dir
	os.MkdirAll(unitDir, 0755)
	os.WriteFile(filepath.Join(unitDir, "urnetwork.service"), []byte("[Unit]\n"), 0644)
	calls = nil
	var out, errOut bytes.Buffer
	if code := serviceCmd(docopt.Opts{"restart": true}, &out, &errOut); code != 0 {
		t.Fatal(errOut.String())
	}
	if !strings.Contains(strings.Join(calls, "\n"), "systemctl --user restart urnetwork.service") {
		t.Fatal(strings.Join(calls, "\n"))
	}
	if code := serviceCmd(docopt.Opts{"install": true}, &out, &errOut); code == 0 {
		t.Fatal("installed a second service beside the installer's")
	}
	// a test install with its own state folder is a separate miner
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	if installerService(newSystemdService(false)) != nil {
		t.Fatal("the installer's unit claimed a separate state folder")
	}
	t.Setenv("URNETWORK_STATE_DIR", filepath.Join(home, ".urnetwork"))
	// an older miner under the installer's unit doesn't write a status file
	writeState(t, os.Getenv("URNETWORK_STATE_DIR"), "jwt", fakeJwt(map[string]any{"network_name": "test"}))
	writeState(t, os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt", fakeJwt(map[string]any{"client_id": "01a15cc0"}))
	report, _ = buildMinerStatusReport(time.Now())
	if !report.Running || !report.Unreported || report.State != "running" {
		t.Fatalf("%+v", report)
	}
}

var defaultCliRunAttached = cliRunAttached

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
