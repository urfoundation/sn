package miner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/v2026"
)

// provider status answers "is my miner working?" from this device's state:
// one line per thing that has to be right, each a yes, a no or a warning,
// and the next command when something isn't. provider doctor looks further,
// at the folder, the network and the clock, for why.

// minerExitOffline is the exit code of status when the miner is set up but
// not running or not connected.
const minerExitOffline = 4

type minerStatusReport struct {
	State      string            `json:"state"`
	Version    string            `json:"version"`
	Network    string            `json:"network,omitempty"`
	SignedIn   bool              `json:"signed_in"`
	Registered bool              `json:"registered"`
	ClientId   string            `json:"client_id,omitempty"`
	Running    bool              `json:"running"`
	Pid        int               `json:"pid,omitempty"`
	UptimeS    int64             `json:"uptime_s,omitempty"`
	Connected  bool              `json:"connected"`
	Members    int               `json:"members,omitempty"`
	MembersUp  int               `json:"members_connected,omitempty"`
	Service    minerServiceState `json:"service"`
	// Unreported: a service runs the miner, but it doesn't report its
	// connection (an older version, or one just starting).
	Unreported bool              `json:"connection_unreported,omitempty"`
	Update     minerUpdateReport `json:"update"`
	// Extender: the running miner's extender role, absent while it doesn't
	// run or doesn't report it. It never changes the state or the exit code:
	// the role is optional.
	Extender *minerExtenderReport `json:"extender,omitempty"`
	// the extender's router port mapping, while a miner that reports it runs
	// (connect/EXTENDER.md S10); it never changes the state or the exit code
	// either
	PortMapping *minerPortMappingStatus `json:"port_mapping,omitempty"`
	StateDir    string                  `json:"state_dir"`
	// the application container the miner runs in (state_persistence.go),
	// absent outside one
	Container string `json:"container,omitempty"`
	// what is wrong with how the miner runs here, each with its fix: the
	// state directory's persistence, read now, and the running miner's own
	// findings (miner_findings.go). They never change the state or the exit
	// code: the miner provides with them.
	Findings []minerFinding `json:"findings,omitempty"`
	Problems []string       `json:"problems"`
	Next     string         `json:"next,omitempty"`
}

// minerExtenderReport is the extender's standing with the fix, when one
// command is it.
type minerExtenderReport struct {
	minerExtenderStanding
	Fix string `json:"fix,omitempty"`
}

const minerInstallerUpdateUnknown = "can't tell whether it checks for updates here"

// Why a container's miner doesn't update itself.
const minerContainerUpdateWhy = "only a new image updates a container's miner"

// The status row label of a finding's code.
var minerFindingLabels = map[string]string{
	minerFindingStateNotPersistent: "Identity",
	minerFindingUpdateAvailable:    "Version",
	minerFindingExitDnsBlocked:     "Exit DNS",
}

type minerUpdateReport struct {
	Auto      bool   `json:"auto"`
	Why       string `json:"why_not,omitempty"`
	Latest    string `json:"latest,omitempty"`
	LastCheck string `json:"last_check,omitempty"`
	Result    string `json:"last_result,omitempty"`
	// By names the official installer's tool when it handles updates.
	By string `json:"by,omitempty"`
	// PackageManager is the command that updates a package-managed binary
	// (cli_package_manager.go), which nothing here replaces.
	PackageManager string `json:"package_manager,omitempty"`
}

func buildMinerStatusReport(now time.Time) (minerStatusReport, int) {
	dir, _ := providerStateDir()
	report := minerStatusReport{Version: cliBareVersion(), StateDir: dir, Problems: []string{}}
	facts := readMinerHostFacts(minerHostRoot, os.Getenv)
	report.Container = facts.container
	findings := newMinerFindings()
	signIn := readMinerSignIn()
	device := readMinerDevice()
	if !signIn.signedIn && !device.hasHistory {
		// provide --all-operators: every operator's state is below the top
		signIn, device = readMinerOperatorIdentity(dir)
	}
	report.SignedIn = signIn.signedIn
	report.Network = signIn.networkName
	report.Registered = device.registered
	report.ClientId = device.clientId
	local, localErr := readMinerLocalStatus()
	if localErr == nil && local.running(now) {
		report.Running = true
		report.Pid = local.Pid
		report.UptimeS = int64(now.Sub(local.StartedAt) / time.Second)
		report.Connected = local.connected()
		report.Members = len(local.Members)
		report.MembersUp = local.connectedCount()
		report.PortMapping = local.PortMapping
		for _, member := range local.Members {
			if report.ClientId == "" && member.ClientId != "" {
				report.ClientId = member.ClientId
			}
		}
		if local.Extender != nil {
			report.Extender = &minerExtenderReport{minerExtenderStanding: *local.Extender}
			if local.Extender.Reason == minerExtenderReasonTcpPermission {
				report.Extender.Fix = minerExtenderBindFix()
			}
		}
		for _, finding := range local.Findings {
			findings.set(finding)
		}
	}
	// the state directory as it is kept now, in place of what the miner found
	// at its start
	if dir != "" {
		executable, _ := cliExecutable()
		if finding := minerStatePersistenceFinding(assessMinerStatePersistence(facts, dir), readMinerStateSubstitution(os.Getenv, dir), false, minerContainerImage(executable), now); finding != nil {
			findings.set(*finding)
		} else {
			findings.clear(minerFindingStateNotPersistent)
		}
	}
	report.Findings = findings.current()
	report.Service = readMinerServiceState()
	if !report.Running && report.Service.Active {
		report.Running, report.Unreported = true, true
	}
	updateState := readMinerUpdateState()
	report.Update.Auto, report.Update.Why = minerAutoUpdate(updateState)
	if _, release := parseMinerReleaseVersion(report.Version); !release {
		if _, test := minerUpdateSource(); !test {
			report.Update.Auto, report.Update.Why = false, "this is "+cliBuildName()
		}
	}
	report.Update.Latest = updateState.Latest
	report.Update.Result = updateState.LastResult
	if updateState.LastCheck != nil {
		report.Update.LastCheck = updateState.LastCheck.Format(time.RFC3339)
	}
	if executable, err := cliExecutable(); err == nil {
		if manager := findMinerPackageManager(executable, runtime.GOOS); manager != nil {
			report.Update = minerUpdateReport{Auto: false, Why: "its package manager updates it", PackageManager: manager.command, Latest: report.Update.Latest}
		} else if installer := findMinerInstaller(executable); installer != nil {
			on, known := installer.autoUpdate()
			report.Update = minerUpdateReport{Auto: on, By: installer.display()}
			if !known {
				report.Update.Why = minerInstallerUpdateUnknown
			} else if !on {
				report.Update.Why = "turned off in " + installer.display()
			}
		}
	}
	if report.Container != "" {
		// a container's miner is its image's: nothing here replaces it
		report.Update = minerUpdateReport{Auto: false, Why: minerContainerUpdateWhy, Latest: report.Update.Latest}
	}

	code := 0
	switch {
	case !report.SignedIn && !device.hasHistory:
		report.State = "needs_setup"
		report.Problems = append(report.Problems, "not signed in")
		report.Next = "provider setup"
		code = minerExitNeedsSetup
	case !device.registered && !(report.Running && report.Connected):
		report.State = "needs_setup"
		report.Problems = append(report.Problems, "this device isn't registered yet")
		report.Next = "provider setup"
		code = minerExitNeedsSetup
	case report.Unreported:
		report.State = "running"
	case !report.Running:
		report.State = "stopped"
		report.Problems = append(report.Problems, "the miner isn't running")
		switch {
		case report.Service.Installed:
			report.Next = "provider service start"
		case report.Service.Foreign != "":
			report.Next = "provider service start"
		default:
			report.Next = "provider start"
		}
		code = minerExitOffline
	case !report.Connected:
		report.State = "connecting"
		report.Problems = append(report.Problems, "the miner isn't connected to the network")
		report.Next = "provider doctor"
		code = minerExitOffline
	default:
		report.State = "running"
	}
	return report, code
}

func statusCmd(opts docopt.Opts, stdout io.Writer) int {
	now := time.Now()
	report, code := buildMinerStatusReport(now)
	if asJson, _ := opts.Bool("--json"); asJson {
		raw, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(stdout, string(raw))
		return code
	}
	network := "main network"
	if apiUrl, err := resolveApiUrl(docopt.Opts{}); err == nil && apiUrl != DefaultApiUrl {
		network = minerHost(apiUrl)
	}
	fmt.Fprintf(stdout, "%s %s\n\n", cliBold(stdout, "provider "+report.Version), cliDim(stdout, "· "+network))
	w := statusWriter{out: stdout}
	switch {
	case report.Unreported:
		w.row(true, "Miner", "running"+serviceNote(report.Service))
	case report.Running:
		w.row(true, "Miner", "running for "+cliDuration(time.Duration(report.UptimeS)*time.Second)+serviceNote(report.Service))
	case report.State == "needs_setup":
		w.row(nil, "Miner", "not set up yet")
	default:
		w.row(false, "Miner", "not running"+serviceNote(report.Service))
	}
	if report.SignedIn {
		detail := ""
		if report.Network != "" {
			detail = fmt.Sprintf("network %q", report.Network)
		}
		w.row(true, "Signed in", detail)
	} else {
		w.row(false, "Signed in", "no")
	}
	switch {
	case report.ClientId != "":
		w.row(true, "Device", "registered, miner identity "+shortId(report.ClientId))
	case report.Registered:
		w.row(true, "Device", "registered")
	case readMinerDevice().hasKey:
		w.row(false, "Device", "registration didn't finish")
	case report.SignedIn:
		w.row(false, "Device", "not registered yet")
	}
	switch {
	case report.Unreported:
		w.row(nil, "Connected", cliDim(stdout, "can't tell: this miner doesn't report it (yet)"))
	case report.Connected && 1 < report.Members:
		w.row(true, "Connected", fmt.Sprintf("%d of %d", report.MembersUp, report.Members))
	case report.Connected:
		w.row(true, "Connected", "")
	case report.Running && 1 < report.Members:
		w.row(false, "Connected", fmt.Sprintf("%d of %d, still trying", report.MembersUp, report.Members))
	case report.Running:
		w.row(false, "Connected", "not yet, still trying")
	default:
		w.row(nil, "Connected", cliDim(stdout, "not running"))
	}
	if report.Extender != nil && report.Extender.Bind != minerExtenderBindOff {
		mark, detail := minerExtenderRow(&report.Extender.minerExtenderStanding, report.Extender.Fix, os.Geteuid(), now)
		w.mark(mark, "Extender", detail)
	}
	w.portMapping(report.PortMapping)
	_, release := parseMinerReleaseVersion(report.Version)
	_, testFeed := minerUpdateSource()
	if report.Container != "" {
		w.row(nil, "Updates", cliDim(stdout, "by a new image: this container's miner never updates itself"))
	} else if by := report.Update.By; by != "" {
		switch {
		case report.Update.Auto:
			w.row(true, "Updates", "automatic, by "+by)
		case report.Update.Why == minerInstallerUpdateUnknown:
			w.row(nil, "Updates", cliDim(stdout, "by "+by+"; "+report.Update.Why))
		default:
			w.warn("Updates", report.Update.Why+" (provider update --on)")
		}
	} else if !release && !testFeed {
		w.row(nil, "Updates", cliDim(stdout, "development build, never updates itself"))
	} else if !release && report.Update.Auto {
		w.row(true, "Updates", "automatic, from a test feed")
	} else if !report.Update.Auto {
		w.warn("Updates", "automatic updates are off (provider update --on)")
	} else if report.Update.Latest != "" {
		if latest, ok := parseMinerReleaseVersion(report.Update.Latest); ok {
			current, _ := parseMinerReleaseVersion(report.Version)
			if latest.newerThan(current) {
				w.warn("Updates", report.Update.Latest+" is out; it installs automatically")
			} else {
				w.row(true, "Up to date", report.Version+" is the latest")
			}
		}
	} else {
		w.row(true, "Updates", "automatic")
	}
	for _, finding := range report.Findings {
		w.warn(minerFindingLabels[finding.Code], finding.Title)
	}
	next := report.Next
	if next == "" && 0 < len(report.Findings) {
		// doctor says each finding's fix
		executable, _ := cliExecutable()
		next = minerDoctorCommand(executable)
	}
	if next != "" {
		fmt.Fprintf(stdout, "\n  %s %s\n", cliColorArrow(stdout), next)
	}
	return code
}

func serviceNote(service minerServiceState) string {
	switch {
	case service.Installed && service.Active:
		return " (background service)"
	case service.Installed:
		return " (background service installed, stopped)"
	case service.Foreign != "" && service.Active:
		return " (the installer's " + service.Foreign + ")"
	case service.Foreign != "":
		return " (the installer's " + service.Foreign + ", stopped)"
	}
	return ""
}

func cliColorArrow(w io.Writer) string {
	return cliPaint(w, "36", "→")
}

type statusWriter struct {
	out io.Writer
}

// row writes one checklist line: ok true is ✓, false is ✗, nil is –.
func (self statusWriter) row(ok any, label string, detail string) {
	mark := cliDim(self.out, "–")
	switch ok {
	case true:
		mark = cliGreen(self.out, "✓")
	case false:
		mark = cliRed(self.out, "✗")
	}
	fmt.Fprintf(self.out, "  %s %-12s %s\n", mark, label, detail)
}

func (self statusWriter) warn(label string, detail string) {
	fmt.Fprintf(self.out, "  %s %-12s %s\n", cliYellow(self.out, "!"), label, detail)
}

// The extender's router port mapping line, while a running miner reports it
// (connect/EXTENDER.md S10). A router that did not open the ports is a warning:
// the miner provides without it.
func (self statusWriter) portMapping(status *minerPortMappingStatus) {
	if status == nil {
		return
	}
	ok, warn, detail := minerPortMappingRow(status)
	if warn {
		self.warn("Port mapping", detail)
	} else {
		self.row(ok, "Port mapping", detail)
	}
}

// mark writes a row whose mark may also be "!", a warning.
func (self statusWriter) mark(mark any, label string, detail string) {
	if mark == "!" {
		self.warn(label, detail)
		return
	}
	self.row(mark, label, detail)
}

// explain writes a finding's summary and fix under the row before it,
// indented past the label, each fix line on its own.
func (self statusWriter) explain(summary string, fix string) {
	indent := strings.Repeat(" ", 17)
	if summary != "" {
		fmt.Fprintf(self.out, "%s%s\n", indent, summary)
	}
	if fix == "" {
		return
	}
	fmt.Fprintf(self.out, "%s%s\n", indent, cliBold(self.out, "Fix:"))
	for _, line := range strings.Split(fix, "\n") {
		fmt.Fprintf(self.out, "%s  %s\n", indent, line)
	}
}

// doctor

func doctorCmd(opts docopt.Opts, stdout io.Writer) int {
	settings := defaultDoctorSettings()
	w := statusWriter{out: stdout}
	problems := 0
	fail := func(label string, detail string) {
		problems++
		w.row(false, label, detail)
	}
	fmt.Fprintf(stdout, "%s\n\n", cliBold(stdout, "provider doctor"))

	// the state folder
	dir, err := providerStateDir()
	if err != nil {
		fail("State folder", err.Error())
	} else if info, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		w.row(nil, "State folder", displayStateDir()+" doesn't exist yet (provider login makes it)")
	} else if err != nil {
		fail("State folder", err.Error())
	} else if detail := stateDirProblem(dir, info); detail != "" {
		fail("State folder", detail)
	} else {
		w.row(true, "State folder", displayStateDir()+" is private")
	}

	// whether the state folder, the device's identity, outlives this
	// container and a reboot (state_persistence.go)
	facts := readMinerHostFacts(minerHostRoot, os.Getenv)
	executable, _ := cliExecutable()
	if dir != "" {
		persistence := assessMinerStatePersistence(facts, dir)
		if finding := minerStatePersistenceFinding(persistence, readMinerStateSubstitution(os.Getenv, dir), false, minerContainerImage(executable), time.Now()); finding != nil {
			fail("Identity", finding.Title)
			w.explain(finding.Summary, finding.Fix)
		} else if persistence.container != "" && persistence.mounted {
			w.row(true, "Identity", "kept: "+dir+" is on its own mount, "+persistence.mount.mountPoint+", which outlives this container")
		} else {
			w.row(true, "Identity", "kept in "+displayStateDir())
		}
	}

	report, _ := buildMinerStatusReport(time.Now())
	if report.SignedIn {
		w.row(true, "Signed in", report.Network)
	} else {
		fail("Signed in", "no: provider setup")
	}

	// the network: DNS, TCP and the TLS certificate the miner will see
	apiUrl, err := resolveApiUrl(docopt.Opts{})
	if err != nil {
		fail("Network", err.Error())
	} else {
		for _, check := range doctorNetwork(apiUrl) {
			if check.ok {
				w.row(true, check.label, check.detail)
			} else {
				fail(check.label, check.detail)
			}
		}
	}

	// the miner itself
	switch {
	case report.Unreported:
		w.row(nil, "Miner", "running, but this miner doesn't report whether it's connected; its log: provider service logs")
	case report.Running && report.Connected:
		w.row(true, "Miner", "running and connected")
	case report.Running:
		fail("Miner", "running but not connected after "+cliDuration(time.Duration(report.UptimeS)*time.Second)+"; its log says why: "+doctorLogHint(report.Service))
	case report.Service.Installed:
		fail("Miner", "the background service isn't running: provider service start")
	default:
		w.row(nil, "Miner", "not running ("+minerStartHint()+")")
	}

	// inbound: the extender's tcp 443 bind and its last activation, which the
	// operator's probe answers. A warning, not a problem: the role is optional
	if report.Running {
		var standing *minerExtenderStanding
		fix := ""
		if report.Extender != nil {
			standing, fix = &report.Extender.minerExtenderStanding, report.Extender.Fix
		}
		mark, detail := minerExtenderRow(standing, fix, os.Geteuid(), time.Now())
		w.mark(mark, "Inbound 443", detail)
	} else {
		w.row(nil, "Inbound 443", "the miner isn't running, so there is no extender to check")
	}
	// the extender's router port mapping, which is what can open 443 behind a
	// home router (connect/EXTENDER.md S10); a warning at worst, as above
	w.portMapping(report.PortMapping)

	// updates: can this binary replace itself?
	if facts.container != "" {
		w.row(nil, "Updates", "by a new image: this container's miner never updates itself")
	} else if command := report.Update.PackageManager; command != "" {
		w.row(true, "Updates", "by its package manager: "+command)
	} else if by := report.Update.By; by != "" {
		if report.Update.Auto {
			w.row(true, "Updates", "automatic, by "+by)
		} else {
			w.row(nil, "Updates", report.Update.Why+" (provider update --on)")
		}
	} else if executable != "" {
		if _, release := parseMinerReleaseVersion(cliBareVersion()); release && report.Update.Auto {
			if problem := writableDirProblem(filepath.Dir(executable)); problem != "" {
				fail("Updates", "can't install updates: "+problem)
			} else {
				w.row(true, "Updates", "automatic, and "+filepath.Dir(executable)+" is writable")
			}
		}
	}

	// the exit's DNS: do the encrypted DNS servers that clients, and the
	// network's quality check, resolve names with through this device answer
	// from here (exit_dns.go)
	exitDns := checkMinerExitDns(context.Background(), settings.ExitDnsTimeout, connect.DefaultDnsResolverSettings().RemoteDohUrlsIpv4)
	if exitDns.ok() {
		w.row(true, "Exit DNS", exitDns.detail())
	} else {
		fail("Exit DNS", exitDns.detail())
		w.explain(exitDns.summary(), exitDns.fix(0 < len(readProxySettings())))
	}

	// the version: is a newer release out that nothing here installs
	// (release_check.go)
	current := cliBareVersion()
	if running, release := parseMinerReleaseVersion(current); !release {
		w.row(nil, "Version", current+": a development build, so no release replaces it")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), settings.ReleaseTimeout)
		latest, err := doctorRelease(ctx)
		cancel()
		if err != nil {
			w.row(nil, "Version", current+"; can't read the latest release: "+firstLine(err.Error()))
		} else if finding := minerUpdateFinding(current, latest, readMinerUpdater(facts.container), minerContainerImage(executable), time.Now(), defaultMinerReleaseCheckSettings()); finding != nil {
			fail("Version", finding.Title)
			w.explain(finding.Summary, finding.Fix)
		} else if newer, _ := parseMinerReleaseVersion(latest.TagName); newer.newerThan(running) {
			w.row(true, "Version", latest.version()+" is out, and this miner's automatic update installs it")
		} else {
			w.row(true, "Version", current+" is the latest release")
		}
	}

	fmt.Fprintln(stdout)
	if problems == 0 {
		fmt.Fprintln(stdout, "No problems found.")
		return 0
	}
	fmt.Fprintf(stdout, "%d problem%s found.\n", problems, map[bool]string{true: "", false: "s"}[problems == 1])
	return 1
}

func doctorLogHint(service minerServiceState) string {
	if service.Installed {
		return "provider service logs"
	}
	return displayStatePath(minerLogFileName)
}

func stateDirProblem(dir string, info os.FileInfo) string {
	if !info.IsDir() {
		return dir + " isn't a folder"
	}
	return privateDirProblem(dir, info)
}

func writableDirProblem(dir string) string {
	probe, err := os.CreateTemp(dir, ".provider-update-probe-*")
	if err != nil {
		return dir + " isn't writable by this user"
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return ""
}

type doctorCheck struct {
	ok     bool
	label  string
	detail string
}

// The waits of doctor's checks of the release feed and the exit's DNS.
type doctorSettings struct {
	// ReleaseTimeout bounds the read of the release feed: 15 seconds, a
	// GitHub answer with room for a slow link.
	ReleaseTimeout time.Duration
	// ExitDnsTimeout bounds each encrypted DNS server's answer: 5 seconds,
	// the network quality check's DNS budget through a provider (dial_dns).
	ExitDnsTimeout time.Duration
}

func defaultDoctorSettings() *doctorSettings {
	return &doctorSettings{
		ReleaseTimeout: 15 * time.Second,
		ExitDnsTimeout: 5 * time.Second,
	}
}

// doctorRelease reads the latest release for doctor's Version row, as
// provider update does. A variable so tests stay off the network.
var doctorRelease = func(ctx context.Context) (*minerRelease, error) {
	return fetchMinerRelease(ctx)
}

// doctorNetwork resolves the API host, connects to it and looks at the TLS
// certificate. The miner trusts only its own certificate roots, so a proxy
// that inspects TLS breaks it even when a browser works. A variable so tests
// of the other checks stay off the network.
var doctorNetwork = func(apiUrl string) []doctorCheck {
	parsed, err := url.Parse(apiUrl)
	if err != nil || parsed.Hostname() == "" {
		return []doctorCheck{{false, "Network", "the API URL " + apiUrl + " isn't valid"}}
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(addresses) == 0 {
		return []doctorCheck{{false, "DNS", "can't look up " + host + ": check this device's internet connection"}}
	}
	checks := []doctorCheck{{true, "DNS", host + " resolves"}}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return append(checks, doctorCheck{false, "Connection", "can't reach " + host + ":" + port + ": " + firstLine(err.Error())})
	}
	raw.Close()
	checks = append(checks, doctorCheck{true, "Connection", "reached " + host})
	if config, err := connect.DefaultTlsConfig(); err != nil {
		checks = append(checks, doctorCheck{false, "TLS", firstLine(err.Error())})
	} else {
		config = config.Clone()
		config.ServerName = host
		conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, port), config)
		var unknown x509.UnknownAuthorityError
		switch {
		case errors.As(err, &unknown):
			checks = append(checks, doctorCheck{false, "TLS", host + "'s certificate isn't signed by a root the miner trusts.\n" +
				"                 A proxy or firewall on this network probably inspects TLS. Set\n" +
				"                 URNETWORK_CONNECT_EXTRA_ROOT_CA_FILE to a file of its root CA certificates."})
		case err != nil:
			checks = append(checks, doctorCheck{false, "TLS", "the TLS handshake with " + host + " failed: " + firstLine(err.Error())})
		default:
			conn.Close()
			checks = append(checks, doctorCheck{true, "TLS", "certificate trusted by the miner"})
		}
	}
	if skew, ok := doctorClockSkew(ctx, apiUrl); ok {
		if 2*time.Minute < skew || skew < -2*time.Minute {
			checks = append(checks, doctorCheck{false, "Clock", fmt.Sprintf("this device's clock is off by %s; sign-in fails with a wrong clock. Turn on network time (NTP)", cliDuration(absDuration(skew)))})
		} else {
			checks = append(checks, doctorCheck{true, "Clock", "in sync"})
		}
	}
	return checks
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// doctorClockSkew compares this clock with the API server's Date header.
func doctorClockSkew(ctx context.Context, apiUrl string) (time.Duration, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, apiUrl, nil)
	if err != nil {
		return 0, false
	}
	pool, _ := x509.SystemCertPool()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, InsecureSkipVerify: true}}, Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return 0, false
	}
	response.Body.Close()
	date, err := http.ParseTime(response.Header.Get("Date"))
	if err != nil {
		return 0, false
	}
	return time.Since(date), true
}
