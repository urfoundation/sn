package miner

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
)

// provider update replaces this binary with the newest release of
// urnetwork/build. Automatic updates (provider update --auto, run by the
// service's timer) are on unless turned off with provider update --off or
// URNETWORK_AUTO_UPDATE=0. Each device waits its own share of a rollout
// window after a release, so the whole fleet updates within a day without
// every miner restarting at once. A new version that doesn't come back
// connected is rolled back and skipped.
//
// A download is checked against the sha256 digest the release lists for it.
// Releases are not signed yet; when urnetwork/build publishes signed
// checksums, they are checked here first.

const minerUpdateFileName = "miner-update.json"
const minerUpdateDefaultSource = "https://api.github.com/repos/urnetwork/build/releases/latest"

// minerUpdateRollout spreads the fleet over this long after a release. With
// the 6 hour (plus up to 1 hour random) check interval, every miner has the
// release within 24 hours of publication.
const minerUpdateRollout = 16 * time.Hour

// How long a new version has to come back connected before it is rolled
// back, and how long a rolled back version is skipped.
const minerUpdateHealthWindow = 10 * time.Minute
const minerUpdateHoldBack = 7 * 24 * time.Hour

// The largest download an update reads, and the largest binary it accepts.
const minerUpdateMaxDownload = 1 << 30
const minerUpdateMaxBinary = 512 << 20

// minerUpdateState is kept beside the miner's state.
type minerUpdateState struct {
	Auto       *bool      `json:"auto,omitempty"`
	LastCheck  *time.Time `json:"last_check,omitempty"`
	LastResult string     `json:"last_result,omitempty"`
	Latest     string     `json:"latest,omitempty"`
	HeldBack   string     `json:"held_back,omitempty"`
	HeldUntil  *time.Time `json:"held_until,omitempty"`
	Updated    string     `json:"updated,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

func readMinerUpdateState() minerUpdateState {
	state := minerUpdateState{}
	path, err := providerStatePath(minerUpdateFileName)
	if err != nil {
		return state
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(raw, &state)
	return state
}

func writeMinerUpdateState(state minerUpdateState) error {
	path, err := providerStatePath(minerUpdateFileName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// minerAutoUpdate says whether automatic updates are on, and why not.
func minerAutoUpdate(state minerUpdateState) (bool, string) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("URNETWORK_AUTO_UPDATE"))) {
	case "0", "false", "off", "no":
		return false, "URNETWORK_AUTO_UPDATE is set to off"
	}
	if state.Auto != nil && !*state.Auto {
		return false, "turned off with provider update --off"
	}
	return true, ""
}

// minerReleaseVersion is a release version, YYYY.MM.DD-N.
type minerReleaseVersion struct {
	date  string
	build uint64
}

var minerReleaseVersionPattern = regexp.MustCompile(`^v?(\d{4}\.\d{2}\.\d{2})-(\d+)$`)

func parseMinerReleaseVersion(version string) (minerReleaseVersion, bool) {
	match := minerReleaseVersionPattern.FindStringSubmatch(strings.TrimSpace(version))
	if match == nil {
		return minerReleaseVersion{}, false
	}
	build, err := strconv.ParseUint(match[2], 10, 64)
	if err != nil {
		return minerReleaseVersion{}, false
	}
	return minerReleaseVersion{date: match[1], build: build}, true
}

func (self minerReleaseVersion) newerThan(other minerReleaseVersion) bool {
	if self.date != other.date {
		return other.date < self.date
	}
	return other.build < self.build
}

// minerUpdateSource is the release feed: GitHub's latest release of
// urnetwork/build, or the JSON file or URL URNETWORK_UPDATE_URL names (same
// shape), which is for testing an update before a release.
func minerUpdateSource() (string, bool) {
	if source := strings.TrimSpace(os.Getenv("URNETWORK_UPDATE_URL")); source != "" {
		return source, true
	}
	return minerUpdateDefaultSource, false
}

type minerRelease struct {
	TagName     string              `json:"tag_name"`
	PublishedAt time.Time           `json:"published_at"`
	Draft       bool                `json:"draft"`
	Prerelease  bool                `json:"prerelease"`
	Assets      []minerReleaseAsset `json:"assets"`
	base        string
}

type minerReleaseAsset struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	Url    string `json:"browser_download_url"`
}

func (self *minerRelease) version() string {
	return strings.TrimPrefix(self.TagName, "v")
}

// asset picks this platform's download: a per-platform archive when the
// release has one, else the archive of every platform.
func (self *minerRelease) asset() (*minerReleaseAsset, error) {
	version := self.version()
	names := []string{
		fmt.Sprintf("urnetwork-provider-%s-%s-%s.tar.gz", version, runtime.GOOS, runtime.GOARCH),
		fmt.Sprintf("urnetwork-provider-%s.tar.gz", version),
	}
	for _, name := range names {
		for i := range self.Assets {
			if self.Assets[i].Name == name {
				return &self.Assets[i], nil
			}
		}
	}
	return nil, fmt.Errorf("release %s has no provider download", self.TagName)
}

var minerUpdateClient = &http.Client{
	Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
	CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if 10 <= len(via) {
			return errors.New("too many redirects")
		}
		if request.URL.Scheme != "https" && via[0].URL.Scheme == "https" {
			return errors.New("refusing a redirect away from https")
		}
		return nil
	},
}

// minerTrustedDownload is where the default feed's downloads may come from.
func minerTrustedDownload(location string) bool {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

// minerOpen opens a feed or download: an http(s) URL, or a file path
// (relative paths are relative to the feed's directory).
func minerOpen(ctx context.Context, location string, base string) (io.ReadCloser, error) {
	if strings.HasPrefix(location, "https://") || strings.HasPrefix(location, "http://") {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", "urnetwork-provider/"+cliBareVersion())
		if strings.HasPrefix(location, "https://api.github.com/") {
			request.Header.Set("Accept", "application/vnd.github+json")
		}
		response, err := minerUpdateClient.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("%s answered %s", minerHost(location), response.Status)
		}
		return response.Body, nil
	}
	location = strings.TrimPrefix(location, "file://")
	if !filepath.IsAbs(location) && base != "" {
		location = filepath.Join(base, location)
	}
	return os.Open(location)
}

func minerHost(location string) string {
	if parsed, err := url.Parse(location); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return location
}

func fetchMinerRelease(ctx context.Context) (*minerRelease, error) {
	source, _ := minerUpdateSource()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	body, err := minerOpen(ctx, source, "")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	release := &minerRelease{}
	if err := json.NewDecoder(io.LimitReader(body, 8<<20)).Decode(release); err != nil {
		return nil, fmt.Errorf("the release feed isn't readable: %w", err)
	}
	if release.Draft || release.Prerelease {
		return nil, errors.New("the latest release is a draft or prerelease")
	}
	if _, ok := parseMinerReleaseVersion(release.TagName); !ok {
		return nil, fmt.Errorf("the latest release %q has no version this miner understands", release.TagName)
	}
	if !strings.Contains(source, "://") || strings.HasPrefix(source, "file://") {
		release.base = filepath.Dir(strings.TrimPrefix(source, "file://"))
	}
	return release, nil
}

// minerUpdateAvailable compares the release with this binary. A development
// build only takes updates from a test feed (URNETWORK_UPDATE_URL).
func minerUpdateAvailable(release *minerRelease) (bool, string) {
	latest, _ := parseMinerReleaseVersion(release.TagName)
	current, ok := parseMinerReleaseVersion(minerTargetVersion())
	if !ok {
		if _, test := minerUpdateSource(); test {
			return true, ""
		}
		return false, "this is " + cliBuildName() + ", which never replaces itself with a release"
	}
	if latest.newerThan(current) {
		return true, ""
	}
	return false, ""
}

var errMinerUpdateLocked = errors.New("another update is running")

// minerUpdateTargetPath, once set, is a binary other than this one that the
// update replaces (the installed service's).
var minerUpdateTargetPath string

// minerTargetVersion is the version of the binary an update replaces.
func minerTargetVersion() string {
	if minerUpdateTargetPath == "" {
		return cliBareVersion()
	}
	out, err := exec.Command(minerUpdateTargetPath, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// cliBuildName names a development build for a sentence.
func cliBuildName() string {
	if version := minerTargetVersion(); version != "" {
		return "a development build (" + version + ")"
	}
	return "a development build"
}

// minerRolloutDelay is this device's place in the rollout of a release:
// the same for every check of the same release, spread over the window.
func minerRolloutDelay(version string) time.Duration {
	key := readMinerDevice().clientId
	if key == "" {
		key, _ = os.Hostname()
	}
	hash := fnv.New64a()
	hash.Write([]byte(key + "\x00" + version))
	return time.Duration(hash.Sum64() % uint64(minerUpdateRollout))
}

// installMinerRelease downloads the release, checks it, and swaps it in
// for executable. The old binary stays beside it as executable.previous.
func installMinerRelease(ctx context.Context, release *minerRelease, executable string) error {
	asset, err := release.asset()
	if err != nil {
		return err
	}
	want, ok := strings.CutPrefix(asset.Digest, "sha256:")
	if !ok || len(want) != 64 {
		return fmt.Errorf("release %s lists no sha256 digest for %s, so it can't be checked", release.TagName, asset.Name)
	}
	dir := filepath.Dir(executable)
	// a fresh name each time (O_EXCL), so nothing planted there is followed;
	// Windows runs only a name that ends in .exe
	pattern := "." + filepath.Base(executable) + ".new-*"
	if runtime.GOOS == "windows" {
		pattern += ".exe"
	}
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("can't write beside %s: %w", executable, err)
	}
	newPath := file.Name()
	defer os.Remove(newPath)
	if _, test := minerUpdateSource(); !test && !minerTrustedDownload(asset.Url) {
		file.Close()
		return fmt.Errorf("release %s points its download outside GitHub (%s); nothing was changed", release.TagName, minerHost(asset.Url))
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	body, err := minerOpen(ctx, asset.Url, release.base)
	if err != nil {
		file.Close()
		return err
	}
	defer body.Close()
	hash := sha256.New()
	found, extractErr := extractMinerBinary(io.TeeReader(io.LimitReader(body, minerUpdateMaxDownload), hash), file)
	// hash every byte, also those after the binary
	_, _ = io.Copy(io.Discard, io.TeeReader(io.LimitReader(body, minerUpdateMaxDownload), hash))
	if err := file.Chmod(0755); err != nil {
		file.Close()
		return err
	}
	// on disk before it can take the binary's name
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if extractErr != nil {
		return extractErr
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != strings.ToLower(want) {
		return fmt.Errorf("the download of %s doesn't match its sha256 digest; nothing was changed", asset.Name)
	}
	if !found {
		return fmt.Errorf("%s has no provider for %s/%s", asset.Name, runtime.GOOS, runtime.GOARCH)
	}
	versionOut, err := exec.CommandContext(ctx, newPath, "--version").Output()
	if err != nil {
		return fmt.Errorf("the new binary doesn't run on this device: %w", err)
	}
	if got := strings.TrimSpace(string(versionOut)); got != release.version() {
		return fmt.Errorf("the new binary says it is %q, not %s; nothing was changed", got, release.version())
	}
	return replaceMinerBinary(newPath, executable)
}

func copyFile(from string, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func syncDir(dir string) {
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		handle.Close()
	}
}

// extractMinerBinary copies this platform's provider out of a release
// archive: <os>/<arch>/provider in the archive of every platform, or a
// lone provider in a per-platform one.
func extractMinerBinary(archive io.Reader, out io.Writer) (bool, error) {
	binary := "provider"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	wanted := map[string]bool{
		runtime.GOOS + "/" + runtime.GOARCH + "/" + binary:        true,
		"./" + runtime.GOOS + "/" + runtime.GOARCH + "/" + binary: true,
		binary:        true,
		"./" + binary: true,
	}
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return false, fmt.Errorf("the download isn't a gzip archive: %w", err)
	}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("the download isn't a readable archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || !wanted[header.Name] {
			continue
		}
		if minerUpdateMaxBinary < header.Size {
			return false, errors.New("the binary in the download is too large")
		}
		if _, err := io.Copy(out, io.LimitReader(reader, minerUpdateMaxBinary)); err != nil {
			return false, err
		}
		return true, nil
	}
}

// minerRunningState is what an update needs to know about the running miner.
type minerRunningState struct {
	running   bool
	connected bool
	service   minerServiceManager
}

func readMinerRunningState() minerRunningState {
	state := minerRunningState{}
	if status, err := readMinerLocalStatus(); err == nil && status.running(time.Now()) {
		state.running = true
		state.connected = status.connected()
	}
	for _, system := range []bool{false, true} {
		if manager, err := minerServiceFor(system); err == nil && manager.installed() && manager.active() {
			state.service = manager
			break
		}
	}
	return state
}

// waitMinerHealthy waits for the miner to run version, connected if it was
// connected before. It reports false when the window passes first.
func waitMinerHealthy(version string, needConnected bool, window time.Duration, tick func()) bool {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if status, err := readMinerLocalStatus(); err == nil && status.running(time.Now()) && status.Version == version {
			if !needConnected || status.connected() {
				return true
			}
		}
		if tick != nil {
			tick()
		}
		time.Sleep(3 * time.Second)
	}
	return false
}

// minerUpdateTarget is the binary an update replaces: the installed
// service's own, when there is one, else this one. With a service, its
// state folder is used too, so a sudo run reads the service's status and
// settings rather than root's own.
func minerUpdateTarget() (string, error) {
	for _, system := range []bool{false, true} {
		manager, err := minerServiceFor(system)
		if err != nil || !manager.installed() {
			continue
		}
		executable, stateDir := manager.configured()
		if stateDir != "" && os.Getenv("URNETWORK_STATE_DIR") == "" {
			os.Setenv("URNETWORK_STATE_DIR", stateDir)
		}
		if executable != "" {
			if own, err := cliExecutable(); err != nil || own != executable {
				minerUpdateTargetPath = executable
			}
			return executable, nil
		}
		break
	}
	return cliExecutable()
}

// minerUpdateAccessProblem is why this user can't change the update state
// or the binary of a service another user (root) owns.
func minerUpdateAccessProblem(executable string, binary bool) string {
	if dir, err := providerStateDir(); err == nil {
		if _, err := os.Stat(dir); err == nil && writableDirProblem(dir) != "" || errors.Is(err, os.ErrPermission) {
			return "its state in " + dir + " belongs to another user"
		}
	}
	if binary && writableDirProblem(filepath.Dir(executable)) != "" {
		return filepath.Dir(executable) + " isn't writable by this user"
	}
	return ""
}

func updateCmd(opts docopt.Opts, stdout io.Writer, stderr io.Writer) int {
	executable, executableErr := minerUpdateTarget()
	if executableErr == nil {
		if problem := rootUpdateProblem(executable); problem != "" {
			writeCliError(stderr, "Not run as root: "+problem+"\n  Root would run or replace files another user can change. Run it without sudo, as the user who owns that miner.")
			return 1
		}
		if installer := findMinerInstaller(executable); installer != nil {
			return installerUpdateCmd(opts, installer, stdout, stderr)
		}
	}
	if !cliOptBool(opts, "--status") && !cliOptBool(opts, "--check") && !cliOptBool(opts, "--auto") && executableErr == nil {
		binary := !cliOptBool(opts, "--on") && !cliOptBool(opts, "--off")
		if problem := minerUpdateAccessProblem(executable, binary); problem != "" && runtime.GOOS == "windows" {
			writeCliError(stderr, "Can't change this miner's updates: "+problem+".\n  "+minerOwnFolderHint())
			return 1
		} else if problem != "" && os.Geteuid() != 0 {
			writeCliError(stderr, "Can't change this miner's updates: "+problem+".\n  Run it with sudo: sudo "+strings.Join(append([]string{"provider"}, os.Args[1:]...), " "))
			return 1
		}
	}
	state := readMinerUpdateState()
	switch {
	case cliOptBool(opts, "--on"), cliOptBool(opts, "--off"):
		on := cliOptBool(opts, "--on")
		state.Auto = &on
		if err := writeMinerUpdateState(state); err != nil {
			writeCliError(stderr, "Couldn't save the setting: "+err.Error())
			return 1
		}
		if on {
			fmt.Fprintf(stdout, "  %s Automatic updates are on.\n", cliGreen(stdout, "✓"))
			if auto, why := minerAutoUpdate(state); !auto {
				fmt.Fprintf(stdout, "  %s They stay off while %s.\n", cliYellow(stdout, "!"), why)
			}
		} else {
			fmt.Fprintf(stdout, "  %s Automatic updates are off. Update by hand with: provider update\n", cliGreen(stdout, "✓"))
			fmt.Fprintln(stdout, "    Turn them back on with: provider update --on")
		}
		return 0
	case cliOptBool(opts, "--status"):
		printMinerUpdateStatus(stdout, state)
		return 0
	case cliOptBool(opts, "--auto"):
		if executableErr != nil {
			fmt.Fprintf(stdout, "automatic update: can't find the binary: %s\n", executableErr)
			return 0
		}
		return autoUpdate(stdout, state, executable)
	}
	if executableErr != nil {
		writeCliError(stderr, "Couldn't find the binary to update: "+executableErr.Error())
		return 1
	}
	check := cliOptBool(opts, "--check")
	fmt.Fprintln(stdout, "Checking for a new version…")
	release, err := fetchMinerRelease(context.Background())
	now := time.Now().UTC()
	state.LastCheck = &now
	if err != nil {
		state.LastResult = "check failed: " + firstLine(err.Error())
		_ = writeMinerUpdateState(state)
		writeCliError(stderr, "Couldn't check for updates: "+firstLine(err.Error()))
		return 1
	}
	state.Latest = release.version()
	available, why := minerUpdateAvailable(release)
	if !available {
		state.LastResult = "up to date"
		_ = writeMinerUpdateState(state)
		if why != "" {
			fmt.Fprintf(stdout, "  The latest release is %s, but %s.\n", release.version(), why)
		} else {
			fmt.Fprintf(stdout, "  %s Up to date: %s is the latest release.\n", cliGreen(stdout, "✓"), minerTargetVersion())
		}
		return 0
	}
	fmt.Fprintf(stdout, "  %s is out (%s has %s).\n", release.version(), executable, minerTargetVersion())
	_ = writeMinerUpdateState(state)
	if check {
		fmt.Fprintln(stdout, "  Install it with: provider update")
		return 0
	}
	return applyMinerUpdate(stdout, stderr, release, executable, false)
}

// applyMinerUpdate installs the release, restarts the service's miner and
// rolls back when it doesn't come back as healthy as it was.
func applyMinerUpdate(stdout io.Writer, stderr io.Writer, release *minerRelease, executable string, auto bool) int {
	unlock, err := lockMinerUpdate()
	if errors.Is(err, errMinerUpdateLocked) {
		writeCliError(stderr, "Not updated: another update is running right now")
		return 1
	} else if err != nil {
		writeCliError(stderr, "Not updated: "+err.Error())
		return 1
	}
	defer unlock()
	state := readMinerUpdateState()
	before := readMinerRunningState()
	if err := installMinerRelease(context.Background(), release, executable); err != nil {
		state.LastResult = "update failed: " + firstLine(err.Error())
		_ = writeMinerUpdateState(state)
		writeCliError(stderr, "Not updated: "+firstLine(err.Error()))
		return 1
	}
	fmt.Fprintf(stdout, "  %s Downloaded %s and checked it against the release's sha256 digest\n", cliGreen(stdout, "✓"), release.version())
	fmt.Fprintf(stdout, "  %s Installed %s %s\n", cliGreen(stdout, "✓"), executable, cliDim(stdout, "(previous version kept as "+filepath.Base(executable)+".previous)"))
	now := time.Now().UTC()
	state.Updated = release.version()
	state.UpdatedAt = &now
	state.LastResult = "updated to " + release.version()
	_ = writeMinerUpdateState(state)
	if before.service == nil {
		if before.running {
			fmt.Fprintf(stdout, "  %s Restart your running miner to use %s.\n", cliYellow(stdout, "!"), release.version())
		}
		return 0
	}
	if err := before.service.control("restart"); err != nil {
		// no health check is possible: go back to what ran before
		rollbackErr := rollBackMiner(executable)
		_ = before.service.control("restart")
		state.Updated = ""
		state.LastResult = "the service didn't restart after the update; rolled back"
		_ = writeMinerUpdateState(state)
		if rollbackErr != nil {
			writeCliError(stderr, fmt.Sprintf("The service didn't restart (%s) and the previous version couldn't be restored: %s", err, rollbackErr))
		} else {
			writeCliError(stderr, "The service didn't restart after the update ("+err.Error()+"), so the previous version is back.")
		}
		return 1
	}
	window := minerUpdateHealthWindow
	if !auto {
		fmt.Fprintf(stdout, "  … Restarted the miner, waiting for it to come back%s\n", map[bool]string{true: " connected", false: ""}[before.connected])
	}
	if waitMinerHealthy(release.version(), before.connected, window, nil) {
		fmt.Fprintf(stdout, "  %s Running %s\n", cliGreen(stdout, "✓"), release.version())
		return 0
	}
	// the new version isn't as healthy as the old one was: put the old one back
	if err := rollBackMiner(executable); err != nil {
		writeCliError(stderr, fmt.Sprintf("%s didn't come back and the previous version couldn't be restored: %s", release.version(), err))
		return 1
	}
	_ = before.service.control("restart")
	held := time.Now().UTC().Add(minerUpdateHoldBack)
	state.HeldBack = release.version()
	state.HeldUntil = &held
	state.Updated = ""
	state.LastResult = fmt.Sprintf("%s didn't come back within %s; rolled back", release.version(), window)
	_ = writeMinerUpdateState(state)
	writeCliError(stderr, fmt.Sprintf("%s didn't come back %swithin %s, so the previous version is back.\n  It will be skipped for 7 days.", release.version(), map[bool]string{true: "connected ", false: ""}[before.connected], window))
	return 1
}

// autoUpdate is one run of the update timer: quiet unless it acts.
func autoUpdate(stdout io.Writer, state minerUpdateState, executable string) int {
	if on, why := minerAutoUpdate(state); !on {
		fmt.Fprintf(stdout, "automatic update skipped: %s\n", why)
		return 0
	}
	release, err := fetchMinerRelease(context.Background())
	now := time.Now().UTC()
	state.LastCheck = &now
	if err != nil {
		state.LastResult = "check failed: " + firstLine(err.Error())
		_ = writeMinerUpdateState(state)
		fmt.Fprintf(stdout, "automatic update: check failed: %s\n", firstLine(err.Error()))
		return 0
	}
	state.Latest = release.version()
	available, why := minerUpdateAvailable(release)
	if !available {
		state.LastResult = "up to date"
		_ = writeMinerUpdateState(state)
		if why != "" {
			fmt.Fprintf(stdout, "automatic update: %s\n", why)
		}
		return 0
	}
	if state.HeldBack == release.version() && state.HeldUntil != nil && now.Before(*state.HeldUntil) {
		state.LastResult = release.version() + " was rolled back; skipped until " + state.HeldUntil.Format(time.RFC3339)
		_ = writeMinerUpdateState(state)
		fmt.Fprintf(stdout, "automatic update: %s\n", state.LastResult)
		return 0
	}
	if _, test := minerUpdateSource(); !test {
		if turn := release.PublishedAt.Add(minerRolloutDelay(release.version())); now.Before(turn) {
			state.LastResult = fmt.Sprintf("%s is out; this device's turn in the rollout is %s", release.version(), turn.Format(time.RFC3339))
			_ = writeMinerUpdateState(state)
			fmt.Fprintf(stdout, "automatic update: %s\n", state.LastResult)
			return 0
		}
	}
	_ = writeMinerUpdateState(state)
	fmt.Fprintf(stdout, "automatic update: installing %s (was %s)\n", release.version(), minerTargetVersion())
	// a failed automatic update already said why; the timer itself succeeded
	_ = applyMinerUpdate(stdout, stdout, release, executable, true)
	return 0
}

func printMinerUpdateStatus(stdout io.Writer, state minerUpdateState) {
	on, why := minerAutoUpdate(state)
	row := func(label string, value string) { fmt.Fprintf(stdout, "  %-18s %s\n", label, value) }
	if _, ok := parseMinerReleaseVersion(minerTargetVersion()); !ok {
		if _, test := minerUpdateSource(); !test {
			row("Automatic updates", "never: this is "+cliBuildName())
			return
		}
	}
	if on {
		row("Automatic updates", cliGreen(stdout, "on"))
	} else {
		row("Automatic updates", cliYellow(stdout, "off")+" ("+why+")")
	}
	service := readMinerServiceState()
	switch {
	case service.Installed && runtime.GOOS == "windows":
		row("Checks", "every 6 hours, by "+service.Name)
	case service.Installed:
		row("Checks", "every 6 hours, by "+service.Name+"-update")
	default:
		row("Checks", "only by hand: automatic checks come with provider service install")
	}
	if state.LastCheck != nil {
		row("Last check", fmt.Sprintf("%s ago: %s", cliAgo(*state.LastCheck), state.LastResult))
	}
	source, test := minerUpdateSource()
	if test {
		row("Source", source+" (URNETWORK_UPDATE_URL)")
	} else {
		row("Source", "github.com/urnetwork/build releases")
	}
	if on {
		row("Turn off", "provider update --off  (or URNETWORK_AUTO_UPDATE=0)")
	} else {
		row("Turn on", "provider update --on")
	}
}

// cliAgo is a short, rough duration: 3d 4h, 2h 5m, 40s.
func cliAgo(at time.Time) string {
	return cliDuration(time.Since(at))
}

func cliDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case 24*time.Hour <= d:
		return fmt.Sprintf("%dd %dh", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	case time.Hour <= d:
		return fmt.Sprintf("%dh %dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	case time.Minute <= d:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

// installerUpdateCmd hands updates of a binary the official installer
// manages to its urnet-tools, so only one updater ever runs.
func installerUpdateCmd(opts docopt.Opts, installer *minerInstaller, stdout io.Writer, stderr io.Writer) int {
	var args []string
	switch {
	case cliOptBool(opts, "--auto"):
		fmt.Fprintf(stdout, "automatic update: skipped, %s updates this miner\n", installer.display())
		return 0
	case cliOptBool(opts, "--status"):
		on, known := installer.autoUpdate()
		row := func(label string, value string) { fmt.Fprintf(stdout, "  %-18s %s\n", label, value) }
		switch {
		case !known:
			row("Automatic updates", "unknown")
		case on:
			row("Automatic updates", cliGreen(stdout, "on"))
		default:
			row("Automatic updates", cliYellow(stdout, "off"))
		}
		row("Handled by", installer.display()+", which the URnetwork installer set up")
		row("Source", "github.com/urnetwork/build releases")
		if on {
			row("Turn off", "provider update --off")
		} else {
			row("Turn on", "provider update --on")
		}
		return 0
	case cliOptBool(opts, "--on"):
		args = installer.autoUpdateArgs(true)
	case cliOptBool(opts, "--off"):
		args = installer.autoUpdateArgs(false)
	case cliOptBool(opts, "--check") && installer.windows:
		args = []string{"version"}
	case cliOptBool(opts, "--check"):
		args = []string{"--version"}
	default:
		args = []string{"update"}
	}
	fmt.Fprintf(stdout, "The URnetwork installer manages this miner, so this runs: %s\n", installer.display(args...))
	if err := installer.run(args...); err != nil {
		writeCliError(stderr, installer.display(args...)+" failed: "+firstLine(err.Error()))
		return 1
	}
	return 0
}
