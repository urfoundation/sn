package miner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/docopt/docopt-go"
)

// provider start is provide with a friendlier first run and, in a terminal,
// a short running account instead of the raw logs. provide itself keeps its
// output; both refuse to start a device that was never signed in.

// minerExitNeedsSetup is the exit code of a miner that can't start until the
// device is set up (signed in, or registered). provider status uses it too.
const minerExitNeedsSetup = 3

// cliTerminal is the terminal, once provider start has sent stderr to its
// log. Messages meant for a person go here.
var cliTerminal *os.File

const minerLogFileName = "miner.log"

// A log over this size is cut back to empty when the next start opens it.
const minerLogMaxBytes = 20 << 20

// providePreflight stops a start that can only fail, with a plain message
// and minerExitNeedsSetup. For start in a terminal it asks before
// registering a new device. It returns false when the miner should not start.
func providePreflight(opts docopt.Opts, start bool, in *os.File, out io.Writer, stderr io.Writer) bool {
	for _, option := range []string{"--whole-work-capture", "--original-contract-capture", "--close-report-domain"} {
		if value, _ := opts.String(option); value != "" {
			return true
		}
	}
	for _, option := range []string{"--require-whole-work-capture", "--require-original-contract-capture", "--adopt-legacy-provider-key", "--quarantine-rejected-sign-in"} {
		if set, _ := opts.Bool(option); set {
			return true
		}
	}
	signIn := readMinerSignIn()
	device := readMinerDevice()
	if !signIn.signedIn && !device.hasHistory {
		writeCliError(stderr, "This device isn't signed in yet. Set it up with:\n"+
			"  provider setup\n"+
			"  (or sign in by hand with provider login, using an auth code from your URnetwork app)")
		return false
	}
	if allow, _ := opts.Bool("--allow-client-registration"); !start || allow || device.registered {
		return true
	}
	if device.hasHistory {
		// an earlier first start made the key but never got a client;
		// only a person who sees this finishes it without the flag
		if !device.hasKey || !cliIsTerminal(in) || !cliIsTerminal(out) {
			return true
		}
		fmt.Fprintln(out, "This device's registration didn't finish last time.")
		if cliAsk(in, out, "Finish registering it now?", true) {
			opts["--allow-client-registration"] = true
			return true
		}
		fmt.Fprintln(out, "Not registered. Run provider start again when you're ready.")
		return false
	}
	if cliIsTerminal(in) && cliIsTerminal(out) {
		network := ""
		if signIn.networkName != "" {
			network = fmt.Sprintf(" on network %q", signIn.networkName)
		}
		fmt.Fprintf(out, "This is the first start on this device, so it needs its own miner identity.\n")
		if cliAsk(in, out, "Register it as a new device"+network+"?", true) {
			opts["--allow-client-registration"] = true
			return true
		}
		fmt.Fprintln(out, "Not registered. Run provider start again when you're ready.")
		return false
	}
	writeCliError(stderr, "This is the first start on this device, so it needs its own miner identity.\n"+
		"  Register it as a new device with:\n"+
		"  provider start --allow-client-registration")
	return false
}

// startTerminalUi reports a terminal start in a few plain lines. The raw
// logs and diagnostics go to miner.log in the state directory.
type startTerminalUi struct {
	out     io.Writer
	log     io.Writer
	logPath string

	stateLock   sync.Mutex
	lineBuffer  []byte
	connected   bool
	everDone    bool
	warnedSlow  bool
	registered  bool
	signInIssue bool
	startedAt   time.Time
	stop        chan struct{}
	stopped     chan struct{}
}

// newStartTerminalUi moves stderr (glog, Go log, crash output) to the log
// file and keeps the terminal for itself. It returns nil when stderr can't
// be moved, and the miner then runs with its usual output.
func newStartTerminalUi(stdout *os.File) *startTerminalUi {
	logPath, err := providerStatePath(minerLogFileName)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(logPath); err == nil && minerLogMaxBytes < info.Size() {
		_ = os.Truncate(logPath, 0)
	}
	terminal, err := redirectStderrToFile(logPath)
	if err != nil {
		return nil
	}
	cliTerminal = terminal
	fmt.Fprintf(os.Stderr, "\n==== provider %s start %s ====\n", cliBareVersion(), time.Now().UTC().Format(time.RFC3339))
	return &startTerminalUi{out: stdout, log: os.Stderr, logPath: logPath, startedAt: time.Now(), stop: make(chan struct{}), stopped: make(chan struct{})}
}

func (self *startTerminalUi) begin() {
	signIn := readMinerSignIn()
	fmt.Fprintf(self.out, "%s %s\n", cliBold(self.out, "provider "+cliBareVersion()), cliDim(self.out, "· logs in "+displayStatePath(minerLogFileName)))
	if signIn.networkName != "" {
		self.check(true, "Signed in", fmt.Sprintf("network %q", signIn.networkName))
	} else {
		self.check(true, "Signed in", "")
	}
	if device := readMinerDevice(); device.clientId != "" {
		self.registered = true
		self.check(true, "Miner identity", shortId(device.clientId))
	}
	fmt.Fprintln(self.out, cliDim(self.out, "  … Connecting"))
	go self.watch()
}

func (self *startTerminalUi) check(ok bool, label string, detail string) {
	mark := cliGreen(self.out, "✓")
	if !ok {
		mark = cliYellow(self.out, "!")
	}
	if detail != "" {
		fmt.Fprintf(self.out, "  %s %-15s %s\n", mark, label, detail)
	} else {
		fmt.Fprintf(self.out, "  %s %s\n", mark, label)
	}
}

// watch follows the miner's status file and says when it connects, when it
// is slow to, and when it loses the connection.
func (self *startTerminalUi) watch() {
	defer close(self.stopped)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-self.stop:
			return
		case <-ticker.C:
		}
		status, err := readMinerLocalStatus()
		if err != nil || status.Pid != os.Getpid() {
			continue
		}
		self.stateLock.Lock()
		if !self.registered {
			for _, member := range status.Members {
				if member.ClientId != "" {
					self.registered = true
					self.check(true, "Miner identity", shortId(member.ClientId))
					break
				}
			}
		}
		connected := status.connected()
		switch {
		case connected && !self.connected:
			detail := ""
			if 1 < len(status.Members) {
				detail = fmt.Sprintf("%d of %d", status.connectedCount(), len(status.Members))
			}
			if self.everDone {
				self.check(true, "Reconnected", detail)
			} else {
				self.check(true, "Connected", detail+cliDim(self.out, fmt.Sprintf("  %.0fs", time.Since(self.startedAt).Seconds())))
				fmt.Fprintf(self.out, "\n%s Leave this open, or run it in the background:\n  provider service install\n%s\n", cliGreen(self.out, "● Mining."), cliDim(self.out, "Press Ctrl-C to stop."))
			}
			self.everDone = true
		case !connected && self.connected:
			self.check(false, "Connection lost", "reconnecting…")
		case !connected && !self.everDone && !self.warnedSlow && 60*time.Second < time.Since(self.startedAt):
			self.warnedSlow = true
			self.check(false, "Not connected yet", "still trying. Check: provider doctor")
		}
		self.connected = connected
		self.stateLock.Unlock()
	}
}

// Write takes the miner's diagnostics: every line goes to the log, and the
// few a person must act on are also said plainly.
func (self *startTerminalUi) Write(p []byte) (int, error) {
	_, _ = self.log.Write(p)
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.lineBuffer = append(self.lineBuffer, p...)
	for {
		i := bytes.IndexByte(self.lineBuffer, '\n')
		if i < 0 {
			break
		}
		line := self.lineBuffer[:i]
		self.lineBuffer = self.lineBuffer[i+1:]
		self.diagnostic(line)
	}
	return len(p), nil
}

func (self *startTerminalUi) diagnostic(line []byte) {
	record := struct {
		Event string `json:"event"`
	}{}
	if json.Unmarshal(line, &record) != nil {
		return
	}
	switch record.Event {
	case "network_sign_in_rejected", "network_sign_in_quarantined":
		if !self.signInIssue {
			self.signInIssue = true
			fmt.Fprintf(self.out, "  %s %s\n", cliRed(self.out, "✗"), "The network didn't accept this device's sign-in (it may have expired).\n    Sign in again with: provider login, then provider start")
		}
	case "network_sign_in_renewal_stopped":
		self.check(false, "Sign-in", "can't be renewed; it keeps working until it expires. Run provider login soon")
	case "startup_recovery_required":
		fmt.Fprintf(self.out, "  %s %s\n", cliRed(self.out, "✗"), "This device's miner identity is incomplete: files in "+displayStateDir()+" are missing or changed.\n    Restore your backup of that folder. Details: https://ur.xyz/docs/miner")
	case "status_failed":
		self.check(false, "Status server", "couldn't start (is the --port in use?)")
	}
}

// finish says how the run ended.
func (self *startTerminalUi) finish(err error, interrupted bool) {
	close(self.stop)
	<-self.stopped
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	switch {
	case interrupted:
		fmt.Fprintln(self.out, "\nStopped.")
	case err != nil:
		fmt.Fprintf(self.log, "provider start: the miner stopped: %v\n", err)
		if strings.Contains(err.Error(), "requires recovery") || strings.Contains(err.Error(), "requires_explicit_recovery") || strings.Contains(err.Error(), "retained history") {
			fmt.Fprintf(self.out, "\n%s This device's miner identity in %s is incomplete or doesn't match,\n  so the miner won't start rather than risk making a second identity.\n  If you have a backup of that folder, restore it. Details: %s\n", cliRed(self.out, "✗"), displayStateDir(), displayStatePath(minerLogFileName))
			return
		}
		fmt.Fprintf(self.out, "\n%s The miner stopped: %s\n  Details: %s\n", cliRed(self.out, "✗"), firstLine(err.Error()), displayStatePath(minerLogFileName))
	default:
		fmt.Fprintf(self.out, "\nThe miner stopped. Details: %s\n", displayStatePath(minerLogFileName))
	}
}

// displayStatePath is a file in the state directory as the user would type it.
func displayStatePath(name string) string {
	return strings.TrimSuffix(displayStateDir(), "/") + "/" + name
}

func cliPaint(w io.Writer, code string, text string) string {
	if !cliColor(w) {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func cliGreen(w io.Writer, text string) string  { return cliPaint(w, "32", text) }
func cliYellow(w io.Writer, text string) string { return cliPaint(w, "33", text) }
func cliRed(w io.Writer, text string) string    { return cliPaint(w, "31", text) }
func cliDim(w io.Writer, text string) string    { return cliPaint(w, "2", text) }
func cliBold(w io.Writer, text string) string   { return cliPaint(w, "1", text) }
