package miner

import (
	"os"
	"os/user"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/urnetwork/sdk/v2026"
)

// What the miner tells its operator about the extender role (connect/EXTENDER.md
// G2, C2a). The role is off while tcp 443 won't bind, and stays unpublished
// while the operator's probe can't reach the host on that port, and until now
// the log said only listen_failed. A standing is a closed vocabulary read from
// the SDK status, never the SDK's own text, which can carry addresses
// (provider_diagnostics.go). It is what the log's guidance, provider status
// and provider doctor report, each with the fix.

// The page that says how to run an extender: the ports, firewalls, security
// groups, port forwarding and the Linux privilege.
const minerExtenderDocs = "https://ur.xyz/docs/miner/extender"

// The installer's switch to a system service that may bind tcp 443
// (connect scripts/Provider_Install_Linux.sh --system).
const minerExtenderInstallerFix = "curl -fsSL https://raw.githubusercontent.com/urnetwork/connect/main/scripts/Provider_Install_Linux.sh | sh -s -- --system"

// Why the extender is off or unpublished. Empty while nothing is wrong.
const (
	// The host refused the bind of tcp 443: on Linux a process without
	// CAP_NET_BIND_SERVICE may not bind a port below 1024.
	minerExtenderReasonTcpPermission = "tcp_443_permission_denied"
	// Another process holds tcp 443. Only one process per host can.
	minerExtenderReasonTcpInUse = "tcp_443_in_use"
	// Any other failure to bind tcp 443.
	minerExtenderReasonTcpBindFailed = "tcp_443_bind_failed"
	// The operator's required test of tcp 443 found no answer: a firewall, a
	// security group or a router that doesn't forward the port.
	minerExtenderReasonUnreachable = "unreachable_on_tcp_443"
)

// The standing of the tcp 443 bind: one of these, or a tcp reason above.
const (
	minerExtenderBindOff       = "off"
	minerExtenderBindStarting  = "starting"
	minerExtenderBindListening = "listening"
)

// The standing of the last activation.
const (
	// the role is not running, so nothing is activated
	minerExtenderActivationNone      = "none"
	minerExtenderActivationPending   = "pending"
	minerExtenderActivationActivated = "activated"
	// the operator's probe couldn't reach tcp 443 (minerExtenderReasonUnreachable)
	minerExtenderActivationUnreachable = minerExtenderReasonUnreachable
	minerExtenderActivationRefused     = "refused"
	minerExtenderActivationFailed      = "failed"
	minerExtenderActivationRevoked     = "revoked"
)

// minerExtenderStanding is the extender role of one provider slot, in the
// status file's words.
type minerExtenderStanding struct {
	Bind             string     `json:"bind"`
	Activation       string     `json:"activation"`
	Reason           string     `json:"reason,omitempty"`
	LastActivationAt *time.Time `json:"last_activation_at,omitempty"`
}

// minerExtenderStandingOf reads the SDK status of a provider slot. It reads
// the error case the SDK derived and the bind error's kind, never its text.
func minerExtenderStandingOf(status *sdk.ExtenderProvideStatus) minerExtenderStanding {
	off := minerExtenderStanding{Bind: minerExtenderBindOff, Activation: minerExtenderActivationNone}
	if status == nil {
		return off
	}
	if status.TcpUnavailableError != "" || status.ErrorCase == sdk.ExtenderProvideErrorTcpUnavailable {
		// the role is off and binds again every few minutes (G2)
		reason := minerExtenderTcpBindReason(status.TcpUnavailableError)
		return minerExtenderStanding{Bind: reason, Activation: minerExtenderActivationNone, Reason: reason}
	}
	if !status.Enabled {
		return off
	}
	standing := minerExtenderStanding{Bind: minerExtenderBindStarting, Activation: minerExtenderActivationPending}
	switch {
	case status.Listening:
		standing.Bind = minerExtenderBindListening
	case status.ListenError != "":
		// the tcp carrier was lost and is being bound again, or nothing bound
		standing.Bind = minerExtenderTcpBindReason(status.ListenError)
		standing.Reason = standing.Bind
	}
	if status.LastActivationTime != 0 {
		at := time.UnixMilli(status.LastActivationTime).UTC()
		standing.LastActivationAt = &at
	}
	switch {
	case status.RevokedTime != 0:
		standing.Activation = minerExtenderActivationRevoked
	case status.ActivatedV4 || status.ActivatedV6:
		standing.Activation = minerExtenderActivationActivated
	case status.LastActivationTime != 0 && status.LastActivationError != "":
		switch {
		case minerExtenderActivationUnreachableStatus(status):
			standing.Activation = minerExtenderActivationUnreachable
			if standing.Reason == "" {
				standing.Reason = minerExtenderReasonUnreachable
			}
		case status.LastActivationRefused:
			standing.Activation = minerExtenderActivationRefused
		default:
			standing.Activation = minerExtenderActivationFailed
		}
	}
	return standing
}

// Whether the last activation was refused because the operator's required
// test of the tcp carrier found no answer (connect 1e5483ef,
// ExtenderActivateResult.RequiredTestUnreachable): the case the SDK derived,
// or, for a status read without it, the operator's own words for that test,
// which are what the SDK's rule reads too.
func minerExtenderActivationUnreachableStatus(status *sdk.ExtenderProvideStatus) bool {
	if status.ErrorCase == sdk.ExtenderProvideErrorActivationUnreachable {
		return true
	}
	return status.LastActivationRefused &&
		(strings.HasPrefix(status.LastActivationError, "the tcp carrier did not answer") ||
			strings.HasPrefix(status.LastActivationError, "the tcp carrier failed"))
}

// The kind of a tcp 443 bind error, as the operating system words it: EACCES
// and EPERM on Linux and macOS, WSAEACCES on Windows; EADDRINUSE and
// WSAEADDRINUSE.
func minerExtenderTcpBindReason(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "operation not permitted"),
		strings.Contains(lower, "forbidden by its access permissions"):
		return minerExtenderReasonTcpPermission
	case strings.Contains(lower, "address already in use"),
		strings.Contains(lower, "only one usage of each socket address"):
		return minerExtenderReasonTcpInUse
	}
	return minerExtenderReasonTcpBindFailed
}

// minerExtenderBindFixFor is the command that lets this miner bind tcp 443:
// on Linux, a system service that runs it as this user with only
// CAP_NET_BIND_SERVICE, through the installer that set it up when there is
// one. Empty where there is no such command: other systems don't keep port
// 443 from a user, and a miner that already runs as root lacks the
// capability because its container or sandbox took it away.
func minerExtenderBindFixFor(goos string, executable string, installer bool, username string, euid int) string {
	if goos != "linux" || euid == 0 {
		return ""
	}
	if installer {
		return minerExtenderInstallerFix
	}
	if executable == "" || username == "" {
		return ""
	}
	return "sudo " + cliShellQuote(executable) + " service install --system --as=" + cliShellQuote(username)
}

// minerExtenderBindFix is minerExtenderBindFixFor for this process.
func minerExtenderBindFix() string {
	executable, err := cliExecutable()
	if err != nil {
		executable = ""
	}
	username := ""
	if current, err := user.Current(); err == nil {
		username = current.Username
	}
	return minerExtenderBindFixFor(runtime.GOOS, executable, executable != "" && findMinerInstaller(executable) != nil, username, os.Geteuid())
}

// minerExtenderGuidance is the log's sentence for a reason, with the fix.
func minerExtenderGuidance(reason string, bindFix string, euid int) string {
	switch reason {
	case minerExtenderReasonTcpPermission:
		switch {
		case bindFix != "":
			return "The extender is off: this miner may not bind TCP port 443, which an extender needs, and it tries again every 3 minutes. To let it bind the port, run: " + bindFix + " (see " + minerExtenderDocs + ")."
		case euid == 0:
			return "The extender is off: this miner runs as root but may not bind TCP port 443, so its container or service sandbox lacks CAP_NET_BIND_SERVICE. It tries again every 3 minutes. See " + minerExtenderDocs + "."
		}
		return "The extender is off: this system refused this miner TCP port 443, which an extender needs, and it tries again every 3 minutes. See " + minerExtenderDocs + "."
	case minerExtenderReasonTcpInUse:
		return "The extender is off: another program on this host holds TCP port 443 (another miner, or a web server). Only one process per host can be the extender; this one takes the port within 3 minutes of it coming free. See " + minerExtenderDocs + "."
	case minerExtenderReasonTcpBindFailed:
		return "The extender is off: this miner can't bind TCP port 443, which an extender needs, and it tries again every 3 minutes. See " + minerExtenderDocs + "."
	case minerExtenderReasonUnreachable:
		return "The extender isn't active: the network can't reach this device on TCP port 443 (unreachable on TCP 443). Allow inbound TCP 443, UDP 443 and UDP 4053 in this host's firewall and any cloud security group, or forward them on your router to this device, then restart the miner to try again at once (otherwise it tries once a day). See " + minerExtenderDocs + "."
	}
	return ""
}

// minerExtenderStandings keeps each provider slot's standing for the status
// file. Only one slot of a run carries the role, so the others report off.
// Safe for concurrent use.
type minerExtenderStandings struct {
	stateLock sync.Mutex
	byIndex   map[uint64]minerExtenderStanding
}

func newMinerExtenderStandings() *minerExtenderStandings {
	return &minerExtenderStandings{byIndex: map[uint64]minerExtenderStanding{}}
}

func (self *minerExtenderStandings) set(provider uint64, standing minerExtenderStanding) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.byIndex[provider] = standing
}

// current is the standing of the slot that runs the role, the lowest such
// slot when more than one does; off when every slot reported off, and nil
// when none reported, such as before the first status.
func (self *minerExtenderStandings) current() *minerExtenderStanding {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.byIndex) == 0 {
		return nil
	}
	indexes := make([]uint64, 0, len(self.byIndex))
	for index := range self.byIndex {
		indexes = append(indexes, index)
	}
	sort.Slice(indexes, func(i int, j int) bool { return indexes[i] < indexes[j] })
	for _, index := range indexes {
		if standing := self.byIndex[index]; standing.Bind != minerExtenderBindOff {
			return &standing
		}
	}
	return &minerExtenderStanding{Bind: minerExtenderBindOff, Activation: minerExtenderActivationNone}
}

// minerExtenderRow is what provider status and provider doctor say of the
// extender: a mark (true, false or nil, as statusWriter.row; "!" warns) and
// the detail, with the fix. Nil standing: the miner doesn't report it.
func minerExtenderRow(standing *minerExtenderStanding, bindFix string, euid int, now time.Time) (mark any, detail string) {
	if standing == nil {
		return nil, "this miner doesn't report its extender (an older version)"
	}
	last := ""
	if standing.LastActivationAt != nil {
		last = ", last tried " + cliDuration(now.Sub(*standing.LastActivationAt)) + " ago"
	}
	switch standing.Bind {
	case minerExtenderBindOff:
		return nil, "off on this miner"
	case minerExtenderReasonTcpPermission:
		if bindFix != "" {
			return "!", "off: may not bind TCP 443 (permission denied). Fix: " + bindFix
		}
		return "!", "off: may not bind TCP 443 (permission denied); see " + minerExtenderDocs
	case minerExtenderReasonTcpInUse:
		return "!", "off: another program holds TCP 443 (only one per host can be the extender)"
	case minerExtenderReasonTcpBindFailed:
		return "!", "off: can't bind TCP 443; see " + minerExtenderDocs
	}
	switch standing.Activation {
	case minerExtenderActivationActivated:
		return true, "TCP 443 bound and activated" + last
	case minerExtenderActivationUnreachable:
		return "!", "TCP 443 bound, but unreachable on TCP 443 from the internet" + last + ". Open inbound TCP 443, UDP 443 and UDP 4053 (firewall, security group or router), then restart the miner; see " + minerExtenderDocs
	case minerExtenderActivationRefused:
		return "!", "TCP 443 bound, but the operator refused the last activation" + last
	case minerExtenderActivationFailed:
		return "!", "TCP 443 bound, but the last activation failed" + last
	case minerExtenderActivationRevoked:
		return "!", "revoked by the operator"
	}
	if standing.Bind == minerExtenderBindListening {
		return nil, "TCP 443 bound, activating"
	}
	return nil, "starting"
}
